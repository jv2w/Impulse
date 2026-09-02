package effect

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/world"
)

// This file pins the number every effect produces at every level that matters:
// level 0, level 1, the level where a formula saturates or clamps, and one level
// past it. The values are written out as literals, including the ones that are
// not the decimal they look like, because a float that reads 0.4 but is
// 0.3999999999999999 is exactly the kind of difference a "roughly equal"
// assertion hides and a damage calculation does not.

func TestSpeedMultiplier(t *testing.T) {
	// Speed multiplies the entity's speed by 1 + 0.2*lvl on Start and divides by
	// the same factor on End. The factor is unclamped in both directions.
	cases := []struct {
		lvl  int
		want float64
	}{
		{lvl: -1, want: 0.08000000000000002},
		{lvl: 0, want: 0.1},
		{lvl: 1, want: 0.12},
		{lvl: 2, want: 0.13999999999999999},
		{lvl: 3, want: 0.16000000000000003},
		{lvl: 5, want: 0.2},
		{lvl: 10, want: 0.30000000000000004},
	}
	for _, c := range cases {
		l := newTestLiving()
		Speed.Start(l, c.lvl)
		if l.speed != c.want {
			t.Fatalf("level %d: speed = %v, want %v", c.lvl, l.speed, c.want)
		}
	}
}

func TestSpeedStartEndRoundTripLosesPrecision(t *testing.T) {
	// PINNED BUG: End divides by the factor Start multiplied by, which is not an
	// exact inverse in binary floating point. An entity that gains and loses
	// speed II ends up with a speed that is not the one it started with, and the
	// error accumulates over repeated applications.
	lossy := []int{2, 3}
	for _, lvl := range lossy {
		l := newTestLiving()
		before := l.speed
		Speed.Start(l, lvl)
		Speed.End(l, lvl)
		if l.speed == before {
			t.Fatalf("level %d: the round trip was exact, so this pin needs updating", lvl)
		}
	}
	// Levels whose factor happens to round-trip exactly are pinned too, so that
	// the set of lossy levels cannot quietly grow.
	for _, lvl := range []int{1, 4, 5} {
		l := newTestLiving()
		before := l.speed
		Speed.Start(l, lvl)
		Speed.End(l, lvl)
		if l.speed != before {
			t.Fatalf("level %d: speed = %v after a round trip, want %v", lvl, l.speed, before)
		}
	}
}

func TestSlownessMultiplier(t *testing.T) {
	// Slowness multiplies by 1 - 0.15*lvl, replacing a factor of zero or less
	// with 0.00001 so that End can still divide by it. Level 7 is the first
	// level to hit the clamp.
	cases := []struct {
		lvl  int
		want float64
	}{
		{lvl: 0, want: 0.1},
		{lvl: 1, want: 0.085},
		{lvl: 2, want: 0.06999999999999999},
		{lvl: 5, want: 0.025},
		{lvl: 6, want: 0.010000000000000009},
		{lvl: 7, want: 1.0000000000000002e-06},
		{lvl: 100, want: 1.0000000000000002e-06},
	}
	for _, c := range cases {
		l := newTestLiving()
		Slowness.Start(l, c.lvl)
		if l.speed != c.want {
			t.Fatalf("level %d: speed = %v, want %v", c.lvl, l.speed, c.want)
		}
	}
}

func TestSlownessClampMakesEndWrong(t *testing.T) {
	// PINNED BUG: the clamp is applied independently in Start and in End, so a
	// level past the clamp divides by 0.00001 and restores the original speed by
	// luck rather than by design. The moment the entity's speed is changed by
	// anything else while slowness is active, End multiplies that new speed by
	// 100000.
	l := newTestLiving()
	Slowness.Start(l, 7)
	l.SetSpeed(0.2)
	Slowness.End(l, 7)
	if want := 20000.0; l.speed != want {
		t.Fatalf("speed = %v, want %v: End must divide by the clamped factor", l.speed, want)
	}
}

func TestMiningSpeedMultipliers(t *testing.T) {
	// Haste and conduit power share one formula, 1 - 0.1*lvl clamped at zero,
	// and mining fatigue uses 3^lvl. These are break-time multipliers, so haste
	// lowering the number is what makes mining faster.
	//
	// PINNED CONSTRAINT: the doc comments disagree with the code. haste.go says
	// "increases the mining speed by 20% for each level" while the factor moves
	// by 0.1 per level, and mining_fatigue.go says "decreases the mining speed
	// by 10% for each level" while the factor is a power of three.
	cases := []struct {
		lvl                     int
		haste, conduit, fatigue float64
	}{
		{lvl: 0, haste: 1, conduit: 1, fatigue: 1},
		{lvl: 1, haste: 0.9, conduit: 0.9, fatigue: 3},
		{lvl: 2, haste: 0.8, conduit: 0.8, fatigue: 9},
		{lvl: 3, haste: 0.7, conduit: 0.7, fatigue: 27},
		{lvl: 6, haste: 0.3999999999999999, conduit: 0.3999999999999999, fatigue: 729},
		{lvl: 10, haste: 0, conduit: 0, fatigue: 59049},
		{lvl: 11, haste: 0, conduit: 0, fatigue: 177147},
	}
	for _, c := range cases {
		if got := Haste.Multiplier(c.lvl); got != c.haste {
			t.Fatalf("Haste.Multiplier(%d) = %v, want %v", c.lvl, got, c.haste)
		}
		if got := ConduitPower.Multiplier(c.lvl); got != c.conduit {
			t.Fatalf("ConduitPower.Multiplier(%d) = %v, want %v", c.lvl, got, c.conduit)
		}
		if got := MiningFatigue.Multiplier(c.lvl); got != c.fatigue {
			t.Fatalf("MiningFatigue.Multiplier(%d) = %v, want %v", c.lvl, got, c.fatigue)
		}
	}
	// PINNED BUG: only haste and conduit power clamp. A negative level makes all
	// three return a multiplier that speeds mining up rather than slowing it
	// down, and mining fatigue at a negative level returns a fraction.
	if got, want := Haste.Multiplier(-1), 1.1; got != want {
		t.Fatalf("Haste.Multiplier(-1) = %v, want %v", got, want)
	}
	if got, want := MiningFatigue.Multiplier(-1), 1.0/3.0; got != want {
		t.Fatalf("MiningFatigue.Multiplier(-1) = %v, want %v", got, want)
	}
}

func TestStrengthAndWeaknessMultipliers(t *testing.T) {
	// Strength adds 0.3 per level with no cap at all; weakness subtracts 0.2 per
	// level and saturates at 1, which is total immunity to the wielder's own
	// melee damage.
	strength := []struct {
		lvl  int
		want float64
	}{
		{lvl: 0, want: 0},
		{lvl: 1, want: 0.3},
		{lvl: 2, want: 0.6},
		{lvl: 3, want: 0.8999999999999999},
		{lvl: 10, want: 3},
		{lvl: -1, want: -0.3},
	}
	for _, c := range strength {
		if got := Strength.Multiplier(c.lvl); got != c.want {
			t.Fatalf("Strength.Multiplier(%d) = %v, want %v", c.lvl, got, c.want)
		}
	}

	weakness := []struct {
		lvl  int
		want float64
	}{
		{lvl: 0, want: 0},
		{lvl: 1, want: 0.2},
		{lvl: 3, want: 0.6000000000000001},
		{lvl: 5, want: 1},
		{lvl: 6, want: 1},
		{lvl: 100, want: 1},
		{lvl: -1, want: -0.2},
	}
	for _, c := range weakness {
		if got := Weakness.Multiplier(c.lvl); got != c.want {
			t.Fatalf("Weakness.Multiplier(%d) = %v, want %v", c.lvl, got, c.want)
		}
	}
}

func TestResistanceMultiplier(t *testing.T) {
	// Resistance reduces damage by 20% per level and floors at zero, which level
	// 5 reaches exactly. A source that is not reduced by resistance is returned
	// unchanged whatever the level is.
	cases := []struct {
		lvl  int
		want float64
	}{
		{lvl: 0, want: 1},
		{lvl: 1, want: 0.8},
		{lvl: 2, want: 0.6},
		{lvl: 3, want: 0.3999999999999999},
		{lvl: 4, want: 0.19999999999999996},
		{lvl: 5, want: 0},
		{lvl: 6, want: 0},
		{lvl: -1, want: 1.2},
	}
	for _, c := range cases {
		if got := Resistance.Multiplier(PoisonDamageSource{}, c.lvl); got != c.want {
			t.Fatalf("Resistance.Multiplier(%d) = %v, want %v", c.lvl, got, c.want)
		}
		if got := Resistance.Multiplier(unreducedSource{}, c.lvl); got != 1 {
			t.Fatalf("Resistance.Multiplier(%d) on an unreduced source = %v, want 1", c.lvl, got)
		}
	}
}

// unreducedSource is a damage source that resistance must not touch.
type unreducedSource struct{}

func (unreducedSource) ReducedByArmour() bool     { return false }
func (unreducedSource) ReducedByResistance() bool { return false }
func (unreducedSource) Fire() bool                { return false }
func (unreducedSource) IgnoreTotem() bool         { return false }

func TestAbsorptionAndHealthBoostAmounts(t *testing.T) {
	// Both add four points per level. Absorption sets an absolute value and
	// clears it on End; health boost adds to and subtracts from the current
	// maximum health, so its End is only correct if nothing else changed the
	// maximum in between.
	cases := []struct {
		lvl        int
		absorption float64
		maxHealth  float64
	}{
		{lvl: 0, absorption: 0, maxHealth: 20},
		{lvl: 1, absorption: 4, maxHealth: 24},
		{lvl: 2, absorption: 8, maxHealth: 28},
		{lvl: 5, absorption: 20, maxHealth: 40},
		{lvl: -1, absorption: -4, maxHealth: 16},
	}
	for _, c := range cases {
		l := newTestLiving()
		Absorption.Start(l, c.lvl)
		if l.absorption != c.absorption {
			t.Fatalf("level %d: absorption = %v, want %v", c.lvl, l.absorption, c.absorption)
		}
		Absorption.End(l, c.lvl)
		if l.absorption != 0 {
			t.Fatalf("level %d: absorption = %v after End, want 0", c.lvl, l.absorption)
		}

		l = newTestLiving()
		HealthBoost.Start(l, c.lvl)
		if l.maxHealth != c.maxHealth {
			t.Fatalf("level %d: max health = %v, want %v", c.lvl, l.maxHealth, c.maxHealth)
		}
		HealthBoost.End(l, c.lvl)
		if l.maxHealth != 20 {
			t.Fatalf("level %d: max health = %v after End, want 20", c.lvl, l.maxHealth)
		}
	}
}

func TestHealthBoostEndSubtractsFromWhateverTheMaximumIsNow(t *testing.T) {
	// PINNED BUG: End subtracts 4*lvl from the *current* maximum rather than
	// restoring the one Start saw. Anything that changes the maximum while the
	// effect is active is undone by the wrong amount, and a negative level
	// raises the maximum on Start and lowers it on End.
	l := newTestLiving()
	HealthBoost.Start(l, 2)
	l.SetMaxHealth(100)
	HealthBoost.End(l, 2)
	if want := 92.0; l.maxHealth != want {
		t.Fatalf("max health = %v, want %v", l.maxHealth, want)
	}
}

func TestAbsorptionEndClearsRegardlessOfLevel(t *testing.T) {
	// PINNED BUG: End ignores the level and sets absorption to zero, so removing
	// one absorption effect wipes the absorption granted by any other source.
	l := newTestLiving()
	l.SetAbsorption(12)
	Absorption.End(l, 1)
	if l.absorption != 0 {
		t.Fatalf("absorption = %v, want 0", l.absorption)
	}
}

func TestInvisibilityIsLevelIndependent(t *testing.T) {
	// Invisibility ignores its level entirely: any level turns the entity
	// invisible and End turns it visible again.
	for _, lvl := range []int{-1, 0, 1, 5} {
		l := newTestLiving()
		Invisibility.Start(l, lvl)
		if !l.invisible {
			t.Fatalf("level %d: the entity is still visible", lvl)
		}
		Invisibility.End(l, lvl)
		if l.invisible {
			t.Fatalf("level %d: the entity is still invisible after End", lvl)
		}
	}
}

func TestInstantHealthAndDamageAmounts(t *testing.T) {
	// Instant health heals 2<<lvl and instant damage deals 3<<lvl, both scaled
	// by the potency. The shift means the amount doubles per level rather than
	// growing linearly.
	cases := []struct {
		lvl          int
		potency      float64
		heal, damage float64
	}{
		{lvl: 0, potency: 1, heal: 2, damage: 3},
		{lvl: 1, potency: 1, heal: 4, damage: 6},
		{lvl: 2, potency: 1, heal: 8, damage: 12},
		{lvl: 3, potency: 1, heal: 16, damage: 24},
		{lvl: 1, potency: 0.5, heal: 2, damage: 3},
		{lvl: 1, potency: 2, heal: 8, damage: 12},
		{lvl: 1, potency: 0, heal: 0, damage: 0},
	}
	for _, c := range cases {
		l := newTestLiving()
		InstantHealth.Apply(l, NewInstantWithPotency(InstantHealth, c.lvl, c.potency))
		if got, want := l.heals, []float64{c.heal}; !reflect.DeepEqual(got, want) {
			t.Fatalf("level %d potency %v: heals = %v, want %v", c.lvl, c.potency, got, want)
		}

		l = newTestLiving()
		InstantDamage.Apply(l, NewInstantWithPotency(InstantDamage, c.lvl, c.potency))
		if len(l.hurts) != 1 || l.hurts[0].damage != c.damage {
			t.Fatalf("level %d potency %v: hurts = %v, want damage %v", c.lvl, c.potency, l.hurts, c.damage)
		}
	}
}

func TestInstantEffectsPanicOnANegativeLevel(t *testing.T) {
	// PINNED BUG: the amount is computed with a left shift by the level, so a
	// negative level is a negative shift count and panics at run time. Nothing
	// between a potion's NBT and this shift rejects such a level.
	l := newTestLiving()
	for _, f := range []func(){
		func() { InstantHealth.Apply(l, NewInstant(InstantHealth, -1)) },
		func() { InstantDamage.Apply(l, NewInstant(InstantDamage, -1)) },
	} {
		v := mustPanic(t, f)
		if _, ok := v.(runtimeError); !ok && v == nil {
			t.Fatalf("panic = %v, want a negative shift error", v)
		}
	}
}

// runtimeError matches the runtime error type a bad shift panics with.
type runtimeError interface {
	error
	RuntimeError()
}

func TestIntervalEffects(t *testing.T) {
	// Poison, fatal poison, regeneration and wither all act once every N ticks,
	// where N halves per level and floors at 1. Poison and regeneration shift by
	// lvl-1 while wither shifts by lvl, so wither level 1 is twice as fast as
	// its interval table would suggest from the other three.
	cases := []struct {
		lvl                              int
		poison, regeneration, witherTick int
	}{
		{lvl: 1, poison: 25, regeneration: 50, witherTick: 40},
		{lvl: 2, poison: 12, regeneration: 25, witherTick: 20},
		{lvl: 3, poison: 6, regeneration: 12, witherTick: 10},
		{lvl: 4, poison: 3, regeneration: 6, witherTick: 5},
		{lvl: 5, poison: 1, regeneration: 3, witherTick: 2},
		{lvl: 6, poison: 1, regeneration: 1, witherTick: 1},
		{lvl: 9, poison: 1, regeneration: 1, witherTick: 1},
	}
	for _, c := range cases {
		if got := intervalOf(t, Poison, c.lvl); got != c.poison {
			t.Fatalf("poison level %d: interval = %d, want %d", c.lvl, got, c.poison)
		}
		if got := intervalOf(t, FatalPoison, c.lvl); got != c.poison {
			t.Fatalf("fatal poison level %d: interval = %d, want %d", c.lvl, got, c.poison)
		}
		if got := intervalOf(t, Regeneration, c.lvl); got != c.regeneration {
			t.Fatalf("regeneration level %d: interval = %d, want %d", c.lvl, got, c.regeneration)
		}
		if got := intervalOf(t, Wither, c.lvl); got != c.witherTick {
			t.Fatalf("wither level %d: interval = %d, want %d", c.lvl, got, c.witherTick)
		}
	}
	// Wither shifts by the level itself, so level 0 is the slowest interval it
	// has rather than a panic.
	if got, want := intervalOf(t, Wither, 0), 80; got != want {
		t.Fatalf("wither level 0: interval = %d, want %d", got, want)
	}
}

// intervalOf drives an effect over enough ticks to observe how often it acts and
// returns the tick spacing between the first two actions.
func intervalOf(t *testing.T, e LastingType, lvl int) int {
	t.Helper()
	l := newTestLiving()
	eff := NewInfinite(e, lvl)
	var acted []int
	for tick := 0; tick < 400 && len(acted) < 2; tick++ {
		// Poison refuses to act below two health, so the entity is topped up
		// between ticks to keep the interval the only variable.
		l.health = 20
		before := len(l.hurts) + len(l.heals)
		e.Apply(l, eff)
		if len(l.hurts)+len(l.heals) > before {
			acted = append(acted, tick)
		}
		eff = eff.TickDuration()
	}
	if len(acted) < 2 {
		t.Fatalf("%T level %d acted %d times in 400 ticks, want at least 2", e, lvl, len(acted))
	}
	if acted[0] != 0 {
		t.Fatalf("%T level %d first acted on tick %d, want 0", e, lvl, acted[0])
	}
	return acted[1] - acted[0]
}

func TestIntervalEffectsPanicAtLevelZero(t *testing.T) {
	// PINNED BUG: poison, fatal poison and regeneration compute their interval
	// as 25>>(lvl-1) or 50>>(lvl-1). At level 0 that is a shift by -1, which
	// panics. effect.New accepts level 0, so the only thing standing between a
	// caller and this panic is entity.EffectManager.Add, which rejects the level
	// by panicking too. There is no non-crashing path.
	l := newTestLiving()
	for _, e := range []LastingType{Poison, FatalPoison, Regeneration} {
		v := mustPanic(t, func() { e.Apply(l, NewInfinite(e, 0)) })
		if v == nil {
			t.Fatalf("%T did not panic at level 0", e)
		}
	}
	// Wither shifts by the level rather than lvl-1, so it survives level 0 and
	// panics one level lower instead.
	if v := mustPanic(t, func() { Wither.Apply(l, NewInfinite(Wither, -1)) }); v == nil {
		t.Fatal("wither did not panic at level -1")
	}
}

func TestPoisonNeverKillsButFatalPoisonDoes(t *testing.T) {
	// The one behavioural difference between the two: poison checks that the
	// entity is above one health before dealing its point of damage, so it
	// stops at exactly 1. Fatal poison has no such check.
	l := newTestLiving()
	l.health = 1
	Poison.Apply(l, NewInfinite(Poison, 1))
	if len(l.hurts) != 0 {
		t.Fatalf("poison hurt an entity at 1 health: %v", l.hurts)
	}

	l = newTestLiving()
	l.health = 1
	FatalPoison.Apply(l, NewInfinite(FatalPoison, 1))
	if len(l.hurts) != 1 || l.hurts[0].damage != 1 {
		t.Fatalf("fatal poison did not hurt an entity at 1 health: %v", l.hurts)
	}
}

func TestPoisonThresholdIsStrictlyGreaterThanOne(t *testing.T) {
	// PINNED CONSTRAINT: the guard is Health() > 1, not >= 1, so an entity on
	// exactly one health is spared and one on 1.5 health is taken to 0.5. Poison
	// cannot kill, but it can leave an entity below one health.
	l := newTestLiving()
	l.health = 1.5
	Poison.Apply(l, NewInfinite(Poison, 1))
	if want := 0.5; l.health != want {
		t.Fatalf("health = %v, want %v", l.health, want)
	}
}

func TestIntervalEffectAmountIsAlwaysOne(t *testing.T) {
	// Every interval effect deals or heals exactly one point regardless of
	// level. The level only changes how often it happens, which is why the
	// interval table above is the whole of their behaviour.
	for _, e := range []LastingType{Poison, FatalPoison, Wither} {
		l := newTestLiving()
		e.Apply(l, NewInfinite(e, 5))
		if len(l.hurts) != 1 || l.hurts[0].damage != 1 {
			t.Fatalf("%T dealt %v, want a single point of damage", e, l.hurts)
		}
	}
	l := newTestLiving()
	Regeneration.Apply(l, NewInfinite(Regeneration, 5))
	if got, want := l.heals, []float64{1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("regeneration healed %v, want %v", got, want)
	}
}

func TestHungerExhaustion(t *testing.T) {
	// Hunger exhausts 0.005 points per level every tick it is applied, with no
	// interval and no clamp.
	cases := []struct {
		lvl  int
		want float64
	}{
		{lvl: 0, want: 0},
		{lvl: 1, want: 0.005},
		{lvl: 2, want: 0.01},
		{lvl: 3, want: 0.015},
		{lvl: 10, want: 0.05},
		{lvl: -1, want: -0.005},
	}
	for _, c := range cases {
		l := newTestLiving()
		Hunger.Apply(l, NewInfinite(Hunger, c.lvl))
		if l.exhaustion != c.want {
			t.Fatalf("level %d: exhaustion = %v, want %v", c.lvl, l.exhaustion, c.want)
		}
	}
}

func TestSaturationAmounts(t *testing.T) {
	// Saturation restores the level in food points and twice the level in
	// saturation, every tick it is applied.
	cases := []struct {
		lvl        int
		food       int
		saturation float64
	}{
		{lvl: 0, food: 0, saturation: 0},
		{lvl: 1, food: 1, saturation: 2},
		{lvl: 2, food: 2, saturation: 4},
		{lvl: 5, food: 5, saturation: 10},
		{lvl: -1, food: -1, saturation: -2},
	}
	for _, c := range cases {
		l := newTestLiving()
		Saturation.Apply(l, NewInfinite(Saturation, c.lvl))
		if l.food != c.food || l.saturation != c.saturation {
			t.Fatalf("level %d: food = %d, saturation = %v, want %d, %v", c.lvl, l.food, l.saturation, c.food, c.saturation)
		}
	}
}

func TestEveryEffectColour(t *testing.T) {
	// The colour of every registered effect, by ID. These reach the client as
	// the potion colour on an entity, and ResultingColour averages them, so a
	// single changed byte changes what every mixed effect looks like.
	want := map[int]struct{ r, g, b, a uint8 }{
		1:  {0x33, 0xeb, 0xff, 0xff},
		2:  {0x8b, 0xaf, 0xe0, 0xff},
		3:  {0xd9, 0xc0, 0x43, 0xff},
		4:  {0x4a, 0x42, 0x17, 0xff},
		5:  {0xff, 0xc7, 0x00, 0xff},
		6:  {0xf8, 0x24, 0x23, 0xff},
		7:  {0xa9, 0x65, 0x6a, 0xff},
		8:  {0xfd, 0xff, 0x84, 0xff},
		9:  {0x55, 0x1d, 0x4a, 0xff},
		10: {0xcd, 0x5c, 0xab, 0xff},
		11: {0x91, 0x46, 0xf0, 0xff},
		12: {0xff, 0x99, 0x00, 0xff},
		13: {0x98, 0xda, 0xc0, 0xff},
		14: {0xf6, 0xf6, 0xf6, 0xff},
		15: {0x1f, 0x1f, 0x23, 0xff},
		16: {0xc2, 0xff, 0x66, 0xff},
		17: {0x58, 0x76, 0x53, 0xff},
		18: {0x48, 0x4d, 0x48, 0xff},
		19: {0x87, 0xa3, 0x63, 0xff},
		20: {0x73, 0x61, 0x56, 0xff},
		21: {0xf8, 0x7d, 0x23, 0xff},
		22: {0x25, 0x52, 0xa5, 0xff},
		23: {0xf8, 0x24, 0x23, 0xff},
		24: {0xce, 0xff, 0xff, 0xff},
		25: {0x4e, 0x93, 0x31, 0xff},
		26: {0x1d, 0xc2, 0xd1, 0xff},
		27: {0xf3, 0xcf, 0xb9, 0xff},
		30: {0x29, 0x27, 0x21, 0xff},
	}
	for id, c := range want {
		e, ok := ByID(id)
		if !ok {
			t.Fatalf("effect %d is not registered", id)
		}
		got := e.RGBA()
		if got.R != c.r || got.G != c.g || got.B != c.b || got.A != c.a {
			t.Fatalf("effect %d (%T): RGBA() = %#v, want {0x%02x, 0x%02x, 0x%02x, 0x%02x}", id, e, got, c.r, c.g, c.b, c.a)
		}
	}
	// PINNED CONSTRAINT: instant health and saturation share a colour. ID lookup
	// still round-trips because effectIds is keyed by the effect value, not by
	// its colour, but any code identifying an effect by colour cannot tell them
	// apart.
	if InstantHealth.RGBA() != Saturation.RGBA() {
		t.Fatal("instant health and saturation no longer share a colour")
	}
}

func TestEffectsDoNotDependOnTheDuration(t *testing.T) {
	// Apply reads only the level and the tick counter, never the duration, so an
	// effect with a duration of zero still acts. Expiry is entirely the caller's
	// concern, which is why an effect ticked past its end keeps working.
	l := newTestLiving()
	Wither.Apply(l, New(Wither, 1, 0))
	if len(l.hurts) != 1 {
		t.Fatalf("hurts = %v, want one entry for an effect with no duration left", l.hurts)
	}

	l = newTestLiving()
	Wither.Apply(l, New(Wither, 1, -time.Hour))
	if len(l.hurts) != 1 {
		t.Fatalf("hurts = %v, want one entry for an effect with a negative duration", l.hurts)
	}
}

func TestNoEffectProducesANonFiniteValue(t *testing.T) {
	// A sweep over every numeric method in the package at every level a potion
	// or a command could realistically carry. Nothing here should be able to
	// produce a NaN or an infinity: an effect multiplier that does would corrupt
	// an entity's speed or damage permanently, since the values are stored back
	// onto the entity.
	for lvl := -8; lvl <= 32; lvl++ {
		values := []float64{
			Haste.Multiplier(lvl),
			ConduitPower.Multiplier(lvl),
			MiningFatigue.Multiplier(lvl),
			Strength.Multiplier(lvl),
			Weakness.Multiplier(lvl),
			Resistance.Multiplier(PoisonDamageSource{}, lvl),
		}
		for i, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("level %d: multiplier %d = %v", lvl, i, v)
			}
		}
	}
	// PINNED CONSTRAINT: mining fatigue is math.Pow(3, lvl) with no cap, so it
	// does overflow to infinity, just above any level reachable in practice.
	if got := MiningFatigue.Multiplier(1000); !math.IsInf(got, 1) {
		t.Fatalf("MiningFatigue.Multiplier(1000) = %v, want +Inf", got)
	}
}

var _ world.DamageSource = unreducedSource{}
