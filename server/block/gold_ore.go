package block

import (
	"github.com/jv2w/Impulse/server/item"
)

// GoldOre is a rare mineral block found underground.
type GoldOre struct {
	solid
	bassDrum

	// Type is the type of gold ore.
	Type OreType
}

// BreakInfo ...
func (g GoldOre) BreakInfo() BreakInfo {
	return newBreakInfo(g.Type.Hardness(), func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierIron.HarvestLevel
	}, pickaxeEffective, oreDrops(item.RawGold{}, g)).withBlastResistance(3)
}

// SmeltInfo ...
func (GoldOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(item.GoldIngot{}, 1), 1)
}

// goldOreName gives the encoded name of gold ore of a type.
var goldOreName = enumNamer(OreTypes(), func(t OreType) string { return t.Prefix() + "gold_ore" })

// EncodeItem ...
func (g GoldOre) EncodeItem() (name string, meta int16) {
	return goldOreName(g.Type), 0
}

// EncodeBlock ...
func (g GoldOre) EncodeBlock() (string, map[string]any) {
	return goldOreName(g.Type), nil
}
