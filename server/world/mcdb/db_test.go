package mcdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/mcdb/leveldat"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// TestConfigDefaults fixes what Config.Open fills in for a zero Config. The leveldb block size in particular
// is a stored-format decision, not a runtime tuning knob.
func TestConfigDefaults(t *testing.T) {
	var conf Config
	db, err := conf.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if db.conf.Log == nil {
		t.Error("Log is nil")
	}
	if db.conf.LDBOptions == nil {
		t.Fatal("LDBOptions is nil")
	}
	if got := db.conf.LDBOptions.BlockSize; got != 16*opt.KiB {
		t.Errorf("BlockSize = %v, want %v", got, 16*opt.KiB)
	}
	if db.conf.Blocks != world.BlockRegistry(world.DefaultBlockRegistry) {
		t.Error("Blocks is not DefaultBlockRegistry")
	}
	// Open must not overwrite an explicitly chosen block size.
	conf2 := Config{LDBOptions: &opt.Options{BlockSize: 4 * opt.KiB}, Blocks: newTestRegistry()}
	db2, err := conf2.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if got := db2.conf.LDBOptions.BlockSize; got != 4*opt.KiB {
		t.Errorf("BlockSize = %v, want %v", got, 4*opt.KiB)
	}
}

// TestOpenCreatesDirectories fixes that Open creates the leveldb directory itself and that level.dat and
// levelname.txt appear only once the world is closed.
func TestOpenCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	db := newDBAt(t, dir)

	if _, err := os.Stat(filepath.Join(dir, "db")); err != nil {
		t.Errorf("db directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "level.dat")); !os.IsNotExist(err) {
		t.Errorf("level.dat exists before Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, name := range []string{"level.dat", "levelname.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%v after Close: %v", name, err)
		}
	}
	name, err := os.ReadFile(filepath.Join(dir, "levelname.txt"))
	if err != nil {
		t.Fatalf("read levelname.txt: %v", err)
	}
	if got := string(name); got != db.Settings().Name {
		t.Errorf("levelname.txt = %q, want %q", got, db.Settings().Name)
	}
}

// TestOpenRejectsNewerLevelDat fixes the version gate: a level.dat newer than leveldat.Version is refused,
// while an older one is accepted and upgraded on the next Close.
func TestOpenRejectsNewerLevelDat(t *testing.T) {
	cases := []struct {
		name    string
		ver     int32
		wantErr bool
	}{
		{name: "current", ver: leveldat.Version},
		{name: "older", ver: 9},
		{name: "zero", ver: 0},
		{name: "newer", ver: leveldat.Version + 1, wantErr: true},
		{name: "far newer", ver: 99, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLevelDat(t, dir, c.ver)

			conf := Config{Log: discardLog(), Blocks: newTestRegistry()}
			db, err := conf.Open(dir)
			if db != nil {
				t.Cleanup(func() { _ = db.Close() })
			}
			if c.wantErr {
				if err == nil {
					t.Fatal("open succeeded")
				}
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
		})
	}
}

// writeLevelDat writes a minimal level.dat with the storage version passed.
func writeLevelDat(t *testing.T, dir string, ver int32) {
	t.Helper()
	var d leveldat.Data
	d.FillDefault()
	data, err := nbt.MarshalEncoding(d, nbt.LittleEndian)
	if err != nil {
		t.Fatalf("marshal level.dat: %v", err)
	}
	buf := bytes.NewBuffer(nil)
	if err := binary.Write(buf, binary.LittleEndian, struct{ StorageVersion, FileLength int32 }{ver, int32(len(data))}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	buf.Write(data)
	if err := os.WriteFile(filepath.Join(dir, "level.dat"), buf.Bytes(), 0644); err != nil {
		t.Fatalf("write level.dat: %v", err)
	}
}

// PINNED BUG: Settings hands out the *world.Settings the DB holds,
// so a caller can change the DB's view of the world by writing to the result. The change is not written to
// level.dat, because Close marshals leveldat.Data and only SaveSettings copies into that. The two therefore
// drift apart without any call reporting a problem.
func TestSettingsReturnsTheInternalPointer(t *testing.T) {
	dir := t.TempDir()
	db := newDBAt(t, dir)

	if db.Settings() != db.Settings() {
		t.Fatal("Settings returned two different pointers")
	}
	original := db.Settings().Name
	db.Settings().Name = "mutated"
	if got := db.Settings().Name; got != "mutated" {
		t.Fatalf("Name = %q; Settings no longer aliases the DB's own value", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened := newDBAt(t, dir)
	if got := reopened.Settings().Name; got != original {
		t.Errorf("Name after reopen = %q, want %q; a mutation without SaveSettings is now persisted", got, original)
	}
}

func TestSaveSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db := newDBAt(t, dir)

	s := db.Settings()
	s.Name = "impulse test world"
	s.Spawn = cube.Pos{10, 70, -20}
	s.Time = 4321
	s.TimeCycle = false
	s.WeatherCycle = false
	s.Raining = true
	s.RainTime = 55
	s.Thundering = true
	s.ThunderTime = 66
	s.CurrentTick = 7777
	s.TickRange = 5
	db.SaveSettings(s)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got := newDBAt(t, dir).Settings()
	cases := []struct {
		field     string
		got, want any
	}{
		{"Name", got.Name, "impulse test world"},
		{"Spawn", got.Spawn, cube.Pos{10, 70, -20}},
		{"Time", got.Time, int64(4321)},
		{"TimeCycle", got.TimeCycle, false},
		{"WeatherCycle", got.WeatherCycle, false},
		{"Raining", got.Raining, true},
		{"RainTime", got.RainTime, int64(55)},
		{"Thundering", got.Thundering, true},
		{"ThunderTime", got.ThunderTime, int64(66)},
		{"CurrentTick", got.CurrentTick, int64(7777)},
		{"TickRange", got.TickRange, int32(5)},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%v = %v, want %v", c.field, c.got, c.want)
		}
	}
}

// PINNED BUG: SaveSettings writes into leveldat.Data only, so the
// value Settings returns is not refreshed. Passing a different *world.Settings leaves the DB reporting the
// old one until the world is reopened.
func TestSaveSettingsDoesNotUpdateSettings(t *testing.T) {
	db := newDB(t)
	other := &world.Settings{Name: "other"}
	db.SaveSettings(other)

	if got := db.Settings().Name; got == "other" {
		t.Fatal("Settings reflects the value passed to SaveSettings; the two are now kept in sync")
	}
	if db.Settings() == other {
		t.Fatal("Settings returned the value passed to SaveSettings")
	}
}

// TestSetBlockRegistryNilInstallsTheGlobal fixes that passing nil selects world.DefaultBlockRegistry and
// finalizes it. Finalizing is irreversible and the registry is a package global, so this call permanently
// changes process-wide state: after it, no further block or block state can be registered anywhere.
func TestSetBlockRegistryNilInstallsTheGlobal(t *testing.T) {
	db := newDB(t)
	db.SetBlockRegistry(nil)

	if db.conf.Blocks != world.BlockRegistry(world.DefaultBlockRegistry) {
		t.Fatal("Blocks is not DefaultBlockRegistry")
	}
	// Finalize has run, so the build phase of the global registry is over for the rest of the process.
	v := recovered(func() { world.DefaultBlockRegistry.RegisterBlockState(world.BlockState{Name: "impulse:late"}) })
	if v == nil {
		t.Fatal("RegisterBlockState succeeded after SetBlockRegistry(nil); the registry is no longer finalized")
	}
}

// PINNED BUG: A Chunk keeps the registry it was decoded
// with, while scheduled updates are encoded with whatever registry the DB holds at store time. Swapping the
// registry between a load and a store therefore writes the two halves of one column with different runtime ID
// spaces.
func TestSetBlockRegistryDoesNotReachLoadedChunks(t *testing.T) {
	db := newDB(t)
	pos := world.ChunkPos{0, 0}

	col := emptyColumn()
	col.Chunk.SetBlock(0, 0, 0, 0, ridStone)
	if err := db.StoreColumn(pos, col); err != nil {
		t.Fatalf("store: %v", err)
	}
	loaded, err := db.LoadColumn(pos)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// A registry where the same name maps to a different runtime ID.
	shifted := &testRegistry{blocks: append([]testBlock{{name: "impulse:filler"}}, testBlocks...)}
	db.SetBlockRegistry(shifted)

	if err := db.StoreColumn(pos, loaded); err != nil {
		t.Fatalf("store after swap: %v", err)
	}
	again, err := db.LoadColumn(pos)
	if err != nil {
		t.Fatalf("load after swap: %v", err)
	}
	// The chunk was re-encoded with its own registry (names unchanged) but decoded with the new one, so the
	// runtime ID shifted by one. The block identity survived; the numeric ID did not.
	if got := again.Chunk.Block(0, 0, 0, 0); got != ridStone+1 {
		t.Fatalf("block = %v, want %v; the chunk no longer carries its own registry across a swap", got, ridStone+1)
	}
}

func TestCloseTwice(t *testing.T) {
	db := newDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := db.Close(); !errors.Is(err, leveldb.ErrClosed) {
		t.Errorf("second close: err = %v, want leveldb.ErrClosed", err)
	}
}

// TestMethodsAfterClose fixes that every entry point reports the closed database instead of panicking or
// pretending to succeed.
func TestMethodsAfterClose(t *testing.T) {
	db := newDB(t)
	pos := world.ChunkPos{0, 0}
	if err := db.StoreColumn(pos, emptyColumn()); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := db.LoadColumn(pos); !errors.Is(err, leveldb.ErrClosed) {
		t.Errorf("LoadColumn: err = %v, want leveldb.ErrClosed", err)
	}
	if err := db.StoreColumn(pos, emptyColumn()); !errors.Is(err, leveldb.ErrClosed) {
		t.Errorf("StoreColumn: err = %v, want leveldb.ErrClosed", err)
	}
	iter := db.NewColumnIterator(nil)
	if iter.Next() {
		t.Error("iterator yielded a column after Close")
	}
	iter.Release()

	// Settings is served from memory and keeps working.
	if db.Settings() == nil {
		t.Error("Settings returned nil after Close")
	}
}

// TestStoreColumnOnClosedDBWritesNothing fixes the atomicity of the store batch: the whole column is written
// by a single leveldb.Batch, so a failing write leaves no partial chunk behind.
func TestStoreColumnOnClosedDBWritesNothing(t *testing.T) {
	dir := t.TempDir()
	pos := world.ChunkPos{6, 6}

	db := newDBAt(t, dir)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	col := emptyColumn()
	col.Chunk.SetBlock(0, 0, 0, 0, ridStone)
	if err := db.StoreColumn(pos, col); !errors.Is(err, leveldb.ErrClosed) {
		t.Fatalf("store: err = %v, want leveldb.ErrClosed", err)
	}

	reopened := newDBAt(t, dir)
	if _, err := reopened.LoadColumn(pos); !errors.Is(err, leveldb.ErrNotFound) {
		t.Fatalf("load after failed store: err = %v, want leveldb.ErrNotFound", err)
	}
	k := dbKey{pos: pos}
	for _, key := range [][]byte{k.Sum(keyVersion), k.Sum(key3DData), k.Sum(keyFinalisation), k.Sum(keySubChunkData, 0xfc)} {
		if _, ok := getRaw(t, reopened, key); ok {
			t.Errorf("key %x was written by a store that failed", key)
		}
	}
}

// TestConcurrentColumnAccess fixes that a DB carries no lock of its own and relies entirely on leveldb being
// safe for concurrent use. Distinct columns may be stored and loaded from many goroutines at once.
//
// This says nothing about two goroutines writing the SAME column: storeEntities reads the chunk's entity
// index, builds a batch from it and writes later, and that read-modify-write is not atomic.
func TestConcurrentColumnAccess(t *testing.T) {
	db := newDB(t)
	const goroutines, perGoroutine = 8, 12

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perGoroutine)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				pos := world.ChunkPos{int32(g), int32(i)}
				rid := 1 + uint32(g+i)%5
				col := emptyColumn()
				col.Chunk.SetBlock(0, 0, 0, 0, rid)
				if err := db.StoreColumn(pos, col); err != nil {
					errs <- err
					return
				}
				got, err := db.LoadColumn(pos)
				if err != nil {
					errs <- err
					return
				}
				if b := got.Chunk.Block(0, 0, 0, 0); b != rid {
					errs <- errBlockMismatch{pos: pos, got: b, want: rid}
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

type errBlockMismatch struct {
	pos       world.ChunkPos
	got, want uint32
}

func (e errBlockMismatch) Error() string {
	return "block at " + e.pos.String() + ": got " + itoa(e.got) + ", want " + itoa(e.want)
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
