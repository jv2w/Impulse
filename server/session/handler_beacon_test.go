package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestHandleBeaconPaymentConsumesOne pins vanilla parity: selecting a beacon
// effect consumes exactly 1 payment item, not the whole stack. A 64x diamond
// stack must leave 63 behind.
func TestHandleBeaconPaymentConsumesOne(t *testing.T) {
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
		tx.SetBlock(pos, block.Beacon{}, nil)
		_ = s.ui.SetItem(int(beaconInputSlot), item.NewStack(item.Diamond{}, 64))
	}).Wait(context.Background()); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
	}
	a := &protocol.BeaconPaymentStackRequestAction{PrimaryEffect: 0, SecondaryEffect: 0}
	if err := w.Do(func(tx *world.Tx) {
		if err := h.handleBeaconPayment(a, s, tx); err != nil {
			t.Fatalf("handleBeaconPayment: %v", err)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	var got item.Stack
	if err := w.Do(func(tx *world.Tx) {
		var err error
		got, err = s.ui.Item(int(beaconInputSlot))
		if err != nil {
			t.Fatalf("read payment slot: %v", err)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if got.Count() != 63 {
		t.Fatalf("payment stack count = %d, want 63: a beacon effect must consume 1 item, not the whole stack", got.Count())
	}
	if _, ok := got.Item().(item.Diamond); ok != true && !got.Empty() {
		t.Fatalf("payment item = %T, want item.Diamond with 63 left", got.Item())
	}
}
