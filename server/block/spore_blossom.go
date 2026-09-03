package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// SporeBlossom is a decorative block.
type SporeBlossom struct {
	empty
	transparent
}

// HasLiquidDrops ...
func (s SporeBlossom) HasLiquidDrops() bool {
	return true
}

// NeighbourUpdateTick ...
func (s SporeBlossom) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	// pos.Side(cube.FaceUp) is pure and was computed twice; one value serves both uses.
	above := pos.Side(cube.FaceUp)
	if !tx.Block(above).Model().FaceSolid(above, cube.FaceDown, tx) {
		breakBlock(s, pos, tx)
	}
}

// UseOnBlock ...
func (s SporeBlossom) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, s)
	if !used {
		return
	}
	// pos.Side(cube.FaceUp) is pure and was computed twice; one value serves both uses.
	above := pos.Side(cube.FaceUp)
	if !tx.Block(above).Model().FaceSolid(above, cube.FaceDown, tx) {
		return
	}

	place(tx, pos, s, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (s SporeBlossom) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(s))
}

// FlammabilityInfo ...
func (SporeBlossom) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(15, 100, true)
}

// CompostChance ...
func (SporeBlossom) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (s SporeBlossom) EncodeItem() (name string, meta int16) {
	return "minecraft:spore_blossom", 0
}

// EncodeBlock ...
func (s SporeBlossom) EncodeBlock() (string, map[string]any) {
	return "minecraft:spore_blossom", nil
}
