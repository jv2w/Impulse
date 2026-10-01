package mcdb

import (
	"bytes"
	"math"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// TestKeyConstants fixes the literal value of every key in keys.go. These bytes are the on-disk contract with
// vanilla Bedrock: a key that changes value does not fail loudly, it simply stops finding the data vanilla
// wrote, so every constant is compared against a literal rather than against another constant.
func TestKeyConstants(t *testing.T) {
	// Single-byte keys are appended after the 8- or 12-byte chunk index. The hex values are what appears in a
	// leveldb key dump of a vanilla world.
	t.Run("byte keys", func(t *testing.T) {
		cases := []struct {
			name      string
			got, want byte
		}{
			{"keySubChunkData", keySubChunkData, 0x2f},
			{"keyVersion", keyVersion, 0x2c},
			{"keyVersionOld", keyVersionOld, 0x76},
			{"keyBlockEntities", keyBlockEntities, 0x31},
			{"keyEntitiesOld", keyEntitiesOld, 0x32},
			{"keyPendingScheduledTicks", keyPendingScheduledTicks, 0x33},
			{"keyFinalisation", keyFinalisation, 0x36},
			{"key3DData", key3DData, 0x2b},
			{"key2DData", key2DData, 0x2d},
			{"keyChecksums", keyChecksums, 0x3b},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%v = %#02x (%q), want %#02x (%q)", c.name, c.got, rune(c.got), c.want, rune(c.want))
			}
		}
	})

	// String keys are used as a prefix (the chunk index or the entity ID follows) or, for the per-world keys,
	// as the whole key.
	t.Run("string keys", func(t *testing.T) {
		cases := []struct {
			name      string
			got, want string
		}{
			{"keyEntityIdentifiers", keyEntityIdentifiers, "digp"},
			{"keyEntity", keyEntity, "actorprefix"},
			{"keyAutonomousEntities", keyAutonomousEntities, "AutonomousEntities"},
			{"keyOverworld", keyOverworld, "Overworld"},
			{"keyMobEvents", keyMobEvents, "mobevents"},
			{"keyBiomeData", keyBiomeData, "BiomeData"},
			{"keyScoreboard", keyScoreboard, "scoreboard"},
			{"keyLocalPlayer", keyLocalPlayer, "~local_player"},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%v = %q (% x), want %q (% x)", c.name, c.got, c.got, c.want, c.want)
			}
		}
	})

	// The values stored under keyFinalisation, written as a little-endian int32.
	t.Run("finalisation states", func(t *testing.T) {
		cases := []struct {
			name      string
			got, want int
		}{
			{"finalisationGenerated", finalisationGenerated, 1},
			{"finalisationPopulated", finalisationPopulated, 2},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%v = %v, want %v", c.name, c.got, c.want)
			}
		}
	})
}

// TestIndexGoldenBytes fixes the chunk index: X then Z as little-endian int32s, 8 bytes with no dimension
// field after them, which is what vanilla writes for the Overworld.
func TestIndexGoldenBytes(t *testing.T) {
	cases := []struct {
		name string
		pos  world.ChunkPos
		want []byte
	}{
		{
			name: "overworld origin",
			pos:  world.ChunkPos{0, 0},
			want: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name: "overworld positive",
			pos:  world.ChunkPos{1, 2},
			want: []byte{0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00},
		},
		{
			name: "overworld negative",
			pos:  world.ChunkPos{-1, -2},
			want: []byte{0xff, 0xff, 0xff, 0xff, 0xfe, 0xff, 0xff, 0xff},
		},
		{
			name: "overworld mixed sign",
			pos:  world.ChunkPos{-3, 4},
			want: []byte{0xfd, 0xff, 0xff, 0xff, 0x04, 0x00, 0x00, 0x00},
		},
		{
			name: "overworld extremes",
			pos:  world.ChunkPos{math.MinInt32, math.MaxInt32},
			want: []byte{0x00, 0x00, 0x00, 0x80, 0xff, 0xff, 0xff, 0x7f},
		},
		{
			name: "overworld extremes swapped",
			pos:  world.ChunkPos{math.MaxInt32, math.MinInt32},
			want: []byte{0xff, 0xff, 0xff, 0x7f, 0x00, 0x00, 0x00, 0x80},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := index(c.pos)
			if !bytes.Equal(got, c.want) {
				t.Fatalf("index(%v) = % x (%v bytes), want % x (%v bytes)", c.pos, got, len(got), c.want, len(c.want))
			}
		})
	}
}

// TestSumAppendsSuffix fixes that Sum writes the suffix after the index, never before or inside it. The sub
// chunk cases use 0xfc, which is byte(-64>>4), the bottom sub chunk of the Overworld.
func TestSumAppendsSuffix(t *testing.T) {
	cases := []struct {
		name   string
		pos    world.ChunkPos
		suffix []byte
		want   []byte
	}{
		{
			name: "overworld no suffix",
			pos:  world.ChunkPos{1, 2}, suffix: nil,
			want: []byte{0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00},
		},
		{
			name: "overworld one byte",
			pos:  world.ChunkPos{1, 2}, suffix: []byte{keyVersion},
			want: []byte{0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x2c},
		},
		{
			name: "overworld two bytes",
			pos:  world.ChunkPos{1, 2}, suffix: []byte{keySubChunkData, 0xfc},
			want: []byte{0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x2f, 0xfc},
		},
		{
			name: "overworld negative two bytes",
			pos:  world.ChunkPos{-1, -2}, suffix: []byte{keySubChunkData, 0xfc},
			want: []byte{0xff, 0xff, 0xff, 0xff, 0xfe, 0xff, 0xff, 0xff, 0x2f, 0xfc},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dbKey{pos: c.pos}.Sum(c.suffix...)
			if !bytes.Equal(got, c.want) {
				t.Fatalf("Sum(% x) = % x, want % x", c.suffix, got, c.want)
			}
		})
	}
}

// TestSumIndependentPerCall fixes that each Sum call allocates: two keys built from one dbKey may be held and
// written to at the same time. leveldb.Batch keeps the key slices it is handed until Write, so a Sum that
// reused a buffer would corrupt every key already queued in the batch.
func TestSumIndependentPerCall(t *testing.T) {
	k := dbKey{pos: world.ChunkPos{1, 2}}
	a, b := k.Sum(keyVersion), k.Sum(keyFinalisation)
	if &a[0] == &b[0] {
		t.Fatal("two Sum calls returned slices over the same array")
	}
	a[0], a[len(a)-1] = 0xff, 0xff
	if b[0] != 0x01 {
		t.Errorf("b[0] = %#02x after writing to a, want 0x01", b[0])
	}
	if want := byte(keyFinalisation); b[len(b)-1] != want {
		t.Errorf("b suffix = %#02x after writing to a, want %#02x", b[len(b)-1], want)
	}
	if c := k.Sum(keyVersion); c[0] != 0x01 || c[len(c)-1] != byte(keyVersion) {
		t.Errorf("Sum after mutating an earlier result = % x", c)
	}
}

// TestSumHasNoSpareCapacity fixes that a Sum result is allocated to exactly its length, so appending to one
// result twice never makes the two appends share an array.
func TestSumHasNoSpareCapacity(t *testing.T) {
	for _, suffix := range [][]byte{nil, {keyVersion}, {keySubChunkData, 0xfc}} {
		k := dbKey{pos: world.ChunkPos{1, 2}}.Sum(suffix...)
		if want := 8 + len(suffix); len(k) != want || cap(k) != want {
			t.Fatalf("Sum(% x): len %v cap %v, want len %v cap %v", suffix, len(k), cap(k), want, want)
		}
	}
	over := dbKey{pos: world.ChunkPos{1, 2}}.Sum()
	a, b := append(over, keyVersion), append(over, keyFinalisation)
	if a[8] != byte(keyVersion) || b[8] != byte(keyFinalisation) {
		t.Errorf("appends aliased: a[8] = %#02x, b[8] = %#02x", a[8], b[8])
	}
}

// TestEntityIndexGoldenBytes fixes the per-entity key: the ASCII bytes of "actorprefix" (no length prefix,
// no separator) followed by the ID as a little-endian uint64. The ID is signed on the way in and reinterpreted
// unsigned, so negative IDs occupy the top half of the key space.
func TestEntityIndexGoldenBytes(t *testing.T) {
	// 0x61..0x78 spell "actorprefix".
	cases := []struct {
		name string
		id   int64
		want []byte
	}{
		{
			name: "zero", id: 0,
			want: []byte{
				0x61, 0x63, 0x74, 0x6f, 0x72, 0x70, 0x72, 0x65, 0x66, 0x69, 0x78,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "one", id: 1,
			want: []byte{
				0x61, 0x63, 0x74, 0x6f, 0x72, 0x70, 0x72, 0x65, 0x66, 0x69, 0x78,
				0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "minus one", id: -1,
			want: []byte{
				0x61, 0x63, 0x74, 0x6f, 0x72, 0x70, 0x72, 0x65, 0x66, 0x69, 0x78,
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			},
		},
		{
			name: "min int64", id: math.MinInt64,
			want: []byte{
				0x61, 0x63, 0x74, 0x6f, 0x72, 0x70, 0x72, 0x65, 0x66, 0x69, 0x78,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80,
			},
		},
		{
			name: "max int64", id: math.MaxInt64,
			want: []byte{
				0x61, 0x63, 0x74, 0x6f, 0x72, 0x70, 0x72, 0x65, 0x66, 0x69, 0x78,
				0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := entityIndex(c.id)
			if !bytes.Equal(got, c.want) {
				t.Fatalf("entityIndex(%v) = % x, want % x", c.id, got, c.want)
			}
			if string(got[:len(keyEntity)]) != keyEntity {
				t.Errorf("prefix = %q, want %q", got[:len(keyEntity)], keyEntity)
			}
		})
	}
}

// TestBlockPosFromNBT fixes how a block entity position is recovered from decoded NBT. Only int32 is
// accepted; any other tag type and any absent key make the whole position unusable. Reporting that is what
// keeps a block entity written by a tool that stored int64 from being relocated to the chunk origin, where it
// would overwrite whatever is there on the next store.
func TestBlockPosFromNBT(t *testing.T) {
	cases := []struct {
		name   string
		data   map[string]any
		want   cube.Pos
		wantOK bool
	}{
		{
			name: "all int32", wantOK: true,
			data: map[string]any{"x": int32(1), "y": int32(-2), "z": int32(3)},
			want: cube.Pos{1, -2, 3},
		},
		{
			name: "int32 extremes", wantOK: true,
			data: map[string]any{"x": int32(math.MinInt32), "y": int32(math.MaxInt32), "z": int32(0)},
			want: cube.Pos{math.MinInt32, math.MaxInt32, 0},
		},
		{
			name: "extra keys ignored, lookup is case sensitive", wantOK: true,
			data: map[string]any{"x": int32(1), "y": int32(2), "z": int32(3), "id": "Chest", "X": int32(9)},
			want: cube.Pos{1, 2, 3},
		},
		{name: "nil map", data: nil},
		{name: "empty map", data: map[string]any{}},
		{name: "missing y", data: map[string]any{"x": int32(4), "z": int32(6)}},
		{name: "int64 values", data: map[string]any{"x": int64(1), "y": int64(2), "z": int64(3)}},
		{name: "int16 values", data: map[string]any{"x": int16(1), "y": int16(2), "z": int16(3)}},
		{name: "string values", data: map[string]any{"x": "1", "y": "2", "z": "3"}},
		{name: "nil values", data: map[string]any{"x": nil, "y": nil, "z": nil}},
		{name: "mixed types", data: map[string]any{"x": int32(7), "y": int64(8), "z": "9"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := blockPosFromNBT(c.data)
			if ok != c.wantOK {
				t.Fatalf("blockPosFromNBT(%v) ok = %v, want %v", c.data, ok, c.wantOK)
			}
			if got != c.want {
				t.Fatalf("blockPosFromNBT(%v) = %v, want %v", c.data, got, c.want)
			}
		})
	}
}
