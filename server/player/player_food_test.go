package player

import (
	"testing"

	"github.com/jv2w/Impulse/server/world"
)

// foodLossHandler records and optionally rewrites or cancels a food loss, which
// is the only way the event half of Player.Exhaust becomes observable without a
// session to watch the outgoing packets.
type foodLossHandler struct {
	NopHandler
	calls    int
	from, to int
	rewrite  *int
	cancel   bool
}

func (h *foodLossHandler) HandleFoodLoss(ctx *Context, from int, to *int) {
	h.calls, h.from, h.to = h.calls+1, from, *to
	if h.rewrite != nil {
		*to = *h.rewrite
	}
	if h.cancel {
		ctx.Cancel()
	}
}

// foodState is the whole of a player's hunger and health, sampled once per tick.
type foodState struct {
	health     float64
	food       int
	saturation float64
	exhaustion float64
}

func sampleFood(p *Player) foodState {
	return foodState{
		health:     p.Health(),
		food:       p.hunger.foodLevel,
		saturation: p.hunger.saturationLevel,
		exhaustion: p.hunger.exhaustionLevel,
	}
}

func (s foodState) equal(o foodState) bool {
	return s.health == o.health && s.food == o.food &&
		closeTo(s.saturation, o.saturation) && closeTo(s.exhaustion, o.exhaustion)
}

// Only the two game modes that take damage lose food, and the stub transaction
// reports DifficultyNormal, which does not regenerate food on its own.
func TestExhaustOnlyAppliesToTheModesThatTakeDamage(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        world.GameMode
		wantSat     float64
		wantExhaust float64
	}{
		{name: "survival", mode: world.GameModeSurvival, wantSat: 4, wantExhaust: 1},
		{name: "adventure", mode: world.GameModeAdventure, wantSat: 4, wantExhaust: 1},
		{name: "creative", mode: world.GameModeCreative, wantSat: 5, wantExhaust: 0},
		{name: "spectator", mode: world.GameModeSpectator, wantSat: 5, wantExhaust: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{GameMode: tc.mode})

			p.Exhaust(5)

			if got := p.hunger.saturationLevel; !closeTo(got, tc.wantSat) {
				t.Fatalf("saturation = %v, want %v", got, tc.wantSat)
			}
			if got := p.hunger.exhaustionLevel; !closeTo(got, tc.wantExhaust) {
				t.Fatalf("exhaustion = %v, want %v", got, tc.wantExhaust)
			}
		})
	}
}

// The event only fires when the food level itself moved. Spending saturation is
// invisible to the handler.
func TestExhaustFiresTheEventOnlyWhenFoodMoves(t *testing.T) {
	h := &foodLossHandler{}
	p := newStubPlayer(Config{})
	p.Handle(h)

	// Five saturation points absorb the first five crossings of 4.
	p.Exhaust(20)
	if h.calls != 0 {
		t.Fatalf("HandleFoodLoss calls = %v, want 0: saturation loss raised the event", h.calls)
	}

	p.Exhaust(4)
	if h.calls != 1 {
		t.Fatalf("HandleFoodLoss calls = %v, want 1", h.calls)
	}
	if h.from != 20 || h.to != 19 {
		t.Fatalf("HandleFoodLoss(from %v, to %v), want (20, 19)", h.from, h.to)
	}
	if got := p.Food(); got != 19 {
		t.Fatalf("food = %v, want 19", got)
	}
}

// The handler is handed a pointer to the resulting level, and whatever it
// writes is what the player ends up with. A handler can raise the level as well
// as lower it.
func TestExhaustAppliesTheLevelTheHandlerWritesBack(t *testing.T) {
	for _, want := range []int{0, 5, 19, 20, 30} {
		rewrite := want
		h := &foodLossHandler{rewrite: &rewrite}
		p := newStubPlayer(Config{Saturation: 0, Food: 20, FoodTick: 1})
		p.Handle(h)

		p.Exhaust(4)

		// SetFood clamps, so a level above 20 comes back as 20.
		if got, clamped := p.Food(), min(want, 20); got != clamped {
			t.Fatalf("handler wrote %v, food = %v, want %v", want, got, clamped)
		}
	}
}

// PINNED BUG: cancelling HandleFoodLoss runs hungerManager.resetExhaustion,
// which clears the saturation as well as the exhaustion (hunger.go:61). A
// handler that only meant to keep the food bar where it was destroys the
// buffer that would have absorbed the next few crossings, so the food point it
// saved is lost sooner rather than later. Pinned as-is; decide in stage 1.
func TestCancellingFoodLossWipesTheSaturationBuffer(t *testing.T) {
	h := &foodLossHandler{cancel: true}
	p := newStubPlayer(Config{})
	p.Handle(h)

	// Drain the saturation to 1, then top it back up so that the cancelling
	// exhaust starts from a buffer worth four crossings.
	p.Exhaust(20)
	p.Saturate(0, 4)
	if got := p.hunger.saturationLevel; !closeTo(got, 4) {
		t.Fatalf("setup saturation = %v, want 4", got)
	}

	// 20 more exhaustion takes the four saturation points and then the food
	// point that raises the event.
	p.Exhaust(20)

	if h.calls != 1 {
		t.Fatalf("HandleFoodLoss calls = %v, want 1", h.calls)
	}
	if got := p.Food(); got != 20 {
		t.Fatalf("food = %v, want 20: the cancel did not hold the level", got)
	}
	if got := p.hunger.saturationLevel; got != 0 {
		t.Fatalf("saturation = %v, want 0", got)
	}
	if got := p.hunger.foodTick; got != 1 {
		t.Fatalf("foodTick = %v, want 1: the cancel also rewound the food cycle", got)
	}
}

// PINNED CONSTRAINT: the last branch of Exhaust, the forced StopSprinting when
// the level crosses from 7 or above to 6 or below (player.go:875), cannot be
// reached. StopSprinting returns immediately unless p.sprinting is set, and a
// sprinting player takes it into updateState and out through the world.
func TestExhaustCannotReachTheSprintStopBranch(t *testing.T) {
	p := newStubPlayer(Config{Food: 7, FoodTick: 1, Saturation: 0})
	p.sprinting = true

	mustPanic(t, "Exhaust across the sprint threshold", func() { p.Exhaust(4) })
}

// The three hunger passthroughs on Player do nothing but delegate and then push
// the result to a session that is not there.
func TestFoodPassthroughsDelegateToTheHungerManager(t *testing.T) {
	p := newStubPlayer(Config{})

	p.SetFood(3)
	if got := p.Food(); got != 3 {
		t.Fatalf("Food() = %v, want 3", got)
	}
	p.AddFood(5)
	if got := p.Food(); got != 8 {
		t.Fatalf("Food() = %v, want 8", got)
	}
	p.AddFood(100)
	if got := p.Food(); got != 20 {
		t.Fatalf("Food() = %v, want 20", got)
	}

	p.SetFood(4)
	p.Saturate(2, 10)
	if got := p.Food(); got != 6 {
		t.Fatalf("Food() = %v, want 6", got)
	}
	if got := p.hunger.saturationLevel; !closeTo(got, 6) {
		t.Fatalf("saturation = %v, want 6: the cap did not follow the new food level", got)
	}
}

// goldenFoodTicks is the state after every tickFood call where it differs from
// the call before, over one full 80-tick cycle plus the first tick of the next.
// The player starts at 15 health on a full bar, so quick regeneration runs the
// whole way and pays 6 exhaustion for each half heart it restores.
//
// The cadence is what this pins: a heal at foodTick 1 and then at every
// multiple of 20, nothing in between, and the 80-tick wrap that brings foodTick
// back to 1 on the 81st call. Changing the reset threshold, either modulus or
// the 6 exhaustion per heal moves at least one row.
var goldenFoodTicks = []struct {
	call  int
	state foodState
}{
	{call: 1, state: foodState{health: 16, food: 20, saturation: 4, exhaustion: 2}},
	{call: 20, state: foodState{health: 17, food: 20, saturation: 2, exhaustion: 0}},
	{call: 40, state: foodState{health: 18, food: 20, saturation: 1, exhaustion: 2}},
	{call: 60, state: foodState{health: 19, food: 19, saturation: 0, exhaustion: 0}},
	{call: 81, state: foodState{health: 20, food: 18, saturation: 0, exhaustion: 2}},
}

func TestTickFoodGoldenCycle(t *testing.T) {
	p := newStubPlayer(Config{Health: 15, MaxHealth: 20, Food: 20, Saturation: 5, FoodTick: 1})

	prev := sampleFood(p)
	var changes []struct {
		call  int
		state foodState
	}
	for call := 1; call <= 81; call++ {
		p.tickFood()
		if now := sampleFood(p); !now.equal(prev) {
			changes = append(changes, struct {
				call  int
				state foodState
			}{call, now})
			prev = now
		}
	}

	if len(changes) != len(goldenFoodTicks) {
		t.Fatalf("state changed on %v ticks, want %v: got %+v", len(changes), len(goldenFoodTicks), changes)
	}
	for i, want := range goldenFoodTicks {
		if changes[i].call != want.call || !changes[i].state.equal(want.state) {
			t.Fatalf("change %v: call %v state %+v, want call %v state %+v",
				i, changes[i].call, changes[i].state, want.call, want.state)
		}
	}
	if got := p.hunger.foodTick; got != 2 {
		t.Fatalf("foodTick after 81 calls = %v, want 2: the cycle did not wrap at 80", got)
	}
}

// A player already at full health never heals, so the whole quick regeneration
// branch is a no-op and no exhaustion is spent at all.
func TestTickFoodSpendsNothingAtFullHealth(t *testing.T) {
	p := newStubPlayer(Config{Health: 20, MaxHealth: 20, Food: 20, Saturation: 5, FoodTick: 1})

	before := sampleFood(p)
	for range 81 {
		p.tickFood()
	}

	if now := sampleFood(p); !now.equal(before) {
		t.Fatalf("state = %+v, want %+v unchanged", now, before)
	}
}

// starve is gated on the difficulty's starvation health limit, which is 2 on
// the Normal difficulty the stub reports. A player at or below it takes no
// damage, which is the only arm of starve that does not go through Hurt and out
// to the world.
func TestStarvationIsWithheldAtTheDifficultyHealthLimit(t *testing.T) {
	p := newStubPlayer(Config{Health: 2, MaxHealth: 20, Food: 0, FoodTick: 1, Saturation: 0})

	p.tickFood()

	if got := p.Health(); got != 2 {
		t.Fatalf("health = %v, want 2: starvation damage was dealt below the limit", got)
	}
	if got := p.hunger.foodTick; got != 2 {
		t.Fatalf("foodTick = %v, want 2", got)
	}
}

// PINNED CONSTRAINT: above the limit starve calls Hurt, which broadcasts a hurt
// action to the player's viewers and cannot run without a world.
func TestStarvationAboveTheLimitCannotBeReached(t *testing.T) {
	p := newStubPlayer(Config{Health: 20, MaxHealth: 20, Food: 0, FoodTick: 1, Saturation: 0})

	mustPanic(t, "tickFood while starving at full health", func() { p.tickFood() })
}

// Eating is Saturate, and its cap is the food level after the food has been
// added, so a starving player keeps everything the food carries.
func TestSaturateOnAPlayerFollowsTheNewFoodLevel(t *testing.T) {
	p := newStubPlayer(Config{Food: 1, FoodTick: 1, Saturation: 0})

	// item.Apple restores 4 food and 2.4 saturation.
	p.Saturate(4, 2.4)

	if got := p.Food(); got != 5 {
		t.Fatalf("food = %v, want 5", got)
	}
	if got := p.hunger.saturationLevel; !closeTo(got, 2.4) {
		t.Fatalf("saturation = %v, want 2.4", got)
	}
}
