package session

import (
	"fmt"
	"math/rand/v2"
	"unicode/utf8"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

const (
	// anvilInputSlot is the slot index of the input item in the anvil.
	anvilInputSlot = 0x1
	// anvilMaterialSlot is the slot index of the material in the anvil.
	anvilMaterialSlot = 0x2
)

// handleCraftRecipeOptional handles the CraftRecipeOptional request action, sent when taking a result from an anvil
// menu. It also contains information such as the new name of the item and the multi-recipe network ID.
func (h *ItemStackRequestHandler) handleCraftRecipeOptional(a *protocol.CraftRecipeOptionalStackRequestAction, s *Session, filterStrings []string, co Controllable, tx *world.Tx) (err error) {
	// First check if there actually is an anvil opened.
	if !s.containerOpened.Load() {
		return fmt.Errorf("no anvil container opened")
	}

	pos := *s.openedPos.Load()
	anvil, ok := tx.Block(pos).(block.Anvil)
	if !ok {
		return fmt.Errorf("no anvil container opened")
	}
	if index := int(a.FilterStringIndex); len(filterStrings) > 0 && (index < 0 || index >= len(filterStrings)) {
		return fmt.Errorf("filter string index %v is out of bounds", a.FilterStringIndex)
	}
	// Which input was the problem: the rename string had no length or
	// encoding bound, so it persisted verbatim to item NBT and inventory
	// sync for every viewer. Mirror the book signing caps: 256 bytes and
	// valid UTF-8. Valid renames take the same cost path below.
	if len(filterStrings) > 0 {
		rename := filterStrings[int(a.FilterStringIndex)]
		if len(rename) > 256 {
			return fmt.Errorf("anvil rename can not be longer than 256 bytes")
		}
		if !utf8.ValidString(rename) {
			return fmt.Errorf("anvil rename must be valid utf8")
		}
	}

	input, _ := h.itemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerAnvilInput},
		Slot:      anvilInputSlot,
	}, s, tx)
	if input.Empty() {
		return fmt.Errorf("no item in input input slot")
	}
	material, _ := h.itemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerAnvilMaterial},
		Slot:      anvilMaterialSlot,
	}, s, tx)
	result := input

	// material is never reassigned below, so the three later Empty checks all
	// report what this one does. Empty walks into the item's EncodeItem, so
	// reading it once here is the same answer for less work.
	materialEmpty := material.Empty()

	// The sum of the input's anvil cost as well as the material's anvil cost.
	anvilCost := input.AnvilCost()
	if !materialEmpty {
		anvilCost += material.AnvilCost()
	}

	// The material input may be empty (if the player is only renaming, for example).
	var actionCost, renameCost, repairCount int
	if !materialEmpty {
		// input is not reassigned in this block, so every read of Item below
		// returned this same value. Item calls Empty, which calls EncodeItem,
		// so this is read once instead of up to three times.
		inputItem := input.Item()

		// First check if we are trying to repair the item with a material.
		if repairable, ok := inputItem.(item.Repairable); ok && repairable.RepairableBy(material) {
			result, actionCost, repairCount, err = repairItemWithMaterial(input, material, result)
			if err != nil {
				return err
			}
		} else {
			// Same for material: both reads of its Item in this branch saw
			// this value.
			materialItem := material.Item()
			_, book := materialItem.(item.EnchantedBook)
			_, durable := inputItem.(item.Durable)

			// Ensure that the input item is repairable, or the material item is an enchanted book. If not, this is an
			// invalid scenario, and we should return an error.
			enchantedBook := book && len(material.Enchantments()) > 0
			if !enchantedBook && (inputItem != materialItem || !durable) {
				return fmt.Errorf("input item is not repairable/same type or material item is not an enchanted book")
			}

			// If the material is another durable item, we just need to increase the durability of the result by the
			// material's durability at 12%.
			if durable && !enchantedBook {
				result, actionCost = repairItemWithDurable(input, material, result)
			}

			// Merge enchantments on the material item onto the result item.
			var hasCompatible, hasIncompatible bool
			result, hasCompatible, hasIncompatible, actionCost = mergeEnchantments(input, material, result, actionCost, enchantedBook)

			// If we don't have any compatible enchantments and the input item isn't durable, then this is an invalid
			// scenario, and we should return an error.
			if !durable && hasIncompatible && !hasCompatible {
				return fmt.Errorf("no compatible enchantments but have incompatible ones")
			}
		}
	}

	// If we have a filter string, then the client is intending to rename the item.
	if len(filterStrings) > 0 {
		renameCost = 1
		actionCost += renameCost
		result = result.WithCustomName(filterStrings[int(a.FilterStringIndex)])
	}

	// Calculate the total cost. (action cost + anvil cost)
	cost := actionCost + anvilCost
	if cost <= 0 {
		return fmt.Errorf("no action was taken")
	}

	// If our only action was renaming, the cost should never exceed 40.
	if renameCost == actionCost && renameCost > 0 && cost >= 40 {
		cost = 39
	}

	// We can bypass the "impossible cost" limit if we're in creative mode.
	c := co.GameMode().CreativeInventory()
	if cost >= 40 && !c {
		return fmt.Errorf("impossible cost")
	}

	// Ensure we have enough levels (or if we're in creative mode, ignore the cost) to perform the action.
	level := co.ExperienceLevel()
	if level < cost && !c {
		return fmt.Errorf("not enough experience")
	} else if !c {
		// Which input was the problem: an anvil use followed by a failing
		// action in the same request. The levels, the sound and the damaged
		// anvil stayed while the inputs were put back, and an anvil broken by
		// the use made the revert write into a nil inventory. They happen only
		// once the use has succeeded.
		h.effect(func() { co.SetExperienceLevel(level - cost) })
	}

	// If we had a result item, we need to calculate the new anvil cost and update it on the item.
	if !result.Empty() {
		updatedAnvilCost := result.AnvilCost()
		if !materialEmpty && updatedAnvilCost < material.AnvilCost() {
			updatedAnvilCost = material.AnvilCost()
		}
		if renameCost != actionCost || renameCost == 0 {
			updatedAnvilCost = updatedAnvilCost*2 + 1
		}
		result = result.WithAnvilCost(updatedAnvilCost)
	}

	// If we're not in creative mode, we have a 12% chance of the anvil degrading down one state. If that is the case, we
	// need to play the related sound and update the block state. Otherwise, we play a regular anvil use sound.
	if !c && rand.Float64() < 0.12 {
		damaged := anvil.Break()
		h.effect(func() {
			if _, ok := damaged.(block.Air); ok {
				tx.PlaySound(pos.Vec3Centre(), sound.AnvilBreak{})
			} else {
				tx.PlaySound(pos.Vec3Centre(), sound.AnvilUse{})
			}
			tx.SetBlock(pos, damaged, nil)
		})
	} else {
		h.effect(func() { tx.PlaySound(pos.Vec3Centre(), sound.AnvilUse{}) })
	}

	h.setItemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerAnvilInput},
		Slot:      anvilInputSlot,
	}, item.Stack{}, s, tx)
	if repairCount > 0 {
		h.setItemInSlot(protocol.StackRequestSlotInfo{
			Container: protocol.FullContainerName{ContainerID: protocol.ContainerAnvilMaterial},
			Slot:      anvilMaterialSlot,
		}, material.Grow(-repairCount), s, tx)
	} else {
		h.setItemInSlot(protocol.StackRequestSlotInfo{
			Container: protocol.FullContainerName{ContainerID: protocol.ContainerAnvilMaterial},
			Slot:      anvilMaterialSlot,
		}, item.Stack{}, s, tx)
	}
	return h.createResults(s, tx, result)
}

// repairItemWithMaterial is a helper function that repairs an item stack with a given material stack. It returns the new item
// stack, the cost, and the repaired items count.
func repairItemWithMaterial(input item.Stack, material item.Stack, result item.Stack) (item.Stack, int, int, error) {
	// MaxDurability type asserts the item and builds its DurabilityInfo, and
	// input never changes, so both reads of it gave this number.
	inputMax := input.MaxDurability()

	// Calculate the durability delta using the maximum durability and the current durability.
	delta := min(inputMax-input.Durability(), inputMax/4)
	if delta <= 0 {
		return item.Stack{}, 0, 0, fmt.Errorf("input item is already fully repaired")
	}

	// While the durability delta is more than zero and the repaired count is under the material count, increase
	// the durability of the result by the durability delta.
	// The post statement is written out at the end of the body instead: the
	// body holds no continue, so the order of body then post is unchanged, and
	// the delta expression reads the same updated result it read before while
	// asking it for its maximum durability once rather than twice.
	var cost, count int
	for delta > 0 && count < material.Count() {
		result = result.WithDurability(result.Durability() + delta)
		cost++

		count++
		resultMax := result.MaxDurability()
		delta = min(resultMax-result.Durability(), resultMax/4)
	}
	return result, cost, count, nil
}

// repairItemWithDurable is a helper function that repairs an item with another durable item stack.
func repairItemWithDurable(input item.Stack, durable item.Stack, result item.Stack) (item.Stack, int) {
	// input is never reassigned here, so the three MaxDurability reads and the
	// two Durability reads all gave these numbers. Both walk the item's
	// DurabilityInfo, so each is asked once.
	inputMax, inputDurability := input.MaxDurability(), input.Durability()

	durability := inputDurability + durable.Durability() + inputMax*12/100
	if durability > inputMax {
		durability = inputMax
	}

	// Ensure the durability is higher than the input's current durability.
	var cost int
	if durability > inputDurability {
		result = result.WithDurability(durability)
		cost += 2
	}
	return result, cost
}

// mergeEnchantments merges the enchantments of the material item stack onto the result item stack and returns the result
// item stack, booleans indicating whether the enchantments had any compatible or incompatible enchantments, and the cost.
func mergeEnchantments(input item.Stack, material item.Stack, result item.Stack, cost int, enchantedBook bool) (item.Stack, bool, bool, int) {
	// With nothing to merge the loop below never ran and left every result
	// untouched, so the same values are returned here without reading the
	// input at all.
	materialEnchants := material.Enchantments()
	if len(materialEnchants) == 0 {
		return result, false, false, cost
	}

	// input is never reassigned in the loop, so all three of these were the
	// same on every pass. Item walks EncodeItem, and Enchantments builds and
	// sorts a fresh slice on every call, so both are done once for the whole
	// loop instead of once per material enchantment.
	inputItem := input.Item()
	_, inputBook := inputItem.(item.EnchantedBook)
	inputEnchants := input.Enchantments()

	var hasCompatible, hasIncompatible bool
	for _, enchant := range materialEnchants {
		// First ensure that the enchantment type is compatible with the input item.
		enchantType := enchant.Type()
		compatible := enchantType.CompatibleWithItem(inputItem)
		if inputBook {
			compatible = true
		}

		// Then ensure that each input enchantment is compatible with this material enchantment. If one is not compatible,
		// increase the cost by one.
		for _, otherEnchant := range inputEnchants {
			if otherType := otherEnchant.Type(); enchantType != otherType && !enchantType.CompatibleWithEnchantment(otherType) {
				compatible = false
				cost++
			}
		}

		// Skip the enchantment if it isn't compatible with enchantments on the input item.
		if !compatible {
			hasIncompatible = true
			continue
		}
		hasCompatible = true

		resultLevel := enchant.Level()
		levelCost := resultLevel

		// Check if we have an enchantment of the same type on the input item.
		if existingEnchant, ok := input.Enchantment(enchantType); ok {
			if existingEnchant.Level() > resultLevel || (existingEnchant.Level() == resultLevel && resultLevel == enchantType.MaxLevel()) {
				// The result level is either lower than the existing enchantment's level or is higher than the maximum
				// level, so skip this enchantment.
				hasIncompatible = true
				continue
			} else if existingEnchant.Level() == resultLevel {
				// If the input level is equal to the material level, increase the result level by one.
				resultLevel++
			}
			// Update the level cost. (result level - existing level)
			levelCost = resultLevel - existingEnchant.Level()
		}

		// Now calculate the rarity cost. This is just the application cost of the rarity, however if the
		// material is an enchanted book, then the rarity cost gets halved. If the new rarity cost is under one,
		// it is set to one.
		rarityCost := enchantType.Rarity().Cost()
		if enchantedBook {
			rarityCost = max(1, rarityCost/2)
		}

		// Update the result item with the new enchantment.
		result = result.WithEnchantments(item.NewEnchantment(enchantType, resultLevel))

		// Update the cost appropriately.
		cost += rarityCost * levelCost
		if input.Count() > 1 {
			cost = 40
		}
	}
	return result, hasCompatible, hasIncompatible, cost
}
