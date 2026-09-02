package block

import (
	"github.com/jv2w/Impulse/server/item"
)

// LapisOre is an ore block from which lapis lazuli is obtained.
type LapisOre struct {
	solid
	bassDrum

	// Type is the type of lapis ore.
	Type OreType
}

// BreakInfo ...
func (l LapisOre) BreakInfo() BreakInfo {
	return newBreakInfo(l.Type.Hardness(), func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierStone.HarvestLevel
	}, pickaxeEffective, multiOreDrops(item.LapisLazuli{}, l, 4, 9)).withXPDropRange(2, 5).withBlastResistance(3)
}

// SmeltInfo ...
func (LapisOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(item.LapisLazuli{}, 1), 0.2)
}

// lapisOreName gives the encoded name of lapis ore of a type.
var lapisOreName = enumNamer(OreTypes(), func(t OreType) string { return t.Prefix() + "lapis_ore" })

// EncodeItem ...
func (l LapisOre) EncodeItem() (name string, meta int16) {
	return lapisOreName(l.Type), 0
}

// EncodeBlock ...
func (l LapisOre) EncodeBlock() (string, map[string]any) {
	return lapisOreName(l.Type), nil
}
