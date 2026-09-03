package session

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/creative"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// creativeModeControllable answers GameMode and nothing else. handleCreativeCraft
// reads GameMode and then either returns an error or appends to the handler's
// pending results, so the embedded nil Controllable is never called.
type creativeModeControllable struct {
	Controllable
	mode world.GameMode
}

func (c creativeModeControllable) GameMode() world.GameMode { return c.mode }

// creativeCraftHandler returns a handler whose first pending result is already
// empty. handleCreativeCraft appends its result behind that one, so handleCreate
// bails out on the empty slot 0 before it touches the nil Session and Tx, while
// the appended stack stays observable at index 1.
func creativeCraftHandler() *ItemStackRequestHandler {
	h := &ItemStackRequestHandler{}
	h.pendingResults = append(h.pendingResults, item.Stack{})
	return h
}

// craftCreative runs handleCreativeCraft for the network ID passed in creative
// mode. Both the Session and the Tx are nil: no reachable path in this test
// dereferences them.
func craftCreative(id uint32) (*ItemStackRequestHandler, error) {
	h := creativeCraftHandler()
	a := &protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: id}
	return h, h.handleCreativeCraft(a, nil, nil, creativeModeControllable{mode: world.GameModeCreative})
}

// TestHandleCreativeCraftRejectsNetworkIDZero pins the underflow. Network IDs
// are one-based, so ID 0 makes the uint32 subtraction wrap to 0xFFFFFFFF. The
// bounds check has to reject that on every GOARCH: on a 32-bit build the old
// int(index) conversion narrowed the wrapped value to -1, which passed the
// check and indexed creative.Items() out of range.
func TestHandleCreativeCraftRejectsNetworkIDZero(t *testing.T) {
	h, err := craftCreative(0)
	if err == nil {
		t.Fatal("handleCreativeCraft accepted creative item network ID 0, want an error")
	}
	// The wrapped uint32 is what the message has always printed. Asserted
	// literally: narrowing index to a signed type would print -1 instead.
	const want = "creative item with network ID 4294967295 does not exist"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if len(h.pendingResults) != 1 {
		t.Fatalf("pending results = %d, want the 1 seeded before the call", len(h.pendingResults))
	}
}

// TestHandleCreativeCraftRejectsNetworkIDPastEnd pins the upper bound and its
// message, which prints the zero-based index rather than the network ID.
func TestHandleCreativeCraftRejectsNetworkIDPastEnd(t *testing.T) {
	items := creative.Items()
	h, err := craftCreative(uint32(len(items)) + 1)
	if err == nil {
		t.Fatal("handleCreativeCraft accepted a network ID past the end, want an error")
	}
	if want := "creative item with network ID " + strconv.Itoa(len(items)) + " does not exist"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if len(h.pendingResults) != 1 {
		t.Fatalf("pending results = %d, want the 1 seeded before the call", len(h.pendingResults))
	}
}

// registerCreativeTestItems puts two distinguishable items in the creative
// registry. The registry is a package-level slice filled by the server package
// through a compiler directive, so it is empty in this package's tests and
// RegisterItem is the only exported way to fill it. It is run once per test
// binary so that repeated runs with -count do not keep growing the registry.
var registerCreativeTestItems = sync.OnceFunc(func() {
	creative.RegisterItem(creative.Item{Stack: item.NewStack(item.Diamond{}, 1)})
	creative.RegisterItem(creative.Item{Stack: item.NewStack(item.Emerald{}, 1)})
})

// TestHandleCreativeCraftSelectsItem pins which creative item a valid network
// ID maps to. Network IDs are one-based: ID 1 is index 0 and ID len is the last
// item. The stack is grown to a full stack before it is handed on.
func TestHandleCreativeCraftSelectsItem(t *testing.T) {
	registerCreativeTestItems()
	items := creative.Items()
	if len(items) < 2 {
		t.Fatalf("creative items = %d, want at least 2 for this test", len(items))
	}
	tests := []struct {
		name  string
		id    uint32
		index int
	}{
		{name: "first network ID is index 0", id: 1, index: 0},
		{name: "second network ID is index 1", id: 2, index: 1},
		{name: "last network ID is the last index", id: uint32(len(items)), index: len(items) - 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, err := craftCreative(test.id)
			// handleCreate rejects the empty seeded slot 0. Reaching that error
			// is what proves the bounds check let the ID through.
			if err == nil || !strings.Contains(err.Error(), "tried duplicating created result") {
				t.Fatalf("handleCreativeCraft(%d) error = %v, want the pending-result error", test.id, err)
			}
			if len(h.pendingResults) != 2 {
				t.Fatalf("pending results = %d, want 2", len(h.pendingResults))
			}
			want := items[test.index].Stack
			want = want.Grow(want.MaxCount() - 1)
			if got := h.pendingResults[1]; got.Empty() || !got.Equal(want) || got.Count() != want.Count() {
				t.Fatalf("selected stack = %v (count %d), want %v (count %d)", got, got.Count(), want, want.Count())
			}
		})
	}
}

// TestHandleCreativeCraftRequiresCreativeInventory pins that the game mode is
// checked before the network ID, so a survival player never reaches the bounds
// check at all.
func TestHandleCreativeCraftRequiresCreativeInventory(t *testing.T) {
	h := creativeCraftHandler()
	a := &protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: 0}
	err := h.handleCreativeCraft(a, nil, nil, creativeModeControllable{mode: world.GameModeSurvival})
	const want = "can only craft creative items in gamemode creative/spectator"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
