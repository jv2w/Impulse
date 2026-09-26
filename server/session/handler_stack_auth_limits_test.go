package session

import (
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func newStackTestHandler() *ItemStackRequestHandler {
	return &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
	}
}

// TestItemStackRequestRejectsTooManyRequests pins the per-packet bound: more
// Requests than a legitimate client ever batches must be rejected before any
// action executes. Before the fix the slice was ranged unchecked.
func TestItemStackRequestRejectsTooManyRequests(t *testing.T) {
	s := newStubSession(t, nil)
	h := newStackTestHandler()
	reqs := make([]protocol.ItemStackRequest, 64)
	pk := &packet.ItemStackRequest{Requests: reqs}
	if err := h.Handle(pk, s, nil, nil); err == nil {
		t.Fatalf("ItemStackRequest with %d requests succeeded, want rejection", len(reqs))
	}
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("session wrote %d packets for rejected batch, want 0", len(pks))
	}
}

// TestItemStackRequestRejectsTooManyActions pins the per-request bound: more
// Actions than a legitimate interaction carries must be rejected. Create
// actions with an empty pending list error on the first action, so the only
// observable before-fix behaviour is Handle returning nil after logging.
func TestItemStackRequestRejectsTooManyActions(t *testing.T) {
	s := newStubSession(t, nil)
	h := newStackTestHandler()
	actions := make([]protocol.StackRequestAction, 64)
	for i := range actions {
		actions[i] = &protocol.CreateStackRequestAction{ResultsSlot: 0}
	}
	pk := &packet.ItemStackRequest{Requests: []protocol.ItemStackRequest{{RequestID: 1, Actions: actions}}}
	if err := h.Handle(pk, s, nil, nil); err == nil {
		t.Fatalf("ItemStackRequest with %d actions succeeded, want rejection", len(actions))
	}
}

// TestItemStackRequestRejectsTooManyFilterStrings pins the filter-string
// count and content bounds at the entry point, before any anvil/loom logic
// runs. 16 strings of 300 bytes each must be rejected.
func TestItemStackRequestRejectsTooManyFilterStrings(t *testing.T) {
	s := newStubSession(t, nil)
	h := newStackTestHandler()
	filters := make([]string, 16)
	for i := range filters {
		filters[i] = strings.Repeat("a", 300)
	}
	pk := &packet.ItemStackRequest{Requests: []protocol.ItemStackRequest{{RequestID: 1, FilterStrings: filters}}}
	if err := h.Handle(pk, s, nil, nil); err == nil {
		t.Fatalf("ItemStackRequest with %d filter strings succeeded, want rejection", len(filters))
	}
}

// TestPlayerAuthInputRejectsTooManyBlockActions pins the per-tick bound: a
// movement packet carrying dozens of block actions must be rejected before
// any world work runs. Unknown actions error either way, so the assertion is
// on the cap message. handleBlockActions is called directly to avoid the
// movement path, which needs a full Controllable.
func TestPlayerAuthInputRejectsTooManyBlockActions(t *testing.T) {
	s := newStubSession(t, nil)
	c := &actionControllable{}
	h := PlayerAuthInputHandler{}
	actions := make([]protocol.PlayerBlockAction, 64)
	for i := range actions {
		actions[i] = protocol.PlayerBlockAction{Action: 0x7fffffff}
	}
	err := h.handleBlockActions(actions, s, c)
	if err == nil || !strings.Contains(err.Error(), "too many block actions") {
		t.Fatalf("PlayerAuthInput with %d block actions error = %v, want too-many rejection", len(actions), err)
	}
}
