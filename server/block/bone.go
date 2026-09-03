package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
)

// Bone is a decorative block that can face different directions.
type Bone struct {
	solid

	// Axis is the axis which the bone block faces.
	Axis cube.Axis
}

// Instrument ...
func (b Bone) Instrument() sound.Instrument {
	return sound.Xylophone()
}

// UseOnBlock handles the rotational placing of bone blocks.
func (b Bone) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, b)
	if !used {
		return
	}
	b.Axis = face.Axis()

	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (b Bone) BreakInfo() BreakInfo {
	return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(b))
}

// EncodeItem ...
func (b Bone) EncodeItem() (name string, meta int16) {
	return "minecraft:bone_block", 0
}

// EncodeBlock ...
func (b Bone) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:bone_block", map[string]any{"pillar_axis": b.Axis.String(), "deprecated": int32(0)}
}

// allBoneBlock ...
func allBoneBlock() (boneBlocks []world.Block) {
	axes := cube.Axes()
	// The count is known before the loop, so the list is sized once instead of grown.
	boneBlocks = make([]world.Block, 0, len(axes))
	for _, axis := range axes {
		boneBlocks = append(boneBlocks, Bone{Axis: axis})
	}
	return
}
