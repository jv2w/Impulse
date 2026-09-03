package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/item/recipe"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
	"sync"
	"time"
)

// brewer is a struct that may be embedded by blocks that can brew potions, such as brewing stands.
type brewer struct {
	mu sync.Mutex

	viewers   map[ContainerViewer]struct{}
	inventory *inventory.Inventory

	duration   time.Duration
	fuelAmount int32
	fuelTotal  int32
}

// newBrewer creates a new initialised brewer. The inventory is properly initialised.
func newBrewer() *brewer {
	b := &brewer{viewers: make(map[ContainerViewer]struct{})}
	b.inventory = inventory.New(5, func(slot int, _, item item.Stack) {
		b.mu.Lock()
		defer b.mu.Unlock()
		for viewer := range b.viewers {
			viewer.ViewSlotChange(slot, item)
		}
	})
	return b
}

// InsertItem ...
func (b *brewer) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	// The brewer inventory is cloned by Slots(); the loop returns as soon as it writes to that
	// inventory, so a single clone serves every pass and it is only made once one is needed.
	var brewingSlots []item.Stack
	for sourceSlot, sourceStack := range h.inventory.Slots() {
		var slot int

		if sourceStack.Empty() {
			continue
		}

		// The item behind the stack was read up to five times below and does not change here.
		sourceItem := sourceStack.Item()
		if h.Facing == cube.FaceDown {
			if !recipe.ValidBrewingReagent(sourceItem) {
				// This item is not a valid brewing reagent.
				continue
			}
			slot = 0
		} else if _, ok := sourceItem.(item.BlazePowder); ok {
			slot = 4
		} else {
			_, okPotion := sourceItem.(item.Potion)
			_, okSplash := sourceItem.(item.SplashPotion)
			_, okLingering := sourceItem.(item.LingeringPotion)
			_, okBottle := sourceItem.(item.GlassBottle)
			if !okPotion && !okSplash && !okLingering && !okBottle {
				continue
			}
			if brewingSlots == nil {
				brewingSlots = b.inventory.Slots()
			}
			for brewingSlot, brewingStack := range brewingSlots {
				if brewingSlot == 0 || brewingSlot == 4 {
					continue
				}
				if brewingStack.Count() == brewingStack.MaxCount() || !brewingStack.Comparable(sourceStack) {
					continue
				}

				slot = brewingSlot
				break
			}
			// Could not find an empty slot
			if slot == 0 {
				continue
			}
		}

		stack := sourceStack.Grow(-sourceStack.Count() + 1)
		// Inventory returns the same inventory for the same brewer, so it is asked once instead
		// of once for the read and once for the write.
		inv := b.Inventory(tx, pos)
		it, _ := inv.Item(slot)

		if !sourceStack.Comparable(it) {
			// The items are not the same.
			continue
		}
		if it.Count() == it.MaxCount() {
			// The item has the maximum count that the stack is able to hold.
			continue
		}
		if !it.Empty() {
			stack = it.Grow(1)
		}

		_ = inv.SetItem(slot, stack)
		_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
		return true

	}
	return false
}

// ExtractItem ...
func (b *brewer) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for sourceSlot, sourceStack := range b.inventory.Slots() {
		if sourceStack.Empty() || sourceSlot == 0 || sourceSlot == 4 {
			continue
		}
		_, err := h.inventory.AddItem(sourceStack.Grow(-sourceStack.Count() + 1))
		if err != nil {
			// The hopper is full.
			continue
		}
		_ = b.Inventory(tx, pos).SetItem(sourceSlot, sourceStack.Grow(-1))
		return true
	}
	return false
}

// Duration returns the remaining duration of the brewing process.
func (b *brewer) Duration() time.Duration {
	// The critical section is a single field read that cannot panic, so the unlock does not need
	// a defer frame to survive one.
	b.mu.Lock()
	d := b.duration
	b.mu.Unlock()
	return d
}

// Fuel returns the fuel and maximum fuel of the brewer.
func (b *brewer) Fuel() (fuel, maxFuel int32) {
	// The critical section is two field reads that cannot panic, so the unlock does not need a
	// defer frame to survive one.
	b.mu.Lock()
	fuel, maxFuel = b.fuelAmount, b.fuelTotal
	b.mu.Unlock()
	return fuel, maxFuel
}

// Inventory returns the inventory of the brewer.
func (b *brewer) Inventory(*world.Tx, cube.Pos) *inventory.Inventory {
	return b.inventory
}

// AddViewer adds a viewer to the brewer, so that it is updated whenever the inventory of the brewer is changed.
func (b *brewer) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the brewer, so that slot updates in the inventory are no longer sent to
// it.
func (b *brewer) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.viewers, v)
}

// setDuration sets the brew duration of the brewer to the given duration.
func (b *brewer) setDuration(duration time.Duration) {
	// The critical section is a single field write that cannot panic, so the unlock does not need
	// a defer frame to survive one.
	b.mu.Lock()
	b.duration = duration
	b.mu.Unlock()
}

// setFuel sets the fuel of the brewer to the given fuel and maximum fuel.
func (b *brewer) setFuel(fuel, maxFuel int32) {
	// The critical section is two field writes that cannot panic, so the unlock does not need a
	// defer frame to survive one.
	b.mu.Lock()
	b.fuelAmount, b.fuelTotal = fuel, maxFuel
	b.mu.Unlock()
}

// tickBrewing ticks the brewer, ensuring the necessary items exist in the brewer, and then processing all inputted
// items for the necessary duration.
func (b *brewer) tickBrewing(block string, pos cube.Pos, tx *world.Tx) {
	b.mu.Lock()

	// Get each item in the brewer. We don't need to validate errors here since we know the bounds of the brewer.
	left, _ := b.inventory.Item(1)
	middle, _ := b.inventory.Item(2)
	right, _ := b.inventory.Item(3)

	// Keep track of our past durations, since if any of them change, we need to be able to tell they did and then
	// update the viewers on the change.
	prevDuration := b.duration
	prevFuelAmount := b.fuelAmount
	prevFuelTotal := b.fuelTotal

	// If we need fuel, try and burn some.
	fuel, _ := b.inventory.Item(4)

	if _, ok := fuel.Item().(item.BlazePowder); ok && b.fuelAmount <= 0 {
		defer b.inventory.SetItem(4, fuel.Grow(-1))
		b.fuelAmount, b.fuelTotal = 20, 20
	}

	// Now get the ingredient item.
	ingredient, _ := b.inventory.Item(0)

	// Ensure that we have enough fuel to continue.
	if b.fuelAmount > 0 {
		// Check each input and see if it is affected by the ingredient.
		// The three lookups are pure and their results were only read inside this branch, so they
		// are done here and the ingredient behind the stack is read once instead of three times.
		ingredientItem := ingredient.Item()
		leftOutput, leftAffected := recipe.Perform(block, left.Item(), ingredientItem)
		middleOutput, middleAffected := recipe.Perform(block, middle.Item(), ingredientItem)
		rightOutput, rightAffected := recipe.Perform(block, right.Item(), ingredientItem)

		// Now make sure that we have at least one potion that is affected by the ingredient.
		if leftAffected || middleAffected || rightAffected {
			// Tick our duration. If we have no brew duration, set it to the default of twenty seconds.
			if b.duration == 0 {
				b.duration = time.Second * 20
			}
			b.duration -= time.Millisecond * 50

			// If we have no duration, we are done.
			if b.duration <= 0 {
				// Create the output items.
				if leftAffected {
					defer b.inventory.SetItem(1, leftOutput[0])
				}
				if middleAffected {
					defer b.inventory.SetItem(2, middleOutput[0])
				}
				if rightAffected {
					defer b.inventory.SetItem(3, rightOutput[0])
				}

				// Reduce the ingredient by one.
				defer b.inventory.SetItem(0, ingredient.Grow(-1))
				tx.PlaySound(pos.Vec3Centre(), sound.PotionBrewed{})

				// Decrement the fuel, and reset the duration.
				b.fuelAmount--
				b.duration = 0
			}
		} else {
			// None of the potions are affected by the ingredient, so reset the duration.
			b.duration = 0
		}
	} else {
		// We don't have enough fuel, so reset our progress.
		b.duration, b.fuelAmount, b.fuelTotal = 0, 0, 0
	}

	// Update the viewers on the new durations.
	for v := range b.viewers {
		v.ViewBrewingUpdate(prevDuration, b.duration, prevFuelAmount, b.fuelAmount, prevFuelTotal, b.fuelTotal)
	}

	b.mu.Unlock()
}
