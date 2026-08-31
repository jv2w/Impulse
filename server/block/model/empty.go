package model

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Empty is a model that is completely empty. It has no collision boxes or solid faces.
type Empty struct{}

// BBox returns an empty slice.
func (Empty) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return nil
}

// FaceSolid always returns false.
func (Empty) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
