package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// actionControllable handles the actions the tested branches reach. It embeds
// a nil Controllable so that any branch touching more than intended panics
// instead of silently passing.
type actionControllable struct {
	Controllable
	released int
}

func (c *actionControllable) ReleaseItem() { c.released++ }

// TestHandlePlayerActionInteractWithBlock pins the 1.26.50 rename:
// a 1.26.50 client sends PlayerActionInteractWithBlock where older clients
// sent PlayerActionStartBuildingBlock. The server intentionally does nothing
// for it, but the action must be accepted, not reported as unhandled.
func TestHandlePlayerActionInteractWithBlock(t *testing.T) {
	s := newStubSession(t, nil)
	c := &actionControllable{}
	if err := handlePlayerAction(protocol.PlayerActionInteractWithBlock, 0, protocol.BlockPos{}, selfEntityRuntimeID, s, c); err != nil {
		t.Fatalf("InteractWithBlock returned error %v, want nil", err)
	}
	if c.released != 0 {
		t.Fatalf("InteractWithBlock released the item %d times, want 0", c.released)
	}
	if err := handlePlayerAction(int32(0x7fffffff), 0, protocol.BlockPos{}, selfEntityRuntimeID, s, c); err == nil {
		t.Fatal("unknown action returned nil, want an unhandled-action error")
	}
}
