package model

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Stair is a model for stair-like blocks. These have different solid sides depending on the direction the
// stairs are facing, the surrounding blocks and whether it is upside down or not.
type Stair struct {
	// Facing specifies the direction that the full side of the Stair faces.
	Facing cube.Direction
	// UpsideDown turns the Stair upside-down, meaning the full side of the Stair is turned to the top side of the
	// block.
	UpsideDown bool
}

// BBox returns a slice of physics.BBox depending on if the Stair is upside down and which direction it is facing.
// Additionally, these BBoxs depend on the Stair blocks surrounding this one, which can influence the model.
func (s Stair) BBox(pos cube.Pos, bs world.BlockSource) []cube.BBox {
	t := s.cornerType(pos, bs)
	// Only an inner corner appends the extension box, so the corner type gives the
	// exact final length. cornerType only reads neighbouring blocks, so computing it
	// before the slice exists changes nothing it sees or returns.
	n := 2
	if t == cornerRightInner || t == cornerLeftInner {
		n = 3
	}
	// The slab half, plus the step and, on an inner corner, its extension.
	b := make([]cube.BBox, 1, n)
	b[0] = cube.Box(0, 0, 0, 1, 0.5, 1)
	if s.UpsideDown {
		b[0] = cube.Box(0, 0.5, 0, 1, 1, 1)
	}

	face, oppositeFace := s.Facing.Face(), s.Facing.Opposite().Face()
	switch t {
	case noCorner, cornerRightInner, cornerLeftInner:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(face, 0.5).
			Stretch(s.Facing.RotateRight().Face().Axis(), 0.5))
	}
	switch t {
	case cornerRightOuter:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(face, 0.5).
			ExtendTowards(s.Facing.RotateLeft().Face(), 0.5))
	case cornerLeftOuter:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(face, 0.5).
			ExtendTowards(s.Facing.RotateRight().Face(), 0.5))
	case cornerRightInner:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(oppositeFace, 0.5).
			ExtendTowards(s.Facing.RotateRight().Face(), 0.5))
	case cornerLeftInner:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(oppositeFace, 0.5).
			ExtendTowards(s.Facing.RotateLeft().Face(), 0.5))
	}
	if s.UpsideDown {
		// The offset is the same for every box, and indexing straight from 1 drops the
		// sub-slice header and the per-iteration add; the same boxes are translated by
		// the same vector in the same order.
		down := mgl64.Vec3{0, -0.5}
		for i := 1; i < len(b); i++ {
			b[i] = b[i].Translate(down)
		}
	}
	return b
}

// FaceSolid returns true for all faces of the Stair that are completely filled.
func (s Stair) FaceSolid(pos cube.Pos, face cube.Face, bs world.BlockSource) bool {
	// Stairs always have a closed side at the top or bottom based on their orientation.
	if (face == cube.FaceUp && s.UpsideDown) || (face == cube.FaceDown && !s.UpsideDown) {
		return true
	}

	switch t := s.cornerType(pos, bs); t {
	case cornerRightOuter, cornerLeftOuter:
		// Small corner blocks, they do not block water flowing out horizontally.
		return false
	case noCorner:
		// Not a corner, so only block directly behind the stairs.
		return s.Facing.Face() == face
	case cornerRightInner:
		return face == s.Facing.RotateRight().Face() || face == s.Facing.Face()
	default:
		return face == s.Facing.RotateLeft().Face() || face == s.Facing.Face()
	}
}

const (
	noCorner = iota
	cornerRightInner
	cornerLeftInner
	cornerRightOuter
	cornerLeftOuter
)

// cornerType returns the type of the corner that the stairs form, or 0 if it does not form a corner with any
// other stairs.
func (s Stair) cornerType(pos cube.Pos, bs world.BlockSource) uint8 {
	rotatedFacing := s.Facing.RotateRight()
	if closedSide, ok := bs.Block(pos.Side(s.Facing.Face())).Model().(Stair); ok && closedSide.UpsideDown == s.UpsideDown {
		if closedSide.Facing == rotatedFacing {
			return cornerLeftOuter
		} else if closedSide.Facing == rotatedFacing.Opposite() {
			// This will only form a corner if there is not a stair on the right of this one with the same
			// direction.
			if side, ok := bs.Block(pos.Side(rotatedFacing.Face())).Model().(Stair); !ok || side.Facing != s.Facing || side.UpsideDown != s.UpsideDown {
				return cornerRightOuter
			}
			return noCorner
		}
	}
	if openSide, ok := bs.Block(pos.Side(s.Facing.Opposite().Face())).Model().(Stair); ok && openSide.UpsideDown == s.UpsideDown {
		if openSide.Facing == rotatedFacing {
			// This will only form a corner if there is not a stair on the right of this one with the same
			// direction.
			if side, ok := bs.Block(pos.Side(rotatedFacing.Face())).Model().(Stair); !ok || side.Facing != s.Facing || side.UpsideDown != s.UpsideDown {
				return cornerRightInner
			}
		} else if openSide.Facing == rotatedFacing.Opposite() {
			return cornerLeftInner
		}
	}
	return noCorner
}
