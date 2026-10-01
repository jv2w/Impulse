package entity

import (
	"context"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

func mustDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatalf("world task failed: %v", err)
	}
}

type testMoveConfig struct {
	delta mgl64.Vec3
}

func (c testMoveConfig) Apply(data *world.EntityData) {
	data.Data = &testMoveBehaviour{delta: c.delta}
}

type testMoveBehaviour struct {
	delta mgl64.Vec3
}

func (b *testMoveBehaviour) Tick(e *Ent, _ *world.Tx) *Movement {
	e.data.Pos = e.data.Pos.Add(b.delta)
	return nil
}

type testMovingEntType struct{}

func (testMovingEntType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (testMovingEntType) EncodeEntity() string { return "minecraft:test_moving_ent" }
func (testMovingEntType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}
func (testMovingEntType) DecodeNBT(map[string]any, *world.EntityData) {}
func (testMovingEntType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
