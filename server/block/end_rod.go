package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// EndRod is a decorative light source that emits white particles.
type EndRod struct {
	transparent
	flowingWaterDisplacer

	// Facing is the direction the white end point of the end rod is facing.
	Facing cube.Face
}

// UseOnBlock ...
func (e EndRod) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, e)
	if !used {
		return false
	}

	e.Facing = face
	if other, ok := tx.Block(pos.Side(face.Opposite())).(EndRod); ok {
		if face == other.Facing {
			e.Facing = face.Opposite()
		}
	}
	place(tx, pos, e, user, ctx)
	return placed(ctx)
}

// SideClosed ...
func (EndRod) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// Model ...
func (e EndRod) Model() world.BlockModel {
	return model.EndRod{Axis: e.Facing.Axis()}
}

// LightEmissionLevel ...
func (EndRod) LightEmissionLevel() uint8 {
	return 14
}

// BreakInfo ...
func (e EndRod) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(e))
}

// EncodeItem ...
func (EndRod) EncodeItem() (name string, meta int16) {
	return "minecraft:end_rod", 0
}

// EncodeBlock ...
func (e EndRod) EncodeBlock() (string, map[string]any) {
	if e.Facing.Axis() == cube.Y {
		return "minecraft:end_rod", map[string]any{"facing_direction": int32(e.Facing)}
	}
	return "minecraft:end_rod", map[string]any{"facing_direction": int32(e.Facing.Opposite())}
}

// allEndRods ...
func allEndRods() (b []world.Block) {
	// One rod per face, so the final length is known and the list is allocated once.
	faces := cube.Faces()
	b = make([]world.Block, 0, len(faces))
	for _, f := range faces {
		b = append(b, EndRod{Facing: f})
	}
	return
}
