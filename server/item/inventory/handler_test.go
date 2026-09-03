package inventory

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jv2w/Impulse/server/event"
	"github.com/jv2w/Impulse/server/item"
)

// This file pins the behaviour of handler.go together with (*Inventory).Handle
// and (*Inventory).Handler. It records what the code does today, including
// where that looks wrong; every such spot carries a PINNED BUG marker.

// hdlHolder is a comparable stand-in for the player that normally ends up in a
// Context. Holder is an empty interface, so any type works.
type hdlHolder struct {
	name string
}

// hdlCase is one named call used by the table-driven panic tests.
type hdlCase struct {
	name string
	call func(inv *Inventory)
}

// hdlRecoverValue runs f and returns the value it panicked with, failing the
// test if f returns normally instead.
func hdlRecoverValue(t *testing.T, f func()) any {
	t.Helper()

	var (
		v        any
		panicked bool
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				v, panicked = r, true
			}
		}()
		f()
	}()
	if !panicked {
		t.Fatalf("expected a panic, but the call returned normally")
	}
	return v
}

// hdlAssertNop fails the test if h is not the NopHandler value.
func hdlAssertNop(t *testing.T, h Handler, ctx string) {
	t.Helper()

	if h == nil {
		t.Fatalf("%v: got a nil Handler, want inventory.NopHandler{}", ctx)
	}
	if _, ok := h.(NopHandler); !ok {
		t.Fatalf("%v: got %T, want inventory.NopHandler", ctx, h)
	}
}

func TestHdlDefaultHandlerIsNopHandler(t *testing.T) {
	hdlAssertNop(t, New(3, nil).Handler(), "New(3, nil).Handler()")
	hdlAssertNop(t, New(1, func(int, item.Stack, item.Stack) {}).Handler(), "New with SlotFunc: Handler()")
	hdlAssertNop(t, NewArmour(nil).Inventory().Handler(), "NewArmour(nil).Inventory().Handler()")

	// Clone installs a fresh NopHandler: the Handler of the inventory cloned is
	// deliberately not carried over.
	src := New(3, nil)
	src.Handle(&recordingHandler{})
	hdlAssertNop(t, src.Clone(nil).Handler(), "Clone(nil).Handler()")
}

func TestHdlHandleRoundTripsAndReplaces(t *testing.T) {
	inv := New(3, nil)

	first := &recordingHandler{}
	inv.Handle(first)
	got, ok := inv.Handler().(*recordingHandler)
	if !ok {
		t.Fatalf("Handler() after Handle(first): got %T, want *recordingHandler", inv.Handler())
	}
	if got != first {
		t.Fatalf("Handler() returned %p, want the assigned handler %p", got, first)
	}

	second := &recordingHandler{}
	inv.Handle(second)
	got, ok = inv.Handler().(*recordingHandler)
	if !ok {
		t.Fatalf("Handler() after Handle(second): got %T, want *recordingHandler", inv.Handler())
	}
	if got != second {
		t.Fatalf("Handler() returned %p, want the second handler %p", got, second)
	}
	if got == first {
		t.Fatalf("Handler() still returns the first handler %p: Handle did not replace it", first)
	}

	// A value handler round-trips as a value, not as a pointer to one.
	inv.Handle(NopHandler{})
	hdlAssertNop(t, inv.Handler(), "Handler() after Handle(NopHandler{})")

	// Armour.Handle delegates straight to the underlying Inventory.
	a := NewArmour(nil)
	a.Handle(first)
	if h, ok := a.Inventory().Handler().(*recordingHandler); !ok || h != first {
		t.Fatalf("Armour.Inventory().Handler(): got %v (%T), want %p", h, a.Inventory().Handler(), first)
	}
}

func TestHdlHandleNilInstallsNopHandler(t *testing.T) {
	inv := New(3, nil)

	// Handle(nil) on a fresh inventory neither panics nor stores a nil Handler.
	inv.Handle(nil)
	hdlAssertNop(t, inv.Handler(), "Handler() after Handle(nil)")

	// Handle(nil) after a real handler removes it.
	rec := &recordingHandler{}
	inv.Handle(rec)
	if _, ok := inv.Handler().(*recordingHandler); !ok {
		t.Fatalf("Handler() after Handle(rec): got %T, want *recordingHandler", inv.Handler())
	}
	inv.Handle(nil)
	if h, ok := inv.Handler().(*recordingHandler); ok {
		t.Fatalf("Handler() after Handle(nil): got %p, want the handler to be removed", h)
	}
	hdlAssertNop(t, inv.Handler(), "Handler() after Handle(rec) then Handle(nil)")

	// PINNED BUG: only an untyped nil is replaced by NopHandler{}. A typed nil
	// handler is a non-nil interface, so it is stored as-is and handed back to
	// every caller outside the package, which will dereference it.
	var typed *recordingHandler
	inv.Handle(typed)
	h := inv.Handler()
	if h == nil {
		t.Fatalf("Handler() after Handle((*recordingHandler)(nil)): got a nil interface, want the typed nil stored")
	}
	stored, ok := h.(*recordingHandler)
	if !ok {
		t.Fatalf("Handler() after Handle((*recordingHandler)(nil)): got %T, want *recordingHandler", h)
	}
	if stored != nil {
		t.Fatalf("Handler(): got %p, want the nil pointer that was assigned", stored)
	}
}

func TestHdlZeroValueInventoryPanics(t *testing.T) {
	// PINNED BUG: check() panics on a zero-value Inventory instead of returning
	// an error, so third-party code that forgets New() kills the calling
	// goroutine.
	cases := []hdlCase{
		{name: "Handler", call: func(inv *Inventory) { _ = inv.Handler() }},
		{name: "Handle", call: func(inv *Inventory) { inv.Handle(&recordingHandler{}) }},
		{name: "HandleNil", call: func(inv *Inventory) { inv.Handle(nil) }},
		{name: "HandleNopHandler", call: func(inv *Inventory) { inv.Handle(NopHandler{}) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &Inventory{}
			v := hdlRecoverValue(t, func() { c.call(inv) })

			s, ok := v.(string)
			if !ok {
				t.Fatalf("panic value: got %T (%v), want a string", v, v)
			}
			if !strings.Contains(s, "uninitialised inventory") {
				t.Fatalf("panic message: got %q, want it to mention an uninitialised inventory", s)
			}
		})
	}

	// The panic leaves the mutex released: a second call panics the same way
	// rather than deadlocking.
	inv := &Inventory{}
	_ = hdlRecoverValue(t, func() { _ = inv.Handler() })
	_ = hdlRecoverValue(t, func() { _ = inv.Handler() })
}

func TestHdlInventoryNeverInvokesHandler(t *testing.T) {
	// PINNED BUG: no method on Inventory ever invokes the assigned Handler, so a
	// Handler that cancels its Context cannot block any inventory mutation.
	//
	// Every real dispatch lives outside this package (server/player and
	// server/session fetch inv.Handler().HandleX and call it themselves). If a
	// redesign starts dispatching from inside the package, this test fails.
	rec := &recordingHandler{cancel: true}

	inv := New(5, nil)
	inv.Handle(rec)

	mustSet(t, inv, 0, diamonds(10))
	mustSet(t, inv, 1, sticks(4))
	if err := inv.SetItem(99, diamonds(1)); !errors.Is(err, ErrSlotOutOfRange) {
		t.Fatalf("SetItem(99): got error %v, want ErrSlotOutOfRange", err)
	}
	if n, err := inv.AddItem(diamonds(5)); err != nil || n != 5 {
		t.Fatalf("AddItem(diamonds(5)): got (%v, %v), want (5, nil)", n, err)
	}
	if err := inv.RemoveItem(diamonds(3)); err != nil {
		t.Fatalf("RemoveItem(diamonds(3)): unexpected error %v", err)
	}
	if err := inv.RemoveItemFunc(1, sticks(1).Comparable); err != nil {
		t.Fatalf("RemoveItemFunc: unexpected error %v", err)
	}
	if err := inv.Swap(0, 2); err != nil {
		t.Fatalf("Swap(0, 2): unexpected error %v", err)
	}
	if err := inv.Swap(0, 99); !errors.Is(err, ErrSlotOutOfRange) {
		t.Fatalf("Swap(0, 99): got error %v, want ErrSlotOutOfRange", err)
	}
	if _, err := inv.Item(0); err != nil {
		t.Fatalf("Item(0): unexpected error %v", err)
	}
	if _, err := inv.Item(99); !errors.Is(err, ErrSlotOutOfRange) {
		t.Fatalf("Item(99): got error %v, want ErrSlotOutOfRange", err)
	}
	_ = inv.Slots()
	_ = inv.Items()
	_ = inv.Empty()
	_, _ = inv.First(diamonds(1))
	_, _ = inv.FirstFunc(sticks(1).Comparable)
	_, _ = inv.FirstEmpty()
	_ = inv.ContainsItem(diamonds(1))
	_ = inv.ContainsItemFunc(1, diamonds(1).Comparable)
	_ = inv.Merge(New(2, nil), nil)
	_ = inv.String()
	_ = inv.Size()
	_ = inv.Clear()
	if err := inv.Close(); err != nil {
		t.Fatalf("Close(): got error %v, want nil", err)
	}

	a := NewArmour(nil)
	a.Handle(rec)
	a.Set(helmet(), chestplate(), leggings(), boots())
	a.SetHelmet(item.Stack{})
	a.SetChestplate(chestplate())
	a.SetLeggings(leggings())
	a.SetBoots(boots())
	_ = a.Helmet()
	_ = a.Slots()
	_ = a.Items()
	_ = a.Clear()
	if err := a.Close(); err != nil {
		t.Fatalf("Armour.Close(): got error %v, want nil", err)
	}

	if n := rec.count(); n != 0 {
		t.Fatalf("Handler invocations: got %v, want 0 (%v)", n, rec.calling())
	}
}

func TestHdlContextSemantics(t *testing.T) {
	holder := hdlHolder{name: "player"}
	ctx := event.C(Holder(holder))

	if ctx.Cancelled() {
		t.Fatalf("Cancelled() on a fresh Context: got true, want false")
	}
	if got, ok := ctx.Val().(hdlHolder); !ok || got != holder {
		t.Fatalf("Val(): got %v (%T), want %v", ctx.Val(), ctx.Val(), holder)
	}

	ctx.Cancel()
	if !ctx.Cancelled() {
		t.Fatalf("Cancelled() after Cancel(): got false, want true")
	}

	// Cancel is idempotent and does not touch the holder.
	ctx.Cancel()
	ctx.Cancel()
	if !ctx.Cancelled() {
		t.Fatalf("Cancelled() after repeated Cancel(): got false, want true")
	}
	if got, ok := ctx.Val().(hdlHolder); !ok || got != holder {
		t.Fatalf("Val() after Cancel(): got %v (%T), want %v", ctx.Val(), ctx.Val(), holder)
	}

	// There is no way back: the Context exposes no Continue/reset.
	if reflect.TypeOf(ctx).NumMethod() != 3 {
		t.Fatalf("Context methods: got %v, want 3 (Val, Cancelled, Cancel)", reflect.TypeOf(ctx).NumMethod())
	}

	// inventory.Context is an alias of event.Context[Holder], not a defined
	// type of its own: this assignment only compiles for an alias.
	var _ *event.Context[Holder] = (*Context)(nil)
	if reflect.TypeOf((*Context)(nil)) != reflect.TypeOf((*event.Context[Holder])(nil)) {
		t.Fatalf("*Context is not identical to *event.Context[Holder]")
	}
}

func TestHdlCancelledContextDoesNotBlockMutation(t *testing.T) {
	// PINNED BUG: cancelling the Context inside a Handler has no effect on the
	// Inventory, because nothing in the package consults the Context. Blocking
	// the mutation is entirely the responsibility of the caller outside this
	// package.
	rec := &recordingHandler{cancel: true}
	inv := New(2, nil)
	inv.Handle(rec)
	mustSet(t, inv, 0, diamonds(4))

	ctx := event.C(Holder(hdlHolder{name: "player"}))
	inv.Handler().HandleTake(ctx, 0, diamonds(4))
	if !ctx.Cancelled() {
		t.Fatalf("Cancelled() after a cancelling handler ran: got false, want true")
	}

	// With the Context cancelled, every mutation still goes through.
	if err := inv.RemoveItem(diamonds(4)); err != nil {
		t.Fatalf("RemoveItem after cancel: unexpected error %v", err)
	}
	assertSlot(t, inv, 0, item.Stack{})

	mustSet(t, inv, 1, sticks(2))
	assertSlot(t, inv, 1, sticks(2))

	if n, err := inv.AddItem(diamonds(3)); err != nil || n != 3 {
		t.Fatalf("AddItem after cancel: got (%v, %v), want (3, nil)", n, err)
	}
	assertSlot(t, inv, 0, diamonds(3))

	// Only the one direct call was recorded: the mutations above dispatched
	// nothing.
	if n := rec.count(); n != 1 {
		t.Fatalf("Handler invocations: got %v, want 1 (%v)", n, rec.calling())
	}
}

func TestHdlHandlerArgumentFidelity(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		invoke func(h Handler, ctx *Context, slot int, it item.Stack)
	}{
		{"HandleTake", "take", func(h Handler, ctx *Context, slot int, it item.Stack) { h.HandleTake(ctx, slot, it) }},
		{"HandlePlace", "place", func(h Handler, ctx *Context, slot int, it item.Stack) { h.HandlePlace(ctx, slot, it) }},
		{"HandleDrop", "drop", func(h Handler, ctx *Context, slot int, it item.Stack) { h.HandleDrop(ctx, slot, it) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recordingHandler{}
			inv := New(9, nil)
			inv.Handle(rec)

			holder := hdlHolder{name: c.name}
			ctx := event.C(Holder(holder))
			passed := diamonds(7)

			// This is how server/session invokes a Handler: it fetches the
			// method off Handler() and calls it itself.
			c.invoke(inv.Handler(), ctx, 5, passed)

			calls := rec.calling()
			if len(calls) != 1 {
				t.Fatalf("recorded calls: got %v, want 1 (%v)", len(calls), calls)
			}
			if calls[0].method != c.kind {
				t.Fatalf("recorded method: got %q, want %q", calls[0].method, c.kind)
			}
			if calls[0].slot != 5 {
				t.Fatalf("recorded slot: got %v, want 5", calls[0].slot)
			}
			assertStack(t, calls[0].it, diamonds(7))

			// The stack the caller passed is not modified by the call.
			assertStack(t, passed, diamonds(7))

			// A non-cancelling handler leaves the Context alone.
			if ctx.Cancelled() {
				t.Fatalf("Cancelled(): got true, want false")
			}
			if got, ok := ctx.Val().(hdlHolder); !ok || got != holder {
				t.Fatalf("Val(): got %v (%T), want %v", ctx.Val(), ctx.Val(), holder)
			}

			// The inventory itself is untouched by handler calls.
			assertSlots(t, inv, make([]item.Stack, 9)...)
		})
	}
}

func TestHdlCancellingHandlerCancelsSharedContext(t *testing.T) {
	rec := &recordingHandler{cancel: true}
	inv := New(4, nil)
	inv.Handle(rec)

	// server/session reuses a single Context across a chain of handler calls,
	// so pin that the Context stays cancelled and that the handler keeps
	// recording: nothing short-circuits inside the handler itself.
	ctx := event.C(Holder(hdlHolder{name: "player"}))

	inv.Handler().HandleTake(ctx, 0, diamonds(1))
	if !ctx.Cancelled() {
		t.Fatalf("Cancelled() after HandleTake: got false, want true")
	}
	inv.Handler().HandlePlace(ctx, 1, sticks(2))
	inv.Handler().HandleDrop(ctx, 2, helmet())
	if !ctx.Cancelled() {
		t.Fatalf("Cancelled() after the full chain: got false, want true")
	}

	calls := rec.calling()
	if len(calls) != 3 {
		t.Fatalf("recorded calls: got %v, want 3 (%v)", len(calls), calls)
	}
	wantKind := []string{"take", "place", "drop"}
	wantSlot := []int{0, 1, 2}
	wantIt := []item.Stack{diamonds(1), sticks(2), helmet()}
	for i, got := range calls {
		if got.method != wantKind[i] {
			t.Fatalf("call %v method: got %q, want %q", i, got.method, wantKind[i])
		}
		if got.slot != wantSlot[i] {
			t.Fatalf("call %v slot: got %v, want %v", i, got.slot, wantSlot[i])
		}
		assertStack(t, got.it, wantIt[i])
	}
}

func TestHdlNopHandlerIsNoOp(t *testing.T) {
	// Pins the var _ Handler = NopHandler{} assertion in handler.go: NopHandler
	// satisfies Handler as a value, not as a pointer.
	var h Handler = NopHandler{}

	holder := hdlHolder{name: "player"}
	ctx := event.C(Holder(holder))

	h.HandleTake(ctx, 0, diamonds(1))
	h.HandlePlace(ctx, 1, sticks(2))
	h.HandleDrop(ctx, 2, helmet())

	if ctx.Cancelled() {
		t.Fatalf("Cancelled() after NopHandler ran: got true, want false")
	}
	if got, ok := ctx.Val().(hdlHolder); !ok || got != holder {
		t.Fatalf("Val(): got %v (%T), want %v", ctx.Val(), ctx.Val(), holder)
	}

	// NopHandler is an empty struct: two values are equal and it carries no
	// state between calls.
	if (NopHandler{}) != (NopHandler{}) {
		t.Fatalf("NopHandler{} values are not equal")
	}
	if s := reflect.TypeOf(NopHandler{}).Size(); s != 0 {
		t.Fatalf("NopHandler size: got %v bytes, want 0", s)
	}
}

func TestHdlHandlerInterfaceShape(t *testing.T) {
	// Handler has 3 methods; a redesign that grows it trips this test.
	typ := reflect.TypeOf((*Handler)(nil)).Elem()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("Handler kind: got %v, want interface", typ.Kind())
	}
	if n := typ.NumMethod(); n != 3 {
		t.Fatalf("Handler method count: got %v, want 3", n)
	}

	names := make([]string, typ.NumMethod())
	for i := range names {
		names[i] = typ.Method(i).Name
	}
	slices.Sort(names)
	if want := []string{"HandleDrop", "HandlePlace", "HandleTake"}; !slices.Equal(names, want) {
		t.Fatalf("Handler methods: got %v, want %v", names, want)
	}

	// Every method has the same shape: (*Context, int, item.Stack) and no
	// results, so a Handler can only report back through the Context.
	for i := range names {
		m := typ.Method(i)
		if m.Type.NumIn() != 3 || m.Type.NumOut() != 0 {
			t.Fatalf("%v signature: got %v in / %v out, want 3 in / 0 out", m.Name, m.Type.NumIn(), m.Type.NumOut())
		}
		if got, want := m.Type.In(0), reflect.TypeOf((*Context)(nil)); got != want {
			t.Fatalf("%v argument 0: got %v, want %v", m.Name, got, want)
		}
		if got, want := m.Type.In(1), reflect.TypeOf(int(0)); got != want {
			t.Fatalf("%v argument 1: got %v, want %v", m.Name, got, want)
		}
		if got, want := m.Type.In(2), reflect.TypeOf(item.Stack{}); got != want {
			t.Fatalf("%v argument 2: got %v, want %v", m.Name, got, want)
		}
	}
}

func TestHdlHolderAcceptsAnyValue(t *testing.T) {
	typ := reflect.TypeOf((*Holder)(nil)).Elem()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("Holder kind: got %v, want interface", typ.Kind())
	}
	if n := typ.NumMethod(); n != 0 {
		t.Fatalf("Holder method count: got %v, want 0 (Holder is an empty interface)", n)
	}

	cases := []struct {
		name string
		v    any
	}{
		{"nil", nil},
		{"int", 42},
		{"string", "player"},
		{"struct", hdlHolder{name: "player"}},
		{"pointer", New(1, nil)},
		{"handler", NopHandler{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var h Holder = c.v
			ctx := event.C(h)

			if got := ctx.Val(); got != c.v {
				t.Fatalf("Val(): got %v (%T), want %v (%T)", got, got, c.v, c.v)
			}
			if ctx.Cancelled() {
				t.Fatalf("Cancelled() on a fresh Context: got true, want false")
			}
			ctx.Cancel()
			if got := ctx.Val(); got != c.v {
				t.Fatalf("Val() after Cancel(): got %v, want %v", got, c.v)
			}
		})
	}

	// A nil holder round-trips as a nil interface, not as a typed zero value.
	ctx := event.C(Holder(nil))
	if ctx.Val() != nil {
		t.Fatalf("Val() of a nil Holder: got %v (%T), want nil", ctx.Val(), ctx.Val())
	}
}

func TestHdlHandlerConcurrentAccess(t *testing.T) {
	inv := New(4, nil)
	handlers := make([]*recordingHandler, 8)
	for i := range handlers {
		handlers[i] = &recordingHandler{}
	}

	// errs is indexed per goroutine so that no error is reported from a
	// goroutine other than the test's own.
	errs := make([]error, len(handlers))

	var wg sync.WaitGroup
	for i, h := range handlers {
		wg.Add(3)
		go func() {
			defer wg.Done()
			inv.Handle(h)
		}()
		go func() {
			defer wg.Done()
			_ = inv.Handler()
		}()
		go func() {
			defer wg.Done()
			errs[i] = inv.SetItem(0, diamonds(1))
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("SetItem in goroutine %v: unexpected error %v", i, err)
		}
	}

	got, ok := inv.Handler().(*recordingHandler)
	if !ok {
		t.Fatalf("Handler(): got %T, want *recordingHandler", inv.Handler())
	}
	if !slices.Contains(handlers, got) {
		t.Fatalf("Handler(): got %p, want one of the handlers assigned", got)
	}
	for i, h := range handlers {
		if n := h.count(); n != 0 {
			t.Fatalf("handler %v invocations: got %v, want 0", i, n)
		}
	}
}
