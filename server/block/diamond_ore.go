package block

import (
	"github.com/jv2w/Impulse/server/item"
)

// DiamondOre is a rare ore that generates underground.
type DiamondOre struct {
	solid
	bassDrum

	// Type is the type of diamond ore.
	Type OreType
}

// BreakInfo ...
func (d DiamondOre) BreakInfo() BreakInfo {
	return newBreakInfo(d.Type.Hardness(), func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierIron.HarvestLevel
	}, pickaxeEffective, oreDrops(item.Diamond{}, d)).withXPDropRange(3, 7).withBlastResistance(3)
}

// SmeltInfo ...
func (DiamondOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(item.Diamond{}, 1), 1)
}

// diamondOreName gives the encoded name of diamond ore of a type.
var diamondOreName = enumNamer(OreTypes(), func(t OreType) string { return t.Prefix() + "diamond_ore" })

// EncodeItem ...
func (d DiamondOre) EncodeItem() (name string, meta int16) {
	return diamondOreName(d.Type), 0
}

// EncodeBlock ...
func (d DiamondOre) EncodeBlock() (string, map[string]any) {
	return diamondOreName(d.Type), nil
}
