package mcdb

import (
	"bytes"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// Fixed UUIDs so that a failure names the same keys on every run.
var (
	playerA     = uuid.MustParse("6f9b2e1c-4d3a-4c8e-9f10-2a7b5c6d8e90")
	playerB     = uuid.MustParse("1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f")
	playerOther = uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
)

// playerKey is the record that maps a UUID to the key its server data lives under.
func playerKey(id uuid.UUID) []byte { return []byte("player_" + id.String()) }

// serverKey is the key SavePlayerSpawnPosition picks for a UUID it has never seen.
func serverKey(id uuid.UUID) []byte { return []byte("player_server_" + id.String()) }

// playerNBT encodes v the way db.go writes both player records.
func playerNBT(t *testing.T, v any) []byte {
	t.Helper()
	b, err := nbt.MarshalEncoding(v, nbt.LittleEndian)
	if err != nil {
		t.Fatalf("marshal nbt: %v", err)
	}
	return b
}

// decodePlayerNBT decodes a raw record into a map so that the field names on disk can be asserted.
func decodePlayerNBT(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := nbt.UnmarshalEncoding(b, &m, nbt.LittleEndian); err != nil {
		t.Fatalf("decode nbt: %v", err)
	}
	return m
}

// playerKeys returns every key in the DB starting with "player_", sorted.
func playerKeys(t *testing.T, db *DB) []string {
	t.Helper()
	it := db.ldb.NewIterator(nil, nil)
	defer it.Release()

	var keys []string
	for it.Next() {
		// it.Key() is only valid until the next call, so it is copied here.
		if k := string(it.Key()); strings.HasPrefix(k, "player_") {
			keys = append(keys, k)
		}
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterate keys: %v", err)
	}
	slices.Sort(keys)
	return keys
}

// TestPlayerSpawnPositionRoundTrip fixes what a save/load cycle preserves.
//
// PINNED BUG: cube.Pos holds int but SpawnX/Y/Z are int32 on disk, so the last two rows come back silently
// truncated instead of the save refusing a coordinate it cannot represent.
func TestPlayerSpawnPositionRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		pos  cube.Pos
		want cube.Pos
	}{
		{name: "zero", pos: cube.Pos{0, 0, 0}, want: cube.Pos{0, 0, 0}},
		{name: "positive", pos: cube.Pos{128, 70, 4096}, want: cube.Pos{128, 70, 4096}},
		{name: "negative", pos: cube.Pos{-128, -30, -4096}, want: cube.Pos{-128, -30, -4096}},
		{name: "world bottom", pos: cube.Pos{0, -64, 0}, want: cube.Pos{0, -64, 0}},
		{name: "world top", pos: cube.Pos{0, 319, 0}, want: cube.Pos{0, 319, 0}},
		{name: "int32 limits", pos: cube.Pos{1<<31 - 1, -1 << 31, 1<<31 - 1}, want: cube.Pos{1<<31 - 1, -1 << 31, 1<<31 - 1}},
		{name: "above int32 max", pos: cube.Pos{1 << 31, 1 << 31, 1<<31 + 5}, want: cube.Pos{-1 << 31, -1 << 31, -1<<31 + 5}},
		{name: "below int32 min", pos: cube.Pos{-1<<31 - 1, -1<<31 - 1, -1 << 32}, want: cube.Pos{1<<31 - 1, 1<<31 - 1, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			if err := db.SavePlayerSpawnPosition(playerA, c.pos); err != nil {
				t.Fatalf("save %v: %v", c.pos, err)
			}
			pos, exists, err := db.LoadPlayerSpawnPosition(playerA)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if !exists {
				t.Fatal("exists = false after a save")
			}
			if pos != c.want {
				t.Errorf("pos = %v, want %v (saved %v)", pos, c.want, c.pos)
			}
		})
	}
}

// TestPlayerSpawnPositionOverwrite fixes that a second save replaces the first in place: the player record is
// left untouched and the spawn is rewritten under the server-data key it already names, so no orphan is left
// behind.
func TestPlayerSpawnPositionOverwrite(t *testing.T) {
	db := newDB(t)
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{1, 2, 3}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	before, ok := getRaw(t, db, playerKey(playerA))
	if !ok {
		t.Fatalf("key %q not written", playerKey(playerA))
	}

	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{-4, -5, -6}); err != nil {
		t.Fatalf("second save: %v", err)
	}
	pos, exists, err := db.LoadPlayerSpawnPosition(playerA)
	if err != nil || !exists {
		t.Fatalf("load: pos %v, exists %v, err %v", pos, exists, err)
	}
	if pos != (cube.Pos{-4, -5, -6}) {
		t.Errorf("pos = %v, want (-4,-5,-6); the second save did not win", pos)
	}

	after, _ := getRaw(t, db, playerKey(playerA))
	if !bytes.Equal(before, after) {
		t.Errorf("player record changed on the second save: %x -> %x", before, after)
	}
	want := []string{string(playerKey(playerA)), string(serverKey(playerA))}
	slices.Sort(want)
	if got := playerKeys(t, db); !slices.Equal(got, want) {
		t.Errorf("player keys = %q, want %q", got, want)
	}
}

// TestPlayerSpawnPositionDistinctUUIDs fixes that two players never share a record.
func TestPlayerSpawnPositionDistinctUUIDs(t *testing.T) {
	db := newDB(t)
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{10, 20, 30}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := db.SavePlayerSpawnPosition(playerB, cube.Pos{-10, -20, -30}); err != nil {
		t.Fatalf("save B: %v", err)
	}

	for _, c := range []struct {
		id   uuid.UUID
		want cube.Pos
	}{
		{playerA, cube.Pos{10, 20, 30}},
		{playerB, cube.Pos{-10, -20, -30}},
	} {
		pos, exists, err := db.LoadPlayerSpawnPosition(c.id)
		if err != nil || !exists {
			t.Fatalf("load %v: pos %v, exists %v, err %v", c.id, pos, exists, err)
		}
		if pos != c.want {
			t.Errorf("load %v: pos = %v, want %v", c.id, pos, c.want)
		}
	}
	want := []string{
		string(playerKey(playerA)), string(serverKey(playerA)),
		string(playerKey(playerB)), string(serverKey(playerB)),
	}
	slices.Sort(want)
	if got := playerKeys(t, db); !slices.Equal(got, want) {
		t.Errorf("player keys = %q, want %q", got, want)
	}
}

// TestLoadPlayerSpawnPositionUnknownUUID fixes that a player who never saved is reported as absent rather
// than as an error: exists is false, err is nil.
func TestLoadPlayerSpawnPositionUnknownUUID(t *testing.T) {
	db := newDB(t)
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{1, 2, 3}); err != nil {
		t.Fatalf("save: %v", err)
	}

	pos, exists, err := db.LoadPlayerSpawnPosition(playerB)
	if err != nil {
		t.Fatalf("err = %v, want nil for an unknown uuid", err)
	}
	if exists {
		t.Error("exists = true for a uuid that was never saved")
	}
	if pos != (cube.Pos{}) {
		t.Errorf("pos = %v, want the zero position", pos)
	}

	serverData, key, exists, err := db.loadPlayerData(playerB)
	if serverData != nil || key != "" || exists || err != nil {
		t.Errorf("loadPlayerData = (%v, %q, %v, %v), want (nil, \"\", false, nil)", serverData, key, exists, err)
	}
}

// TestPlayerSpawnPositionAcrossReopen fixes that the spawn survives closing and reopening the world.
func TestPlayerSpawnPositionAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	first := newDBAt(t, dir)
	if err := first.SavePlayerSpawnPosition(playerA, cube.Pos{5, -64, -7}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := first.SavePlayerSpawnPosition(playerB, cube.Pos{0, 319, 0}); err != nil {
		t.Fatalf("save B: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newDBAt(t, dir)
	for _, c := range []struct {
		id   uuid.UUID
		want cube.Pos
	}{
		{playerA, cube.Pos{5, -64, -7}},
		{playerB, cube.Pos{0, 319, 0}},
	} {
		pos, exists, err := second.LoadPlayerSpawnPosition(c.id)
		if err != nil || !exists {
			t.Fatalf("load %v after reopen: pos %v, exists %v, err %v", c.id, pos, exists, err)
		}
		if pos != c.want {
			t.Errorf("load %v after reopen: pos = %v, want %v", c.id, pos, c.want)
		}
	}

	// A save after the reopen must still overwrite rather than start a second record.
	if err := second.SavePlayerSpawnPosition(playerA, cube.Pos{1, 1, 1}); err != nil {
		t.Fatalf("save after reopen: %v", err)
	}
	want := []string{
		string(playerKey(playerA)), string(serverKey(playerA)),
		string(playerKey(playerB)), string(serverKey(playerB)),
	}
	slices.Sort(want)
	if got := playerKeys(t, second); !slices.Equal(got, want) {
		t.Errorf("player keys = %q, want %q", got, want)
	}
}

// TestSavePlayerSpawnPositionWritesVanillaKeys fixes the on-disk shape of both records. The key names and the
// MsaId/ServerId/SelfSignedId field names are the vanilla format, not an internal detail: a world written here
// is read by the game.
func TestSavePlayerSpawnPositionWritesVanillaKeys(t *testing.T) {
	db := newDB(t)
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{7, 8, 9}); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, ok := getRaw(t, db, playerKey(playerA))
	if !ok {
		t.Fatalf("key %q not written", playerKey(playerA))
	}
	record := decodePlayerNBT(t, raw)
	for _, k := range []string{"MsaId", "ServerId", "SelfSignedId"} {
		if _, ok := record[k]; !ok {
			t.Errorf("player record has no %q field: %+v", k, record)
		}
	}
	if got := record["MsaId"]; got != playerA.String() {
		t.Errorf("MsaId = %v, want %v", got, playerA.String())
	}
	if got := record["SelfSignedId"]; got != "" {
		t.Errorf("SelfSignedId = %v, want the empty string", got)
	}
	if got, want := record["ServerId"], string(serverKey(playerA)); got != want {
		t.Errorf("ServerId = %v, want %v", got, want)
	}

	rawServer, ok := getRaw(t, db, serverKey(playerA))
	if !ok {
		t.Fatalf("key %q not written", serverKey(playerA))
	}
	server := decodePlayerNBT(t, rawServer)
	// A first save starts from an empty compound, so the spawn is all the server data holds.
	if len(server) != 3 {
		t.Errorf("server data has %v fields (%+v), want only SpawnX/SpawnY/SpawnZ", len(server), server)
	}
	for k, want := range map[string]int32{"SpawnX": 7, "SpawnY": 8, "SpawnZ": 9} {
		if got := server[k]; got != want {
			t.Errorf("%v = %v (%T), want int32 %v", k, got, got, want)
		}
	}
}

// TestLoadPlayerSpawnPositionCorrupt pins what each hostile on-disk state produces. Two rows panic; the rest
// return an error and report exists as true.
//
// PINNED BUG: LoadPlayerSpawnPosition asserts x.(int32) on data read from disk without the comma-ok form, so
// a spawn field stored as any other NBT type takes down the calling goroutine instead of returning an error.
//
// PINNED BUG: none of these errors carries a sentinel. loadPlayerData and LoadPlayerSpawnPosition build them
// with fmt.Errorf and a plain string, so a caller that wants to tell "this record is garbage, reset it" from
// "the database is broken, stop" has nothing to branch on but the message text. Only the two rows that wrap a
// leveldb error can be matched with errors.Is.
func TestLoadPlayerSpawnPositionCorrupt(t *testing.T) {
	// saveThenCorruptServerData writes a valid pair of records and then replaces the server data.
	saveThenCorruptServerData := func(server map[string]any) func(*testing.T, *DB) {
		return func(t *testing.T, db *DB) {
			if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{1, 2, 3}); err != nil {
				t.Fatalf("save: %v", err)
			}
			putRaw(t, db, serverKey(playerA), playerNBT(t, server))
		}
	}
	cases := []struct {
		name      string
		setup     func(*testing.T, *DB)
		wantPanic bool
		wantErrIs error
	}{
		{
			name: "player record is not nbt",
			setup: func(t *testing.T, db *DB) {
				putRaw(t, db, playerKey(playerA), []byte("not nbt at all"))
			},
		},
		{
			name: "MsaId does not match the uuid",
			setup: func(t *testing.T, db *DB) {
				putRaw(t, db, playerKey(playerA), playerNBT(t, playerData{
					UUID: playerOther.String(), ServerID: string(serverKey(playerA)),
				}))
				putRaw(t, db, serverKey(playerA), playerNBT(t, map[string]any{
					"SpawnX": int32(1), "SpawnY": int32(2), "SpawnZ": int32(3),
				}))
			},
		},
		{
			name: "ServerId is empty",
			setup: func(t *testing.T, db *DB) {
				putRaw(t, db, playerKey(playerA), playerNBT(t, playerData{UUID: playerA.String()}))
			},
		},
		{
			name: "ServerId points at a missing key",
			setup: func(t *testing.T, db *DB) {
				putRaw(t, db, playerKey(playerA), playerNBT(t, playerData{
					UUID: playerA.String(), ServerID: "player_server_gone",
				}))
			},
			wantErrIs: leveldb.ErrNotFound,
		},
		{
			name:  "server data has no spawn fields",
			setup: saveThenCorruptServerData(map[string]any{"Health": int32(20)}),
		},
		{
			name: "SpawnX is an int64",
			setup: saveThenCorruptServerData(map[string]any{
				"SpawnX": int64(1), "SpawnY": int32(2), "SpawnZ": int32(3),
			}),
			wantPanic: true,
		},
		{
			name: "SpawnX is a string",
			setup: saveThenCorruptServerData(map[string]any{
				"SpawnX": "1", "SpawnY": int32(2), "SpawnZ": int32(3),
			}),
			wantPanic: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := newDB(t)
			c.setup(t, db)

			var pos cube.Pos
			var exists bool
			var err error
			p := recovered(func() { pos, exists, err = db.LoadPlayerSpawnPosition(playerA) })

			if c.wantPanic {
				if p == nil {
					t.Fatalf("no panic; LoadPlayerSpawnPosition returned pos %v, exists %v, err %v", pos, exists, err)
				}
				e, ok := p.(error)
				if !ok {
					t.Fatalf("panic value = %v (%T), want an error", p, p)
				}
				var assertion *runtime.TypeAssertionError
				if !errors.As(e, &assertion) {
					t.Fatalf("panic value = %v (%T), want *runtime.TypeAssertionError", p, p)
				}
				if !strings.Contains(e.Error(), "int32") {
					t.Errorf("panic message = %q, want it to name int32", e.Error())
				}
				return
			}
			if p != nil {
				t.Fatalf("panicked with %v", p)
			}
			if err == nil {
				t.Fatalf("err = nil, want an error; pos %v, exists %v", pos, exists)
			}
			if c.wantErrIs != nil && !errors.Is(err, c.wantErrIs) {
				t.Errorf("err = %v, want it to wrap %v", err, c.wantErrIs)
			}
			// The UUID in the message is the only thing every one of these errors has in common: there is
			// no sentinel and no error type to match on.
			if !strings.Contains(err.Error(), playerA.String()) {
				t.Errorf("err = %v, want it to name the uuid %v", err, playerA)
			}
			// exists is true on every failure path, whether or not the player has a record at all.
			if !exists {
				t.Errorf("exists = false, want true")
			}
			if pos != (cube.Pos{}) {
				t.Errorf("pos = %v, want the zero position alongside an error", pos)
			}
		})
	}
}

// TestSavePlayerSpawnPositionCorruptRecord fixes that a damaged player record is never repaired. The key
// exists, so the save path goes through loadPlayerData, fails there, and writes nothing: every later save for
// that UUID fails the same way.
func TestSavePlayerSpawnPositionCorruptRecord(t *testing.T) {
	db := newDB(t)
	putRaw(t, db, playerKey(playerA), []byte("not nbt at all"))

	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{1, 2, 3}); err == nil {
		t.Fatal("save succeeded over a corrupt player record")
	}
	if _, ok := getRaw(t, db, serverKey(playerA)); ok {
		t.Error("server data was written although the save failed")
	}
	raw, ok := getRaw(t, db, playerKey(playerA))
	if !ok || !bytes.Equal(raw, []byte("not nbt at all")) {
		t.Errorf("player record = %x (present %v), want the corrupt bytes left in place", raw, ok)
	}
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{4, 5, 6}); err == nil {
		t.Error("a second save recovered from the corrupt record")
	}
}

// TestLoadPlayerDataFreshMap fixes that the map loadPlayerData returns is a fresh decode the caller owns:
// writing to it changes neither the DB nor a later call.
func TestLoadPlayerDataFreshMap(t *testing.T) {
	db := newDB(t)
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{10, 20, 30}); err != nil {
		t.Fatalf("save: %v", err)
	}

	first, key, exists, err := db.loadPlayerData(playerA)
	if err != nil || !exists {
		t.Fatalf("first load: exists %v, err %v", exists, err)
	}
	if key != string(serverKey(playerA)) {
		t.Errorf("key = %q, want %q", key, serverKey(playerA))
	}
	first["SpawnX"] = int32(-1)
	first["injected"] = "x"

	second, key2, exists2, err := db.loadPlayerData(playerA)
	if err != nil || !exists2 {
		t.Fatalf("second load: exists %v, err %v", exists2, err)
	}
	if key2 != key {
		t.Errorf("key = %q on the second load, want %q", key2, key)
	}
	if got := second["SpawnX"]; got != int32(10) {
		t.Errorf("SpawnX = %v, want 10; the two calls share a map", got)
	}
	if _, ok := second["injected"]; ok {
		t.Errorf("the injected key came back: %+v", second)
	}
	if pos, _, err := db.LoadPlayerSpawnPosition(playerA); err != nil || pos != (cube.Pos{10, 20, 30}) {
		t.Errorf("pos = %v, err = %v; want (10,20,30) unchanged on disk", pos, err)
	}
}

// TestPlayerSpawnPositionAfterClose fixes that both entry points report a closed DB as a value rather than by
// panicking, and that the leveldb sentinel survives the wrapping.
//
// PINNED BUG: the "unknown uuid" subtest shows exists is true for a player with no record at all. exists is
// hardcoded to true on every path loadPlayerData takes after the first Get, so it says nothing about whether
// the player exists once err is non-nil.
func TestPlayerSpawnPositionAfterClose(t *testing.T) {
	db := newDB(t)
	if err := db.SavePlayerSpawnPosition(playerA, cube.Pos{1, 1, 1}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	t.Run("load", func(t *testing.T) {
		var pos cube.Pos
		var err error
		if p := recovered(func() { pos, _, err = db.LoadPlayerSpawnPosition(playerA) }); p != nil {
			t.Fatalf("panicked with %v", p)
		}
		if !errors.Is(err, leveldb.ErrClosed) {
			t.Errorf("err = %v, want it to wrap leveldb.ErrClosed", err)
		}
		if pos != (cube.Pos{}) {
			t.Errorf("pos = %v, want the zero position", pos)
		}
	})

	t.Run("load of an unknown uuid", func(t *testing.T) {
		var exists bool
		var err error
		if p := recovered(func() { _, exists, err = db.LoadPlayerSpawnPosition(playerB) }); p != nil {
			t.Fatalf("panicked with %v", p)
		}
		if !errors.Is(err, leveldb.ErrClosed) {
			t.Errorf("err = %v, want it to wrap leveldb.ErrClosed", err)
		}
		if !exists {
			t.Error("exists = false; the closed DB no longer claims an unsaved player exists")
		}
	})

	t.Run("save", func(t *testing.T) {
		var err error
		if p := recovered(func() { err = db.SavePlayerSpawnPosition(playerA, cube.Pos{2, 2, 2}) }); p != nil {
			t.Fatalf("panicked with %v", p)
		}
		if !errors.Is(err, leveldb.ErrClosed) {
			t.Errorf("err = %v, want it to wrap leveldb.ErrClosed", err)
		}
	})
}
