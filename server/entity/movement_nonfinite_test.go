package entity

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Reproduction for the non-finite velocity defect: a NaN or infinite component
// from a packet or a plugin must neither hang the world goroutine nor poison
// the entity position. These assert the fixed behaviour and fail on the current
// code (NaN is carried into the delta; Inf never returns).
func TestCheckCollisionRejectsNaNVelocityFixed(t *testing.T) {
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{math.NaN(), 0, 0.5})

		for axis, v := range dPos {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("delta[%d] = %v, want finite (no poisoned axis)", axis, v)
			}
		}
		for axis, v := range vel {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("velocity[%d] = %v, want finite", axis, v)
			}
		}
		// The poisoned axis must not move; the healthy axis keeps working, which
		// also proves the NaN did not silently disable collision everywhere.
		if dPos[0] != 0 {
			t.Fatalf("delta X = %v, want 0 for the rejected axis", dPos[0])
		}
		if dPos[2] != 0.5 {
			t.Fatalf("delta Z = %v, want the full 0.5 on the healthy axis", dPos[2])
		}
	})
}

func TestCheckCollisionRejectsInfVelocityPromptly(t *testing.T) {
	// Direct call on purpose: before the fix this hangs inside
	// blockBBoxsAround, so run it isolated with go test -timeout to watch it
	// fail. After the fix it returns promptly with a finite delta.
	w := syncWorld(t)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, vel := c.CheckCollision(tx, e, pos, mgl64.Vec3{math.Inf(1), 0, 0})

		for axis, v := range dPos {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("delta[%d] = %v, want finite", axis, v)
			}
		}
		for axis, v := range vel {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("velocity[%d] = %v, want finite", axis, v)
			}
		}
		if dPos[0] != 0 {
			t.Fatalf("delta X = %v, want 0 for the rejected axis", dPos[0])
		}
	})
}

func TestBlockBBoxsAroundNonFiniteBoxReturnsNilPromptly(t *testing.T) {
	// Direct call on purpose: before the fix this walks from math.MinInt64 to
	// a finite bound and never returns, so run it isolated with go test
	// -timeout to watch it fail. After the fix it returns nil promptly.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		box := cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125).
			Translate(mgl64.Vec3{0.5, 64, 0.5}).
			Extend(mgl64.Vec3{0, math.Inf(-1), 0})
		if got := blockBBoxsAround(tx, box); got != nil {
			t.Fatalf("blockBBoxsAround() with infinite box = %v, want nil", got)
		}
	})
}
