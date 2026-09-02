package effect

import (
	"fmt"
	"image/color"
	"reflect"
	"slices"
	"testing"
)

// register.go builds two package-level maps from an init function: the state is
// global, and it is constructed by init rather than by a constructor. The IDs it
// holds are the
// on-disk and on-network representation of an effect, so the mapping is result
// integrity: getting one wrong silently rewrites saved potions and effects.

// registered is the complete, exhaustive expectation for the effect registry.
// Every ID that is registered appears here, and every ID that is not is absent.
// The order is the registration order in register.go.
var registered = []struct {
	id   int
	name string
	eff  Type
}{
	{id: 1, name: "speed", eff: Speed},
	{id: 2, name: "slowness", eff: Slowness},
	{id: 3, name: "haste", eff: Haste},
	{id: 4, name: "miningFatigue", eff: MiningFatigue},
	{id: 5, name: "strength", eff: Strength},
	{id: 6, name: "instantHealth", eff: InstantHealth},
	{id: 7, name: "instantDamage", eff: InstantDamage},
	{id: 8, name: "jumpBoost", eff: JumpBoost},
	{id: 9, name: "nausea", eff: Nausea},
	{id: 10, name: "regeneration", eff: Regeneration},
	{id: 11, name: "resistance", eff: Resistance},
	{id: 12, name: "fireResistance", eff: FireResistance},
	{id: 13, name: "waterBreathing", eff: WaterBreathing},
	{id: 14, name: "invisibility", eff: Invisibility},
	{id: 15, name: "blindness", eff: Blindness},
	{id: 16, name: "nightVision", eff: NightVision},
	{id: 17, name: "hunger", eff: Hunger},
	{id: 18, name: "weakness", eff: Weakness},
	{id: 19, name: "poison", eff: Poison},
	{id: 20, name: "wither", eff: Wither},
	{id: 21, name: "healthBoost", eff: HealthBoost},
	{id: 22, name: "absorption", eff: Absorption},
	{id: 23, name: "saturation", eff: Saturation},
	{id: 24, name: "levitation", eff: Levitation},
	{id: 25, name: "fatalPoison", eff: FatalPoison},
	{id: 26, name: "conduitPower", eff: ConduitPower},
	{id: 27, name: "slowFalling", eff: SlowFalling},
	{id: 30, name: "darkness", eff: Darkness},
}

func TestRegistryIsExhaustive(t *testing.T) {
	// Every registered ID resolves to the exact effect value the package exports
	// under it, and every registered effect resolves back to its ID. This is the
	// full round trip in both directions, checked one entry at a time rather
	// than by counting.
	for _, r := range registered {
		got, ok := ByID(r.id)
		if !ok {
			t.Fatalf("ByID(%d) = _, false, want %s", r.id, r.name)
		}
		if got != r.eff {
			t.Fatalf("ByID(%d) = %T, want %s", r.id, got, r.name)
		}
		if name := fmt.Sprintf("%T", got); name != "effect."+r.name {
			t.Fatalf("ByID(%d) is %s, want effect.%s", r.id, name, r.name)
		}
		back, ok := ID(r.eff)
		if !ok {
			t.Fatalf("ID(%s) = _, false, want %d", r.name, r.id)
		}
		if back != r.id {
			t.Fatalf("ID(%s) = %d, want %d", r.name, back, r.id)
		}
	}
}

func TestRegistryHoldsNothingElse(t *testing.T) {
	// The registry has no way to enumerate itself, so absence is checked over
	// the whole plausible ID range. This is what makes the table above
	// exhaustive rather than merely correct: an effect added without a test
	// change fails here.
	ids := make([]int, 0, len(registered))
	for _, r := range registered {
		ids = append(ids, r.id)
	}
	for id := -8; id <= 128; id++ {
		_, ok := ByID(id)
		if want := slices.Contains(ids, id); ok != want {
			t.Fatalf("ByID(%d) found = %v, want %v", id, ok, want)
		}
	}
	if got, want := len(registered), 28; got != want {
		t.Fatalf("the registry holds %d effects, want %d", got, want)
	}
}

func TestUnimplementedEffectIDs(t *testing.T) {
	// PINNED SCOPE: 28 (bad omen) and 29 (hero of the village) are deliberately
	// unregistered because they require villages, which are out of scope. A
	// saved entity carrying either ID decodes to nothing, and there
	// is no error path that says so: ByID returns false and the caller decides.
	for _, id := range []int{28, 29} {
		if e, ok := ByID(id); ok {
			t.Fatalf("ByID(%d) = %T, want nothing: the effect is out of scope", id, e)
		}
	}
	// The gap is between 27 and 30, so the two IDs on either side of it must be
	// present; that is what makes the hole a hole rather than the end of the
	// table.
	for _, id := range []int{27, 30} {
		if _, ok := ByID(id); !ok {
			t.Fatalf("ByID(%d) = _, false, want an effect", id)
		}
	}
}

func TestIDOfAnUnregisteredEffect(t *testing.T) {
	// A Type the registry has never seen resolves to zero and false rather than
	// to a wrong ID. Note that 0 is also not a valid effect ID, so a caller that
	// ignores the bool writes a zero to disk.
	if id, ok := ID(unregisteredEffect{}); ok || id != 0 {
		t.Fatalf("ID(unregistered) = %d, %v, want 0, false", id, ok)
	}
	if _, ok := ByID(0); ok {
		t.Fatal("ByID(0) found an effect, so 0 is a usable ID after all")
	}
}

// unregisteredEffect is a Type the registry does not know about.
type unregisteredEffect struct{ nopLasting }

func (unregisteredEffect) RGBA() color.RGBA { return color.RGBA{} }

func TestIDIsKeyedByValueNotByType(t *testing.T) {
	// effectIds is a map[Type]int, so lookups compare interface values. Every
	// effect in this package is an empty struct, which makes two values of the
	// same type equal and two values of different types distinct even when both
	// are empty. That is why instant health and saturation, which share a
	// colour, still map to different IDs.
	if got, want := reflect.TypeOf(Speed).Size(), uintptr(0); got != want {
		t.Fatalf("effect values are %d bytes, want %d: the registry relies on them being empty structs", got, want)
	}
	if id, ok := ID(speed{}); !ok || id != 1 {
		t.Fatalf("ID(speed{}) = %d, %v, want 1, true: a fresh value must find the same entry", id, ok)
	}

	healthID, _ := ID(InstantHealth)
	saturationID, _ := ID(Saturation)
	if healthID == saturationID {
		t.Fatalf("instant health and saturation share ID %d", healthID)
	}
}

func TestRegisterOverwritesBothDirections(t *testing.T) {
	// PINNED BUG: Register is exported and writes straight into two package-level
	// maps with no locking and no duplicate check. Anyone may rebind a vanilla
	// effect ID at any time, from any goroutine, for every server in the
	// process. The maps are built in init, so there is no point at which they
	// are complete and frozen.
	original, ok := ByID(1)
	if !ok {
		t.Fatal("effect 1 is not registered")
	}
	t.Cleanup(func() {
		Register(1, original)
		delete(effectIds, unregisteredEffect{})
	})

	Register(1, unregisteredEffect{})
	if got, _ := ByID(1); got != Type(unregisteredEffect{}) {
		t.Fatalf("ByID(1) = %T after re-registering, want the replacement", got)
	}
	// The reverse map keeps the old entry, so an effect can be unreachable by ID
	// while still reporting one.
	if id, ok := ID(original); !ok || id != 1 {
		t.Fatalf("ID(speed) = %d, %v after its ID was taken, want 1, true", id, ok)
	}
}

func TestRegisteringOneEffectTwiceKeepsTheLastID(t *testing.T) {
	// PINNED BUG: registering one effect under two IDs leaves both entries in
	// the forward map while the reverse map keeps only the later ID. Encoding
	// then decoding such an effect changes its ID silently.
	t.Cleanup(func() {
		delete(effects, 99)
		Register(1, Speed)
	})

	Register(99, Speed)
	if got, _ := ByID(99); got != Type(Speed) {
		t.Fatalf("ByID(99) = %T, want speed", got)
	}
	if got, _ := ByID(1); got != Type(Speed) {
		t.Fatalf("ByID(1) = %T, want speed: the first registration must survive", got)
	}
	if id, _ := ID(Speed); id != 99 {
		t.Fatalf("ID(speed) = %d, want 99: the reverse map keeps only the last", id)
	}
}
