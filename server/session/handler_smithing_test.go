package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/item/recipe"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// newSmithingTestSession builds a Session with a smithing table at pos and a UI
// inventory large enough for the three smithing slots (0x33-0x35).
func newSmithingTestSession(t *testing.T, w *world.World, pos cube.Pos) *Session {
	t.Helper()
	s := newStubSession(t, nil)
	s.ui = inventory.New(64, nil)
	s.inv = inventory.New(36, nil)
	s.offHand = inventory.New(1, nil)
	s.enderChest = inventory.New(27, nil)
	s.armour = inventory.NewArmour(nil)
	s.recipes = map[uint32]recipe.Recipe{
		1: recipe.NewSmithingTransform(
			item.NewStack(item.Diamond{}, 1),
			item.NewStack(item.IronIngot{}, 1),
			item.NewStack(item.Stick{}, 1),
			item.NewStack(item.Emerald{}, 1),
			"smithing_table",
		),
	}
	s.containerOpened.Store(true)
	s.openedPos.Store(&pos)
	s.openedWindow.Store(inventory.New(1, nil))
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.SmithingTable{}, nil)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("set smithing table: %v", err)
	}
	return s
}

// TestHandleSmithingRejectsEmptySlots reproduces the infinite issuance: with
// 0,0,0 in the three smithing slots the handler must error and produce no
// result. Before the fix matchingStacks(air, expected) passes via
// Comparable empty=true, Grow(-1) stays 0 and Grow(1).WithItem(output) mints
// 1x output from nothing.
func TestHandleSmithingRejectsEmptySlots(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 0, 0}
	s := newSmithingTestSession(t, w, pos)

	h := &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
	}
	a := &protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1}

	var handleErr error
	if err := w.Do(func(tx *world.Tx) {
		handleErr = h.handleSmithing(a, s, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if handleErr == nil {
		t.Fatal("handleSmithing with empty input/material/template slots succeeded, want an error")
	}
	// No result may have been produced from air.
	if len(h.pendingResults) != 0 {
		t.Fatalf("pending results = %d, want 0: output was minted from empty slots", len(h.pendingResults))
	}
	var counts [3]int
	if err := w.Do(func(tx *world.Tx) {
		for i, slot := range []byte{smithingInputSlot, smithingMaterialSlot, smithingTemplateSlot} {
			st, _ := s.ui.Item(int(slot))
			counts[i] = st.Count()
		}
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if counts != [3]int{0, 0, 0} {
		t.Fatalf("slot counts = %v, want [0 0 0]", counts)
	}
}
