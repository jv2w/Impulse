package session

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// recordingHandler is a slog.Handler that keeps every record it is given. Both
// the world and the session log through it so that a test can tell which side
// wrote what.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// find returns the first record whose message is msg, and the record's
// attributes as a map.
func (h *recordingHandler) find(msg string) (slog.Record, map[string]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message != msg {
			continue
		}
		attrs := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		return r, attrs, true
	}
	return slog.Record{}, nil, false
}

// panicControllable is the Controllable the panicking session controls. Only
// the handful of methods the packet loop and the session teardown reach are
// implemented; the embedded nil Controllable would panic on anything else,
// which is exactly the signal wanted if this test ever grows a new call.
type panicControllable struct {
	Controllable
	h      *world.EntityHandle
	closed atomic.Bool
	moved  atomic.Bool
}

func (c *panicControllable) H() *world.EntityHandle  { return c.h }
func (c *panicControllable) Position() mgl64.Vec3    { return mgl64.Vec3{} }
func (c *panicControllable) Rotation() cube.Rotation { return cube.Rotation{} }
func (c *panicControllable) Close() error            { c.closed.Store(true); return nil }
func (c *panicControllable) MoveItemsToInventory()   { c.moved.Store(true) }

// UUID is reached through chat.Global.Unsubscribe in Session.close.
func (c *panicControllable) UUID() uuid.UUID { return uuid.UUID{1} }

// panicEntityType hands out one shared panicControllable so the test can read
// back what the teardown did to it.
type panicEntityType struct{ c *panicControllable }

func (t panicEntityType) Open(_ *world.Tx, h *world.EntityHandle, _ *world.EntityData) world.Entity {
	t.c.h = h
	return t.c
}
func (panicEntityType) EncodeEntity() string                        { return "impulse:panic_test" }
func (panicEntityType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (panicEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (panicEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type panicEntityConfig struct{}

func (panicEntityConfig) Apply(*world.EntityData) {}

// panicPacketHandler panics the way a defective packet handler would, and
// counts how many packets it was given.
type panicPacketHandler struct {
	value any
	calls atomic.Int32
}

func (h *panicPacketHandler) Handle(packet.Packet, *Session, *world.Tx, Controllable) error {
	h.calls.Add(1)
	panic(h.value)
}

// TestHandlePacketsSurvivesHandlerPanic pins the isolation of a panicking
// packet handler. A panic inside the world callback is recovered on the world
// goroutine by the world's own task recovery and re-panicked on the session
// goroutine; nothing above handlePackets used to recover it, so it took the
// process down. Three things are asserted: the process lives (this test
// returning at all), the session is torn down and stops reading, and the panic
// is logged on both sides.
func TestHandlePacketsSurvivesHandlerPanic(t *testing.T) {
	log := &recordingHandler{}
	w := world.Config{Log: slog.New(log)}.New()
	t.Cleanup(func() { _ = w.Close() })

	c := &panicControllable{}
	handle := world.NewEntity(panicEntityType{c: c}, panicEntityConfig{})
	if err := w.Do(func(tx *world.Tx) { tx.AddEntity(handle) }).Wait(context.Background()); err != nil {
		t.Fatalf("add controllable: %v", err)
	}

	panicValue := &struct{ message string }{"packet handler panic"}
	handler := &panicPacketHandler{value: panicValue}

	var stopped atomic.Bool
	conn := &stubConn{read: make(chan packet.Packet, 4)}
	s := newStubSession(t, conn)
	s.ent = handle
	s.conf.Log = slog.New(log)
	s.conf.HandleStop = func(*world.Tx, Controllable) { stopped.Store(true) }
	s.inv, s.offHand = inventory.New(36, nil), inventory.New(1, nil)
	s.armour = inventory.NewArmour(nil)
	if err := w.Do(func(tx *world.Tx) {
		s.chunkLoader = world.NewLoader(1, tx.World(), s)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("create chunk loader: %v", err)
	}
	// Text is the packet the panicking handler is registered for. A second one
	// is queued behind it to prove the read loop does not carry on.
	s.handlers[(&packet.Text{}).ID()] = handler
	conn.read <- &packet.Text{Message: "first"}
	conn.read <- &packet.Text{Message: "second"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handlePackets()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handlePackets did not return after its handler panicked")
	}

	// (1) The session is gone: the read loop stopped on the first packet and
	// the teardown in handlePackets' defer ran to completion.
	if got := handler.calls.Load(); got != 1 {
		t.Fatalf("handler was called %d times, want 1: the read loop kept going after the panic", got)
	}
	if !c.closed.Load() {
		t.Fatal("the Controllable was not closed, so the session teardown did not run")
	}
	if !c.moved.Load() {
		t.Fatal("Session.close did not run: MoveItemsToInventory was never called")
	}
	if !stopped.Load() {
		t.Fatal("HandleStop was never called, so the session was not closed")
	}

	// (2) The panic is on the record, with the packet type that caused it.
	_, attrs, ok := log.find("panic handling packet")
	if !ok {
		t.Fatal("the swallowed panic was not logged by the session")
	}
	if want := "*packet.Text"; attrs["packet"] != want {
		t.Fatalf("logged packet = %q, want %q", attrs["packet"], want)
	}
	if !strings.Contains(attrs["panic"], "packet handler panic") {
		t.Fatalf("logged panic value = %q, want it to name the panic", attrs["panic"])
	}
	if !strings.Contains(attrs["stack"], "goroutine") {
		t.Fatalf("logged stack = %q, want a goroutine stack", attrs["stack"])
	}
	// The world logged the original panicking stack when it recovered on its
	// own goroutine. That is the half that names the handler frame.
	_, worldAttrs, ok := log.find("scheduled task panicked")
	if !ok {
		t.Fatal("the world did not log the panic it recovered")
	}
	if !strings.Contains(worldAttrs["stack"], "panicPacketHandler") {
		t.Fatalf("world stack = %q, want the panicking handler frame", worldAttrs["stack"])
	}

	// (3) The world goroutine survived and holds no lock: a fresh transaction
	// still runs.
	ran, err := world.Call(context.Background(), w, func(*world.Tx) (bool, error) { return true, nil })
	if err != nil || !ran {
		t.Fatalf("the world stopped running transactions after the panic: ran = %v, err = %v", ran, err)
	}
}

// nopPacketHandler is the benchmark counterpart of panicPacketHandler: it does
// the least a real handler can do.
type nopPacketHandler struct{}

func (nopPacketHandler) Handle(packet.Packet, *Session, *world.Tx, Controllable) error { return nil }

// benchPacketSession builds a Session on a synchronous world, where the owner
// callback runs inline on the calling goroutine. The world queue round trip a
// real server pays would otherwise drown out what is being measured here.
func benchPacketSession(b *testing.B) (*Session, *world.World) {
	b.Helper()
	w := world.Config{Log: slog.New(slog.DiscardHandler), Synchronous: true}.New()
	b.Cleanup(func() { _ = w.Close() })

	handle := world.NewEntity(panicEntityType{c: &panicControllable{}}, panicEntityConfig{})
	if err := w.Do(func(tx *world.Tx) { tx.AddEntity(handle) }).Wait(context.Background()); err != nil {
		b.Fatalf("add controllable: %v", err)
	}
	s := &Session{
		conf:     Config{Log: slog.New(slog.DiscardHandler)},
		conn:     &stubConn{},
		packets:  make(chan packet.Packet, 256),
		handlers: map[uint32]packetHandler{(&packet.Text{}).ID(): nopPacketHandler{}},
		ent:      handle,
	}
	return s, w
}

// BenchmarkHandlePacketRecovered measures the packet path with the recovery
// wrapper in place. It is paired with BenchmarkHandlePacketDirect below, which
// runs the shape the loop had before the wrapper was added; the difference
// between the two is what the recovery costs per packet.
func BenchmarkHandlePacketRecovered(b *testing.B) {
	s, _ := benchPacketSession(b)
	pk := &packet.Text{Message: "bench"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := s.handlePacketRecovered(pk); err != nil {
			b.Fatalf("handle packet: %v", err)
		}
	}
}

// BenchmarkHandlePacketDirect is the baseline: the call handlePackets made
// before handlePacketRecovered wrapped it.
func BenchmarkHandlePacketDirect(b *testing.B) {
	s, _ := benchPacketSession(b)
	pk := &packet.Text{Message: "bench"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err := s.withControllable(context.Background(), func(tx *world.Tx, c Controllable) error {
			return s.handlePacket(pk, tx, c)
		})
		if err != nil {
			b.Fatalf("handle packet: %v", err)
		}
	}
}
