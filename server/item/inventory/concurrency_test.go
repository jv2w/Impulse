package inventory

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
)

// This file pins the concurrent behaviour of Inventory and Armour. Every test
// here runs real goroutines: -race on code that never runs concurrently proves
// nothing. The assertions are invariants that must survive the planned
// region-parallel execution model, not snapshots of a particular interleaving.
// Nothing here asserts an ordering between goroutines.

const (
	// concGoroutines is the writer/reader fan-out used throughout. It is a small
	// constant rather than GOMAXPROCS so that the work done per test stays the
	// same on every machine.
	concGoroutines = 8
	// concSlotsEach is how many slots each writer owns exclusively. Disjoint
	// ownership is what makes the final inventory contents deterministic.
	concSlotsEach = 4
	concSlots     = concGoroutines * concSlotsEach
)

// concErrs collects failures raised from inside goroutines. Calling t.Errorf
// from a goroutine is legal but the messages interleave badly and t.Fatalf from
// a non-test goroutine is not allowed at all, so failures are buffered and
// replayed on the test goroutine.
type concErrs struct {
	mu   sync.Mutex
	msgs []string
}

// addf buffers a failure message. At most 16 are kept: a broken invariant under
// contention fails thousands of times and the first few are enough.
func (e *concErrs) addf(format string, a ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.msgs) < 16 {
		e.msgs = append(e.msgs, fmt.Sprintf(format, a...))
	}
}

// report replays every buffered message as a test failure.
func (e *concErrs) report(t *testing.T) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, m := range e.msgs {
		t.Error(m)
	}
}

// concGuard runs fn on its own goroutine and waits up to the timeout for it to
// return, reporting false on timeout or panic. On false the caller must return
// at once: fn may still be running, so anything it writes is unsafe to read and
// t must no longer be touched by it. The result channel is buffered so a late
// completion never blocks a leaked goroutine.
func concGuard(t *testing.T, name string, timeout time.Duration, fn func()) bool {
	t.Helper()

	res := make(chan any, 1)
	go func() {
		defer func() { res <- recover() }()
		fn()
	}()
	select {
	case r := <-res:
		if r != nil {
			t.Errorf("%v: panicked: %v", name, r)
			return false
		}
		return true
	case <-time.After(timeout):
		t.Errorf("%v: did not return within %v; the inventory mutex is most likely held across the SlotFunc", name, timeout)
		return false
	}
}

// concReentrant returns a recorder.reentrant hook that runs f on the first
// SlotFunc invocation only. f writes the inventory, which fires the SlotFunc
// again, so depth is kept as a second and independent stop: should the
// once-semantics ever be lost, the recursion terminates instead of blowing the
// stack.
func concReentrant(f func()) func(int, item.Stack, item.Stack) {
	depth, fired := 0, false
	return func(int, item.Stack, item.Stack) {
		if depth > 0 || fired {
			return
		}
		fired = true
		depth++
		defer func() { depth-- }()
		f()
	}
}

// concValidStack reports whether a stack read out of an inventory is coherent:
// either empty, or holding between one and MaxCount items. A torn read would
// show up as a count outside that range.
func concValidStack(s item.Stack) bool {
	return s.Empty() || (s.Count() >= 1 && s.Count() <= s.MaxCount())
}

// concHeld returns the total number of items across every slot.
func concHeld(inv *Inventory) int {
	n := 0
	for _, s := range inv.Slots() {
		n += s.Count()
	}
	return n
}

// concMultiset keys the non-empty stacks passed by item type and count so that
// two slot slices can be compared irrespective of slot order.
func concMultiset(stacks []item.Stack) map[string]int {
	m := make(map[string]int, len(stacks))
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		m[fmt.Sprintf("%v x%v", itemType(s), s.Count())]++
	}
	return m
}

// concSameMultiset fails the test if got and want differ.
func concSameMultiset(t *testing.T, what string, got, want map[string]int) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%v: got %v distinct stacks, want %v (got %v, want %v)", what, len(got), len(want), got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%v: stack %q appears %v times, want %v (got %v)", what, k, got[k], v, got)
		}
	}
}

// concSlotDelta returns the net change in item count described by every
// SlotFunc invocation recorded. Because setItem produces exactly one callback
// per committed change, and the callback's after value is the stack that was
// actually stored, this sum equals the total held by an inventory that started
// out empty.
func concSlotDelta(rec *recorder) int {
	d := 0
	for _, c := range rec.calling() {
		d += c.after.Count() - c.before.Count()
	}
	return d
}

// TestConcurrentReadersAndWriters runs every read method against SetItem
// writers working on disjoint slots. Reads must never observe a slice of the
// wrong length or a stack with an impossible count, and the writers' last
// writes must all survive.
func TestConcurrentReadersAndWriters(t *testing.T) {
	inv := New(concSlots, nil)
	errs := &concErrs{}

	var wg sync.WaitGroup
	done := make(chan struct{})

	for w := range concGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			base := w * concSlotsEach
			for i := range 150 {
				for s := base; s < base+concSlotsEach; s++ {
					if err := inv.SetItem(s, diamonds(1+(i+s)%64)); err != nil {
						errs.addf("SetItem(%v): unexpected error %v", s, err)
						return
					}
				}
			}
			for s := base; s < base+concSlotsEach; s++ {
				if err := inv.SetItem(s, diamonds(w+1)); err != nil {
					errs.addf("SetItem(%v): unexpected error %v", s, err)
				}
			}
		}()
	}

	var readerWG sync.WaitGroup
	for r := range concGoroutines {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()

			for i := 0; i < 20000; i++ {
				select {
				case <-done:
					return
				default:
				}

				size := inv.Size()
				if size != concSlots {
					errs.addf("Size(): got %v, want %v", size, concSlots)
					return
				}
				if slots := inv.Slots(); len(slots) != size {
					errs.addf("len(Slots()): got %v, want Size() = %v", len(slots), size)
					return
				}
				for _, it := range inv.Items() {
					if it.Empty() || !concValidStack(it) {
						errs.addf("Items(): incoherent stack %v", it)
						return
					}
				}
				slot := (r*7 + i) % concSlots
				it, err := inv.Item(slot)
				if err != nil {
					errs.addf("Item(%v): unexpected error %v", slot, err)
					return
				}
				if !concValidStack(it) {
					errs.addf("Item(%v): incoherent stack %v", slot, it)
					return
				}
				inv.Empty()
				inv.First(diamonds(1))
				inv.FirstEmpty()
				if i%128 == 0 {
					// String walks every slot and formats it, so it is called
					// rarely: it is here for the race detector, not for load.
					_ = inv.String()
				}
			}
		}()
	}

	wg.Wait()
	close(done)
	readerWG.Wait()
	errs.report(t)

	for w := range concGoroutines {
		for s := w * concSlotsEach; s < (w+1)*concSlotsEach; s++ {
			assertSlot(t, inv, s, diamonds(w+1))
		}
	}
}

// TestConcurrentAddItemConservation pins that AddItem neither loses nor
// duplicates items under contention: the counts it reports having added always
// add up to exactly what the inventory holds. This holds because AddItem keeps
// the write lock for its whole scan-and-fill, releasing it only to run the
// callbacks it collected.
func TestConcurrentAddItemConservation(t *testing.T) {
	t.Run("room to spare", func(t *testing.T) {
		inv := New(16, nil)
		errs := &concErrs{}

		var added atomic.Int64
		var wg sync.WaitGroup
		for range concGoroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 100 {
					n, err := inv.AddItem(diamonds(1))
					if err != nil {
						errs.addf("AddItem: unexpected error %v (n = %v)", err, n)
						return
					}
					if n != 1 {
						errs.addf("AddItem: got n = %v, want 1", n)
						return
					}
					added.Add(int64(n))
				}
			}()
		}
		wg.Wait()
		errs.report(t)

		want := int64(concGoroutines * 100)
		if got := added.Load(); got != want {
			t.Fatalf("sum of AddItem counts: got %v, want %v", got, want)
		}
		if held := concHeld(inv); int64(held) != want {
			t.Fatalf("items held: got %v, want %v (the sum of every AddItem return)", held, want)
		}
	})

	t.Run("too small", func(t *testing.T) {
		// Two slots of diamonds hold 128 items; 320 are offered.
		const capacity = 2 * 64
		inv := New(2, nil)
		errs := &concErrs{}

		var added atomic.Int64
		var failures atomic.Int64
		var wg sync.WaitGroup
		for range concGoroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 40 {
					n, err := inv.AddItem(diamonds(1))
					added.Add(int64(n))
					if err == nil {
						continue
					}
					failures.Add(1)
					if n >= 1 {
						errs.addf("AddItem: got n = %v with error %v, want n < 1", n, err)
						return
					}
				}
			}()
		}
		wg.Wait()
		errs.report(t)

		if got := added.Load(); got != capacity {
			t.Fatalf("sum of AddItem counts: got %v, want %v", got, capacity)
		}
		if held := concHeld(inv); held != capacity {
			t.Fatalf("items held: got %v, want %v", held, capacity)
		}
		if want := int64(concGoroutines*40 - capacity); failures.Load() != want {
			t.Fatalf("failed AddItem calls: got %v, want %v", failures.Load(), want)
		}
	})
}

// TestConcurrentAddAndRemoveConservation runs adders against removers and pins
// that nothing leaks in either direction. Removal uses n = 1, which is the only
// count whose outcome is knowable from the error alone: RemoveItemFunc(1, ...)
// either finds a comparable slot and takes exactly one item, or finds none and
// takes nothing. The recorded before/after pairs give a second, independent
// accounting of the same total.
func TestConcurrentAddAndRemoveConservation(t *testing.T) {
	rec := &recorder{}
	inv := New(32, rec.fn)
	errs := &concErrs{}

	var added, removed atomic.Int64
	var wg sync.WaitGroup

	for range concGoroutines / 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 250 {
				n, err := inv.AddItem(diamonds(1))
				if err != nil {
					errs.addf("AddItem: unexpected error %v (n = %v); the inventory should never fill up", err, n)
					return
				}
				added.Add(int64(n))
			}
		}()
	}
	for range concGoroutines / 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := diamonds(1)
			for range 250 {
				if err := inv.RemoveItemFunc(1, want.Comparable); err == nil {
					removed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	errs.report(t)

	held := int64(concHeld(inv))
	if got := added.Load() - removed.Load(); got != held {
		t.Fatalf("added - removed = %v, but the inventory holds %v", got, held)
	}
	if got := int64(concSlotDelta(rec)); got != held {
		t.Fatalf("sum of recorded slot deltas = %v, but the inventory holds %v", got, held)
	}
	for slot, s := range inv.Slots() {
		if !concValidStack(s) {
			t.Fatalf("slot %v holds an incoherent stack %v", slot, s)
		}
	}
}

// TestConcurrentSwapPreservesMultiset pins that Swap is atomic across both of
// its slots: it performs both setItem calls while still holding the write lock
// and only releases it to run the two callbacks, so no concurrent reader or
// swapper can observe or act on a half-finished exchange. The multiset of
// non-empty stacks is therefore invariant.
func TestConcurrentSwapPreservesMultiset(t *testing.T) {
	fill := func() *Inventory {
		inv := New(16, nil)
		for i := range 6 {
			mustSet(t, inv, i, diamonds(i+1))
		}
		for i := range 6 {
			mustSet(t, inv, 6+i, sticks(i+1))
		}
		return inv
	}

	// A single-goroutine run first: if Swap lost items on its own, a failure
	// under contention would say nothing about concurrency.
	seq := fill()
	want := concMultiset(seq.Slots())
	r := rand.New(rand.NewPCG(1, 2))
	for range 1000 {
		if err := seq.Swap(r.IntN(16), r.IntN(16)); err != nil {
			t.Fatalf("Swap: unexpected error %v", err)
		}
	}
	concSameMultiset(t, "sequential swaps", concMultiset(seq.Slots()), want)

	inv := fill()
	errs := &concErrs{}

	var wg sync.WaitGroup
	for g := range concGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			r := rand.New(rand.NewPCG(uint64(g)+1, 2))
			for range 300 {
				a, b := r.IntN(16), r.IntN(16)
				if err := inv.Swap(a, b); err != nil {
					errs.addf("Swap(%v, %v): unexpected error %v", a, b, err)
					return
				}
				if slots := inv.Slots(); len(slots) != 16 {
					errs.addf("len(Slots()): got %v, want 16", len(slots))
					return
				}
			}
		}()
	}
	wg.Wait()
	errs.report(t)

	concSameMultiset(t, "concurrent swaps", concMultiset(inv.Slots()), want)
}

// TestReentrantSlotFuncDoesNotDeadlock pins that a SlotFunc may call back into
// the same inventory. SetItem and Swap release the lock before invoking the
// callbacks they built; AddItem, RemoveItemFunc and Clear collect theirs with
// defer and unlock explicitly before returning, so those run unlocked too. Each
// case also pins the resulting state, the order the callbacks arrive in and the
// before/after pairs they report.
func TestReentrantSlotFuncDoesNotDeadlock(t *testing.T) {
	t.Run("SetItem", func(t *testing.T) {
		rec := &recorder{}
		inv := New(4, rec.fn)

		var sawCommitted item.Stack
		var sawSize, sawSlots int
		rec.reentrant = concReentrant(func() {
			sawCommitted, _ = inv.Item(0)
			sawSize = inv.Size()
			sawSlots = len(inv.Slots())
			_ = inv.SetItem(1, sticks(3))
		})

		if !concGuard(t, "SetItem with a reentrant SlotFunc", 2*time.Second, func() {
			_ = inv.SetItem(0, diamonds(5))
		}) {
			return
		}

		// The write is already committed when the callback runs: SetItem stores
		// the stack under the lock and only then releases it and calls f.
		assertStack(t, sawCommitted, diamonds(5))
		if sawSize != 4 || sawSlots != 4 {
			t.Fatalf("callback observed Size() = %v and len(Slots()) = %v, want 4 and 4", sawSize, sawSlots)
		}
		assertSlots(t, inv, diamonds(5), sticks(3), item.Stack{}, item.Stack{})

		// The reentrant write fires the SlotFunc again, nested inside the first
		// invocation.
		if got, want := rec.slots(), []int{0, 1}; !slices.Equal(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
		calls := rec.calling()
		assertStack(t, calls[1].before, item.Stack{})
		assertStack(t, calls[1].after, sticks(3))
	})

	t.Run("Swap", func(t *testing.T) {
		rec := &recorder{}
		inv := New(4, nil)
		mustSet(t, inv, 0, diamonds(2))
		mustSet(t, inv, 1, sticks(3))
		inv.SlotFunc(rec.fn)

		rec.reentrant = concReentrant(func() {
			_ = inv.SetItem(1, diamonds(7))
		})

		if !concGuard(t, "Swap with a reentrant SlotFunc", 2*time.Second, func() {
			if err := inv.Swap(0, 1); err != nil {
				panic(err)
			}
		}) {
			return
		}

		assertSlots(t, inv, sticks(3), diamonds(7), item.Stack{}, item.Stack{})

		calls := rec.calling()
		if len(calls) != 3 {
			t.Fatalf("SlotFunc calls: got %v, want 3 (%v)", len(calls), rec.slots())
		}
		assertStack(t, calls[0].before, diamonds(2))
		assertStack(t, calls[0].after, sticks(3))
		assertStack(t, calls[1].before, diamonds(2))
		assertStack(t, calls[1].after, diamonds(7))

		// PINNED BUG: Swap commits both slots under the lock but reports them
		// afterwards, so the second callback still announces the stack Swap put
		// there even though the first callback has already replaced it. Slot 1
		// holds diamonds(7) at this point, not the sticks(3) announced here. Any
		// listener rebuilding state from before/after pairs ends up wrong.
		assertStack(t, calls[2].before, sticks(3))
		assertStack(t, calls[2].after, diamonds(2))
	})

	t.Run("AddItem", func(t *testing.T) {
		rec := &recorder{}
		inv := New(4, nil)
		mustSet(t, inv, 0, diamonds(60))
		mustSet(t, inv, 1, diamonds(60))
		inv.SlotFunc(rec.fn)

		rec.reentrant = concReentrant(func() {
			_ = inv.SetItem(3, sticks(1))
		})

		var n int
		var err error
		if !concGuard(t, "AddItem with a reentrant SlotFunc", 2*time.Second, func() {
			n, err = inv.AddItem(diamonds(10))
		}) {
			return
		}

		if n != 10 || err != nil {
			t.Fatalf("AddItem: got (%v, %v), want (10, nil)", n, err)
		}
		assertSlots(t, inv, diamonds(64), diamonds(64), diamonds(2), sticks(1))

		// AddItem collects its callbacks with defer, so they arrive in reverse
		// order of the writes: the empty slot filled last is reported first.
		if got, want := rec.slots(), []int{2, 3, 1, 0}; !slices.Equal(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})

	t.Run("RemoveItemFunc", func(t *testing.T) {
		rec := &recorder{}
		inv := New(4, nil)
		for i := range 3 {
			mustSet(t, inv, i, diamonds(5))
		}
		inv.SlotFunc(rec.fn)

		rec.reentrant = concReentrant(func() {
			_ = inv.SetItem(3, sticks(1))
		})

		var err error
		if !concGuard(t, "RemoveItemFunc with a reentrant SlotFunc", 2*time.Second, func() {
			err = inv.RemoveItemFunc(12, diamonds(1).Comparable)
		}) {
			return
		}

		if err != nil {
			t.Fatalf("RemoveItemFunc: unexpected error %v", err)
		}
		assertSlots(t, inv, item.Stack{}, item.Stack{}, diamonds(3), sticks(1))
		if got, want := rec.slots(), []int{2, 3, 1, 0}; !slices.Equal(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		rec := &recorder{}
		inv := New(4, nil)
		mustSet(t, inv, 0, diamonds(1))
		mustSet(t, inv, 1, sticks(2))
		mustSet(t, inv, 3, diamonds(3))
		inv.SlotFunc(rec.fn)

		rec.reentrant = concReentrant(func() {
			_ = inv.SetItem(3, diamonds(9))
		})

		var items []item.Stack
		if !concGuard(t, "Clear with a reentrant SlotFunc", 2*time.Second, func() {
			items = inv.Clear()
		}) {
			return
		}

		if len(items) != 3 {
			t.Fatalf("Clear returned %v stacks, want 3", len(items))
		}
		concSameMultiset(t, "stacks returned by Clear", concMultiset(items), concMultiset([]item.Stack{diamonds(1), sticks(2), diamonds(3)}))

		// PINNED BUG: Clear reports every stack it took, then runs the callbacks
		// with the lock released. A SlotFunc that writes during those callbacks
		// wins, so Clear hands the caller diamonds(3) for slot 3 while slot 3
		// actually holds diamonds(9) and the inventory is not empty afterwards.
		assertSlots(t, inv, item.Stack{}, item.Stack{}, item.Stack{}, diamonds(9))
		if inv.Empty() {
			t.Fatal("inventory is empty after Clear, but the reentrant write should have survived")
		}
		if got, want := rec.slots(), []int{3, 3, 1, 0}; !slices.Equal(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})
}

// TestReentrantSlotFuncObservesReleasedLock is the load-bearing pin behind
// setItem returning a func() instead of calling inv.f inline: every mutating
// entry point invokes the SlotFunc with the mutex released. The callback takes
// the read lock via Size and the write lock via SetItem, so a redesign that
// inlines the callback deadlocks here and is reported as a failure instead of
// hanging the suite.
func TestReentrantSlotFuncObservesReleasedLock(t *testing.T) {
	type concCase struct {
		name string
		run  func(inv *Inventory)
	}
	cases := []concCase{
		{"SetItem", func(inv *Inventory) { _ = inv.SetItem(0, diamonds(4)) }},
		{"Swap", func(inv *Inventory) { _ = inv.Swap(0, 2) }},
		{"AddItem", func(inv *Inventory) { _, _ = inv.AddItem(diamonds(4)) }},
		{"RemoveItemFunc", func(inv *Inventory) { _ = inv.RemoveItemFunc(1, diamonds(1).Comparable) }},
		{"Clear", func(inv *Inventory) { _ = inv.Clear() }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			inv := New(4, nil)
			mustSet(t, inv, 0, diamonds(6))
			inv.SlotFunc(rec.fn)

			var sawSize int
			var wroteErr error
			var wrote bool
			rec.reentrant = concReentrant(func() {
				sawSize = inv.Size()
				wroteErr = inv.SetItem(1, sticks(2))
				wrote = true
			})

			if !concGuard(t, c.name+" with a locking SlotFunc", 2*time.Second, func() {
				c.run(inv)
			}) {
				return
			}

			if rec.count() == 0 {
				t.Fatalf("%v did not call the SlotFunc at all", c.name)
			}
			if !wrote {
				t.Fatalf("%v: the SlotFunc never completed its reentrant write", c.name)
			}
			if wroteErr != nil {
				t.Fatalf("%v: reentrant SetItem returned %v", c.name, wroteErr)
			}
			if sawSize != 4 {
				t.Fatalf("%v: the SlotFunc observed Size() = %v, want 4", c.name, sawSize)
			}
			assertSlot(t, inv, 1, sticks(2))
		})
	}
}

// TestConcurrentValidatorPanicReleasesMutex pins that a panic escaping the slot
// validator propagates to the caller without stranding the inventory mutex.
// Every mutating entry point holds the write lock while it consults the
// validator, so a validator that panics is the cheapest way to unwind out of
// the locked section of each of them. A caller that recovers the panic must be
// left with an inventory that still works.
func TestConcurrentValidatorPanicReleasesMutex(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(inv *Inventory)
	}{
		{"SetItem", func(inv *Inventory) { _ = inv.SetItem(0, diamonds(1)) }},
		{"Swap", func(inv *Inventory) { _ = inv.Swap(0, 1) }},
		{"AddItem", func(inv *Inventory) { _, _ = inv.AddItem(diamonds(1)) }},
		{"RemoveItemFunc", func(inv *Inventory) { _ = inv.RemoveItemFunc(1, diamonds(1).Comparable) }},
		{"Clear", func(inv *Inventory) { _ = inv.Clear() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv := New(4, nil)
			mustSet(t, inv, 0, diamonds(6))
			inv.SlotValidatorFunc(func(item.Stack, int) bool { panic("validator blew up") })

			panicked := make(chan any, 1)
			go func() {
				defer func() { panicked <- recover() }()
				c.run(inv)
			}()
			select {
			case r := <-panicked:
				if r == nil {
					t.Fatalf("%v did not propagate the validator panic", c.name)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%v never returned", c.name)
			}

			reachable := make(chan struct{})
			go func() {
				inv.Size()
				close(reachable)
			}()
			select {
			case <-reachable:
			case <-time.After(2 * time.Second):
				t.Fatalf("the inventory mutex is still held after %v propagated the validator panic", c.name)
			}
		})
	}
}

// TestConcurrentCheckPanicReleasesMutex pins the same contract for the panic
// raised by check on an inventory that was not built by New: it escapes to the
// caller with the mutex released, so the panic stays recoverable.
func TestConcurrentCheckPanicReleasesMutex(t *testing.T) {
	inv := &Inventory{}

	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_ = inv.SetItem(0, diamonds(1))
	}()
	select {
	case r := <-panicked:
		if r == nil {
			t.Fatal("SetItem on a zero-value inventory did not panic")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SetItem never returned")
	}

	reachable := make(chan struct{})
	go func() {
		inv.Size()
		close(reachable)
	}()
	select {
	case <-reachable:
	case <-time.After(2 * time.Second):
		t.Fatal("the inventory mutex is still held after check panicked")
	}
}

// TestConcurrentValidatorSwapping pins that replacing the slot validator while
// writes are in flight is safe: setItem reads inv.validator under the same write
// lock that SlotValidatorFunc takes to replace it. Nil is never installed here
// because SlotValidatorFunc stores it unguarded and the next write panics.
func TestConcurrentValidatorSwapping(t *testing.T) {
	inv := New(concSlots, nil)
	errs := &concErrs{}

	// Both validators accept every diamond stack, so the writers' last writes
	// are still deterministic whichever one is installed at the time.
	allow := func(item.Stack, int) bool { return true }
	noSticks := func(s item.Stack, _ int) bool {
		_, ok := s.Item().(item.Stick)
		return !ok
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	for w := range concGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			base := w * concSlotsEach
			for i := range 100 {
				for s := base; s < base+concSlotsEach; s++ {
					if err := inv.SetItem(s, diamonds(1+(i+s)%64)); err != nil {
						errs.addf("SetItem(%v): unexpected error %v", s, err)
						return
					}
				}
			}
			for s := base; s < base+concSlotsEach; s++ {
				if err := inv.SetItem(s, diamonds(w+1)); err != nil {
					errs.addf("SetItem(%v): unexpected error %v", s, err)
				}
			}
		}()
	}

	var swapWG sync.WaitGroup
	for g := range 4 {
		swapWG.Add(1)
		go func() {
			defer swapWG.Done()
			for i := 0; i < 200; i++ {
				select {
				case <-done:
					return
				default:
				}
				if (i+g)%2 == 0 {
					inv.SlotValidatorFunc(allow)
				} else {
					inv.SlotValidatorFunc(noSticks)
				}
			}
		}()
	}

	wg.Wait()
	close(done)
	swapWG.Wait()
	errs.report(t)

	inv.SlotValidatorFunc(allow)
	for w := range concGoroutines {
		for s := w * concSlotsEach; s < (w+1)*concSlotsEach; s++ {
			assertSlot(t, inv, s, diamonds(w+1))
		}
	}
}

// TestConcurrentSlotFuncSwapping pins that replacing the SlotFunc is safe as
// long as no write is in flight. SlotFunc is swapped here while readers hammer
// the inventory, then writes are driven against whichever function ended up
// installed.
//
// PINNED BUG: SlotFunc and Close assign inv.f under the write lock, but the
// closure setItem returns reads inv.f after the lock has been released. Swapping
// the SlotFunc concurrently with a write is therefore a genuine data race on
// that field, which is why the writers below are kept out of the swapping phase.
func TestConcurrentSlotFuncSwapping(t *testing.T) {
	rec1, rec2 := &recorder{}, &recorder{}
	inv := New(concSlots, rec1.fn)
	errs := &concErrs{}

	var wg sync.WaitGroup
	done := make(chan struct{})

	for r := range concGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				select {
				case <-done:
					return
				default:
				}
				if slots := inv.Slots(); len(slots) != concSlots {
					errs.addf("len(Slots()): got %v, want %v", len(slots), concSlots)
					return
				}
				slot := (r*5 + i) % concSlots
				if it, err := inv.Item(slot); err != nil || !concValidStack(it) {
					errs.addf("Item(%v): got (%v, %v)", slot, it, err)
					return
				}
				inv.Empty()
			}
		}()
	}

	var swapWG sync.WaitGroup
	for g := range 4 {
		swapWG.Add(1)
		go func() {
			defer swapWG.Done()
			for i := range 200 {
				if (i+g)%2 == 0 {
					inv.SlotFunc(rec1.fn)
				} else {
					inv.SlotFunc(rec2.fn)
				}
			}
		}()
	}
	swapWG.Wait()
	close(done)
	wg.Wait()
	errs.report(t)

	// Whichever function was installed last, the inventory still writes and
	// reads back consistently, and exactly one of the two recorders sees it.
	before1, before2 := rec1.count(), rec2.count()
	for w := range concGoroutines {
		for s := w * concSlotsEach; s < (w+1)*concSlotsEach; s++ {
			mustSet(t, inv, s, diamonds(w+1))
		}
	}
	for w := range concGoroutines {
		for s := w * concSlotsEach; s < (w+1)*concSlotsEach; s++ {
			assertSlot(t, inv, s, diamonds(w+1))
		}
	}
	if got := (rec1.count() - before1) + (rec2.count() - before2); got != concSlots {
		t.Fatalf("SlotFunc invocations across both recorders: got %v, want %v", got, concSlots)
	}
}

// TestConcurrentMerge pins that Merge produces a coherent snapshot while both
// source inventories are being written. The two inventories are always distinct:
// Merge takes a read lock on each, so merging an inventory with itself takes the
// same RWMutex twice and deadlocks as soon as a writer is queued between them.
func TestConcurrentMerge(t *testing.T) {
	a, b := New(8, nil), New(4, nil)
	errs := &concErrs{}

	var wg sync.WaitGroup
	done := make(chan struct{})

	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				if err := a.SetItem(w*2, diamonds(1+(i+w)%64)); err != nil {
					errs.addf("a.SetItem: unexpected error %v", err)
					return
				}
				if err := a.SetItem(w*2+1, diamonds(1+i%64)); err != nil {
					errs.addf("a.SetItem: unexpected error %v", err)
					return
				}
				if err := b.SetItem(w, sticks(1+i%64)); err != nil {
					errs.addf("b.SetItem: unexpected error %v", err)
					return
				}
			}
		}()
	}

	var mergeWG sync.WaitGroup
	for range 2 {
		mergeWG.Add(1)
		go func() {
			defer mergeWG.Done()
			for i := 0; i < 2000; i++ {
				select {
				case <-done:
					return
				default:
				}
				m := a.Merge(b, nil)
				if m.Size() != 12 {
					errs.addf("Merge: Size() = %v, want 12", m.Size())
					return
				}
				slots := m.Slots()
				if len(slots) != 12 {
					errs.addf("Merge: len(Slots()) = %v, want 12", len(slots))
					return
				}
				for slot, s := range slots {
					if !concValidStack(s) {
						errs.addf("Merge: slot %v holds an incoherent stack %v", slot, s)
						return
					}
					if s.Empty() {
						continue
					}
					// The first eight slots come from a, the last four from b.
					if _, ok := s.Item().(item.Diamond); ok != (slot < 8) {
						errs.addf("Merge: slot %v holds %v, which came from the wrong inventory", slot, s)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
	close(done)
	mergeWG.Wait()
	errs.report(t)

	// A quiescent merge copies rather than aliases: neither source is reachable
	// through the result.
	m := a.Merge(b, nil)
	mustSet(t, m, 0, diamonds(7))
	mustSet(t, m, 8, diamonds(7))
	mustSet(t, a, 0, diamonds(11))
	mustSet(t, b, 0, sticks(11))
	assertSlot(t, m, 0, diamonds(7))
	assertSlot(t, m, 8, diamonds(7))
	assertSlot(t, a, 0, diamonds(11))
	assertSlot(t, b, 0, sticks(11))
}

// TestConcurrentArmour pins the validator promise of Armour under contention:
// whatever the interleaving, every occupied slot holds a piece that canAddArmour
// accepts for that slot, and the inventory always reports exactly four slots.
func TestConcurrentArmour(t *testing.T) {
	a := NewArmour(nil)
	errs := &concErrs{}

	check := func(slots []item.Stack) bool {
		if len(slots) != 4 {
			errs.addf("Slots(): got %v entries, want 4", len(slots))
			return false
		}
		for slot, s := range slots {
			if !concValidStack(s) {
				errs.addf("slot %v holds an incoherent stack %v", slot, s)
				return false
			}
			if !canAddArmour(s, slot) {
				errs.addf("slot %v holds %v, which canAddArmour rejects for that slot", slot, s)
				return false
			}
		}
		return true
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				switch (i + w) % 4 {
				case 0:
					a.SetHelmet(helmet())
				case 1:
					// Rejected by the validator: boots are not a helmet.
					a.SetHelmet(boots())
				case 2:
					a.SetBoots(boots())
				case 3:
					a.SetBoots(item.Stack{})
				}
			}
		}()
	}

	var readWG sync.WaitGroup
	src := testSource{reduced: true}
	for range 4 {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			for i := 0; i < 2000; i++ {
				select {
				case <-done:
					return
				default:
				}
				if !check(a.Slots()) {
					return
				}
				for _, it := range a.Items() {
					if it.Empty() {
						errs.addf("Items() returned an empty stack")
						return
					}
				}
				if r := a.DamageReduction(10, src); r < 0 || r > 10 {
					errs.addf("DamageReduction(10): got %v, want 0 <= r <= 10", r)
					return
				}
				if lvl := a.HighestEnchantmentLevel(enchantment.Thorns); lvl != 0 {
					errs.addf("HighestEnchantmentLevel(Thorns): got %v, want 0", lvl)
					return
				}
				if i%128 == 0 {
					_ = a.String()
				}
			}
		}()
	}

	wg.Wait()
	close(done)
	readWG.Wait()
	errs.report(t)

	check(a.Slots())
	errs.report(t)

	// The rejected helmet writes never landed, and the inventory still works.
	a.Set(helmet(), chestplate(), leggings(), boots())
	assertSlots(t, a.Inventory(), helmet(), chestplate(), leggings(), boots())
}

// TestConcurrentClose pins what Close does to a busy inventory.
//
// PINNED BUG: Close assigns inv.f under the write lock, but the closure setItem
// returns reads inv.f after the lock has been released, so Close running
// alongside any call that actually changes a slot is an unsynchronised
// read/write on that field. This was confirmed with -race and reproduces every
// run; it is the same defect as the one noted on TestConcurrentSlotFuncSwapping.
// The writers below therefore finish before the closers start, and the calls
// kept running against Close are the ones that take a lock without ever
// reaching setItem: a read-locking Slots, a write-locking ContainsItemFunc, a
// RemoveItemFunc that matches nothing and an out-of-range SetItem.
func TestConcurrentClose(t *testing.T) {
	rec := &recorder{}
	inv := New(concSlots, rec.fn)
	errs := &concErrs{}

	var wg sync.WaitGroup
	for w := range concGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := w * concSlotsEach
			for i := range 100 {
				for s := base; s < base+concSlotsEach; s++ {
					if err := inv.SetItem(s, diamonds(1+(i+s)%64)); err != nil {
						errs.addf("SetItem(%v): unexpected error %v", s, err)
						return
					}
				}
			}
			for s := base; s < base+concSlotsEach; s++ {
				if err := inv.SetItem(s, diamonds(w+1)); err != nil {
					errs.addf("SetItem(%v): unexpected error %v", s, err)
				}
			}
		}()
	}
	wg.Wait()
	errs.report(t)

	var closeWG, busyWG sync.WaitGroup
	done := make(chan struct{})
	for range concGoroutines {
		busyWG.Add(1)
		go func() {
			defer busyWG.Done()
			for i := 0; i < 2000; i++ {
				select {
				case <-done:
					return
				default:
				}
				if slots := inv.Slots(); len(slots) != concSlots {
					errs.addf("len(Slots()): got %v, want %v", len(slots), concSlots)
					return
				}
				if !inv.ContainsItem(diamonds(1)) {
					errs.addf("ContainsItem(diamonds(1)): got false, want true")
					return
				}
				// No slot holds sticks, so this scans under the write lock and
				// leaves every slot alone.
				if err := inv.RemoveItemFunc(1, sticks(1).Comparable); err == nil {
					errs.addf("RemoveItemFunc: got nil, want an error; the inventory holds no sticks")
					return
				}
				if err := inv.SetItem(concSlots, diamonds(1)); err == nil {
					errs.addf("SetItem(%v): got nil, want ErrSlotOutOfRange", concSlots)
					return
				}
			}
		}()
	}
	for range 2 {
		closeWG.Add(1)
		go func() {
			defer closeWG.Done()
			for range 50 {
				if err := inv.Close(); err != nil {
					errs.addf("Close: got error %v, want nil", err)
					return
				}
			}
		}()
	}

	closeWG.Wait()
	close(done)
	busyWG.Wait()
	errs.report(t)

	// PINNED BUG: Close swaps the SlotFunc for a no-op and nothing else, despite
	// documenting that it "also clears any items that may currently be in the
	// inventory". The inventory keeps its contents and stays fully readable and
	// writable, so a closed inventory is indistinguishable from an open one
	// except that nobody is told about changes any more.
	if inv.Empty() {
		t.Fatal("inventory is empty after Close, but Close does not clear items")
	}
	for w := range concGoroutines {
		for s := w * concSlotsEach; s < (w+1)*concSlotsEach; s++ {
			assertSlot(t, inv, s, diamonds(w+1))
		}
	}

	rec.reset()
	mustSet(t, inv, 0, sticks(9))
	assertSlot(t, inv, 0, sticks(9))
	if n := rec.count(); n != 0 {
		t.Fatalf("SlotFunc invocations after Close: got %v, want 0", n)
	}
	if _, err := inv.AddItem(diamonds(1)); err != nil {
		t.Fatalf("AddItem after Close: unexpected error %v", err)
	}
	if err := inv.Close(); err != nil {
		t.Fatalf("second Close: got error %v, want nil", err)
	}
}
