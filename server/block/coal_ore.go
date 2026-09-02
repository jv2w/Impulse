package block

import "github.com/jv2w/Impulse/server/item"

// CoalOre is a common ore.
type CoalOre struct {
	solid
	bassDrum

	// Type is the type of coal ore.
	Type OreType
}

// BreakInfo ...
func (c CoalOre) BreakInfo() BreakInfo {
	return newBreakInfo(c.Type.Hardness(), pickaxeHarvestable, pickaxeEffective, oreDrops(item.Coal{}, c)).withXPDropRange(0, 2).withBlastResistance(3)
}

// SmeltInfo ...
func (CoalOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(item.Coal{}, 1), 0.1)
}

// coalOreName gives the encoded name of coal ore of a type.
var coalOreName = enumNamer(OreTypes(), func(t OreType) string { return t.Prefix() + "coal_ore" })

// EncodeItem ...
func (c CoalOre) EncodeItem() (name string, meta int16) {
	return coalOreName(c.Type), 0
}

// EncodeBlock ...
func (c CoalOre) EncodeBlock() (string, map[string]any) {
	return coalOreName(c.Type), nil

}
