package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"math/rand/v2"
	"time"
)

// Coral is a non-solid block that comes in 5 variants.
type Coral struct {
	empty
	transparent
	bassDrum
	sourceWaterDisplacer

	// Type is the type of coral of the block.
	Type CoralType
	// Dead is whether the coral is dead.
	Dead bool
}

// UseOnBlock ...
func (c Coral) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	// The position below is computed once; pos does not change in between, so the
	// same block and the same face are tested.
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	if liquid, ok := tx.Liquid(pos); ok {
		if water, ok := liquid.(Water); ok {
			if water.Depth != 8 {
				return false
			}
		}
	}

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (c Coral) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (c Coral) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// NeighbourUpdateTick ...
func (c Coral) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	// The position below is computed once; pos does not change in between, so the
	// same block and the same face are tested.
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		breakBlock(c, pos, tx)
		return
	} else if c.Dead {
		return
	}
	tx.ScheduleBlockUpdate(pos, c, time.Second*5/2)
}

// ScheduledTick ...
func (c Coral) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	adjacentWater := false
	pos.Neighbours(func(neighbour cube.Pos) {
		if liquid, ok := tx.Liquid(neighbour); ok {
			if _, ok := liquid.(Water); ok {
				adjacentWater = true
			}
		}
	}, tx.Range())
	if !adjacentWater {
		c.Dead = true
		tx.SetBlock(pos, c, nil)
	}
}

// BreakInfo ...
func (c Coral) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(c))
}

// coralName and deadCoralName give the encoded name of live and dead coral of a
// type. The joins that built the name on every encode run once at start instead.
// enumNamer prepends "minecraft:" on a hit and on a miss alike, so a type
// outside the table joins the same strings and panics the same way.
var (
	coralName     = enumNamer(CoralTypes(), func(t CoralType) string { return t.String() + "_coral" })
	deadCoralName = enumNamer(CoralTypes(), func(t CoralType) string { return "dead_" + t.String() + "_coral" })
)

// EncodeBlock ...
func (c Coral) EncodeBlock() (name string, properties map[string]any) {
	if c.Dead {
		return deadCoralName(c.Type), nil
	}
	return coralName(c.Type), nil
}

// EncodeItem ...
func (c Coral) EncodeItem() (name string, meta int16) {
	if c.Dead {
		return deadCoralName(c.Type), 0
	}
	return coralName(c.Type), 0
}

// allCoral returns a list of all coral block variants
func allCoral() (c []world.Block) {
	// The coral type list is built once instead of once per call of f, and the
	// final count is known so the list is sized once. Same blocks in the same
	// order.
	types := CoralTypes()
	c = make([]world.Block, 0, len(types)*2)
	f := func(dead bool) {
		for _, t := range types {
			c = append(c, Coral{Type: t, Dead: dead})
		}
	}
	f(true)
	f(false)
	return
}
