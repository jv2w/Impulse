package entity

import (
	"context"
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// benchWorld is syncWorld for benchmarks, which cannot use a *testing.T.
func benchWorld(b *testing.B) *world.World {
	b.Helper()
	w := world.Config{Synchronous: true}.New()
	b.Cleanup(func() {
		_ = w.Close()
	})
	return w
}

func benchDo(b *testing.B, w *world.World, f func(tx *world.Tx)) {
	b.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		b.Fatalf("world task failed: %v", err)
	}
}

// benchFloor lays a stone platform under pos so that the block searches below
// find bounding boxes rather than air.
func benchFloor(tx *world.Tx, pos mgl64.Vec3) {
	base := cube.PosFromVec3(pos)
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			tx.SetBlock(base.Add(cube.Pos{x, -1, z}), block.Stone{}, nil)
		}
	}
}

// benchItem spawns an item entity that neither expires nor merges over the
// course of a benchmark: the stack is already at its maximum count and the
// existence duration outlives any run.
func benchItem(pos mgl64.Vec3) *world.EntityHandle {
	stack := item.NewStack(item.Apple{}, 64)
	return world.EntitySpawnOpts{Position: pos}.New(ItemType, ItemBehaviourConfig{
		Item:              stack,
		Gravity:           0.04,
		Drag:              0.02,
		ExistenceDuration: math.MaxInt64,
	})
}

func BenchmarkEntTickItem(b *testing.B) {
	w := benchWorld(b)
	pos := mgl64.Vec3{5.5, 64, 5.5}
	benchDo(b, w, func(tx *world.Tx) {
		benchFloor(tx, pos)
		e := tx.AddEntity(benchItem(pos)).(*Ent)

		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			e.Tick(tx, int64(i))
		}
	})
}

func BenchmarkItemBehaviourCheckNearby(b *testing.B) {
	w := benchWorld(b)
	pos := mgl64.Vec3{5.5, 64, 5.5}
	benchDo(b, w, func(tx *world.Tx) {
		e := tx.AddEntity(benchItem(pos)).(*Ent)
		// Neighbours close enough to be considered, but full, so merge always
		// declines and the walk runs to completion on every call.
		for i := 0; i < 8; i++ {
			tx.AddEntity(benchItem(pos.Add(mgl64.Vec3{float64(i)*0.1 - 0.4, 0, 0.25})))
		}
		i := e.Behaviour().(*ItemBehaviour)

		b.ReportAllocs()
		for b.Loop() {
			i.checkNearby(e, tx)
		}
	})
}

var (
	benchDPos mgl64.Vec3
	benchVel  mgl64.Vec3
)

func BenchmarkCheckCollision(b *testing.B) {
	w := benchWorld(b)
	pos := mgl64.Vec3{5.5, 64, 5.5}
	benchDo(b, w, func(tx *world.Tx) {
		benchFloor(tx, pos)
		e := tx.AddEntity(benchItem(pos))

		c := &MovementComputer{Gravity: 0.04, Drag: 0.02, DragBeforeGravity: true}
		vel := mgl64.Vec3{0.1, -0.08, 0.1}

		b.ReportAllocs()
		for b.Loop() {
			benchDPos, benchVel = c.CheckCollision(tx, e, pos, vel)
		}
	})
}
