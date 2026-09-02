package player

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/player/hud"
	"github.com/jv2w/Impulse/server/player/input"
	"github.com/jv2w/Impulse/server/session"
	"github.com/jv2w/Impulse/server/world"
)

// PINNED CONSTRAINT: a *Player cannot be built the way the server builds one.
// Config.Apply fills in every manager and inventory without touching a world,
// so the only two fields left over are the transaction and the entity handle,
// and each gets a substitute here:
//
//   - p.s stays nil. Player.session() (player.go:3418) hands back session.Nop
//     for a nil session, and Nop drops every packet in writePacket
//     (session/session.go:620), so all Send* calls are silent no-ops.
//   - p.tx is a zero &world.Tx{}. Its closed flag is false, so Tx.World()
//     (world/tx.go:362) returns the nil *World it holds instead of panicking,
//     and (*World).Difficulty() is nil-receiver safe (world/world.go:1080).
//     The stub therefore behaves as a world on DifficultyNormal that has no
//     loaded chunks at all.
//
// The zero transaction is what makes NewEventContext, and with it every handler
// dispatch, reachable. It stops at the first call that reads a field of the nil
// *World. TestStubTransactionBoundaries pins exactly where that is.
//
//   - p.handle comes from world.EntitySpawnOpts.New, which needs no world. It
//     holds its own unexported EntityData, so the playerData behind it is not
//     the one this stub uses; only its UUID is ever read, by Player.UUID and
//     through it Player.Data.
func newStubPlayer(cfg Config) *Player {
	if cfg.EnchantmentSeed == 0 {
		// fillDefaults would otherwise draw a fresh random seed per call.
		cfg.EnchantmentSeed = 1
	}
	data := &world.EntityData{}
	cfg.Apply(data)
	return &Player{
		tx:         &world.Tx{},
		handle:     world.EntitySpawnOpts{ID: stubUUID}.New(Type, cfg),
		data:       data,
		playerData: data.Data.(*playerData),
	}
}

// stubUUID is fixed so that a Config round trip through Player.Data can assert
// on it. EntitySpawnOpts.New only generates one when the ID is the nil UUID.
var stubUUID = uuid.MustParse("00000000-0000-4000-8000-0000000000ff")

// newOpenedPlayer builds the stub through ptype.Open, the constructor a real
// player goes through. Open installs the off-hand slot validator (type.go:23)
// and the three slot funcs (type.go:34-40) that newStubPlayer leaves off, so
// this is the only fixture that sees a real player's inventory behaviour.
//
// PINNED CONSTRAINT: those slot funcs broadcast through Player.viewers(), so on
// this fixture every accepted write to the held slot, the off-hand or the
// armour panics on the nil world. Only refused writes and reads are usable.
func newOpenedPlayer(cfg Config) *Player {
	if cfg.EnchantmentSeed == 0 {
		cfg.EnchantmentSeed = 1
	}
	data := &world.EntityData{}
	cfg.Apply(data)
	return Type.Open(&world.Tx{}, world.EntitySpawnOpts{ID: stubUUID}.New(Type, cfg), data).(*Player)
}

// mustPanic runs f and reports whether it panicked, so that a boundary pin
// states the failure it expects instead of taking the whole test binary down.
func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%v did not panic; the stub reaches further than pinned", name)
		}
	}()
	f()
}

// The zero transaction stands in for a world only as far as the nil *World is
// never read. These are the calls a reachable-looking method hits first when it
// crosses that line. The list is the direct input to splitting
// session.Controllable into smaller roles: every entry is a method whose own
// work needs nothing from the world, blocked by one collaborator it reaches
// through the transaction.
func TestStubTransactionBoundaries(t *testing.T) {
	p := newStubPlayer(Config{})

	// Tx.Viewers (world/tx.go:295) reads World.chunks. Player.viewers()
	// (player.go:3493) goes through it, so every broadcast does. This is what
	// blocks SetHeldSlot, SetGameMode, SetScale, SetNameTag, StartSprinting,
	// AddEffect and every other setter whose last statement is updateState.
	mustPanic(t, "viewers", func() { p.viewers() })
	mustPanic(t, "updateState", func() { p.updateState() })
	mustPanic(t, "SetHeldSlot", func() { _ = p.SetHeldSlot(3) })
	mustPanic(t, "SetScale", func() { p.SetScale(2) })
	mustPanic(t, "AddEffect", func() { p.RemoveEffect(nil) })

	// Tx.AddEntity (world/tx.go:259) reads World.entities. Drop always goes
	// through it; dropItems only reaches it for a stack it actually spawns, so
	// the clearing half of the death drop is pinnable and the spawning half is
	// not. See TestDropItemsClearsAllThreeInventories.
	mustPanic(t, "Drop", func() { p.Drop(item.NewStack(item.Apple{}, 1)) })
	mustPanic(t, "dropItems with a droppable stack", func() {
		_ = p.inv.SetItem(0, item.NewStack(item.Apple{}, 1))
		p.dropItems()
	})

	// Tx.Block and Tx.Liquid load a chunk, so the whole block interaction
	// surface is out of reach: PickBlock, BreakBlock, PlaceBlock, UseItemOnBlock
	// and Breathing.
	mustPanic(t, "PickBlock", func() { p.PickBlock(cube.Pos{}) })
	mustPanic(t, "Breathing", func() { p.Breathing() })

	// Tx.PlaySound (world/tx.go:250) is reached by damageItem only when the
	// stack it damages actually breaks, so item damage is pinnable up to but
	// not including the break.
	mustPanic(t, "damageItem to breaking", func() {
		p.damageItem(item.NewStack(item.Pickaxe{Tier: item.ToolTierWood}, 1), 1000)
	})

}

// PINNED BUG: four Player methods panic outright when the player has no
// session, because session.Nop leaves the fields they use nil. A Player is
// usable without a session everywhere else, so these four are the exception
// rather than the rule. Pinned as-is; decide in stage 1.
func TestSessionlessPlayerPanicsOnFourMessagingMethods(t *testing.T) {
	p := newStubPlayer(Config{})

	// session/text.go:134 dereferences a nil *string.
	mustPanic(t, "RemoveScoreboard", func() { p.RemoveScoreboard() })
	// session/player.go:476 indexes a nil handler map and asserts the result.
	mustPanic(t, "SendForm", func() { p.SendForm(nil) })
	mustPanic(t, "CloseDialogue", func() { p.CloseDialogue() })
	// session/player.go:826 assigns into a nil map.
	mustPanic(t, "HideHudElement", func() { p.HideHudElement(hud.Hunger()) })
}

// What the zero transaction does provide: a Context that dispatches to the
// handler and can be cancelled, and a Normal difficulty.
func TestStubTransactionSupportsEventContexts(t *testing.T) {
	p := newStubPlayer(Config{})

	ctx := NewEventContext(p.tx, p)
	if ctx.Cancelled() {
		t.Fatal("a fresh event context is already cancelled")
	}
	ctx.Cancel()
	if !ctx.Cancelled() {
		t.Fatal("Cancel() did not cancel the context")
	}
	if ctx.Player() != p {
		t.Fatal("Player() did not return the player the context was built for")
	}

	if got := p.tx.World().Difficulty(); got != world.DifficultyNormal {
		t.Fatalf("stub difficulty = %#v, want world.DifficultyNormal", got)
	}
}

// NewEventContext refuses a transaction that is not the player's own, and does
// so with a panic rather than a value.
//
// PINNED BUG: context.go:19 turns three distinct caller mistakes into one
// unrecoverable panic with a single message, for a failure the caller could be
// told about. Pinned as-is; decide in stage 1.
func TestNewEventContextPanicsOnAForeignTransaction(t *testing.T) {
	p := newStubPlayer(Config{})

	mustPanic(t, "NewEventContext(nil, p)", func() { NewEventContext(nil, p) })
	mustPanic(t, "NewEventContext(tx, nil)", func() { NewEventContext(p.tx, nil) })
	mustPanic(t, "NewEventContext(other, p)", func() { NewEventContext(&world.Tx{}, p) })
}

// Player.session() is the only reason the rest of this package's tests can run
// with no network session at all.
func TestSessionFallsBackToNop(t *testing.T) {
	p := newStubPlayer(Config{})
	if p.session() != session.Nop {
		t.Fatal("session() with a nil session did not return session.Nop")
	}
	// Every Send* the reachable methods go through, in one go: none of them may
	// touch the nil connection.
	p.sendFood()
	p.session().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
	p.session().SendHealth(p.Health(), p.MaxHealth(), p.absorptionHealth)

	// PINNED CONSTRAINT: session.Nop is a package-level singleton shared by
	// every sessionless player in the process. Player.LockInput and
	// Player.ClearInputLocks write to it, so a test that used them would leak
	// state into every other test in this binary. They are left uncovered.
	if p.InputLocked(input.Jump()) {
		t.Fatal("a fresh input lock is set on the shared Nop session")
	}
}
