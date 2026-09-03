package nbtconv

import (
	"errors"
	"fmt"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
)

// InvFromNBT decodes the data of an NBT slice into the inventory passed. Entries are independent: every entry that
// decodes is written, and the error returned joins the failures of the rest. Rejecting the whole slice on one bad
// entry would erase the items around it on the next save, which is a larger loss than the entry itself.
//
// An entry fails when it is not a compound, when item.ReadNBT rejects it, when its "Slot" is not the byte InvToNBT
// writes, or when that slot lies outside inv. An entry that decodes to an empty stack is a free slot and is skipped.
func InvFromNBT(inv *inventory.Inventory, items []any) error {
	var errs []error
	for i, itemData := range items {
		data, ok := itemData.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf("item %d: %T is not a compound", i, itemData))
			continue
		}
		it, err := item.ReadNBT(data, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("item %d: %w", i, err))
			continue
		}
		if it.Empty() {
			continue
		}
		// A "Slot" of another type read as a uint8 is 0, which silently overwrites whatever the first slot holds.
		raw := data["Slot"]
		slot, ok := raw.(uint8)
		if !ok {
			// The error path read the same entry a second time; the map is not
			// written between the two reads, so the type reported is the same.
			errs = append(errs, fmt.Errorf("item %d: slot is %T, not uint8", i, raw))
			continue
		}
		if err := inv.SetItem(int(slot), it); err != nil {
			errs = append(errs, fmt.Errorf("item %d: slot %d: %w", i, slot, err))
		}
	}
	return errors.Join(errs...)
}

// InvToNBT encodes an inventory to a data slice which may be encoded as NBT.
func InvToNBT(inv *inventory.Inventory) []any {
	var items []any
	for index, i := range inv.Slots() {
		if i.Empty() {
			continue
		}
		data := item.WriteNBT(i, true)
		data["Slot"] = byte(index)
		items = append(items, data)
	}
	return items
}
