package session

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// anvilControllable handles the controllable methods the rename path reaches.
// It embeds a nil Controllable so any branch touching more panics instead of
// silently passing.
type anvilControllable struct {
	Controllable
	mode  world.GameMode
	level int
}

func (c *anvilControllable) GameMode() world.GameMode     { return c.mode }
func (c *anvilControllable) ExperienceLevel() int         { return c.level }
func (c *anvilControllable) SetExperienceLevel(level int) { c.level = level }

// TestHandleAnvilRejectsLongRename pins the external-input bound: a custom
// name longer than 256 bytes or with invalid UTF-8 must be rejected before it
// reaches NBT/inventory sync. Before the fix only FilterStringIndex was
// bounds-checked, so a 300-byte name was accepted and persisted.
func TestHandleAnvilRejectsLongRename(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 0, 0}
	s := newStubSession(t, nil)
	s.ui = inventory.New(64, nil)
	s.inv = inventory.New(36, nil)
	s.offHand = inventory.New(1, nil)
	s.enderChest = inventory.New(27, nil)
	s.armour = inventory.NewArmour(nil)
	s.containerOpened.Store(true)
	s.openedPos.Store(&pos)
	s.openedWindow.Store(inventory.New(1, nil))
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.Anvil{}, nil)
		_ = s.ui.SetItem(int(anvilInputSlot), item.NewStack(item.Diamond{}, 1))
		_ = s.ui.SetItem(int(anvilMaterialSlot), item.Stack{})
	}).Wait(context.Background()); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
	}
	longName := strings.Repeat("a", 300)
	if len(longName) <= 256 {
		t.Fatalf("test name = %d bytes, want >256 to exercise the bound", len(longName))
	}
	if !utf8.ValidString(longName) {
		t.Fatalf("test name must be valid UTF-8 to isolate the length bound")
	}
	a := &protocol.CraftRecipeOptionalStackRequestAction{FilterStringIndex: 0}
	co := &anvilControllable{mode: world.GameModeSurvival, level: 40}

	var handleErr error
	if err := w.Do(func(tx *world.Tx) {
		handleErr = h.handleCraftRecipeOptional(a, s, []string{longName}, co, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if handleErr == nil {
		t.Fatalf("anvil rename of %d bytes succeeded, want rejection at 256 bytes", len(longName))
	}
}

// TestHandleAnvilRejectsInvalidUTF8Rename pins the second half of the bound:
// invalid UTF-8 must be rejected even when short.
func TestHandleAnvilRejectsInvalidUTF8Rename(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 0, 0}
	s := newStubSession(t, nil)
	s.ui = inventory.New(64, nil)
	s.inv = inventory.New(36, nil)
	s.offHand = inventory.New(1, nil)
	s.enderChest = inventory.New(27, nil)
	s.armour = inventory.NewArmour(nil)
	s.containerOpened.Store(true)
	s.openedPos.Store(&pos)
	s.openedWindow.Store(inventory.New(1, nil))
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.Anvil{}, nil)
		_ = s.ui.SetItem(int(anvilInputSlot), item.NewStack(item.Diamond{}, 1))
		_ = s.ui.SetItem(int(anvilMaterialSlot), item.Stack{})
	}).Wait(context.Background()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h := &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
	}
	bad := string([]byte{0xff, 0xfe, 0x41})
	if utf8.ValidString(bad) {
		t.Fatalf("test string is valid UTF-8, want invalid bytes")
	}
	a := &protocol.CraftRecipeOptionalStackRequestAction{FilterStringIndex: 0}
	co := &anvilControllable{mode: world.GameModeSurvival, level: 40}
	var handleErr error
	if err := w.Do(func(tx *world.Tx) {
		handleErr = h.handleCraftRecipeOptional(a, s, []string{bad}, co, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if handleErr == nil {
		t.Fatal("anvil rename with invalid UTF-8 succeeded, want rejection")
	}
}
