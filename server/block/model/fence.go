package model

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Fence is a model used by fences of any type. It can attach to blocks with solid faces and other fences of the same
// type and has a model height just slightly over 1.
type Fence struct {
	// Wood specifies if the Fence is made from wood. This field is used to check if two fences are able to attach to
	// each other.
	Wood bool
}

const (
	fenceHeight = 1.5
	fenceInset  = 0.375
)

// BBox returns multiple physics.BBox depending on how many connections it has with the surrounding blocks.
func (f Fence) BBox(pos cube.Pos, s world.BlockSource) []cube.BBox {
	connectWest, connectEast := f.Connects(pos, cube.FaceWest, s), f.Connects(pos, cube.FaceEast, s)
	connectNorth, connectSouth := f.Connects(pos, cube.FaceNorth, s), f.Connects(pos, cube.FaceSouth, s)

	// The boxes below are one per connection, or a single centre post when there is
	// none, so this capacity is exactly the length the appends reach. The slice is
	// still freshly allocated on every call and the connection checks run in the
	// same order as before.
	n := 0
	if connectWest {
		n++
	}
	if connectEast {
		n++
	}
	if connectNorth {
		n++
	}
	if connectSouth {
		n++
	}
	if n == 0 {
		n = 1
	}
	boxes := make([]cube.BBox, 0, n)

	// Check if we have any connections on the Z axis
	if connectWest || connectEast {
		sideBox := cube.Box(0, 0, 0, 1, fenceHeight, 1).Stretch(cube.Z, -fenceInset)
		if connectWest {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceEast, -fenceInset))
		}
		if connectEast {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceWest, -fenceInset))
		}
	}

	// Check if we have any connections on the X axis
	if connectNorth || connectSouth {
		sideBox := cube.Box(0, 0, 0, 1, fenceHeight, 1).Stretch(cube.X, -fenceInset)
		if connectNorth {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceSouth, -fenceInset))
		}
		if connectSouth {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceNorth, -fenceInset))
		}
	}

	// If no connections, create a center post box
	if len(boxes) == 0 {
		boxes = append(boxes, cube.Box(fenceInset, 0, fenceInset, 1-fenceInset, fenceHeight, 1-fenceInset))
	}

	return boxes
}

// FaceSolid returns true if the face is cube.FaceDown or cube.FaceUp.
func (f Fence) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceDown || face == cube.FaceUp
}

// Connects returns true if the Fence connects to the block at the face passed.
// A Fence connects to fences of the same kind, to fence gates and to blocks
// with a solid face towards it.
func (f Fence) Connects(pos cube.Pos, face cube.Face, src world.BlockSource) bool {
	sidePos := pos.Side(face)
	sideModel := src.Block(sidePos).Model()
	if fence, ok := sideModel.(Fence); ok && fence.Wood == f.Wood {
		return true
	}
	if sideModel.FaceSolid(sidePos, face.Opposite(), src) {
		return true
	}
	_, ok := sideModel.(FenceGate)
	return ok
}
