package entity

import (
	"math"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// The entity used throughout this file is testMovingEntType, whose BBox is
// cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125): 0.25 wide, 0.25 tall, with
// its origin at the centre of the bottom face. Every hand-computed expectation
// below follows from those numbers.

func TestCheckCollisionResolvesYBeforeX(t *testing.T) {
	// An entity descending diagonally past the top corner of a block. Before the
	// Y step it is entirely above the block, so an X step taken first would find
	// nothing in its Y/Z plane and move the full 0.5. Y runs first and drops the
	// box down beside the block, so the X step is then clamped against it.
	//
	// This is the assertion that a spatial or ordering rewrite is most likely to
	// break: the axis order IS the behaviour.
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64.30, 0.5}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 63, 0}, block.Stone{}, nil)
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{0.5, -0.35, 0})

		// 0.375 is the gap between the entity box's max X (0.625) and the block's
		// min X (1). A resolution order of X before Y would give the full 0.5.
		assertVec3(t, "delta", dPos, mgl64.Vec3{0.375, -0.35, 0})
		assertVec3(t, "velocity", vel, mgl64.Vec3{0, -0.35, 0})
		if c.OnGround() {
			t.Fatal("OnGround() = true after a blocked horizontal move, want false")
		}
	})
}

func TestCheckCollisionResolvesXBeforeZ(t *testing.T) {
	// An entity moving diagonally at a block that is one step along both X and Z.
	// Before the X step the box is outside the block's Z range, so a Z step taken
	// first would move the full 0.5 and X would be the axis that gets clamped.
	// X runs first, slides the box under the block's X range, and Z is clamped
	// instead.
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 1}, block.Stone{}, nil)
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{0.5, 0, 0.5})

		// X passes through untouched, Z is cut to the 0.375 gap. Swapping the two
		// axes would give exactly the mirror image, {0.375, 0, 0.5}.
		assertVec3(t, "delta", dPos, mgl64.Vec3{0.5, 0, 0.375})
		assertVec3(t, "velocity", vel, mgl64.Vec3{0.5, 0, 0})
	})
}

func TestCheckCollisionOnGround(t *testing.T) {
	tests := []struct {
		name         string
		blocks       map[cube.Pos]world.Block
		pos, vel     mgl64.Vec3
		startGround  bool
		wantDelta    mgl64.Vec3
		wantVelocity mgl64.Vec3
		wantGround   bool
	}{
		{
			// Landing: the Y delta is cut short, the entity was going down, so it
			// is now on the ground and its Y velocity is dropped.
			name:   "landing on a block sets on ground",
			blocks: map[cube.Pos]world.Block{{0, 63, 0}: block.Stone{}},
			pos:    mgl64.Vec3{0.5, 64.02, 0.5}, vel: mgl64.Vec3{0, -0.05, 0},
			wantDelta: mgl64.Vec3{0, -0.02, 0}, wantVelocity: mgl64.Vec3{}, wantGround: true,
		},
		{
			// Hitting a ceiling also cuts the Y delta short and zeroes the Y
			// velocity, but must not report the entity as standing on anything.
			name:   "hitting a ceiling does not set on ground",
			blocks: map[cube.Pos]world.Block{{0, 64, 0}: block.Stone{}},
			pos:    mgl64.Vec3{0.5, 63.7, 0.5}, vel: mgl64.Vec3{0, 0.1, 0},
			wantDelta: mgl64.Vec3{0, 0.05, 0}, wantVelocity: mgl64.Vec3{}, wantGround: false,
		},
		{
			// Any non-zero Y velocity clears the flag before the axes are
			// resolved, so walking off a ledge takes effect on the same tick.
			name: "any vertical movement in open air clears on ground",
			pos:  mgl64.Vec3{0.5, 64, 0.5}, vel: mgl64.Vec3{0, -0.5, 0}, startGround: true,
			wantDelta: mgl64.Vec3{0, -0.5, 0}, wantVelocity: mgl64.Vec3{0, -0.5, 0}, wantGround: false,
		},
		{
			// A purely horizontal move never touches the flag, so an entity
			// sliding along a floor stays on the ground between landings.
			name: "purely horizontal movement keeps on ground",
			pos:  mgl64.Vec3{0.5, 64, 0.5}, vel: mgl64.Vec3{0.5, 0, 0}, startGround: true,
			wantDelta: mgl64.Vec3{0.5, 0, 0}, wantVelocity: mgl64.Vec3{0.5, 0, 0}, wantGround: true,
		},
		{
			// The same move with the flag already false leaves it false: nothing
			// in CheckCollision can set it without a downward Y resolution.
			name: "purely horizontal movement never sets on ground",
			pos:  mgl64.Vec3{0.5, 64, 0.5}, vel: mgl64.Vec3{0.5, 0, 0},
			wantDelta: mgl64.Vec3{0.5, 0, 0}, wantVelocity: mgl64.Vec3{0.5, 0, 0}, wantGround: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			handle := world.EntitySpawnOpts{Position: test.pos}.New(testMovingEntType{}, testMoveConfig{})
			mustDo(t, w, func(tx *world.Tx) {
				for pos, b := range test.blocks {
					tx.SetBlock(pos, b, nil)
				}
				e := tx.AddEntity(handle)

				c := &MovementComputer{}
				c.onGround = test.startGround
				dPos, vel := c.CheckCollision(tx, e, test.pos, test.vel)

				assertVec3(t, "delta", dPos, test.wantDelta)
				assertVec3(t, "velocity", vel, test.wantVelocity)
				if got := c.OnGround(); got != test.wantGround {
					t.Fatalf("OnGround() = %v, want %v", got, test.wantGround)
				}
			})
		})
	}
}

func TestCheckCollisionAxisThreshold(t *testing.T) {
	// PINNED BUG: the axis-skip guard is FloatEqualThreshold(delta, 0, epsilon)
	// with epsilon = 0.001, but mgl64 squares the threshold when either operand
	// is zero, so the real cutoff is 1e-6. An entity settling onto a floor with a
	// downward velocity between 1e-6 and 1e-3 has its Y axis resolved as
	// intended, but below 1e-6 the Y axis is skipped entirely: the entity sinks
	// into the floor and OnGround stays false, which is the opposite of what the
	// named constant promises. projectile.go:281 compensates for the same mgl64
	// behaviour with an explicit square root, so the omission here looks
	// unintended. Pinned as-is; decide in stage 1.
	tests := []struct {
		name       string
		velY       float64
		wantDeltaY float64
		wantGround bool
	}{
		{name: "above the squared cutoff, resolved", velY: -5e-6, wantDeltaY: 0, wantGround: true},
		{name: "below the squared cutoff, skipped", velY: -5e-7, wantDeltaY: -5e-7, wantGround: false},
		{name: "below the named epsilon but above the squared cutoff", velY: -5e-4, wantDeltaY: 0, wantGround: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			pos := mgl64.Vec3{0.5, 64, 0.5}
			handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
				e := tx.AddEntity(handle)

				c := &MovementComputer{}
				dPos, _ := c.CheckCollision(tx, e, pos, mgl64.Vec3{0, test.velY, 0})

				if dPos[1] != test.wantDeltaY {
					t.Fatalf("delta Y = %.17g, want %.17g", dPos[1], test.wantDeltaY)
				}
				if got := c.OnGround(); got != test.wantGround {
					t.Fatalf("OnGround() = %v, want %v", got, test.wantGround)
				}
			})
		})
	}
}

func TestCheckCollisionZeroesOnlyBlockedAxes(t *testing.T) {
	// Walking straight into a wall while also drifting along Z: only the blocked
	// axis loses its velocity, the free axis keeps every bit of it.
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{0.5, 0, 0.2})

		assertVec3(t, "delta", dPos, mgl64.Vec3{0.375, 0, 0.2})
		assertVec3(t, "velocity", vel, mgl64.Vec3{0, 0, 0.2})
	})
}

func TestCheckCollisionIgnoresOtherEntities(t *testing.T) {
	// PINNED SCOPE: entities do not collide with each other (movement.go:122
	// TODO). Intentionally out of scope per CLAUDE.md 2.6; pinned so a rewrite
	// cannot change it silently. A second entity sitting exactly on the path is
	// passed straight through, velocity intact.
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	mover := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	blocker := world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 0.5}}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(mover)
		tx.AddEntity(blocker)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{1, 0, 0})

		assertVec3(t, "delta", dPos, mgl64.Vec3{1, 0, 0})
		assertVec3(t, "velocity", vel, mgl64.Vec3{1, 0, 0})
	})
}

func TestBlockBBoxsAroundRange(t *testing.T) {
	// The searched volume is the box grown by 0.25, floored on the minimum and
	// ceiled on the maximum, with the maximum treated as exclusive.
	tests := []struct {
		name string
		box  cube.BBox
		want []cube.Pos
	}{
		{
			name: "a box well inside one block collects only that block",
			box:  cube.Box(0.4, 64.4, 0.4, 0.6, 64.6, 0.6),
			want: []cube.Pos{{0, 64, 0}},
		},
		{
			// Grown, this box spans exactly [0,1] on every axis: the ceiling lands
			// on 1 and the loop stops below it, so the neighbours starting at 1
			// are not collected.
			name: "the maximum bound is exclusive",
			box:  cube.Box(0.25, 64.25, 0.25, 0.75, 64.75, 0.75),
			want: []cube.Pos{{0, 64, 0}},
		},
		{
			name: "a box straddling a corner collects all eight blocks",
			box:  cube.Box(0.8, 64.8, 0.8, 1.2, 65.2, 1.2),
			want: []cube.Pos{
				{0, 64, 0}, {0, 64, 1}, {1, 64, 0}, {1, 64, 1},
				{0, 65, 0}, {0, 65, 1}, {1, 65, 0}, {1, 65, 1},
			},
		},
		{
			name: "negative coordinates floor away from zero",
			box:  cube.Box(-0.6, -0.6, -0.6, -0.4, -0.4, -0.4),
			want: []cube.Pos{{-1, -1, -1}},
		},
		{
			name: "a box crossing a chunk boundary collects both sides",
			box:  cube.Box(15.9, 64.4, 0.4, 16.1, 64.6, 0.6),
			want: []cube.Pos{{15, 64, 0}, {16, 64, 0}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				for _, pos := range test.want {
					tx.SetBlock(pos, block.Stone{}, nil)
				}
				got := blockBBoxsAround(tx, test.box)
				if len(got) != len(test.want) {
					t.Fatalf("blockBBoxsAround() returned %d boxes %v, want %d", len(got), got, len(test.want))
				}
				for _, pos := range test.want {
					want := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())
					if !slices.ContainsFunc(got, func(b cube.BBox) bool { return b == want }) {
						t.Fatalf("blockBBoxsAround() = %v, missing the full cube at %v", got, pos)
					}
				}
			})
		})
	}
}

func TestBlockBBoxsAroundExcludesNeighboursJustOutOfRange(t *testing.T) {
	// The companion to the exclusive-maximum case above: the same box with a
	// block placed at the excluded position must still return only one box.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 65, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 1}, block.Stone{}, nil)

		got := blockBBoxsAround(tx, cube.Box(0.25, 64.25, 0.25, 0.75, 64.75, 0.75))
		if len(got) != 1 {
			t.Fatalf("blockBBoxsAround() returned %d boxes %v, want only the block at the centre", len(got), got)
		}
	})
}

func TestBlockBBoxsAroundAllocatesNothingInOpenAir(t *testing.T) {
	// A hot-path contract: entities moving through air must not allocate a slice
	// every tick, so the nil return is behaviour, not an accident.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		if got := blockBBoxsAround(tx, cube.Box(0.4, 64.4, 0.4, 0.6, 64.6, 0.6)); got != nil {
			t.Fatalf("blockBBoxsAround() in open air = %v, want nil", got)
		}
	})
}

func TestBlockBBoxsAroundSkipsModellessBlocks(t *testing.T) {
	// Blocks whose model has no BBox contribute nothing and do not force the
	// slice to be allocated either.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Water{Depth: 8, Still: true}, nil)
		if got := blockBBoxsAround(tx, cube.Box(0.4, 64.4, 0.4, 0.6, 64.6, 0.6)); got != nil {
			t.Fatalf("blockBBoxsAround() around a block without a BBox = %v, want nil", got)
		}
	})
}

func TestBlockBBoxsAroundCollectsEveryBoxOfAMultiBoxBlock(t *testing.T) {
	// Stairs report two boxes. Both must be returned, and both must already be
	// translated into world coordinates.
	w := syncWorld(t)
	pos := cube.Pos{0, 64, 0}
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(pos, block.Stairs{Block: block.Stone{}, Facing: cube.North}, nil)

		got := blockBBoxsAround(tx, cube.Box(0.4, 64.4, 0.4, 0.6, 64.6, 0.6))
		if len(got) != 2 {
			t.Fatalf("blockBBoxsAround() over a stair returned %d boxes %v, want 2", len(got), got)
		}
		unit := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())
		for i, b := range got {
			if !within(unit, b) {
				t.Fatalf("box %d = %v, want it translated into the unit cube at %v", i, b, pos)
			}
		}
	})
}

func TestBlockBBoxsAroundCollectsTallBlocksFromBelow(t *testing.T) {
	// A fence is 1.5 blocks tall, but the search only covers whole blocks around
	// the box, so the parts of a tall model that stick out of their own block are
	// only found through the block position itself. Pinning the collected count
	// keeps a rewrite from quietly changing the searched volume.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.WoodFence{}, nil)

		// A box floating in the air above the fence post. Grown by 0.25 it spans
		// y [64.9, 65.6], so only y 64 and 65 are searched; the fence at y 64 is
		// reached and contributes its post box.
		got := blockBBoxsAround(tx, cube.Box(0.4, 65.15, 0.4, 0.6, 65.35, 0.6))
		if len(got) == 0 {
			t.Fatal("blockBBoxsAround() above a fence returned nothing, want the fence post box")
		}
		for i, b := range got {
			if b.Max()[1] <= 65 {
				t.Fatalf("box %d = %v, want a box reaching above y=65", i, b)
			}
		}
	})
}

// within reports whether inner lies entirely inside outer.
func within(outer, inner cube.BBox) bool {
	for axis := range 3 {
		if inner.Min()[axis] < outer.Min()[axis]-physEpsilon || inner.Max()[axis] > outer.Max()[axis]+physEpsilon {
			return false
		}
	}
	return true
}

func TestCheckCollisionNonFiniteVelocity(t *testing.T) {
	// PINNED BUG: CheckCollision does not reject non-finite velocities, and it
	// splits them. The NaN is returned as the movement delta and lands in the
	// entity position, while the "did this axis get blocked" test reads NaN as a
	// mismatch and zeroes the velocity. The entity is therefore teleported to a
	// NaN position and left with a velocity that says nothing happened. Pinned
	// as-is; decide in stage 1.
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{math.NaN(), 0, 0})

		if !math.IsNaN(dPos[0]) {
			t.Fatalf("delta X = %v, want NaN carried through unguarded", dPos[0])
		}
		if vel[0] != 0 {
			t.Fatalf("velocity X = %v, want 0: NaN compares unequal to itself, so the axis reads as blocked", vel[0])
		}
	})
}
