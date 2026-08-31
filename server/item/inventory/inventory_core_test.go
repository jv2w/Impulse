package inventory

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"github.com/jv2w/Impulse/server/item"
)

// This file pins the core surface of Inventory: New, Clone, SlotFunc,
// SlotValidatorFunc, Item, SetItem, Slots, Items, Size, String and Close, plus
// the unexported validSlot/check/setItem as they are observable through those.
//
// These are behaviour-pinning tests. Where the behaviour is plainly wrong it is
// still pinned exactly as it is today and flagged with a PINNED BUG comment.

// coreSizePanic is the value New panics with for a non-positive size.
const coreSizePanic = "inventory size must be at least 1"

// coreUninitialisedPanic is the value check panics with for a zero-value Inventory.
const coreUninitialisedPanic = "uninitialised inventory: inventory must be constructed using inventory.New()"

// coreRecovered runs f and returns whatever it panicked with, or nil if it
// returned normally.
func coreRecovered(f func()) (recovered any) {
	defer func() { recovered = recover() }()
	f()
	return nil
}

// coreValidatorCall is one recorded SlotValidatorFunc invocation.
type coreValidatorCall struct {
	stack item.Stack
	slot  int
}

// coreValidator is a SlotValidatorFunc that records its arguments and returns a
// fixed verdict. It is not safe for concurrent use; nothing here is concurrent.
type coreValidator struct {
	allow bool
	calls []coreValidatorCall
}

func (v *coreValidator) fn(s item.Stack, slot int) bool {
	v.calls = append(v.calls, coreValidatorCall{stack: s, slot: slot})
	return v.allow
}

// coreOutOfRangeSlots returns the slots that must be rejected for an inventory
// of the size passed, covering both ends and the extremes of int.
func coreOutOfRangeSlots(size int) []struct {
	name string
	slot int
} {
	return []struct {
		name string
		slot int
	}{
		{"minus one", -1},
		{"size", size},
		{"size plus one", size + 1},
		{"min int", math.MinInt},
		{"max int", math.MaxInt},
	}
}

func TestCoreNewPanicsOnNonPositiveSize(t *testing.T) {
	for _, c := range []struct {
		name string
		size int
	}{
		{"zero", 0},
		{"minus one", -1},
		{"min int", math.MinInt},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := coreRecovered(func() { New(c.size, nil) })
			if r == nil {
				t.Fatalf("New(%v, nil): expected a panic, got none", c.size)
			}
			if s, ok := r.(string); !ok || s != coreSizePanic {
				t.Fatalf("New(%v, nil): panicked with %#v, want %q", c.size, r, coreSizePanic)
			}
		})
	}
	for _, size := range []int{1, 2, 27} {
		if r := coreRecovered(func() { New(size, nil) }); r != nil {
			t.Fatalf("New(%v, nil): unexpected panic %#v", size, r)
		}
	}
}

func TestCoreNewInitialState(t *testing.T) {
	rec := &recorder{}
	inv := New(3, rec.fn)

	if got := inv.Size(); got != 3 {
		t.Fatalf("Size(): got %v, want 3", got)
	}
	if got := len(inv.Slots()); got != 3 {
		t.Fatalf("len(Slots()): got %v, want 3", got)
	}
	for slot, it := range inv.Slots() {
		if !it.Empty() {
			t.Fatalf("slot %v: got %v, want an empty stack", slot, it)
		}
	}
	if got := inv.Items(); len(got) != 0 {
		t.Fatalf("Items(): got %v entries, want 0", len(got))
	}
	if got := rec.count(); got != 0 {
		t.Fatalf("SlotFunc calls after New: got %v, want 0", got)
	}

	// Two inventories built by New share no state.
	a, b := New(1, nil), New(1, nil)
	mustSet(t, a, 0, diamonds(1))
	assertSlot(t, b, 0, item.Stack{})
}

func TestCoreNewNilSlotFuncIsReplacedByNoOp(t *testing.T) {
	inv := New(2, nil)

	// New substitutes a no-op for a nil SlotFunc, so a write must not panic.
	if r := coreRecovered(func() {
		if err := inv.SetItem(0, diamonds(4)); err != nil {
			t.Errorf("SetItem: unexpected error %v", err)
		}
	}); r != nil {
		t.Fatalf("SetItem with a nil SlotFunc passed to New: unexpected panic %#v", r)
	}
	assertSlot(t, inv, 0, diamonds(4))
}

func TestCoreSizeIsConstant(t *testing.T) {
	for _, size := range []int{1, 2, 5, 36} {
		inv := New(size, nil)
		if got := inv.Size(); got != size {
			t.Fatalf("Size() after New(%v): got %v", size, got)
		}
		mustSet(t, inv, size-1, diamonds(1))
		if got := inv.Size(); got != size {
			t.Fatalf("Size() after SetItem: got %v, want %v", got, size)
		}
		inv.SlotFunc(func(int, item.Stack, item.Stack) {})
		inv.SlotValidatorFunc(func(item.Stack, int) bool { return false })
		if got := inv.Size(); got != size {
			t.Fatalf("Size() after SlotFunc/SlotValidatorFunc: got %v, want %v", got, size)
		}
		if err := inv.Close(); err != nil {
			t.Fatalf("Close: unexpected error %v", err)
		}
		if got := inv.Size(); got != size {
			t.Fatalf("Size() after Close: got %v, want %v", got, size)
		}
		if got := inv.Clone(nil).Size(); got != size {
			t.Fatalf("Clone().Size(): got %v, want %v", got, size)
		}
	}
}

func TestCoreItemOutOfRange(t *testing.T) {
	const size = 4
	inv := filled(t, size, diamonds(2))

	for _, c := range coreOutOfRangeSlots(size) {
		t.Run(c.name, func(t *testing.T) {
			got, err := inv.Item(c.slot)
			if !errors.Is(err, ErrSlotOutOfRange) {
				t.Fatalf("Item(%v): got error %v, want ErrSlotOutOfRange", c.slot, err)
			}
			// The stack returned alongside the error is the zero Stack.
			assertStack(t, got, item.Stack{})
			if got.Count() != 0 || got.Item() != nil {
				t.Fatalf("Item(%v): got %v, want the zero item.Stack", c.slot, got)
			}
		})
	}

	// The two in-range boundaries must not error.
	for _, slot := range []int{0, size - 1} {
		if _, err := inv.Item(slot); err != nil {
			t.Fatalf("Item(%v): unexpected error %v", slot, err)
		}
	}
}

func TestCoreSetItemOutOfRange(t *testing.T) {
	const size = 4
	for _, c := range coreOutOfRangeSlots(size) {
		t.Run(c.name, func(t *testing.T) {
			rec, val := &recorder{}, &coreValidator{allow: true}
			inv := New(size, rec.fn)
			inv.SlotValidatorFunc(val.fn)
			mustSet(t, inv, 0, diamonds(2))
			rec.reset()
			val.calls = nil

			err := inv.SetItem(c.slot, sticks(1))
			if !errors.Is(err, ErrSlotOutOfRange) {
				t.Fatalf("SetItem(%v): got error %v, want ErrSlotOutOfRange", c.slot, err)
			}
			// An out-of-range write reaches neither the SlotFunc nor the validator:
			// validSlot is checked before setItem is ever reached.
			if got := rec.count(); got != 0 {
				t.Fatalf("SlotFunc calls after an out-of-range SetItem: got %v, want 0", got)
			}
			if got := len(val.calls); got != 0 {
				t.Fatalf("validator calls after an out-of-range SetItem: got %v, want 0", got)
			}
			assertSlots(t, inv, diamonds(2), item.Stack{}, item.Stack{}, item.Stack{})
		})
	}
}

func TestCoreSizeOneInventoryBoundaries(t *testing.T) {
	rec := &recorder{}
	inv := New(1, rec.fn)

	mustSet(t, inv, 0, diamonds(1))
	assertSlot(t, inv, 0, diamonds(1))
	if got := rec.count(); got != 1 {
		t.Fatalf("SlotFunc calls: got %v, want 1", got)
	}
	for _, slot := range []int{-1, 1, 2, math.MinInt, math.MaxInt} {
		if err := inv.SetItem(slot, sticks(1)); !errors.Is(err, ErrSlotOutOfRange) {
			t.Fatalf("SetItem(%v): got error %v, want ErrSlotOutOfRange", slot, err)
		}
		if _, err := inv.Item(slot); !errors.Is(err, ErrSlotOutOfRange) {
			t.Fatalf("Item(%v): got error %v, want ErrSlotOutOfRange", slot, err)
		}
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("SlotFunc calls after rejected writes: got %v, want 1", got)
	}
}

func TestCoreSetItemCallsSlotFuncInOrder(t *testing.T) {
	rec := &recorder{}
	inv := New(3, rec.fn)

	mustSet(t, inv, 2, diamonds(1))
	mustSet(t, inv, 0, sticks(5))
	mustSet(t, inv, 2, diamonds(7))
	// Overwriting a slot with an identical stack still fires: setItem does no
	// change detection.
	mustSet(t, inv, 0, sticks(5))
	// So does clearing an already-empty slot.
	mustSet(t, inv, 1, item.Stack{})

	calls := rec.calling()
	if len(calls) != 5 {
		t.Fatalf("SlotFunc calls: got %v, want 5", len(calls))
	}
	want := []struct {
		slot          int
		before, after item.Stack
	}{
		{2, item.Stack{}, diamonds(1)},
		{0, item.Stack{}, sticks(5)},
		{2, diamonds(1), diamonds(7)},
		{0, sticks(5), sticks(5)},
		{1, item.Stack{}, item.Stack{}},
	}
	for i, w := range want {
		if calls[i].slot != w.slot {
			t.Fatalf("call %v: slot %v, want %v", i, calls[i].slot, w.slot)
		}
		assertStack(t, calls[i].before, w.before)
		assertStack(t, calls[i].after, w.after)
	}
	assertSlots(t, inv, sticks(5), item.Stack{}, diamonds(7))
}

func TestCoreSetItemClampsToMaxCount(t *testing.T) {
	// PINNED BUG: setItem silently truncates a stack larger than its max count
	// and still returns a nil error, so the excess items vanish without the
	// caller ever being told. Pinned as-is; decide in stage 1.
	for _, c := range []struct {
		name  string
		set   item.Stack
		want  item.Stack
		clamp bool
	}{
		{"diamonds under max", diamonds(1), diamonds(1), false},
		{"diamonds at max", diamonds(64), diamonds(64), false},
		{"diamonds one over max", diamonds(65), diamonds(64), true},
		{"diamonds far over max", diamonds(100), diamonds(64), true},
		{"diamonds max int", item.NewStack(item.Diamond{}, math.MaxInt), diamonds(64), true},
		{"armour at max", helmet(), helmet(), false},
		{"armour over max", helmet().Grow(4), helmet(), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			inv := New(1, rec.fn)

			if err := inv.SetItem(0, c.set); err != nil {
				t.Fatalf("SetItem: got error %v, want nil", err)
			}
			assertSlot(t, inv, 0, c.want)

			calls := rec.calling()
			if len(calls) != 1 {
				t.Fatalf("SlotFunc calls: got %v, want 1", len(calls))
			}
			// The after stack handed to the SlotFunc is the clamped one, not what
			// the caller passed.
			assertStack(t, calls[0].after, c.want)
			if c.clamp && calls[0].after.Count() == c.set.Count() {
				t.Fatalf("SlotFunc after count: got the unclamped %v", calls[0].after.Count())
			}
		})
	}
}

func TestCoreSetItemValidatorRejection(t *testing.T) {
	// PINNED BUG: a rejected write returns a nil error, so the caller cannot tell
	// the difference between a stored stack and a discarded one. Pinned as-is;
	// decide in stage 1.
	rec, val := &recorder{}, &coreValidator{allow: false}
	inv := New(2, rec.fn)
	inv.SlotValidatorFunc(val.fn)

	if err := inv.SetItem(1, diamonds(3)); err != nil {
		t.Fatalf("SetItem with a rejecting validator: got error %v, want nil", err)
	}
	// Nothing was written and the SlotFunc never fired.
	assertSlots(t, inv, item.Stack{}, item.Stack{})
	if got := len(inv.Items()); got != 0 {
		t.Fatalf("Items(): got %v entries, want 0", got)
	}
	if got := rec.count(); got != 0 {
		t.Fatalf("SlotFunc calls after a rejected write: got %v, want 0", got)
	}
	if len(val.calls) != 1 {
		t.Fatalf("validator calls: got %v, want 1", len(val.calls))
	}
	if val.calls[0].slot != 1 {
		t.Fatalf("validator slot: got %v, want 1", val.calls[0].slot)
	}
	assertStack(t, val.calls[0].stack, diamonds(3))
}

func TestCoreSetItemValidatorSeesUnclampedStack(t *testing.T) {
	// The validator runs before the max count clamp, so it is handed the stack as
	// the caller wrote it, not the stack that ends up stored.
	val := &coreValidator{allow: true}
	inv := New(1, nil)
	inv.SlotValidatorFunc(val.fn)

	mustSet(t, inv, 0, diamonds(100))

	if len(val.calls) != 1 {
		t.Fatalf("validator calls: got %v, want 1", len(val.calls))
	}
	if got := val.calls[0].stack.Count(); got != 100 {
		t.Fatalf("validator stack count: got %v, want 100", got)
	}
	assertSlot(t, inv, 0, diamonds(64))
}

func TestCoreSlotValidatorFuncReplacement(t *testing.T) {
	inv := New(1, nil)

	// The default validator installed by New accepts everything.
	mustSet(t, inv, 0, diamonds(1))
	assertSlot(t, inv, 0, diamonds(1))

	inv.SlotValidatorFunc(func(item.Stack, int) bool { return false })
	if err := inv.SetItem(0, sticks(1)); err != nil {
		t.Fatalf("SetItem: got error %v, want nil", err)
	}
	assertSlot(t, inv, 0, diamonds(1))

	inv.SlotValidatorFunc(func(item.Stack, int) bool { return true })
	mustSet(t, inv, 0, sticks(1))
	assertSlot(t, inv, 0, sticks(1))
}

func TestCoreSlotFuncReplacement(t *testing.T) {
	first, second := &recorder{}, &recorder{}
	inv := New(2, first.fn)

	mustSet(t, inv, 0, diamonds(1))
	inv.SlotFunc(second.fn)
	mustSet(t, inv, 1, sticks(2))

	if got := first.count(); got != 1 {
		t.Fatalf("first SlotFunc calls: got %v, want 1", got)
	}
	calls := second.calling()
	if len(calls) != 1 {
		t.Fatalf("second SlotFunc calls: got %v, want 1", len(calls))
	}
	if calls[0].slot != 1 {
		t.Fatalf("second SlotFunc slot: got %v, want 1", calls[0].slot)
	}
	assertStack(t, calls[0].after, sticks(2))
}

func TestCoreSlotFuncNilPanicsOnNextWrite(t *testing.T) {
	// PINNED BUG: SlotFunc stores nil verbatim rather than reapplying the no-op
	// substitution New performs, so the next write panics on a nil func call.
	// Worse, the slot has already been written by the time the panic fires, so
	// the inventory is mutated without the SlotFunc ever running. Pinned as-is;
	// decide in stage 1.
	inv := New(2, nil)
	mustSet(t, inv, 0, diamonds(1))
	inv.SlotFunc(nil)

	r := coreRecovered(func() { _ = inv.SetItem(0, sticks(3)) })
	if r == nil {
		t.Fatalf("SetItem after SlotFunc(nil): expected a panic, got none")
	}
	if _, ok := r.(runtime.Error); !ok {
		t.Fatalf("SetItem after SlotFunc(nil): panicked with %#v, want a runtime.Error", r)
	}

	// SetItem unlocks before invoking the SlotFunc, so the panic leaves the mutex
	// free and the inventory usable.
	if !inv.mu.TryLock() {
		t.Fatalf("mutex still held after the panic in the SlotFunc")
	}
	inv.mu.Unlock()

	// The write landed regardless.
	assertSlot(t, inv, 0, sticks(3))
	if got := inv.Size(); got != 2 {
		t.Fatalf("Size() after the panic: got %v, want 2", got)
	}
}

func TestCoreSlotValidatorFuncNilPanics(t *testing.T) {
	// PINNED BUG: SlotValidatorFunc stores nil verbatim, so the next write panics
	// inside setItem rather than reinstating the accept-everything validator the
	// way New does. Pinned as-is; decide in stage 1.
	inv := New(2, nil)
	mustSet(t, inv, 0, diamonds(1))
	inv.SlotValidatorFunc(nil)

	r := coreRecovered(func() { _ = inv.SetItem(1, sticks(3)) })
	if r == nil {
		t.Fatalf("SetItem after SlotValidatorFunc(nil): expected a panic, got none")
	}
	if _, ok := r.(runtime.Error); !ok {
		t.Fatalf("SetItem after SlotValidatorFunc(nil): panicked with %#v, want a runtime.Error", r)
	}

	// The panic unwinds through the deferred unlock, so the inventory survives it
	// and a caller that recovers can still read it and install a working
	// validator.
	assertSlot(t, inv, 0, diamonds(1))
	assertSlot(t, inv, 1, item.Stack{})
	inv.SlotValidatorFunc(func(item.Stack, int) bool { return true })
	mustSet(t, inv, 1, sticks(3))
	assertSlot(t, inv, 1, sticks(3))
}

func TestCoreSlotsReturnsACopy(t *testing.T) {
	inv := filled(t, 3, diamonds(2), sticks(4))

	slots := inv.Slots()
	if len(slots) != 3 {
		t.Fatalf("len(Slots()): got %v, want 3", len(slots))
	}
	slots[0] = sticks(9)
	slots[2] = diamonds(1)

	// Mutating the returned slice must not reach the inventory.
	assertSlots(t, inv, diamonds(2), sticks(4), item.Stack{})

	// Two calls hand out distinct backing arrays.
	a, b := inv.Slots(), inv.Slots()
	a[1] = item.Stack{}
	assertStack(t, b[1], sticks(4))
}

func TestCoreItemsSkipsEmptyAndKeepsOrder(t *testing.T) {
	inv := New(5, nil)
	mustSet(t, inv, 0, diamonds(1))
	mustSet(t, inv, 2, sticks(2))
	mustSet(t, inv, 4, helmet())

	items := inv.Items()
	if len(items) != 3 {
		t.Fatalf("len(Items()): got %v, want 3", len(items))
	}
	for i, want := range []item.Stack{diamonds(1), sticks(2), helmet()} {
		assertStack(t, items[i], want)
	}

	// The slice returned is freshly built: mutating it changes nothing.
	items[0] = sticks(63)
	assertSlot(t, inv, 0, diamonds(1))

	// An inventory with nothing in it yields a zero-length slice.
	if got := New(3, nil).Items(); len(got) != 0 {
		t.Fatalf("Items() on an empty inventory: got %v entries, want 0", len(got))
	}
}

func TestCoreCloneCopiesSlotsIndependently(t *testing.T) {
	inv := filled(t, 3, diamonds(2), sticks(4))
	clone := inv.Clone(nil)

	if got := clone.Size(); got != 3 {
		t.Fatalf("clone Size(): got %v, want 3", got)
	}
	assertSlots(t, clone, diamonds(2), sticks(4), item.Stack{})

	mustSet(t, clone, 0, helmet())
	mustSet(t, inv, 2, diamonds(9))
	assertSlots(t, clone, helmet(), sticks(4), item.Stack{})
	assertSlots(t, inv, diamonds(2), sticks(4), diamonds(9))
}

func TestCoreCloneRoutesSlotFuncToTheNewFunc(t *testing.T) {
	orig, cloned := &recorder{}, &recorder{}
	inv := New(2, orig.fn)
	mustSet(t, inv, 0, diamonds(1))

	clone := inv.Clone(cloned.fn)
	mustSet(t, clone, 1, sticks(2))

	// A write to the clone reaches only the SlotFunc passed to Clone.
	if got := orig.count(); got != 1 {
		t.Fatalf("original SlotFunc calls: got %v, want 1", got)
	}
	calls := cloned.calling()
	if len(calls) != 1 {
		t.Fatalf("clone SlotFunc calls: got %v, want 1", len(calls))
	}
	if calls[0].slot != 1 {
		t.Fatalf("clone SlotFunc slot: got %v, want 1", calls[0].slot)
	}
	assertStack(t, calls[0].after, sticks(2))

	// And a write to the original never reaches the clone's SlotFunc.
	mustSet(t, inv, 1, diamonds(5))
	if got := cloned.count(); got != 1 {
		t.Fatalf("clone SlotFunc calls after writing to the original: got %v, want 1", got)
	}
	if got := orig.count(); got != 2 {
		t.Fatalf("original SlotFunc calls: got %v, want 2", got)
	}
}

func TestCoreCloneNilSlotFuncIsReplacedByNoOp(t *testing.T) {
	orig := &recorder{}
	inv := New(1, orig.fn)
	clone := inv.Clone(nil)

	if r := coreRecovered(func() {
		if err := clone.SetItem(0, diamonds(1)); err != nil {
			t.Errorf("SetItem: unexpected error %v", err)
		}
	}); r != nil {
		t.Fatalf("SetItem on a Clone(nil): unexpected panic %#v", r)
	}
	assertSlot(t, clone, 0, diamonds(1))
	if got := orig.count(); got != 0 {
		t.Fatalf("original SlotFunc calls: got %v, want 0", got)
	}
}

func TestCoreCloneDropsTheValidator(t *testing.T) {
	// PINNED BUG: Clone hardcodes an always-true validator, so cloning an
	// inventory silently strips whatever placement restriction it carried. Pinned
	// as-is; decide in stage 1.
	inv := New(2, nil)
	inv.SlotValidatorFunc(func(item.Stack, int) bool { return false })

	if err := inv.SetItem(0, diamonds(1)); err != nil {
		t.Fatalf("SetItem: got error %v, want nil", err)
	}
	assertSlot(t, inv, 0, item.Stack{})

	clone := inv.Clone(nil)
	mustSet(t, clone, 0, diamonds(1))
	assertSlot(t, clone, 0, diamonds(1))
}

// Clone copies the slot slice with slices.Clone, which copies item.Stack
// values. That is only a real copy as long as a Stack owns everything it holds,
// so this covers the one field that used to be shared by reference.
func TestCoreCloneDoesNotShareLore(t *testing.T) {
	lore := []string{"first line", "second line"}
	inv := New(1, nil)
	mustSet(t, inv, 0, diamonds(1).WithLore(lore...))
	clone := inv.Clone(nil)

	origStack, err := inv.Item(0)
	if err != nil {
		t.Fatalf("Item: unexpected error %v", err)
	}
	origLore := origStack.Lore()
	if len(origLore) != 2 {
		t.Fatalf("len(Lore()): got %v, want 2", len(origLore))
	}
	origLore[0] = "mutated"

	cloneStack, err := clone.Item(0)
	if err != nil {
		t.Fatalf("clone Item: unexpected error %v", err)
	}
	if got := cloneStack.Lore()[0]; got != "first line" {
		t.Fatalf("clone lore after mutating the original: got %q, want %q", got, "first line")
	}
	if lore[0] != "first line" {
		t.Fatalf("caller lore after mutating through the inventory: got %q, want %q", lore[0], "first line")
	}
}

func TestCoreCloneOfZeroValueInventory(t *testing.T) {
	// PINNED BUG: Clone never calls check and builds its slots from Slots(),
	// which does not either. Cloning a zero-value Inventory therefore hands back
	// a size-0 inventory, an invariant New refuses to produce. Pinned as-is;
	// decide in stage 1.
	var zero Inventory

	var clone *Inventory
	if r := coreRecovered(func() { clone = zero.Clone(nil) }); r != nil {
		t.Fatalf("Clone on a zero-value Inventory: unexpected panic %#v", r)
	}
	if got := clone.Size(); got != 0 {
		t.Fatalf("clone Size(): got %v, want 0", got)
	}
	if got := len(clone.Slots()); got != 0 {
		t.Fatalf("len(clone.Slots()): got %v, want 0", got)
	}
	// The clone is as unusable as its source.
	r := coreRecovered(func() { _, _ = clone.Item(0) })
	if s, ok := r.(string); !ok || s != coreUninitialisedPanic {
		t.Fatalf("clone Item(0): panicked with %#v, want %q", r, coreUninitialisedPanic)
	}
}

func TestCoreCloseKeepsContentsAndSilencesSlotFunc(t *testing.T) {
	// PINNED BUG: Close documents that it "clears any items that may currently be
	// in the inventory", but it only replaces the SlotFunc. Every slot survives.
	// Pinned as-is; decide in stage 1.
	rec := &recorder{}
	inv := New(3, rec.fn)
	mustSet(t, inv, 0, diamonds(5))
	mustSet(t, inv, 1, sticks(2))
	rec.reset()

	if err := inv.Close(); err != nil {
		t.Fatalf("Close: got error %v, want nil", err)
	}

	assertSlots(t, inv, diamonds(5), sticks(2), item.Stack{})
	if got := inv.Size(); got != 3 {
		t.Fatalf("Size() after Close: got %v, want 3", got)
	}
	if got := len(inv.Items()); got != 2 {
		t.Fatalf("len(Items()) after Close: got %v, want 2", got)
	}
	want := "(" + diamonds(5).String() + ", " + sticks(2).String() + ", " + item.Stack{}.String() + ")"
	if got := inv.String(); got != want {
		t.Fatalf("String() after Close:\ngot  %v\nwant %v", got, want)
	}

	// Writes still succeed after Close, they just no longer reach the SlotFunc
	// registered before it.
	mustSet(t, inv, 2, diamonds(1))
	assertSlot(t, inv, 2, diamonds(1))
	if got := rec.count(); got != 0 {
		t.Fatalf("SlotFunc calls after Close: got %v, want 0", got)
	}

	// Close is idempotent and always returns nil.
	for i := range 3 {
		if err := inv.Close(); err != nil {
			t.Fatalf("Close call %v: got error %v, want nil", i+2, err)
		}
	}
	assertSlots(t, inv, diamonds(5), sticks(2), diamonds(1))
}

func TestCoreSlotFuncReRegisteredAfterClose(t *testing.T) {
	before := &recorder{}
	inv := New(2, before.fn)
	mustSet(t, inv, 0, diamonds(5))
	if err := inv.Close(); err != nil {
		t.Fatalf("Close: got error %v, want nil", err)
	}

	after := &recorder{}
	inv.SlotFunc(after.fn)
	mustSet(t, inv, 0, sticks(1))

	if got := before.count(); got != 1 {
		t.Fatalf("pre-Close SlotFunc calls: got %v, want 1", got)
	}
	calls := after.calling()
	if len(calls) != 1 {
		t.Fatalf("post-Close SlotFunc calls: got %v, want 1", len(calls))
	}
	if calls[0].slot != 0 {
		t.Fatalf("post-Close SlotFunc slot: got %v, want 0", calls[0].slot)
	}
	assertStack(t, calls[0].before, diamonds(5))
	assertStack(t, calls[0].after, sticks(1))
}

func TestCoreZeroValueInventory(t *testing.T) {
	// PINNED BUG: check is applied inconsistently. Item, SetItem and Close guard
	// against a zero-value Inventory and panic, while Slots, Items, Size and
	// String happily operate on it and return values. Pinned as-is; decide in
	// stage 1.
	t.Run("panics", func(t *testing.T) {
		for _, c := range []struct {
			name string
			call func(inv *Inventory)
		}{
			{"Item", func(inv *Inventory) { _, _ = inv.Item(0) }},
			{"Item out of range", func(inv *Inventory) { _, _ = inv.Item(-1) }},
			{"SetItem", func(inv *Inventory) { _ = inv.SetItem(0, diamonds(1)) }},
			{"Close", func(inv *Inventory) { _ = inv.Close() }},
		} {
			t.Run(c.name, func(t *testing.T) {
				// A fresh zero value per case: SetItem does not unlock through a
				// defer, so a panicking call can leave the mutex held.
				inv := &Inventory{}
				r := coreRecovered(func() { c.call(inv) })
				if r == nil {
					t.Fatalf("%v on a zero-value Inventory: expected a panic, got none", c.name)
				}
				if s, ok := r.(string); !ok || s != coreUninitialisedPanic {
					t.Fatalf("%v: panicked with %#v, want %q", c.name, r, coreUninitialisedPanic)
				}
			})
		}
	})

	t.Run("survives", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			check func(t *testing.T, inv *Inventory)
		}{
			{"Size", func(t *testing.T, inv *Inventory) {
				if got := inv.Size(); got != 0 {
					t.Errorf("Size(): got %v, want 0", got)
				}
			}},
			{"Slots", func(t *testing.T, inv *Inventory) {
				if got := inv.Slots(); len(got) != 0 {
					t.Errorf("Slots(): got %v entries, want 0", len(got))
				}
			}},
			{"Items", func(t *testing.T, inv *Inventory) {
				if got := inv.Items(); len(got) != 0 {
					t.Errorf("Items(): got %v entries, want 0", len(got))
				}
			}},
			{"String", func(t *testing.T, inv *Inventory) {
				if got := inv.String(); got != "()" {
					t.Errorf("String(): got %q, want %q", got, "()")
				}
			}},
		} {
			t.Run(c.name, func(t *testing.T) {
				inv := &Inventory{}
				if r := coreRecovered(func() { c.check(t, inv) }); r != nil {
					t.Fatalf("%v on a zero-value Inventory: unexpected panic %#v", c.name, r)
				}
			})
		}
	})
}

func TestCoreString(t *testing.T) {
	empty := item.Stack{}
	stack := diamonds(3)

	for _, c := range []struct {
		name string
		inv  *Inventory
		want string
	}{
		{"size one empty", New(1, nil), "(" + empty.String() + ")"},
		{"size one filled", filled(t, 1, stack), "(" + stack.String() + ")"},
		{"size three one stack", filled(t, 3, stack), "(" + stack.String() + ", " + empty.String() + ", " + empty.String() + ")"},
		{"size two two stacks", filled(t, 2, stack, sticks(1)), "(" + stack.String() + ", " + sticks(1).String() + ")"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.inv.String(); got != c.want {
				t.Fatalf("String():\ngot  %v\nwant %v", got, c.want)
			}
		})
	}
}
