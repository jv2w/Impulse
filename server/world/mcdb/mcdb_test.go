package mcdb

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// testBlock is one entry of testRegistry. Its index in testRegistry.blocks is its runtime ID.
type testBlock struct {
	name       string
	properties map[string]any
}

// testBlocks is the fixed block table shared by every test in this package. Runtime IDs are the indices, so
// air is 0 and the values below stay stable across runs, which is what golden comparisons need.
//
// Names are deliberately not minecraft:* so that blockupgrader.Upgrade, which disk decoding runs on every
// palette entry, leaves them untouched.
var testBlocks = []testBlock{
	{name: "impulse:air"},
	{name: "impulse:stone"},
	{name: "impulse:glass"},
	{name: "impulse:chest", properties: map[string]any{"facing": int32(2)}},
	{name: "impulse:torch", properties: map[string]any{"facing": int32(1)}},
	{name: "impulse:water", properties: map[string]any{"depth": int32(0)}},
}

// testRegistry is a deterministic world.BlockRegistry for the characterization tests.
//
// world.DefaultBlockRegistry is unusable here: it is a package global whose Finalize() is irreversible, so a
// test that used it would leak state into every later test in the process. See TestDefaultBlockRegistryGlobal
// for what the global costs.
//
// The embedded interface is nil on purpose. mcdb only ever uses the chunk.BlockRegistry subset plus
// Finalize; if that ever changes, the offending call panics with a nil dereference instead of passing
// silently.
type testRegistry struct {
	world.BlockRegistry

	blocks []testBlock
}

func newTestRegistry() *testRegistry { return &testRegistry{blocks: testBlocks} }

func (r *testRegistry) Finalize()            {}
func (r *testRegistry) BlockCount() int      { return len(r.blocks) }
func (r *testRegistry) AirRuntimeID() uint32 { return 0 }

func (r *testRegistry) RuntimeIDToState(rid uint32) (string, map[string]any, bool) {
	if int(rid) >= len(r.blocks) {
		return "", nil, false
	}
	b := r.blocks[rid]
	props := map[string]any{}
	maps.Copy(props, b.properties)
	return b.name, props, true
}

func (r *testRegistry) StateToRuntimeID(name string, properties map[string]any) (uint32, bool) {
	for rid, b := range r.blocks {
		if b.name != name || len(b.properties) != len(properties) {
			continue
		}
		if maps.Equal(propsOf(b.properties), propsOf(properties)) {
			return uint32(rid), true
		}
	}
	return 0, false
}

// propsOf renders property values as strings so map comparison does not depend on the concrete numeric type
// NBT decoding happened to produce.
func propsOf(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func (r *testRegistry) FilteringBlock(rid uint32) uint8 {
	if rid == 0 || rid == 2 || rid == 4 {
		return 0
	}
	return 15
}
func (r *testRegistry) LightBlock(rid uint32) uint8 {
	if rid == 4 {
		return 14
	}
	return 0
}
func (r *testRegistry) RandomTickBlock(uint32) bool           { return false }
func (r *testRegistry) NBTBlock(rid uint32) bool              { return rid == 3 }
func (r *testRegistry) LiquidDisplacingBlock(rid uint32) bool { return rid == 5 }
func (r *testRegistry) LiquidBlock(rid uint32) bool           { return rid == 5 }
func (r *testRegistry) HashToRuntimeID(hash uint32) (uint32, bool) {
	if int(hash) >= len(r.blocks) {
		return 0, false
	}
	return hash, true
}
func (r *testRegistry) RuntimeIDToHash(rid uint32) (uint32, bool) {
	if int(rid) >= len(r.blocks) {
		return 0, false
	}
	return rid, true
}

// discardLog is a logger that swallows everything. Several read paths in db.go log and continue instead of
// returning an error; without this the test output would be buried in those lines.
func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// newDB opens a DB on a fresh temporary directory using testRegistry. The DB is closed when the test ends.
// Closing it again inside the test is harmless.
func newDB(t *testing.T) *DB {
	t.Helper()
	return newDBAt(t, t.TempDir())
}

// newDBAt opens a DB on dir. Use it when a test needs to reopen the same world.
func newDBAt(t *testing.T, dir string) *DB {
	t.Helper()
	conf := Config{Log: discardLog(), Blocks: newTestRegistry()}
	db, err := conf.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		// Ignore the error: a test may have closed the DB already, and on Windows the temp dir cannot be
		// removed while leveldb still holds the files.
		_ = db.Close()
	})
	return db
}

// putRaw writes a raw leveldb key, bypassing every encoder in db.go. Corruption tests use it to produce
// values that the store path would never write.
func putRaw(t *testing.T, db *DB, key, value []byte) {
	t.Helper()
	if err := db.ldb.Put(key, value, nil); err != nil {
		t.Fatalf("put raw key %x: %v", key, err)
	}
}

// getRaw reads a raw leveldb key. ok is false if the key does not exist.
func getRaw(t *testing.T, db *DB, key []byte) (value []byte, ok bool) {
	t.Helper()
	v, err := db.ldb.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("get raw key %x: %v", key, err)
	}
	return v, true
}

// deleteRaw removes a raw leveldb key.
func deleteRaw(t *testing.T, db *DB, key []byte) {
	t.Helper()
	if err := db.ldb.Delete(key, nil); err != nil {
		t.Fatalf("delete raw key %x: %v", key, err)
	}
}

// storeLoad stores col at pos and reads it back.
func storeLoad(t *testing.T, db *DB, pos world.ChunkPos, col *chunk.Column) *chunk.Column {
	t.Helper()
	if err := db.StoreColumn(pos, col); err != nil {
		t.Fatalf("store column %v: %v", pos, err)
	}
	got, err := db.LoadColumn(pos)
	if err != nil {
		t.Fatalf("load column %v: %v", pos, err)
	}
	return got
}

// emptyColumn returns a Column holding an all-air chunk and nothing else.
func emptyColumn() *chunk.Column {
	return &chunk.Column{Chunk: chunk.New(newTestRegistry(), world.Range())}
}

// assertColumnEq compares got against want field by field.
//
// chunk.Chunk.Equals is deliberately not used: PalettedStorage.Equal returns false whenever the palette's
// first value is non-zero, so c.Equals(c) is false for any chunk with a block in it. That defect is pinned in
// the chunk package's own tests. Blocks and biomes are compared by walking coordinates instead.
func assertColumnEq(t *testing.T, want, got *chunk.Column, r cube.Range) {
	t.Helper()
	assertChunkEq(t, want.Chunk, got.Chunk, r)
	assertEntitiesEq(t, want.Entities, got.Entities)
	assertBlockEntitiesEq(t, want.BlockEntities, got.BlockEntities)
	if want.Tick != got.Tick {
		t.Errorf("tick: want %v, got %v", want.Tick, got.Tick)
	}
	assertScheduledEq(t, want.ScheduledBlocks, got.ScheduledBlocks)
}

// assertChunkEq compares every block (layer 0 and 1) and every biome in r.
func assertChunkEq(t *testing.T, want, got *chunk.Chunk, r cube.Range) {
	t.Helper()
	if want.Range() != got.Range() {
		t.Fatalf("range: want %v, got %v", want.Range(), got.Range())
	}
	blockDiffs, biomeDiffs := 0, 0
	for y := int16(r[0]); int(y) <= r[1]; y++ {
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				for layer := uint8(0); layer < 2; layer++ {
					w, g := want.Block(x, y, z, layer), got.Block(x, y, z, layer)
					if w != g && blockDiffs < 8 {
						blockDiffs++
						t.Errorf("block (%v,%v,%v) layer %v: want rid %v, got %v", x, y, z, layer, w, g)
					}
				}
				if w, g := want.Biome(x, y, z), got.Biome(x, y, z); w != g && biomeDiffs < 8 {
					biomeDiffs++
					t.Errorf("biome (%v,%v,%v): want %v, got %v", x, y, z, w, g)
				}
			}
		}
	}
}

func assertEntitiesEq(t *testing.T, want, got []chunk.Entity) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("entities: want %v, got %v (%+v)", len(want), len(got), got)
	}
	for i := range want {
		if want[i].ID != got[i].ID {
			t.Errorf("entity %v: id want %v, got %v", i, want[i].ID, got[i].ID)
		}
		if !reflect.DeepEqual(want[i].Data, got[i].Data) {
			t.Errorf("entity %v data: want %+v, got %+v", i, want[i].Data, got[i].Data)
		}
	}
}

func assertBlockEntitiesEq(t *testing.T, want, got []chunk.BlockEntity) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("block entities: want %v, got %v (%+v)", len(want), len(got), got)
	}
	for i := range want {
		if want[i].Pos != got[i].Pos {
			t.Errorf("block entity %v: pos want %v, got %v", i, want[i].Pos, got[i].Pos)
		}
		if !reflect.DeepEqual(want[i].Data, got[i].Data) {
			t.Errorf("block entity %v data: want %+v, got %+v", i, want[i].Data, got[i].Data)
		}
	}
}

func assertScheduledEq(t *testing.T, want, got []chunk.ScheduledBlockUpdate) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("scheduled updates: want %v, got %v (%+v)", len(want), len(got), got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("scheduled update %v: want %+v, got %+v", i, want[i], got[i])
		}
	}
}
