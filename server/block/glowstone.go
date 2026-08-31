package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
)

// Glowstone is commonly found on the ceiling of the nether dimension.
type Glowstone struct {
	solid
}

func (g Glowstone) Instrument() sound.Instrument {
	return sound.Pling()
}

// CanRedstoneWireStepDown keeps dust from stepping down over glowstone despite its solid top face.
func (Glowstone) CanRedstoneWireStepDown(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

func (Glowstone) RedstoneNonConductive() {}

func (g Glowstone) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, discreteDrops(item.GlowstoneDust{}, g, 2, 4, 4))
}

func (Glowstone) EncodeItem() (name string, meta int16) {
	return "minecraft:glowstone", 0
}

func (Glowstone) EncodeBlock() (string, map[string]any) {
	return "minecraft:glowstone", nil
}

func (Glowstone) LightEmissionLevel() uint8 {
	return 15
}
