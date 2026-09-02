package block

import (
	"github.com/jv2w/Impulse/server/item"
)

// CopperOre is a rare mineral block found underground.
type CopperOre struct {
	solid
	bassDrum

	// Type is the type of copper ore.
	Type OreType
}

// BreakInfo ...
func (c CopperOre) BreakInfo() BreakInfo {
	return newBreakInfo(c.Type.Hardness(), func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierStone.HarvestLevel
	}, pickaxeEffective, multiOreDrops(item.RawCopper{}, c, 2, 5)).withBlastResistance(3)
}

// SmeltInfo ...
func (CopperOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(item.CopperIngot{}, 1), 0.7)
}

// copperOreName gives the encoded name of copper ore of a type.
var copperOreName = enumNamer(OreTypes(), func(t OreType) string { return t.Prefix() + "copper_ore" })

// EncodeItem ...
func (c CopperOre) EncodeItem() (name string, meta int16) {
	return copperOreName(c.Type), 0
}

// EncodeBlock ...
func (c CopperOre) EncodeBlock() (string, map[string]any) {
	return copperOreName(c.Type), nil
}
