package block

import (
	"math/rand/v2"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// SugarCane is a plant block that generates naturally near water.
type SugarCane struct {
	empty
	transparent

	// Age is the growth state of sugar cane. Values range from 0 to 15.
	Age int
}

// UseOnBlock ensures the placement of the block is OK.
func (c SugarCane) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	if !c.canGrowHere(pos, tx, true) {
		return false
	}

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (c SugarCane) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !c.canGrowHere(pos, tx, true) {
		breakBlock(c, pos, tx)
	}
}

// RandomTick ...
func (c SugarCane) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !c.canGrowHere(pos, tx, true) {
		breakBlock(c, pos, tx)
		return
	}
	if c.Age < 15 {
		c.Age++
	} else if c.Age == 15 {
		c.Age = 0
		if c.canGrowHere(pos.Side(cube.FaceDown), tx, false) {
			for y := 1; y < 3; y++ {
				// pos.Add(cube.Pos{0, y}) is pure and was computed up to three times per step; one
				// value serves every use, and the same blocks are read in the same order.
				above := pos.Add(cube.Pos{0, y})
				if _, ok := tx.Block(above).(Air); ok {
					tx.SetBlock(above, SugarCane{}, nil)
					break
				} else if _, ok := tx.Block(above).(SugarCane); !ok {
					break
				}
			}
		}
	}
	tx.SetBlock(pos, c, nil)
}

// BoneMeal ...
func (c SugarCane) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	// The walk down is unchanged: the same tx.Block calls happen on the same positions in the same
	// order, but pos.Side(cube.FaceDown) is now computed once per step instead of twice.
	for {
		below := pos.Side(cube.FaceDown)
		if _, ok := tx.Block(below).(SugarCane); !ok {
			break
		}
		pos = below
	}
	if c.canGrowHere(pos.Side(cube.FaceDown), tx, false) {
		for y := 1; y < 3; y++ {
			// pos.Add(cube.Pos{0, y}) is pure and was computed twice; one value serves both uses.
			above := pos.Add(cube.Pos{0, y})
			if _, ok := tx.Block(above).(Air); ok {
				tx.SetBlock(above, SugarCane{}, nil)
			}
		}
		return item.BoneMealResultSmall
	}
	return item.BoneMealResultNone
}

// canGrowHere implements logic to check if sugar cane can live/grow here.
func (c SugarCane) canGrowHere(pos cube.Pos, tx *world.Tx, recursive bool) bool {
	// pos.Side(cube.FaceDown) is pure and was computed twice; one value serves both uses.
	below := pos.Side(cube.FaceDown)
	if _, ok := tx.Block(below).(SugarCane); ok && recursive {
		return c.canGrowHere(below, tx, recursive)
	}

	if supportsVegetation(c, tx.Block(pos.Sub(cube.Pos{0, 1}))) {
		for _, face := range cube.HorizontalFaces() {
			if liquid, ok := tx.Liquid(pos.Side(face).Side(cube.FaceDown)); ok {
				if _, ok := liquid.(Water); ok {
					return true
				}
			}
		}
	}
	return false
}

// HasLiquidDrops ...
func (c SugarCane) HasLiquidDrops() bool {
	return true
}

// BreakInfo ...
func (c SugarCane) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(c))
}

// EncodeItem ...
func (c SugarCane) EncodeItem() (name string, meta int16) {
	return "minecraft:sugar_cane", 0
}

// EncodeBlock ...
func (c SugarCane) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:reeds", map[string]any{"age": int32(c.Age)}
}

// allSugarCane returns all possible states of a sugar cane block.
func allSugarCane() (b []world.Block) {
	// The final count is known, so the slice is presized and grows without reallocating.
	b = make([]world.Block, 0, 16)
	for i := 0; i < 16; i++ {
		b = append(b, SugarCane{Age: i})
	}
	return
}
