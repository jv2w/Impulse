package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Fern is a transparent plant block which can be used to obtain seeds and as decoration.
type Fern struct {
	replaceable
	transparent
	empty
}

// FlammabilityInfo ...
func (g Fern) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (g Fern) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, grassDrops(g))
}

// BoneMeal attempts to affect the block using a bone meal item.
func (g Fern) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	upper := DoubleTallGrass{Type: FernDoubleTallGrass(), UpperPart: true}
	// The position above and the grass type are each wanted twice and neither changes here, so
	// upper.Type is the very value the second call built.
	up := pos.Side(cube.FaceUp)
	if replaceableWith(tx, up, upper) {
		tx.SetBlock(pos, DoubleTallGrass{Type: upper.Type}, nil)
		tx.SetBlock(up, upper, nil)
		return item.BoneMealResultSmall
	}
	return item.BoneMealResultNone
}

// CompostChance ...
func (g Fern) CompostChance() float64 {
	return 0.3
}

// NeighbourUpdateTick ...
func (g Fern) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(g, pos, tx)
	}
}

// HasLiquidDrops ...
func (g Fern) HasLiquidDrops() bool {
	return true
}

// UseOnBlock ...
func (g Fern) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, g)
	if !used || !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, g, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (g Fern) EncodeItem() (name string, meta int16) {
	return "minecraft:fern", 0
}

// EncodeBlock ...
func (g Fern) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:fern", nil
}
