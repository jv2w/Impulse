package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Carpet is a colourful block that can be obtained by killing/shearing sheep, or crafted using four string.
type Carpet struct {
	carpet
	transparent
	sourceWaterDisplacer

	// Colour is the colour of the carpet.
	Colour item.Colour
}

// FlammabilityInfo ...
func (c Carpet) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 20, true)
}

// SideClosed ...
func (Carpet) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (c Carpet) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, nothingEffective, oneOf(c))
}

// carpetName gives the encoded name of this block in a colour.
var carpetName = colourNamer(item.Colour.String, "_carpet")

// EncodeItem ...
func (c Carpet) EncodeItem() (name string, meta int16) {
	return carpetName(c.Colour), 0
}

// EncodeBlock ...
func (c Carpet) EncodeBlock() (name string, properties map[string]any) {
	return carpetName(c.Colour), nil
}

// HasLiquidDrops ...
func (Carpet) HasLiquidDrops() bool {
	return true
}

// NeighbourUpdateTick ...
func (c Carpet) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Air); ok {
		breakBlock(c, pos, tx)
	}
}

// UseOnBlock handles not placing carpets on top of air blocks.
func (c Carpet) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, c)
	if !used {
		return
	}

	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Air); ok {
		return
	}

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// allCarpet ...
func allCarpet() (carpets []world.Block) {
	for _, c := range item.Colours() {
		carpets = append(carpets, Carpet{Colour: c})
	}
	return
}
