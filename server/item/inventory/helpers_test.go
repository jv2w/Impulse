package inventory

import (
	"sync"
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// This file holds the fakes and assertion helpers shared by every test in the
// package. Tests here pin current behaviour rather than desired behaviour: a
// failure means the behaviour changed, not necessarily that it became wrong.

// slotCall is one recorded SlotFunc invocation.
type slotCall struct {
	slot          int
	before, after item.Stack
}

// recorder records every SlotFunc invocation in call order. It is safe for
// concurrent use so that it can also be handed to the concurrency tests.
type recorder struct {
	mu    sync.Mutex
	calls []slotCall

	// reentrant, if non-nil, is invoked from within the SlotFunc itself. It is
	// used to pin what happens when a callback touches the inventory again.
	reentrant func(slot int, before, after item.Stack)
}

// fn is the SlotFunc to pass to New, Clone or NewArmour.
func (r *recorder) fn(slot int, before, after item.Stack) {
	r.mu.Lock()
	r.calls = append(r.calls, slotCall{slot: slot, before: before, after: after})
	re := r.reentrant
	r.mu.Unlock()

	if re != nil {
		re(slot, before, after)
	}
}

// calling returns a copy of the calls recorded so far.
func (r *recorder) calling() []slotCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]slotCall(nil), r.calls...)
}

// count returns the number of SlotFunc invocations recorded.
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// slots returns the slot index of every recorded call, in call order.
func (r *recorder) slots() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := make([]int, len(r.calls))
	for i, c := range r.calls {
		s[i] = c.slot
	}
	return s
}

// reset drops all recorded calls, keeping the reentrant hook.
func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// handlerCall is one recorded Handler invocation.
type handlerCall struct {
	method string // "take", "place" or "drop"
	slot   int
	it     item.Stack
}

// recordingHandler is a Handler that records each call. If cancel is true, it
// cancels the Context of every event it handles.
type recordingHandler struct {
	mu     sync.Mutex
	calls  []handlerCall
	cancel bool
}

func (h *recordingHandler) record(method string, ctx *Context, slot int, it item.Stack) {
	h.mu.Lock()
	h.calls = append(h.calls, handlerCall{method: method, slot: slot, it: it})
	cancel := h.cancel
	h.mu.Unlock()

	if cancel {
		ctx.Cancel()
	}
}

func (h *recordingHandler) HandleTake(ctx *Context, slot int, it item.Stack) {
	h.record("take", ctx, slot, it)
}

func (h *recordingHandler) HandlePlace(ctx *Context, slot int, it item.Stack) {
	h.record("place", ctx, slot, it)
}

func (h *recordingHandler) HandleDrop(ctx *Context, slot int, it item.Stack) {
	h.record("drop", ctx, slot, it)
}

// calling returns a copy of the Handler calls recorded so far.
func (h *recordingHandler) calling() []handlerCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]handlerCall(nil), h.calls...)
}

// count returns the number of Handler invocations recorded.
func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

// testSource is a world.DamageSource whose armour reduction can be toggled.
type testSource struct {
	reduced bool
}

func (s testSource) ReducedByArmour() bool     { return s.reduced }
func (s testSource) ReducedByResistance() bool { return true }
func (s testSource) Fire() bool                { return false }
func (s testSource) IgnoreTotem() bool         { return false }

var _ world.DamageSource = testSource{}

// diamonds returns a stack of n diamonds. item.Diamond has no MaxCounter, so
// its max count is the default 64.
func diamonds(n int) item.Stack {
	return item.NewStack(item.Diamond{}, n)
}

// sticks returns a stack of n sticks: an item type comparable to nothing that
// diamonds are comparable to.
func sticks(n int) item.Stack {
	return item.NewStack(item.Stick{}, n)
}

// helmet, chestplate, leggings and boots return iron armour pieces. Every
// armour piece has a max count of 1.
func helmet() item.Stack {
	return item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1)
}

func chestplate() item.Stack {
	return item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1)
}

func leggings() item.Stack {
	return item.NewStack(item.Leggings{Tier: item.ArmourTierIron{}}, 1)
}

func boots() item.Stack {
	return item.NewStack(item.Boots{Tier: item.ArmourTierIron{}}, 1)
}

// assertSlot fails the test if the stack in slot of inv is not equal to want.
// It compares with item.Stack.Equal and additionally compares emptiness and
// the concrete item type, because Equal considers any empty stack equal to any
// other empty stack.
func assertSlot(t *testing.T, inv *Inventory, slot int, want item.Stack) {
	t.Helper()

	got, err := inv.Item(slot)
	if err != nil {
		t.Fatalf("Item(%v): unexpected error %v", slot, err)
	}
	assertStack(t, got, want)
}

// assertStack fails the test if got is not equal to want, comparing count,
// emptiness and concrete item type.
func assertStack(t *testing.T, got, want item.Stack) {
	t.Helper()

	if got.Empty() != want.Empty() {
		t.Fatalf("stack emptiness: got %v (%v), want %v (%v)", got.Empty(), got, want.Empty(), want)
	}
	if got.Count() != want.Count() {
		t.Fatalf("stack count: got %v, want %v", got.Count(), want.Count())
	}
	if !got.Equal(want) {
		t.Fatalf("stack: got %v, want %v", got, want)
	}
	if gt, wt := itemType(got), itemType(want); gt != wt {
		t.Fatalf("stack item type: got %v, want %v", gt, wt)
	}
}

// itemType returns a printable identity for the item type of a stack. Empty
// stacks report "<empty>" because item.Stack.Item returns nil for them.
func itemType(s item.Stack) string {
	if s.Empty() {
		return "<empty>"
	}
	name, meta := s.Item().EncodeItem()
	return name + ":" + string(rune('0'+meta%10))
}

// assertSlots fails the test if the full slot contents of inv do not match
// want, element for element.
func assertSlots(t *testing.T, inv *Inventory, want ...item.Stack) {
	t.Helper()

	got := inv.Slots()
	if len(got) != len(want) {
		t.Fatalf("Slots(): got %v slots, want %v", len(got), len(want))
	}
	for i := range want {
		assertSlot(t, inv, i, want[i])
	}
}

// mustSet sets a stack in a slot, failing the test if an error is returned.
func mustSet(t *testing.T, inv *Inventory, slot int, it item.Stack) {
	t.Helper()

	if err := inv.SetItem(slot, it); err != nil {
		t.Fatalf("SetItem(%v): unexpected error %v", slot, err)
	}
}

// filled returns a new inventory of the size passed with the stacks passed set
// in the slots starting at 0. No SlotFunc is registered.
func filled(t *testing.T, size int, stacks ...item.Stack) *Inventory {
	t.Helper()

	inv := New(size, nil)
	for i, s := range stacks {
		mustSet(t, inv, i, s)
	}
	return inv
}
