package nbtconv_test

import (
	"image/color"
	"math"
	"os"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/internal/nbtconv"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// TestMain finalizes the default block registry, which nbtconv.Block reaches through world.BlockByName. Without this
// the very first call to nbtconv.Block panics; see TestBlockReadPanicsOnNonFinalizedRegistry.
func TestMain(m *testing.M) {
	world.DefaultBlockRegistry.Finalize()
	os.Exit(m.Run())
}

// TestBlockReadOnNonFinalizedRegistry covers the lookup nbtconv.Block inherits from world.BlockByName when the registry
// backing it has not been finalized. nbtconv.Block is called from block entity DecodeNBT implementations, which are in
// turn reachable from client-supplied NBT, so the liveness of the build-then-Finalize lifetime must not decide whether
// decoding a chest survives. A fresh registry is used because TestMain finalizes DefaultBlockRegistry.
//
// PINNED CONSTRAINT: the two-phase lifetime itself remains, and CLAUDE.md 2.4 bans it; only its panic is gone.
func TestBlockReadOnNonFinalizedRegistry(t *testing.T) {
	b, ok := world.NewBlockRegistry().BlockByName("minecraft:stone", nil)
	if b != nil || ok {
		t.Errorf("BlockByName on a non-finalized registry = (%#v, %v), want (nil, false)", b, ok)
	}
}

// The scalar readers in read.go are the sole gateway between raw NBT maps and every block entity's DecodeNBT. Their
// contract is total: an absent key, a key holding the wrong Go type, and a key holding the zero value are all reported
// identically as the zero value, with no error channel. Pinning that indistinguishability is the point of these tests -
// it is the mechanism by which a save-format type drift silently blanks a block entity field instead of failing loudly.

// TestBoolReadsUint8One pins that Bool is defined strictly as Uint8(m,k)==1, so uint8(2) is false and an int32 or bool
// value at the key reads false even though a human would call the stored value "true".
//
// PINNED BUG: a bool stored under the key (rather than uint8) silently reads false. NBT decodes booleans as uint8, so
// this only bites callers that build maps by hand, but nothing reports the mismatch.
func TestBoolReadsUint8One(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		want bool
	}{
		{name: "uint8 one", m: map[string]any{"k": uint8(1)}, want: true},
		{name: "uint8 zero", m: map[string]any{"k": uint8(0)}, want: false},
		{name: "uint8 two is not true", m: map[string]any{"k": uint8(2)}, want: false},
		{name: "missing key", m: map[string]any{}, want: false},
		{name: "nil map", m: nil, want: false},
		{name: "go bool true reads false", m: map[string]any{"k": true}, want: false},
		{name: "int32 one reads false", m: map[string]any{"k": int32(1)}, want: false},
		{name: "byte alias one is uint8", m: map[string]any{"k": byte(1)}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nbtconv.Bool(tt.m, "k"); got != tt.want {
				t.Errorf("Bool = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestScalarReadersZeroOnTypeMismatch pins that every scalar reader returns the zero value both for an absent key and
// for a key whose value has any other Go type, including a wider or narrower integer of the same signedness.
//
// PINNED BUG: no reader distinguishes "absent" from "present but wrong type". A save written by a build that stored
// BurnTime as int32 is read back as 0 by a build reading int16, silently voiding the furnace's progress rather than
// failing. CLAUDE.md 2.2 wants failure carried as a value; these helpers have no way to carry one.
func TestScalarReadersZeroOnTypeMismatch(t *testing.T) {
	// Each reader is exercised with its own type (must round-trip), with every other scalar type at the same key
	// (must yield zero), and with the key absent (must yield zero).
	wrong := map[string]any{
		"uint8":   uint8(7),
		"string":  "seven",
		"int16":   int16(7),
		"int32":   int32(7),
		"int64":   int64(7),
		"float32": float32(7),
		"float64": float64(7),
		"slice":   []any{7},
	}
	t.Run("uint8", func(t *testing.T) {
		if got := nbtconv.Uint8(map[string]any{"k": uint8(7)}, "k"); got != 7 {
			t.Errorf("Uint8 = %d, want 7", got)
		}
		for name, v := range wrong {
			if name == "uint8" {
				continue
			}
			if got := nbtconv.Uint8(map[string]any{"k": v}, "k"); got != 0 {
				t.Errorf("Uint8 of %s = %d, want 0", name, got)
			}
		}
		if got := nbtconv.Uint8(map[string]any{}, "k"); got != 0 {
			t.Errorf("Uint8 of absent key = %d, want 0", got)
		}
	})
	t.Run("string", func(t *testing.T) {
		if got := nbtconv.String(map[string]any{"k": "seven"}, "k"); got != "seven" {
			t.Errorf("String = %q, want %q", got, "seven")
		}
		for name, v := range wrong {
			if name == "string" {
				continue
			}
			if got := nbtconv.String(map[string]any{"k": v}, "k"); got != "" {
				t.Errorf("String of %s = %q, want empty", name, got)
			}
		}
		if got := nbtconv.String(map[string]any{}, "k"); got != "" {
			t.Errorf("String of absent key = %q, want empty", got)
		}
	})
	t.Run("int16", func(t *testing.T) {
		if got := nbtconv.Int16(map[string]any{"k": int16(7)}, "k"); got != 7 {
			t.Errorf("Int16 = %d, want 7", got)
		}
		for name, v := range wrong {
			if name == "int16" {
				continue
			}
			if got := nbtconv.Int16(map[string]any{"k": v}, "k"); got != 0 {
				t.Errorf("Int16 of %s = %d, want 0", name, got)
			}
		}
		if got := nbtconv.Int16(map[string]any{}, "k"); got != 0 {
			t.Errorf("Int16 of absent key = %d, want 0", got)
		}
	})
	t.Run("int32", func(t *testing.T) {
		if got := nbtconv.Int32(map[string]any{"k": int32(7)}, "k"); got != 7 {
			t.Errorf("Int32 = %d, want 7", got)
		}
		for name, v := range wrong {
			if name == "int32" {
				continue
			}
			if got := nbtconv.Int32(map[string]any{"k": v}, "k"); got != 0 {
				t.Errorf("Int32 of %s = %d, want 0", name, got)
			}
		}
		if got := nbtconv.Int32(map[string]any{}, "k"); got != 0 {
			t.Errorf("Int32 of absent key = %d, want 0", got)
		}
	})
	t.Run("int64", func(t *testing.T) {
		if got := nbtconv.Int64(map[string]any{"k": int64(7)}, "k"); got != 7 {
			t.Errorf("Int64 = %d, want 7", got)
		}
		for name, v := range wrong {
			if name == "int64" {
				continue
			}
			if got := nbtconv.Int64(map[string]any{"k": v}, "k"); got != 0 {
				t.Errorf("Int64 of %s = %d, want 0", name, got)
			}
		}
		if got := nbtconv.Int64(map[string]any{}, "k"); got != 0 {
			t.Errorf("Int64 of absent key = %d, want 0", got)
		}
	})
	t.Run("float32", func(t *testing.T) {
		if got := nbtconv.Float32(map[string]any{"k": float32(7)}, "k"); got != 7 {
			t.Errorf("Float32 = %v, want 7", got)
		}
		for name, v := range wrong {
			if name == "float32" {
				continue
			}
			if got := nbtconv.Float32(map[string]any{"k": v}, "k"); got != 0 {
				t.Errorf("Float32 of %s = %v, want 0", name, got)
			}
		}
		if got := nbtconv.Float32(map[string]any{}, "k"); got != 0 {
			t.Errorf("Float32 of absent key = %v, want 0", got)
		}
	})
	t.Run("float64", func(t *testing.T) {
		if got := nbtconv.Float64(map[string]any{"k": float64(7)}, "k"); got != 7 {
			t.Errorf("Float64 = %v, want 7", got)
		}
		for name, v := range wrong {
			if name == "float64" {
				continue
			}
			if got := nbtconv.Float64(map[string]any{"k": v}, "k"); got != 0 {
				t.Errorf("Float64 of %s = %v, want 0", name, got)
			}
		}
		if got := nbtconv.Float64(map[string]any{}, "k"); got != 0 {
			t.Errorf("Float64 of absent key = %v, want 0", got)
		}
	})
	t.Run("slice", func(t *testing.T) {
		if got := nbtconv.Slice(map[string]any{"k": []any{7}}, "k"); len(got) != 1 {
			t.Errorf("Slice length = %d, want 1", len(got))
		}
		for name, v := range wrong {
			if name == "slice" {
				continue
			}
			if got := nbtconv.Slice(map[string]any{"k": v}, "k"); got != nil {
				t.Errorf("Slice of %s = %v, want nil", name, got)
			}
		}
		// A concretely typed slice is not []any and therefore reads as nil, even though NBT lists of compounds are
		// the only shape Slice is ever used on.
		if got := nbtconv.Slice(map[string]any{"k": []map[string]any{{}}}, "k"); got != nil {
			t.Errorf("Slice of []map[string]any = %v, want nil", got)
		}
		if got := nbtconv.Slice(map[string]any{}, "k"); got != nil {
			t.Errorf("Slice of absent key = %v, want nil", got)
		}
	})
}

// TestScalarReadersPreserveExtremes pins that the readers perform no range checking or clamping: the full range of each
// type survives, and out-of-range values are impossible to express because the type assertion already constrains them.
func TestScalarReadersPreserveExtremes(t *testing.T) {
	if got := nbtconv.Uint8(map[string]any{"k": uint8(255)}, "k"); got != 255 {
		t.Errorf("Uint8 max = %d, want 255", got)
	}
	if got := nbtconv.Int16(map[string]any{"k": int16(-32768)}, "k"); got != -32768 {
		t.Errorf("Int16 min = %d, want -32768", got)
	}
	if got := nbtconv.Int32(map[string]any{"k": int32(math.MinInt32)}, "k"); got != math.MinInt32 {
		t.Errorf("Int32 min = %d, want %d", got, math.MinInt32)
	}
	if got := nbtconv.Int64(map[string]any{"k": int64(math.MaxInt64)}, "k"); got != math.MaxInt64 {
		t.Errorf("Int64 max = %d, want %d", got, int64(math.MaxInt64))
	}
	if got := nbtconv.Float64(map[string]any{"k": math.Inf(1)}, "k"); !math.IsInf(got, 1) {
		t.Errorf("Float64 +Inf = %v, want +Inf", got)
	}
	nan := nbtconv.Float64(map[string]any{"k": math.NaN()}, "k")
	if !math.IsNaN(nan) {
		t.Errorf("Float64 NaN = %v, want NaN", nan)
	}
}

// TestTickDuration pins the tick-to-Duration conversion at exactly 50ms per tick for each of the three integer widths
// TickDuration accepts, including the sign-extension of a negative int16/int32 into a negative Duration.
func TestTickDuration(t *testing.T) {
	if got := nbtconv.TickDuration[uint8](map[string]any{"k": uint8(20)}, "k"); got != time.Second {
		t.Errorf("TickDuration[uint8](20) = %v, want 1s", got)
	}
	if got := nbtconv.TickDuration[int16](map[string]any{"k": int16(20)}, "k"); got != time.Second {
		t.Errorf("TickDuration[int16](20) = %v, want 1s", got)
	}
	if got := nbtconv.TickDuration[int32](map[string]any{"k": int32(20)}, "k"); got != time.Second {
		t.Errorf("TickDuration[int32](20) = %v, want 1s", got)
	}
	if got := nbtconv.TickDuration[int16](map[string]any{"k": int16(1)}, "k"); got != 50*time.Millisecond {
		t.Errorf("TickDuration[int16](1) = %v, want 50ms", got)
	}
	// A negative tick count is not rejected; it becomes a negative Duration and any caller comparing it against zero
	// sees an already-elapsed timer.
	if got := nbtconv.TickDuration[int16](map[string]any{"k": int16(-1)}, "k"); got != -50*time.Millisecond {
		t.Errorf("TickDuration[int16](-1) = %v, want -50ms", got)
	}
	// The type mismatch rule of the scalar readers propagates: an int32 read as int16 yields a zero Duration.
	if got := nbtconv.TickDuration[int16](map[string]any{"k": int32(20)}, "k"); got != 0 {
		t.Errorf("TickDuration[int16] of int32 = %v, want 0", got)
	}
	if got := nbtconv.TickDuration[int32](map[string]any{}, "k"); got != 0 {
		t.Errorf("TickDuration[int32] of absent key = %v, want 0", got)
	}
}

// TestTickDurationPanicsOnUnsupportedType pins that TickDuration panics for any integer type outside {uint8, int16,
// int32}, with the exact message.
//
// PINNED BUG: read.go's default branch is `panic("invalid tick duration value type")`, violating CLAUDE.md 2.2's ban on
// enum-switch default panics. The type parameter is constrained only to constraints.Integer, so `int` and `int64` -
// both entirely plausible at a call site - compile and then panic at run time rather than failing to compile.
func TestTickDurationPanicsOnUnsupportedType(t *testing.T) {
	assertPanics := func(t *testing.T, name string, f func()) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("TickDuration[%s] did not panic", name)
			}
			if s, ok := r.(string); !ok || s != "invalid tick duration value type" {
				t.Errorf("TickDuration[%s] panicked with %#v, want %q", name, r, "invalid tick duration value type")
			}
		}()
		f()
	}
	m := map[string]any{"k": int64(20)}
	assertPanics(t, "int", func() { _ = nbtconv.TickDuration[int](m, "k") })
	assertPanics(t, "int64", func() { _ = nbtconv.TickDuration[int64](m, "k") })
	assertPanics(t, "int8", func() { _ = nbtconv.TickDuration[int8](m, "k") })
	assertPanics(t, "uint16", func() { _ = nbtconv.TickDuration[uint16](m, "k") })
	assertPanics(t, "uint32", func() { _ = nbtconv.TickDuration[uint32](m, "k") })
	assertPanics(t, "uint64", func() { _ = nbtconv.TickDuration[uint64](m, "k") })
	// The panic fires on the type parameter alone, before the map is consulted, so an empty map still panics.
	assertPanics(t, "int64 with empty map", func() { _ = nbtconv.TickDuration[int64](map[string]any{}, "k") })
}

// TestRotationKeys pins the golden NBT key names "Yaw" and "Pitch", the float32 storage type, and the component order
// of the returned cube.Rotation (yaw first, pitch second).
func TestRotationKeys(t *testing.T) {
	got := nbtconv.Rotation(map[string]any{"Yaw": float32(90), "Pitch": float32(-45)})
	if want := (cube.Rotation{90, -45}); got != want {
		t.Errorf("Rotation = %v, want %v", got, want)
	}
	// Lower-case keys are a different key: the reader is case-sensitive and reports zero rotation.
	if got := nbtconv.Rotation(map[string]any{"yaw": float32(90), "pitch": float32(-45)}); got != (cube.Rotation{}) {
		t.Errorf("Rotation of lower-case keys = %v, want zero", got)
	}
	// float64 values under the correct keys are silently dropped: Rotation reads float32 only.
	if got := nbtconv.Rotation(map[string]any{"Yaw": float64(90), "Pitch": float64(-45)}); got != (cube.Rotation{}) {
		t.Errorf("Rotation of float64 = %v, want zero", got)
	}
	if got := nbtconv.Rotation(map[string]any{}); got != (cube.Rotation{}) {
		t.Errorf("Rotation of empty map = %v, want zero", got)
	}
}

// TestVec3 pins that Vec3 accepts exactly two encodings - a []any of float32 and a concrete []float32 - each of length
// exactly 3, and returns the zero vector for every other shape.
//
// PINNED BUG: a wrong-length list is reported as the zero vector rather than as an error, so a truncated save silently
// relocates whatever the vector positioned to the origin.
func TestVec3(t *testing.T) {
	want := mgl64.Vec3{1, -2, 3.5}
	tests := []struct {
		name string
		m    map[string]any
		want mgl64.Vec3
	}{
		{name: "slice of any", m: map[string]any{"k": []any{float32(1), float32(-2), float32(3.5)}}, want: want},
		{name: "concrete float32 slice", m: map[string]any{"k": []float32{1, -2, 3.5}}, want: want},
		{name: "truncated list", m: map[string]any{"k": []any{float32(1), float32(-2)}}, want: mgl64.Vec3{}},
		{name: "over-long list", m: map[string]any{"k": []any{float32(1), float32(2), float32(3), float32(4)}}, want: mgl64.Vec3{}},
		{name: "empty list", m: map[string]any{"k": []any{}}, want: mgl64.Vec3{}},
		{name: "truncated concrete list", m: map[string]any{"k": []float32{1, 2}}, want: mgl64.Vec3{}},
		{name: "float64 elements zero out", m: map[string]any{"k": []any{1.0, -2.0, 3.5}}, want: mgl64.Vec3{}},
		{name: "concrete float64 slice", m: map[string]any{"k": []float64{1, -2, 3.5}}, want: mgl64.Vec3{}},
		{name: "missing key", m: map[string]any{}, want: mgl64.Vec3{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nbtconv.Vec3(tt.m, "k"); got != tt.want {
				t.Errorf("Vec3 = %v, want %v", got, tt.want)
			}
		})
	}
	// A single bad element zeroes only that component; the rest survive. Partial corruption is not detected.
	if got := nbtconv.Vec3(map[string]any{"k": []any{float32(1), "x", float32(3)}}, "k"); got != (mgl64.Vec3{1, 0, 3}) {
		t.Errorf("Vec3 with one bad element = %v, want {1 0 3}", got)
	}
}

// TestPos pins the integer counterpart of Vec3: []any of int32 or a concrete []int32, length exactly 3, zero cube.Pos
// otherwise. int32 is the only accepted element type, so a list of Go ints reads as the origin.
func TestPos(t *testing.T) {
	want := cube.Pos{1, -2, 3}
	tests := []struct {
		name string
		m    map[string]any
		want cube.Pos
	}{
		{name: "slice of any", m: map[string]any{"k": []any{int32(1), int32(-2), int32(3)}}, want: want},
		{name: "concrete int32 slice", m: map[string]any{"k": []int32{1, -2, 3}}, want: want},
		{name: "truncated list", m: map[string]any{"k": []any{int32(1), int32(-2)}}, want: cube.Pos{}},
		{name: "over-long list", m: map[string]any{"k": []int32{1, 2, 3, 4}}, want: cube.Pos{}},
		{name: "int elements zero out", m: map[string]any{"k": []any{1, -2, 3}}, want: cube.Pos{}},
		{name: "int64 elements zero out", m: map[string]any{"k": []any{int64(1), int64(-2), int64(3)}}, want: cube.Pos{}},
		{name: "missing key", m: map[string]any{}, want: cube.Pos{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nbtconv.Pos(tt.m, "k"); got != tt.want {
				t.Errorf("Pos = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestVec3AndPosWriterRoundTrip pins that the writers emit concrete typed slices of exactly three elements, which is
// the second of the two encodings the matching reader accepts.
func TestVec3AndPosWriterRoundTrip(t *testing.T) {
	v := mgl64.Vec3{1.5, -2.5, 3.5}
	f := nbtconv.Vec3ToFloat32Slice(v)
	if len(f) != 3 || f[0] != 1.5 || f[1] != -2.5 || f[2] != 3.5 {
		t.Fatalf("Vec3ToFloat32Slice = %v, want [1.5 -2.5 3.5]", f)
	}
	if got := nbtconv.Vec3(map[string]any{"k": f}, "k"); got != v {
		t.Errorf("Vec3 round trip = %v, want %v", got, v)
	}
	// float64 precision beyond float32 is lost by the writer; the round trip is lossy, not exact.
	lossy := mgl64.Vec3{0.1, 0, 0}
	if got := nbtconv.Vec3(map[string]any{"k": nbtconv.Vec3ToFloat32Slice(lossy)}, "k"); got == lossy {
		t.Errorf("Vec3 round trip of 0.1 was exact; expected float32 precision loss")
	}

	p := cube.Pos{1, -2, 3}
	i := nbtconv.PosToInt32Slice(p)
	if len(i) != 3 || i[0] != 1 || i[1] != -2 || i[2] != 3 {
		t.Fatalf("PosToInt32Slice = %v, want [1 -2 3]", i)
	}
	if got := nbtconv.Pos(map[string]any{"k": i}, "k"); got != p {
		t.Errorf("Pos round trip = %v, want %v", got, p)
	}
	// A position beyond int32 range is truncated by the writer without complaint.
	if got := nbtconv.PosToInt32Slice(cube.Pos{math.MaxInt32 + 1, 0, 0}); got[0] != math.MinInt32 {
		t.Errorf("PosToInt32Slice overflow x = %d, want %d", got[0], int32(math.MinInt32))
	}
}

// TestWriteBlockGoldenKeys pins the three golden keys WriteBlock emits - "name", "states" and "version" - and that
// "version" carries chunk.CurrentBlockVersion verbatim. These key names are the on-disk contract for every block
// stored inside a block entity (item frames, lecterns, jukeboxes).
func TestWriteBlockGoldenKeys(t *testing.T) {
	m := nbtconv.WriteBlock(block.Stone{})
	if len(m) != 3 {
		t.Errorf("WriteBlock produced %d keys (%v), want exactly 3", len(m), m)
	}
	name, ok := m["name"].(string)
	if !ok {
		t.Fatalf("WriteBlock key \"name\" = %#v, want string", m["name"])
	}
	if name != "minecraft:stone" {
		t.Errorf("WriteBlock name = %q, want %q", name, "minecraft:stone")
	}
	if _, ok := m["states"].(map[string]any); !ok {
		t.Errorf("WriteBlock key \"states\" = %#v, want map[string]any", m["states"])
	}
	v, ok := m["version"].(int32)
	if !ok {
		t.Fatalf("WriteBlock key \"version\" = %#v, want int32", m["version"])
	}
	if v != chunk.CurrentBlockVersion {
		t.Errorf("WriteBlock version = %d, want chunk.CurrentBlockVersion (%d)", v, chunk.CurrentBlockVersion)
	}
}

// TestBlockReadGoldenKeys pins that Block reads the nested "name" and "states" keys written by WriteBlock, and that
// every failure mode - absent key, wrong nesting type, unknown block name - returns a nil world.Block rather than
// panicking or substituting air.
//
// PINNED BUG: an unknown block name yields nil, and the bool from world.BlockByName is discarded at read.go:147. A
// caller that stores the result without a nil check propagates a nil Block into the world.
func TestBlockReadGoldenKeys(t *testing.T) {
	written := nbtconv.WriteBlock(block.Stone{})
	if got := nbtconv.Block(map[string]any{"Block": written}, "Block"); got != (block.Stone{}) {
		t.Errorf("Block round trip = %#v, want block.Stone{}", got)
	}
	// The nested keys are read individually, so a map carrying only "name" still resolves when the block has no
	// properties that matter.
	if got := nbtconv.Block(map[string]any{"Block": map[string]any{"name": "minecraft:stone"}}, "Block"); got != (block.Stone{}) {
		t.Errorf("Block without states = %#v, want block.Stone{}", got)
	}
	tests := []struct {
		name string
		m    map[string]any
	}{
		{name: "missing key", m: map[string]any{}},
		{name: "wrong nesting type", m: map[string]any{"Block": "minecraft:stone"}},
		{name: "empty compound", m: map[string]any{"Block": map[string]any{}}},
		{name: "unknown block name", m: map[string]any{"Block": map[string]any{"name": "minecraft:not_a_block"}}},
		{name: "name of wrong type", m: map[string]any{"Block": map[string]any{"name": int32(1)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nbtconv.Block(tt.m, "Block"); got != nil {
				t.Errorf("Block = %#v, want nil", got)
			}
		})
	}
}

// TestBlockStatesOfWrongTypeDegradeToDefault pins that a "states" value which is not a map[string]any is dropped by the
// comma-ok assertion at read.go:142, after which the block resolves against its default properties.
//
// PINNED BUG: the outcome depends on whether the block has any state at all. A stateless block still resolves, so the
// corruption is invisible; a stateful one fails the exact-properties lookup and comes back nil, which read.go reports
// the same way as an absent key. Neither path reports that the states compound was unreadable. The golden state key
// "torch_facing_direction" is pinned here because it is the on-disk name, not a Go field name.
func TestBlockStatesOfWrongTypeDegradeToDefault(t *testing.T) {
	torch := nbtconv.WriteBlock(block.Torch{Facing: cube.FaceNorth})
	if states := torch["states"].(map[string]any); states["torch_facing_direction"] != "north" {
		t.Fatalf("torch states = %v, want torch_facing_direction=north", states)
	}
	if got := nbtconv.Block(map[string]any{"Block": torch}, "Block"); got != (block.Torch{Facing: cube.FaceNorth}) {
		t.Fatalf("Block of intact torch = %#v, want Torch{Facing: FaceNorth}", got)
	}
	for _, corrupt := range []any{"x", int32(1), []any{}, nil} {
		stateful := map[string]any{"name": torch["name"], "states": corrupt}
		if got := nbtconv.Block(map[string]any{"Block": stateful}, "Block"); got != nil {
			t.Errorf("stateful block with states %#v = %#v, want nil", corrupt, got)
		}
		// A block whose EncodeBlock emits no properties is unaffected: the lookup key is identical either way.
		stateless := map[string]any{"name": "minecraft:stone", "states": corrupt}
		if got := nbtconv.Block(map[string]any{"Block": stateless}, "Block"); got != (block.Stone{}) {
			t.Errorf("stateless block with states %#v = %#v, want block.Stone{}", corrupt, got)
		}
	}
}

// TestBlockIgnoresVersion pins that Block never consults the "version" key WriteBlock writes. Block data saved under an
// older block version is decoded against the current registry with no upgrade path and no rejection.
//
// PINNED SCOPE: block state upgrading across game versions is not implemented here; this test records the absence
// rather than demanding it.
func TestBlockIgnoresVersion(t *testing.T) {
	m := nbtconv.WriteBlock(block.Stone{})
	m["version"] = int32(0)
	if got := nbtconv.Block(map[string]any{"Block": m}, "Block"); got != (block.Stone{}) {
		t.Errorf("Block with version 0 = %#v, want block.Stone{}", got)
	}
	delete(m, "version")
	if got := nbtconv.Block(map[string]any{"Block": m}, "Block"); got != (block.Stone{}) {
		t.Errorf("Block without version = %#v, want block.Stone{}", got)
	}
}

// TestRGBAInt32Conversion pins the ARGB byte order used for sign text colours and the special case that turns fully
// transparent black into opaque black.
//
// PINNED BUG: the round trip is asymmetric. Int32FromRGBA keys its special case on R, G and B alone, ignoring A, so any
// colour with zero RGB - including a deliberately transparent one - comes back as opaque black (A=255). Conversely
// RGBAFromInt32(0) is the transparent colour that Int32FromRGBA can never produce.
func TestRGBAInt32Conversion(t *testing.T) {
	tests := []struct {
		name string
		in   color.RGBA
		want int32
	}{
		{name: "opaque white", in: color.RGBA{A: 0xff, R: 0xff, G: 0xff, B: 0xff}, want: -1},
		{name: "opaque red", in: color.RGBA{A: 0xff, R: 0xff}, want: int32(-0x1000000 + 0xff0000)},
		{name: "opaque black special case", in: color.RGBA{A: 0xff}, want: -0x1000000},
		{name: "zero value takes black special case", in: color.RGBA{}, want: -0x1000000},
		{name: "transparent black takes black special case", in: color.RGBA{A: 0x00}, want: -0x1000000},
		{name: "transparent blue is not special-cased", in: color.RGBA{A: 0x00, B: 0x01}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nbtconv.Int32FromRGBA(tt.in); got != tt.want {
				t.Errorf("Int32FromRGBA(%v) = %d (%#08x), want %d", tt.in, got, uint32(got), tt.want)
			}
		})
	}

	if got := nbtconv.RGBAFromInt32(-1); got != (color.RGBA{A: 0xff, R: 0xff, G: 0xff, B: 0xff}) {
		t.Errorf("RGBAFromInt32(-1) = %v, want opaque white", got)
	}
	if got := nbtconv.RGBAFromInt32(0); got != (color.RGBA{}) {
		t.Errorf("RGBAFromInt32(0) = %v, want zero value", got)
	}
	if got := nbtconv.RGBAFromInt32(-0x1000000); got != (color.RGBA{A: 0xff}) {
		t.Errorf("RGBAFromInt32(-0x1000000) = %v, want opaque black", got)
	}
	// Every non-black colour survives the round trip exactly.
	for _, c := range []color.RGBA{
		{A: 0xff, R: 0xff, G: 0x00, B: 0x00},
		{A: 0x80, R: 0x12, G: 0x34, B: 0x56},
		{A: 0x00, R: 0x01, G: 0x02, B: 0x03},
	} {
		if got := nbtconv.RGBAFromInt32(nbtconv.Int32FromRGBA(c)); got != c {
			t.Errorf("round trip of %v = %v", c, got)
		}
	}
	// The transparent-black round trip is the documented asymmetry: alpha is fabricated.
	if got := nbtconv.RGBAFromInt32(nbtconv.Int32FromRGBA(color.RGBA{})); got != (color.RGBA{A: 0xff}) {
		t.Errorf("round trip of transparent black = %v, want opaque black", got)
	}
}

// TestInvNBTRoundTrip pins the golden "Slot" key, that InvToNBT skips empty slots entirely rather than emitting
// placeholders, and that the slot index is stored as a byte.
func TestInvNBTRoundTrip(t *testing.T) {
	inv := inventory.New(27, nil)
	if err := inv.SetItem(0, item.NewStack(item.Diamond{}, 5)); err != nil {
		t.Fatalf("set slot 0: %v", err)
	}
	if err := inv.SetItem(26, item.NewStack(item.Emerald{}, 1)); err != nil {
		t.Fatalf("set slot 26: %v", err)
	}

	data := nbtconv.InvToNBT(inv)
	if len(data) != 2 {
		t.Fatalf("InvToNBT produced %d entries, want 2 (empty slots skipped)", len(data))
	}
	slots := make(map[byte]bool, 2)
	for _, e := range data {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("InvToNBT entry = %#v, want map[string]any", e)
		}
		s, ok := m["Slot"].(byte)
		if !ok {
			t.Fatalf("InvToNBT entry key \"Slot\" = %#v, want byte", m["Slot"])
		}
		slots[s] = true
	}
	if !slots[0] || !slots[26] {
		t.Errorf("InvToNBT slots = %v, want 0 and 26", slots)
	}

	out := inventory.New(27, nil)
	nbtconv.InvFromNBT(out, data)
	for _, slot := range []int{0, 26} {
		want, _ := inv.Item(slot)
		got, err := out.Item(slot)
		if err != nil {
			t.Fatalf("read slot %d: %v", slot, err)
		}
		if !got.Comparable(want) || got.Count() != want.Count() {
			t.Errorf("slot %d = %v, want %v", slot, got, want)
		}
	}
}

// TestInvFromNBTDropsUnreadableEntries pins that InvFromNBT swallows every failure: a malformed entry, an entry naming
// an item this build does not know, and an entry whose slot is out of range all leave the inventory untouched with no
// error returned.
//
// PINNED BUG: item.ReadNBT's error and inv.SetItem's error are both discarded (item.go:16 and item.go:19). An item that
// this build cannot resolve is deleted from the chest on the next save, silently and irreversibly. The comment in
// item.go acknowledges this; the test pins it so a future fix has to change the test deliberately.
func TestInvFromNBTDropsUnreadableEntries(t *testing.T) {
	tests := []struct {
		name  string
		items []any
	}{
		{name: "nil entry", items: []any{nil}},
		{name: "entry of wrong type", items: []any{"not a compound"}},
		{name: "empty compound", items: []any{map[string]any{}}},
		{name: "unknown item name", items: []any{map[string]any{"Name": "minecraft:not_an_item", "Count": byte(1), "Slot": byte(0)}}},
		{name: "slot beyond inventory size", items: []any{map[string]any{"Name": "minecraft:diamond", "Count": byte(1), "Damage": int16(0), "Slot": byte(200)}}},
		{name: "count of wrong type", items: []any{map[string]any{"Name": "minecraft:diamond", "Count": int32(1), "Slot": byte(0)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := inventory.New(27, nil)
			nbtconv.InvFromNBT(inv, tt.items)
			for slot, it := range inv.Slots() {
				if !it.Empty() {
					t.Errorf("slot %d = %v, want empty", slot, it)
				}
			}
		})
	}
}

// TestInvFromNBTSlotKeyIsUint8 pins that the slot index is read through Uint8, so an entry storing "Slot" as int32 -
// the type a hand-built or foreign-format map would plausibly use - lands in slot 0 instead of the intended slot.
//
// PINNED BUG: a wrong-typed Slot silently overwrites slot 0. Two such entries in one chest overwrite each other and the
// remaining items are lost.
func TestInvFromNBTSlotKeyIsUint8(t *testing.T) {
	inv := inventory.New(27, nil)
	nbtconv.InvFromNBT(inv, []any{
		map[string]any{"Name": "minecraft:diamond", "Count": byte(1), "Damage": int16(0), "Slot": int32(5)},
	})
	it, err := inv.Item(0)
	if err != nil {
		t.Fatalf("read slot 0: %v", err)
	}
	if it.Empty() {
		t.Fatal("slot 0 is empty; expected the int32 Slot to have been read as 0")
	}
	if five, _ := inv.Item(5); !five.Empty() {
		t.Errorf("slot 5 = %v, want empty", five)
	}
}

var _ world.Block = block.Stone{}
