package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/recipe"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

const (
	// smithingInputSlot is the slot index of the input item in the smithing table.
	smithingInputSlot = 0x33
	// smithingMaterialSlot is the slot index of the material in the smithing table.
	smithingMaterialSlot = 0x34
	// smithingTemplateSlot is the slot index of the template item in the smithing table.
	smithingTemplateSlot = 0x35
)

// handleSmithing handles a CraftRecipe stack request action made using a smithing table.
func (h *ItemStackRequestHandler) handleSmithing(a *protocol.CraftRecipeStackRequestAction, s *Session, tx *world.Tx) error {
	// First, check the recipe and ensure it is valid for the smithing table.
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		return fmt.Errorf("recipe with network id %v does not exist", a.RecipeNetworkID)
	}
	if craft.Block() != "smithing_table" {
		return fmt.Errorf("recipe with network id %v is not a smithing table recipe", a.RecipeNetworkID)
	}
	// The trim case is recorded while the recipe type is already being tested, so the same assertion is not
	// repeated below. The two types are still tried in the same order, so every recipe takes the same branch.
	var trimRecipe bool
	switch craft.(type) {
	case recipe.SmithingTransform:
	case recipe.SmithingTrim:
		trimRecipe = true
	default:
		return fmt.Errorf("recipe with network id %v is not a smithing recipe", a.RecipeNetworkID)
	}

	// Check if the input item and material item match what the recipe requires.
	expectedInputs := craft.Input()
	input, _ := h.itemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerSmithingTableInput},
		Slot:      smithingInputSlot,
	}, s, tx)
	if !matchingStacks(input, expectedInputs[0]) {
		return fmt.Errorf("input item is not the same as expected input")
	}
	material, _ := h.itemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerSmithingTableMaterial},
		Slot:      smithingMaterialSlot,
	}, s, tx)
	if !matchingStacks(material, expectedInputs[1]) {
		return fmt.Errorf("material item is not the same as expected material")
	}
	template, _ := h.itemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerSmithingTableTemplate},
		Slot:      smithingTemplateSlot,
	}, s, tx)
	if !matchingStacks(template, expectedInputs[2]) {
		return fmt.Errorf("template item is not the same as expected template")
	}
	// Which input was the problem: an empty slot passes matchingStacks
	// because Stack.Comparable reports true when either side is empty, so
	// Grow(-1) stayed at 0 while Grow(1).WithItem minted output from air.
	// Rejecting empties here keeps valid crafts identical: non-empty stacks
	// take the same consume-then-produce path below.
	if input.Empty() || material.Empty() || template.Empty() {
		return fmt.Errorf("smithing table slots must not be empty")
	}

	// Create the output using the input stack as reference and the recipe's output item type.
	h.setItemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerSmithingTableInput},
		Slot:      smithingInputSlot,
	}, input.Grow(-1), s, tx)
	h.setItemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerSmithingTableMaterial},
		Slot:      smithingMaterialSlot,
	}, material.Grow(-1), s, tx)
	h.setItemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerSmithingTableTemplate},
		Slot:      smithingTemplateSlot,
	}, template.Grow(-1), s, tx)

	// Only one input item is consumed above, so only one may be produced: input.WithItem keeps the count of the whole
	// input stack.
	result := input.Grow(1 - input.Count())
	if trimRecipe {
		var trim item.ArmourTrim
		if t, ok := template.Item().(item.SmithingTemplate); ok {
			trim.Template = t.Template
		} else {
			return fmt.Errorf("template item is not a smithing template")
		}
		if trim.Material, ok = material.Item().(item.ArmourTrimMaterial); !ok {
			return fmt.Errorf("material item is not an armour trim material")
		}
		trimmable, ok := input.Item().(item.Trimmable)
		if !ok {
			return fmt.Errorf("input item is not trimmable")
		}
		return h.createResults(s, tx, result.WithItem(trimmable.WithTrim(trim)))
	}
	return h.createResults(s, tx, result.WithItem(craft.Output()[0].Item()))
}
