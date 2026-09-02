package player

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// A nil *Player is a legal receiver for the two scheduling helpers: the nil
// check precedes every field access, so a caller holding a reference that has
// already gone away gets a finished task instead of a crash.
func TestSchedulingOnANilPlayerFailsAsAValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(f func(*world.Tx, *Player)) *world.Task
	}{
		{name: "Player.Do", call: func(f func(*world.Tx, *Player)) *world.Task { return (*Player)(nil).Do(f) }},
		{
			name: "Player.DoAfter",
			call: func(f func(*world.Tx, *Player)) *world.Task { return (*Player)(nil).DoAfter(time.Hour, f) },
		},
		{name: "package Do", call: func(f func(*world.Tx, *Player)) *world.Task { return Do(nil, f) }},
		{
			name: "package DoAfter",
			call: func(f func(*world.Tx, *Player)) *world.Task { return DoAfter(nil, time.Hour, f) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			task := tc.call(func(*world.Tx, *Player) { called = true })

			if called {
				t.Fatal("the callback ran for a player that does not exist")
			}
			select {
			case <-task.Done():
			default:
				t.Fatal("the task is not finished; a timer was started for a closed entity")
			}
			if !errors.Is(task.Err(), world.ErrEntityClosed) {
				t.Fatalf("Err() = %v, want world.ErrEntityClosed", task.Err())
			}
			if err := task.Wait(context.Background()); !errors.Is(err, world.ErrEntityClosed) {
				t.Fatalf("Wait() = %v, want world.ErrEntityClosed", err)
			}
			if task.Cancel() {
				t.Fatal("Cancel() = true on an already finished task")
			}
		})
	}
}

func TestNewRefOnANilHandle(t *testing.T) {
	if got := NewRef(nil).Handle(); got != nil {
		t.Fatalf("NewRef(nil).Handle() = %v, want nil", got)
	}
}

// PINNED BUG: NopProvider.Load reports "there is no stored data" with
// errors.New(""), a fresh empty-message error on every call. The server
// branches on it (server.go), so it is a condition callers must distinguish,
// which needs a sentinel or an error type. As written it matches nothing,
// carries no message, and cannot even be compared to itself.
// Pinned as-is; decide in stage 1.
func TestNopProviderLoadReturnsAnUnmatchableError(t *testing.T) {
	var p NopProvider

	cfg, w, err := p.Load(stubUUID, func(world.Dimension) *world.World { return nil })

	if err == nil {
		t.Fatal("Load returned a nil error, want a failure")
	}
	if err.Error() != "" {
		t.Fatalf("Load error message = %q, want the empty string", err.Error())
	}
	if _, _, err2 := p.Load(stubUUID, nil); errors.Is(err, err2) {
		t.Fatal("two Load errors compare equal; the sentinel was introduced")
	}
	if w != nil {
		t.Fatalf("Load world = %v, want nil", w)
	}
	if cfg.Inventory != nil || cfg.GameMode != nil {
		t.Fatalf("Load config = %+v, want the zero Config", cfg)
	}

	if err := p.Save(stubUUID, cfg, nil); err != nil {
		t.Fatalf("Save = %v, want nil", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
}

// NopHandler never cancels and never writes through the out-parameters it is
// handed. Those pointers are how a Handler changes damage, healing, experience
// and food, so a NopHandler that touched one would silently change the game for
// every server that embeds it.
func TestNopHandlerWritesThroughNoOutParameter(t *testing.T) {
	p := newStubPlayer(Config{})
	ctx := NewEventContext(p.tx, p)
	var h NopHandler

	food, xp, damage := 20, 7, 3
	health, hurt := 2.5, 4.5
	immunity := time.Second
	stack := apples(5)

	h.HandleFoodLoss(ctx, 20, &food)
	h.HandleExperienceGain(ctx, &xp)
	h.HandleItemDamage(ctx, woodPickaxe(), &damage)
	h.HandleHeal(ctx, &health, nil)
	h.HandleHurt(ctx, &hurt, false, &immunity, nil)
	h.HandleItemPickup(ctx, &stack)

	if food != 20 || xp != 7 || damage != 3 {
		t.Fatalf("int out-params = %v, %v, %v, want 20, 7, 3", food, xp, damage)
	}
	if health != 2.5 || hurt != 4.5 || immunity != time.Second {
		t.Fatalf("other out-params = %v, %v, %v, want 2.5, 4.5, 1s", health, hurt, immunity)
	}
	if stack.Count() != 5 {
		t.Fatalf("picked up stack count = %v, want 5", stack.Count())
	}
	if ctx.Cancelled() {
		t.Fatal("NopHandler cancelled the context")
	}
}

// format joins the operands with single spaces and strips the newline
// fmt.Sprintln adds, which is what every message, popup and tip on Player runs
// through.
func TestFormatJoinsWithSpaces(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []any
		want string
	}{
		{name: "nothing", in: nil, want: ""},
		{name: "one value", in: []any{"hello"}, want: "hello"},
		{name: "several values", in: []any{"a", 1, true}, want: "a 1 true"},
		{name: "an embedded newline is kept", in: []any{"a\nb"}, want: "a\nb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := format(tc.in); got != tc.want {
				t.Fatalf("format(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// PINNED BUG: format trims the newline suffix twice, so on top of the one
// fmt.Sprintln adds it also eats one the caller wrote. A message deliberately
// ending in a blank line loses it, and only the last one: two trailing newlines
// come back as one. Pinned as-is; decide in stage 1.
func TestFormatEatsOneNewlineOfTheCallersOwn(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "a\n", want: "a"},
		{in: "a\n\n", want: "a\n"},
		{in: "a\n\n\n", want: "a\n\n"},
	} {
		if got := format([]any{tc.in}); got != tc.want {
			t.Fatalf("format(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// PINNED BUG: the doc comment on EyeHeight promises 1.62, 1.26 sneaking and
// 0.52 prone. The code returns 1.62, 1.27 and 0.4. Eye height decides reach and
// line of sight, so the comment is the one that is wrong. Pinned as-is; decide
// in stage 1.
func TestEyeAndTorsoHeightGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*playerData)
		want float64
	}{
		{name: "standing", set: func(*playerData) {}, want: 1.62},
		{name: "sneaking", set: func(d *playerData) { d.sneaking = true }, want: 1.27},
		{name: "swimming", set: func(d *playerData) { d.swimming = true }, want: 0.4},
		{name: "crawling", set: func(d *playerData) { d.crawling = true }, want: 0.4},
		{name: "gliding", set: func(d *playerData) { d.gliding = true }, want: 0.4},
		{
			name: "prone beats sneaking",
			set:  func(d *playerData) { d.swimming, d.sneaking = true, true },
			want: 0.4,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{})
			tc.set(p.playerData)

			if got := p.EyeHeight(); got != tc.want {
				t.Fatalf("EyeHeight() = %v, want %v", got, tc.want)
			}
			if got, want := p.TorsoHeight(), tc.want-0.1; !closeTo(got, want) {
				t.Fatalf("TorsoHeight() = %v, want %v", got, want)
			}
		})
	}
}

// The eye height is not scaled, unlike the bounding box, so a scaled player's
// eyes stay where a default player's are.
func TestEyeHeightIgnoresTheScale(t *testing.T) {
	p := newStubPlayer(Config{})
	p.scale = 4

	if got := p.EyeHeight(); got != 1.62 {
		t.Fatalf("EyeHeight() at scale 4 = %v, want 1.62", got)
	}
}

// Cooldowns are keyed on the item's encoded name and expire by wall clock.
func TestItemCooldowns(t *testing.T) {
	p := newStubPlayer(Config{})

	if p.HasCooldown(item.Apple{}) {
		t.Fatal("a fresh player already has an apple cooldown")
	}

	p.SetCooldown(item.Apple{}, time.Hour)
	if !p.HasCooldown(item.Apple{}) {
		t.Fatal("HasCooldown = false right after SetCooldown")
	}
	if p.HasCooldown(item.Diamond{}) {
		t.Fatal("the apple cooldown applied to a diamond")
	}

	// An expired entry is both reported as absent and dropped from the map.
	p.SetCooldown(item.Diamond{}, -time.Hour)
	if p.HasCooldown(item.Diamond{}) {
		t.Fatal("an expired cooldown is still reported")
	}
	if _, ok := p.cooldowns["minecraft:diamond"]; ok {
		t.Fatal("the expired entry was not deleted")
	}
}

func TestCooldownsIgnoreANilItem(t *testing.T) {
	p := newStubPlayer(Config{})

	p.SetCooldown(nil, time.Hour)
	if p.HasCooldown(nil) {
		t.Fatal("HasCooldown(nil) = true, want false")
	}
	if len(p.cooldowns) != 0 {
		t.Fatalf("cooldowns = %v, want empty", p.cooldowns)
	}
}

// Without a session OnGround reads the movement computer rather than the flag
// the network path maintains, so a sessionless player's footing comes from the
// simulation.
func TestOnGroundReadsTheMovementComputerWithoutASession(t *testing.T) {
	p := newStubPlayer(Config{})
	p.onGround = true

	if p.OnGround() {
		t.Fatal("OnGround() = true, want the movement computer's false")
	}
}
