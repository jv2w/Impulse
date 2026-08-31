package nbtconv

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
)

// InvFromNBT decodes the data of an NBT slice into the inventory passed.
func InvFromNBT(inv *inventory.Inventory, items []any) {
	for _, itemData := range items {
		data, _ := itemData.(map[string]any)
		// An item this build cannot resolve leaves its slot empty; InvFromNBT
		// has no channel to report the loss on.
		it, _ := item.ReadNBT(data, nil)
		if it.Empty() {
			continue
		}
		_ = inv.SetItem(int(Uint8(data, "Slot")), it)
	}
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
