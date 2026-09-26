package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// closeControllable counts MoveItemsToInventory calls. It embeds a nil
// Controllable so any other method panics instead of silently passing.
type closeControllable struct {
	Controllable
	moved int
}

func (c *closeControllable) MoveItemsToInventory() { c.moved++ }

// TestHandleContainerCloseIgnoresUnknownWindow pins the validation: a
// ContainerClose for a window the session never opened must not move items and
// must not echo a client-controlled container type. Before the fix
// MoveItemsToInventory ran before the WindowID check and the default branch
// echoed pk.ContainerType verbatim.
func TestHandleContainerCloseIgnoresUnknownWindow(t *testing.T) {
	s := newStubSession(t, nil)
	s.invOpened = true
	c := &closeControllable{}
	h := &ContainerCloseHandler{}

	pk := &packet.ContainerClose{WindowID: 99, ContainerType: 42}
	if err := h.Handle(pk, s, nil, c); err != nil {
		t.Fatalf("Handle(bogus window) error = %v, want nil ignore", err)
	}
	if c.moved != 0 {
		t.Fatalf("MoveItemsToInventory called %d times for unknown window 99, want 0", c.moved)
	}
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("session wrote %d packets for unknown window 99, want 0 (no client-type echo): %#v", len(pks), pks)
	}
	if !s.invOpened {
		t.Fatal("invOpened = false after bogus close, want unchanged true")
	}
}

// TestHandleContainerCloseEchoesStoredType pins that a valid container close
// echoes the stored container type, not client input. Regression guard for
// the validation move: legitimate closes keep working.
func TestHandleContainerCloseEchoesStoredType(t *testing.T) {
	s := newStubSession(t, nil)
	s.invOpened = true
	s.containerOpened.Store(true)
	s.openedWindowID.Store(7)
	s.openedContainerID.Store(11)
	s.openedWindow.Store(inventory.New(1, nil))
	pos := cube.Pos{0, 0, 0}
	s.openedPos.Store(&pos)
	c := &closeControllable{}
	h := &ContainerCloseHandler{}

	// Air at the opened position means RemoveViewer is skipped, but the
	// window state still clears through the same path as a real container.
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	var handleErr error
	if err := w.Do(func(tx *world.Tx) {
		handleErr = h.Handle(&packet.ContainerClose{WindowID: 7, ContainerType: 99}, s, tx, c)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if handleErr != nil {
		t.Fatalf("Handle(valid window) error = %v, want nil", handleErr)
	}
	if c.moved != 1 {
		t.Fatalf("MoveItemsToInventory called %d times for valid close, want 1", c.moved)
	}
	pk := onlyPacket(t, s).(*packet.ContainerClose)
	if pk.WindowID != 7 {
		t.Fatalf("echo WindowID = %d, want 7", pk.WindowID)
	}
	if pk.ContainerType != 11 {
		t.Fatalf("echo ContainerType = %d, want stored 11, not client 99", pk.ContainerType)
	}
}
