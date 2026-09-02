package block

import (
	"github.com/jv2w/Impulse/server/item"
)

// IronOre is a mineral block found underground.
type IronOre struct {
	solid
	bassDrum

	// Type is the type of iron ore.
	Type OreType
}

// BreakInfo ...
func (i IronOre) BreakInfo() BreakInfo {
	return newBreakInfo(i.Type.Hardness(), func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierStone.HarvestLevel
	}, pickaxeEffective, oreDrops(item.RawIron{}, i)).withBlastResistance(3)
}

// SmeltInfo ...
func (IronOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(item.IronIngot{}, 1), 0.7)
}

// ironOreName gives the encoded name of iron ore of a type.
var ironOreName = enumNamer(OreTypes(), func(t OreType) string { return t.Prefix() + "iron_ore" })

// EncodeItem ...
func (i IronOre) EncodeItem() (name string, meta int16) {
	return ironOreName(i.Type), 0
}

// EncodeBlock ...
func (i IronOre) EncodeBlock() (string, map[string]any) {
	return ironOreName(i.Type), nil
}
