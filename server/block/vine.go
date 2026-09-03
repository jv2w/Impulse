package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"math/rand/v2"
)

// Vines are climbable non-solid vegetation blocks that grow on walls.
type Vines struct {
	replaceable
	transparent
	empty
	sourceWaterDisplacer

	// NorthDirection is true if the vines are attached towards north.
	NorthDirection bool
	// EastDirection is true if the vines are attached towards east.
	EastDirection bool
	// SouthDirection is true if the vines are attached towards south.
	SouthDirection bool
	// WestDirection is true if the vines are attached towards west.
	WestDirection bool
}

// CompostChance ...
func (Vines) CompostChance() float64 {
	return 0.5
}

// SideClosed ...
func (Vines) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// HasLiquidDrops ...
func (Vines) HasLiquidDrops() bool {
	return false
}

// FlammabilityInfo ...
func (Vines) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(15, 100, true)
}

// BreakInfo ...
func (v Vines) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears
	}, axeEffective, oneOf(v))
}

// EntityInside ...
func (Vines) EntityInside(_ cube.Pos, _ *world.Tx, e world.Entity) {
	if fallEntity, ok := e.(fallDistanceEntity); ok {
		fallEntity.ResetFallDistance()
	}
}

// WithAttachment returns a Vines block with an attachment on the given cube.Direction.
func (v Vines) WithAttachment(direction cube.Direction, attached bool) Vines {
	switch direction {
	case cube.North:
		v.NorthDirection = attached
		return v
	case cube.East:
		v.EastDirection = attached
		return v
	case cube.South:
		v.SouthDirection = attached
		return v
	case cube.West:
		v.WestDirection = attached
		return v
	}
	panic("should never happen")
}

// Attachment returns the attachment of the vines at the given direction.
func (v Vines) Attachment(direction cube.Direction) bool {
	switch direction {
	case cube.North:
		return v.NorthDirection
	case cube.East:
		return v.EastDirection
	case cube.South:
		return v.SouthDirection
	case cube.West:
		return v.WestDirection
	}
	panic("should never happen")
}

// attached reports whether the vines are attached in any direction. It is the
// same answer Attachments gives, without building the list: Attachment maps
// each of the four directions onto exactly one of the fields below.
func (v Vines) attached() bool {
	return v.NorthDirection || v.EastDirection || v.SouthDirection || v.WestDirection
}

// Attachments returns all attachments of the vines.
func (v Vines) Attachments() (attachments []cube.Direction) {
	for _, d := range cube.Directions() {
		if v.Attachment(d) {
			attachments = append(attachments, d)
		}
	}
	return
}

// UseOnBlock ...
func (v Vines) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if !v.canSpreadTo(tx, pos) || face.Axis() == cube.Y {
		return false
	}
	pos, face, used := firstReplaceable(tx, pos, face, v)
	if !used {
		return false
	}
	if _, ok := tx.Block(pos).(Vines); ok {
		// Do not overwrite existing vine block.
		return false
	}
	//noinspection GoAssignmentToReceiver
	v = v.WithAttachment(face.Direction().Opposite(), true)

	place(tx, pos, v, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (v Vines) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	above, updated := tx.Block(pos.Side(cube.FaceUp)), false
	// Attachments walks the same directions in the same order and only collects the attached ones,
	// so checking each direction here visits exactly the same ones without building the list. Each
	// step only ever clears its own direction, so a direction still reads its pre-loop value.
	for _, d := range cube.Directions() {
		if !v.Attachment(d) {
			continue
		}
		if !v.canSpreadTo(tx, pos.Side(d.Face())) {
			if o, ok := above.(Vines); !ok || !o.Attachment(d) {
				//noinspection GoAssignmentToReceiver
				v = v.WithAttachment(d, false)
				updated = true
			}
		}
	}
	if !updated {
		return
	}
	// The list was built only to ask whether it was empty.
	if !v.attached() {
		breakBlock(v, pos, tx)
		return
	}
	tx.SetBlock(pos, v, nil)
}

// RandomTick ...
func (v Vines) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if r.Float64() > 0.25 {
		// Vines have a 25% chance of spreading.
		return
	}

	// Choose a random direction to spread.
	face := cube.Face(r.IntN(len(cube.Faces())))
	selectedPos := pos.Side(face)

	// If a horizontal direction was chosen and the vine block is not already
	// attached in that direction, attempt to spread in that direction.
	if face.Axis() != cube.Y && !v.Attachment(face.Direction()) {
		if !v.canSpread(tx, pos) {
			// No further attempt to spread vertically will be made.
			return
		}
		// Attempt to create a new vine block if there is a neighbouring air block
		// in the chosen direction.
		if _, ok := tx.Block(selectedPos).(Air); ok {
			rightRotatedFace := face.RotateRight()
			leftRotatedFace := face.RotateLeft()

			// The rotated faces' directions were derived again further down; they are pure and
			// unchanged, so one value each serves every use.
			rightDirection, leftDirection := rightRotatedFace.Direction(), leftRotatedFace.Direction()
			attachedOnRight := v.Attachment(rightDirection)
			attachedOnLeft := v.Attachment(leftDirection)

			rightSelectedPos := selectedPos.Side(rightRotatedFace)
			leftSelectedPos := selectedPos.Side(leftRotatedFace)

			// Four attempts to create a new vine block will be made, in the
			// following order:
			// 1) If the current vine block is attached in the direction towards
			//    the right ("clockwise") of the chosen direction, and a solid
			//    block can support a vine on that direction in the selected
			//    position, create a new vine block attached on that clockwise
			//    direction at the selected position.
			// 2) If the clockwise direction fails, try again with the left
			//    ("counter-clockwise") direction.
			// 3) If the current vine block is attached in the direction towards
			//    the right of the chosen direction, the current vine block is
			//    also backed by a solid block in that same direction, and the
			//    block neighbouring the selected position in that direction is
			//    air, spread into that air block onto the face opposite of the
			//    chosen direction. The vine jumps from one face of a block onto
			//    another as a result.
			// 4) If the clockwise direction fails, try again with the left
			//    direction.
			if attachedOnRight && v.canSpreadTo(tx, rightSelectedPos) {
				tx.SetBlock(selectedPos, (Vines{}).WithAttachment(rightDirection, true), nil)
			} else if attachedOnLeft && v.canSpreadTo(tx, leftSelectedPos) {
				tx.SetBlock(selectedPos, (Vines{}).WithAttachment(leftDirection, true), nil)
			} else if _, ok = tx.Block(rightSelectedPos).(Air); ok && attachedOnRight && v.canSpreadTo(tx, pos.Side(rightRotatedFace)) {
				tx.SetBlock(rightSelectedPos, (Vines{}).WithAttachment(face.Opposite().Direction(), true), nil)
			} else if _, ok = tx.Block(leftSelectedPos).(Air); ok && attachedOnLeft && v.canSpreadTo(tx, pos.Side(leftRotatedFace)) {
				tx.SetBlock(leftSelectedPos, (Vines{}).WithAttachment(face.Opposite().Direction(), true), nil)
			}
		} else if v.canSpreadTo(tx, selectedPos) {
			// If the neighbouring block is solid, update the vine to be attached in that direction.
			tx.SetBlock(pos, v.WithAttachment(face.Direction(), true), nil)
		}
		return
	}

	// If the chosen direction is Up and the position above is within the height
	// limit, attempt to spread upwards.
	if face == cube.FaceUp && selectedPos.OutOfBounds(tx.Range()) {
		// Vines can only spread upwards into an air block.
		if _, ok := tx.Block(selectedPos).(Air); ok {
			if !v.canSpread(tx, pos) {
				// No further attempt to spread down will be made.
				return
			}
			newVines := Vines{}
			for _, f := range cube.HorizontalFaces() {
				// For each direction the current vine block is attached on,
				// there is a 50% chance for the new above vine block to
				// attach onto the direction, if there is also a solid block
				// in that direction to support the vine.
				if r.IntN(2) == 0 && v.Attachment(f.Direction()) && v.canSpreadTo(tx, selectedPos.Side(f)) {
					newVines = newVines.WithAttachment(f.Direction(), true)
				}
			}
			// The list was built only to ask whether it held anything.
			if newVines.attached() {
				tx.SetBlock(selectedPos, newVines, nil)
			}
			return
		}
	}

	// If an attempt to spread horizontally or upwards has failed but not exited
	// early, attempt to spread downwards.
	selectedPos = pos.Side(cube.FaceDown)
	if selectedPos.OutOfBounds(tx.Range()) {
		return
	}
	newVines, vines := tx.Block(selectedPos).(Vines)
	if _, ok := tx.Block(selectedPos).(Air); !ok && !vines {
		// The block under the current vine block must be air or a vine block.
		return
	}
	var changed bool
	for _, f := range cube.HorizontalFaces() {
		// For each direction the current vine block is attached on, there is a
		// 50% chance for the below vine block to attach onto the direction if
		// it is not already attached in that direction.
		if r.IntN(2) == 0 && v.Attachment(f.Direction()) && !newVines.Attachment(f.Direction()) {
			newVines, changed = newVines.WithAttachment(f.Direction(), true), true
		}
	}
	if changed {
		tx.SetBlock(selectedPos, newVines, nil)
	}
}

// EncodeItem ...
func (Vines) EncodeItem() (name string, meta int16) {
	return "minecraft:vine", 0
}

// EncodeBlock ...
func (v Vines) EncodeBlock() (string, map[string]any) {
	// The same four bits in the same order, set directly instead of walking a temporary list of
	// the very same booleans.
	var bits int
	if v.SouthDirection {
		bits |= 1 << 0
	}
	if v.WestDirection {
		bits |= 1 << 1
	}
	if v.NorthDirection {
		bits |= 1 << 2
	}
	if v.EastDirection {
		bits |= 1 << 3
	}
	return "minecraft:vine", map[string]any{"vine_direction_bits": int32(bits)}
}

// canSpreadTo returns true if the block at the position passed fills a whole cube, as vines need.
func (Vines) canSpreadTo(tx *world.Tx, pos cube.Pos) bool {
	switch tx.Block(pos).Model().(type) {
	case model.Solid, model.Leaves:
		return true
	}
	return false
}

// canSpread returns true if the vines can spread from the given position. Vines
// may only spread horizontally or upwards if there are fewer than 4 vines within
// a 9x9x3 area centred around the Vines.
func (v Vines) canSpread(tx *world.Tx, pos cube.Pos) bool {
	var count int
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			for y := -1; y <= 1; y++ {
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(Vines); ok {
					count++
					// The centre vine is counted, for a max of 4+1=5.
					if count >= 5 {
						return false
					}
				}
			}
		}
	}
	return true
}

// allVines ...
func allVines() (b []world.Block) {
	// Two states on each of four directions, so the slice is presized to its final length and
	// grows without reallocating. The same blocks are appended in the same order.
	b = make([]world.Block, 0, 16)
	for _, north := range []bool{true, false} {
		for _, east := range []bool{true, false} {
			for _, south := range []bool{true, false} {
				for _, west := range []bool{true, false} {
					b = append(b, Vines{
						NorthDirection: north,
						EastDirection:  east,
						SouthDirection: south,
						WestDirection:  west,
					})
				}
			}
		}
	}
	return
}
