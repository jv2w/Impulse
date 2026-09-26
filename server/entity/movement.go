package entity

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"math"
)

// MovementComputer is used to compute movement of an entity. When constructed, the Gravity of the entity
// the movement is computed for must be passed.
type MovementComputer struct {
	Gravity, Drag     float64
	DragBeforeGravity bool

	onGround bool
}

// Movement represents the movement of a world.Entity as a result of a call to MovementComputer.TickMovement. The
// resulting position and velocity can be obtained by calling Position and Velocity. These can be sent to viewers by
// calling Send.
type Movement struct {
	v                    []world.Viewer
	e                    world.Entity
	pos, vel, dpos, dvel mgl64.Vec3
	rot                  cube.Rotation
	onGround             bool
}

// Send sends the Movement to any viewers watching the entity at the time of the movement. If the position/velocity
// changes were negligible, nothing is sent.
func (m *Movement) Send() {
	posChanged := !m.dpos.ApproxEqualThreshold(zeroVec3, epsilon)
	velChanged := !m.dvel.ApproxEqualThreshold(zeroVec3, epsilon)
	if !posChanged && !velChanged {
		// The loop below called nothing on any viewer in this case, so walking
		// the viewer list at all was pure waste.
		return
	}

	for _, v := range m.v {
		if posChanged {
			v.ViewEntityMovement(m.e, m.pos, m.rot, m.onGround)
		}
		if velChanged {
			v.ViewEntityVelocity(m.e, m.vel)
		}
	}
}

// Position returns the position as a result of the Movement as an mgl64.Vec3.
func (m *Movement) Position() mgl64.Vec3 {
	return m.pos
}

// Velocity returns the velocity after the Movement as an mgl64.Vec3.
func (m *Movement) Velocity() mgl64.Vec3 {
	return m.vel
}

// Rotation returns the rotation, yaw and pitch, of the entity after the Movement.
func (m *Movement) Rotation() cube.Rotation {
	return m.rot
}

// TickMovement performs a movement tick on an entity. Velocity is applied and changed according to the values
// of its Drag and Gravity.
// The new position of the entity after movement is returned.
// The resulting Movement can be sent to viewers by calling Movement.Send.
func (c *MovementComputer) TickMovement(e world.Entity, pos, vel mgl64.Vec3, rot cube.Rotation, tx *world.Tx) *Movement {
	viewers := tx.Viewers(pos)

	velBefore := vel
	vel = c.applyHorizontalForces(tx, pos, c.applyVerticalForces(vel))
	dPos, vel := c.CheckCollision(tx, e, pos, vel)

	return &Movement{v: viewers, e: e,
		pos: pos.Add(dPos), vel: vel, dpos: dPos, dvel: vel.Sub(velBefore),
		rot: rot, onGround: c.onGround,
	}
}

// OnGround checks if the entity that this computer calculates is currently on the ground.
func (c *MovementComputer) OnGround() bool {
	return c.onGround
}

// zeroVec3 is a mgl64.Vec3 with zero values.
var zeroVec3 mgl64.Vec3

// epsilon is the epsilon used for thresholds for change used for change in position and velocity.
const epsilon = 0.001

// applyVerticalForces applies gravity and drag on the Y axis, based on the Gravity and Drag values set.
func (c *MovementComputer) applyVerticalForces(vel mgl64.Vec3) mgl64.Vec3 {
	if c.DragBeforeGravity {
		vel[1] *= 1 - c.Drag
	}
	vel[1] -= c.Gravity
	if !c.DragBeforeGravity {
		vel[1] *= 1 - c.Drag
	}
	return vel
}

// applyHorizontalForces applies friction to the velocity based on the Drag value, reducing it on the X and Z axes.
func (c *MovementComputer) applyHorizontalForces(tx *world.Tx, pos, vel mgl64.Vec3) mgl64.Vec3 {
	friction := 1 - c.Drag
	if c.onGround {
		if f, ok := tx.Block(cube.PosFromVec3(pos).Side(cube.FaceDown)).(interface {
			Friction() float64
		}); ok {
			friction *= f.Friction()
		} else {
			friction *= 0.6
		}
	}
	vel[0] *= friction
	vel[2] *= friction
	return vel
}

// finiteVec3 reports whether every component of v is finite (neither NaN nor
// an infinity). It is pure, allocates nothing and touches no shared state, so
// it is safe on the tick hot path.
func finiteVec3(v mgl64.Vec3) bool {
	return !math.IsNaN(v[0]) && !math.IsInf(v[0], 0) &&
		!math.IsNaN(v[1]) && !math.IsInf(v[1], 0) &&
		!math.IsNaN(v[2]) && !math.IsInf(v[2], 0)
}

// CheckCollision handles the collision of the entity with blocks, adapting the velocity of the entity if it
// happens to collide with a block.
// The final velocity and the Vec3 that the entity should move is returned.
func (c *MovementComputer) CheckCollision(tx *world.Tx, e world.Entity, pos, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	// TODO: Implement collision with other entities.
	// Which input was the problem: a non-finite velocity component (NaN or Inf)
	// from a client packet or a plugin. Extend(vel) below turned it into a box
	// with a non-finite bound, so blockBBoxsAround either walked from
	// math.MinInt64 (hanging the world goroutine) or collapsed Floor/Ceil to the
	// same int (disabling collision on every axis), and the NaN survived into
	// the returned delta and then the entity position.
	// Why the behaviour is identical: finite velocities skip the branch below
	// untouched. Only a component that could never collide meaningfully is
	// replaced by 0, the fail-closed choice (no movement on that axis). vel is
	// an mgl64.Vec3 value, so the rewrite touches only this call's copy.
	if !finiteVec3(vel) {
		for i := range vel {
			if math.IsNaN(vel[i]) || math.IsInf(vel[i], 0) {
				vel[i] = 0
			}
		}
	}
	deltaX, deltaY, deltaZ := vel[0], vel[1], vel[2]

	// Entities only ever have a single bounding box.
	entityBBox := e.H().Type().BBox(e).Translate(pos)
	blocks := blockBBoxsAround(tx, entityBBox.Extend(vel))

	// With no block box around, none of the three loops below could change a
	// delta and entityBBox is dead after them, so the whole axis pass is skipped.
	if len(blocks) != 0 {
		if !mgl64.FloatEqualThreshold(deltaY, 0, epsilon) {
			// First we move the entity BBox on the Y axis.
			for _, blockBBox := range blocks {
				deltaY = entityBBox.YOffset(blockBBox, deltaY)
			}
			entityBBox = entityBBox.Translate(mgl64.Vec3{0, deltaY})
		}
		if !mgl64.FloatEqualThreshold(deltaX, 0, epsilon) {
			// Then on the X axis.
			for _, blockBBox := range blocks {
				deltaX = entityBBox.XOffset(blockBBox, deltaX)
			}
			entityBBox = entityBBox.Translate(mgl64.Vec3{deltaX})
		}
		if !mgl64.FloatEqualThreshold(deltaZ, 0, epsilon) {
			// And finally on the Z axis.
			for _, blockBBox := range blocks {
				deltaZ = entityBBox.ZOffset(blockBBox, deltaZ)
			}
		}
	}
	if !mgl64.FloatEqual(vel[1], 0) {
		// The Y velocity of the entity is currently not 0, meaning it is moving either up or down. We can
		// then assume the entity is not currently on the ground.
		c.onGround = false
	}
	if !mgl64.FloatEqual(deltaX, vel[0]) {
		vel[0] = 0
	}
	if !mgl64.FloatEqual(deltaY, vel[1]) {
		// The entity either hit the ground or hit the ceiling.
		if vel[1] < 0 {
			// The entity was going down, so we can assume it is now on the ground.
			c.onGround = true
		}
		vel[1] = 0
	}
	if !mgl64.FloatEqual(deltaZ, vel[2]) {
		vel[2] = 0
	}
	return mgl64.Vec3{deltaX, deltaY, deltaZ}, vel
}

// blockBBoxsAround returns all blocks around the entity passed, using the BBox passed to make a prediction of
// what blocks need to have their BBox returned.
func blockBBoxsAround(tx *world.Tx, box cube.BBox) []cube.BBox {
	grown := box.Grow(0.25)
	min, max := grown.Min(), grown.Max()
	// Which input was the problem: a box with a non-finite bound, reached here
	// through Extend with a non-finite velocity or a non-finite position from a
	// packet. int(math.Floor(Inf)) converts to math.MinInt64, so the loops below
	// walked billions of blocks and hung the world goroutine.
	// Why the behaviour is identical: finite boxes take the same path below.
	// Only a box that could never match a real block returns nil here, and nil
	// already means "no blocks" on the open-air hot path, with no allocation
	// either way.
	// Concurrency: tx must be used on the world goroutine only; this only reads
	// through it. The returned slice is freshly allocated and owned by the caller.
	if !finiteVec3(min) || !finiteVec3(max) {
		return nil
	}
	minX, minY, minZ := int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))
	// The maximum bounds are exclusive: A block starting exactly at the box's
	// maximum cannot collide with it.
	maxX, maxY, maxZ := int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))

	// A prediction of one BBox per block, plus an additional 2, in case. Allocate
	// it lazily so that entities moving through air do not allocate an empty slice
	// every tick.
	predicted := (maxX-minX)*(maxY-minY)*(maxZ-minZ) + 2
	var blockBBoxs []cube.BBox
	for y := minY; y < maxY; y++ {
		// The int to float64 conversions of the block position were redone for
		// every box of every block; y and x are fixed for their whole subtree and
		// the offset vector is the same for all boxes of one block.
		fy := float64(y)
		for x := minX; x < maxX; x++ {
			fx := float64(x)
			for z := minZ; z < maxZ; z++ {
				pos := cube.Pos{x, y, z}
				boxes := tx.Block(pos).Model().BBox(pos, tx)
				if len(boxes) == 0 {
					continue
				}
				if blockBBoxs == nil {
					blockBBoxs = make([]cube.BBox, 0, predicted)
				}
				offset := mgl64.Vec3{fx, fy, float64(z)}
				for _, box := range boxes {
					blockBBoxs = append(blockBBoxs, box.Translate(offset))
				}
			}
		}
	}
	return blockBBoxs
}
