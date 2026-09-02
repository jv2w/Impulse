package effect

import (
	"image/color"
	"reflect"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// This file pins the Effect value type: what the constructors accept, what they
// leave unvalidated, and how an Effect changes as it is ticked. The numbers each
// effect produces are pinned in level_test.go and the ID mapping in
// register_test.go.
//
// The effect manager that applies these values lives in server/entity and is
// pinned there. Two failures reported from that side originate here and are
// pinned below: effect.New accepts a level of 0 or less, and several Apply
// implementations shift by Level()-1 and therefore panic on such a level.

// testLiving implements every interface an effect in this package looks for, so
// one fixture serves all of them. Each call is recorded so that a test can
// assert what an effect did rather than only what it returned.
type testLiving struct {
	health, maxHealth float64
	speed             float64

	absorption float64
	invisible  bool
	exhaustion float64
	food       int
	saturation float64

	hurts []hurt
	heals []float64
}

// state is the comparable part of a testLiving, used to assert that an effect
// left the entity alone.
type state struct {
	health, maxHealth, speed, absorption, exhaustion, saturation float64
	food                                                         int
	invisible                                                    bool
}

func (l *testLiving) state() state {
	return state{health: l.health, maxHealth: l.maxHealth, speed: l.speed, absorption: l.absorption,
		exhaustion: l.exhaustion, saturation: l.saturation, food: l.food, invisible: l.invisible}
}

type hurt struct {
	damage float64
	source world.DamageSource
}

func newTestLiving() *testLiving {
	return &testLiving{health: 20, maxHealth: 20, speed: 0.1}
}

func (*testLiving) H() *world.EntityHandle  { return nil }
func (*testLiving) Position() mgl64.Vec3    { return mgl64.Vec3{} }
func (*testLiving) Rotation() cube.Rotation { return cube.Rotation{} }
func (*testLiving) Close() error            { return nil }

func (l *testLiving) Health() float64           { return l.health }
func (l *testLiving) MaxHealth() float64        { return l.maxHealth }
func (l *testLiving) SetMaxHealth(v float64)    { l.maxHealth = v }
func (l *testLiving) Speed() float64            { return l.speed }
func (l *testLiving) SetSpeed(v float64)        { l.speed = v }
func (l *testLiving) SetAbsorption(v float64)   { l.absorption = v }
func (l *testLiving) SetInvisible()             { l.invisible = true }
func (l *testLiving) SetVisible()               { l.invisible = false }
func (l *testLiving) Exhaust(points float64)    { l.exhaustion += points }
func (l *testLiving) Saturate(f int, s float64) { l.food += f; l.saturation += s }

func (l *testLiving) Hurt(damage float64, source world.DamageSource) (float64, bool) {
	l.hurts = append(l.hurts, hurt{damage: damage, source: source})
	l.health -= damage
	return damage, true
}

func (l *testLiving) Heal(health float64, _ world.HealingSource) float64 {
	l.heals = append(l.heals, health)
	l.health += health
	return health
}

// deafEntity implements world.Entity and nothing else, so it exercises the
// optional-interface branches every effect guards its work with.
type deafEntity struct{}

func (deafEntity) H() *world.EntityHandle  { return nil }
func (deafEntity) Position() mgl64.Vec3    { return mgl64.Vec3{} }
func (deafEntity) Rotation() cube.Rotation { return cube.Rotation{} }
func (deafEntity) Close() error            { return nil }

// mustPanic runs f and returns the value it panicked with, failing the test if
// f returned normally.
func mustPanic(t *testing.T, f func()) (v any) {
	t.Helper()
	defer func() { v = recover() }()
	f()
	t.Fatal("expected a panic, but the call returned normally")
	return nil
}

func TestConstructorsValidateNothing(t *testing.T) {
	// PINNED BUG: none of the four constructors checks the level or the
	// duration. A level of 0 or less and a negative duration are both accepted
	// and stored verbatim, and the resulting Effect is a perfectly ordinary
	// value that an entity will happily be given. The level checks live in
	// entity.EffectManager.Add, which panics rather than returning a value, so
	// there is currently no path that rejects a bad level without crashing.
	// CLAUDE.md 2.2: the validation belongs here, as a (Effect, error) result.
	cases := []struct {
		name string
		eff  Effect
		lvl  int
		d    time.Duration
	}{
		{name: "level zero", eff: New(Speed, 0, time.Second), lvl: 0, d: time.Second},
		{name: "negative level", eff: New(Speed, -5, time.Second), lvl: -5, d: time.Second},
		{name: "negative duration", eff: New(Speed, 1, -time.Second), lvl: 1, d: -time.Second},
		{name: "ambient with level zero", eff: NewAmbient(Speed, 0, time.Second), lvl: 0, d: time.Second},
		{name: "infinite with negative level", eff: NewInfinite(Speed, -1), lvl: -1},
		{name: "instant with level zero", eff: NewInstant(InstantHealth, 0), lvl: 0},
		{name: "instant with negative level", eff: NewInstant(InstantDamage, -2), lvl: -2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.eff.Level(); got != c.lvl {
				t.Fatalf("Level() = %d, want %d", got, c.lvl)
			}
			if got := c.eff.Duration(); got != c.d {
				t.Fatalf("Duration() = %v, want %v", got, c.d)
			}
		})
	}
}

func TestConstructorFlags(t *testing.T) {
	// The four constructors differ only in the flags they set. Duration is
	// always zero for an instant or infinite effect, and Ambient and Infinite
	// are mutually exclusive because no constructor sets both.
	cases := []struct {
		name              string
		eff               Effect
		duration          time.Duration
		ambient, infinite bool
	}{
		{name: "New", eff: New(Speed, 1, 5*time.Second), duration: 5 * time.Second},
		{name: "NewAmbient", eff: NewAmbient(Speed, 1, 5*time.Second), duration: 5 * time.Second, ambient: true},
		{name: "NewInfinite", eff: NewInfinite(Speed, 1), infinite: true},
		{name: "NewInstant", eff: NewInstant(InstantHealth, 1)},
		{name: "NewInstantWithPotency", eff: NewInstantWithPotency(InstantHealth, 1, 0.5)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.eff.Duration(); got != c.duration {
				t.Fatalf("Duration() = %v, want %v", got, c.duration)
			}
			if got := c.eff.Ambient(); got != c.ambient {
				t.Fatalf("Ambient() = %v, want %v", got, c.ambient)
			}
			if got := c.eff.Infinite(); got != c.infinite {
				t.Fatalf("Infinite() = %v, want %v", got, c.infinite)
			}
			if c.eff.ParticlesHidden() {
				t.Fatal("ParticlesHidden() = true, want false: only WithoutParticles sets it")
			}
			if got := c.eff.Tick(); got != 0 {
				t.Fatalf("Tick() = %d, want 0", got)
			}
		})
	}
}

func TestWithoutParticlesReturnsACopy(t *testing.T) {
	// Effect is a value type and every builder returns a copy, so an Effect that
	// has been handed out cannot be changed behind the holder's back.
	original := New(Speed, 1, time.Second)
	hidden := original.WithoutParticles()
	if original.ParticlesHidden() {
		t.Fatal("WithoutParticles mutated the receiver")
	}
	if !hidden.ParticlesHidden() {
		t.Fatal("the returned Effect does not have particles hidden")
	}
}

func TestPotencyDefaultsDifferPerConstructor(t *testing.T) {
	// PINNED BUG: only NewInstant and NewInstantWithPotency set a potency. Every
	// other constructor leaves it at 0, and so does the zero Effect. Potency
	// only feeds instantHealth and instantDamage, which are instant types and
	// therefore unreachable through New, but a hand-built Effect{} or an Effect
	// from a decoder would heal and damage for exactly nothing.
	l := newTestLiving()
	InstantHealth.Apply(l, NewInstant(InstantHealth, 1))
	if got, want := l.heals, []float64{4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("heals = %v, want %v", got, want)
	}

	l = newTestLiving()
	InstantHealth.Apply(l, Effect{t: InstantHealth, lvl: 1})
	if got, want := l.heals, []float64{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("heals = %v, want %v: an Effect built without a potency heals nothing", got, want)
	}
}

func TestTickDurationOnlyAdvancesLastingEffects(t *testing.T) {
	// TickDuration subtracts one twentieth of a second and increments the tick
	// counter, but only for a LastingType. An instant effect is returned
	// unchanged, so its Tick() stays 0 no matter how often it is ticked.
	eff := New(Speed, 1, time.Second)
	for i := range 3 {
		if got, want := eff.Tick(), i; got != want {
			t.Fatalf("Tick() = %d, want %d", got, want)
		}
		eff = eff.TickDuration()
	}
	if got, want := eff.Duration(), time.Second-3*(time.Second/20); got != want {
		t.Fatalf("Duration() = %v, want %v", got, want)
	}

	instant := NewInstant(InstantHealth, 1)
	for range 3 {
		instant = instant.TickDuration()
	}
	if got := instant.Tick(); got != 0 {
		t.Fatalf("Tick() = %d for an instant effect, want 0", got)
	}
}

func TestTickDurationOnAnInfiniteEffect(t *testing.T) {
	// An infinite effect still advances its tick counter, which is what the
	// interval-based effects use, but its duration never moves off zero.
	eff := NewInfinite(Poison, 1)
	for range 5 {
		eff = eff.TickDuration()
	}
	if got, want := eff.Tick(), 5; got != want {
		t.Fatalf("Tick() = %d, want %d", got, want)
	}
	if got := eff.Duration(); got != 0 {
		t.Fatalf("Duration() = %v, want 0", got)
	}
	if !eff.Infinite() {
		t.Fatal("Infinite() = false after ticking")
	}
}

func TestTickDurationGoesNegative(t *testing.T) {
	// PINNED CONSTRAINT: TickDuration does not stop at zero. Expiry is entirely
	// the caller's job, so an Effect that is ticked past its duration keeps
	// counting down into negative time while still reporting a growing tick.
	eff := New(Speed, 1, time.Second/20)
	eff = eff.TickDuration().TickDuration().TickDuration()
	if got, want := eff.Duration(), -2*(time.Second/20); got != want {
		t.Fatalf("Duration() = %v, want %v", got, want)
	}
	if got, want := eff.Tick(), 3; got != want {
		t.Fatalf("Tick() = %d, want %d", got, want)
	}
}

func TestZeroEffectIsInertExceptForItsColour(t *testing.T) {
	// PINNED BUG: the zero Effect has a nil Type. TickDuration tolerates it
	// because the type assertion simply fails, but Type() returns nil and
	// ResultingColour calls RGBA on it without a check, so a single zero Effect
	// in a list crashes the colour calculation.
	var eff Effect
	if eff.Type() != nil {
		t.Fatalf("Type() = %v, want nil", eff.Type())
	}
	if got := eff.TickDuration(); got != eff {
		t.Fatalf("TickDuration() = %#v, want the zero Effect unchanged", got)
	}
	if v := mustPanic(t, func() { ResultingColour([]Effect{eff}) }); v == nil {
		t.Fatal("ResultingColour did not panic on a zero Effect")
	}
}

func TestResultingColour(t *testing.T) {
	// The colour is the arithmetic mean of every visible effect's RGBA with
	// integer truncation, and the fallback for an empty list is the vanilla
	// potion blue. Ambient is true only when every counted effect is ambient.
	cases := []struct {
		name    string
		effects []Effect
		want    color.RGBA
		ambient bool
	}{
		{name: "no effects", effects: nil, want: color.RGBA{R: 0x38, G: 0x5d, B: 0xc6, A: 0xff}},
		{name: "single effect", effects: []Effect{NewInfinite(Speed, 1)},
			want: color.RGBA{R: 0x33, G: 0xeb, B: 0xff, A: 0xff}},
		{name: "two effects are averaged", effects: []Effect{NewInfinite(Speed, 1), NewInfinite(Poison, 1)},
			want: color.RGBA{R: 0x5d, G: 0xc7, B: 0xb1, A: 0xff}},
		{name: "ambient only", effects: []Effect{NewAmbient(Speed, 1, time.Second)},
			want: color.RGBA{R: 0x33, G: 0xeb, B: 0xff, A: 0xff}, ambient: true},
		{name: "one non-ambient effect clears the flag",
			effects: []Effect{NewAmbient(Speed, 1, time.Second), NewInfinite(Poison, 1)},
			want:    color.RGBA{R: 0x5d, G: 0xc7, B: 0xb1, A: 0xff}},
		{name: "hidden effects are skipped entirely",
			effects: []Effect{NewInfinite(Speed, 1), NewInfinite(Poison, 1).WithoutParticles()},
			want:    color.RGBA{R: 0x33, G: 0xeb, B: 0xff, A: 0xff}},
		{name: "every effect hidden falls back to the default",
			effects: []Effect{NewInfinite(Speed, 1).WithoutParticles()},
			want:    color.RGBA{R: 0x38, G: 0x5d, B: 0xc6, A: 0xff}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ambient := ResultingColour(c.effects)
			if got != c.want {
				t.Fatalf("ResultingColour() = %#v, want %#v", got, c.want)
			}
			if ambient != c.ambient {
				t.Fatalf("ResultingColour() ambient = %v, want %v", ambient, c.ambient)
			}
		})
	}
}

func TestHiddenNonAmbientEffectDoesNotClearTheAmbientFlag(t *testing.T) {
	// PINNED BUG: the particle-hidden check continues the loop before the
	// ambient check, so a non-ambient effect with hidden particles never clears
	// the flag. The result is an ambient particle rate for an entity that has a
	// non-ambient effect.
	got, ambient := ResultingColour([]Effect{
		NewInfinite(Speed, 1).WithoutParticles(),
		NewAmbient(Poison, 1, time.Second),
	})
	if want := (color.RGBA{R: 0x87, G: 0xa3, B: 0x63, A: 0xff}); got != want {
		t.Fatalf("ResultingColour() = %#v, want %#v", got, want)
	}
	if !ambient {
		t.Fatal("ambient = false, want true: the hidden non-ambient effect must be skipped before the ambient check")
	}
}

func TestLastingAndInstantTypesArePartitioned(t *testing.T) {
	// Exhaustive check of which registered effects are lasting. Only the two
	// instant effects are not, which is what makes them unusable with New: the
	// compiler rejects effect.New(InstantHealth, ...) because New takes a
	// LastingType. That compile-time guard is the package's only real
	// validation, so the partition must not drift.
	instant := map[reflect.Type]bool{
		reflect.TypeOf(InstantHealth): true,
		reflect.TypeOf(InstantDamage): true,
	}
	for id := range 64 {
		e, ok := ByID(id)
		if !ok {
			continue
		}
		_, lasting := e.(LastingType)
		if want := !instant[reflect.TypeOf(e)]; lasting != want {
			t.Fatalf("effect %d (%T): implements LastingType = %v, want %v", id, e, lasting, want)
		}
	}
	if len(instant) != 2 {
		t.Fatalf("the instant set holds %d types, want 2", len(instant))
	}
}

func TestNopLastingDoesNothing(t *testing.T) {
	// Most effects embed nopLasting and are pure client-side markers: Start, End
	// and Apply are all no-ops, and the effect exists only to carry an ID and a
	// colour to the client. Pinning this keeps a future implementation from
	// being added by accident rather than on purpose.
	markers := []LastingType{Blindness, Darkness, FireResistance, JumpBoost, Levitation,
		Nausea, NightVision, SlowFalling, WaterBreathing, ConduitPower, Haste,
		MiningFatigue, Resistance, Strength, Weakness}
	for _, m := range markers {
		l := newTestLiving()
		before := l.state()
		m.Start(l, 3)
		m.Apply(l, NewInfinite(m, 3))
		m.End(l, 3)
		if got := l.state(); got != before {
			t.Fatalf("%T changed the entity: %+v, want %+v", m, got, before)
		}
		if len(l.hurts) != 0 || len(l.heals) != 0 {
			t.Fatalf("%T hurt or healed the entity: %v, %v", m, l.hurts, l.heals)
		}
	}
}

func TestEffectsIgnoreEntitiesThatDoNotImplementTheirInterface(t *testing.T) {
	// Every effect that needs more than world.Entity guards its work with a type
	// assertion and silently does nothing when it fails. An effect applied to
	// the wrong kind of entity is therefore a no-op, not an error and not a
	// panic.
	e := deafEntity{}
	for _, f := range []func(){
		func() { Absorption.Start(e, 2) },
		func() { Absorption.End(e, 2) },
		func() { HealthBoost.Start(e, 2) },
		func() { HealthBoost.End(e, 2) },
		func() { Invisibility.Start(e, 1) },
		func() { Invisibility.End(e, 1) },
		func() { Speed.Start(e, 1) },
		func() { Speed.End(e, 1) },
		func() { Slowness.Start(e, 1) },
		func() { Slowness.End(e, 1) },
		func() { Hunger.Apply(e, NewInfinite(Hunger, 1)) },
		func() { Saturation.Apply(e, NewInfinite(Saturation, 1)) },
		func() { Poison.Apply(e, NewInfinite(Poison, 1)) },
		func() { FatalPoison.Apply(e, NewInfinite(FatalPoison, 1)) },
		func() { Regeneration.Apply(e, NewInfinite(Regeneration, 1)) },
		func() { Wither.Apply(e, NewInfinite(Wither, 1)) },
		func() { InstantHealth.Apply(e, NewInstant(InstantHealth, 1)) },
		func() { InstantDamage.Apply(e, NewInstant(InstantDamage, 1)) },
	} {
		f()
	}
}

func TestDamageSourceProperties(t *testing.T) {
	// The three damage sources this package defines all bypass armour and are
	// all reduced by resistance, and none of them ignores a totem. A change
	// here silently changes how much damage every poisoned or withering entity
	// takes.
	cases := []struct {
		name                             string
		source                           world.DamageSource
		armour, resistance, fire, ignore bool
	}{
		{name: "poison", source: PoisonDamageSource{}, resistance: true},
		{name: "fatal poison", source: PoisonDamageSource{Fatal: true}, resistance: true},
		{name: "wither", source: WitherDamageSource{}, resistance: true},
		{name: "instant damage", source: InstantDamageSource{}, resistance: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.source.ReducedByArmour(); got != c.armour {
				t.Fatalf("ReducedByArmour() = %v, want %v", got, c.armour)
			}
			if got := c.source.ReducedByResistance(); got != c.resistance {
				t.Fatalf("ReducedByResistance() = %v, want %v", got, c.resistance)
			}
			if got := c.source.Fire(); got != c.fire {
				t.Fatalf("Fire() = %v, want %v", got, c.fire)
			}
			if got := c.source.IgnoreTotem(); got != c.ignore {
				t.Fatalf("IgnoreTotem() = %v, want %v", got, c.ignore)
			}
		})
	}
}

func TestPoisonSourceCarriesFatality(t *testing.T) {
	// Poison and fatalPoison share one damage source type and are told apart by
	// the Fatal field. A handler distinguishing them has to read the field: the
	// two are the same Go type.
	l := newTestLiving()
	Poison.Apply(l, NewInfinite(Poison, 1))
	if got := l.hurts[0].source.(PoisonDamageSource); got.Fatal {
		t.Fatal("poison reported Fatal = true")
	}

	l = newTestLiving()
	FatalPoison.Apply(l, NewInfinite(FatalPoison, 1))
	if got := l.hurts[0].source.(PoisonDamageSource); !got.Fatal {
		t.Fatal("fatal poison reported Fatal = false")
	}
}
