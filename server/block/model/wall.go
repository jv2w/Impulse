package model

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Wall is a model used by all wall types.
type Wall struct {
	// NorthConnection is the height of the connection for the north direction.
	NorthConnection float64
	// EastConnection is the height of the connection for the east direction.
	EastConnection float64
	// SouthConnection is the height of the connection for the south direction.
	SouthConnection float64
	// WestConnection is the height of the connection for the west direction.
	WestConnection float64
	// Post is if the wall is the full height of a block or not.
	Post bool
}

// BBox ...
func (w Wall) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	postHeight := 0.8125
	if w.Post {
		postHeight = 1
	}
	// Each connection height is compared once and the result reused, so the slice is
	// sized to exactly the number of boxes appended below. The same boxes come out in
	// the same order, and the slice is still allocated fresh on every call.
	north, east := w.NorthConnection > 0, w.EastConnection > 0
	south, west := w.SouthConnection > 0, w.WestConnection > 0
	n := 1
	if north {
		n++
	}
	if east {
		n++
	}
	if south {
		n++
	}
	if west {
		n++
	}
	// The post, plus at most one box per horizontal connection.
	boxes := make([]cube.BBox, 1, n)
	boxes[0] = cube.Box(0.25, 0, 0.25, 0.75, postHeight, 0.75)
	if north {
		boxes = append(boxes, cube.Box(0.25, 0, 0, 0.75, w.NorthConnection, 0.25))
	}
	if east {
		boxes = append(boxes, cube.Box(0.75, 0, 0.25, 1, w.EastConnection, 0.75))
	}
	if south {
		boxes = append(boxes, cube.Box(0.25, 0, 0.75, 0.75, w.SouthConnection, 1))
	}
	if west {
		boxes = append(boxes, cube.Box(0, 0, 0.25, 0.25, w.WestConnection, 0.75))
	}
	return boxes
}

// FaceSolid returns true if the face is in the Y axis.
func (w Wall) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face.Axis() == cube.Y
}
