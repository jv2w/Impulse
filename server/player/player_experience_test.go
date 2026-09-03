package player

import (
	"math"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
	"github.com/jv2w/Impulse/server/world"
)

// xpHandler cancels or rewrites an experience gain, the only two things a
// Handler can do to one.
type xpHandler struct {
	NopHandler
	calls   int
	seen    int
	rewrite *int
	cancel  bool
}

func (h *xpHandler) HandleExperienceGain(ctx *Context, amount *int) {
	h.calls, h.seen = h.calls+1, *amount
	if h.rewrite != nil {
		*amount = *h.rewrite
	}
	if h.cancel {
		ctx.Cancel()
	}
}

// mendingPickaxe returns a wooden pickaxe with Mending and the durability
// asked for. The wooden tier has 59 maximum durability.
func mendingPickaxe(durability int) item.Stack {
	return woodPickaxe().
		WithEnchantments(item.NewEnchantment(enchantment.Mending, 1)).
		WithDurability(durability)
}

// A fresh player has no experience at all, and the three readers agree on it.
func TestNewPlayerHasNoExperience(t *testing.T) {
	p := newStubPlayer(Config{})
	if got := p.Experience(); got != 0 {
		t.Fatalf("Experience() = %v, want 0", got)
	}
	if got := p.ExperienceLevel(); got != 0 {
		t.Fatalf("ExperienceLevel() = %v, want 0", got)
	}
	if got := p.ExperienceProgress(); got != 0 {
		t.Fatalf("ExperienceProgress() = %v, want 0", got)
	}
}

// The first two levels sit on exact experience totals, so they can be pinned
// without any tolerance: 7 points is level 1 and 16 is level 2.
func TestAddExperienceReachesTheFirstTwoLevelsExactly(t *testing.T) {
	p := newStubPlayer(Config{})

	if got := p.AddExperience(7); got != 7 {
		t.Fatalf("AddExperience(7) = %v, want 7", got)
	}
	if got := p.ExperienceLevel(); got != 1 {
		t.Fatalf("level after 7 experience = %v, want 1", got)
	}
	if got := p.ExperienceProgress(); got != 0 {
		t.Fatalf("progress at exactly level 1 = %v, want 0", got)
	}

	p.AddExperience(9)
	if got := p.Experience(); got != 16 {
		t.Fatalf("Experience() = %v, want 16", got)
	}
	if got := p.ExperienceLevel(); got != 2 {
		t.Fatalf("level after 16 experience = %v, want 2", got)
	}
}

func TestAddExperienceHonoursTheHandler(t *testing.T) {
	rewrite := 16
	for _, tc := range []struct {
		name      string
		h         *xpHandler
		want      int
		wantLevel int
	}{
		{name: "cancelled", h: &xpHandler{cancel: true}, want: 0, wantLevel: 0},
		{name: "rewritten", h: &xpHandler{rewrite: &rewrite}, want: 16, wantLevel: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{})
			p.Handle(tc.h)

			got := p.AddExperience(7)

			if tc.h.calls != 1 || tc.h.seen != 7 {
				t.Fatalf("handler calls = %v seen = %v, want 1 and 7", tc.h.calls, tc.h.seen)
			}
			if got != tc.want {
				t.Fatalf("AddExperience = %v, want %v", got, tc.want)
			}
			if lvl := p.ExperienceLevel(); lvl != tc.wantLevel {
				t.Fatalf("level = %v, want %v", lvl, tc.wantLevel)
			}
		})
	}
}

// Removing more than the player has resets to zero rather than going negative.
func TestRemoveExperienceFloorsAtZero(t *testing.T) {
	p := newStubPlayer(Config{Experience: 16})

	p.RemoveExperience(5)
	if got := p.Experience(); got != 11 {
		t.Fatalf("Experience() = %v, want 11", got)
	}

	p.RemoveExperience(1000)
	if got := p.Experience(); got != 0 {
		t.Fatalf("Experience() = %v, want 0", got)
	}
	if got := p.ExperienceLevel(); got != 0 {
		t.Fatalf("level = %v, want 0", got)
	}
}

func TestSetExperienceLevelLandsOnTheLevelAsked(t *testing.T) {
	p := newStubPlayer(Config{})
	for _, level := range []int{0, 1, 5, 16, 17, 31, 32, 50} {
		p.SetExperienceLevel(level)
		if got := p.ExperienceLevel(); got != level {
			t.Fatalf("SetExperienceLevel(%v) then ExperienceLevel() = %v", level, got)
		}
	}
}

// PINNED BUG: the level and progress setters panic on an out-of-range argument
// instead of returning an error. The panic comes
// from entity.ExperienceManager, but Player is the exported surface that lets a
// caller reach it.
func TestExperienceSettersPanicOutOfRange(t *testing.T) {
	p := newStubPlayer(Config{})
	mustPanic(t, "SetExperienceLevel(-1)", func() { p.SetExperienceLevel(-1) })
	mustPanic(t, "SetExperienceProgress(-0.1)", func() { p.SetExperienceProgress(-0.1) })
	mustPanic(t, "SetExperienceProgress(1.1)", func() { p.SetExperienceProgress(1.1) })
}

// PINNED BUG: SetExperienceProgress does not round trip. It converts the
// fraction into an integer experience total plus a decimal remainder and then
// solves the level curve backwards, and the two do not cancel out.
// rather than with a golden number, because the exact error belongs to
// entity.ExperienceManager and would move with it.
func TestSetExperienceProgressDoesNotRoundTrip(t *testing.T) {
	p := newStubPlayer(Config{})
	p.SetExperienceLevel(5)

	p.SetExperienceProgress(0.5)

	got := p.ExperienceProgress()
	if p.ExperienceLevel() != 5 {
		t.Fatalf("level = %v, want 5: the progress write moved the level", p.ExperienceLevel())
	}
	if math.Abs(got-0.5) < 0.01 {
		t.Fatalf("progress = %v, want a value at least 0.01 away from 0.5: the round trip became exact", got)
	}
}

func TestCanCollectExperienceGating(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    Config
		before func(*Player)
		want   bool
	}{
		{name: "a live survival player", cfg: Config{}, want: true},
		{name: "creative", cfg: Config{GameMode: world.GameModeCreative}, want: true},
		{name: "spectator", cfg: Config{GameMode: world.GameModeSpectator}, want: false},
		{
			name:   "dead",
			cfg:    Config{},
			before: func(p *Player) { p.health.AddHealth(-20) },
			want:   false,
		},
		{
			name: "within the 100ms pickup delay",
			cfg:  Config{},
			before: func(p *Player) {
				now := time.Now()
				p.lastXPPickup = &now
			},
			want: false,
		},
		{
			name: "past the 100ms pickup delay",
			cfg:  Config{},
			before: func(p *Player) {
				earlier := time.Now().Add(-time.Second)
				p.lastXPPickup = &earlier
			},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(tc.cfg)
			if tc.before != nil {
				tc.before(p)
			}
			if got := p.CanCollectExperience(); got != tc.want {
				t.Fatalf("CanCollectExperience() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Collecting stamps the pickup time, so a second collection in the same
// instant is refused.
func TestCollectExperienceThrottlesItself(t *testing.T) {
	p := newStubPlayer(Config{})

	if !p.CollectExperience(7) {
		t.Fatal("first CollectExperience = false, want true")
	}
	if p.CollectExperience(7) {
		t.Fatal("second CollectExperience = true, want false: the 100ms delay did not apply")
	}
	if got := p.Experience(); got != 7 {
		t.Fatalf("Experience() = %v, want 7", got)
	}
}

// Mending spends the collected experience on repairing an item before any of it
// reaches the level bar: two durability per experience point.
func TestMendItemsRepairsTheHeldItemBeforeLevelling(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, mendingPickaxe(10)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	left := p.mendItems(5)

	if left != 0 {
		t.Fatalf("leftover experience = %v, want 0: 5 points repair 10 durability", left)
	}
	main, _ := p.HeldItems()
	if got := main.Durability(); got != 20 {
		t.Fatalf("durability = %v, want 20", got)
	}
}

// A repair that cannot use all the experience hands the rest back.
func TestMendItemsReturnsTheLeftoverExperience(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, mendingPickaxe(10)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	left := p.mendItems(100)

	// 49 durability were missing, costing ceil(49/2) = 25 experience.
	if left != 75 {
		t.Fatalf("leftover experience = %v, want 75", left)
	}
	main, _ := p.HeldItems()
	if got := main.Durability(); got != main.MaxDurability() {
		t.Fatalf("durability = %v, want %v", got, main.MaxDurability())
	}
}

// PINNED BUG: a repair of exactly one durability point costs nothing, because
// the experience is only deducted when the repair is 2 or more (player.go:2575).
// An item kept one point below full can be topped up for free on every orb.
func TestMendItemsRepairsTheLastDurabilityPointForFree(t *testing.T) {
	p := newStubPlayer(Config{})
	full := woodPickaxe().MaxDurability()
	if err := p.inv.SetItem(0, mendingPickaxe(full-1)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	left := p.mendItems(5)

	if left != 5 {
		t.Fatalf("leftover experience = %v, want 5: the last point was charged for", left)
	}
	main, _ := p.HeldItems()
	if got := main.Durability(); got != full {
		t.Fatalf("durability = %v, want %v", got, full)
	}
}

// An item already at full durability is not a mending candidate, so nothing is
// spent on it.
func TestMendItemsIgnoresAnUndamagedItem(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, mendingPickaxe(woodPickaxe().MaxDurability())); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	if left := p.mendItems(5); left != 5 {
		t.Fatalf("leftover experience = %v, want 5", left)
	}
}

// Armour is a mending candidate too, and the repaired piece is written back
// into the slot it came from.
func TestMendItemsRepairsArmour(t *testing.T) {
	p := newStubPlayer(Config{})
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1).
		WithEnchantments(item.NewEnchantment(enchantment.Mending, 1))
	p.armour.SetHelmet(helmet.WithDurability(10))

	left := p.mendItems(3)

	if left != 0 {
		t.Fatalf("leftover experience = %v, want 0", left)
	}
	if got := p.armour.Helmet().Durability(); got != 16 {
		t.Fatalf("helmet durability = %v, want 16", got)
	}
}

// PINNED BUG: the write back at player.go:2576-2580 tests the off-hand first
// and compares by value, so when both hands hold the same damaged mending item
// the repair always lands in the off-hand. The main hand is never repaired no
// matter which of the two the random draw picked, and the player is charged for
// a repair the main hand did not get.
func TestMendItemsRepairsTheOffHandWhenBothHandsMatch(t *testing.T) {
	p := newStubPlayer(Config{})
	damaged := mendingPickaxe(10)
	if err := p.inv.SetItem(0, damaged); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}
	if err := p.offHand.SetItem(0, damaged); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}

	left := p.mendItems(5)

	main, off := p.HeldItems()
	if left != 0 {
		t.Fatalf("leftover experience = %v, want 0", left)
	}
	if got := off.Durability(); got != 20 {
		t.Fatalf("off-hand durability = %v, want 20", got)
	}
	if got := main.Durability(); got != 10 {
		t.Fatalf("main hand durability = %v, want 10: it was repaired as well", got)
	}
}

// PINNED CONSTRAINT: with two or more distinct mending candidates the item to
// repair is drawn with rand.IntN from the math/rand/v2 global generator, which
// has no seed. Which item is repaired cannot be pinned; only the invariant that
// exactly one of them moves and the total repair is what the experience paid
// for.
func TestMendItemsRepairsExactlyOneOfSeveralCandidates(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, mendingPickaxe(10)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1).
		WithEnchantments(item.NewEnchantment(enchantment.Mending, 1))
	p.armour.SetHelmet(helmet.WithDurability(10))

	left := p.mendItems(3)

	main, _ := p.HeldItems()
	repaired := 0
	if main.Durability() != 10 {
		repaired++
	}
	if p.armour.Helmet().Durability() != 10 {
		repaired++
	}
	if repaired != 1 {
		t.Fatalf("%v items were repaired, want exactly 1", repaired)
	}
	if left != 0 {
		t.Fatalf("leftover experience = %v, want 0", left)
	}
}
