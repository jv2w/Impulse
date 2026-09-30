package entity

import (
	"image/color"
	"maps"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
	"github.com/jv2w/Impulse/server/world"
)

// ---------------------------------------------------------------------------
// HealthManager
// ---------------------------------------------------------------------------

func TestNewHealthManagerClamping(t *testing.T) {
	tests := []struct {
		name                string
		health, max         float64
		wantHealth, wantMax float64
	}{
		{name: "health below the max is kept", health: 10, max: 20, wantHealth: 10, wantMax: 20},
		{name: "health equal to the max is kept", health: 20, max: 20, wantHealth: 20, wantMax: 20},
		{name: "health above the max is clamped down", health: 25, max: 20, wantHealth: 20, wantMax: 20},
		// PINNED BUG: the constructor clamps health down but never up, so an
		// entity can be constructed below the floor that AddHealth enforces on
		// every later write.
		{name: "negative health is not clamped up to zero", health: -5, max: 20, wantHealth: -5, wantMax: 20},
		// PINNED BUG: SetMaxHealth replaces a max of 0 with 1, but the
		// constructor accepts 0 and drags health down with it. The entity is
		// born dead and AddHealth can never lift it: min(0+n, 0) is 0 forever.
		{name: "a zero max is accepted and pins health at zero", health: 5, max: 0, wantHealth: 0, wantMax: 0},
		// PINNED BUG: same asymmetry with a negative max, which SetMaxHealth
		// would also have replaced with 1.
		{name: "a negative max is accepted and drags health below zero", health: 5, max: -3, wantHealth: -3, wantMax: -3},
		{name: "health under a negative max is left alone", health: -5, max: -3, wantHealth: -5, wantMax: -3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := NewHealthManager(test.health, test.max)
			if got := m.Health(); math.Abs(got-test.wantHealth) > physEpsilon {
				t.Fatalf("NewHealthManager(%v, %v).Health() = %v, want %v", test.health, test.max, got, test.wantHealth)
			}
			if got := m.MaxHealth(); math.Abs(got-test.wantMax) > physEpsilon {
				t.Fatalf("NewHealthManager(%v, %v).MaxHealth() = %v, want %v", test.health, test.max, got, test.wantMax)
			}
		})
	}
}

func TestHealthManagerConstructorAcceptsAMaxTheSetterRejects(t *testing.T) {
	// PINNED BUG: NewHealthManager and SetMaxHealth disagree about what a valid
	// max is. The same value is stored verbatim by one and rewritten to 1 by the
	// other, so an entity's max depends on which door the value came through.
	for _, max := range []float64{0, -3, -1e9} {
		m := NewHealthManager(20, max)
		if got := m.MaxHealth(); math.Abs(got-max) > physEpsilon {
			t.Fatalf("NewHealthManager(20, %v).MaxHealth() = %v, want %v accepted verbatim", max, got, max)
		}
		m.SetMaxHealth(max)
		if got := m.MaxHealth(); math.Abs(got-1) > physEpsilon {
			t.Fatalf("SetMaxHealth(%v) then MaxHealth() = %v, want 1", max, got)
		}
	}
}

func TestHealthManagerAddHealthClamps(t *testing.T) {
	tests := []struct {
		name             string
		health, max, add float64
		want             float64
	}{
		{name: "adds within range", health: 10, max: 20, add: 5, want: 15},
		{name: "adding past the max clamps to the max", health: 10, max: 20, add: 50, want: 20},
		{name: "adding at the max stays at the max", health: 20, max: 20, add: 1, want: 20},
		{name: "adding zero changes nothing", health: 10, max: 20, add: 0, want: 10},
		{name: "subtracting past zero clamps to zero", health: 10, max: 20, add: -50, want: 0},
		{name: "subtracting at zero stays at zero", health: 0, max: 20, add: -1, want: 0},
		{name: "positive infinity clamps to the max", health: 10, max: 20, add: math.Inf(1), want: 20},
		{name: "negative infinity clamps to zero", health: 10, max: 20, add: math.Inf(-1), want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := NewHealthManager(test.health, test.max)
			m.AddHealth(test.add)
			if got := m.Health(); math.Abs(got-test.want) > physEpsilon {
				t.Fatalf("AddHealth(%v) from %v/%v then Health() = %v, want %v", test.add, test.health, test.max, got, test.want)
			}
			if got := m.MaxHealth(); math.Abs(got-test.max) > physEpsilon {
				t.Fatalf("AddHealth(%v) changed MaxHealth() to %v, want %v untouched", test.add, got, test.max)
			}
		})
	}
}

func TestHealthManagerAddHealthEscapesANonPositiveMax(t *testing.T) {
	// PINNED BUG: AddHealth is max(min(health+n, max), 0). With a non-positive
	// max the two clamps disagree and the outer one wins, so health lands on 0 —
	// which is above the max the manager was built with.
	m := NewHealthManager(20, -3)
	if got := m.Health(); math.Abs(got-(-3)) > physEpsilon {
		t.Fatalf("Health() = %v, want -3 before any AddHealth", got)
	}
	m.AddHealth(0)
	if got := m.Health(); math.Abs(got) > physEpsilon {
		t.Fatalf("AddHealth(0) with a max of -3 then Health() = %v, want 0", got)
	}
	if m.Health() <= m.MaxHealth() {
		t.Fatalf("Health() = %v, MaxHealth() = %v, want health above the max", m.Health(), m.MaxHealth())
	}

	zero := NewHealthManager(20, 0)
	zero.AddHealth(100)
	if got := zero.Health(); math.Abs(got) > physEpsilon {
		t.Fatalf("AddHealth(100) with a max of 0 then Health() = %v, want 0 (unrevivable)", got)
	}
}

func TestHealthManagerAddHealthIgnoresNaN(t *testing.T) {
	// Go's builtin min and max propagate NaN, which would leave health neither
	// alive nor dead for good, so a NaN amount is ignored.
	m := NewHealthManager(10, 20)
	m.AddHealth(math.NaN())
	if got := m.Health(); got != 10 {
		t.Fatalf("AddHealth(NaN) then Health() = %v, want 10", got)
	}
	m.AddHealth(20)
	if got := m.Health(); got != 20 {
		t.Fatalf("AddHealth(20) after a NaN then Health() = %v, want 20", got)
	}
}

func TestHealthManagerSetMaxHealth(t *testing.T) {
	tests := []struct {
		name                string
		health, max, set    float64
		wantMax, wantHealth float64
	}{
		{name: "lowering the max lowers current health", health: 20, max: 20, set: 10, wantMax: 10, wantHealth: 10},
		{name: "lowering the max leaves lower health alone", health: 5, max: 20, set: 10, wantMax: 10, wantHealth: 5},
		{name: "raising the max does not raise current health", health: 10, max: 10, set: 20, wantMax: 20, wantHealth: 10},
		{name: "a zero max becomes one", health: 10, max: 20, set: 0, wantMax: 1, wantHealth: 1},
		{name: "a negative max becomes one", health: 10, max: 20, set: -5, wantMax: 1, wantHealth: 1},
		{name: "a max of one keeps health of one", health: 1, max: 20, set: 1, wantMax: 1, wantHealth: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := NewHealthManager(test.health, test.max)
			m.SetMaxHealth(test.set)
			if got := m.MaxHealth(); math.Abs(got-test.wantMax) > physEpsilon {
				t.Fatalf("SetMaxHealth(%v) then MaxHealth() = %v, want %v", test.set, got, test.wantMax)
			}
			if got := m.Health(); math.Abs(got-test.wantHealth) > physEpsilon {
				t.Fatalf("SetMaxHealth(%v) then Health() = %v, want %v", test.set, got, test.wantHealth)
			}
		})
	}
}

func TestHealthManagerSetMaxHealthNaN(t *testing.T) {
	// A NaN max is treated like one that is too low and becomes one.
	m := NewHealthManager(10, 20)
	m.SetMaxHealth(math.NaN())
	if got := m.MaxHealth(); got != 1 {
		t.Fatalf("SetMaxHealth(NaN) then MaxHealth() = %v, want 1", got)
	}
	if got := m.Health(); got != 1 {
		t.Fatalf("SetMaxHealth(NaN) then Health() = %v, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// effect package: construction, ticking and colour
// ---------------------------------------------------------------------------

func TestEffectConstructorsDoNotValidate(t *testing.T) {
	// None of the constructors reject a level of 0 or below, or a negative
	// duration. EffectManager.Add's panic is the only guard in the whole layer,
	// so any code path that stores or applies an Effect without going through Add
	// sees whatever the caller built.
	tests := []struct {
		name         string
		eff          effect.Effect
		wantLevel    int
		wantDuration time.Duration
		wantInfinite bool
		wantAmbient  bool
		wantLasting  bool
	}{
		{name: "New with a level of zero", eff: effect.New(effect.Speed, 0, time.Second), wantLevel: 0, wantDuration: time.Second, wantLasting: true},
		{name: "New with a negative level", eff: effect.New(effect.Speed, -3, time.Second), wantLevel: -3, wantDuration: time.Second, wantLasting: true},
		{name: "New with a negative duration", eff: effect.New(effect.Speed, 1, -time.Second), wantLevel: 1, wantDuration: -time.Second, wantLasting: true},
		{name: "NewAmbient with a negative level", eff: effect.NewAmbient(effect.Speed, -1, time.Second), wantLevel: -1, wantDuration: time.Second, wantAmbient: true, wantLasting: true},
		{name: "NewInfinite with a level of zero", eff: effect.NewInfinite(effect.Speed, 0), wantLevel: 0, wantInfinite: true, wantLasting: true},
		{name: "NewInstant with a negative level", eff: effect.NewInstant(effect.InstantHealth, -2), wantLevel: -2},
		{name: "NewInstantWithPotency with a level of zero and a negative potency", eff: effect.NewInstantWithPotency(effect.InstantDamage, 0, -4), wantLevel: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.eff.Level(); got != test.wantLevel {
				t.Fatalf("Level() = %d, want %d", got, test.wantLevel)
			}
			if got := test.eff.Duration(); got != test.wantDuration {
				t.Fatalf("Duration() = %v, want %v", got, test.wantDuration)
			}
			if got := test.eff.Infinite(); got != test.wantInfinite {
				t.Fatalf("Infinite() = %v, want %v", got, test.wantInfinite)
			}
			if got := test.eff.Ambient(); got != test.wantAmbient {
				t.Fatalf("Ambient() = %v, want %v", got, test.wantAmbient)
			}
			if _, got := test.eff.Type().(effect.LastingType); got != test.wantLasting {
				t.Fatalf("Type() implements LastingType = %v, want %v", got, test.wantLasting)
			}
			if test.eff.ParticlesHidden() {
				t.Fatal("ParticlesHidden() = true for an effect built without WithoutParticles")
			}
		})
	}
}

func TestEffectWithoutParticlesReturnsACopy(t *testing.T) {
	e := effect.New(effect.Speed, 1, time.Second)
	hidden := e.WithoutParticles()
	if e.ParticlesHidden() {
		t.Fatal("WithoutParticles() mutated its receiver, want a copy")
	}
	if !hidden.ParticlesHidden() {
		t.Fatal("WithoutParticles() result ParticlesHidden() = false, want true")
	}
}

func TestEffectTickDurationIsAValueReceiver(t *testing.T) {
	// TickDuration has a value receiver and returns the advanced copy. Calling it
	// for its side effect does nothing at all; the caller has to store the result.
	// EffectManager.Tick is the only place that does.
	e := effect.New(effect.Speed, 1, 10*time.Second)

	e.TickDuration()
	if got := e.Duration(); got != 10*time.Second {
		t.Fatalf("Duration() after a discarded TickDuration() = %v, want 10s", got)
	}
	if got := e.Tick(); got != 0 {
		t.Fatalf("Tick() after a discarded TickDuration() = %d, want 0", got)
	}

	next := e.TickDuration()
	if got, want := next.Duration(), 10*time.Second-time.Second/20; got != want {
		t.Fatalf("TickDuration().Duration() = %v, want %v", got, want)
	}
	if got := next.Tick(); got != 1 {
		t.Fatalf("TickDuration().Tick() = %d, want 1", got)
	}
	if got := e.Duration(); got != 10*time.Second {
		t.Fatalf("Duration() of the receiver = %v, want 10s untouched", got)
	}
}

func TestEffectTickDurationByKind(t *testing.T) {
	tests := []struct {
		name         string
		eff          effect.Effect
		wantDuration time.Duration
		wantTick     int
	}{
		{name: "a lasting finite effect loses one tick of duration", eff: effect.New(effect.Speed, 1, time.Second), wantDuration: time.Second - time.Second/20, wantTick: 1},
		{name: "a lasting infinite effect keeps its duration but still ticks", eff: effect.NewInfinite(effect.Speed, 1), wantDuration: 0, wantTick: 1},
		{name: "a lasting effect keeps decrementing past zero", eff: effect.New(effect.Speed, 1, 0), wantDuration: -time.Second / 20, wantTick: 1},
		{name: "an instant effect neither loses duration nor ticks", eff: effect.NewInstant(effect.InstantHealth, 1), wantDuration: 0, wantTick: 0},
		{name: "an instant damage effect neither loses duration nor ticks", eff: effect.NewInstant(effect.InstantDamage, 1), wantDuration: 0, wantTick: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.eff.TickDuration()
			if d := got.Duration(); d != test.wantDuration {
				t.Fatalf("TickDuration().Duration() = %v, want %v", d, test.wantDuration)
			}
			if n := got.Tick(); n != test.wantTick {
				t.Fatalf("TickDuration().Tick() = %d, want %d", n, test.wantTick)
			}
			if inf := got.Infinite(); inf != test.eff.Infinite() {
				t.Fatalf("TickDuration().Infinite() = %v, want %v unchanged", inf, test.eff.Infinite())
			}
		})
	}
}

func TestEffectZeroDurationIsAmbiguous(t *testing.T) {
	// Duration() == 0 means three different things. EffectManager.expired reads
	// it together with Infinite(), which separates the infinite case, but nothing
	// separates "instant" from "expired" except a LastingType assertion. Any
	// rewrite that keys expiry off Duration() alone will drop instant effects into
	// the expired branch and call End on a Type that has no End.
	instant := effect.NewInstant(effect.InstantHealth, 1)
	infinite := effect.NewInfinite(effect.Speed, 1)
	expired := effect.New(effect.Speed, 1, 0)

	for name, e := range map[string]effect.Effect{"instant": instant, "infinite": infinite, "expired": expired} {
		if got := e.Duration(); got != 0 {
			t.Fatalf("%s effect Duration() = %v, want 0", name, got)
		}
	}
	if instant.Infinite() || expired.Infinite() {
		t.Fatalf("Infinite() = %v and %v for the instant and expired effects, want false and false", instant.Infinite(), expired.Infinite())
	}
	if !infinite.Infinite() {
		t.Fatal("Infinite() = false for an effect built with NewInfinite, want true")
	}
	if _, ok := instant.Type().(effect.LastingType); ok {
		t.Fatal("an instant effect's Type() implements LastingType, want it not to")
	}
	if _, ok := expired.Type().(effect.LastingType); !ok {
		t.Fatal("an expired lasting effect's Type() does not implement LastingType, want it to")
	}
}

func TestResultingColourIsOrderIndependent(t *testing.T) {
	// ResultingColour sums the channels and divides by the count, so the result
	// cannot depend on the order it walks the slice. That matters because the only
	// producer of that slice is EffectManager.Effects, whose order is random.
	speed := effect.New(effect.Speed, 1, time.Second)
	regen := effect.New(effect.Regeneration, 1, time.Second)
	poison := effect.New(effect.Poison, 1, time.Second)
	want := color.RGBA{R: 0x82, G: 0xa3, B: 0xaf, A: 0xff}

	for _, order := range [][]effect.Effect{
		{speed, regen, poison},
		{speed, poison, regen},
		{regen, speed, poison},
		{regen, poison, speed},
		{poison, speed, regen},
		{poison, regen, speed},
	} {
		got, ambient := effect.ResultingColour(order)
		if got != want {
			t.Fatalf("ResultingColour(%s) = %v, want %v", livingEffectOrder(order), got, want)
		}
		if ambient {
			t.Fatalf("ResultingColour(%s) ambient = true, want false", livingEffectOrder(order))
		}
	}
}

func TestResultingColourSpecialCases(t *testing.T) {
	blue := color.RGBA{R: 0x38, G: 0x5d, B: 0xc6, A: 0xff}
	regen := color.RGBA{R: 0xcd, G: 0x5c, B: 0xab, A: 0xff}

	tests := []struct {
		name        string
		effects     []effect.Effect
		want        color.RGBA
		wantAmbient bool
	}{
		{name: "no effects fall back to the default blue", want: blue},
		{name: "only hidden effects fall back to the default blue", effects: []effect.Effect{
			effect.NewAmbient(effect.Speed, 1, time.Second).WithoutParticles(),
		}, want: blue},
		{name: "a single effect is its own colour", effects: []effect.Effect{
			effect.New(effect.Regeneration, 1, time.Second),
		}, want: regen},
		{name: "all ambient effects report ambient", effects: []effect.Effect{
			effect.NewAmbient(effect.Regeneration, 1, time.Second),
		}, want: regen, wantAmbient: true},
		// A hidden effect is skipped before the ambient check, so it cannot drag
		// the ambient flag down either.
		{name: "a hidden non ambient effect is skipped entirely", effects: []effect.Effect{
			effect.New(effect.Speed, 1, time.Second).WithoutParticles(),
			effect.NewAmbient(effect.Regeneration, 1, time.Second),
		}, want: regen, wantAmbient: true},
		{name: "one non ambient effect clears the ambient flag", effects: []effect.Effect{
			effect.NewAmbient(effect.Regeneration, 1, time.Second),
			effect.New(effect.Regeneration, 1, time.Second),
		}, want: regen},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ambient := effect.ResultingColour(test.effects)
			if got != test.want {
				t.Fatalf("ResultingColour() = %v, want %v", got, test.want)
			}
			if ambient != test.wantAmbient {
				t.Fatalf("ResultingColour() ambient = %v, want %v", ambient, test.wantAmbient)
			}
		})
	}
}

func TestEffectIntervalShiftPanicsBelowLevelOne(t *testing.T) {
	// PINNED BUG: Poison, FatalPoison and Regeneration compute their interval as
	// a right shift by Level()-1, and Wither by Level(). A level at or below the
	// point where the shift count goes negative crashes the goroutine that ticks
	// the entity with a runtime panic. EffectManager.Add rejects such levels, so
	// the only way in is Apply called directly or a manager whose map was filled
	// without going through Add — which is exactly what a rewrite of the storage
	// layer would do.
	tests := []struct {
		name      string
		eff       effect.Effect
		wantPanic bool
	}{
		{name: "poison at level zero", eff: effect.New(effect.Poison, 0, time.Second), wantPanic: true},
		{name: "poison at level one", eff: effect.New(effect.Poison, 1, time.Second)},
		{name: "fatal poison at level zero", eff: effect.New(effect.FatalPoison, 0, time.Second), wantPanic: true},
		{name: "regeneration at level zero", eff: effect.New(effect.Regeneration, 0, time.Second), wantPanic: true},
		{name: "regeneration at a negative level", eff: effect.New(effect.Regeneration, -1, time.Second), wantPanic: true},
		// Wither shifts by Level() rather than Level()-1, so level 0 survives and
		// only a negative level crashes it.
		{name: "wither at level zero", eff: effect.New(effect.Wither, 0, time.Second)},
		{name: "wither at a negative level", eff: effect.New(effect.Wither, -1, time.Second), wantPanic: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := newLivingFake(20, 20)
			recovered := livingRecover(func() { test.eff.Type().Apply(e, test.eff) })
			if !test.wantPanic {
				if recovered != nil {
					t.Fatalf("Apply() panicked with %v, want no panic", recovered)
				}
				return
			}
			if recovered == nil {
				t.Fatal("Apply() returned normally, want a negative shift panic")
			}
			err, ok := recovered.(runtime.Error)
			if !ok {
				t.Fatalf("Apply() panicked with %T (%v), want a runtime.Error", recovered, recovered)
			}
			if !strings.Contains(err.Error(), "negative shift amount") {
				t.Fatalf("Apply() panic = %q, want it to mention a negative shift amount", err.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// EffectManager
// ---------------------------------------------------------------------------

func TestEffectManagerAddPanicsOnInvalidInput(t *testing.T) {
	// PINNED BUG: EffectManager.Add panics on input a caller cannot always
	// validate, instead of returning the failure as a value.
	tests := []struct {
		name string
		eff  effect.Effect
		want string
	}{
		{name: "a lasting effect at level zero", eff: effect.New(effect.Speed, 0, time.Second),
			want: "(*EffectManager).Add: effect cannot have level of 0 or below: 0"},
		{name: "a lasting effect at a negative level", eff: effect.New(effect.Speed, -2, time.Second),
			want: "(*EffectManager).Add: effect cannot have level of 0 or below: -2"},
		{name: "an instant effect at level zero", eff: effect.NewInstant(effect.InstantHealth, 0),
			want: "(*EffectManager).Add: effect cannot have level of 0 or below: 0"},
		{name: "an infinite effect at level zero", eff: effect.NewInfinite(effect.Speed, 0),
			want: "(*EffectManager).Add: effect cannot have level of 0 or below: 0"},
		{name: "a negative duration", eff: effect.New(effect.Speed, 1, -time.Second),
			want: "(*EffectManager).Add: effect cannot have negative duration: -1s"},
		// The level check runs first, so a value that is invalid twice reports the
		// level, never the duration.
		{name: "a bad level outranks a bad duration", eff: effect.New(effect.Speed, 0, -time.Second),
			want: "(*EffectManager).Add: effect cannot have level of 0 or below: 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := newLivingFake(20, 20)
			m := NewEffectManager()
			recovered := livingRecover(func() { m.Add(test.eff, e) })
			if recovered == nil {
				t.Fatal("Add() returned normally, want a panic")
			}
			got, ok := recovered.(string)
			if !ok {
				t.Fatalf("Add() panicked with %T (%v), want a string", recovered, recovered)
			}
			if got != test.want {
				t.Fatalf("Add() panic = %q, want %q", got, test.want)
			}
			if n := len(m.Effects()); n != 0 {
				t.Fatalf("Effects() after a rejected Add = %d effects, want 0", n)
			}
			if n := len(e.calls); n != 0 {
				t.Fatalf("a rejected Add reached the entity with %v, want no calls", e.callNames())
			}
		})
	}
}

func TestEffectManagerAddAcceptsAZeroDuration(t *testing.T) {
	// Only a negative duration is rejected. A zero duration is stored and dies on
	// the first Tick, which is a different outcome from being refused outright.
	e := newLivingFake(20, 20)
	m := NewEffectManager()
	if recovered := livingRecover(func() { m.Add(effect.New(effect.Speed, 1, 0), e) }); recovered != nil {
		t.Fatalf("Add() with a zero duration panicked with %v, want no panic", recovered)
	}
	if n := len(m.Effects()); n != 1 {
		t.Fatalf("Effects() = %d effects, want 1 stored", n)
	}
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) { m.Tick(e, tx) })
	if n := len(m.Effects()); n != 0 {
		t.Fatalf("Effects() after one Tick = %d effects, want 0", n)
	}
}

func TestEffectManagerInstantEffectsAreAppliedAndNotStored(t *testing.T) {
	tests := []struct {
		name      string
		eff       effect.Effect
		wantCall  string
		wantValue float64
		wantSrc   any
	}{
		{name: "instant health at level one heals 2<<1", eff: effect.NewInstant(effect.InstantHealth, 1),
			wantCall: "Heal", wantValue: 4, wantSrc: effect.InstantHealingSource{}},
		{name: "instant health at level two heals 2<<2", eff: effect.NewInstant(effect.InstantHealth, 2),
			wantCall: "Heal", wantValue: 8, wantSrc: effect.InstantHealingSource{}},
		{name: "potency scales the healed amount", eff: effect.NewInstantWithPotency(effect.InstantHealth, 1, 0.5),
			wantCall: "Heal", wantValue: 2, wantSrc: effect.InstantHealingSource{}},
		{name: "instant damage at level one hurts 3<<1", eff: effect.NewInstant(effect.InstantDamage, 1),
			wantCall: "Hurt", wantValue: 6, wantSrc: effect.InstantDamageSource{}},
		{name: "potency scales the damage dealt", eff: effect.NewInstantWithPotency(effect.InstantDamage, 1, 2),
			wantCall: "Hurt", wantValue: 12, wantSrc: effect.InstantDamageSource{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := newLivingFake(10, 40)
			m := NewEffectManager()
			if got := m.Add(test.eff, e); got != test.eff {
				t.Fatalf("Add() returned %v, want the effect passed", got)
			}
			if got, want := e.callNames(), []string{test.wantCall}; !slices.Equal(got, want) {
				t.Fatalf("calls on the entity = %v, want %v", got, want)
			}
			call := e.calls[0]
			if math.Abs(call.v-test.wantValue) > physEpsilon {
				t.Fatalf("%s() argument = %v, want %v", test.wantCall, call.v, test.wantValue)
			}
			if call.arg != test.wantSrc {
				t.Fatalf("%s() source = %#v, want %#v", test.wantCall, call.arg, test.wantSrc)
			}
			if n := len(m.Effects()); n != 0 {
				t.Fatalf("Effects() after an instant effect = %d, want 0 (instant effects are never stored)", n)
			}
			if _, ok := m.Effect(test.eff.Type()); ok {
				t.Fatal("Effect() found an instant effect, want false")
			}
		})
	}
}

func TestEffectManagerStoresLastingEffectsKeyedByType(t *testing.T) {
	e := newLivingFake(20, 20)
	m := NewEffectManager()

	m.Add(effect.New(effect.Speed, 1, 10*time.Second), e)
	m.Add(effect.New(effect.Regeneration, 1, 10*time.Second), e)
	if n := len(m.Effects()); n != 2 {
		t.Fatalf("Effects() with two different types = %d, want 2", n)
	}

	// The map key is reflect.TypeOf(e.Type()), so a second Speed replaces the
	// first rather than stacking with it, no matter how the levels differ.
	m.Add(effect.New(effect.Speed, 5, 20*time.Second), e)
	if n := len(m.Effects()); n != 2 {
		t.Fatalf("Effects() after re-adding Speed = %d, want 2", n)
	}
	got, ok := m.Effect(effect.Speed)
	if !ok {
		t.Fatal("Effect(Speed) = false, want the stored Speed effect")
	}
	if got.Level() != 5 || got.Duration() != 20*time.Second {
		t.Fatalf("stored Speed = level %d for %v, want level 5 for 20s", got.Level(), got.Duration())
	}
}

func TestEffectManagerAddReplacementRule(t *testing.T) {
	// effect.go:55 exactly. The existing effect is kept when its level is higher,
	// or when the levels are equal and either the existing duration is longer
	// while the new effect is not infinite, or the existing effect is infinite.
	//
	// Every added effect carries WithoutParticles so the winner is identifiable
	// even in rows where the two effects are otherwise identical. The flag takes
	// no part in the rule.
	const long = 10 * time.Minute
	const oneTick = time.Second / 20

	tests := []struct {
		name             string
		existing, added  effect.Effect
		wantKeptExisting bool
	}{
		{name: "a higher level with one tick left beats a lower level with ten minutes",
			existing: effect.New(effect.Speed, 2, oneTick), added: effect.New(effect.Speed, 1, long).WithoutParticles(), wantKeptExisting: true},
		{name: "a higher level replaces a lower level with a far longer duration",
			existing: effect.New(effect.Speed, 1, long), added: effect.New(effect.Speed, 2, oneTick).WithoutParticles()},
		{name: "equal levels keep the longer existing duration",
			existing: effect.New(effect.Speed, 1, 10*time.Second), added: effect.New(effect.Speed, 1, 5*time.Second).WithoutParticles(), wantKeptExisting: true},
		{name: "equal levels take the longer new duration",
			existing: effect.New(effect.Speed, 1, 5*time.Second), added: effect.New(effect.Speed, 1, 10*time.Second).WithoutParticles()},
		{name: "equal levels and equal durations replace",
			existing: effect.New(effect.Speed, 1, 10*time.Second), added: effect.New(effect.Speed, 1, 10*time.Second).WithoutParticles()},
		{name: "a new infinite effect beats a longer finite one at the same level",
			existing: effect.New(effect.Speed, 1, long), added: effect.NewInfinite(effect.Speed, 1).WithoutParticles()},
		{name: "an existing infinite effect beats a long finite one at the same level",
			existing: effect.NewInfinite(effect.Speed, 1), added: effect.New(effect.Speed, 1, long).WithoutParticles(), wantKeptExisting: true},
		{name: "an existing infinite effect beats an equal infinite one",
			existing: effect.NewInfinite(effect.Speed, 1), added: effect.NewInfinite(effect.Speed, 1).WithoutParticles(), wantKeptExisting: true},
		{name: "a higher level infinite effect replaces an infinite one",
			existing: effect.NewInfinite(effect.Speed, 1), added: effect.NewInfinite(effect.Speed, 2).WithoutParticles()},
		{name: "an existing higher level infinite effect is kept against an infinite one",
			existing: effect.NewInfinite(effect.Speed, 2), added: effect.NewInfinite(effect.Speed, 1).WithoutParticles(), wantKeptExisting: true},
		{name: "a higher level finite effect replaces an infinite one",
			existing: effect.NewInfinite(effect.Speed, 1), added: effect.New(effect.Speed, 2, oneTick).WithoutParticles()},
		{name: "a lower level finite effect loses to an infinite one",
			existing: effect.NewInfinite(effect.Speed, 2), added: effect.New(effect.Speed, 1, long).WithoutParticles(), wantKeptExisting: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := newLivingFake(20, 20)
			m := NewEffectManager()
			m.Add(test.existing, e)

			want := test.added
			if test.wantKeptExisting {
				want = test.existing
			}
			if got := m.Add(test.added, e); got != want {
				t.Fatalf("Add() returned level %d for %v (hidden particles %v), want level %d for %v (hidden particles %v)",
					got.Level(), got.Duration(), got.ParticlesHidden(), want.Level(), want.Duration(), want.ParticlesHidden())
			}
			stored, ok := m.Effect(effect.Speed)
			if !ok {
				t.Fatal("Effect(Speed) = false after a replacement decision, want an effect")
			}
			if stored != want {
				t.Fatalf("stored effect = level %d for %v (hidden particles %v), want level %d for %v (hidden particles %v)",
					stored.Level(), stored.Duration(), stored.ParticlesHidden(), want.Level(), want.Duration(), want.ParticlesHidden())
			}
			if n := len(m.Effects()); n != 1 {
				t.Fatalf("Effects() = %d, want 1", n)
			}
		})
	}
}

func TestEffectManagerReplacementSwapsTheMapEntryBeforeEndAndStart(t *testing.T) {
	// Add writes the new effect into the map first and only then runs End on the
	// old one and Start on the new one. A LastingType that reads the manager back
	// during End therefore sees the effect that replaced it, never itself.
	e := newLivingFake(20, 20)
	m := NewEffectManager()
	m.Add(effect.New(effect.Speed, 1, 10*time.Second), e)

	var seen []int
	e.observe = func(c livingCall) {
		if c.name != "SetSpeed" {
			return
		}
		stored, ok := m.Effect(effect.Speed)
		if !ok {
			t.Error("Effect(Speed) = false during a Start or End call, want the stored effect")
			return
		}
		seen = append(seen, stored.Level())
	}
	m.Add(effect.New(effect.Speed, 2, 10*time.Second), e)

	if want := []int{2, 2}; !slices.Equal(seen, want) {
		t.Fatalf("levels stored during End and Start = %v, want %v", seen, want)
	}

	// Speed multiplies by 1+lvl*0.2 on Start and divides by it on End. The three
	// values pin the order: Start(1), then End(1), then Start(2). Running Start
	// before End would give 0.168 and 0.14 instead.
	speeds := e.callsNamed("SetSpeed")
	want := []float64{0.1 * 1.2, 0.1, 0.1 * 1.4}
	if len(speeds) != len(want) {
		t.Fatalf("SetSpeed calls = %d, want %d", len(speeds), len(want))
	}
	for i, w := range want {
		if math.Abs(speeds[i].v-w) > physEpsilon {
			t.Fatalf("SetSpeed call %d = %.17g, want %.17g", i, speeds[i].v, w)
		}
	}
}

func TestEffectManagerReplacementRefusalRunsNothing(t *testing.T) {
	// When the existing effect wins, neither End nor Start runs and the entity is
	// untouched.
	e := newLivingFake(20, 20)
	m := NewEffectManager()
	m.Add(effect.New(effect.Speed, 2, 10*time.Second), e)
	before := len(e.calls)

	m.Add(effect.New(effect.Speed, 1, 10*time.Minute), e)
	if got := len(e.calls); got != before {
		t.Fatalf("calls on the entity = %v, want no calls beyond the %d from the first Add", e.callNames(), before)
	}
}

func TestEffectManagerInitialEffectsAreNotAppliedUntilFlushed(t *testing.T) {
	e := newLivingFake(20, 20)
	initial := effect.New(effect.Speed, 1, 10*time.Second)
	m := NewEffectManager(initial)

	if n := len(e.calls); n != 0 {
		t.Fatalf("calls on the entity after NewEffectManager = %v, want none", e.callNames())
	}

	got, ok := m.Effect(effect.Speed)
	if !ok || got != initial {
		t.Fatalf("Effect(Speed) = %v, %v before a flush, want the initial effect and true", got, ok)
	}
	if n := len(e.calls); n != 0 {
		t.Fatalf("Effect() flushed the initial effects: calls = %v, want none", e.callNames())
	}

	// PINNED BUG: Effects() reports initial effects without flushing them, so it
	// lists an effect whose LastingType.Start has never run. A caller reading
	// Effects() to render or serialise state sees an effect the entity is not
	// actually under.
	all := m.Effects()
	if len(all) != 1 || all[0] != initial {
		t.Fatalf("Effects() = %v, want exactly the initial effect", all)
	}
	if n := len(e.calls); n != 0 {
		t.Fatalf("Effects() flushed the initial effects: calls = %v, want none", e.callNames())
	}
	if math.Abs(e.Speed()-0.1) > physEpsilon {
		t.Fatalf("Speed() = %v, want 0.1 because Speed.Start never ran", e.Speed())
	}
}

func TestEffectManagerFlushesInitialEffects(t *testing.T) {
	tests := []struct {
		name string
		act  func(m *EffectManager, e *livingFake, tx *world.Tx)
	}{
		{name: "Add", act: func(m *EffectManager, e *livingFake, _ *world.Tx) {
			m.Add(effect.New(effect.Regeneration, 1, time.Second), e)
		}},
		{name: "Remove", act: func(m *EffectManager, e *livingFake, _ *world.Tx) {
			m.Remove(effect.Regeneration, e)
		}},
		{name: "Tick", act: func(m *EffectManager, e *livingFake, tx *world.Tx) {
			m.Tick(e, tx)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			e := newLivingFake(20, 20)
			m := NewEffectManager(effect.New(effect.Speed, 1, 10*time.Second))
			mustDo(t, w, func(tx *world.Tx) { test.act(m, e, tx) })

			speeds := e.callsNamed("SetSpeed")
			if len(speeds) != 1 {
				t.Fatalf("SetSpeed calls after %s = %d, want 1 from the flushed Speed.Start", test.name, len(speeds))
			}
			if want := 0.1 * 1.2; math.Abs(speeds[0].v-want) > physEpsilon {
				t.Fatalf("SetSpeed = %.17g, want %.17g", speeds[0].v, want)
			}
			if _, ok := m.Effect(effect.Speed); !ok {
				t.Fatalf("Effect(Speed) = false after %s flushed the initial effects, want true", test.name)
			}
		})
	}
}

func TestEffectManagerEffectsAppendsInitialEffectsAfterStoredOnes(t *testing.T) {
	// Effects() is append(stored..., initial...). This ordering is only reachable
	// by building the manager directly: every exported entry point that fills the
	// map flushes the initial effects first, so no caller can observe both halves
	// through the public API today. Pinned because a rewrite of the storage layer
	// is free to make it observable.
	stored := effect.New(effect.Speed, 1, time.Second)
	initial := effect.New(effect.Regeneration, 1, time.Second)
	m := &EffectManager{
		effects:        map[reflect.Type]effect.Effect{reflect.TypeOf(stored.Type()): stored},
		initialEffects: []effect.Effect{initial},
	}
	got := m.Effects()
	if len(got) != 2 {
		t.Fatalf("Effects() = %d effects, want 2", len(got))
	}
	if got[0] != stored {
		t.Fatalf("Effects()[0] = %v, want the stored effect", got[0])
	}
	if got[1] != initial {
		t.Fatalf("Effects()[1] = %v, want the initial effect", got[1])
	}
}

func TestEffectManagerTickAppliesBeforeAdvancingTheTickCounter(t *testing.T) {
	// Tick calls Apply on the stored effect and only then stores TickDuration's
	// result, so the first Tick sees Tick() == 0. Every interval effect uses
	// Tick()%interval == 0, so all of them fire on the very first tick rather
	// than after one full interval.
	w := syncWorld(t)
	e := newLivingFake(10, 20)
	m := NewEffectManager()
	m.Add(effect.New(effect.Regeneration, 1, time.Minute), e)

	mustDo(t, w, func(tx *world.Tx) {
		for range 51 {
			m.Tick(e, tx)
		}
	})

	// Regeneration's interval is max(50>>(lvl-1), 1) = 50, so ticks 0 and 50 heal.
	heals := e.callsNamed("Heal")
	if len(heals) != 2 {
		t.Fatalf("Heal calls over 51 ticks = %d, want 2 (ticks 0 and 50)", len(heals))
	}
	for i, call := range heals {
		if math.Abs(call.v-1) > physEpsilon {
			t.Fatalf("Heal call %d amount = %v, want 1", i, call.v)
		}
		if want := (effect.RegenerationHealingSource{}); call.arg != want {
			t.Fatalf("Heal call %d source = %#v, want %#v", i, call.arg, want)
		}
	}
	if want := 12.0; math.Abs(e.Health()-want) > physEpsilon {
		t.Fatalf("Health() after two regeneration heals = %v, want %v", e.Health(), want)
	}
}

func TestEffectManagerTickPoisonFiresImmediatelyAndSpares(t *testing.T) {
	// Poison's interval is max(25>>(lvl-1), 1) and it refuses to reduce health to
	// 1 or below. FatalPoison shares the interval but has no such guard.
	tests := []struct {
		name      string
		eff       effect.Effect
		health    float64
		ticks     int
		wantHurts int
		wantFatal bool
	}{
		{name: "poison level one fires on tick zero and again on tick 25", eff: effect.New(effect.Poison, 1, time.Minute), health: 20, ticks: 26, wantHurts: 2},
		{name: "poison level two halves the interval", eff: effect.New(effect.Poison, 2, time.Minute), health: 20, ticks: 26, wantHurts: 3},
		{name: "poison stops at one health", eff: effect.New(effect.Poison, 1, time.Minute), health: 2, ticks: 26, wantHurts: 1},
		{name: "fatal poison keeps going below one health", eff: effect.New(effect.FatalPoison, 1, time.Minute), health: 2, ticks: 26, wantHurts: 2, wantFatal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			e := newLivingFake(test.health, 20)
			m := NewEffectManager()
			m.Add(test.eff, e)
			mustDo(t, w, func(tx *world.Tx) {
				for range test.ticks {
					m.Tick(e, tx)
				}
			})
			hurts := e.callsNamed("Hurt")
			if len(hurts) != test.wantHurts {
				t.Fatalf("Hurt calls over %d ticks = %d, want %d", test.ticks, len(hurts), test.wantHurts)
			}
			for i, call := range hurts {
				if math.Abs(call.v-1) > physEpsilon {
					t.Fatalf("Hurt call %d amount = %v, want 1", i, call.v)
				}
				if want := (effect.PoisonDamageSource{Fatal: test.wantFatal}); call.arg != want {
					t.Fatalf("Hurt call %d source = %#v, want %#v", i, call.arg, want)
				}
			}
		})
	}
}

func TestEffectManagerTickRemovesExpiredEffects(t *testing.T) {
	// An effect expires when its duration reaches 0 and it is not infinite. The
	// broadcast to tx.Viewers happens once per Tick and only when something
	// actually expired.
	t.Run("an expiring effect ends and notifies viewers", func(t *testing.T) {
		w := syncWorld(t)
		v := &livingViewer{}
		e := newLivingFake(20, 20)
		m := NewEffectManager()
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, v)
			m.Add(effect.New(effect.Speed, 1, time.Second/20), e)

			m.Tick(e, tx)
			if n := len(m.Effects()); n != 1 {
				t.Fatalf("Effects() after the tick that spends the last duration = %d, want 1", n)
			}
			if n := len(v.states); n != 0 {
				t.Fatalf("ViewEntityState calls before anything expired = %d, want 0", n)
			}

			m.Tick(e, tx)
			if n := len(m.Effects()); n != 0 {
				t.Fatalf("Effects() after the expiring tick = %d, want 0", n)
			}
			if n := len(v.states); n != 1 {
				t.Fatalf("ViewEntityState calls after an expiry = %d, want 1", n)
			}
			if v.states[0] != world.Entity(e) {
				t.Fatalf("ViewEntityState received %v, want the ticked entity", v.states[0])
			}
			if math.Abs(e.Speed()-0.1) > physEpsilon {
				t.Fatalf("Speed() after Speed.End = %.17g, want 0.1 restored", e.Speed())
			}
		})
	})

	t.Run("a live effect notifies nobody", func(t *testing.T) {
		w := syncWorld(t)
		v := &livingViewer{}
		e := newLivingFake(20, 20)
		m := NewEffectManager()
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, v)
			m.Add(effect.New(effect.Speed, 1, time.Minute), e)
			for range 5 {
				m.Tick(e, tx)
			}
			if n := len(v.states); n != 0 {
				t.Fatalf("ViewEntityState calls with nothing expiring = %d, want 0", n)
			}
			if n := len(m.Effects()); n != 1 {
				t.Fatalf("Effects() = %d, want 1 still stored", n)
			}
		})
	})

	t.Run("an infinite effect never expires", func(t *testing.T) {
		w := syncWorld(t)
		v := &livingViewer{}
		e := newLivingFake(20, 20)
		m := NewEffectManager()
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, v)
			m.Add(effect.NewInfinite(effect.Speed, 1), e)
			for range 20 {
				m.Tick(e, tx)
			}
			stored, ok := m.Effect(effect.Speed)
			if !ok {
				t.Fatal("Effect(Speed) = false for an infinite effect after 20 ticks, want true")
			}
			if got := stored.Duration(); got != 0 {
				t.Fatalf("infinite effect Duration() after 20 ticks = %v, want 0 unchanged", got)
			}
			if got := stored.Tick(); got != 20 {
				t.Fatalf("infinite effect Tick() = %d, want 20", got)
			}
			if n := len(v.states); n != 0 {
				t.Fatalf("ViewEntityState calls for an infinite effect = %d, want 0", n)
			}
		})
	})

	t.Run("several expiring effects share one broadcast", func(t *testing.T) {
		w := syncWorld(t)
		v := &livingViewer{}
		e := newLivingFake(20, 20)
		m := NewEffectManager()
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, v)
			m.Add(effect.New(effect.Speed, 1, 0), e)
			m.Add(effect.New(effect.Regeneration, 1, 0), e)
			m.Tick(e, tx)
			if n := len(m.Effects()); n != 0 {
				t.Fatalf("Effects() = %d, want 0", n)
			}
			if n := len(v.states); n != 1 {
				t.Fatalf("ViewEntityState calls for two expiries in one Tick = %d, want 1", n)
			}
		})
	})
}

func TestEffectManagerEffectsOrderIsIndeterminate(t *testing.T) {
	// LATENT: EffectManager stores effects in a map, so Effects() and Tick() run
	// in a random order every call. Order-sensitive effect interactions (an
	// expiring HealthBoost clamping health via SetMaxHealth against a Regeneration
	// heal in the same tick) are already nondeterministic today. Pinned as an
	// unordered set on purpose; asserting a sequence here would be a false
	// failure.
	e := newLivingFake(20, 20)
	m := NewEffectManager()
	effs := []effect.Effect{
		effect.New(effect.Speed, 1, time.Minute),
		effect.New(effect.Regeneration, 1, time.Minute),
		effect.New(effect.Poison, 1, time.Minute),
		effect.New(effect.Wither, 1, time.Minute),
	}
	for _, eff := range effs {
		m.Add(eff, e)
	}

	want := livingEffectCounts(effs)
	orders := map[string]struct{}{}
	const rounds = 500
	for range rounds {
		got := m.Effects()
		if len(got) != len(effs) {
			t.Fatalf("Effects() = %d effects, want %d", len(got), len(effs))
		}
		if counts := livingEffectCounts(got); !maps.Equal(counts, want) {
			t.Fatalf("Effects() set = %v, want %v", counts, want)
		}
		orders[livingEffectOrder(got)] = struct{}{}
	}
	t.Logf("Effects() produced %d distinct orders over %d calls with %d stored effects", len(orders), rounds, len(effs))
}

func TestEffectManagerRemove(t *testing.T) {
	t.Run("removes a stored effect and runs End", func(t *testing.T) {
		e := newLivingFake(20, 20)
		m := NewEffectManager()
		m.Add(effect.New(effect.Speed, 1, time.Minute), e)
		m.Remove(effect.Speed, e)

		if n := len(m.Effects()); n != 0 {
			t.Fatalf("Effects() after Remove = %d, want 0", n)
		}
		if _, ok := m.Effect(effect.Speed); ok {
			t.Fatal("Effect(Speed) = true after Remove, want false")
		}
		if want := []string{"SetSpeed", "SetSpeed"}; !slices.Equal(e.callNames(), want) {
			t.Fatalf("calls on the entity = %v, want %v (Start then End)", e.callNames(), want)
		}
		if math.Abs(e.Speed()-0.1) > physEpsilon {
			t.Fatalf("Speed() after Remove = %.17g, want 0.1 restored", e.Speed())
		}
	})

	t.Run("removing an absent effect does nothing", func(t *testing.T) {
		e := newLivingFake(20, 20)
		m := NewEffectManager()
		m.Add(effect.New(effect.Speed, 1, time.Minute), e)
		before := len(e.calls)

		m.Remove(effect.Regeneration, e)
		if got := len(e.calls); got != before {
			t.Fatalf("calls on the entity = %v, want no calls beyond the %d from Add", e.callNames(), before)
		}
		if n := len(m.Effects()); n != 1 {
			t.Fatalf("Effects() = %d, want the untouched Speed effect", n)
		}
	})

	t.Run("flushes initial effects before removing them", func(t *testing.T) {
		// Removing an effect that is still an unflushed initial effect starts it
		// and immediately ends it, so both Start and End run for an effect the
		// caller only ever asked to remove.
		e := newLivingFake(20, 20)
		m := NewEffectManager(effect.New(effect.Speed, 1, time.Minute))
		m.Remove(effect.Speed, e)

		if want := []string{"SetSpeed", "SetSpeed"}; !slices.Equal(e.callNames(), want) {
			t.Fatalf("calls on the entity = %v, want %v (flushed Start then End)", e.callNames(), want)
		}
		if n := len(m.Effects()); n != 0 {
			t.Fatalf("Effects() = %d, want 0", n)
		}
	})
}

// ---------------------------------------------------------------------------
// Damage sources
// ---------------------------------------------------------------------------

func TestDamageSourceProperties(t *testing.T) {
	// The complete truth table for damage.go. Note that no source in this file
	// returns Fire() == true: fire damage sources live in the block package, so
	// fire resistance never blocks any of these.
	//
	// affectedBy is nil for the sources that do not implement
	// enchantment.AffectedDamageSource at all. For those the interface assertion
	// in ProtectionFactor fails and no source-specific protection enchantment can
	// ever reduce them.
	tests := []struct {
		name        string
		src         world.DamageSource
		armour      bool
		resistance  bool
		fire        bool
		ignoreTotem bool
		affectedBy  item.EnchantmentType
	}{
		{name: "attack", src: AttackDamageSource{}, armour: true, resistance: true},
		{name: "void", src: VoidDamageSource{}, ignoreTotem: true},
		{name: "suffocation", src: SuffocationDamageSource{}},
		{name: "drowning", src: DrowningDamageSource{}},
		{name: "fall", src: FallDamageSource{}, resistance: true, affectedBy: enchantment.FeatherFalling},
		{name: "glide", src: GlideDamageSource{}, resistance: true},
		{name: "lightning", src: LightningDamageSource{}, armour: true, resistance: true},
		{name: "projectile", src: ProjectileDamageSource{}, armour: true, resistance: true, affectedBy: enchantment.ProjectileProtection},
		{name: "explosion", src: ExplosionDamageSource{}, armour: true, resistance: true, affectedBy: enchantment.BlastProtection},
	}
	others := []item.EnchantmentType{
		enchantment.FeatherFalling,
		enchantment.ProjectileProtection,
		enchantment.BlastProtection,
		enchantment.FireProtection,
		enchantment.Protection,
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.src.ReducedByArmour(); got != test.armour {
				t.Fatalf("ReducedByArmour() = %v, want %v", got, test.armour)
			}
			if got := test.src.ReducedByResistance(); got != test.resistance {
				t.Fatalf("ReducedByResistance() = %v, want %v", got, test.resistance)
			}
			if got := test.src.Fire(); got != test.fire {
				t.Fatalf("Fire() = %v, want %v", got, test.fire)
			}
			if got := test.src.IgnoreTotem(); got != test.ignoreTotem {
				t.Fatalf("IgnoreTotem() = %v, want %v", got, test.ignoreTotem)
			}

			affected, ok := test.src.(enchantment.AffectedDamageSource)
			if want := test.affectedBy != nil; ok != want {
				t.Fatalf("implements enchantment.AffectedDamageSource = %v, want %v", ok, want)
			}
			if !ok {
				return
			}
			if !affected.AffectedByEnchantment(test.affectedBy) {
				t.Fatalf("AffectedByEnchantment(%T) = false, want true", test.affectedBy)
			}
			for _, other := range others {
				if other == test.affectedBy {
					continue
				}
				if affected.AffectedByEnchantment(other) {
					t.Fatalf("AffectedByEnchantment(%T) = true, want only %T to match", other, test.affectedBy)
				}
			}
		})
	}
}

func TestProtectionFactorFollowsTheDamageSourceInterface(t *testing.T) {
	// The consequence of the table above. Generic Protection is special-cased on
	// ReducedByResistance and ignores AffectedByEnchantment entirely, so a source
	// that implements nothing is still protected against by Protection but by no
	// targeted enchantment.
	tests := []struct {
		name string
		src  world.DamageSource
		ench item.Enchantment
		want float64
	}{
		{name: "blast protection against an explosion", src: ExplosionDamageSource{}, ench: item.NewEnchantment(enchantment.BlastProtection, 4), want: 4 * 0.08},
		{name: "blast protection against gliding, which implements no enchantment hook", src: GlideDamageSource{}, ench: item.NewEnchantment(enchantment.BlastProtection, 4)},
		{name: "feather falling against a fall", src: FallDamageSource{}, ench: item.NewEnchantment(enchantment.FeatherFalling, 4), want: 4 * 0.12},
		{name: "feather falling against an explosion", src: ExplosionDamageSource{}, ench: item.NewEnchantment(enchantment.FeatherFalling, 4)},
		{name: "generic protection against gliding, via ReducedByResistance", src: GlideDamageSource{}, ench: item.NewEnchantment(enchantment.Protection, 4), want: 4 * 0.04},
		{name: "generic protection against the void, which resistance does not reduce", src: VoidDamageSource{}, ench: item.NewEnchantment(enchantment.Protection, 4)},
		{name: "the factor is capped at 0.8", src: ExplosionDamageSource{}, ench: item.NewEnchantment(enchantment.BlastProtection, 20), want: 0.8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := enchantment.ProtectionFactor(test.src, []item.Enchantment{test.ench})
			if math.Abs(got-test.want) > physEpsilon {
				t.Fatalf("ProtectionFactor() = %v, want %v", got, test.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HurtEntity / DamageableEntity
// ---------------------------------------------------------------------------

func TestHurtEntityLivingPath(t *testing.T) {
	tests := []struct {
		name           string
		n              float64
		vulnerable     bool
		wantN          float64
		wantVulnerable bool
	}{
		{name: "a vulnerable entity reports the damage it took", n: 7, vulnerable: true, wantN: 7, wantVulnerable: true},
		// ok is about whether the entity can be damaged at all, vulnerable about
		// whether this particular hit landed. An immune entity still reports ok.
		{name: "an invulnerable entity still reports ok", n: 0, vulnerable: false, wantN: 0},
		{name: "damage absorbed to zero is still vulnerable", n: 0, vulnerable: true, wantVulnerable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := newLivingFake(20, 20)
			e.hurt = func(float64, world.DamageSource) (float64, bool) { return test.n, test.vulnerable }

			src := AttackDamageSource{}
			n, vulnerable, ok := HurtEntity(e, 9, src)
			if !ok {
				t.Fatal("HurtEntity() ok = false for a Living entity, want true")
			}
			if math.Abs(n-test.wantN) > physEpsilon {
				t.Fatalf("HurtEntity() n = %v, want %v", n, test.wantN)
			}
			if vulnerable != test.wantVulnerable {
				t.Fatalf("HurtEntity() vulnerable = %v, want %v", vulnerable, test.wantVulnerable)
			}
			calls := e.callsNamed("Hurt")
			if len(calls) != 1 {
				t.Fatalf("Living.Hurt calls = %d, want 1", len(calls))
			}
			if math.Abs(calls[0].v-9) > physEpsilon {
				t.Fatalf("Living.Hurt damage = %v, want 9 forwarded unchanged", calls[0].v)
			}
			if calls[0].arg != world.DamageSource(src) {
				t.Fatalf("Living.Hurt source = %#v, want %#v", calls[0].arg, src)
			}
			if !DamageableEntity(e) {
				t.Fatal("DamageableEntity() = false for a Living entity, want true")
			}
		})
	}
}

func TestHurtEntityBehaviourPath(t *testing.T) {
	// An *Ent whose Behaviour implements Hurt is damageable without being Living.
	// Everything the projectile path does after a successful hit — knockback,
	// potion effects, setting the victim on fire — is guarded by a Living
	// assertion in projectile.go:hitEntity, so none of it reaches this entity.
	tests := []struct {
		name       string
		n          float64
		vulnerable bool
	}{
		{name: "a vulnerable behaviour", n: 5, vulnerable: true},
		{name: "an invulnerable behaviour still reports ok", n: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			b := &livingHurtBehaviour{n: test.n, vulnerable: test.vulnerable}
			livingWithBehaviour(t, w, b, func(e *Ent) {
				if _, isLiving := any(e).(Living); isLiving {
					t.Fatal("*Ent implements Living, want the behaviour path to be the only way to hurt it")
				}
				if !DamageableEntity(e) {
					t.Fatal("DamageableEntity() = false for an *Ent with a damageable behaviour, want true")
				}

				src := VoidDamageSource{}
				n, vulnerable, ok := HurtEntity(e, 3, src)
				if !ok {
					t.Fatal("HurtEntity() ok = false, want true")
				}
				if math.Abs(n-test.n) > physEpsilon {
					t.Fatalf("HurtEntity() n = %v, want %v", n, test.n)
				}
				if vulnerable != test.vulnerable {
					t.Fatalf("HurtEntity() vulnerable = %v, want %v", vulnerable, test.vulnerable)
				}
				if len(b.got) != 1 {
					t.Fatalf("Behaviour.Hurt calls = %d, want 1", len(b.got))
				}
				if math.Abs(b.got[0].v-3) > physEpsilon {
					t.Fatalf("Behaviour.Hurt damage = %v, want 3 forwarded unchanged", b.got[0].v)
				}
				if b.got[0].arg != world.DamageSource(src) {
					t.Fatalf("Behaviour.Hurt source = %#v, want %#v", b.got[0].arg, src)
				}
			})
		})
	}
}

func TestHurtEntityUndamageableEntities(t *testing.T) {
	w := syncWorld(t)

	t.Run("an Ent whose behaviour cannot be hurt", func(t *testing.T) {
		livingWithBehaviour(t, w, &testMoveBehaviour{}, func(e *Ent) {
			n, vulnerable, ok := HurtEntity(e, 10, AttackDamageSource{})
			if n != 0 || vulnerable || ok {
				t.Fatalf("HurtEntity() = %v, %v, %v, want 0, false, false", n, vulnerable, ok)
			}
			if DamageableEntity(e) {
				t.Fatal("DamageableEntity() = true for a behaviour without Hurt, want false")
			}
		})
	})

	t.Run("an entity that is neither Living nor an Ent", func(t *testing.T) {
		e := livingPlainEntity{handle: world.EntitySpawnOpts{}.New(testMovingEntType{}, testMoveConfig{})}
		n, vulnerable, ok := HurtEntity(e, 10, AttackDamageSource{})
		if n != 0 || vulnerable || ok {
			t.Fatalf("HurtEntity() = %v, %v, %v, want 0, false, false", n, vulnerable, ok)
		}
		if DamageableEntity(e) {
			t.Fatal("DamageableEntity() = true for a plain world.Entity, want false")
		}
	})
}

func TestHurtEntityLivingIsCheckedBeforeTheBehaviour(t *testing.T) {
	// PINNED SCOPE: HurtEntity checks Living first and only then asserts the
	// concrete type *Ent, so the documented precedence is unobservable: *Ent does
	// not implement Living and no other type can be an *Ent. The ordering is
	// pinned here as the two branches being mutually exclusive. A rewrite that
	// replaces the *Ent assertion with an interface would make the precedence
	// reachable and must decide it deliberately.
	w := syncWorld(t)
	livingWithBehaviour(t, w, &livingHurtBehaviour{n: 1, vulnerable: true}, func(e *Ent) {
		if _, ok := any(e).(Living); ok {
			t.Fatal("*Ent implements Living, so the two HurtEntity branches overlap")
		}
	})
	if _, ok := any(newLivingFake(20, 20)).(*Ent); ok {
		t.Fatal("a Living implementation is also an *Ent, so the two HurtEntity branches overlap")
	}
}

func TestHurtEntityForwardsExtremeDamageUnchanged(t *testing.T) {
	// HurtEntity validates nothing. It hands the damage to the target verbatim
	// and returns whatever the target reports, so every guard has to live in the
	// target. These comparisons are deliberately exact: the point is that no
	// arithmetic happened on the way through.
	tests := []struct {
		name   string
		damage float64
	}{
		{name: "negative", damage: -5},
		{name: "zero", damage: 0},
		{name: "positive infinity", damage: math.Inf(1)},
		{name: "negative infinity", damage: math.Inf(-1)},
		{name: "not a number", damage: math.NaN()},
	}
	src := AttackDamageSource{}
	w := syncWorld(t)
	for _, test := range tests {
		t.Run("living/"+test.name, func(t *testing.T) {
			e := newLivingFake(20, 20)
			e.hurt = func(damage float64, _ world.DamageSource) (float64, bool) { return damage, false }

			n, vulnerable, ok := HurtEntity(e, test.damage, src)
			if !ok || vulnerable {
				t.Fatalf("HurtEntity() vulnerable, ok = %v, %v, want false, true", vulnerable, ok)
			}
			calls := e.callsNamed("Hurt")
			if len(calls) != 1 {
				t.Fatalf("Living.Hurt calls = %d, want 1", len(calls))
			}
			if !livingSameFloat(calls[0].v, test.damage) {
				t.Fatalf("Living.Hurt damage = %v, want %v forwarded verbatim", calls[0].v, test.damage)
			}
			if !livingSameFloat(n, test.damage) {
				t.Fatalf("HurtEntity() n = %v, want %v returned verbatim", n, test.damage)
			}
		})

		t.Run("behaviour/"+test.name, func(t *testing.T) {
			b := &livingHurtBehaviour{n: test.damage, vulnerable: true, echo: true}
			livingWithBehaviour(t, w, b, func(e *Ent) {
				n, vulnerable, ok := HurtEntity(e, test.damage, src)
				if !ok || !vulnerable {
					t.Fatalf("HurtEntity() vulnerable, ok = %v, %v, want true, true", vulnerable, ok)
				}
				if len(b.got) != 1 {
					t.Fatalf("Behaviour.Hurt calls = %d, want 1", len(b.got))
				}
				if !livingSameFloat(b.got[0].v, test.damage) {
					t.Fatalf("Behaviour.Hurt damage = %v, want %v forwarded verbatim", b.got[0].v, test.damage)
				}
				if !livingSameFloat(n, test.damage) {
					t.Fatalf("HurtEntity() n = %v, want %v returned verbatim", n, test.damage)
				}
			})
		})
	}
}

func TestHurtEntityHealthConsequencesOfExtremeDamage(t *testing.T) {
	// What the damage does once it lands is HealthManager.AddHealth's doing,
	// reached as AddHealth(-damage). *player.Player rejects dmg < 0 before it gets
	// there, so the negative case is guarded in the only production Living, but
	// the guard is `dmg < 0` — which is false for NaN.
	t.Run("negative damage adds health", func(t *testing.T) {
		e := newLivingFake(10, 20)
		HurtEntity(e, -5, AttackDamageSource{})
		if want := 15.0; math.Abs(e.Health()-want) > physEpsilon {
			t.Fatalf("Health() after -5 damage = %v, want %v", e.Health(), want)
		}
	})

	t.Run("infinite damage clamps to zero health", func(t *testing.T) {
		e := newLivingFake(10, 20)
		HurtEntity(e, math.Inf(1), AttackDamageSource{})
		if math.Abs(e.Health()) > physEpsilon {
			t.Fatalf("Health() after +Inf damage = %v, want 0", e.Health())
		}
		if !e.Dead() {
			t.Fatal("Dead() = false after +Inf damage, want true")
		}
	})

	t.Run("negative infinite damage clamps to the max", func(t *testing.T) {
		e := newLivingFake(10, 20)
		HurtEntity(e, math.Inf(-1), AttackDamageSource{})
		if want := 20.0; math.Abs(e.Health()-want) > physEpsilon {
			t.Fatalf("Health() after -Inf damage = %v, want %v", e.Health(), want)
		}
	})

	t.Run("NaN damage leaves health alone", func(t *testing.T) {
		// NaN damage reaches HealthManager.AddHealth, which ignores it, so the
		// entity can still be killed afterwards.
		e := newLivingFake(10, 20)
		HurtEntity(e, math.NaN(), AttackDamageSource{})
		if e.Health() != 10 {
			t.Fatalf("Health() after NaN damage = %v, want 10", e.Health())
		}
		HurtEntity(e, 1000, AttackDamageSource{})
		if !e.Dead() {
			t.Fatalf("Dead() = false after 1000 damage, want true (health %v)", e.Health())
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// livingCall is one recorded mutation of a livingFake, kept in call order so a
// test can pin the sequence the health, damage and effect layers produce.
type livingCall struct {
	name string
	v    float64 // health, damage or speed argument; 0 for calls that have none
	arg  any     // world.DamageSource, world.HealingSource, effect.Effect or effect.Type
}

// livingFake is a minimal entity.Living used to observe what the health, damage
// and effect layers do to a living entity. *player.Player is the only production
// Living and is far too heavy to build in a unit test.
//
// Its health is a real HealthManager reached the same way Player reaches it, so
// clamping seen through Health() is production behaviour rather than the fake's.
// It is never added to a world: only H() needs the handle.
type livingFake struct {
	handle *world.EntityHandle
	pos    mgl64.Vec3
	health *HealthManager
	speed  float64
	vel    mgl64.Vec3
	added  []effect.Effect
	calls  []livingCall

	// observe runs after every recorded call. Tests use it to read manager state
	// from inside a LastingType's Start or End.
	observe func(livingCall)
	// hurt replaces the default damage application and decides the (n, vulnerable)
	// pair Hurt returns. Health is left untouched while it is set.
	hurt func(damage float64, src world.DamageSource) (float64, bool)
}

// newLivingFake returns a livingFake at the origin chunk, so addViewer's loader
// covers it, with a default speed of 0.1.
func newLivingFake(health, maxHealth float64) *livingFake {
	e := &livingFake{pos: mgl64.Vec3{0.5, 64, 0.5}, health: NewHealthManager(health, maxHealth), speed: 0.1}
	e.handle = world.EntitySpawnOpts{Position: e.pos}.New(livingEntType{}, livingConf{e: e})
	return e
}

func (e *livingFake) record(c livingCall) {
	e.calls = append(e.calls, c)
	if e.observe != nil {
		e.observe(c)
	}
}

func (e *livingFake) callsNamed(name string) []livingCall {
	var out []livingCall
	for _, c := range e.calls {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

func (e *livingFake) callNames() []string {
	names := make([]string, len(e.calls))
	for i, c := range e.calls {
		names[i] = c.name
	}
	return names
}

func (e *livingFake) H() *world.EntityHandle { return e.handle }
func (e *livingFake) Position() mgl64.Vec3   { return e.pos }
func (e *livingFake) Rotation() cube.Rotation {
	return cube.Rotation{}
}
func (e *livingFake) Close() error { return nil }

func (e *livingFake) Health() float64    { return e.health.Health() }
func (e *livingFake) MaxHealth() float64 { return e.health.MaxHealth() }
func (e *livingFake) Dead() bool         { return e.health.Health() <= 0 }

func (e *livingFake) SetMaxHealth(v float64) {
	e.health.SetMaxHealth(v)
	e.record(livingCall{name: "SetMaxHealth", v: v})
}

func (e *livingFake) Hurt(damage float64, src world.DamageSource) (n float64, vulnerable bool) {
	n, vulnerable = damage, true
	if e.hurt != nil {
		n, vulnerable = e.hurt(damage, src)
	} else {
		e.health.AddHealth(-damage)
	}
	e.record(livingCall{name: "Hurt", v: damage, arg: src})
	return n, vulnerable
}

func (e *livingFake) Heal(health float64, src world.HealingSource) float64 {
	e.health.AddHealth(health)
	e.record(livingCall{name: "Heal", v: health, arg: src})
	return health
}

func (e *livingFake) KnockBack(src mgl64.Vec3, force, _ float64) {
	e.record(livingCall{name: "KnockBack", v: force, arg: src})
}

func (e *livingFake) Velocity() mgl64.Vec3 { return e.vel }

func (e *livingFake) SetVelocity(velocity mgl64.Vec3) {
	e.vel = velocity
	e.record(livingCall{name: "SetVelocity", arg: velocity})
}

func (e *livingFake) AddEffect(eff effect.Effect) {
	e.added = append(e.added, eff)
	e.record(livingCall{name: "AddEffect", v: float64(eff.Level()), arg: eff})
}

func (e *livingFake) RemoveEffect(t effect.Type) {
	e.record(livingCall{name: "RemoveEffect", arg: t})
}

func (e *livingFake) Effects() []effect.Effect { return e.added }

func (e *livingFake) Speed() float64 { return e.speed }

func (e *livingFake) SetSpeed(v float64) {
	e.speed = v
	e.record(livingCall{name: "SetSpeed", v: v})
}

// livingEntType exists only to give livingFake a real world.EntityHandle. Open
// hands the same fake back so a handle opened in a transaction cannot diverge
// from the value the test holds.
type livingEntType struct{}

func (livingEntType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	e := data.Data.(*livingFake)
	e.handle = handle
	return e
}

func (livingEntType) EncodeEntity() string { return "impulse:test_living" }

func (livingEntType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}

func (livingEntType) DecodeNBT(map[string]any, *world.EntityData) {}

func (livingEntType) EncodeNBT(*world.EntityData) map[string]any { return nil }

type livingConf struct{ e *livingFake }

func (c livingConf) Apply(data *world.EntityData) { data.Data = c.e }

// livingViewer records the entities EffectManager.Tick broadcasts a state update
// for.
type livingViewer struct {
	world.NopViewer

	states []world.Entity
}

func (v *livingViewer) ViewEntityState(e world.Entity) { v.states = append(v.states, e) }

// livingPlainEntity is a world.Entity that is neither Living nor an *Ent, so
// HurtEntity has no path to it.
type livingPlainEntity struct{ handle *world.EntityHandle }

func (e livingPlainEntity) H() *world.EntityHandle { return e.handle }
func (livingPlainEntity) Position() mgl64.Vec3     { return mgl64.Vec3{} }
func (livingPlainEntity) Rotation() cube.Rotation  { return cube.Rotation{} }
func (livingPlainEntity) Close() error             { return nil }

// livingHurtBehaviour is an Ent Behaviour that satisfies behaviourDamageable, the
// only way an entity that is not Living can be hurt.
type livingHurtBehaviour struct {
	n          float64
	vulnerable bool
	// echo returns the damage received instead of n, to pin verbatim forwarding.
	echo bool
	got  []livingCall
}

func (*livingHurtBehaviour) Tick(*Ent, *world.Tx) *Movement { return nil }

func (b *livingHurtBehaviour) Hurt(_ *Ent, damage float64, src world.DamageSource) (float64, bool) {
	b.got = append(b.got, livingCall{name: "Hurt", v: damage, arg: src})
	if b.echo {
		return damage, b.vulnerable
	}
	return b.n, b.vulnerable
}

type livingBehaviourConf struct{ b Behaviour }

func (c livingBehaviourConf) Apply(data *world.EntityData) { data.Data = c.b }

// livingWithBehaviour spawns an *Ent driven by b in w and hands it to f inside a
// transaction.
func livingWithBehaviour(t *testing.T, w *world.World, b Behaviour, f func(e *Ent)) {
	t.Helper()
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(testMovingEntType{}, livingBehaviourConf{b: b})
	mustDo(t, w, func(tx *world.Tx) {
		added := tx.AddEntity(handle)
		e, ok := added.(*Ent)
		if !ok {
			t.Fatalf("AddEntity() returned %T, want *Ent", added)
		}
		f(e)
	})
}

// livingRecover runs f and returns the value it panicked with, or nil if it
// returned normally.
func livingRecover(f func()) (recovered any) {
	defer func() { recovered = recover() }()
	f()
	return nil
}

// livingSameFloat reports whether two floats are the same value, counting NaN as
// equal to NaN so verbatim forwarding of extreme inputs can be pinned.
func livingSameFloat(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	return a == b
}

// livingEffectCounts keys effects the way EffectManager does, so a set can be
// compared without depending on map iteration order.
func livingEffectCounts(effs []effect.Effect) map[string]int {
	counts := make(map[string]int, len(effs))
	for _, e := range effs {
		counts[reflect.TypeOf(e.Type()).String()]++
	}
	return counts
}

// livingEffectOrder renders the order of a slice of effects as a single string,
// for counting how many distinct orders a map-backed API produces.
func livingEffectOrder(effs []effect.Effect) string {
	names := make([]string, len(effs))
	for i, e := range effs {
		names[i] = reflect.TypeOf(e.Type()).String()
	}
	return strings.Join(names, ",")
}
