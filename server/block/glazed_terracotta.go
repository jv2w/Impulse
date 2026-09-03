package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// GlazedTerracotta is a vibrant solid block that comes in the 16 regular dye colours.
type GlazedTerracotta struct {
	solid
	bassDrum

	// Colour specifies the colour of the block.
	Colour item.Colour
	// Facing specifies the face of the block.
	Facing cube.Direction
}

// BreakInfo ...
func (t GlazedTerracotta) BreakInfo() BreakInfo {
	return newBreakInfo(1.4, pickaxeHarvestable, pickaxeEffective, oneOf(t))
}

// glazedTerracottaName gives the encoded name of this block in a colour.
var glazedTerracottaName = colourNamer(item.Colour.SilverString, "_glazed_terracotta")

// EncodeItem ...
func (t GlazedTerracotta) EncodeItem() (name string, meta int16) {
	return glazedTerracottaName(t.Colour), 0
}

// EncodeBlock ...
func (t GlazedTerracotta) EncodeBlock() (name string, properties map[string]any) {
	if t.Facing == unknownDirection {
		return glazedTerracottaName(t.Colour), map[string]any{"facing_direction": int32(0)}
	}
	return glazedTerracottaName(t.Colour), map[string]any{"facing_direction": int32(2 + t.Facing)}
}

// UseOnBlock ensures the proper facing is used when placing a glazed terracotta block, by using the opposite of the player.
func (t GlazedTerracotta) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, t)
	if !used {
		return
	}
	t.Facing = user.Rotation().Direction().Opposite()

	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// allGlazedTerracotta returns glazed terracotta blocks with all possible colours.
func allGlazedTerracotta() (b []world.Block) {
	// coloursByID holds the same sixteen colours in the same order, so ranging
	// over it builds the same list without item.Colours allocating a fresh slice
	// on every direction.
	dirs := append(cube.Directions(), unknownDirection)
	b = make([]world.Block, 0, len(dirs)*len(coloursByID))
	for _, dir := range dirs {
		for _, c := range coloursByID {
			b = append(b, GlazedTerracotta{Colour: c, Facing: dir})
		}
	}
	return b
}
