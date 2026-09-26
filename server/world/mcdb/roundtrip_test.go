package mcdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// Runtime IDs of testBlocks, by index.
const (
	ridAir uint32 = iota
	ridStone
	ridGlass
	ridChest
	ridTorch
	ridWater
)

// dims is every dimension a column can be stored under. index() encodes each one differently, so every
// round-trip case runs against all three.
var dims = []struct {
	name string
	dim  world.Dimension
}{
	{"overworld", world.Overworld},
	{"nether", world.Nether},
	{"end", world.End},
}

func TestRoundTripEmptyColumn(t *testing.T) {
	for _, d := range dims {
		t.Run(d.name, func(t *testing.T) {
			db := newDB(t)
			want := emptyColumn(d.dim)
			got := storeLoad(t, db, world.ChunkPos{0, 0}, d.dim, want)

			assertColumnEq(t, want, got, d.dim.Range())
			if got.Entities != nil {
				t.Errorf("entities: want nil, got %+v", got.Entities)
			}
			if got.BlockEntities != nil {
				t.Errorf("block entities: want nil, got %+v", got.BlockEntities)
			}
			if got.ScheduledBlocks != nil {
				t.Errorf("scheduled blocks: want nil, got %+v", got.ScheduledBlocks)
			}
		})
	}
}

func TestRoundTripBlocks(t *testing.T) {
	type placement struct {
		x, z  uint8
		y     int16
		layer uint8
		rid   uint32
	}
	cases := []struct {
		name       string
		dim        world.Dimension
		placements []placement
	}{
		{
			name: "single block",
			dim:  world.Overworld,
			placements: []placement{
				{x: 3, y: 10, z: 7, rid: ridStone},
			},
		},
		{
			name: "several sub chunks",
			dim:  world.Overworld,
			placements: []placement{
				{x: 0, y: -60, z: 0, rid: ridStone},
				{x: 5, y: 5, z: 5, rid: ridGlass},
				{x: 15, y: 100, z: 15, rid: ridChest},
				{x: 8, y: 200, z: 8, rid: ridTorch},
				{x: 1, y: 300, z: 1, rid: ridWater},
			},
		},
		{
			name: "world height boundaries",
			dim:  world.Overworld,
			placements: []placement{
				{x: 0, y: -64, z: 0, rid: ridStone},
				{x: 15, y: 319, z: 15, rid: ridGlass},
			},
		},
		{
			name: "nether boundaries",
			dim:  world.Nether,
			placements: []placement{
				{x: 0, y: 0, z: 0, rid: ridStone},
				{x: 15, y: 127, z: 15, rid: ridGlass},
			},
		},
		{
			name: "end boundaries",
			dim:  world.End,
			placements: []placement{
				{x: 0, y: 0, z: 0, rid: ridStone},
				{x: 15, y: 255, z: 15, rid: ridGlass},
			},
		},
		{
			name: "second layer",
			dim:  world.Overworld,
			placements: []placement{
				{x: 4, y: 40, z: 4, layer: 0, rid: ridWater},
				{x: 4, y: 40, z: 4, layer: 1, rid: ridGlass},
			},
		},
		{
			name: "full sub chunk of distinct blocks",
			dim:  world.Overworld,
			placements: func() []placement {
				var p []placement
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						p = append(p, placement{x: x, y: int16(16 + int(x+z)%16), z: z, rid: 1 + uint32(x+z)%5})
					}
				}
				return p
			}(),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			want := emptyColumn(c.dim)
			for _, p := range c.placements {
				want.Chunk.SetBlock(p.x, p.y, p.z, p.layer, p.rid)
			}
			got := storeLoad(t, db, world.ChunkPos{2, -3}, c.dim, want)
			assertColumnEq(t, want, got, c.dim.Range())

			// Guard against a comparison that would pass on two all-air chunks.
			for _, p := range c.placements {
				if g := got.Chunk.Block(p.x, p.y, p.z, p.layer); g != p.rid {
					t.Errorf("block (%v,%v,%v) layer %v: want rid %v, got %v", p.x, p.y, p.z, p.layer, p.rid, g)
				}
			}
		})
	}
}

func TestRoundTripBiomes(t *testing.T) {
	cases := []struct {
		name  string
		setup func(c *chunk.Chunk)
	}{
		{
			name:  "all default",
			setup: func(*chunk.Chunk) {},
		},
		{
			name: "one biome per sub chunk",
			setup: func(c *chunk.Chunk) {
				r := c.Range()
				for y := int16(r[0]); int(y) <= r[1]; y++ {
					b := uint32((int(y)-r[0])>>4) + 1
					for x := uint8(0); x < 16; x++ {
						for z := uint8(0); z < 16; z++ {
							c.SetBiome(x, y, z, b)
						}
					}
				}
			},
		},
		{
			name: "same biome in every sub chunk",
			setup: func(c *chunk.Chunk) {
				r := c.Range()
				for y := int16(r[0]); int(y) <= r[1]; y++ {
					for x := uint8(0); x < 16; x++ {
						for z := uint8(0); z < 16; z++ {
							c.SetBiome(x, y, z, 4)
						}
					}
				}
			},
		},
		{
			name: "biomes varying within a sub chunk",
			setup: func(c *chunk.Chunk) {
				for y := int16(0); y < 16; y++ {
					for x := uint8(0); x < 16; x++ {
						for z := uint8(0); z < 16; z++ {
							c.SetBiome(x, y, z, uint32(x)*31+uint32(z)*7+uint32(y))
						}
					}
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			want := emptyColumn(world.Overworld)
			c.setup(want.Chunk)
			got := storeLoad(t, db, world.ChunkPos{0, 0}, world.Overworld, want)
			assertColumnEq(t, want, got, world.Overworld.Range())
		})
	}
}

// TestStoreBiomesPrefixesEmptyHeightmap fixes the layout of key3DData: 512 bytes of heightmap that the loader
// discards, then the biome storages. Vanilla reads those 512 bytes, so their presence is part of the format.
func TestStoreBiomesPrefixesEmptyHeightmap(t *testing.T) {
	db := newDB(t)
	k := dbKey{pos: world.ChunkPos{0, 0}, dim: world.Overworld}
	if err := db.StoreColumn(k.pos, k.dim, emptyColumn(k.dim)); err != nil {
		t.Fatalf("store column: %v", err)
	}
	data, ok := getRaw(t, db, k.Sum(key3DData))
	if !ok {
		t.Fatal("key3DData not written")
	}
	if len(data) <= 512 {
		t.Fatalf("len(key3DData) = %v, want > 512", len(data))
	}
	if !bytes.Equal(data[:512], make([]byte, 512)) {
		t.Errorf("heightmap prefix = %x, want 512 zero bytes", data[:512])
	}
}

// TestLoadBiomesSharedStorage covers the "pointer to previous storage" flag (0x7f) that vanilla writes when
// consecutive sub chunks hold identical biome data. The store path in this package never emits it, so the
// value is crafted by hand.
func TestLoadBiomesSharedStorage(t *testing.T) {
	db := newDB(t)
	pos, dim := world.ChunkPos{0, 0}, world.Overworld
	if err := db.StoreColumn(pos, dim, emptyColumn(dim)); err != nil {
		t.Fatalf("store column: %v", err)
	}

	k := dbKey{pos: pos, dim: dim}
	subChunks := (dim.Range().Height() >> 4) + 1

	// One paletted storage with 0 bits per index (a single palette entry, no indices), followed by a
	// "same as previous" marker for every remaining sub chunk.
	biomes := make([]byte, 512)
	biomes = append(biomes, 0x00)
	biomes = binary.LittleEndian.AppendUint32(biomes, 7)
	biomes = append(biomes, bytes.Repeat([]byte{0x7f << 1}, subChunks-1)...)
	putRaw(t, db, k.Sum(key3DData), biomes)

	col, err := db.LoadColumn(pos, dim)
	if err != nil {
		t.Fatalf("load column: %v", err)
	}
	r := dim.Range()
	for y := int16(r[0]); int(y) <= r[1]; y += 16 {
		if got := col.Chunk.Biome(0, y, 0); got != 7 {
			t.Fatalf("biome at y=%v: want 7, got %v", y, got)
		}
	}
	// The shared storages must be copies, not the same pointer: writing to one sub chunk's biomes may not
	// change another's.
	col.Chunk.SetBiome(0, 0, 0, 9)
	if got := col.Chunk.Biome(0, 16, 0); got != 7 {
		t.Errorf("biome at y=16 after writing y=0: want 7, got %v (storages are aliased)", got)
	}
}

func TestRoundTripBlockEntities(t *testing.T) {
	cases := []struct {
		name string
		be   []chunk.BlockEntity
	}{
		{
			name: "single",
			be: []chunk.BlockEntity{
				{Pos: cube.Pos{1, 2, 3}, Data: map[string]any{
					"id": "Chest", "x": int32(1), "y": int32(2), "z": int32(3),
				}},
			},
		},
		{
			name: "several, negative and boundary Y",
			be: []chunk.BlockEntity{
				{Pos: cube.Pos{0, -64, 0}, Data: map[string]any{
					"id": "Chest", "x": int32(0), "y": int32(-64), "z": int32(0),
				}},
				{Pos: cube.Pos{15, 319, 15}, Data: map[string]any{
					"id": "Sign", "Text": "hello", "x": int32(15), "y": int32(319), "z": int32(15),
				}},
				{Pos: cube.Pos{-5, 7, -9}, Data: map[string]any{
					"id": "Furnace", "BurnTime": int16(200), "x": int32(-5), "y": int32(7), "z": int32(-9),
				}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			want := emptyColumn(world.Overworld)
			want.BlockEntities = c.be
			got := storeLoad(t, db, world.ChunkPos{1, 1}, world.Overworld, want)
			assertColumnEq(t, want, got, world.Overworld.Range())
		})
	}
}

// TestBlockEntityPosOverwritesNBTCoordinates fixes that BlockEntity.Pos is authoritative on store: the x/y/z
// keys in Data are overwritten from Pos, and Pos is read back from those keys on load.
func TestBlockEntityPosOverwritesNBTCoordinates(t *testing.T) {
	db := newDB(t)
	col := emptyColumn(world.Overworld)
	col.BlockEntities = []chunk.BlockEntity{
		{Pos: cube.Pos{4, 5, 6}, Data: map[string]any{"id": "Chest", "x": int32(99), "y": int32(99), "z": int32(99)}},
	}
	got := storeLoad(t, db, world.ChunkPos{0, 0}, world.Overworld, col)

	if len(got.BlockEntities) != 1 {
		t.Fatalf("block entities: want 1, got %v", len(got.BlockEntities))
	}
	if p := got.BlockEntities[0].Pos; p != (cube.Pos{4, 5, 6}) {
		t.Errorf("pos: want (4,5,6), got %v", p)
	}
	for _, k := range []string{"x", "y", "z"} {
		if _, ok := got.BlockEntities[0].Data[k].(int32); !ok {
			t.Errorf("data[%q]: want int32, got %T", k, got.BlockEntities[0].Data[k])
		}
	}
	if got.BlockEntities[0].Data["x"] != int32(4) {
		t.Errorf("data[\"x\"]: want 4, got %v", got.BlockEntities[0].Data["x"])
	}
}

func TestRoundTripEntities(t *testing.T) {
	cases := []struct {
		name     string
		entities []chunk.Entity
	}{
		{
			name: "single",
			entities: []chunk.Entity{
				{ID: 1, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(1)}},
			},
		},
		{
			name: "several, including negative IDs",
			entities: []chunk.Entity{
				{ID: -1, Data: map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(-1)}},
				{ID: 0, Data: map[string]any{"identifier": "minecraft:sheep", "UniqueID": int64(0)}},
				{ID: 1 << 40, Data: map[string]any{"identifier": "minecraft:zombie", "UniqueID": int64(1 << 40)}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			want := emptyColumn(world.Overworld)
			want.Entities = c.entities
			got := storeLoad(t, db, world.ChunkPos{-4, 8}, world.Overworld, want)
			assertColumnEq(t, want, got, world.Overworld.Range())
		})
	}
}

// TestStoreEntitiesWritesNewFormat fixes the storage layout of the new (actorprefix/digp) entity format: one
// key per entity holding its NBT, plus one index key holding the little-endian IDs back to back.
func TestStoreEntitiesWritesNewFormat(t *testing.T) {
	db := newDB(t)
	pos, dim := world.ChunkPos{3, 4}, world.Overworld
	col := emptyColumn(dim)
	col.Entities = []chunk.Entity{
		{ID: 7, Data: map[string]any{"identifier": "minecraft:pig"}},
		{ID: -9, Data: map[string]any{"identifier": "minecraft:cow"}},
	}
	if err := db.StoreColumn(pos, dim, col); err != nil {
		t.Fatalf("store column: %v", err)
	}

	ids, ok := getRaw(t, db, append([]byte(keyEntityIdentifiers), index(pos, dim)...))
	if !ok {
		t.Fatal("digp key not written")
	}
	var want []byte
	for _, id := range []int64{7, -9} {
		want = binary.LittleEndian.AppendUint64(want, uint64(id))
	}
	if !bytes.Equal(ids, want) {
		t.Errorf("digp value = %x, want %x", ids, want)
	}
	for _, id := range []int64{7, -9} {
		if _, ok := getRaw(t, db, entityIndex(id)); !ok {
			t.Errorf("entity %v not written under %x", id, entityIndex(id))
		}
	}
	// The old-format key must be cleared so a load never falls back to it.
	if _, ok := getRaw(t, db, dbKey{pos: pos, dim: dim}.Sum(keyEntitiesOld)); ok {
		t.Error("keyEntitiesOld still present after storing new-format entities")
	}
}

// TestLoadEntitiesOldFormat covers the pre-actorprefix layout: NBT compounds appended to each other under
// keyEntitiesOld. It is only read when the digp key is absent.
func TestLoadEntitiesOldFormat(t *testing.T) {
	t.Run("with UniqueID", func(t *testing.T) {
		db := newDB(t)
		pos, dim := world.ChunkPos{0, 0}, world.Overworld
		k := dbKey{pos: pos, dim: dim}
		if err := db.StoreColumn(pos, dim, emptyColumn(dim)); err != nil {
			t.Fatalf("store column: %v", err)
		}
		putRaw(t, db, k.Sum(keyEntitiesOld), appendNBT(t,
			map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(11)},
			map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(-22)},
		))

		col, err := db.LoadColumn(pos, dim)
		if err != nil {
			t.Fatalf("load column: %v", err)
		}
		want := []chunk.Entity{
			{ID: 11, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(11)}},
			{ID: -22, Data: map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(-22)}},
		}
		assertEntitiesEq(t, want, col.Entities)
	})

	t.Run("missing UniqueID gets a random ID", func(t *testing.T) {
		db := newDB(t)
		pos, dim := world.ChunkPos{0, 0}, world.Overworld
		k := dbKey{pos: pos, dim: dim}
		if err := db.StoreColumn(pos, dim, emptyColumn(dim)); err != nil {
			t.Fatalf("store column: %v", err)
		}
		putRaw(t, db, k.Sum(keyEntitiesOld), appendNBT(t,
			map[string]any{"identifier": "minecraft:pig"},
		))

		col, err := db.LoadColumn(pos, dim)
		if err != nil {
			t.Fatalf("load column: %v", err)
		}
		if len(col.Entities) != 1 {
			t.Fatalf("entities: want 1, got %v", len(col.Entities))
		}
		// The ID is drawn from math/rand/v2 with no seeding hook, so only its effect is observable: the
		// entity is kept rather than dropped, and Data is untouched.
		if _, ok := col.Entities[0].Data["UniqueID"]; ok {
			t.Errorf("UniqueID was added to Data on load: %+v", col.Entities[0].Data)
		}
	})

	t.Run("digp wins over keyEntitiesOld", func(t *testing.T) {
		db := newDB(t)
		pos, dim := world.ChunkPos{0, 0}, world.Overworld
		k := dbKey{pos: pos, dim: dim}
		col := emptyColumn(dim)
		col.Entities = []chunk.Entity{{ID: 5, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(5)}}}
		if err := db.StoreColumn(pos, dim, col); err != nil {
			t.Fatalf("store column: %v", err)
		}
		putRaw(t, db, k.Sum(keyEntitiesOld), appendNBT(t,
			map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(6)},
		))

		got, err := db.LoadColumn(pos, dim)
		if err != nil {
			t.Fatalf("load column: %v", err)
		}
		assertEntitiesEq(t, col.Entities, got.Entities)
	})
}

// TestStoreColumnMigratesOldFormatEntities fixes that a store always clears keyEntitiesOld, so a world read in
// the old format and written back is migrated rather than left with two competing entity lists.
func TestStoreColumnMigratesOldFormatEntities(t *testing.T) {
	db := newDB(t)
	pos, dim := world.ChunkPos{0, 0}, world.Overworld
	k := dbKey{pos: pos, dim: dim}
	if err := db.StoreColumn(pos, dim, emptyColumn(dim)); err != nil {
		t.Fatalf("store column: %v", err)
	}
	putRaw(t, db, k.Sum(keyEntitiesOld), appendNBT(t,
		map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(11)},
	))

	col, err := db.LoadColumn(pos, dim)
	if err != nil {
		t.Fatalf("load column: %v", err)
	}
	if err := db.StoreColumn(pos, dim, col); err != nil {
		t.Fatalf("store column: %v", err)
	}
	if _, ok := getRaw(t, db, k.Sum(keyEntitiesOld)); ok {
		t.Error("keyEntitiesOld survived a store")
	}
	got, err := db.LoadColumn(pos, dim)
	if err != nil {
		t.Fatalf("load column: %v", err)
	}
	assertEntitiesEq(t, col.Entities, got.Entities)
}

func TestRoundTripScheduledUpdates(t *testing.T) {
	cases := []struct {
		name    string
		tick    int64
		updates []chunk.ScheduledBlockUpdate
	}{
		{
			name: "single",
			tick: 100,
			updates: []chunk.ScheduledBlockUpdate{
				{Pos: cube.Pos{1, 2, 3}, Block: ridStone, Tick: 120},
			},
		},
		{
			name: "several with negative coordinates and ticks",
			tick: 5000,
			updates: []chunk.ScheduledBlockUpdate{
				{Pos: cube.Pos{0, -64, 0}, Block: ridWater, Tick: 5001},
				{Pos: cube.Pos{-3, 0, -7}, Block: ridChest, Tick: -1},
				{Pos: cube.Pos{15, 319, 15}, Block: ridTorch, Tick: 1 << 40},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			want := emptyColumn(world.Overworld)
			want.Tick, want.ScheduledBlocks = c.tick, c.updates
			got := storeLoad(t, db, world.ChunkPos{0, 0}, world.Overworld, want)
			assertColumnEq(t, want, got, world.Overworld.Range())
		})
	}
}

// PINNED BUG: Column.Tick lives inside the
// keyPendingScheduledTicks value, and storeScheduledUpdates deletes that key outright when there is nothing
// scheduled. A column with no pending ticks therefore comes back with Tick 0 no matter what was stored.
func TestColumnTickLostWithoutScheduledUpdates(t *testing.T) {
	db := newDB(t)
	col := emptyColumn(world.Overworld)
	col.Tick = 123456

	got := storeLoad(t, db, world.ChunkPos{0, 0}, world.Overworld, col)
	if got.Tick != 0 {
		t.Fatalf("Tick = %v; the tick is no longer dropped when ScheduledBlocks is empty", got.Tick)
	}
}

// PINNED BUG: Column.Tick is an int64 but scheduledUpdates.CurrentTick is
// an int32, so any tick above 2^31-1 is silently truncated on store.
func TestColumnTickTruncatedToInt32(t *testing.T) {
	db := newDB(t)
	col := emptyColumn(world.Overworld)
	col.Tick = 1 << 31
	col.ScheduledBlocks = []chunk.ScheduledBlockUpdate{{Pos: cube.Pos{0, 0, 0}, Block: ridStone, Tick: 1}}

	got := storeLoad(t, db, world.ChunkPos{0, 0}, world.Overworld, col)
	if got.Tick != -1<<31 {
		t.Fatalf("Tick = %v, want %v (int32 truncation of %v)", got.Tick, -1<<31, col.Tick)
	}
	// Per-update ticks are int64 on disk and survive.
	if got.ScheduledBlocks[0].Tick != 1 {
		t.Errorf("update tick = %v, want 1", got.ScheduledBlocks[0].Tick)
	}
}

func TestRoundTripChunkPositions(t *testing.T) {
	positions := []world.ChunkPos{
		{0, 0},
		{1, 1},
		{-1, -1},
		{-1, 1},
		{1, -1},
		{0, -1},
		{-1, 0},
		{1 << 20, -1 << 20},
		{-1 << 24, 1 << 24},
		{2147483647, -2147483648},
	}
	for _, d := range dims {
		t.Run(d.name, func(t *testing.T) {
			db := newDB(t)
			// All positions live in the same DB so that a coordinate encoding that collided would show up as
			// a wrong block rather than passing.
			for i, pos := range positions {
				col := emptyColumn(d.dim)
				col.Chunk.SetBlock(0, int16(d.dim.Range()[0]), 0, 0, 1+uint32(i)%5)
				if err := db.StoreColumn(pos, d.dim, col); err != nil {
					t.Fatalf("store %v: %v", pos, err)
				}
			}
			for i, pos := range positions {
				col, err := db.LoadColumn(pos, d.dim)
				if err != nil {
					t.Fatalf("load %v: %v", pos, err)
				}
				want := 1 + uint32(i)%5
				if got := col.Chunk.Block(0, int16(d.dim.Range()[0]), 0, 0); got != want {
					t.Errorf("%v: block = %v, want %v", pos, got, want)
				}
			}
		})
	}
}

// TestDimensionsDoNotCollide fixes that the same chunk position in different dimensions is stored under
// different keys.
func TestDimensionsDoNotCollide(t *testing.T) {
	db := newDB(t)
	pos := world.ChunkPos{5, -5}
	for i, d := range dims {
		col := emptyColumn(d.dim)
		col.Chunk.SetBlock(0, int16(d.dim.Range()[0]), 0, 0, uint32(i)+1)
		if err := db.StoreColumn(pos, d.dim, col); err != nil {
			t.Fatalf("store %v: %v", d.name, err)
		}
	}
	for i, d := range dims {
		col, err := db.LoadColumn(pos, d.dim)
		if err != nil {
			t.Fatalf("load %v: %v", d.name, err)
		}
		if got, want := col.Chunk.Block(0, int16(d.dim.Range()[0]), 0, 0), uint32(i)+1; got != want {
			t.Errorf("%v: block = %v, want %v", d.name, got, want)
		}
		if got, want := col.Chunk.Range(), d.dim.Range(); got != want {
			t.Errorf("%v: range = %v, want %v", d.name, got, want)
		}
	}
}

// TestUnregisteredDimensionIsRejected covers a Dimension that was never passed to world.RegisterDimension.
// index() encodes such a dimension as ID 0, which is the Overworld's key, so LoadColumn and StoreColumn have
// to refuse it before a key is built: a store would otherwise write over the Overworld chunk at the same
// position, and a shorter range would leave that chunk undecodable for good.
func TestUnregisteredDimensionIsRejected(t *testing.T) {
	unknown := unknownDim{world.Overworld.Range()}
	if _, ok := world.DimensionID(unknown); ok {
		t.Fatal("unknownDim is registered; this test needs an unregistered dimension")
	}
	// The reason the guard belongs above index rather than inside it: index has no way to report the
	// collision, and the key it returns is indistinguishable from an Overworld one.
	if got, want := index(world.ChunkPos{2, 2}, unknown), index(world.ChunkPos{2, 2}, world.Overworld); !bytes.Equal(got, want) {
		t.Fatalf("index(unknown) = %x, index(overworld) = %x, want them equal", got, want)
	}

	t.Run("store", func(t *testing.T) {
		db := newDB(t)
		pos := world.ChunkPos{2, 2}

		overworld := emptyColumn(world.Overworld)
		overworld.Chunk.SetBlock(0, 0, 0, 0, ridStone)
		if err := db.StoreColumn(pos, world.Overworld, overworld); err != nil {
			t.Fatalf("store overworld: %v", err)
		}
		other := emptyColumn(unknown)
		other.Chunk.SetBlock(0, 0, 0, 0, ridGlass)
		err := db.StoreColumn(pos, unknown, other)
		if err == nil {
			t.Fatal("store unknown dimension succeeded")
		}
		if !strings.Contains(err.Error(), "unregistered dimension") {
			t.Errorf("err = %v, want it to contain \"unregistered dimension\"", err)
		}

		col, loadErr := db.LoadColumn(pos, world.Overworld)
		if loadErr != nil {
			t.Fatalf("load overworld: %v", loadErr)
		}
		if got := col.Chunk.Block(0, 0, 0, 0); got != ridStone {
			t.Errorf("overworld block = %v, want %v", got, ridStone)
		}
	})

	t.Run("load", func(t *testing.T) {
		db := newDB(t)
		pos := world.ChunkPos{2, 2}
		if err := db.StoreColumn(pos, world.Overworld, emptyColumn(world.Overworld)); err != nil {
			t.Fatalf("store overworld: %v", err)
		}

		_, err := db.LoadColumn(pos, unknown)
		if err == nil {
			t.Fatal("load unknown dimension succeeded; it returned the overworld column")
		}
		if !strings.Contains(err.Error(), "unregistered dimension") {
			t.Errorf("err = %v, want it to contain \"unregistered dimension\"", err)
		}
	})
}

// TestStoreColumnReplacesPreviousState fixes that a store fully replaces what was there: entities, block
// entities and scheduled ticks left over from an earlier store must not survive.
func TestStoreColumnReplacesPreviousState(t *testing.T) {
	db := newDB(t)
	pos, dim := world.ChunkPos{0, 0}, world.Overworld
	k := dbKey{pos: pos, dim: dim}

	full := emptyColumn(dim)
	full.Chunk.SetBlock(1, 1, 1, 0, ridStone)
	full.Entities = []chunk.Entity{
		{ID: 100, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(100)}},
		{ID: 101, Data: map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(101)}},
	}
	full.BlockEntities = []chunk.BlockEntity{
		{Pos: cube.Pos{1, 1, 1}, Data: map[string]any{"id": "Chest", "x": int32(1), "y": int32(1), "z": int32(1)}},
	}
	full.Tick = 10
	full.ScheduledBlocks = []chunk.ScheduledBlockUpdate{{Pos: cube.Pos{1, 1, 1}, Block: ridStone, Tick: 11}}
	if err := db.StoreColumn(pos, dim, full); err != nil {
		t.Fatalf("store full: %v", err)
	}

	empty := emptyColumn(dim)
	if err := db.StoreColumn(pos, dim, empty); err != nil {
		t.Fatalf("store empty: %v", err)
	}

	got, err := db.LoadColumn(pos, dim)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertColumnEq(t, empty, got, dim.Range())

	// The entity records themselves must be gone, not merely unreferenced.
	for _, id := range []int64{100, 101} {
		if _, ok := getRaw(t, db, entityIndex(id)); ok {
			t.Errorf("entity %v survived a store that dropped it", id)
		}
	}
	for _, key := range [][]byte{
		append([]byte(keyEntityIdentifiers), index(pos, dim)...),
		k.Sum(keyBlockEntities),
		k.Sum(keyPendingScheduledTicks),
	} {
		if _, ok := getRaw(t, db, key); ok {
			t.Errorf("key %x survived a store that dropped its contents", key)
		}
	}
}

// TestStoreColumnKeepsEntitiesThatStay fixes the deletion rule in storeEntities: only IDs that disappear from
// the chunk's index are removed.
func TestStoreColumnKeepsEntitiesThatStay(t *testing.T) {
	db := newDB(t)
	pos, dim := world.ChunkPos{0, 0}, world.Overworld

	col := emptyColumn(dim)
	col.Entities = []chunk.Entity{
		{ID: 1, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(1)}},
		{ID: 2, Data: map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(2)}},
	}
	if err := db.StoreColumn(pos, dim, col); err != nil {
		t.Fatalf("store: %v", err)
	}
	col.Entities = col.Entities[:1]
	if err := db.StoreColumn(pos, dim, col); err != nil {
		t.Fatalf("store: %v", err)
	}

	if _, ok := getRaw(t, db, entityIndex(1)); !ok {
		t.Error("entity 1 was removed although it is still in the chunk")
	}
	if _, ok := getRaw(t, db, entityIndex(2)); ok {
		t.Error("entity 2 survived although it left the chunk")
	}
}

func TestStoreColumnMutatesCallerData(t *testing.T) {
	// StoreColumn is read-only on its argument: the storage keys live in the
	// on-disk bytes, never in the caller's maps.
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
		t.Fatalf("entity data gained UniqueID: StoreColumn wrote into the caller's map")
	}
	if _, ok := blockEntityData["x"]; ok {
		t.Fatalf("block entity data gained storage keys: StoreColumn wrote into the caller's map")
	}
}

// TestRoundTripAcrossReopen fixes that a stored column survives closing and reopening the world, which is the
// only thing users ultimately care about.
func TestRoundTripAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	pos, dim := world.ChunkPos{-7, 3}, world.Nether

	want := emptyColumn(dim)
	want.Chunk.SetBlock(2, 40, 6, 0, ridChest)
	want.Chunk.SetBiome(2, 40, 6, 12)
	want.Entities = []chunk.Entity{{ID: 3, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(3)}}}
	want.BlockEntities = []chunk.BlockEntity{
		{Pos: cube.Pos{2, 40, 6}, Data: map[string]any{"id": "Chest", "x": int32(2), "y": int32(40), "z": int32(6)}},
	}
	want.Tick = 77
	want.ScheduledBlocks = []chunk.ScheduledBlockUpdate{{Pos: cube.Pos{2, 40, 6}, Block: ridWater, Tick: 80}}

	first := newDBAt(t, dir)
	if err := first.StoreColumn(pos, dim, want); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newDBAt(t, dir)
	got, err := second.LoadColumn(pos, dim)
	if err != nil {
		t.Fatalf("load after reopen: %v", err)
	}
	assertColumnEq(t, want, got, dim.Range())
}

// TestLoadColumnNotFound fixes the documented contract of LoadColumn: a position that was never written
// reports leveldb.ErrNotFound through errors.Is.
func TestLoadColumnNotFound(t *testing.T) {
	db := newDB(t)
	for _, d := range dims {
		col, err := db.LoadColumn(world.ChunkPos{99, 99}, d.dim)
		if !errors.Is(err, leveldb.ErrNotFound) {
			t.Errorf("%v: err = %v, want leveldb.ErrNotFound", d.name, err)
		}
		if col != nil {
			t.Errorf("%v: column = %+v, want nil", d.name, col)
		}
	}
}

// TestStoreColumnWritesVersionAndFinalisation fixes the two single-value keys the store path always writes.
// Both are read by vanilla, and neither is derived from the Column.
func TestStoreColumnWritesVersionAndFinalisation(t *testing.T) {
	db := newDB(t)
	pos, dim := world.ChunkPos{0, 0}, world.Overworld
	k := dbKey{pos: pos, dim: dim}
	if err := db.StoreColumn(pos, dim, emptyColumn(dim)); err != nil {
		t.Fatalf("store: %v", err)
	}

	ver, ok := getRaw(t, db, k.Sum(keyVersion))
	if !ok {
		t.Fatal("keyVersion not written")
	}
	if !bytes.Equal(ver, []byte{chunkVersion}) {
		t.Errorf("version = %x, want %x", ver, []byte{chunkVersion})
	}
	fin, ok := getRaw(t, db, k.Sum(keyFinalisation))
	if !ok {
		t.Fatal("keyFinalisation not written")
	}
	if !bytes.Equal(fin, []byte{2, 0, 0, 0}) {
		t.Errorf("finalisation = %x, want 02000000 (finalisationPopulated, little endian)", fin)
	}
	// keyVersionOld is never written; vanilla only reads it for compatibility.
	if _, ok := getRaw(t, db, k.Sum(keyVersionOld)); ok {
		t.Error("keyVersionOld was written")
	}
}

// TestStoreColumnWritesEverySubChunk fixes that all sub chunks of the dimension's range get a key, empty ones
// included, and that the sub chunk index byte is the absolute Y >> 4 truncated to a byte.
func TestStoreColumnWritesEverySubChunk(t *testing.T) {
	for _, d := range dims {
		t.Run(d.name, func(t *testing.T) {
			db := newDB(t)
			pos := world.ChunkPos{0, 0}
			k := dbKey{pos: pos, dim: d.dim}
			if err := db.StoreColumn(pos, d.dim, emptyColumn(d.dim)); err != nil {
				t.Fatalf("store: %v", err)
			}
			r := d.dim.Range()
			n := (r.Height() >> 4) + 1
			for i := 0; i < n; i++ {
				y := byte(i + (r[0] >> 4))
				sub, ok := getRaw(t, db, k.Sum(keySubChunkData, y))
				if !ok {
					t.Fatalf("sub chunk %v (key byte %v) not written", i, y)
				}
				if len(sub) < 3 || sub[0] != chunk.SubChunkVersion || sub[2] != y {
					t.Errorf("sub chunk %v header = %x, want version %v and index %v", i, sub[:min(3, len(sub))], chunk.SubChunkVersion, y)
				}
			}
			// One past the top must not exist.
			if _, ok := getRaw(t, db, k.Sum(keySubChunkData, byte(n+(r[0]>>4)))); ok {
				t.Error("a sub chunk above the dimension range was written")
			}
		})
	}
}

// appendNBT encodes each map as a little-endian NBT compound and appends the results, which is the layout of
// keyEntitiesOld and keyBlockEntities.
func appendNBT(t *testing.T, maps ...map[string]any) []byte {
	t.Helper()
	buf := bytes.NewBuffer(nil)
	enc := nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian)
	for _, m := range maps {
		if err := enc.Encode(m); err != nil {
			t.Fatalf("encode nbt: %v", err)
		}
	}
	return buf.Bytes()
}
