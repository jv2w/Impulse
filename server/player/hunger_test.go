package player

import (
	"math"
	"sync"
	"testing"
)

// hungerEpsilon is the absolute tolerance used when comparing saturation and
// exhaustion levels. Every golden sequence below steps exhaustion by 0.5, which
// is exact in binary and stays exact under the repeated addition and the -= 4
// in exhaust, so no drift is expected at all; the bound only exists so that a
// future change of the step size does not turn every comparison into a literal
// bit match. 1e-9 is the same absolute bound the entity physics goldens use.
const hungerEpsilon = 1e-9

func closeTo(got, want float64) bool {
	return math.Abs(got-want) <= hungerEpsilon
}

// checkHunger compares the three continuous fields of a manager at once. The
// fields are read without the mutex on purpose: every caller here is
// single-goroutine, and taking the lock would hide a deadlock introduced by a
// future change to the locked methods.
func checkHunger(t *testing.T, m *hungerManager, food int, saturation, exhaustion float64) {
	t.Helper()
	if m.foodLevel != food || !closeTo(m.saturationLevel, saturation) || !closeTo(m.exhaustionLevel, exhaustion) {
		t.Fatalf("hunger = (food %v, saturation %v, exhaustion %v), want (food %v, saturation %v, exhaustion %v)",
			m.foodLevel, m.saturationLevel, m.exhaustionLevel, food, saturation, exhaustion)
	}
}

func TestNewHungerManagerStartsFullWithFiveSaturation(t *testing.T) {
	m := newHungerManager()
	checkHunger(t, m, 20, 5, 0)
	if m.foodTick != 1 {
		t.Fatalf("foodTick = %v, want 1", m.foodTick)
	}
}

func TestSetFoodClampsToTheZeroToTwentyRange(t *testing.T) {
	for _, tc := range []struct {
		set, want int
	}{
		{set: -1000, want: 0},
		{set: -1, want: 0},
		{set: 0, want: 0},
		{set: 7, want: 7},
		{set: 20, want: 20},
		{set: 21, want: 20},
		{set: math.MaxInt, want: 20},
	} {
		m := newHungerManager()
		m.SetFood(tc.set)
		if got := m.Food(); got != tc.want {
			t.Fatalf("SetFood(%v) then Food() = %v, want %v", tc.set, got, tc.want)
		}
	}
}

// SetFood and AddFood only clamp the food level. Saturation is left where it
// was, so a manager can hold more saturation than food, which saturate itself
// would never produce.
func TestSetFoodLeavesSaturationAboveTheNewFoodLevel(t *testing.T) {
	m := newHungerManager()
	m.SetFood(1)
	checkHunger(t, m, 1, 5, 0)
}

func TestAddFoodClampsAtBothEnds(t *testing.T) {
	for _, tc := range []struct {
		start, add, want int
	}{
		{start: 20, add: 1, want: 20},
		{start: 20, add: math.MaxInt32, want: 20},
		{start: 10, add: 5, want: 15},
		{start: 10, add: -5, want: 5},
		{start: 3, add: -10, want: 0},
		{start: 0, add: -1, want: 0},
	} {
		m := newHungerManager()
		m.SetFood(tc.start)
		m.AddFood(tc.add)
		if got := m.Food(); got != tc.want {
			t.Fatalf("food %v then AddFood(%v) = %v, want %v", tc.start, tc.add, got, tc.want)
		}
	}
}

// saturate adds the food first and only then clamps the saturation, so the cap
// is the level after eating, not before it. Eating from an almost empty bar
// therefore keeps all of the saturation the food carries.
func TestSaturateCapsSaturationAtTheFoodLevelAfterEating(t *testing.T) {
	for _, tc := range []struct {
		name           string
		food           int
		saturation     float64
		addFood        int
		addSaturation  float64
		wantFood       int
		wantSaturation float64
	}{
		{
			name: "apple on a full bar keeps the cap at 20",
			food: 20, saturation: 5, addFood: 4, addSaturation: 2.4,
			wantFood: 20, wantSaturation: 7.4,
		},
		{
			name: "apple on an empty bar is capped by the level after eating",
			food: 0, saturation: 0, addFood: 4, addSaturation: 2.4,
			wantFood: 4, wantSaturation: 2.4,
		},
		{
			name: "one food point caps the saturation at one",
			food: 0, saturation: 0, addFood: 1, addSaturation: 6,
			wantFood: 1, wantSaturation: 1,
		},
		{
			name: "food overflow is clamped before the saturation cap applies",
			food: 18, saturation: 0, addFood: 10, addSaturation: 100,
			wantFood: 20, wantSaturation: 20,
		},
		{
			name: "negative food wipes the saturation with it",
			food: 20, saturation: 12, addFood: -20, addSaturation: 3,
			wantFood: 0, wantSaturation: 0,
		},
		{
			name: "negative saturation is clamped at zero",
			food: 20, saturation: 2, addFood: 0, addSaturation: -10,
			wantFood: 20, wantSaturation: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHungerManager()
			m.foodLevel, m.saturationLevel = tc.food, tc.saturation
			m.saturate(tc.addFood, tc.addSaturation)
			checkHunger(t, m, tc.wantFood, tc.wantSaturation, 0)
		})
	}
}

// desaturate spends saturation before food, and does nothing at all once both
// are gone. The order is the whole point of the saturation bar: swapping the
// two branches would make food drain while saturation still sits untouched.
func TestDesaturateSpendsSaturationBeforeFood(t *testing.T) {
	for _, tc := range []struct {
		name           string
		food           int
		saturation     float64
		wantFood       int
		wantSaturation float64
	}{
		{name: "saturation first", food: 20, saturation: 5, wantFood: 20, wantSaturation: 4},
		{name: "fractional saturation is floored at zero", food: 20, saturation: 0.4, wantFood: 20, wantSaturation: 0},
		{name: "food once saturation is gone", food: 20, saturation: 0, wantFood: 19, wantSaturation: 0},
		{name: "food at one", food: 1, saturation: 0, wantFood: 0, wantSaturation: 0},
		{name: "nothing left to spend", food: 0, saturation: 0, wantFood: 0, wantSaturation: 0},
		{name: "saturation is spent even with an empty bar", food: 0, saturation: 3, wantFood: 0, wantSaturation: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newHungerManager()
			m.foodLevel, m.saturationLevel = tc.food, tc.saturation
			m.desaturate()
			checkHunger(t, m, tc.wantFood, tc.wantSaturation, 0)
		})
	}
}

// The three golden arrays below hold the state of a fresh hungerManager after
// each of 56 consecutive exhaust(0.5) calls, laid out eight values per line so
// that one line is one full exhaustion cycle. 0.5 is exact in binary, so eight
// calls reach exactly 4 and cost exactly one point with no rounding slack.
//
// A change to either constant in exhaust moves the whole table: raising the
// threshold from 4 stretches the cycle, and changing the -= 4 leaves a
// remainder behind. A swap of the two branches in desaturate moves the drain
// from the saturation array to the food array on the very first cycle.
var (
	goldenExhaustFood = [56]int{
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 19,
		19, 19, 19, 19, 19, 19, 19, 18,
	}
	goldenExhaustSaturation = [56]float64{
		5, 5, 5, 5, 5, 5, 5, 4,
		4, 4, 4, 4, 4, 4, 4, 3,
		3, 3, 3, 3, 3, 3, 3, 2,
		2, 2, 2, 2, 2, 2, 2, 1,
		1, 1, 1, 1, 1, 1, 1, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
	goldenExhaustExhaustion = [56]float64{
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
		0.5, 1, 1.5, 2, 2.5, 3, 3.5, 0,
	}
)

func TestExhaustGoldenSequence(t *testing.T) {
	m := newHungerManager()
	for i := range goldenExhaustFood {
		m.exhaust(0.5)
		if m.foodLevel != goldenExhaustFood[i] ||
			!closeTo(m.saturationLevel, goldenExhaustSaturation[i]) ||
			!closeTo(m.exhaustionLevel, goldenExhaustExhaustion[i]) {
			t.Fatalf("after %v exhaust(0.5) calls: (food %v, saturation %v, exhaustion %v), want (food %v, saturation %v, exhaustion %v)",
				i+1, m.foodLevel, m.saturationLevel, m.exhaustionLevel,
				goldenExhaustFood[i], goldenExhaustSaturation[i], goldenExhaustExhaustion[i])
		}
	}
}

// The default five saturation points buy six full exhaustion cycles before the
// food bar itself moves: five to spend the saturation and one to take the first
// food point. That is 24 exhaustion in total.
func TestFirstFoodPointCostsTwentyFourExhaustion(t *testing.T) {
	m := newHungerManager()
	for range 23 {
		m.exhaust(1)
	}
	checkHunger(t, m, 20, 0, 3)

	m.exhaust(1)
	checkHunger(t, m, 19, 0, 0)
}

// exhaust keeps the leftover below 4 across calls, so the cost of a food point
// does not depend on how the exhaustion was split up.
func TestExhaustCarriesTheRemainderAcrossCalls(t *testing.T) {
	m := newHungerManager()
	m.saturationLevel = 0

	m.exhaust(3.5)
	checkHunger(t, m, 20, 0, 3.5)

	m.exhaust(0.5)
	checkHunger(t, m, 19, 0, 0)

	m.exhaust(0.25)
	m.exhaust(3.5)
	checkHunger(t, m, 19, 0, 3.75)

	m.exhaust(0.25)
	checkHunger(t, m, 18, 0, 0)
}

// A single large exhaust drains everything the manager has in one call and then
// spins out the rest against an empty bar.
func TestOneLargeExhaustDrainsTheWholeBar(t *testing.T) {
	m := newHungerManager()
	m.exhaust(400)
	checkHunger(t, m, 0, 0, 0)

	// 100 desaturations were available, 25 of them did anything.
	m.exhaust(4)
	checkHunger(t, m, 0, 0, 0)
}

func TestExhaustBelowFourNeverTouchesTheBar(t *testing.T) {
	m := newHungerManager()
	m.exhaust(3.999)
	checkHunger(t, m, 20, 5, 3.999)
}

// PINNED BUG: exhaust never clamps the exhaustion level at 0, so a negative
// value from a caller is stored and permanently offsets every later drain.
// player.Exhaust passes its argument straight through, so any handler or block
// that exhausts by a negative amount buys the player free food.
func TestNegativeExhaustPermanentlyOffsetsTheDrain(t *testing.T) {
	m := newHungerManager()
	m.exhaust(-100)
	checkHunger(t, m, 20, 5, -100)

	// 24 exhaustion would normally have cost a food point, see
	// TestFirstFoodPointCostsTwentyFourExhaustion.
	for range 24 {
		m.exhaust(1)
	}
	checkHunger(t, m, 20, 5, -76)
}

// PINNED BUG: NaN is neither < 4 nor >= 4, so the loop in exhaust never runs
// and the NaN is stored. Every later exhaust adds to NaN and stays NaN, so one
// NaN from a caller disables the hunger bar for the rest of the player's life.
//
// The +Inf case is worse and is deliberately not exercised here: +Inf >= 4 is
// true and +Inf - 4 is still +Inf, so exhaust spins forever while holding m.mu.
// A test for it could not terminate.
func TestNaNExhaustDisablesTheHungerBarForever(t *testing.T) {
	m := newHungerManager()
	m.exhaust(math.NaN())
	if !math.IsNaN(m.exhaustionLevel) {
		t.Fatalf("exhaustion = %v, want NaN", m.exhaustionLevel)
	}
	checkHunger := func(food int, saturation float64) {
		t.Helper()
		if m.foodLevel != food || !closeTo(m.saturationLevel, saturation) {
			t.Fatalf("hunger = (food %v, saturation %v), want (food %v, saturation %v)",
				m.foodLevel, m.saturationLevel, food, saturation)
		}
	}
	checkHunger(20, 5)

	m.exhaust(1e9)
	if !math.IsNaN(m.exhaustionLevel) {
		t.Fatalf("exhaustion after a large exhaust = %v, want NaN", m.exhaustionLevel)
	}
	checkHunger(20, 5)
}

// PINNED BUG: resetExhaustion is named and documented as if it only cleared the
// exhaustion level, but it also wipes the saturation and rewinds the food tick.
// player.Exhaust calls it when a handler cancels HandleFoodLoss, so cancelling
// the event costs the player every saturation point they had.
func TestResetExhaustionAlsoClearsSaturationAndFoodTick(t *testing.T) {
	m := newHungerManager()
	m.exhaustionLevel, m.foodTick = 3.5, 47

	m.resetExhaustion()

	checkHunger(t, m, 20, 0, 0)
	if m.foodTick != 1 {
		t.Fatalf("foodTick = %v, want 1", m.foodTick)
	}
}

func TestResetRestoresEveryDefault(t *testing.T) {
	m := newHungerManager()
	m.foodLevel, m.saturationLevel, m.exhaustionLevel, m.foodTick = 3, 1.25, 2.5, 61

	m.Reset()

	checkHunger(t, m, 20, 5, 0)
	if m.foodTick != 1 {
		t.Fatalf("foodTick = %v, want 1", m.foodTick)
	}
}

// The four predicates read off nothing but the food and saturation levels, and
// their thresholds are what gate regeneration, sprinting and starvation damage
// in Player.tickFood.
func TestHungerPredicateThresholds(t *testing.T) {
	for _, tc := range []struct {
		food                              int
		saturation                        float64
		quickRegen, regen, sprint, starve bool
	}{
		{food: 20, saturation: 5, quickRegen: true, regen: true, sprint: true},
		{food: 20, saturation: 0.0001, quickRegen: true, regen: true, sprint: true},
		{food: 20, saturation: 0, quickRegen: false, regen: true, sprint: true},
		{food: 19, saturation: 5, quickRegen: false, regen: true, sprint: true},
		{food: 18, saturation: 0, quickRegen: false, regen: true, sprint: true},
		{food: 17, saturation: 0, quickRegen: false, regen: false, sprint: true},
		{food: 7, saturation: 0, quickRegen: false, regen: false, sprint: true},
		{food: 6, saturation: 0, quickRegen: false, regen: false, sprint: false},
		{food: 1, saturation: 0, quickRegen: false, regen: false, sprint: false},
		{food: 0, saturation: 0, quickRegen: false, regen: false, sprint: false, starve: true},
	} {
		m := newHungerManager()
		m.foodLevel, m.saturationLevel = tc.food, tc.saturation
		if got := m.canQuicklyRegenerate(); got != tc.quickRegen {
			t.Fatalf("food %v saturation %v: canQuicklyRegenerate() = %v, want %v", tc.food, tc.saturation, got, tc.quickRegen)
		}
		if got := m.canRegenerate(); got != tc.regen {
			t.Fatalf("food %v: canRegenerate() = %v, want %v", tc.food, got, tc.regen)
		}
		if got := m.canSprint(); got != tc.sprint {
			t.Fatalf("food %v: canSprint() = %v, want %v", tc.food, got, tc.sprint)
		}
		if got := m.starving(); got != tc.starve {
			t.Fatalf("food %v: starving() = %v, want %v", tc.food, got, tc.starve)
		}
	}
}

// PINNED BUG: only the exported methods and exhaust/saturate take m.mu.
// Player.sendFood (player.go:818) and Player.tickFood (player.go:2805) read
// foodLevel, saturationLevel, exhaustionLevel and foodTick with no lock at all,
// and Config.Apply (conf.go:114) writes all four the same way. This test
// exercises only the locked methods, so it stays clean under -race; the
// unlocked readers are a real race that is pinned by description, not by a
// test that would have to fail to prove it.
func TestHungerManagerLockedMethodsAreConcurrencySafe(t *testing.T) {
	m := newHungerManager()

	const goroutines, iterations = 8, 500
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				switch g % 4 {
				case 0:
					m.exhaust(0.5)
				case 1:
					m.saturate(1, 0.5)
				case 2:
					m.AddFood(-1)
				case 3:
					_ = m.Food()
					_ = m.canQuicklyRegenerate()
					_ = m.canSprint()
				}
			}
		}()
	}
	wg.Wait()

	if m.foodLevel < 0 || m.foodLevel > 20 {
		t.Fatalf("food = %v, want within 0-20", m.foodLevel)
	}
	if m.saturationLevel < 0 {
		t.Fatalf("saturation = %v, want at least 0", m.saturationLevel)
	}
}

// StarvationDamageSource is the only world.DamageSource this package defines.
// Armour and resistance must not reduce starvation damage, and a totem must
// still save the player from it.
func TestStarvationDamageSourceIsUnreducible(t *testing.T) {
	var s StarvationDamageSource
	if s.ReducedByArmour() || s.ReducedByResistance() || s.Fire() || s.IgnoreTotem() {
		t.Fatalf("StarvationDamageSource = (armour %v, resistance %v, fire %v, ignoreTotem %v), want all false",
			s.ReducedByArmour(), s.ReducedByResistance(), s.Fire(), s.IgnoreTotem())
	}
}
