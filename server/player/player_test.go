package player

import (
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
)

// newConsumptionPlayer builds the smallest Player that handleUseContext can run
// against: only the inventories, the held slot and the game mode are reachable
// from it as long as the UseContext carries no damage, no count subtraction and
// no new item. A Player with a tx, a handle and a session cannot be built
// outside a running world.
func newConsumptionPlayer(mainHandSlots int) *Player {
	return &Player{playerData: &playerData{
		gameMode: world.GameModeSurvival,
		inv:      inventory.New(mainHandSlots, nil),
		offHand:  inventory.New(1, nil),
		heldSlot: new(uint32),
	}}
}

// countOf reports how many items comparable to want are held across the two
// inventories.
func countOf(p *Player, want item.Stack) int {
	n := 0
	for _, s := range append(p.inv.Slots(), p.offHand.Slots()...) {
		if !s.Empty() && s.Comparable(want) {
			n += s.Count()
		}
	}
	return n
}

// Inventory.RemoveItemFunc is all or nothing, so an off-hand that holds fewer
// items than the context consumes gives up none of them. The whole consumed
// stack has to come out of the main inventory; deducting the off-hand's count
// from it leaves that many items consumed by nobody.
func TestHandleUseContextConsumesTheFullStackWhenTheOffHandIsTooSmall(t *testing.T) {
	p := newConsumptionPlayer(4)
	apple := func(n int) item.Stack { return item.NewStack(item.Apple{}, n) }

	if err := p.offHand.SetItem(0, apple(1)); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}
	if err := p.inv.SetItem(1, apple(5)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	p.handleUseContext(&item.UseContext{ConsumedItems: []item.Stack{apple(2)}})

	if got, want := countOf(p, apple(1)), 4; got != want {
		t.Fatalf("apples left = %v, want %v: %v of the consumed stack were never taken", got, want, got-want)
	}
	if got, want := p.offHand.Slots()[0].Count(), 1; got != want {
		t.Fatalf("off-hand count = %v, want %v: the rejected removal touched a slot", got, want)
	}
}

// The off-hand path still has to win when it can satisfy the removal on its
// own, leaving the main inventory alone.
func TestHandleUseContextPrefersTheOffHandWhenItCanSatisfyTheRemoval(t *testing.T) {
	p := newConsumptionPlayer(4)
	apple := func(n int) item.Stack { return item.NewStack(item.Apple{}, n) }

	if err := p.offHand.SetItem(0, apple(3)); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}
	if err := p.inv.SetItem(1, apple(5)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	p.handleUseContext(&item.UseContext{ConsumedItems: []item.Stack{apple(2)}})

	if got, want := p.offHand.Slots()[0].Count(), 1; got != want {
		t.Fatalf("off-hand count = %v, want %v", got, want)
	}
	if got, want := p.inv.Slots()[1].Count(), 5; got != want {
		t.Fatalf("inventory count = %v, want %v: the main inventory was charged as well", got, want)
	}
}
