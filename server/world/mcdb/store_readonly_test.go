package mcdb

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// Regression for the StoreColumn caller-mutation defect: storing a column is a
// read-only operation on its argument. The on-disk bytes still carry the
// storage keys (UniqueID, x/y/z), but the caller's maps must come back
// untouched.
func TestStoreColumnLeavesCallerDataAlone(t *testing.T) {
	db := newDB(t)
	col := emptyColumn(world.Overworld)
	entityData := map[string]any{"identifier": "minecraft:pig"}
	blockEntityData := map[string]any{"id": "Chest"}
	col.Entities = []chunk.Entity{{ID: 42, Data: entityData}}
	col.BlockEntities = []chunk.BlockEntity{{Pos: cube.Pos{1, 2, 3}, Data: blockEntityData}}

	if err := db.StoreColumn(world.ChunkPos{0, 0}, world.Overworld, col); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, ok := entityData["UniqueID"]; ok {
		t.Fatalf("entity data gained %q: StoreColumn wrote into the caller's map", "UniqueID")
	}
	for _, k := range []string{"x", "y", "z"} {
		if _, ok := blockEntityData[k]; ok {
			t.Fatalf("block entity data gained %q: StoreColumn wrote into the caller's map", k)
		}
	}

	// The stored bytes still carry the keys: golden values on the round trip.
	got, err := db.LoadColumn(world.ChunkPos{0, 0}, world.Overworld)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].ID != 42 {
		t.Fatalf("entities = %+v, want one entity with ID 42", got.Entities)
	}
	if v, ok := got.Entities[0].Data["UniqueID"]; !ok || v != int64(42) {
		t.Fatalf("stored entity UniqueID = %v (present %v), want int64(42)", v, ok)
	}
	if len(got.BlockEntities) != 1 {
		t.Fatalf("block entities = %+v, want one", got.BlockEntities)
	}
	for k, want := range map[string]any{"x": int32(1), "y": int32(2), "z": int32(3)} {
		if v := got.BlockEntities[0].Data[k]; v != want {
			t.Fatalf("stored block entity %q = %v, want %v", k, v, want)
		}
	}
}
