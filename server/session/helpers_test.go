package session

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The conversion helpers below sit on the packet path in both directions, so
// what is pinned is the axis order and the rounding, not just the arithmetic.
// Every expected value is written out literally; a swapped axis has to change a
// literal to keep the test green.

func TestVec32To64(t *testing.T) {
	tests := []struct {
		name string
		in   mgl32.Vec3
		want mgl64.Vec3
	}{
		{name: "zero", in: mgl32.Vec3{0, 0, 0}, want: mgl64.Vec3{0, 0, 0}},
		{
			name: "axes stay in order",
			in:   mgl32.Vec3{1, 2, 3},
			want: mgl64.Vec3{1, 2, 3},
		},
		{
			name: "negatives keep their sign",
			in:   mgl32.Vec3{-1, -2, -3},
			want: mgl64.Vec3{-1, -2, -3},
		},
		{
			// 0.1 is not representable in binary. Widening a float32 0.1 to
			// float64 exposes the float32 rounding error rather than hiding it,
			// so the widened value is not 0.1.
			name: "widening exposes the float32 rounding error",
			in:   mgl32.Vec3{0.1, 0.2, 0.3},
			want: mgl64.Vec3{float64(float32(0.1)), float64(float32(0.2)), float64(float32(0.3))},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vec32To64(test.in); got != test.want {
				t.Fatalf("vec32To64(%v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

func TestVec32To64NonFinite(t *testing.T) {
	// PINNED BUG: a client sends float32 positions. vec32To64 widens NaN and
	// the infinities without rejecting them, so a hostile PlayerAuthInput can
	// put a non-finite mgl64.Vec3 into server-side movement code. Pinned as-is;
	// decide in stage 1.
	got := vec32To64(mgl32.Vec3{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))})
	if !math.IsNaN(got[0]) {
		t.Fatalf("vec32To64 X = %v, want NaN", got[0])
	}
	if !math.IsInf(got[1], 1) {
		t.Fatalf("vec32To64 Y = %v, want +Inf", got[1])
	}
	if !math.IsInf(got[2], -1) {
		t.Fatalf("vec32To64 Z = %v, want -Inf", got[2])
	}
}

func TestVec64To32(t *testing.T) {
	tests := []struct {
		name string
		in   mgl64.Vec3
		want mgl32.Vec3
	}{
		{name: "zero", in: mgl64.Vec3{0, 0, 0}, want: mgl32.Vec3{0, 0, 0}},
		{
			name: "axes stay in order",
			in:   mgl64.Vec3{1, 2, 3},
			want: mgl32.Vec3{1, 2, 3},
		},
		{
			name: "narrowing rounds to nearest float32",
			in:   mgl64.Vec3{0.1, 0.2, 0.3},
			want: mgl32.Vec3{0.1, 0.2, 0.3},
		},
		{
			// PINNED BUG: narrowing a float64 beyond the float32 range yields
			// an infinity rather than a clamp or an error. Pinned as-is;
			// decide in stage 1.
			name: "overflow becomes an infinity",
			in:   mgl64.Vec3{math.MaxFloat64, -math.MaxFloat64, 0},
			want: mgl32.Vec3{float32(math.Inf(1)), float32(math.Inf(-1)), 0},
		},
		{
			// A float64 below the smallest float32 subnormal flushes to zero.
			name: "underflow becomes zero",
			in:   mgl64.Vec3{5e-324, -5e-324, 0},
			want: mgl32.Vec3{0, 0, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vec64To32(test.in); got != test.want {
				t.Fatalf("vec64To32(%v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

func TestVec2To32(t *testing.T) {
	tests := []struct {
		name string
		in   mgl64.Vec2
		want mgl32.Vec2
	}{
		{name: "zero", in: mgl64.Vec2{0, 0}, want: mgl32.Vec2{0, 0}},
		{name: "axes stay in order", in: mgl64.Vec2{1, 2}, want: mgl32.Vec2{1, 2}},
		{name: "negatives keep their sign", in: mgl64.Vec2{-1.5, -2.5}, want: mgl32.Vec2{-1.5, -2.5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vec2To32(test.in); got != test.want {
				t.Fatalf("vec2To32(%v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

// TestBlockPosFromProtocol pins the axis order of the protocol.BlockPos to
// cube.Pos conversion. protocol.BlockPos is a [3]int32 whose accessors are
// X, Y, Z in that order; every value below is distinct so that a swap of any
// two axes changes the result.
func TestBlockPosFromProtocol(t *testing.T) {
	tests := []struct {
		name string
		in   protocol.BlockPos
		want cube.Pos
	}{
		{name: "origin", in: protocol.BlockPos{0, 0, 0}, want: cube.Pos{0, 0, 0}},
		{
			name: "distinct axes are not swapped",
			in:   protocol.BlockPos{1, 2, 3},
			want: cube.Pos{1, 2, 3},
		},
		{
			name: "negatives keep their sign",
			in:   protocol.BlockPos{-1, -2, -3},
			want: cube.Pos{-1, -2, -3},
		},
		{
			// The client is free to send any int32 here. Widening to int keeps
			// the value; on a 32-bit build int is 32 bits wide and the value
			// still fits, so no truncation is possible either way.
			name: "int32 extremes widen without truncation",
			in:   protocol.BlockPos{math.MaxInt32, math.MinInt32, math.MaxInt32},
			want: cube.Pos{math.MaxInt32, math.MinInt32, math.MaxInt32},
		},
		{
			// Y is the only axis with a world range, and nothing here checks
			// it. Range validation is the caller's job.
			name: "out of range Y is not clamped",
			in:   protocol.BlockPos{0, 100000, 0},
			want: cube.Pos{0, 100000, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := blockPosFromProtocol(test.in); got != test.want {
				t.Fatalf("blockPosFromProtocol(%v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

func TestBoolByte(t *testing.T) {
	if got := boolByte(true); got != 1 {
		t.Fatalf("boolByte(true) = %d, want 1", got)
	}
	if got := boolByte(false); got != 0 {
		t.Fatalf("boolByte(false) = %d, want 0", got)
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero", in: 0, want: 0},
		{name: "positive is unchanged", in: 7, want: 7},
		{name: "negative is negated", in: -7, want: 7},
		{name: "minus one", in: -1, want: 1},
		{name: "max int", in: math.MaxInt, want: math.MaxInt},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := abs(test.in); got != test.want {
				t.Fatalf("abs(%d) = %d, want %d", test.in, got, test.want)
			}
		})
	}
}

// TestAbsMinIntOverflows pins the one input abs gets wrong. Negating the most
// negative int overflows back onto itself in two's complement.
//
// PINNED BUG: abs(math.MinInt) returns math.MinInt, a negative "absolute
// value". Pinned as-is; decide in stage 1.
func TestAbsMinIntOverflows(t *testing.T) {
	if got := abs(math.MinInt); got != math.MinInt {
		t.Fatalf("abs(math.MinInt) = %d, want %d (the overflowed value this pins)", got, math.MinInt)
	}
}

// TestEntityOffset pins that the client-side Y offset comes from the entity
// type, not the entity, and that it lands on the Y axis alone.
func TestEntityOffset(t *testing.T) {
	plain := newStubEntity(t, stubEntityType{name: "impulse:plain"})
	if got := entityOffset(plain); got != (mgl64.Vec3{}) {
		t.Fatalf("entityOffset of a type without NetworkOffset = %v, want the zero vector", got)
	}

	offset := newStubEntity(t, offsetEntityType{
		stubEntityType: stubEntityType{name: "impulse:offset"},
		offset:         1.62,
	})
	if got, want := entityOffset(offset), (mgl64.Vec3{0, 1.62, 0}); got != want {
		t.Fatalf("entityOffset = %v, want %v", got, want)
	}

	negative := newStubEntity(t, offsetEntityType{
		stubEntityType: stubEntityType{name: "impulse:negative"},
		offset:         -0.5,
	})
	if got, want := entityOffset(negative), (mgl64.Vec3{0, -0.5, 0}); got != want {
		t.Fatalf("entityOffset with a negative offset = %v, want %v", got, want)
	}
}

// TestTierToSoundEvent pins the armour tier to sound event mapping. The sound
// names are protocol strings the client resolves, so they are asserted
// literally rather than through the packet constants.
func TestTierToSoundEvent(t *testing.T) {
	tests := []struct {
		name string
		tier item.ArmourTier
		want string
	}{
		{name: "leather", tier: item.ArmourTierLeather{}, want: "armor.equip_leather"},
		{name: "copper", tier: item.ArmourTierCopper{}, want: "armor.equip_copper"},
		{name: "gold", tier: item.ArmourTierGold{}, want: "armor.equip_gold"},
		{name: "chain", tier: item.ArmourTierChain{}, want: "armor.equip_chain"},
		{name: "iron", tier: item.ArmourTierIron{}, want: "armor.equip_iron"},
		{name: "diamond", tier: item.ArmourTierDiamond{}, want: "armor.equip_diamond"},
		{name: "netherite", tier: item.ArmourTierNetherite{}, want: "armor.equip_netherite"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := tierToSoundEvent(test.tier); got != test.want {
				t.Fatalf("tierToSoundEvent(%T) = %q, want %q", test.tier, got, test.want)
			}
		})
	}
}

// TestTierToSoundEventDefaults pins the fallback. The switch is on the concrete
// type, so a dyed leather tier still matches ArmourTierLeather, but a tier
// defined outside server/item falls through to the generic sound. This is the
// enumeration default done right: a value, not a panic.
func TestTierToSoundEventDefaults(t *testing.T) {
	if got := tierToSoundEvent(dyedLeatherTier()); got != "armor.equip_leather" {
		t.Fatalf("tierToSoundEvent of a dyed leather tier = %q, want %q", got, "armor.equip_leather")
	}
	if got := tierToSoundEvent(foreignArmourTier{}); got != "armor.equip_generic" {
		t.Fatalf("tierToSoundEvent of an unknown tier = %q, want %q", got, "armor.equip_generic")
	}
	if got := tierToSoundEvent(nil); got != "armor.equip_generic" {
		t.Fatalf("tierToSoundEvent(nil) = %q, want %q", got, "armor.equip_generic")
	}
}

func dyedLeatherTier() item.ArmourTier {
	tier := item.ArmourTierLeather{}
	tier.Colour.R, tier.Colour.A = 255, 255
	return tier
}

// foreignArmourTier is an item.ArmourTier that the type switch in
// tierToSoundEvent does not name.
type foreignArmourTier struct{}

func (foreignArmourTier) BaseDurability() float64      { return 1 }
func (foreignArmourTier) Toughness() float64           { return 0 }
func (foreignArmourTier) KnockBackResistance() float64 { return 0 }
func (foreignArmourTier) EnchantmentValue() int        { return 0 }
func (foreignArmourTier) Name() string                 { return "impulse_foreign" }

func TestValueOrDefault(t *testing.T) {
	if got := valueOrDefault(0, 7); got != 7 {
		t.Fatalf("valueOrDefault(0, 7) = %d, want 7", got)
	}
	if got := valueOrDefault(3, 7); got != 3 {
		t.Fatalf("valueOrDefault(3, 7) = %d, want 3", got)
	}
	if got := valueOrDefault("", "fallback"); got != "fallback" {
		t.Fatalf("valueOrDefault(\"\", \"fallback\") = %q, want \"fallback\"", got)
	}
	// PINNED SCOPE: the zero value is indistinguishable from "unset", so an
	// explicit zero can never be kept. Callers such as debugShapeToProtocol
	// rely on that. Out of scope.
	if got := valueOrDefault(float32(0), float32(1)); got != 1 {
		t.Fatalf("valueOrDefault of an explicit zero = %v, want the default 1", got)
	}
}

// TestGameTypeFromMode pins the game mode to network game type mapping. The
// golden values are the protocol's own: survival 0, creative 1, adventure 2,
// spectator 6.
func TestGameTypeFromMode(t *testing.T) {
	if got := gameTypeFromMode(world.GameModeSurvival); got != 0 {
		t.Fatalf("gameTypeFromMode(survival) = %d, want %d", got, packet.GameTypeSurvival)
	}
	if got := gameTypeFromMode(world.GameModeCreative); got != 1 {
		t.Fatalf("gameTypeFromMode(creative) = %d, want %d", got, packet.GameTypeCreative)
	}

	// PINNED BUG: the mapping tests only AllowsFlying and CreativeInventory, so
	// adventure collapses onto survival (0) and spectator, which allows flying
	// but has no creative inventory, collapses onto survival as well. The
	// client therefore never receives game type 2 or 6 from this function.
	// Pinned as-is; decide in stage 1.
	if got := gameTypeFromMode(world.GameModeAdventure); got != 0 {
		t.Fatalf("gameTypeFromMode(adventure) = %d, want 0 (the collapse this pins)", got)
	}
	if got := gameTypeFromMode(world.GameModeSpectator); got != 0 {
		t.Fatalf("gameTypeFromMode(spectator) = %d, want 0 (the collapse this pins)", got)
	}
}

// TestGameTypeFromModeGoldenConstants pins the numeric game types themselves,
// so that a gophertunnel bump that renumbers them is caught here rather than
// silently changing what the client is told.
func TestGameTypeFromModeGoldenConstants(t *testing.T) {
	golden := map[string]int32{
		"survival":  0,
		"creative":  1,
		"adventure": 2,
		"spectator": 6,
	}
	got := map[string]int32{
		"survival":  packet.GameTypeSurvival,
		"creative":  packet.GameTypeCreative,
		"adventure": packet.GameTypeAdventure,
		"spectator": packet.GameTypeSpectator,
	}
	for name, want := range golden {
		if got[name] != want {
			t.Fatalf("packet game type %s = %d, want %d", name, got[name], want)
		}
	}
}
