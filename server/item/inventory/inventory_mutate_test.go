package inventory

import (
	"errors"
	"math"
	"testing"

	"github.com/jv2w/Impulse/server/item"
)

// This file pins the observable behaviour of the mutating and querying methods
// of Inventory: AddItem, RemoveItem, RemoveItemFunc, ContainsItem,
// ContainsItemFunc, Swap, Clear, Merge, Empty, First, FirstFunc and FirstEmpty.
//
// These are behaviour-pinning tests. The current code is the specification.
// Where the pinned behaviour is a defect, the test carries a defect marker
// and asserts the defect, so that a redesign has to acknowledge the change
// rather than silently alter it.
//
// Two properties recur and are worth stating once:
//
//   - AddItem, RemoveItemFunc and Clear collect their setItem callbacks and run
//     them once the lock is released, in LIFO order relative to the slots
//     visited, after the return value has been fixed but before the caller
//     resumes. Swap instead calls its two callbacks explicitly and so fires in
//     ascending order.
//   - setItem writes nothing and returns a nil callback when the
//     SlotValidatorFunc rejects the write. AddItem, Swap and Clear act on that;
//     RemoveItemFunc still deducts the count as if the write had landed.

// mutRejectAll is a SlotValidatorFunc that refuses every write.
func mutRejectAll(item.Stack, int) bool { return false }

// mutOnlySlot returns a SlotValidatorFunc accepting writes to the slot passed
// and refusing every other slot.
func mutOnlySlot(accepted int) SlotValidatorFunc {
	return func(_ item.Stack, slot int) bool { return slot == accepted }
}

// mutComparable returns a predicate matching stacks comparable to it, without
// the empty-stack short circuit of item.Stack.Comparable: an empty stack passed
// here matches nothing.
func mutComparable(it item.Stack) func(item.Stack) bool {
	return func(s item.Stack) bool { return !it.Empty() && !s.Empty() && it.Comparable(s) }
}

// mutPanics reports whether calling f panicked.
func mutPanics(f func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	f()
	return false
}

// mutAssertCalls fails the test if the SlotFunc calls recorded do not match
// want exactly, in order, comparing slot, before and after.
func mutAssertCalls(t *testing.T, r *recorder, want ...slotCall) {
	t.Helper()

	got := r.calling()
	if len(got) != len(want) {
		t.Fatalf("SlotFunc calls: got %v calls on slots %v, want %v", len(got), r.slots(), len(want))
	}
	for i := range want {
		if got[i].slot != want[i].slot {
			t.Fatalf("SlotFunc call %v: got slot %v, want slot %v", i, got[i].slot, want[i].slot)
		}
		assertStack(t, got[i].before, want[i].before)
		assertStack(t, got[i].after, want[i].after)
	}
}

// mutAssertSlotOrder fails the test if the slots the SlotFunc was called for do
// not match want exactly, in call order.
func mutAssertSlotOrder(t *testing.T, r *recorder, want ...int) {
	t.Helper()

	got := r.slots()
	if len(got) != len(want) {
		t.Fatalf("SlotFunc call order: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SlotFunc call order: got %v, want %v", got, want)
		}
	}
}

// mutRecording returns an inventory of the size passed with the stacks passed
// set from slot 0 onwards, and a recorder installed only after the setup writes
// so that the setup does not show up in the recorded calls.
func mutRecording(t *testing.T, size int, stacks ...item.Stack) (*Inventory, *recorder) {
	t.Helper()

	inv := filled(t, size, stacks...)
	r := &recorder{}
	inv.SlotFunc(r.fn)
	return inv, r
}

func TestMutAddItemEmptyStack(t *testing.T) {
	// AddItem short circuits on Stack.Empty before doing anything at all, and
	// Empty is true for a zero Stack, for a real item with a count of 0 and for
	// a zero Stack grown to a positive count (its item type is still nil).
	for name, in := range map[string]item.Stack{
		"zero stack":        {},
		"count 0 diamonds":  diamonds(0),
		"count 0 armour":    item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 0),
		"grown zero stack":  item.Stack{}.Grow(5),
		"count 0 with sock": sticks(0),
	} {
		t.Run(name, func(t *testing.T) {
			inv, r := mutRecording(t, 3, diamonds(2))

			n, err := inv.AddItem(in)
			if n != 0 || err != nil {
				t.Fatalf("AddItem(%v): got (%v, %v), want (0, nil)", in, n, err)
			}
			if c := r.count(); c != 0 {
				t.Fatalf("AddItem(empty): got %v SlotFunc calls, want 0", c)
			}
			assertSlots(t, inv, diamonds(2), item.Stack{}, item.Stack{})
		})
	}
}

func TestMutAddItemExactFit(t *testing.T) {
	inv, r := mutRecording(t, 1)

	n, err := inv.AddItem(diamonds(64))
	if n != 64 || err != nil {
		t.Fatalf("AddItem(64 diamonds): got (%v, %v), want (64, nil)", n, err)
	}
	assertSlots(t, inv, diamonds(64))
	mutAssertCalls(t, r, slotCall{slot: 0, before: item.Stack{}, after: diamonds(64)})
}

func TestMutAddItemTopsUpExistingStacksBeforeEmptySlots(t *testing.T) {
	// The first loop only visits non-empty slots and remembers the empty ones,
	// so an existing partial stack in a later slot is filled before an empty
	// slot that comes earlier.
	inv, r := mutRecording(t, 3, item.Stack{}, item.Stack{}, diamonds(60))

	n, err := inv.AddItem(diamonds(10))
	if n != 10 || err != nil {
		t.Fatalf("AddItem(10 diamonds): got (%v, %v), want (10, nil)", n, err)
	}
	// Slot 2 is topped up to its max count of 64 first, the remaining 6 land in
	// the first empty slot and slot 1 is never touched.
	assertSlots(t, inv, diamonds(6), item.Stack{}, diamonds(64))

	// The callbacks are deferred, so they fire in reverse registration order:
	// the empty-slot write (slot 0) before the top-up (slot 2).
	mutAssertCalls(t, r,
		slotCall{slot: 0, before: item.Stack{}, after: diamonds(6)},
		slotCall{slot: 2, before: diamonds(60), after: diamonds(64)},
	)
}

func TestMutAddItemSkipsFullStacksOfTheSameItem(t *testing.T) {
	// AddStack refuses when the receiver is already at MaxCount, so the slot is
	// left alone and the item goes to an empty slot instead.
	inv, r := mutRecording(t, 2, diamonds(64))

	n, err := inv.AddItem(diamonds(5))
	if n != 5 || err != nil {
		t.Fatalf("AddItem(5 diamonds): got (%v, %v), want (5, nil)", n, err)
	}
	assertSlots(t, inv, diamonds(64), diamonds(5))
	mutAssertCalls(t, r, slotCall{slot: 1, before: item.Stack{}, after: diamonds(5)})
}

func TestMutAddItemSpillsAcrossEmptySlots(t *testing.T) {
	inv, r := mutRecording(t, 3)

	n, err := inv.AddItem(diamonds(130))
	if n != 130 || err != nil {
		t.Fatalf("AddItem(130 diamonds): got (%v, %v), want (130, nil)", n, err)
	}
	assertSlots(t, inv, diamonds(64), diamonds(64), diamonds(2))
	// Deferred callbacks: descending slot order.
	mutAssertCalls(t, r,
		slotCall{slot: 2, before: item.Stack{}, after: diamonds(2)},
		slotCall{slot: 1, before: item.Stack{}, after: diamonds(64)},
		slotCall{slot: 0, before: item.Stack{}, after: diamonds(64)},
	)
}

func TestMutAddItemRespectsMaxCountOfOne(t *testing.T) {
	// Every armour piece has a max count of 1, so a stack of 3 helmets occupies
	// three slots.
	inv, r := mutRecording(t, 4)

	n, err := inv.AddItem(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 3))
	if n != 3 || err != nil {
		t.Fatalf("AddItem(3 helmets): got (%v, %v), want (3, nil)", n, err)
	}
	assertSlots(t, inv, helmet(), helmet(), helmet(), item.Stack{})
	mutAssertSlotOrder(t, r, 2, 1, 0)
}

func TestMutAddItemPartialFill(t *testing.T) {
	inv, r := mutRecording(t, 2)

	n, err := inv.AddItem(diamonds(200))
	if err == nil {
		t.Fatalf("AddItem(200 diamonds) into 2 slots: got nil error, want an error")
	}
	// n is what actually landed, not what was asked for.
	if n != 128 {
		t.Fatalf("AddItem(200 diamonds): got n = %v, want 128", n)
	}
	// The pinned fact is that there is NO sentinel for this failure: the error
	// is a bare fmt.Errorf with no wrapped cause, so a caller cannot branch on
	// it with errors.Is and is forced to compare strings.
	if got, want := err.Error(), "could not add full item stack to inventory"; got != want {
		t.Fatalf("AddItem error: got %q, want %q", got, want)
	}
	if u := errors.Unwrap(err); u != nil {
		t.Fatalf("AddItem error wraps %v, want no wrapped cause", u)
	}
	if errors.Is(err, ErrSlotOutOfRange) {
		t.Fatalf("AddItem error unexpectedly matches ErrSlotOutOfRange")
	}
	assertSlots(t, inv, diamonds(64), diamonds(64))
	mutAssertSlotOrder(t, r, 1, 0)
}

func TestMutAddItemFullInventory(t *testing.T) {
	for _, c := range []struct {
		name    string
		held    item.Stack
		add     item.Stack
		wantN   int
		wantErr bool
	}{
		{name: "same item at max count", held: diamonds(64), add: diamonds(1), wantN: 0, wantErr: true},
		{name: "incomparable item", held: diamonds(64), add: sticks(1), wantN: 0, wantErr: true},
		{name: "incomparable partial stack", held: sticks(1), add: diamonds(1), wantN: 0, wantErr: true},
		{name: "comparable partial stack fits", held: diamonds(63), add: diamonds(1), wantN: 1},
		{name: "comparable partial stack overflows", held: diamonds(63), add: diamonds(2), wantN: 1, wantErr: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, 1, c.held)

			n, err := inv.AddItem(c.add)
			if n != c.wantN {
				t.Fatalf("AddItem: got n = %v, want %v", n, c.wantN)
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("AddItem: got error %v, want error: %v", err, c.wantErr)
			}
			if c.wantN == 0 {
				// Nothing landed, so nothing was written and no callback fired.
				assertSlot(t, inv, 0, c.held)
				if got := r.count(); got != 0 {
					t.Fatalf("AddItem: got %v SlotFunc calls, want 0", got)
				}
			}
		})
	}
}

func TestMutAddItemRejectingValidatorReportsShortfall(t *testing.T) {
	// A slot the validator refuses stores nothing, so the stack is not consumed
	// there and the count reported is what actually landed. The refused slot is
	// skipped and the remainder goes on to the next one.
	inv, r := mutRecording(t, 2)
	inv.SlotValidatorFunc(mutRejectAll)

	n, err := inv.AddItem(diamonds(10))
	if n != 0 || err == nil {
		t.Fatalf("AddItem with rejecting validator: got (%v, %v), want (0, an error)", n, err)
	}
	if !inv.Empty() {
		t.Fatalf("AddItem with rejecting validator: inventory is %v, want empty", inv)
	}
	if got := r.count(); got != 0 {
		t.Fatalf("AddItem with rejecting validator: got %v SlotFunc calls, want 0", got)
	}
}

func TestMutAddItemSkipsRejectedSlots(t *testing.T) {
	inv, r := mutRecording(t, 2)
	inv.SlotValidatorFunc(mutOnlySlot(1))

	n, err := inv.AddItem(diamonds(10))
	if n != 10 || err != nil {
		t.Fatalf("AddItem: got (%v, %v), want (10, nil)", n, err)
	}
	assertSlots(t, inv, item.Stack{}, diamonds(10))
	mutAssertCalls(t, r, slotCall{slot: 1, before: item.Stack{}, after: diamonds(10)})
}

func TestMutRemoveItemEmptyStackRewritesFirstSlot(t *testing.T) {
	// PINNED BUG: RemoveItem with an empty stack removes nothing but still
	// rewrites the first non-empty slot and fires the SlotFunc for it.
	// RemoveItem passes it.Comparable, which is unconditionally true for an
	// empty receiver, and a count of 0, so the loop enters the first non-empty
	// slot and calls setItem(slot, slotIt.Grow(0)) with an unchanged count.
	inv, r := mutRecording(t, 3, item.Stack{}, diamonds(5), sticks(2))

	if err := inv.RemoveItem(item.Stack{}); err != nil {
		t.Fatalf("RemoveItem(empty): got error %v, want nil", err)
	}
	assertSlots(t, inv, item.Stack{}, diamonds(5), sticks(2))
	mutAssertCalls(t, r, slotCall{slot: 1, before: diamonds(5), after: diamonds(5)})
}

func TestMutRemoveItemEmptyStackOnEmptyInventory(t *testing.T) {
	// With no non-empty slot the loop body never runs, so n stays 0 and the
	// error is nil: removing nothing from nothing succeeds.
	inv, r := mutRecording(t, 3)

	if err := inv.RemoveItem(item.Stack{}); err != nil {
		t.Fatalf("RemoveItem(empty) on empty inventory: got error %v, want nil", err)
	}
	if got := r.count(); got != 0 {
		t.Fatalf("got %v SlotFunc calls, want 0", got)
	}
}

func TestMutRemoveItemMatchesComparableItemsOnly(t *testing.T) {
	inv, r := mutRecording(t, 3, sticks(4), diamonds(5), diamonds(5))

	if err := inv.RemoveItem(diamonds(5)); err != nil {
		t.Fatalf("RemoveItem(5 diamonds): got error %v, want nil", err)
	}
	// The stick slot is skipped entirely and does not count towards n.
	assertSlots(t, inv, sticks(4), item.Stack{}, diamonds(5))
	mutAssertCalls(t, r, slotCall{slot: 1, before: diamonds(5), after: item.Stack{}})
}

func TestMutRemoveItemFuncAccounting(t *testing.T) {
	for _, c := range []struct {
		name  string
		held  []item.Stack
		n     int
		want  []item.Stack
		order []int
	}{
		{
			name:  "fewer than the slot holds",
			held:  []item.Stack{diamonds(10)},
			n:     3,
			want:  []item.Stack{diamonds(7)},
			order: []int{0},
		},
		{
			name:  "exactly the slot holds",
			held:  []item.Stack{diamonds(10)},
			n:     10,
			want:  []item.Stack{item.Stack{}},
			order: []int{0},
		},
		{
			name:  "across two slots",
			held:  []item.Stack{diamonds(5), diamonds(5)},
			n:     8,
			want:  []item.Stack{item.Stack{}, diamonds(2)},
			order: []int{1, 0},
		},
		{
			name:  "across three slots stops at the last needed",
			held:  []item.Stack{diamonds(5), diamonds(5), diamonds(5)},
			n:     12,
			want:  []item.Stack{item.Stack{}, item.Stack{}, diamonds(3)},
			order: []int{2, 1, 0},
		},
		{
			name:  "exactly everything",
			held:  []item.Stack{diamonds(5), diamonds(5)},
			n:     10,
			want:  []item.Stack{item.Stack{}, item.Stack{}},
			order: []int{1, 0},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, len(c.held), c.held...)

			if err := inv.RemoveItemFunc(c.n, mutComparable(diamonds(1))); err != nil {
				t.Fatalf("RemoveItemFunc(%v): got error %v, want nil", c.n, err)
			}
			assertSlots(t, inv, c.want...)
			mutAssertSlotOrder(t, r, c.order...)
		})
	}
}

func TestMutRemoveItemFuncShortfallRemovesNothing(t *testing.T) {
	// A removal that cannot be satisfied in full leaves the inventory exactly as
	// it was. A caller that reacts to the error by retrying, or by taking the
	// items from somewhere else, must not find them already gone.
	for _, c := range []struct {
		name string
		held []item.Stack
		n    int
	}{
		{name: "short across two slots", held: []item.Stack{diamonds(5), diamonds(3)}, n: 20},
		// A slot the comparable function rejects is neither emptied nor counted
		// towards n, so the removal fails even though the inventory holds enough
		// items in total.
		{name: "short because a slot does not match", held: []item.Stack{sticks(10), diamonds(2)}, n: 5},
		{name: "max int", held: []item.Stack{diamonds(5), sticks(2), diamonds(7)}, n: math.MaxInt},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, len(c.held), c.held...)

			err := inv.RemoveItemFunc(c.n, mutComparable(diamonds(1)))
			if err == nil {
				t.Fatalf("RemoveItemFunc(%v): got nil error, want an error", c.n)
			}
			// As with AddItem, no sentinel exists for this failure.
			if got, want := err.Error(), "could not remove all items from the inventory"; got != want {
				t.Fatalf("RemoveItemFunc error: got %q, want %q", got, want)
			}
			if u := errors.Unwrap(err); u != nil {
				t.Fatalf("RemoveItemFunc error wraps %v, want no wrapped cause", u)
			}
			assertSlots(t, inv, c.held...)
			if got := r.count(); got != 0 {
				t.Fatalf("got %v SlotFunc calls, want 0", got)
			}
		})
	}
}

func TestMutRemoveItemFuncZeroCount(t *testing.T) {
	// PINNED BUG: RemoveItemFunc with n == 0 rewrites the first matching slot
	// with an identical stack and fires the SlotFunc for it, because the loop
	// is entered before n is tested.
	inv, r := mutRecording(t, 2, diamonds(5), diamonds(5))

	if err := inv.RemoveItemFunc(0, mutComparable(diamonds(1))); err != nil {
		t.Fatalf("RemoveItemFunc(0): got error %v, want nil", err)
	}
	assertSlots(t, inv, diamonds(5), diamonds(5))
	mutAssertCalls(t, r, slotCall{slot: 0, before: diamonds(5), after: diamonds(5)})
}

func TestMutRemoveItemFuncNegativeCountGrowsStack(t *testing.T) {
	// PINNED BUG: a negative n makes RemoveItemFunc ADD items. The count
	// written is slotIt.Grow(-n), which grows for a negative n, and the result
	// is only bounded by the max count clamp inside setItem.
	for _, c := range []struct {
		name string
		n    int
		want item.Stack
	}{
		{name: "grows by n", n: -5, want: diamonds(8)},
		{name: "clamped at max count", n: -100, want: diamonds(64)},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, 2, diamonds(3), diamonds(3))

			if err := inv.RemoveItemFunc(c.n, mutComparable(diamonds(1))); err != nil {
				t.Fatalf("RemoveItemFunc(%v): got error %v, want nil", c.n, err)
			}
			// Only the first matching slot is visited: n is already <= 0 after
			// the first subtraction, so the loop breaks.
			assertSlots(t, inv, c.want, diamonds(3))
			mutAssertCalls(t, r, slotCall{slot: 0, before: diamonds(3), after: c.want})
		})
	}
}

func TestMutRemoveItemFuncRejectingValidatorReportsFalseSuccess(t *testing.T) {
	// PINNED BUG: RemoveItemFunc returns a nil error while nothing was removed,
	// because the rejected setItem writes nothing but the count is deducted
	// regardless.
	inv, r := mutRecording(t, 2, diamonds(5), diamonds(5))
	inv.SlotValidatorFunc(mutRejectAll)

	if err := inv.RemoveItemFunc(10, mutComparable(diamonds(1))); err != nil {
		t.Fatalf("RemoveItemFunc with rejecting validator: got error %v, want nil", err)
	}
	assertSlots(t, inv, diamonds(5), diamonds(5))
	if got := r.count(); got != 0 {
		t.Fatalf("got %v SlotFunc calls, want 0", got)
	}
}

func TestMutContainsItemEmptyStack(t *testing.T) {
	// PINNED BUG: ContainsItem with an empty stack is always true, including on
	// a completely empty inventory. The count asked for is 0 and the loop only
	// has to leave n <= 0.
	for name, in := range map[string]item.Stack{
		"zero stack":       {},
		"count 0 diamonds": diamonds(0),
		"count 0 sticks":   sticks(0),
	} {
		t.Run(name, func(t *testing.T) {
			if !New(3, nil).ContainsItem(in) {
				t.Fatalf("ContainsItem(%v) on an empty inventory: got false, want true", in)
			}
			if !filled(t, 3, sticks(1)).ContainsItem(in) {
				t.Fatalf("ContainsItem(%v): got false, want true", in)
			}
		})
	}
}

func TestMutContainsItemCounting(t *testing.T) {
	inv := filled(t, 4, diamonds(3), sticks(9), diamonds(4))

	for _, c := range []struct {
		name string
		it   item.Stack
		want bool
	}{
		{name: "less than held", it: diamonds(5), want: true},
		{name: "exactly held", it: diamonds(7), want: true},
		{name: "one more than held", it: diamonds(8)},
		{name: "single slot suffices", it: sticks(9), want: true},
		{name: "not present at all", it: helmet()},
		{name: "count above max count", it: diamonds(65)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := inv.ContainsItem(c.it); got != c.want {
				t.Fatalf("ContainsItem(%v): got %v, want %v", c.it, got, c.want)
			}
		})
	}
}

func TestMutContainsItemFuncCounts(t *testing.T) {
	// ContainsItemFunc takes the write lock even though it only reads, so it is
	// not reentrant: calling it from a SlotFunc callback would deadlock. That
	// is pinned here as a documented property rather than as a test, since a
	// deadlock cannot be asserted.
	inv := filled(t, 3, diamonds(3), sticks(9), diamonds(4))

	for _, c := range []struct {
		name       string
		n          int
		comparable func(item.Stack) bool
		want       bool
	}{
		{name: "zero is always true", n: 0, comparable: func(item.Stack) bool { return false }, want: true},
		{name: "negative is always true", n: -1, comparable: func(item.Stack) bool { return false }, want: true},
		{name: "min int is always true", n: math.MinInt, comparable: func(item.Stack) bool { return false }, want: true},
		{name: "counts across slots", n: 7, comparable: mutComparable(diamonds(1)), want: true},
		{name: "one short of enough", n: 8, comparable: mutComparable(diamonds(1))},
		{name: "counts everything", n: 16, comparable: func(item.Stack) bool { return true }, want: true},
		{name: "max int is never enough", n: math.MaxInt, comparable: func(item.Stack) bool { return true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := inv.ContainsItemFunc(c.n, c.comparable); got != c.want {
				t.Fatalf("ContainsItemFunc(%v): got %v, want %v", c.n, got, c.want)
			}
		})
	}
}

func TestMutContainsItemFuncSkipsEmptySlots(t *testing.T) {
	// The comparable function is never called for an empty slot, so a predicate
	// that would match one cannot make the count grow.
	inv := filled(t, 4, diamonds(1))

	var calls int
	got := inv.ContainsItemFunc(2, func(item.Stack) bool {
		calls++
		return true
	})
	if got {
		t.Fatalf("ContainsItemFunc(2): got true, want false")
	}
	if calls != 1 {
		t.Fatalf("comparable calls: got %v, want 1", calls)
	}
}

func TestMutContainsItemFuncDoesNotMutate(t *testing.T) {
	inv, r := mutRecording(t, 2, diamonds(3), sticks(1))

	if !inv.ContainsItemFunc(3, mutComparable(diamonds(1))) {
		t.Fatalf("ContainsItemFunc(3): got false, want true")
	}
	assertSlots(t, inv, diamonds(3), sticks(1))
	if got := r.count(); got != 0 {
		t.Fatalf("got %v SlotFunc calls, want 0", got)
	}
}

func TestMutSwap(t *testing.T) {
	for _, c := range []struct {
		name         string
		held         []item.Stack
		a, b         int
		want         []item.Stack
		wantCalls    []slotCall
		unusedSuffix int
	}{
		{
			name:      "full with empty",
			held:      []item.Stack{diamonds(5), item.Stack{}},
			a:         0,
			b:         1,
			want:      []item.Stack{item.Stack{}, diamonds(5)},
			wantCalls: []slotCall{{slot: 0, before: diamonds(5), after: item.Stack{}}, {slot: 1, before: item.Stack{}, after: diamonds(5)}},
		},
		{
			name:      "empty with full",
			held:      []item.Stack{item.Stack{}, diamonds(5)},
			a:         0,
			b:         1,
			want:      []item.Stack{diamonds(5), item.Stack{}},
			wantCalls: []slotCall{{slot: 0, before: item.Stack{}, after: diamonds(5)}, {slot: 1, before: diamonds(5), after: item.Stack{}}},
		},
		{
			name:      "full with full",
			held:      []item.Stack{diamonds(5), sticks(3)},
			a:         0,
			b:         1,
			want:      []item.Stack{sticks(3), diamonds(5)},
			wantCalls: []slotCall{{slot: 0, before: diamonds(5), after: sticks(3)}, {slot: 1, before: sticks(3), after: diamonds(5)}},
		},
		{
			name:      "descending slot order still fires ascending",
			held:      []item.Stack{diamonds(5), sticks(3)},
			a:         1,
			b:         0,
			want:      []item.Stack{sticks(3), diamonds(5)},
			wantCalls: []slotCall{{slot: 1, before: sticks(3), after: diamonds(5)}, {slot: 0, before: diamonds(5), after: sticks(3)}},
		},
		{
			// A slot swapped with itself keeps its contents but is written
			// twice, so the SlotFunc fires twice rather than not at all.
			name:      "same slot with itself",
			held:      []item.Stack{diamonds(5), sticks(3)},
			a:         0,
			b:         0,
			want:      []item.Stack{diamonds(5), sticks(3)},
			wantCalls: []slotCall{{slot: 0, before: diamonds(5), after: diamonds(5)}, {slot: 0, before: diamonds(5), after: diamonds(5)}},
		},
		{
			name:      "empty slot with itself",
			held:      []item.Stack{item.Stack{}, sticks(3)},
			a:         0,
			b:         0,
			want:      []item.Stack{item.Stack{}, sticks(3)},
			wantCalls: []slotCall{{slot: 0}, {slot: 0}},
		},
		{
			name:      "size 1 inventory with itself",
			held:      []item.Stack{diamonds(1)},
			a:         0,
			b:         0,
			want:      []item.Stack{diamonds(1)},
			wantCalls: []slotCall{{slot: 0, before: diamonds(1), after: diamonds(1)}, {slot: 0, before: diamonds(1), after: diamonds(1)}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, len(c.held), c.held...)

			if err := inv.Swap(c.a, c.b); err != nil {
				t.Fatalf("Swap(%v, %v): got error %v, want nil", c.a, c.b, err)
			}
			assertSlots(t, inv, c.want...)
			// Swap calls its two callbacks explicitly rather than deferring
			// them, so they fire in the order the slots were written.
			mutAssertCalls(t, r, c.wantCalls...)
		})
	}
}

func TestMutSwapOutOfRange(t *testing.T) {
	for _, c := range []struct {
		name string
		a, b int
	}{
		{name: "first negative", a: -1, b: 0},
		{name: "second negative", a: 0, b: -1},
		{name: "first at size", a: 2, b: 0},
		{name: "second at size", a: 0, b: 2},
		{name: "second above size", a: 0, b: 3},
		{name: "both out of range", a: -1, b: 2},
		{name: "min int", a: math.MinInt, b: 0},
		{name: "max int", a: 0, b: math.MaxInt},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, 2, diamonds(5), sticks(3))

			err := inv.Swap(c.a, c.b)
			if !errors.Is(err, ErrSlotOutOfRange) {
				t.Fatalf("Swap(%v, %v): got error %v, want ErrSlotOutOfRange", c.a, c.b, err)
			}
			// Neither slot is written when only one of the two is invalid.
			assertSlots(t, inv, diamonds(5), sticks(3))
			if got := r.count(); got != 0 {
				t.Fatalf("got %v SlotFunc calls, want 0", got)
			}
		})
	}
}

func TestMutSwapRejectedValidatorDestroysItem(t *testing.T) {
	// PINNED BUG: Swap performs its two writes independently. When the
	// validator accepts one and rejects the other, the accepted write lands and
	// the stack that should have moved the other way is destroyed, while the
	// stack that did move is left duplicated in both slots.
	for _, c := range []struct {
		name     string
		accepted int
		want     []item.Stack
		wantCall slotCall
	}{
		{
			name:     "only slot 0 accepted",
			accepted: 0,
			want:     []item.Stack{sticks(3), sticks(3)},
			wantCall: slotCall{slot: 0, before: diamonds(5), after: sticks(3)},
		},
		{
			name:     "only slot 1 accepted",
			accepted: 1,
			want:     []item.Stack{diamonds(5), diamonds(5)},
			wantCall: slotCall{slot: 1, before: sticks(3), after: diamonds(5)},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, r := mutRecording(t, 2, diamonds(5), sticks(3))
			inv.SlotValidatorFunc(mutOnlySlot(c.accepted))

			if err := inv.Swap(0, 1); err != nil {
				t.Fatalf("Swap(0, 1): got error %v, want nil", err)
			}
			assertSlots(t, inv, c.want...)
			// Only the accepted write reports a change: the rejected one
			// returns a no-op callback.
			mutAssertCalls(t, r, c.wantCall)
		})
	}
}

func TestMutClear(t *testing.T) {
	inv, r := mutRecording(t, 4, diamonds(5), item.Stack{}, sticks(3), diamonds(1))

	got := inv.Clear()
	if len(got) != 3 {
		t.Fatalf("Clear(): got %v stacks, want 3", len(got))
	}
	// Only non-empty stacks are returned, in ascending slot order.
	assertStack(t, got[0], diamonds(5))
	assertStack(t, got[1], sticks(3))
	assertStack(t, got[2], diamonds(1))

	if !inv.Empty() {
		t.Fatalf("Clear(): inventory is %v, want empty", inv)
	}
	assertSlots(t, inv, item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{})

	// The callbacks are deferred, so they fire in descending slot order, the
	// reverse of the order the stacks appear in the returned slice.
	mutAssertCalls(t, r,
		slotCall{slot: 3, before: diamonds(1), after: item.Stack{}},
		slotCall{slot: 2, before: sticks(3), after: item.Stack{}},
		slotCall{slot: 0, before: diamonds(5), after: item.Stack{}},
	)
}

func TestMutClearEmptyInventory(t *testing.T) {
	inv, r := mutRecording(t, 3)

	got := inv.Clear()
	if got == nil {
		t.Fatalf("Clear() on an empty inventory: got nil, want a non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("Clear() on an empty inventory: got %v stacks, want 0", len(got))
	}
	if c := r.count(); c != 0 {
		t.Fatalf("got %v SlotFunc calls, want 0", c)
	}
}

func TestMutClearReturnedSliceIsNotAliased(t *testing.T) {
	inv := filled(t, 2, diamonds(5), sticks(3))

	got := inv.Clear()
	got[0] = helmet()
	// Writing to the returned slice does not resurrect anything, and refilling
	// the inventory does not write back into the returned slice.
	if !inv.Empty() {
		t.Fatalf("inventory is %v after mutating the Clear() result, want empty", inv)
	}
	mustSet(t, inv, 0, diamonds(9))
	assertStack(t, got[0], helmet())
	assertStack(t, got[1], sticks(3))
}

func TestMutClearReturnsOnlyWhatItTook(t *testing.T) {
	// Clear hands the caller the stacks it removed. A slot the validator refuses
	// to empty keeps its stack, so that stack must not also be returned: the
	// caller would put it somewhere else while it is still in the inventory.
	t.Run("every slot rejected", func(t *testing.T) {
		inv, r := mutRecording(t, 2, diamonds(5), sticks(3))
		inv.SlotValidatorFunc(mutRejectAll)

		if got := inv.Clear(); len(got) != 0 {
			t.Fatalf("Clear(): got %v stacks, want 0", len(got))
		}
		assertSlots(t, inv, diamonds(5), sticks(3))
		if c := r.count(); c != 0 {
			t.Fatalf("got %v SlotFunc calls, want 0", c)
		}
	})

	t.Run("one slot rejected", func(t *testing.T) {
		inv, r := mutRecording(t, 2, diamonds(5), sticks(3))
		inv.SlotValidatorFunc(mutOnlySlot(1))

		got := inv.Clear()
		if len(got) != 1 {
			t.Fatalf("Clear(): got %v stacks, want 1", len(got))
		}
		assertStack(t, got[0], sticks(3))
		assertSlots(t, inv, diamonds(5), item.Stack{})
		mutAssertCalls(t, r, slotCall{slot: 1, before: sticks(3), after: item.Stack{}})
	})
}

func TestMutMergeSizeAndOrder(t *testing.T) {
	a := filled(t, 3, diamonds(5), item.Stack{}, sticks(3))
	b := filled(t, 2, helmet())

	m := a.Merge(b, nil)
	if got := m.Size(); got != 5 {
		t.Fatalf("Merge().Size(): got %v, want 5", got)
	}
	// The receiver's slots come first, then the argument's, at their original
	// offsets within each source.
	assertSlots(t, m, diamonds(5), item.Stack{}, sticks(3), helmet(), item.Stack{})
}

func TestMutMergeCopiesSlots(t *testing.T) {
	a := filled(t, 2, diamonds(5))
	b := filled(t, 2, sticks(3))

	m := a.Merge(b, nil)

	mustSet(t, a, 0, helmet())
	mustSet(t, b, 0, boots())
	assertSlots(t, m, diamonds(5), item.Stack{}, sticks(3), item.Stack{})

	mustSet(t, m, 0, leggings())
	mustSet(t, m, 2, chestplate())
	assertSlots(t, a, helmet(), item.Stack{})
	assertSlots(t, b, boots(), item.Stack{})
}

func TestMutMergeSlotFuncOwnership(t *testing.T) {
	ra, rb, rm := &recorder{}, &recorder{}, &recorder{}
	a, b := New(2, ra.fn), New(2, rb.fn)
	mustSet(t, a, 0, diamonds(5))
	mustSet(t, b, 0, sticks(3))
	ra.reset()
	rb.reset()

	m := a.Merge(b, rm.fn)
	// Merge assigns the slots directly instead of going through setItem, so no
	// SlotFunc runs for the merged contents.
	if c := rm.count(); c != 0 {
		t.Fatalf("Merge(): got %v SlotFunc calls on the merged inventory, want 0", c)
	}

	mustSet(t, m, 1, helmet())
	mutAssertCalls(t, rm, slotCall{slot: 1, before: item.Stack{}, after: helmet()})
	// The sources' SlotFuncs are not inherited and are not called for writes to
	// the merged inventory.
	if c := ra.count() + rb.count(); c != 0 {
		t.Fatalf("got %v SlotFunc calls on the source inventories, want 0", c)
	}
}

func TestMutMergeNilSlotFunc(t *testing.T) {
	a, b := filled(t, 1, diamonds(5)), filled(t, 1, sticks(3))

	m := a.Merge(b, nil)
	// New substitutes a no-op for a nil SlotFunc, so writing does not panic.
	mustSet(t, m, 0, helmet())
	assertSlots(t, m, helmet(), sticks(3))
}

func TestMutMergeResetsValidator(t *testing.T) {
	// Merge builds the new inventory with New, which installs an always-true
	// validator. A source's restrictive validator is therefore dropped, and
	// slots that source would refuse can be written in the merged inventory.
	a := filled(t, 1, diamonds(5))
	a.SlotValidatorFunc(mutRejectAll)
	b := filled(t, 1, sticks(3))

	m := a.Merge(b, nil)
	assertSlots(t, m, diamonds(5), sticks(3))

	mustSet(t, m, 0, helmet())
	assertSlot(t, m, 0, helmet())
	// The source itself still refuses writes.
	mustSet(t, a, 0, boots())
	assertSlot(t, a, 0, diamonds(5))
}

func TestMutFirst(t *testing.T) {
	inv := filled(t, 4, item.Stack{}, sticks(3), diamonds(1))

	for _, c := range []struct {
		name     string
		it       item.Stack
		wantSlot int
		wantOk   bool
	}{
		{
			// PINNED BUG: First with an empty stack matches the first non-empty
			// slot instead of finding nothing, because item.Stack.Comparable is
			// unconditionally true when either stack is empty.
			name: "empty stack matches the first non-empty slot", it: item.Stack{}, wantSlot: 1, wantOk: true,
		},
		{name: "count 0 stack behaves the same", it: diamonds(0), wantSlot: 1, wantOk: true},
		{name: "found", it: diamonds(1), wantSlot: 2, wantOk: true},
		// First ignores the count entirely: it only compares item types.
		{name: "count is ignored", it: diamonds(64), wantSlot: 2, wantOk: true},
		{name: "count above max count is ignored", it: diamonds(math.MaxInt), wantSlot: 2, wantOk: true},
		{name: "not found", it: helmet(), wantSlot: -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			slot, ok := inv.First(c.it)
			if slot != c.wantSlot || ok != c.wantOk {
				t.Fatalf("First(%v): got (%v, %v), want (%v, %v)", c.it, slot, ok, c.wantSlot, c.wantOk)
			}
		})
	}
}

func TestMutFirstFuncSkipsEmptySlots(t *testing.T) {
	// The predicate is not called for empty slots, so a predicate matching
	// everything still returns the first non-empty slot.
	inv := filled(t, 4, item.Stack{}, item.Stack{}, diamonds(2))

	var seen []item.Stack
	slot, ok := inv.FirstFunc(func(s item.Stack) bool {
		seen = append(seen, s)
		return true
	})
	if slot != 2 || !ok {
		t.Fatalf("FirstFunc: got (%v, %v), want (2, true)", slot, ok)
	}
	if len(seen) != 1 {
		t.Fatalf("predicate calls: got %v, want 1", len(seen))
	}
	assertStack(t, seen[0], diamonds(2))
}

func TestMutFirstFuncNoMatch(t *testing.T) {
	inv := filled(t, 2, diamonds(2))

	slot, ok := inv.FirstFunc(func(item.Stack) bool { return false })
	if slot != -1 || ok {
		t.Fatalf("FirstFunc: got (%v, %v), want (-1, false)", slot, ok)
	}
}

func TestMutFirstEmpty(t *testing.T) {
	for _, c := range []struct {
		name     string
		inv      func(t *testing.T) *Inventory
		wantSlot int
		wantOk   bool
	}{
		{
			name:     "fresh inventory",
			inv:      func(*testing.T) *Inventory { return New(3, nil) },
			wantSlot: 0, wantOk: true,
		},
		{
			name:     "first slots taken",
			inv:      func(t *testing.T) *Inventory { return filled(t, 3, diamonds(1), sticks(1)) },
			wantSlot: 2, wantOk: true,
		},
		{
			name:     "full inventory",
			inv:      func(t *testing.T) *Inventory { return filled(t, 2, diamonds(1), sticks(1)) },
			wantSlot: -1,
		},
		{
			name:     "size 1 full",
			inv:      func(t *testing.T) *Inventory { return filled(t, 1, diamonds(1)) },
			wantSlot: -1,
		},
		{
			// A stack of a real item with a count of 0 is Empty, so the slot
			// counts as free.
			name:     "count 0 stack counts as empty",
			inv:      func(t *testing.T) *Inventory { return filled(t, 2, diamonds(0), sticks(1)) },
			wantSlot: 0, wantOk: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			slot, ok := c.inv(t).FirstEmpty()
			if slot != c.wantSlot || ok != c.wantOk {
				t.Fatalf("FirstEmpty(): got (%v, %v), want (%v, %v)", slot, ok, c.wantSlot, c.wantOk)
			}
		})
	}
}

func TestMutEmpty(t *testing.T) {
	inv := New(2, nil)
	if !inv.Empty() {
		t.Fatalf("Empty() on a fresh inventory: got false, want true")
	}

	mustSet(t, inv, 1, diamonds(1))
	if inv.Empty() {
		t.Fatalf("Empty() with an item: got true, want false")
	}

	inv.Clear()
	if !inv.Empty() {
		t.Fatalf("Empty() after Clear(): got false, want true")
	}

	// A stack of a real item with a count of 0 is Empty, so it does not make
	// the inventory non-empty.
	mustSet(t, inv, 0, diamonds(0))
	if !inv.Empty() {
		t.Fatalf("Empty() with a count 0 stack: got false, want true")
	}
}

func TestMutZeroValueInventory(t *testing.T) {
	// check panics on an inventory that was not built with New. The First
	// family never calls check and so reports "not found" instead, and AddItem
	// returns before check when the stack passed is empty.
	for _, c := range []struct {
		name       string
		call       func(inv *Inventory)
		wantPanics bool
	}{
		{name: "AddItem", call: func(inv *Inventory) { inv.AddItem(diamonds(1)) }, wantPanics: true},
		{name: "AddItem with an empty stack", call: func(inv *Inventory) {
			if n, err := inv.AddItem(item.Stack{}); n != 0 || err != nil {
				t.Fatalf("AddItem(empty): got (%v, %v), want (0, nil)", n, err)
			}
		}},
		{name: "RemoveItem", call: func(inv *Inventory) { inv.RemoveItem(diamonds(1)) }, wantPanics: true},
		{name: "RemoveItemFunc", call: func(inv *Inventory) {
			inv.RemoveItemFunc(1, func(item.Stack) bool { return true })
		}, wantPanics: true},
		{name: "ContainsItem", call: func(inv *Inventory) { inv.ContainsItem(diamonds(1)) }, wantPanics: true},
		{name: "ContainsItemFunc", call: func(inv *Inventory) {
			inv.ContainsItemFunc(1, func(item.Stack) bool { return true })
		}, wantPanics: true},
		{name: "Swap", call: func(inv *Inventory) { inv.Swap(0, 0) }, wantPanics: true},
		{name: "Clear", call: func(inv *Inventory) { inv.Clear() }, wantPanics: true},
		{name: "Empty", call: func(inv *Inventory) { inv.Empty() }, wantPanics: true},
		{name: "First", call: func(inv *Inventory) {
			if slot, ok := inv.First(diamonds(1)); slot != -1 || ok {
				t.Fatalf("First: got (%v, %v), want (-1, false)", slot, ok)
			}
		}},
		{name: "FirstFunc", call: func(inv *Inventory) {
			if slot, ok := inv.FirstFunc(func(item.Stack) bool { return true }); slot != -1 || ok {
				t.Fatalf("FirstFunc: got (%v, %v), want (-1, false)", slot, ok)
			}
		}},
		{name: "FirstEmpty", call: func(inv *Inventory) {
			if slot, ok := inv.FirstEmpty(); slot != -1 || ok {
				t.Fatalf("FirstEmpty: got (%v, %v), want (-1, false)", slot, ok)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			// A fresh inventory per case: the panicking methods hold the mutex
			// when check panics and never release it.
			if got := mutPanics(func() { c.call(&Inventory{}) }); got != c.wantPanics {
				t.Fatalf("panicked: got %v, want %v", got, c.wantPanics)
			}
		})
	}
}
