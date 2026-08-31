package world

import (
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// TestCloseSeqOrder pins the shutdown sequence of World.close: the close
// transaction runs user code first, then its deferred work, then swaps the
// Handler out, and only then saves and unloads the chunks. Settings are written
// last, after every column.
func TestCloseSeqOrder(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{Synchronous: true})
	log.w = w
	w.Handle(&closeSeqHandler{log: log})

	runWorld(w, func(tx *Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, closeSeqBlock{}, nil)
		tx.AddEntity(EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(closeSeqEntityType{}, closeSeqEntityConfig{log: log}))
	})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	want := []string{"handleClose", "deferred", "storeColumn", "entityClose(nopHandler)", "saveSettings", "providerClose"}
	if got := log.events(); !slices.Equal(got, want) {
		t.Fatalf("close sequence = %v, want %v", got, want)
	}
}

// TestCloseSeqChannelTransitions pins the order the shutdown channels flip in.
// closeStarted is closed before the close transaction runs, while closing and
// queueClosing stay open until it has finished, so work queued from inside the
// transaction still reaches the world.
func TestCloseSeqChannelTransitions(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{Synchronous: true})
	log.w = w

	var inTransaction [3]bool
	w.Handle(&closeSeqHandler{log: log, onClose: func(tx *Tx) {
		world := tx.World()
		inTransaction = [3]bool{
			closeSeqChannelClosed(world.closeStarted),
			closeSeqChannelClosed(world.closing),
			closeSeqChannelClosed(world.queueClosing),
		}
	}})

	if w.closed.Load() {
		t.Fatal("world reported closed before Close was called")
	}
	_ = w.Close()

	if want := [3]bool{true, false, false}; inTransaction != want {
		t.Fatalf("closeStarted/closing/queueClosing during the close transaction = %v, want %v", inTransaction, want)
	}
	after := [3]bool{
		closeSeqChannelClosed(w.closeStarted),
		closeSeqChannelClosed(w.closing),
		closeSeqChannelClosed(w.queueClosing),
	}
	if want := [3]bool{true, true, true}; after != want {
		t.Fatalf("closeStarted/closing/queueClosing after Close = %v, want %v", after, want)
	}
	if !w.closed.Load() {
		t.Fatal("closed flag not set after Close")
	}
}

// TestCloseSeqIsIdempotent pins that the sequence runs once however often Close
// is called.
func TestCloseSeqIsIdempotent(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{Synchronous: true})
	log.w = w
	runWorld(w, func(tx *Tx) { tx.SetBlock(cube.Pos{0, 64, 0}, closeSeqBlock{}, nil) })

	for range 3 {
		_ = w.Close()
	}
	if got := countOccurrences(log.events(), "storeColumn"); got != 1 {
		t.Fatalf("columns stored across three Close calls = %d, want 1", got)
	}
	if got := countOccurrences(log.events(), "providerClose"); got != 1 {
		t.Fatalf("provider closed %d time(s) across three Close calls, want 1", got)
	}
}

// TestCloseSeqReadOnlySkipsSaving pins that a read-only World writes nothing on
// close, neither columns nor settings.
func TestCloseSeqReadOnlySkipsSaving(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{Synchronous: true, ReadOnly: true})
	log.w = w
	runWorld(w, func(tx *Tx) { tx.SetBlock(cube.Pos{0, 64, 0}, closeSeqBlock{}, nil) })

	_ = w.Close()
	for _, event := range []string{"storeColumn", "saveSettings"} {
		if got := countOccurrences(log.events(), event); got != 0 {
			t.Fatalf("read-only World produced %d %s event(s), want 0", got, event)
		}
	}
}

// TestCloseSeqStoresOnlyModifiedChunks pins that saving skips columns that were
// only read: a chunk becomes storable when something writes to it.
func TestCloseSeqStoresOnlyModifiedChunks(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{Synchronous: true})
	log.w = w

	touched, untouched := ChunkPos{0, 0}, ChunkPos{5, 5}
	runWorld(w, func(tx *Tx) {
		tx.chunk(untouched)
		tx.SetBlock(cube.Pos{0, 64, 0}, closeSeqBlock{}, nil)
	})

	_ = w.Close()
	if got := log.stored(); !slices.Equal(got, []ChunkPos{touched}) {
		t.Fatalf("stored columns = %v, want only the modified %v", got, touched)
	}
}

// TestCloseSeqNilBlockStoresNilBlockEntity pins what SetBlock does with a nil
// block when the air state is flagged as carrying NBT: it writes the air runtime
// ID and stores nil as the block entity at that position.
//
// PINNED BUG: Tx.SetBlock documents nil as "set the block to air", but it puts a
// nil Block into Column.BlockEntities whenever the air runtime ID is NBT-flagged,
// and saving that column then dereferences the nil in columnTo. The vanilla
// registry is safe because its air is not an NBTer; any registry built without
// those implementations is not. Pinned as-is; decide in stage 1.
func TestCloseSeqNilBlockStoresNilBlockEntity(t *testing.T) {
	log := &closeSeqLog{}
	w := closeSeqWorld(t, &closeSeqProvider{log: log}, Config{Synchronous: true})

	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(pos, nil, nil)
		col := tx.chunk(chunkPosFromBlockPos(pos))
		be, ok := col.BlockEntities[pos]
		if !ok {
			t.Fatal("no block entity recorded for the position set to nil")
		}
		if be != nil {
			t.Fatalf("block entity for the position set to nil = %#v, want nil", be)
		}
		// Drop the entry again so closing the World does not walk into the panic
		// this test documents.
		delete(col.BlockEntities, pos)
	})
}

// TestCloseSeqProviderOwnership pins which World closes the shared Provider: the
// one that took the first reference to the Settings, regardless of how many
// Worlds are still using them.
//
// PINNED BUG: the advancing World closes the Provider on its own Close even when
// other Worlds still hold the same Settings, and a non-advancing World never
// closes it, so ownership does not follow the reference count it decrements.
// Pinned as-is; decide in stage 1.
func TestCloseSeqProviderOwnership(t *testing.T) {
	t.Run("non advancing world leaves the provider open", func(t *testing.T) {
		log := &closeSeqLog{}
		set := defaultSettings()
		provider := &closeSeqProvider{log: log, NopProvider: NopProvider{Set: set}}
		first := closeSeqWorld(t, provider, Config{Synchronous: true})
		second := closeSeqWorld(t, provider, Config{Synchronous: true})

		_ = second.Close()
		if got := countOccurrences(log.events(), "providerClose"); got != 0 {
			t.Fatalf("provider closed %d time(s) by the non-advancing World, want 0", got)
		}
		_ = first.Close()
		if got := countOccurrences(log.events(), "providerClose"); got != 1 {
			t.Fatalf("provider closed %d time(s) after both Worlds closed, want 1", got)
		}
	})
	t.Run("advancing world closes it while another world is live", func(t *testing.T) {
		log := &closeSeqLog{}
		set := defaultSettings()
		provider := &closeSeqProvider{log: log, NopProvider: NopProvider{Set: set}}
		first := closeSeqWorld(t, provider, Config{Synchronous: true})
		second := closeSeqWorld(t, provider, Config{Synchronous: true})

		_ = first.Close()
		if got := countOccurrences(log.events(), "providerClose"); got != 1 {
			t.Fatalf("provider closed %d time(s) by the advancing World, want 1", got)
		}
		// The surviving World keeps using the Provider it no longer owns.
		second.AdvanceTick()
		_ = second.Close()
		if got := countOccurrences(log.events(), "providerClose"); got != 1 {
			t.Fatalf("provider closed %d time(s) in total, want 1", got)
		}
	})
}

// TestCloseSeqAutoSave pins that a non-Synchronous World saves on its own on the
// SaveInterval, and that a negative interval disables saving entirely.
func TestCloseSeqAutoSave(t *testing.T) {
	t.Run("saves on the interval", func(t *testing.T) {
		log := &closeSeqLog{}
		provider := &closeSeqProvider{log: log}
		closeSeqWorld(t, provider, Config{SaveInterval: time.Millisecond * 20})

		closeSeqWaitFor(t, func() bool { return countOccurrences(log.events(), "saveSettings") > 0 })
	})
	t.Run("negative interval disables saving", func(t *testing.T) {
		log := &closeSeqLog{}
		provider := &closeSeqProvider{log: log}
		closeSeqWorld(t, provider, Config{SaveInterval: -1})

		time.Sleep(time.Millisecond * 200)
		if got := countOccurrences(log.events(), "saveSettings"); got != 0 {
			t.Fatalf("settings saved %d time(s) with a negative SaveInterval, want 0", got)
		}
	})
}

// TestCloseSeqChunkUnload pins that the unload timer closes chunks nobody is
// viewing and leaves viewed chunks loaded.
func TestCloseSeqChunkUnload(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{ChunkUnloadInterval: time.Millisecond * 20, SaveInterval: -1})

	viewed, unviewed := ChunkPos{0, 0}, ChunkPos{9, 9}
	runWorld(w, func(tx *Tx) {
		tx.World().addViewer(tx, tx.chunk(viewed), &Loader{viewer: NopViewer{}})
		tx.chunk(unviewed)
	})

	closeSeqWaitFor(t, func() bool {
		var loaded []ChunkPos
		runWorld(w, func(tx *Tx) { loaded = closeSeqLoadedChunks(tx) })
		return slices.Equal(loaded, []ChunkPos{viewed})
	})
}

// TestCloseSeqSynchronousRunsNoBackgroundWork pins that a Synchronous World has
// no timers at all: it neither saves, unloads nor ticks until it is driven.
func TestCloseSeqSynchronousRunsNoBackgroundWork(t *testing.T) {
	log := &closeSeqLog{}
	provider := &closeSeqProvider{log: log}
	w := closeSeqWorld(t, provider, Config{
		Synchronous:         true,
		SaveInterval:        time.Millisecond * 10,
		ChunkUnloadInterval: time.Millisecond * 10,
	})
	runWorld(w, func(tx *Tx) { tx.chunk(ChunkPos{0, 0}) })
	before := w.currentTick()

	time.Sleep(time.Millisecond * 200)

	if got := log.events(); len(got) != 0 {
		t.Fatalf("Synchronous World produced provider events %v, want none", got)
	}
	if got := w.currentTick(); got != before {
		t.Fatalf("current tick after 200ms = %d, want %d", got, before)
	}
	var loaded []ChunkPos
	runWorld(w, func(tx *Tx) { loaded = closeSeqLoadedChunks(tx) })
	if !slices.Equal(loaded, []ChunkPos{{0, 0}}) {
		t.Fatalf("loaded chunks after 200ms = %v, want the chunk to stay loaded", loaded)
	}
}

// closeSeqWorld builds a World on the recording provider, filling in the fields
// every test in this file shares.
func closeSeqWorld(t *testing.T, provider Provider, conf Config) *World {
	t.Helper()

	if conf.Blocks == nil {
		registry := NewBlockRegistry()
		registry.RegisterBlockState(BlockState{Name: "test:close_seq_block", Properties: map[string]any{}})
		registry.RegisterBlock(closeSeqBlock{})
		conf.Blocks = registry
	}
	conf.Provider = provider
	conf.RandSource = rand.NewPCG(0x434c4f53, 0x45000000)
	conf.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	w := conf.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// closeSeqWaitFor polls cond until it holds or the test's patience runs out.
// Shutdown and timer behaviour is observed rather than timed, so a slow machine
// makes the test slower and not wrong.
func closeSeqWaitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(time.Second * 5)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held within 5s")
		}
		time.Sleep(time.Millisecond * 5)
	}
}

// closeSeqLoadedChunks returns the loaded chunk positions in a stable order.
func closeSeqLoadedChunks(tx *Tx) []ChunkPos {
	loaded := make([]ChunkPos, 0, len(tx.World().chunks))
	for pos := range tx.World().chunks {
		loaded = append(loaded, pos)
	}
	slices.SortFunc(loaded, func(a, b ChunkPos) int {
		if a[0] != b[0] {
			return int(a[0] - b[0])
		}
		return int(a[1] - b[1])
	})
	return loaded
}

func closeSeqChannelClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// closeSeqLog records the shutdown events in the order they happened. The
// Provider is called from background goroutines in the non-Synchronous tests, so
// every access is guarded.
type closeSeqLog struct {
	mu   sync.Mutex
	seq  []string
	cols []ChunkPos
	w    *World
}

func (l *closeSeqLog) record(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq = append(l.seq, event)
}

func (l *closeSeqLog) events() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seq)
}

func (l *closeSeqLog) stored() []ChunkPos {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.cols)
}

// closeSeqProvider records the writes a World makes and otherwise behaves like
// NopProvider.
type closeSeqProvider struct {
	NopProvider
	log *closeSeqLog
}

func (p *closeSeqProvider) StoreColumn(pos ChunkPos, _ Dimension, _ *chunk.Column) error {
	p.log.mu.Lock()
	p.log.seq = append(p.log.seq, "storeColumn")
	p.log.cols = append(p.log.cols, pos)
	p.log.mu.Unlock()
	return nil
}

func (p *closeSeqProvider) SaveSettings(*Settings) { p.log.record("saveSettings") }

func (p *closeSeqProvider) Close() error {
	p.log.record("providerClose")
	return nil
}

// closeSeqHandler records the close event and hands one callback to Tx.Defer, so
// the position of deferred work inside the close transaction is observable.
type closeSeqHandler struct {
	NopHandler
	log     *closeSeqLog
	onClose func(*Tx)
}

func (h *closeSeqHandler) HandleClose(tx *Tx) {
	h.log.record("handleClose")
	if h.onClose != nil {
		h.onClose(tx)
	}
	tx.Defer(func(*Tx) { h.log.record("deferred") })
}

// closeSeqBlock is a plain registered block, used where a test needs a column to
// be marked modified without relying on the air state.
type closeSeqBlock struct{}

func (closeSeqBlock) EncodeBlock() (string, map[string]any) {
	return "test:close_seq_block", nil
}
func (closeSeqBlock) Hash() (uint64, uint64) { return 1 << 62, 0 }
func (closeSeqBlock) Model() BlockModel      { return unknownModel{} }

type closeSeqEntityConfig struct{ log *closeSeqLog }

func (c closeSeqEntityConfig) Apply(data *EntityData) { data.Data = c.log }

type closeSeqEntityType struct{}

func (closeSeqEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &closeSeqEntity{handle: handle, data: data}
}
func (closeSeqEntityType) EncodeEntity() string                  { return "test:close_seq_entity" }
func (closeSeqEntityType) BBox(Entity) cube.BBox                 { return cube.BBox{} }
func (closeSeqEntityType) DecodeNBT(map[string]any, *EntityData) {}
func (closeSeqEntityType) EncodeNBT(*EntityData) map[string]any  { return nil }

type closeSeqEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (e *closeSeqEntity) H() *EntityHandle        { return e.handle }
func (e *closeSeqEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *closeSeqEntity) Rotation() cube.Rotation { return e.data.Rot }

// Close records whether the World had already swapped its Handler out by the
// time the chunk holding this entity was closed.
func (e *closeSeqEntity) Close() error {
	log := e.data.Data.(*closeSeqLog)
	which := "customHandler"
	if _, ok := log.w.Handler().(NopHandler); ok {
		which = "nopHandler"
	}
	log.record("entityClose(" + which + ")")
	return nil
}
