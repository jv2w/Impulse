package session

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/entity"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/potion"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Entity metadata is a one-way conversion: nothing reads it back, so a round
// trip cannot catch a mistake here. What is pinned instead is the literal wire
// contract -- which metadata key holds which value, and which bit index inside
// the flag words each entity state occupies. Both are numbers the client hard
// codes, so a change in either is a protocol break, not a refactor.

// Golden metadata keys. protocol.EntityDataKey* is an iota block; these are the
// values it currently produces for the keys this package writes.
const (
	goldenKeyFlags              = 0
	goldenKeyVariant            = 2
	goldenKeyColorIndex         = 3
	goldenKeyName               = 4
	goldenKeyOwner              = 5
	goldenKeyAirSupply          = 7
	goldenKeyEffectColor        = 8
	goldenKeyEffectAmbience     = 9
	goldenKeyValue              = 15
	goldenKeyCustomDisplay      = 18
	goldenKeyPlayerFlags        = 26
	goldenKeyBedPosition        = 28
	goldenKeyAuxValueData       = 36
	goldenKeyScale              = 38
	goldenKeyAirSupplyMax       = 42
	goldenKeyMarkVariant        = 43
	goldenKeyBlockTarget        = 47
	goldenKeyWidth              = 53
	goldenKeyHeight             = 54
	goldenKeyFuseTime           = 55
	goldenKeyDataRadius         = 61
	goldenKeyAlwaysShowNameTag  = 81
	goldenKeyScore              = 84
	goldenKeyFlagsTwo           = 92
	goldenKeyDataDuration       = 95
	goldenKeyDataChangeRate     = 97
	goldenKeyDataChangeOnPickup = 98
	goldenKeyLastDeathPosition  = 127
	goldenKeyLastDeathDimension = 128
	goldenKeyPlayerHasDied      = 129
	goldenKeyVisibleMobEffects  = 131

	// DisplayFirework is an alias for DisplayBlockState rather than a key of
	// its own, so a firework's display NBT and a falling block's state share
	// one slot.
	goldenKeyDisplayFirework = 16
)

// Golden flag bit indices inside EntityDataKeyFlags.
const (
	goldenFlagOnFire         = 0
	goldenFlagSneaking       = 1
	goldenFlagSprinting      = 3
	goldenFlagUsingItem      = 4
	goldenFlagInvisible      = 5
	goldenFlagIgnited        = 10
	goldenFlagBaby           = 11
	goldenFlagCritical       = 13
	goldenFlagShowName       = 14
	goldenFlagAlwaysShowName = 15
	goldenFlagNoAI           = 16
	goldenFlagClimb          = 19
	goldenFlagGliding        = 32
	goldenFlagBreathing      = 35
	goldenFlagShowBottom     = 38
	goldenFlagLingering      = 47
	goldenFlagHasCollision   = 48
	goldenFlagHasGravity     = 49
	goldenFlagEnchanted      = 52
	goldenFlagSwimming       = 57

	// Crawling is flag 114, which lives in the second flag word. The code
	// masks it with 63, landing it on bit 50 of EntityDataKeyFlagsTwo.
	goldenFlagCrawlingRaw    = 114
	goldenFlagCrawlingMasked = 50
)

// TestEntityMetadataGoldenKeysAndFlags pins the numbers themselves. Everything
// else in this file is written in terms of the protocol constants; this test is
// the one place that asserts those constants still hold the values the client
// expects, so a gophertunnel bump that renumbers a key fails here first.
func TestEntityMetadataGoldenKeysAndFlags(t *testing.T) {
	keys := map[string][2]uint32{
		"Flags":                   {protocol.EntityDataKeyFlags, goldenKeyFlags},
		"Variant":                 {protocol.EntityDataKeyVariant, goldenKeyVariant},
		"ColorIndex":              {protocol.EntityDataKeyColorIndex, goldenKeyColorIndex},
		"Name":                    {protocol.EntityDataKeyName, goldenKeyName},
		"Owner":                   {protocol.EntityDataKeyOwner, goldenKeyOwner},
		"AirSupply":               {protocol.EntityDataKeyAirSupply, goldenKeyAirSupply},
		"EffectColor":             {protocol.EntityDataKeyEffectColor, goldenKeyEffectColor},
		"EffectAmbience":          {protocol.EntityDataKeyEffectAmbience, goldenKeyEffectAmbience},
		"Value":                   {protocol.EntityDataKeyValue, goldenKeyValue},
		"CustomDisplay":           {protocol.EntityDataKeyCustomDisplay, goldenKeyCustomDisplay},
		"PlayerFlags":             {protocol.EntityDataKeyPlayerFlags, goldenKeyPlayerFlags},
		"BedPosition":             {protocol.EntityDataKeyBedPosition, goldenKeyBedPosition},
		"AuxValueData":            {protocol.EntityDataKeyAuxValueData, goldenKeyAuxValueData},
		"Scale":                   {protocol.EntityDataKeyScale, goldenKeyScale},
		"AirSupplyMax":            {protocol.EntityDataKeyAirSupplyMax, goldenKeyAirSupplyMax},
		"MarkVariant":             {protocol.EntityDataKeyMarkVariant, goldenKeyMarkVariant},
		"BlockTarget":             {protocol.EntityDataKeyBlockTarget, goldenKeyBlockTarget},
		"Width":                   {protocol.EntityDataKeyWidth, goldenKeyWidth},
		"Height":                  {protocol.EntityDataKeyHeight, goldenKeyHeight},
		"FuseTime":                {protocol.EntityDataKeyFuseTime, goldenKeyFuseTime},
		"DataRadius":              {protocol.EntityDataKeyDataRadius, goldenKeyDataRadius},
		"AlwaysShowNameTag":       {protocol.EntityDataKeyAlwaysShowNameTag, goldenKeyAlwaysShowNameTag},
		"Score":                   {protocol.EntityDataKeyScore, goldenKeyScore},
		"FlagsTwo":                {protocol.EntityDataKeyFlagsTwo, goldenKeyFlagsTwo},
		"DataDuration":            {protocol.EntityDataKeyDataDuration, goldenKeyDataDuration},
		"DataChangeRate":          {protocol.EntityDataKeyDataChangeRate, goldenKeyDataChangeRate},
		"DataChangeOnPickup":      {protocol.EntityDataKeyDataChangeOnPickup, goldenKeyDataChangeOnPickup},
		"PlayerLastDeathPosition": {protocol.EntityDataKeyPlayerLastDeathPosition, goldenKeyLastDeathPosition},
		"PlayerLastDeathDim":      {protocol.EntityDataKeyPlayerLastDeathDimension, goldenKeyLastDeathDimension},
		"PlayerHasDied":           {protocol.EntityDataKeyPlayerHasDied, goldenKeyPlayerHasDied},
		"VisibleMobEffects":       {protocol.EntityDataKeyVisibleMobEffects, goldenKeyVisibleMobEffects},
		"DisplayFirework":         {protocol.EntityDataKeyDisplayFirework, goldenKeyDisplayFirework},
	}
	for name, pair := range keys {
		if pair[0] != pair[1] {
			t.Errorf("EntityDataKey%s = %d, want the golden %d", name, pair[0], pair[1])
		}
	}

	flags := map[string][2]uint8{
		"OnFire":         {protocol.EntityDataFlagOnFire, goldenFlagOnFire},
		"Sneaking":       {protocol.EntityDataFlagSneaking, goldenFlagSneaking},
		"Sprinting":      {protocol.EntityDataFlagSprinting, goldenFlagSprinting},
		"UsingItem":      {protocol.EntityDataFlagUsingItem, goldenFlagUsingItem},
		"Invisible":      {protocol.EntityDataFlagInvisible, goldenFlagInvisible},
		"Ignited":        {protocol.EntityDataFlagIgnited, goldenFlagIgnited},
		"Baby":           {protocol.EntityDataFlagBaby, goldenFlagBaby},
		"Critical":       {protocol.EntityDataFlagCritical, goldenFlagCritical},
		"ShowName":       {protocol.EntityDataFlagShowName, goldenFlagShowName},
		"AlwaysShowName": {protocol.EntityDataFlagAlwaysShowName, goldenFlagAlwaysShowName},
		"NoAI":           {protocol.EntityDataFlagNoAI, goldenFlagNoAI},
		"Climb":          {protocol.EntityDataFlagClimb, goldenFlagClimb},
		"Gliding":        {protocol.EntityDataFlagGliding, goldenFlagGliding},
		"Breathing":      {protocol.EntityDataFlagBreathing, goldenFlagBreathing},
		"ShowBottom":     {protocol.EntityDataFlagShowBottom, goldenFlagShowBottom},
		"Lingering":      {protocol.EntityDataFlagLingering, goldenFlagLingering},
		"HasCollision":   {protocol.EntityDataFlagHasCollision, goldenFlagHasCollision},
		"HasGravity":     {protocol.EntityDataFlagHasGravity, goldenFlagHasGravity},
		"Enchanted":      {protocol.EntityDataFlagEnchanted, goldenFlagEnchanted},
		"Swimming":       {protocol.EntityDataFlagSwimming, goldenFlagSwimming},
		"Crawling":       {protocol.EntityDataFlagCrawling, goldenFlagCrawlingRaw},
	}
	for name, pair := range flags {
		if pair[0] != pair[1] {
			t.Errorf("EntityDataFlag%s = %d, want the golden %d", name, pair[0], pair[1])
		}
	}
	if got := protocol.EntityDataFlagCrawling & 63; got != goldenFlagCrawlingMasked {
		t.Errorf("EntityDataFlagCrawling&63 = %d, want the golden %d", got, goldenFlagCrawlingMasked)
	}
}

// bit returns the flag word with only the bit index passed set.
func bit(index uint8) int64 { return 1 << int64(index) }

// TestAddSpecificMetadataFlags pins each entity state to the exact flag word it
// produces. The expectation is the whole int64, not a bit test, so setting an
// extra bit fails just as loudly as setting the wrong one.
func TestAddSpecificMetadataFlags(t *testing.T) {
	tests := []struct {
		name         string
		e            any
		wantFlags    int64
		wantFlagsTwo int64
	}{
		{name: "nothing implemented", e: struct{}{}},
		{name: "sneaking", e: stubSneaker{true}, wantFlags: bit(goldenFlagSneaking)},
		{name: "not sneaking", e: stubSneaker{false}},
		{name: "sprinting", e: stubSprinter{true}, wantFlags: bit(goldenFlagSprinting)},
		{name: "swimming", e: stubSwimmer{true}, wantFlags: bit(goldenFlagSwimming)},
		{name: "crawling lands in the second flag word", e: stubCrawler{true}, wantFlagsTwo: bit(goldenFlagCrawlingMasked)},
		{name: "gliding", e: stubGlider{true}, wantFlags: bit(goldenFlagGliding)},
		{name: "baby", e: stubBaby{true}, wantFlags: bit(goldenFlagBaby)},
		{name: "invisible", e: stubInvisible{true}, wantFlags: bit(goldenFlagInvisible)},
		{name: "immobile sets NoAI", e: stubImmobile{true}, wantFlags: bit(goldenFlagNoAI)},
		{name: "on fire", e: stubOnFire{time.Second}, wantFlags: bit(goldenFlagOnFire)},
		{name: "fire duration zero is not on fire", e: stubOnFire{0}},
		{name: "using item", e: stubUsing{true}, wantFlags: bit(goldenFlagUsingItem)},
		{name: "critical arrow", e: stubArrow{true}, wantFlags: bit(goldenFlagCritical)},
		{
			name:      "survival game mode has collision",
			e:         stubGameMode{world.GameModeSurvival},
			wantFlags: bit(goldenFlagHasCollision),
		},
		{
			// Spectator is the one vanilla mode without collision.
			name: "spectator game mode has no collision",
			e:    stubGameMode{world.GameModeSpectator},
		},
		{
			name:      "breathing",
			e:         stubBreather{breathing: true},
			wantFlags: bit(goldenFlagBreathing),
		},
		{name: "not breathing", e: stubBreather{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			m := protocol.NewEntityMetadata()
			s.addSpecificMetadata(test.e, m)

			if got := m[protocol.EntityDataKeyFlags].(int64); got != test.wantFlags {
				t.Fatalf("flags = %#b, want %#b", got, test.wantFlags)
			}
			if got := m[protocol.EntityDataKeyFlagsTwo].(int64); got != test.wantFlagsTwo {
				t.Fatalf("flags two = %#b, want %#b", got, test.wantFlagsTwo)
			}
		})
	}
}

// TestAddSpecificMetadataFlagCombinations pins that combined states OR together
// rather than overwriting one another, and that the two flag words stay
// independent.
func TestAddSpecificMetadataFlagCombinations(t *testing.T) {
	tests := []struct {
		name         string
		e            any
		wantFlags    int64
		wantFlagsTwo int64
	}{
		{
			name:      "burning and invisible",
			e:         stubCombined{onFire: time.Second, invisible: true},
			wantFlags: bit(goldenFlagOnFire) | bit(goldenFlagInvisible),
		},
		{
			name:      "sneaking and swimming",
			e:         stubCombined{sneaking: true, swimming: true},
			wantFlags: bit(goldenFlagSneaking) | bit(goldenFlagSwimming),
		},
		{
			name:         "sneaking and crawling split across both words",
			e:            stubCombined{sneaking: true, crawling: true},
			wantFlags:    bit(goldenFlagSneaking),
			wantFlagsTwo: bit(goldenFlagCrawlingMasked),
		},
		{
			// Nothing rejects contradictory states: sprinting while sneaking
			// while swimming sets all three bits at once.
			name:      "contradictory states are all set",
			e:         stubCombined{sneaking: true, sprinting: true, swimming: true},
			wantFlags: bit(goldenFlagSneaking) | bit(goldenFlagSprinting) | bit(goldenFlagSwimming),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			m := protocol.NewEntityMetadata()
			s.addSpecificMetadata(test.e, m)

			if got := m[protocol.EntityDataKeyFlags].(int64); got != test.wantFlags {
				t.Fatalf("flags = %#b, want %#b", got, test.wantFlags)
			}
			if got := m[protocol.EntityDataKeyFlagsTwo].(int64); got != test.wantFlagsTwo {
				t.Fatalf("flags two = %#b, want %#b", got, test.wantFlagsTwo)
			}
		})
	}
}

// TestAddSpecificMetadataSleeping pins the sleeping state, which is the only
// one written into EntityDataKeyPlayerFlags. gophertunnel has no named constant
// for that bit, so the code writes a literal 1.
func TestAddSpecificMetadataSleeping(t *testing.T) {
	s := newStubSession(t, nil)
	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubSleeper{pos: cube.Pos{1, 2, 3}, sleeping: true}, m)

	if got, want := m[protocol.EntityDataKeyBedPosition], (protocol.BlockPos{1, 2, 3}); got != want {
		t.Fatalf("bed position = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyPlayerFlags].(byte), byte(1<<1); got != want {
		t.Fatalf("player flags = %#b, want %#b", got, want)
	}
	if got := m[protocol.EntityDataKeyFlags].(int64); got != 0 {
		t.Fatalf("sleeping set a bit in the main flag word: %#b", got)
	}

	awake := newStubSession(t, nil)
	m = protocol.NewEntityMetadata()
	awake.addSpecificMetadata(stubSleeper{pos: cube.Pos{1, 2, 3}}, m)
	if _, ok := m[protocol.EntityDataKeyBedPosition]; ok {
		t.Fatal("an awake sleeper still wrote a bed position")
	}
	if got := m[protocol.EntityDataKeyPlayerFlags].(byte); got != 0 {
		t.Fatalf("an awake sleeper set player flags to %#b", got)
	}
}

// TestAddSpecificMetadataValues pins the non-flag metadata: which key each
// value lands in and what Go type it is written as. The type matters because
// the metadata is serialised by type, not by key.
func TestAddSpecificMetadataValues(t *testing.T) {
	tests := []struct {
		name string
		e    any
		want map[uint32]any
	}{
		{
			name: "breather converts durations to ticks",
			e:    stubBreather{air: 15 * time.Second, maxAir: 20 * time.Second},
			want: map[uint32]any{
				protocol.EntityDataKeyAirSupply:    int16(300),
				protocol.EntityDataKeyAirSupplyMax: int16(400),
			},
		},
		{
			// A duration that is not a whole number of ticks truncates towards
			// zero: 49ms of a 50ms tick is nothing.
			name: "sub-tick air supply truncates to zero",
			e:    stubBreather{air: 49 * time.Millisecond, maxAir: 99 * time.Millisecond},
			want: map[uint32]any{
				protocol.EntityDataKeyAirSupply:    int16(0),
				protocol.EntityDataKeyAirSupplyMax: int16(1),
			},
		},
		{
			name: "experience orb",
			e:    stubOrb{7},
			want: map[uint32]any{protocol.EntityDataKeyValue: int32(7)},
		},
		{
			name: "scale is a float32",
			e:    stubScaled{0.5},
			want: map[uint32]any{protocol.EntityDataKeyScale: float32(0.5)},
		},
		{
			name: "score tag",
			e:    stubScoreTag{"12"},
			want: map[uint32]any{protocol.EntityDataKeyScore: "12"},
		},
		{
			name: "variant",
			e:    stubVariable{3},
			want: map[uint32]any{protocol.EntityDataKeyVariant: int32(3)},
		},
		{
			name: "mark variant",
			e:    stubMarkVariable{4},
			want: map[uint32]any{protocol.EntityDataKeyMarkVariant: int32(4)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			m := protocol.NewEntityMetadata()
			s.addSpecificMetadata(test.e, m)

			for key, want := range test.want {
				got, ok := m[key]
				if !ok {
					t.Fatalf("metadata key %d was not written", key)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("metadata key %d = %v (%T), want %v (%T)", key, got, got, want, want)
				}
			}
		})
	}
}

// TestAddSpecificMetadataTNT pins that a primed TNT both reports its fuse in
// ticks and lights the ignited flag, and that the flag is unconditional: a fuse
// of zero still ignites.
func TestAddSpecificMetadataTNT(t *testing.T) {
	s := newStubSession(t, nil)
	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubTNT{4 * time.Second}, m)

	if got, want := m[protocol.EntityDataKeyFuseTime], int32(80); got != want {
		t.Fatalf("fuse time = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyFlags].(int64), bit(goldenFlagIgnited); got != want {
		t.Fatalf("flags = %#b, want %#b", got, want)
	}

	zero := newStubSession(t, nil)
	m = protocol.NewEntityMetadata()
	zero.addSpecificMetadata(stubTNT{0}, m)
	if got, want := m[protocol.EntityDataKeyFlags].(int64), bit(goldenFlagIgnited); got != want {
		t.Fatalf("a zero fuse gave flags %#b, want the ignited bit %#b", got, want)
	}
}

// TestAddSpecificMetadataEndCrystal pins that the crystal's base flag is both
// set and explicitly unset, and that the beam target only appears when there is
// one.
func TestAddSpecificMetadataEndCrystal(t *testing.T) {
	s := newStubSession(t, nil)
	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubEndCrystal{base: true, target: cube.Pos{-1, 2, -3}, hasTarget: true}, m)

	if got, want := m[protocol.EntityDataKeyFlags].(int64), bit(goldenFlagShowBottom); got != want {
		t.Fatalf("flags = %#b, want %#b", got, want)
	}
	if got, want := m[protocol.EntityDataKeyBlockTarget], (protocol.BlockPos{-1, 2, -3}); got != want {
		t.Fatalf("block target = %v, want %v", got, want)
	}

	hidden := newStubSession(t, nil)
	m = protocol.NewEntityMetadata()
	hidden.addSpecificMetadata(stubEndCrystal{}, m)
	if got := m[protocol.EntityDataKeyFlags].(int64); got != 0 {
		t.Fatalf("a crystal without a base gave flags %#b, want 0", got)
	}
	if _, ok := m[protocol.EntityDataKeyBlockTarget]; ok {
		t.Fatal("a crystal without a beam target still wrote one")
	}
}

// TestAddSpecificMetadataAreaEffectCloud pins the deliberately invalid values
// the cloud is given to stop the client shrinking it, and the effect colour it
// derives.
func TestAddSpecificMetadataAreaEffectCloud(t *testing.T) {
	s := newStubSession(t, nil)
	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubAreaEffectCloud{
		radius:  2.5,
		effects: []effect.Effect{effect.New(effect.Speed, 1, time.Minute)},
	}, m)

	if got, want := m[protocol.EntityDataKeyDataRadius], float32(2.5); got != want {
		t.Fatalf("radius = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyDataDuration], int32(math.MaxInt32); got != want {
		t.Fatalf("duration = %v, want the sentinel %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyDataChangeOnPickup], float32(math.SmallestNonzeroFloat32); got != want {
		t.Fatalf("change on pickup = %v, want the sentinel %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyDataChangeRate], float32(math.SmallestNonzeroFloat32); got != want {
		t.Fatalf("change rate = %v, want the sentinel %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyEffectAmbience], byte(0); got != want {
		t.Fatalf("effect ambience = %v, want %v", got, want)
	}
	// nbtconv packs the effect's RGBA into an int32; the sign comes from the
	// opaque alpha channel landing in the top byte.
	if got, want := m[protocol.EntityDataKeyEffectColor], int32(-13374465); got != want {
		t.Fatalf("effect colour = %v, want %v", got, want)
	}

	ambient := newStubSession(t, nil)
	m = protocol.NewEntityMetadata()
	ambient.addSpecificMetadata(stubAreaEffectCloud{
		effects: []effect.Effect{effect.NewAmbient(effect.Speed, 1, time.Minute)},
	}, m)
	if got, want := m[protocol.EntityDataKeyEffectAmbience], byte(1); got != want {
		t.Fatalf("ambient cloud ambience = %v, want %v", got, want)
	}
}

// TestAddSpecificMetadataVisibleEffects pins the seven-bit packing of visible
// effects: each effect shifts the accumulator left by seven and ORs in its ID
// doubled, with the low bit marking an ambient effect.
func TestAddSpecificMetadataVisibleEffects(t *testing.T) {
	tests := []struct {
		name    string
		effects []effect.Effect
		want    int64
	}{
		{name: "none", effects: nil, want: 0},
		{
			// Speed is registered as ID 1: 1<<1 == 2.
			name:    "one effect",
			effects: []effect.Effect{effect.New(effect.Speed, 1, time.Minute)},
			want:    2,
		},
		{
			name:    "ambient sets the low bit",
			effects: []effect.Effect{effect.NewAmbient(effect.Speed, 1, time.Minute)},
			want:    3,
		},
		{
			// Speed (1) then Regeneration (10): (2 << 7) | (10 << 1) == 276.
			name: "two effects pack in order",
			effects: []effect.Effect{
				effect.New(effect.Speed, 1, time.Minute),
				effect.New(effect.Regeneration, 1, time.Minute),
			},
			want: 276,
		},
		{
			name: "hidden particles are skipped entirely",
			effects: []effect.Effect{
				effect.New(effect.Speed, 1, time.Minute).WithoutParticles(),
				effect.New(effect.Regeneration, 1, time.Minute),
			},
			want: 20,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			m := protocol.NewEntityMetadata()
			s.addSpecificMetadata(stubEffectBearer{test.effects}, m)

			if got, want := m[protocol.EntityDataKeyVisibleMobEffects], test.want; got != want {
				t.Fatalf("visible mob effects = %v, want %v", got, want)
			}
		})
	}
}

// TestAddSpecificMetadataVisibleEffectsOverflow pins what happens past the
// ninth effect. Each effect consumes seven bits of an int64, so nine is all
// that fits and the tenth shifts the first one out.
//
// PINNED BUG: with ten or more visible effects the packing silently discards
// the oldest entries, and an ambient tenth-from-last effect leaves its low bit
// in the sign bit, making the value negative. Pinned as-is; decide in stage 1.
func TestAddSpecificMetadataVisibleEffectsOverflow(t *testing.T) {
	pack := func(t *testing.T, effects []effect.Effect) int64 {
		t.Helper()
		s := newStubSession(t, nil)
		m := protocol.NewEntityMetadata()
		s.addSpecificMetadata(stubEffectBearer{effects}, m)
		return m[protocol.EntityDataKeyVisibleMobEffects].(int64)
	}
	tail := func() []effect.Effect {
		var effects []effect.Effect
		for range 9 {
			effects = append(effects, effect.New(effect.Regeneration, 1, time.Minute))
		}
		return effects
	}

	// The tenth-from-last effect contributes only its ambient bit; its ID is
	// shifted clean out of the int64.
	withSpeed := pack(t, append([]effect.Effect{effect.New(effect.Speed, 1, time.Minute)}, tail()...))
	withDarkness := pack(t, append([]effect.Effect{effect.New(effect.Darkness, 1, time.Minute)}, tail()...))
	if withSpeed != withDarkness {
		t.Fatalf("the tenth-from-last effect still changed the packing: %v vs %v", withSpeed, withDarkness)
	}
	if withSpeed != pack(t, tail()) {
		t.Fatalf("dropping the tenth-from-last effect changed the packing: %v vs %v",
			withSpeed, pack(t, tail()))
	}

	// Its ambient bit, however, survives -- straight into the sign bit.
	ambient := pack(t, append([]effect.Effect{effect.NewAmbient(effect.Speed, 1, time.Minute)}, tail()...))
	if ambient >= 0 {
		t.Fatalf("an ambient tenth-from-last effect packed to %v, want a negative value", ambient)
	}
	if ambient != withSpeed+math.MinInt64 {
		t.Fatalf("ambient packing = %v, want %v", ambient, withSpeed+math.MinInt64)
	}
}

// TestAddSpecificMetadataSplashPotion pins the potion tip encoding. Potions
// with an ID above 4 also get a custom display value one higher than the ID.
func TestAddSpecificMetadataSplashPotion(t *testing.T) {
	tests := []struct {
		name              string
		id                int32
		wantAux           int16
		wantCustomDisplay any
	}{
		{name: "water has no tip", id: 0, wantAux: 0},
		{name: "id four is still untipped", id: 4, wantAux: 4},
		{name: "id five is tipped", id: 5, wantAux: 5, wantCustomDisplay: uint8(6)},
		{name: "high id", id: 42, wantAux: 42, wantCustomDisplay: uint8(43)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			m := protocol.NewEntityMetadata()
			s.addSpecificMetadata(stubSplash{potion.From(test.id)}, m)

			if got, want := m[protocol.EntityDataKeyAuxValueData], test.wantAux; got != want {
				t.Fatalf("aux value = %v, want %v", got, want)
			}
			got, ok := m[protocol.EntityDataKeyCustomDisplay]
			if test.wantCustomDisplay == nil {
				if ok {
					t.Fatalf("custom display was written as %v for an untipped potion", got)
				}
				return
			}
			if !reflect.DeepEqual(got, test.wantCustomDisplay) {
				t.Fatalf("custom display = %v (%T), want %v (%T)",
					got, got, test.wantCustomDisplay, test.wantCustomDisplay)
			}
		})
	}
}

// TestAddSpecificMetadataSplashPotionTipWraps pins the highest potion ID.
//
// PINNED BUG: the tip is computed as a uint8 addition, so potion 255 writes a
// custom display of 0 rather than 256, and the value written is a uint8 while
// every other writer of EntityDataKeyCustomDisplay writes an int64. Pinned
// as-is; decide in stage 1.
func TestAddSpecificMetadataSplashPotionTipWraps(t *testing.T) {
	s := newStubSession(t, nil)
	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubSplash{potion.From(255)}, m)

	if got, want := m[protocol.EntityDataKeyAuxValueData], int16(255); got != want {
		t.Fatalf("aux value = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyCustomDisplay], uint8(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("custom display = %v (%T), want the wrapped %v (%T)", got, got, want, want)
	}
}

// TestAddSpecificMetadataOwner pins that an owner is reported as the runtime ID
// the session has assigned it, and that a nil owner writes nothing.
func TestAddSpecificMetadataOwner(t *testing.T) {
	s := newStubSession(t, nil)
	owner := newStubEntity(t, stubEntityType{name: "impulse:owner"})
	s.entityRuntimeIDs[owner.H()] = 42
	s.entities[42] = owner.H()

	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubOwned{owner.H()}, m)
	if got, want := m[protocol.EntityDataKeyOwner], int64(42); got != want {
		t.Fatalf("owner = %v, want %v", got, want)
	}

	none := newStubSession(t, nil)
	m = protocol.NewEntityMetadata()
	none.addSpecificMetadata(stubOwned{}, m)
	if _, ok := m[protocol.EntityDataKeyOwner]; ok {
		t.Fatal("a nil owner still wrote an owner key")
	}
}

// TestAddSpecificMetadataUnknownOwner pins what a viewer is told about an owner
// the session has never been shown.
//
// PINNED BUG: handleRuntimeID logs and returns 0 for an unknown handle, so the
// owner key is written as runtime ID 0. No entity ever has that ID -- the
// session's own player is 1 -- so the client is handed a dangling reference
// rather than the key being left out. Pinned as-is; decide in stage 1.
func TestAddSpecificMetadataUnknownOwner(t *testing.T) {
	s := newStubSession(t, nil)
	owner := newStubEntity(t, stubEntityType{name: "impulse:owner"})

	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubOwned{owner.H()}, m)
	if got, want := m[protocol.EntityDataKeyOwner], int64(0); got != want {
		t.Fatalf("owner = %v, want the dangling %v this pins", got, want)
	}
	if _, ok := s.entityRuntimeIDs[owner.H()]; ok {
		t.Fatal("an unknown owner was registered as a side effect of reading it")
	}
}

// TestAddSpecificMetadataFireworkOwner pins that a firework writes its owner to
// CustomDisplay rather than Owner, and only while attached.
func TestAddSpecificMetadataFireworkOwner(t *testing.T) {
	owner := newStubEntity(t, stubEntityType{name: "impulse:owner"})

	attached := newStubSession(t, nil)
	attached.entityRuntimeIDs[owner.H()] = 9
	attached.entities[9] = owner.H()
	m := protocol.NewEntityMetadata()
	attached.addSpecificMetadata(stubFirework{attached: true, owner: owner.H()}, m)
	if got, want := m[protocol.EntityDataKeyCustomDisplay], int64(9); got != want {
		t.Fatalf("custom display = %v, want %v", got, want)
	}
	if _, ok := m[protocol.EntityDataKeyOwner]; ok {
		t.Fatal("a firework wrote the owner key as well as custom display")
	}
	if _, ok := m[protocol.EntityDataKeyDisplayFirework]; !ok {
		t.Fatal("a firework did not write its display NBT")
	}

	// PINNED SCOPE: the firework branch is an if/else with the plain owner
	// branch, so a detached firework reports no owner at all rather than
	// falling back to EntityDataKeyOwner. Out of scope.
	detached := newStubSession(t, nil)
	detached.entityRuntimeIDs[owner.H()] = 9
	m = protocol.NewEntityMetadata()
	detached.addSpecificMetadata(stubFirework{owner: owner.H()}, m)
	if _, ok := m[protocol.EntityDataKeyCustomDisplay]; ok {
		t.Fatal("a detached firework wrote a custom display")
	}
	if _, ok := m[protocol.EntityDataKeyOwner]; ok {
		t.Fatal("a detached firework fell back to the owner key")
	}
}

// TestAddSpecificMetadataDeathPosition pins that the last death position is
// written only for the session's own entity: it is a client-side respawn
// compass hint and means nothing for anyone else.
func TestAddSpecificMetadataDeathPosition(t *testing.T) {
	s := newStubSession(t, nil)
	self := newStubEntity(t, stubEntityType{name: "impulse:self"})
	s.ent = self.H()

	m := protocol.NewEntityMetadata()
	s.addSpecificMetadata(stubLiving{
		uuid:     self.H().UUID(),
		deathPos: mgl64.Vec3{1, 2, 3},
		deathDim: world.Nether,
		died:     true,
	}, m)
	if got, want := m[protocol.EntityDataKeyPlayerLastDeathPosition], (mgl32.Vec3{1, 2, 3}); got != want {
		t.Fatalf("last death position = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyPlayerLastDeathDimension], int32(1); got != want {
		t.Fatalf("last death dimension = %v, want the nether ID %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyPlayerHasDied], uint8(1); got != want {
		t.Fatalf("has died = %v, want %v", got, want)
	}

	alive := newStubSession(t, nil)
	alive.ent = self.H()
	m = protocol.NewEntityMetadata()
	alive.addSpecificMetadata(stubLiving{uuid: self.H().UUID()}, m)
	if _, ok := m[protocol.EntityDataKeyPlayerLastDeathPosition]; ok {
		t.Fatal("a living entity that has not died wrote a death position")
	}
	if got, want := m[protocol.EntityDataKeyPlayerHasDied], uint8(0); got != want {
		t.Fatalf("has died = %v, want %v", got, want)
	}

	other := newStubSession(t, nil)
	other.ent = self.H()
	m = protocol.NewEntityMetadata()
	other.addSpecificMetadata(stubLiving{uuid: uuid.New(), died: true}, m)
	if _, ok := m[protocol.EntityDataKeyPlayerHasDied]; ok {
		t.Fatal("another player's death state was written into their metadata")
	}

	// PINNED CONSTRAINT: the branch is guarded on s.ent being non-nil, so a
	// Session that has not yet been handed a handle writes nothing at all.
	unattached := newStubSession(t, nil)
	m = protocol.NewEntityMetadata()
	unattached.addSpecificMetadata(stubLiving{uuid: self.H().UUID(), died: true}, m)
	if _, ok := m[protocol.EntityDataKeyPlayerHasDied]; ok {
		t.Fatal("a session without a handle wrote a death state")
	}
}

// TestNameTagState pins the three-way answer the name tag helper gives: the
// tag, whether it shows at all distances, and whether the entity has one.
func TestNameTagState(t *testing.T) {
	tests := []struct {
		name           string
		e              any
		wantTag        string
		wantAlwaysShow bool
		wantNamed      bool
	}{
		{
			name:           "nothing implemented defaults to always show",
			e:              struct{}{},
			wantAlwaysShow: true,
		},
		{
			name:           "named without an always show state",
			e:              stubNamed{"Steve"},
			wantTag:        "Steve",
			wantAlwaysShow: true,
			wantNamed:      true,
		},
		{
			name:      "named and hidden at a distance",
			e:         stubNamedAlways{name: "Steve"},
			wantTag:   "Steve",
			wantNamed: true,
		},
		{
			name:           "named and always shown",
			e:              stubNamedAlways{name: "Steve", always: true},
			wantTag:        "Steve",
			wantAlwaysShow: true,
			wantNamed:      true,
		},
		{
			// PINNED SCOPE: an entity that reports an always show state but no
			// name is reported as unnamed, and the state is still returned.
			// Out of scope.
			name: "always show without a name",
			e:    stubAlwaysShow{true},

			wantAlwaysShow: true,
		},
		{
			name:      "empty name is still a name",
			e:         stubNamed{""},
			wantTag:   "",
			wantNamed: true,

			wantAlwaysShow: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tag, always, named := nameTagState(test.e)
			if tag != test.wantTag || always != test.wantAlwaysShow || named != test.wantNamed {
				t.Fatalf("nameTagState = (%q, %v, %v), want (%q, %v, %v)",
					tag, always, named, test.wantTag, test.wantAlwaysShow, test.wantNamed)
			}
		})
	}
}

// TestWriteNameTagMetadata pins the interaction between the name tag string and
// the two visibility flags. An empty name clears both flags even though the
// entity is named, and the always-show flag is gated on the name being shown at
// all.
func TestWriteNameTagMetadata(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		alwaysShow bool

		wantName       string
		wantAlwaysByte uint8
		wantFlags      int64
	}{
		{
			name:       "named and always shown",
			tag:        "Steve",
			alwaysShow: true,

			wantName:       "Steve",
			wantAlwaysByte: 1,
			wantFlags:      bit(goldenFlagShowName) | bit(goldenFlagAlwaysShowName),
		},
		{
			name: "named but only shown up close",
			tag:  "Steve",

			wantName:  "Steve",
			wantFlags: bit(goldenFlagShowName),
		},
		{
			name:       "an empty name clears both flags",
			tag:        "",
			alwaysShow: true,
		},
		{
			name: "a name made only of a colour code still counts as shown",
			tag:  "§c",

			wantName:  "§c",
			wantFlags: bit(goldenFlagShowName),
		},
		{
			name: "unicode names pass through byte for byte",
			tag:  "こんにちは",

			wantName:  "こんにちは",
			wantFlags: bit(goldenFlagShowName),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := protocol.NewEntityMetadata()
			writeNameTagMetadata(m, test.tag, test.alwaysShow)

			if got := m[protocol.EntityDataKeyName]; got != test.wantName {
				t.Fatalf("name = %q, want %q", got, test.wantName)
			}
			if got := m[protocol.EntityDataKeyAlwaysShowNameTag]; got != test.wantAlwaysByte {
				t.Fatalf("always show name tag byte = %v, want %v", got, test.wantAlwaysByte)
			}
			if got := m[protocol.EntityDataKeyFlags].(int64); got != test.wantFlags {
				t.Fatalf("flags = %#b, want %#b", got, test.wantFlags)
			}
		})
	}
}

// TestWriteNameTagMetadataUnsetsPreviousFlags pins that the helper clears the
// two flags it owns rather than only setting them, so a rename to the empty
// string actually hides an already visible name tag.
func TestWriteNameTagMetadata_Unsets(t *testing.T) {
	m := protocol.NewEntityMetadata()
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagShowName)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagAlwaysShowName)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagOnFire)

	writeNameTagMetadata(m, "", true)

	if got, want := m[protocol.EntityDataKeyFlags].(int64), bit(goldenFlagOnFire); got != want {
		t.Fatalf("flags = %#b, want %#b: the name flags should be cleared and nothing else", got, want)
	}
}

// TestParseEntityMetadataDefaults pins the metadata every entity gets before
// any state is looked at: its bounding box as width and height, three zeroed
// keys, and the gravity and climb flags.
func TestParseEntityMetadataDefaults(t *testing.T) {
	s := newStubSession(t, nil)
	e := newStubEntity(t, stubEntityType{
		name: "impulse:plain",
		bbox: cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3),
	})

	m := s.parseEntityMetadata(e)

	if got, want := m[protocol.EntityDataKeyWidth], float32(0.6); got != want {
		t.Fatalf("width = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyHeight], float32(1.8); got != want {
		t.Fatalf("height = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyEffectColor], int32(0); got != want {
		t.Fatalf("effect colour = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyEffectAmbience], byte(0); got != want {
		t.Fatalf("effect ambience = %v, want %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyColorIndex], byte(0); got != want {
		t.Fatalf("colour index = %v, want %v", got, want)
	}

	want := bit(goldenFlagHasGravity) | bit(goldenFlagClimb)
	if got := m[protocol.EntityDataKeyFlags].(int64); got != want {
		t.Fatalf("flags = %#b, want %#b", got, want)
	}
	if got := m[protocol.EntityDataKeyFlagsTwo].(int64); got != 0 {
		t.Fatalf("flags two = %#b, want 0", got)
	}

	// PINNED SCOPE: gravity and climb are set for every entity type, including
	// ones that neither fall nor climb. Out of scope.
	if !m.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagClimb) {
		t.Fatal("climb is not set on a plain entity")
	}
}

// TestParseEntityMetadataWidthIsTheBoxWidth pins that width comes from the X
// span of the bounding box, so a box that is wider in Z reports the X width.
func TestParseEntityMetadataWidthIsTheBoxWidth(t *testing.T) {
	s := newStubSession(t, nil)
	e := newStubEntity(t, stubEntityType{
		name: "impulse:oblong",
		bbox: cube.Box(0, 0, 0, 1, 2, 4),
	})

	m := s.parseEntityMetadata(e)
	if got, want := m[protocol.EntityDataKeyWidth], float32(1); got != want {
		t.Fatalf("width = %v, want the X span %v", got, want)
	}
	if got, want := m[protocol.EntityDataKeyHeight], float32(2); got != want {
		t.Fatalf("height = %v, want the Y span %v", got, want)
	}
}

// TestParseEntityMetadataGlint pins the enchanted flag, which is decided by the
// entity type rather than the entity.
func TestParseEntityMetadataGlint(t *testing.T) {
	s := newStubSession(t, nil)
	glinting := &stubEntity{handle: world.NewEntity(glintEntityType{glint: true}, stubEntityConf{})}

	m := s.parseEntityMetadata(glinting)
	want := bit(goldenFlagHasGravity) | bit(goldenFlagClimb) | bit(goldenFlagEnchanted)
	if got := m[protocol.EntityDataKeyFlags].(int64); got != want {
		t.Fatalf("flags = %#b, want %#b", got, want)
	}

	plain := &stubEntity{handle: world.NewEntity(glintEntityType{}, stubEntityConf{})}
	m = s.parseEntityMetadata(plain)
	if m.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagEnchanted) {
		t.Fatal("a type reporting no glint still set the enchanted flag")
	}
}

// TestParseEntityMetadataLingering pins that the lingering flag is keyed on the
// entity type being exactly entity.LingeringPotionType, not on any interface.
func TestParseEntityMetadataLingering(t *testing.T) {
	s := newStubSession(t, nil)
	e := &stubEntity{handle: world.NewEntity(entity.LingeringPotionType, stubEntityConf{})}

	m := s.parseEntityMetadata(e)
	if !m.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagLingering) {
		t.Fatal("a lingering potion did not set the lingering flag")
	}

	// The lingering potion's box is 0.25 wide and 0.25 tall.
	if got, want := m[protocol.EntityDataKeyWidth], float32(0.25); got != want {
		t.Fatalf("width = %v, want %v", got, want)
	}

	other := newStubEntity(t, stubEntityType{name: "impulse:not_lingering"})
	m = s.parseEntityMetadata(other)
	if m.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagLingering) {
		t.Fatal("a non-lingering entity set the lingering flag")
	}
}

// TestParseEntityMetadataReadsEntityState pins that state carried by the entity
// itself, not just its type, reaches the metadata.
func TestParseEntityMetadataReadsEntityState(t *testing.T) {
	s := newStubSession(t, nil)
	e := &statefulStubEntity{
		stubEntity: stubEntity{handle: world.NewEntity(stubEntityType{name: "impulse:stateful"}, stubEntityConf{})},
		sneaking:   true,
		nameTag:    "Steve",
	}

	m := s.parseEntityMetadata(e)
	want := bit(goldenFlagHasGravity) | bit(goldenFlagClimb) |
		bit(goldenFlagSneaking) | bit(goldenFlagShowName) | bit(goldenFlagAlwaysShowName)
	if got := m[protocol.EntityDataKeyFlags].(int64); got != want {
		t.Fatalf("flags = %#b, want %#b", got, want)
	}
	if got := m[protocol.EntityDataKeyName]; got != "Steve" {
		t.Fatalf("name = %v, want Steve", got)
	}
}

// Stubs below implement exactly one of the unexported interfaces
// addSpecificMetadata probes, so a test case only triggers the branch it names.

type stubSneaker struct{ v bool }

func (s stubSneaker) Sneaking() bool { return s.v }

type stubSprinter struct{ v bool }

func (s stubSprinter) Sprinting() bool { return s.v }

type stubSwimmer struct{ v bool }

func (s stubSwimmer) Swimming() bool { return s.v }

type stubCrawler struct{ v bool }

func (s stubCrawler) Crawling() bool { return s.v }

type stubGlider struct{ v bool }

func (s stubGlider) Gliding() bool { return s.v }

type stubBaby struct{ v bool }

func (s stubBaby) Baby() bool { return s.v }

type stubInvisible struct{ v bool }

func (s stubInvisible) Invisible() bool { return s.v }

type stubImmobile struct{ v bool }

func (s stubImmobile) Immobile() bool { return s.v }

type stubOnFire struct{ d time.Duration }

func (s stubOnFire) OnFireDuration() time.Duration { return s.d }

type stubUsing struct{ v bool }

func (s stubUsing) UsingItem() bool { return s.v }

type stubArrow struct{ v bool }

func (s stubArrow) Critical() bool { return s.v }

type stubGameMode struct{ mode world.GameMode }

func (s stubGameMode) GameMode() world.GameMode { return s.mode }

type stubBreather struct {
	breathing bool
	air       time.Duration
	maxAir    time.Duration
}

func (s stubBreather) Breathing() bool             { return s.breathing }
func (s stubBreather) AirSupply() time.Duration    { return s.air }
func (s stubBreather) MaxAirSupply() time.Duration { return s.maxAir }

type stubOrb struct{ xp int }

func (s stubOrb) Experience() int { return s.xp }

type stubScaled struct{ scale float64 }

func (s stubScaled) Scale() float64 { return s.scale }

type stubScoreTag struct{ tag string }

func (s stubScoreTag) ScoreTag() string { return s.tag }

type stubVariable struct{ v int32 }

func (s stubVariable) Variant() int32 { return s.v }

type stubMarkVariable struct{ v int32 }

func (s stubMarkVariable) MarkVariant() int32 { return s.v }

type stubTNT struct{ fuse time.Duration }

func (s stubTNT) Fuse() time.Duration { return s.fuse }

type stubSleeper struct {
	pos      cube.Pos
	sleeping bool
}

func (s stubSleeper) Sleeping() (cube.Pos, bool) { return s.pos, s.sleeping }

type stubEndCrystal struct {
	base      bool
	target    cube.Pos
	hasTarget bool
}

func (s stubEndCrystal) ShowBase() bool               { return s.base }
func (s stubEndCrystal) BeamTarget() (cube.Pos, bool) { return s.target, s.hasTarget }

type stubEffectBearer struct{ effects []effect.Effect }

func (s stubEffectBearer) Effects() []effect.Effect { return s.effects }

type stubAreaEffectCloud struct {
	radius  float64
	effects []effect.Effect
}

func (s stubAreaEffectCloud) Radius() float64          { return s.radius }
func (s stubAreaEffectCloud) Effects() []effect.Effect { return s.effects }

type stubSplash struct{ p potion.Potion }

func (s stubSplash) Potion() potion.Potion { return s.p }

type stubOwned struct{ owner *world.EntityHandle }

func (s stubOwned) Owner() *world.EntityHandle { return s.owner }

type stubFirework struct {
	attached bool
	owner    *world.EntityHandle
}

func (s stubFirework) Firework() item.Firework    { return item.Firework{} }
func (s stubFirework) Attached() bool             { return s.attached }
func (s stubFirework) Owner() *world.EntityHandle { return s.owner }

type stubLiving struct {
	uuid     uuid.UUID
	deathPos mgl64.Vec3
	deathDim world.Dimension
	died     bool
}

func (s stubLiving) UUID() uuid.UUID { return s.uuid }
func (s stubLiving) DeathPosition() (mgl64.Vec3, world.Dimension, bool) {
	return s.deathPos, s.deathDim, s.died
}

type stubNamed struct{ name string }

func (s stubNamed) NameTag() string { return s.name }

type stubAlwaysShow struct{ always bool }

func (s stubAlwaysShow) AlwaysShowNameTag() bool { return s.always }

type stubNamedAlways struct {
	name   string
	always bool
}

func (s stubNamedAlways) NameTag() string         { return s.name }
func (s stubNamedAlways) AlwaysShowNameTag() bool { return s.always }

// stubCombined implements several state interfaces at once so that flag
// combinations can be pinned.
type stubCombined struct {
	sneaking  bool
	sprinting bool
	swimming  bool
	crawling  bool
	invisible bool
	onFire    time.Duration
}

func (s stubCombined) Sneaking() bool                { return s.sneaking }
func (s stubCombined) Sprinting() bool               { return s.sprinting }
func (s stubCombined) Swimming() bool                { return s.swimming }
func (s stubCombined) Crawling() bool                { return s.crawling }
func (s stubCombined) Invisible() bool               { return s.invisible }
func (s stubCombined) OnFireDuration() time.Duration { return s.onFire }

// glintEntityType is a world.EntityType that also reports a glint, the only
// state parseEntityMetadata reads off the type rather than the entity.
type glintEntityType struct {
	stubEntityType
	glint bool
}

func (t glintEntityType) Glint() bool { return t.glint }

// statefulStubEntity is a world.Entity that also carries entity state, so that
// parseEntityMetadata reaches addSpecificMetadata with something to report.
type statefulStubEntity struct {
	stubEntity
	sneaking bool
	nameTag  string
}

func (e *statefulStubEntity) Sneaking() bool  { return e.sneaking }
func (e *statefulStubEntity) NameTag() string { return e.nameTag }
