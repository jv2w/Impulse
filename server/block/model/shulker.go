package model

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Shulker is the model of a shulker box. The bounding box grows along the
// facing axis as the lid opens.
type Shulker struct {
	// Facing is the direction that the lid opens towards.
	Facing cube.Face
	// Progress is the lid animation progress, ranging from 0 (closed) to 10 (fully open).
	Progress int32
}

// BBox returns a single bounding box that extends outward along Facing as the
// lid opens.
func (s Shulker) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	peak := ShulkerPhysicalPeak(s.Progress)
	return []cube.BBox{full.ExtendTowards(s.Facing, peak)}
}

// ShulkerPhysicalPeak returns the lid extension along the facing axis for a
// given Progress in [0, 10]. The curve eases out cubically so the lid moves
// quickly and settles.
func ShulkerPhysicalPeak(progress int32) float64 {
	t := float64(progress) / 10.0
	// (1.0-t) was computed three times; binding it once multiplies the exact same
	// value in the same left to right order, so the result is bit-identical.
	u := 1.0 - t
	return (1.0 - u*u*u) * 0.5
}

// FaceSolid always returns false.
func (Shulker) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
