package mcdb

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// corruptPos is the position every corruption test writes to. It is arbitrary but non-zero and negative on
// one axis so that a key built the wrong way round does not accidentally hit the right record.
var corruptPos = world.ChunkPos{1, -2}

// seedColumn stores a plain column at corruptPos so that every key the load path reads exists and is valid.
// Individual tests then damage one key and observe the result.
func seedColumn(t *testing.T, db *DB) dbKey {
	t.Helper()
	k := dbKey{pos: corruptPos, dim: world.Overworld}
	col := emptyColumn(k.dim)
	col.Chunk.SetBlock(1, 2, 3, 0, ridStone)
	if err := db.StoreColumn(k.pos, k.dim, col); err != nil {
		t.Fatalf("seed column: %v", err)
	}
	return k
}

// recovered runs fn and returns the value it panicked with, or nil if it returned normally.
func recovered(fn func()) (v any) {
	defer func() { v = recover() }()
	fn()
	return nil
}

func TestLoadColumnCorruptVersion(t *testing.T) {
	cases := []struct {
		name    string
		value   []byte
		wantErr string
	}{
		{name: "empty", value: []byte{}, wantErr: "expected 1 version byte, got 0"},
		{name: "two bytes", value: []byte{42, 42}, wantErr: "expected 1 version byte, got 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			k := seedColumn(t, db)
			putRaw(t, db, k.Sum(keyVersion), c.value)

			_, err := db.LoadColumn(k.pos, k.dim)
			if err == nil {
				t.Fatal("load succeeded")
			}
			// version() builds this with fmt.Errorf and exposes no sentinel, so a caller cannot branch on
			// it. Matching the text is the only option available.
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestLoadColumnUnknownVersionLoadsAnyway fixes that an unrecognised chunk version is only logged, never
// refused. A world written by a future Bedrock version is decoded with the current format regardless.
func TestLoadColumnUnknownVersionLoadsAnyway(t *testing.T) {
	for _, ver := range []byte{0, 1, 41, 43, 255} {
		db := newDB(t)
		k := seedColumn(t, db)
		putRaw(t, db, k.Sum(keyVersion), []byte{ver})

		col, err := db.LoadColumn(k.pos, k.dim)
		if err != nil {
			t.Fatalf("version %v: load: %v", ver, err)
		}
		if got := col.Chunk.Block(1, 2, 3, 0); got != ridStone {
			t.Errorf("version %v: block = %v, want %v", ver, got, ridStone)
		}
	}
}

// TestLoadColumnFallsBackToVersionOld fixes the keyVersion -> keyVersionOld fallback that lets pre-1.16
// worlds load.
func TestLoadColumnFallsBackToVersionOld(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)
	deleteRaw(t, db, k.Sum(keyVersion))

	if _, err := db.LoadColumn(k.pos, k.dim); !errors.Is(err, leveldb.ErrNotFound) {
		t.Fatalf("without either version key: err = %v, want leveldb.ErrNotFound", err)
	}
	putRaw(t, db, k.Sum(keyVersionOld), []byte{chunkVersion})
	col, err := db.LoadColumn(k.pos, k.dim)
	if err != nil {
		t.Fatalf("with keyVersionOld only: %v", err)
	}
	if got := col.Chunk.Block(1, 2, 3, 0); got != ridStone {
		t.Errorf("block = %v, want %v", got, ridStone)
	}
}

func TestLoadColumnCorruptBiomes(t *testing.T) {
	cases := []struct {
		name  string
		value []byte
		// wantOK is true when the damage is tolerated and the column still loads.
		wantOK  bool
		wantErr string
	}{
		{
			name:    "empty",
			value:   []byte{},
			wantErr: "expected at least 513 bytes for 3D data, got 0",
		},
		{
			name:    "heightmap only",
			value:   make([]byte, 512),
			wantErr: "expected at least 513 bytes for 3D data, got 512",
		},
		{
			name:  "one byte of biome data",
			value: make([]byte, 513),
			// A 0x00 header declares a single-entry palette, and the four bytes of that entry are missing.
			wantErr: "EOF",
		},
		{
			name:    "truncated mid storage",
			value:   append(make([]byte, 512), 0x02, 0x00, 0x00),
			wantErr: "not enough block data present",
		},
		{
			name:    "non canonical palette size",
			value:   append(make([]byte, 512), 0x07<<1),
			wantErr: "invalid palette size",
		},
		{
			name:    "first storage points at the previous one",
			value:   append(make([]byte, 512), 0x7f<<1),
			wantErr: "first biome storage pointed to previous one",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			k := seedColumn(t, db)
			putRaw(t, db, k.Sum(key3DData), c.value)

			_, err := db.LoadColumn(k.pos, k.dim)
			if c.wantOK {
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("load succeeded")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestLoadColumnMissingBiomesLoadsDefaults fixes the one biome failure that is tolerated: a chunk with no
// key3DData at all (a pre-height-change world) loads with biome 0 everywhere.
func TestLoadColumnMissingBiomesLoadsDefaults(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)
	deleteRaw(t, db, k.Sum(key3DData))

	col, err := db.LoadColumn(k.pos, k.dim)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := col.Chunk.Biome(0, 0, 0); got != 0 {
		t.Errorf("biome = %v, want 0", got)
	}
	if got := col.Chunk.Block(1, 2, 3, 0); got != ridStone {
		t.Errorf("block = %v, want %v", got, ridStone)
	}
}

func TestLoadColumnCorruptSubChunk(t *testing.T) {
	// damage receives the sub chunk bytes the store path wrote and returns what to put back.
	cases := []struct {
		name    string
		damage  func(sub []byte) []byte
		wantErr string
	}{
		{
			name:    "empty value",
			damage:  func([]byte) []byte { return []byte{} },
			wantErr: "",
		},
		{
			name:    "unknown sub chunk version",
			damage:  func(sub []byte) []byte { return append([]byte{7}, sub[1:]...) },
			wantErr: "unknown sub chunk version 7",
		},
		{
			name:    "version byte only",
			damage:  func(sub []byte) []byte { return sub[:1] },
			wantErr: "error reading storage count",
		},
		{
			name:    "truncated before the index byte",
			damage:  func(sub []byte) []byte { return sub[:2] },
			wantErr: "error reading sub-chunk index",
		},
		{
			name:    "truncated block data",
			damage:  func(sub []byte) []byte { return sub[:5] },
			wantErr: "not enough block data present",
		},
		{
			name: "non canonical palette size",
			damage: func(sub []byte) []byte {
				out := bytes.Clone(sub)
				out[3] = 0x07 << 1
				return out
			},
			wantErr: "invalid palette size",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			k := seedColumn(t, db)
			// The sub chunk holding y=2 for the overworld range is index byte 0.
			key := k.Sum(keySubChunkData, 0)
			sub, ok := getRaw(t, db, key)
			if !ok {
				t.Fatal("sub chunk 0 not written")
			}
			putRaw(t, db, key, c.damage(sub))

			col, err := db.LoadColumn(k.pos, k.dim)
			if c.wantErr == "" {
				// An empty value means "no data for this sub chunk", so the column loads without the block.
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				if got := col.Chunk.Block(1, 2, 3, 0); got != ridAir {
					t.Errorf("block = %v, want air; the damaged sub chunk was still applied at y=2", got)
				}
				return
			}
			if err == nil {
				t.Fatal("load succeeded")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestLoadColumnSubChunkIndexByte covers the version-9 sub chunk header, whose third byte carries the
// absolute Y of the sub chunk. The loader trusts it over the key it read the value from.
func TestLoadColumnSubChunkIndexByte(t *testing.T) {
	// The block seeded at y=2 lives in sub chunk index 4 for the overworld, stored under key byte 0.
	t.Run("in range index relocates the data silently", func(t *testing.T) {
		db := newDB(t)
		k := seedColumn(t, db)
		key := k.Sum(keySubChunkData, 0)
		sub, ok := getRaw(t, db, key)
		if !ok {
			t.Fatal("sub chunk 0 not written")
		}
		moved := bytes.Clone(sub)
		// int8(0xff) - int8(-64>>4) = 3, so the sub chunk lands at index 3, covering y -16..-1. A target
		// below the source matters: DiskDecode walks the sub chunks in order, so a higher target would be
		// overwritten again by the sub chunk that legitimately owns it.
		moved[2] = 0xff
		putRaw(t, db, key, moved)

		col, err := db.LoadColumn(k.pos, k.dim)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := col.Chunk.Block(1, 2, 3, 0); got != ridAir {
			t.Errorf("block at y=2 = %v, want air", got)
		}
		if got := col.Chunk.Block(1, -14, 3, 0); got != ridStone {
			t.Errorf("block at y=-14 = %v, want %v; the header index no longer overrides the key", got, ridStone)
		}
	})

	// A single damaged byte in a world save must not take down the goroutine running the load, so the disk
	// path validates the translated index exactly as NetworkDecodeBuffer does.
	t.Run("out of range index is an error", func(t *testing.T) {
		db := newDB(t)
		k := seedColumn(t, db)
		key := k.Sum(keySubChunkData, 0)
		sub, ok := getRaw(t, db, key)
		if !ok {
			t.Fatal("sub chunk 0 not written")
		}
		broken := bytes.Clone(sub)
		broken[2] = 200
		putRaw(t, db, key, broken)

		var err error
		if v := recovered(func() { _, err = db.LoadColumn(k.pos, k.dim) }); v != nil {
			t.Fatalf("LoadColumn panicked with %v, want an error", v)
		}
		if err == nil {
			t.Fatal("load succeeded")
		}
		if !strings.Contains(err.Error(), "invalid sub-chunk index 204") {
			t.Errorf("err = %v, want it to contain \"invalid sub-chunk index 204\"", err)
		}
	})
}

// TestLoadColumnTruncatedEntityIndexIsReported covers a digp value whose length is not a multiple of 8.
// `ids[i : i+8]` is bounds-checked against capacity rather than length, so walking such a value reads
// whatever the allocator left after it and fabricates entity IDs out of it. The length must be checked
// before the walk, and the damage reported rather than turned into missing entities.
func TestLoadColumnTruncatedEntityIndexIsReported(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)

	// An ID whose high bytes are non-zero: a five-byte prefix cannot reconstruct it whatever follows in
	// memory, so the outcome does not depend on what the allocator left there.
	const id = int64(1) << 40
	col := emptyColumn(k.dim)
	col.Entities = []chunk.Entity{{ID: id, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": id}}}
	if err := db.StoreColumn(k.pos, k.dim, col); err != nil {
		t.Fatalf("store: %v", err)
	}

	idsKey := append([]byte(keyEntityIdentifiers), index(k.pos, k.dim)...)
	full, ok := getRaw(t, db, idsKey)
	if !ok {
		t.Fatal("digp key not written")
	}
	putRaw(t, db, idsKey, full[:5])

	_, err := db.LoadColumn(k.pos, k.dim)
	if err == nil {
		t.Fatal("load succeeded")
	}
	if !strings.Contains(err.Error(), "chunk entity index has 5 bytes") {
		t.Errorf("err = %v, want it to contain \"chunk entity index has 5 bytes\"", err)
	}
	if _, ok := getRaw(t, db, entityIndex(id)); !ok {
		t.Error("the entity record was removed by the failed load")
	}
}

// TestStoreColumnTruncatedEntityIndexIsReported is the write side of the same value. storeEntities feeds
// `digpPrev[i:]` to binary.LittleEndian.Uint64, which is indexed by length and panics on a short tail. The
// store must report the damaged index instead, and must not write a partial column over it.
func TestStoreColumnTruncatedEntityIndexIsReported(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)
	idsKey := append([]byte(keyEntityIdentifiers), index(k.pos, k.dim)...)
	putRaw(t, db, idsKey, make([]byte, 4))

	var err error
	if v := recovered(func() { err = db.StoreColumn(k.pos, k.dim, emptyColumn(k.dim)) }); v != nil {
		t.Fatalf("StoreColumn panicked with %v, want an error", v)
	}
	if err == nil {
		t.Fatal("store succeeded")
	}
	if !strings.Contains(err.Error(), "chunk entity index has 4 bytes") {
		t.Errorf("err = %v, want it to contain \"chunk entity index has 4 bytes\"", err)
	}
	// The batch is written in one go at the end of storeColumn, so a rejected index leaves the column on
	// disk exactly as it was.
	if got, ok := getRaw(t, db, idsKey); !ok || len(got) != 4 {
		t.Errorf("digp value = %v, %v, want the untouched 4 bytes", got, ok)
	}
}

// TestLoadColumnDanglingEntityReferenceDropsEveryEntity is a PINNED BUG with a data-loss path attached. When
// the digp index names an entity whose actorprefix record is gone, entities() returns leveldb.ErrNotFound.
// column() treats that as "this chunk has no entities" and drops the entities it had already decoded. A
// subsequent store then deletes their records for good.
func TestLoadColumnDanglingEntityReferenceDropsEveryEntity(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)

	col := emptyColumn(k.dim)
	col.Entities = []chunk.Entity{
		{ID: 1, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(1)}},
		{ID: 2, Data: map[string]any{"identifier": "minecraft:cow", "UniqueID": int64(2)}},
	}
	if err := db.StoreColumn(k.pos, k.dim, col); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Lose one of the two records, as a crash between two batches or a chunk hand-off would.
	deleteRaw(t, db, entityIndex(2))

	got, err := db.LoadColumn(k.pos, k.dim)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entities) != 0 {
		t.Fatalf("entities = %+v, want none; the surviving entity is no longer dropped", got.Entities)
	}
	if _, ok := getRaw(t, db, entityIndex(1)); !ok {
		t.Fatal("entity 1 disappeared before the store")
	}

	// The load/store cycle a world does on unload now destroys the survivor.
	if err := db.StoreColumn(k.pos, k.dim, got); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, ok := getRaw(t, db, entityIndex(1)); ok {
		t.Fatal("entity 1 survived the store; the data-loss path is closed")
	}
}

// TestLoadColumnCorruptEntityNBTKeepsEmptyEntity is a PINNED BUG. A record that fails to decode is logged and
// then appended anyway with an empty Data map, so the entity is silently reduced to its ID. Storing the
// column back writes that empty map over the record.
func TestLoadColumnCorruptEntityNBTKeepsEmptyEntity(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)

	col := emptyColumn(k.dim)
	col.Entities = []chunk.Entity{{ID: 3, Data: map[string]any{"identifier": "minecraft:pig", "UniqueID": int64(3)}}}
	if err := db.StoreColumn(k.pos, k.dim, col); err != nil {
		t.Fatalf("store: %v", err)
	}
	putRaw(t, db, entityIndex(3), []byte{0xff, 0xff, 0xff})

	got, err := db.LoadColumn(k.pos, k.dim)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("entities = %+v, want 1; an undecodable entity is no longer kept", got.Entities)
	}
	if got.Entities[0].ID != 3 {
		t.Errorf("id = %v, want 3", got.Entities[0].ID)
	}
	if len(got.Entities[0].Data) != 0 {
		t.Errorf("data = %+v, want empty", got.Entities[0].Data)
	}
}

// TestLoadColumnCorruptOldFormatEntities fixes that the old format does report a decode failure, unlike the
// new one.
func TestLoadColumnCorruptOldFormatEntities(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)
	deleteRaw(t, db, append([]byte(keyEntityIdentifiers), index(k.pos, k.dim)...))
	putRaw(t, db, k.Sum(keyEntitiesOld), []byte{0x0a, 0x00, 0x00})

	_, err := db.LoadColumn(k.pos, k.dim)
	if err == nil {
		t.Fatal("load succeeded")
	}
	if !strings.Contains(err.Error(), "decode entity nbt") {
		t.Errorf("err = %v, want it to contain \"decode entity nbt\"", err)
	}
}

func TestLoadColumnCorruptBlockEntities(t *testing.T) {
	cases := []struct {
		name    string
		value   []byte
		wantErr string
	}{
		{name: "garbage", value: []byte{0xff, 0xff, 0xff, 0xff}, wantErr: "decode nbt"},
		{name: "truncated compound", value: []byte{0x0a, 0x00, 0x00}, wantErr: "decode nbt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			k := seedColumn(t, db)
			putRaw(t, db, k.Sum(keyBlockEntities), c.value)

			_, err := db.LoadColumn(k.pos, k.dim)
			if err == nil {
				t.Fatal("load succeeded")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestLoadColumnBlockEntityWithoutCoordinates fixes what blockPosFromNBT does with a block entity that has no
// usable x/y/z: the position becomes the origin and no error is raised.
func TestLoadColumnBlockEntityWithoutCoordinates(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
	}{
		{name: "missing", data: map[string]any{"id": "Chest"}},
		{name: "wrong types", data: map[string]any{"id": "Chest", "x": int64(5), "y": "6", "z": int16(7)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			k := seedColumn(t, db)
			putRaw(t, db, k.Sum(keyBlockEntities), appendNBT(t, c.data))

			col, err := db.LoadColumn(k.pos, k.dim)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(col.BlockEntities) != 1 {
				t.Fatalf("block entities: want 1, got %v", len(col.BlockEntities))
			}
			if p := col.BlockEntities[0].Pos; p != (cube.Pos{0, 0, 0}) {
				t.Errorf("pos = %v, want the origin", p)
			}
		})
	}
}

func TestLoadColumnCorruptScheduledTicks(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)
	putRaw(t, db, k.Sum(keyPendingScheduledTicks), []byte{0xff, 0xff, 0xff, 0xff})

	_, err := db.LoadColumn(k.pos, k.dim)
	if err == nil {
		t.Fatal("load succeeded")
	}
	if !strings.Contains(err.Error(), "read nbt") {
		t.Errorf("err = %v, want it to contain \"read nbt\"", err)
	}
}

// TestLoadColumnUndecodableScheduledTickBecomesZeroValue is a PINNED BUG. scheduledUpdates allocates the
// result slice up front and only `continue`s when a block state fails to decode, so the failed entry stays in
// the slice as a zero value: a scheduled update on air at the chunk origin, at tick 0. The failure is invisible
// to the caller and the fabricated update is written back on the next store.
func TestLoadColumnUndecodableScheduledTickBecomesZeroValue(t *testing.T) {
	cases := []struct {
		name  string
		state any
	}{
		{name: "unknown block name", state: map[string]any{
			"name": "impulse:does-not-exist", "states": map[string]any{}, "version": chunk.CurrentBlockVersion,
		}},
		{name: "missing blockState", state: nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			k := seedColumn(t, db)

			bad := map[string]any{"x": int32(9), "y": int32(9), "z": int32(9), "time": int64(99)}
			if c.state != nil {
				bad["blockState"] = c.state
			}
			good := map[string]any{
				"x": int32(1), "y": int32(2), "z": int32(3), "time": int64(50),
				"blockState": map[string]any{
					"name": "impulse:stone", "states": map[string]any{}, "version": chunk.CurrentBlockVersion,
				},
			}
			data, err := nbt.MarshalEncoding(scheduledUpdates{
				CurrentTick: 40, TickList: []map[string]any{bad, good},
			}, nbt.LittleEndian)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			putRaw(t, db, k.Sum(keyPendingScheduledTicks), data)

			col, err := db.LoadColumn(k.pos, k.dim)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(col.ScheduledBlocks) != 2 {
				t.Fatalf("scheduled blocks = %+v, want 2 entries", col.ScheduledBlocks)
			}
			want := chunk.ScheduledBlockUpdate{}
			if col.ScheduledBlocks[0] != want {
				t.Errorf("entry 0 = %+v, want the zero value; failed entries are no longer left in place", col.ScheduledBlocks[0])
			}
			if got := (chunk.ScheduledBlockUpdate{Pos: cube.Pos{1, 2, 3}, Block: ridStone, Tick: 50}); col.ScheduledBlocks[1] != got {
				t.Errorf("entry 1 = %+v, want %+v", col.ScheduledBlocks[1], got)
			}
		})
	}
}

// TestLoadColumnScheduledTickWithWrongFieldTypes fixes that the tick and position fields are read with
// unchecked-value type assertions, so a value of the wrong NBT type silently becomes zero.
func TestLoadColumnScheduledTickWithWrongFieldTypes(t *testing.T) {
	db := newDB(t)
	k := seedColumn(t, db)

	data, err := nbt.MarshalEncoding(scheduledUpdates{
		CurrentTick: 40,
		TickList: []map[string]any{{
			// time as an int32 rather than an int64, x as a string.
			"x": "1", "y": int32(2), "z": int32(3), "time": int32(50),
			"blockState": map[string]any{
				"name": "impulse:stone", "states": map[string]any{}, "version": chunk.CurrentBlockVersion,
			},
		}},
	}, nbt.LittleEndian)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	putRaw(t, db, k.Sum(keyPendingScheduledTicks), data)

	col, err := db.LoadColumn(k.pos, k.dim)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := chunk.ScheduledBlockUpdate{Pos: cube.Pos{0, 2, 3}, Block: ridStone, Tick: 0}
	if len(col.ScheduledBlocks) != 1 || col.ScheduledBlocks[0] != want {
		t.Fatalf("scheduled blocks = %+v, want [%+v]", col.ScheduledBlocks, want)
	}
}

// TestLoadColumnVersionKeyOnly fixes the minimum a chunk needs to load: the version key alone yields an empty
// column rather than an error, because every other key is optional.
func TestLoadColumnVersionKeyOnly(t *testing.T) {
	db := newDB(t)
	k := dbKey{pos: corruptPos, dim: world.Overworld}
	putRaw(t, db, k.Sum(keyVersion), []byte{chunkVersion})

	col, err := db.LoadColumn(k.pos, k.dim)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertColumnEq(t, emptyColumn(k.dim), col, k.dim.Range())
}

// TestLoadColumnSubChunkKeyBytesWrap fixes the sub chunk index byte. subChunks() derives it from
// uint8(i + r[0]>>4), which wraps: the overworld's lowest sub chunk is byte 252 while the nether's is 0.
// Only the dimension ID in the index prefix keeps the two apart.
func TestLoadColumnSubChunkKeyBytesWrap(t *testing.T) {
	db := newDB(t)
	k := dbKey{pos: corruptPos, dim: world.Overworld}
	if err := db.StoreColumn(k.pos, k.dim, emptyColumn(k.dim)); err != nil {
		t.Fatalf("store: %v", err)
	}
	// The overworld range starts at -64, so the first sub chunk is stored under byte 0xfc, not 0x00.
	if _, ok := getRaw(t, db, k.Sum(keySubChunkData, 0xfc)); !ok {
		t.Error("lowest overworld sub chunk not stored under key byte 0xfc")
	}
	nether := dbKey{pos: corruptPos, dim: world.Nether}
	if err := db.StoreColumn(nether.pos, nether.dim, emptyColumn(nether.dim)); err != nil {
		t.Fatalf("store nether: %v", err)
	}
	if _, ok := getRaw(t, db, nether.Sum(keySubChunkData, 0x00)); !ok {
		t.Error("lowest nether sub chunk not stored under key byte 0x00")
	}
	// The two are only kept apart by the dimension ID in the index prefix.
	if bytes.Equal(k.Sum(keySubChunkData, 0), nether.Sum(keySubChunkData, 0)) {
		t.Error("overworld and nether sub chunk keys collide")
	}
}

// TestVersionKeyLengthsMatchIteratorAssumption fixes the invariant ColumnIterator relies on: a version key is
// 9 bytes in the overworld and 13 bytes elsewhere, and nothing else the store path writes has that shape with
// a version byte at the end.
func TestVersionKeyLengthsMatchIteratorAssumption(t *testing.T) {
	for _, d := range dims {
		k := dbKey{pos: world.ChunkPos{1, 2}, dim: d.dim}
		want := 9
		if d.dim != world.Dimension(world.Overworld) {
			want = 13
		}
		if got := len(k.Sum(keyVersion)); got != want {
			t.Errorf("%v: len(version key) = %v, want %v", d.name, got, want)
		}
		if got := len(k.Sum(keySubChunkData, 0)); got == want {
			t.Errorf("%v: sub chunk key is also %v bytes long", d.name, got)
		}
	}
}

// errString renders a recovered panic value for comparison.
func errString(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
