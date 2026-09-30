package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// dropControllable records the stacks dropped through it and answers nothing
// else: the stack request paths under test only call Drop on it.
type dropControllable struct {
	Controllable
	dropped []item.Stack
}

func (c *dropControllable) Drop(s item.Stack) int {
	c.dropped = append(c.dropped, s)
	return s.Count()
}

// newStackRequestSession returns a Session with the player's inventories set up
// and no container opened, a World to run the request in and a fresh handler.
func newStackRequestSession(t *testing.T) (*Session, *world.World, *ItemStackRequestHandler) {
	t.Helper()
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := newStubSession(t, nil)
	s.ui = inventory.New(64, nil)
	s.inv = inventory.New(36, nil)
	s.offHand = inventory.New(1, nil)
	s.enderChest = inventory.New(27, nil)
	s.armour = inventory.NewArmour(nil)
	s.openedWindow.Store(inventory.New(1, nil))
	h := &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
	}
	return s, w, h
}

// runStackRequest handles req inside a transaction of w.
func runStackRequest(t *testing.T, w *world.World, h *ItemStackRequestHandler, s *Session, c Controllable, req protocol.ItemStackRequest) {
	t.Helper()
	if err := w.Do(func(tx *world.Tx) {
		_ = h.handleRequest(req, s, tx, c)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
}

// invSlot names slot of the player's main inventory with the stack network ID
// the client would send for it.
func invSlot(s *Session, slot byte) protocol.StackRequestSlotInfo {
	it, _ := s.inv.Item(int(slot))
	return protocol.StackRequestSlotInfo{
		Container:      protocol.FullContainerName{ContainerID: protocol.ContainerInventory},
		Slot:           slot,
		StackNetworkID: item_id(it),
	}
}

// failingAction is a Take whose source names a stack network ID the server
// does not have, so it fails its slot check.
func failingAction(slot byte) *protocol.TakeStackRequestAction {
	return take(1, protocol.StackRequestSlotInfo{
		Container:      protocol.FullContainerName{ContainerID: protocol.ContainerInventory},
		Slot:           slot,
		StackNetworkID: 999999,
	}, protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerInventory},
		Slot:      slot + 1,
	})
}

// take builds a Take action. Its fields sit in an unexported embedded struct,
// so they cannot be set in a composite literal.
func take(count byte, src, dst protocol.StackRequestSlotInfo) *protocol.TakeStackRequestAction {
	a := &protocol.TakeStackRequestAction{}
	a.Count, a.Source, a.Destination = count, src, dst
	return a
}

func countItems(inv *inventory.Inventory) int {
	n := 0
	for _, it := range inv.Slots() {
		n += it.Count()
	}
	return n
}

// TestStackRequestDropThenFailureKeepsDropFinal drops a stack and then fails in
// the same request. The dropped items exist in the world, so the slot they
// left must stay empty rather than be restored.
func TestStackRequestDropThenFailureKeepsDropFinal(t *testing.T) {
	s, w, h := newStackRequestSession(t)
	_ = s.inv.SetItem(0, item.NewStack(item.Diamond{}, 64))
	c := &dropControllable{}

	runStackRequest(t, w, h, s, c, protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		&protocol.DropStackRequestAction{Count: 64, Source: invSlot(s, 0)},
		failingAction(5),
	}})

	if len(c.dropped) != 1 || c.dropped[0].Count() != 64 {
		t.Fatalf("dropped = %v, want one stack of 64", c.dropped)
	}
	if got := countItems(s.inv); got != 0 {
		t.Fatalf("items left in inventory = %d, want 0: the 64 dropped are in the world", got)
	}
}

// TestStackRequestRevertRestoresFirstContents moves a stack from slot 0 to 1
// and then from 1 to 2 before failing. The revert must put the stack back in
// slot 0 alone, not also leave it in slot 1.
func TestStackRequestRevertRestoresFirstContents(t *testing.T) {
	s, w, h := newStackRequestSession(t)
	_ = s.inv.SetItem(0, item.NewStack(item.Diamond{}, 32))

	first := take(32, invSlot(s, 0), invSlot(s, 1))
	// Slot 1 is named by the request's own ID, as the client does for a slot
	// an earlier action of the same request changed.
	second := take(32,
		protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerInventory}, Slot: 1, StackNetworkID: -1},
		invSlot(s, 2),
	)
	runStackRequest(t, w, h, s, &dropControllable{}, protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		first, second, failingAction(5),
	}})

	for slot, want := range []int{32, 0, 0} {
		if it, _ := s.inv.Item(slot); it.Count() != want {
			t.Errorf("slot %d holds %d items after the revert, want %d", slot, it.Count(), want)
		}
	}
}

// TestStackRequestRevertRestoresOffhand moves a stack into the offhand, named
// by a slot number other than 0, before failing. The offhand has one slot, so
// the revert must write slot 0 rather than the slot number the client named.
func TestStackRequestRevertRestoresOffhand(t *testing.T) {
	s, w, h := newStackRequestSession(t)
	_ = s.inv.SetItem(0, item.NewStack(item.Diamond{}, 16))

	toOffhand := take(16, invSlot(s, 0),
		protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerOffhand}, Slot: 1},
	)
	runStackRequest(t, w, h, s, &dropControllable{}, protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{
		toOffhand, failingAction(5),
	}})

	if got := countItems(s.offHand); got != 0 {
		t.Errorf("offhand holds %d items after the revert, want 0", got)
	}
	if it, _ := s.inv.Item(0); it.Count() != 16 {
		t.Errorf("slot 0 holds %d items after the revert, want 16", it.Count())
	}
}

// TestStackRequestRefusedWriteFails moves a diamond into an armour slot, which
// the armour inventory refuses. The request must fail and leave the diamond
// where it was instead of removing it from its slot.
func TestStackRequestRefusedWriteFails(t *testing.T) {
	s, w, h := newStackRequestSession(t)
	_ = s.inv.SetItem(0, item.NewStack(item.Diamond{}, 1))

	toArmour := take(1, invSlot(s, 0),
		protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerArmor}, Slot: 0},
	)
	runStackRequest(t, w, h, s, &dropControllable{}, protocol.ItemStackRequest{RequestID: -1, Actions: []protocol.StackRequestAction{toArmour}})

	if it, _ := s.inv.Item(0); it.Count() != 1 {
		t.Fatalf("slot 0 holds %d items, want the 1 diamond the armour slot refused", it.Count())
	}
	if got := countItems(s.armour.Inventory()); got != 0 {
		t.Fatalf("armour holds %d items, want 0", got)
	}
}
