package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"strconv"
)

// Light is an invisible block that can produce any light level.
type Light struct {
	empty
	replaceable
	transparent
	flowingWaterDisplacer

	// Level is the light level that the light block produces. It is a number from 0-15, where 15 is the
	// brightest and 0 is no light at all.
	Level int
}

// SideClosed ...
func (Light) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// lightName gives the encoded name of a light block of a level.
var lightName = enumNamer([]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, func(l int) string {
	return "light_block_" + strconv.Itoa(l)
})

// EncodeItem ...
func (l Light) EncodeItem() (name string, meta int16) {
	// enumNamer prepends "minecraft:" to the same suffix the concatenation
	// built, and falls back to building it for a level outside 0-15, so the
	// name is unchanged while the sixteen real levels only cost a lookup.
	return lightName(l.Level), 0
}

// LightEmissionLevel ...
func (l Light) LightEmissionLevel() uint8 {
	return uint8(l.Level)
}

// EncodeBlock ...
func (l Light) EncodeBlock() (name string, properties map[string]any) {
	return lightName(l.Level), nil
}

// allLight returns all possible light blocks.
func allLight() []world.Block {
	m := make([]world.Block, 0, 16)
	for i := 0; i < 16; i++ {
		m = append(m, Light{Level: i})
	}
	return m
}
