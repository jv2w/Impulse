package model

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Hopper is a model used by hoppers.
type Hopper struct{}

// BBox returns a physics.BBox that spans a full block.
func (h Hopper) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	// The horizontal face list is a fixed package-level array, so reading it once
	// gives the same faces in the same order as the two reads it replaces.
	horizontal := cube.HorizontalFaces()
	bbox := make([]cube.BBox, 1, 1+len(horizontal))
	bbox[0] = full.ExtendTowards(cube.FaceUp, -0.375)
	for _, f := range horizontal {
		bbox = append(bbox, full.ExtendTowards(f, -0.875))
	}
	return bbox
}

// FaceSolid only returns true for the top face of the hopper.
func (Hopper) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceUp
}
