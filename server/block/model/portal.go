package model

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Portal is a model used by portal blocks.
type Portal struct {
	// Axis is the axis that the portal faces.
	Axis cube.Axis
}

// BBox ...
func (Portal) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return nil
}

// FaceSolid ...
func (Portal) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
