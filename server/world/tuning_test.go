package world

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// TestTuningZeroValueIsVanilla pins that a World built without toggles
// answers every rule with the value the vanilla path relies on. The zero value
// of Tuning is what Config leaves behind when a caller sets none of them, so
// a rule that read as set here would change behaviour for every existing
// caller.
func TestTuningZeroValueIsVanilla(t *testing.T) {
	w := Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	if got := w.FixedLight(); got != 0 {
		t.Errorf("FixedLight() = %d, want 0", got)
	}
	if w.ExplosionsDisabled() {
		t.Error("ExplosionsDisabled() = true, want false")
	}
	if got := w.PickupScanPeriod(); got != 1 {
		t.Errorf("PickupScanPeriod() = %d, want 1", got)
	}
	if w.ExperienceOrbsDisabled() {
		t.Error("ExperienceOrbsDisabled() = true, want false")
	}
	if w.HungerDisabled() {
		t.Error("HungerDisabled() = true, want false")
	}
	if w.FallDamageDisabled() {
		t.Error("FallDamageDisabled() = true, want false")
	}
	if got, want := w.Tuning(), (Tuning{PickupScanPeriod: 1}); got != want {
		t.Errorf("Tuning() = %+v, want %+v", got, want)
	}
}

// TestTuningNilWorldIsVanilla pins the nil receiver answers. The player stub
// fixtures hold a zero Tx, whose World is nil, and every rule is read from code
// those fixtures reach.
func TestTuningNilWorldIsVanilla(t *testing.T) {
	var w *World
	if got, want := w.Tuning(), (Tuning{}); got != want {
		t.Errorf("Tuning() on a nil World = %+v, want %+v", got, want)
	}
	if got := w.PickupScanPeriod(); got != 1 {
		t.Errorf("PickupScanPeriod() on a nil World = %d, want 1", got)
	}
	if w.FixedLight() != 0 || w.ExplosionsDisabled() || w.ExperienceOrbsDisabled() ||
		w.HungerDisabled() || w.FallDamageDisabled() {
		t.Error("a rule on a nil World read as set, want all vanilla")
	}
	w.SetTuning(Tuning{ExplosionsDisabled: true})
}

// TestTuningUnstoredWorldIsVanilla pins the answers of a World that never
// went through Config.New, which is what a Server holds for its world
// until createWorld replaces it. Nothing has been stored, so every rule reads
// as its atomic's zero value and only PickupScanPeriod needs a floor.
func TestTuningUnstoredWorldIsVanilla(t *testing.T) {
	w := &World{}
	if got, want := w.Tuning(), (Tuning{PickupScanPeriod: 1}); got != want {
		t.Errorf("Tuning() on an unstored World = %+v, want %+v", got, want)
	}
}

// TestTuningClampOutOfRangeValues pins what a caller's out of range value
// turns into: FixedLight must stay a valid nibble because Tx.Light returns it
// as a light level, and PickupScanPeriod must stay at least 1 because
// ItemBehaviour uses period-1 as a countdown.
func TestTuningClampOutOfRangeValues(t *testing.T) {
	for _, test := range []struct {
		name string
		in   Tuning
		want Tuning
	}{
		{
			name: "negative light clamps to vanilla",
			in:   Tuning{FixedLight: -1},
			want: Tuning{PickupScanPeriod: 1},
		},
		{
			name: "light above a nibble clamps to 15",
			in:   Tuning{FixedLight: 99},
			want: Tuning{FixedLight: 15, PickupScanPeriod: 1},
		},
		{
			name: "zero scan period reads as every tick",
			in:   Tuning{PickupScanPeriod: 0},
			want: Tuning{PickupScanPeriod: 1},
		},
		{
			name: "negative scan period reads as every tick",
			in:   Tuning{PickupScanPeriod: -8},
			want: Tuning{PickupScanPeriod: 1},
		},
		{
			name: "everything set at once round trips",
			in: Tuning{
				FixedLight: 7, ExplosionsDisabled: true, PickupScanPeriod: 20,
				ExperienceOrbsDisabled: true, HungerDisabled: true, FallDamageDisabled: true,
			},
			want: Tuning{
				FixedLight: 7, ExplosionsDisabled: true, PickupScanPeriod: 20,
				ExperienceOrbsDisabled: true, HungerDisabled: true, FallDamageDisabled: true,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := Config{Synchronous: true, Tuning: test.in}.New()
			t.Cleanup(func() { _ = w.Close() })

			if got := w.Tuning(); got != test.want {
				t.Fatalf("Config.Tuning %+v produced %+v, want %+v", test.in, got, test.want)
			}
			// SetTuning goes through the same store, so it clamps identically.
			other := Config{Synchronous: true}.New()
			t.Cleanup(func() { _ = other.Close() })
			other.SetTuning(test.in)
			if got := other.Tuning(); got != test.want {
				t.Fatalf("SetTuning(%+v) produced %+v, want %+v", test.in, got, test.want)
			}
		})
	}
}

// TestVanillaLightIsPropagatedOnLoad is the baseline for the test below: with no
// fixed light, a column arrives with the light of its emitter already
// propagated into the storage, and Tx.Light reports what the storage holds.
func TestVanillaLightIsPropagatedOnLoad(t *testing.T) {
	w := tuningLitWorld(t, Tuning{})

	runWorld(w, func(tx *Tx) {
		col := tuningLoad(t, tx)
		if got := col.Light(0, tuningLampY, 0); got != tuningEmission {
			t.Errorf("stored light at the emitter = %d, want %d", got, tuningEmission)
		}
		if got := tx.Light(tuningLampPos); got != tuningEmission {
			t.Errorf("Tx.Light at the emitter = %d, want %d", got, tuningEmission)
		}
		if got := tx.SkyLight(tuningLampPos); got != 0 {
			t.Errorf("Tx.SkyLight at the emitter = %d, want 0", got)
		}
	})
}

// TestFixedLightPinsQueriesAndSkipsPropagation pins both halves of the fixed
// light rule. Tx.Light and Tx.SkyLight answer with the level at any position,
// including outside the world's range, and the column is loaded without the
// flood fill, so the emitter's light never reaches the storage. The light
// slices are still handed out, so reading them does not index a nil slice.
func TestFixedLightPinsQueriesAndSkipsPropagation(t *testing.T) {
	const level = 7
	w := tuningLitWorld(t, Tuning{FixedLight: level})

	runWorld(w, func(tx *Tx) {
		col := tuningLoad(t, tx)
		if got := col.Light(0, tuningLampY, 0); got != 0 {
			t.Errorf("stored light at the emitter = %d, want 0 (no propagation)", got)
		}
		// An empty sub chunk above the emitter still gets its slices, which is
		// the part of the fill FillFixed keeps.
		if got := col.SkyLight(0, tuningLampY+64, 0); got != 15 {
			t.Errorf("stored skylight above the emitter = %d, want 15 (slices initialised)", got)
		}

		for _, pos := range []cube.Pos{
			tuningLampPos,
			{0, tuningLampY + 64, 0},
			{8, 0, 8},
			{0, tx.Range()[1] + 10, 0},
			{0, tx.Range()[0] - 10, 0},
		} {
			if got := tx.Light(pos); got != level {
				t.Errorf("Tx.Light(%v) = %d, want %d", pos, got, level)
			}
			if got := tx.SkyLight(pos); got != level {
				t.Errorf("Tx.SkyLight(%v) = %d, want %d", pos, got, level)
			}
		}
	})
}

// The emitter sits at tuningLampY with an emission below 15, so that a value
// propagation wrote can never be confused with the 15 that an empty sub chunk is
// initialised to.
const (
	tuningLampY    = 5
	tuningEmission = 14
)

var tuningLampPos = cube.Pos{0, tuningLampY, 0}

// tuningLitWorld returns a synchronous World generating a single light
// emitting block at tuningLampPos, with the rules passed applied.
func tuningLitWorld(t *testing.T, rules Tuning) *World {
	t.Helper()

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:game_rule_lamp", Properties: map[string]any{}})
	registry.RegisterBlock(tuningLamp{})
	registry.Finalize()

	w := Config{
		Synchronous: true,
		Blocks:      registry,
		Generator:   tuningLampGenerator{lamp: registry.BlockRuntimeID(tuningLamp{})},
		Tuning:      rules,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// tuningLoad loads the column at the origin and returns it. Tx.Block is what
// pulls the column through World.loadChunk, which is where light is decided.
func tuningLoad(t *testing.T, tx *Tx) *Column {
	t.Helper()

	tx.Block(tuningLampPos)
	col, ok := tx.World().chunks[ChunkPos{0, 0}]
	if !ok {
		t.Fatal("column at the origin was not loaded")
	}
	return col
}

// tuningLampGenerator places one emitter, leaving the sub chunk that holds it
// non-empty and every sub chunk above it empty.
type tuningLampGenerator struct{ lamp uint32 }

func (g tuningLampGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk) {
	if pos != (ChunkPos{}) {
		return
	}
	c.SetBlock(0, tuningLampY, 0, 0, g.lamp)
}

func (tuningLampGenerator) DefaultSpawn() cube.Pos { return tuningLampPos }

// tuningLamp emits light, which is what gives the fill something to
// propagate. It does not implement lightDiffuser, so the registry gives it the
// default light filter of 15.
type tuningLamp struct{}

func (tuningLamp) EncodeBlock() (string, map[string]any) {
	return "test:game_rule_lamp", nil
}
func (tuningLamp) Hash() (uint64, uint64)    { return 1 << 36, 0 }
func (tuningLamp) Model() BlockModel         { return tuningLampModel{} }
func (tuningLamp) LightEmissionLevel() uint8 { return tuningEmission }

type tuningLampModel struct{}

func (tuningLampModel) BBox(cube.Pos, BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
}
func (tuningLampModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool { return true }
