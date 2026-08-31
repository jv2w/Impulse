package world

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/block/cube"
)

// TestTickNeighbourRegistrationOrder pins the order doBlockUpdatesAround
// registers updates in, and that performNeighbourUpdates replays that order
// unchanged: the changed position first, then its neighbours along +X, -X, +Y,
// -Y, +Z, -Z.
func TestTickNeighbourRegistrationOrder(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	centre := cube.Pos{8, 64, 8}
	want := [][2]cube.Pos{
		{centre, centre},
		{{9, 64, 8}, centre},
		{{7, 64, 8}, centre},
		{{8, 65, 8}, centre},
		{{8, 63, 8}, centre},
		{{8, 64, 9}, centre},
		{{8, 64, 7}, centre},
	}

	runWorld(w, func(tx *Tx) {
		for _, update := range want {
			tickNeighbourPlant(tx, update[0], tickNeighbourTicker{log: log})
		}
		// Writing the same block back still runs doBlockUpdatesAround, so the
		// production registration path is what produces the order asserted here.
		tx.SetBlock(centre, tickNeighbourTicker{log: log}, nil)
	})
	log.reset()
	w.AdvanceTick()

	if got := log.updates; !slices.Equal(got, want) {
		t.Fatalf("neighbour update order = %v, want %v", got, want)
	}
}

// TestTickNeighbourUpdatesQueuedDuringPassRunNextTick pins that
// performNeighbourUpdates works from a clone taken before it clears the queue,
// so an update a NeighbourUpdateTicker registers is deferred to the next tick
// instead of extending the current pass.
func TestTickNeighbourUpdatesQueuedDuringPassRunNextTick(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	first, second := cube.Pos{1, 64, 1}, cube.Pos{5, 64, 5}
	runWorld(w, func(tx *Tx) {
		tickNeighbourPlant(tx, first, tickNeighbourChain{log: log, next: second})
		tickNeighbourPlant(tx, second, tickNeighbourTicker{log: log})
		tx.World().updateNeighbour(first, first)
	})
	log.reset()

	startTick := w.currentTick()
	w.AdvanceTick()
	if got := log.ticks; !slices.Equal(got, []int64{startTick + 1}) {
		t.Fatalf("ticks observed on the first tick = %v, want only %d", got, startTick+1)
	}
	if got := log.updates; len(got) != 1 || got[0][0] != first {
		t.Fatalf("first tick updates = %v, want only the chained block at %v", got, first)
	}

	w.AdvanceTick()
	if got := log.ticks; !slices.Equal(got, []int64{startTick + 1, startTick + 2, startTick + 2}) {
		t.Fatalf("ticks observed after two ticks = %v, want the chained update to land on %d", got, startTick+2)
	}
	// The chain registers its target before re-arming itself, so the second tick
	// replays those two in that order.
	want := [][2]cube.Pos{{first, first}, {second, first}, {first, first}}
	if got := log.updates; !slices.Equal(got, want) {
		t.Fatalf("updates over two ticks = %v, want %v", got, want)
	}
}

// TestTickNeighbourDuplicateRegistrationsAllRun pins that updateNeighbour
// appends without deduplicating, so a position registered n times in one tick
// has its NeighbourUpdateTick called n times.
func TestTickNeighbourDuplicateRegistrationsAllRun(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	pos := cube.Pos{3, 64, 3}
	changed := []cube.Pos{{4, 64, 3}, {3, 65, 3}, {4, 64, 3}}
	runWorld(w, func(tx *Tx) {
		tickNeighbourPlant(tx, pos, tickNeighbourTicker{log: log})
		for _, c := range changed {
			tx.World().updateNeighbour(pos, c)
		}
	})
	log.reset()
	w.AdvanceTick()

	want := [][2]cube.Pos{{pos, changed[0]}, {pos, changed[1]}, {pos, changed[2]}}
	if got := log.updates; !slices.Equal(got, want) {
		t.Fatalf("updates for a position registered three times = %v, want %v", got, want)
	}
}

// TestTickNeighbourAdditionalLiquidAlsoTicks pins that a position carries two
// neighbour update receivers: the block on the first layer and, after it, a
// Liquid sitting on the second layer.
func TestTickNeighbourAdditionalLiquidAlsoTicks(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	pos := cube.Pos{6, 64, 6}
	runWorld(w, func(tx *Tx) {
		tickNeighbourPlant(tx, pos, tickNeighbourTicker{log: log})
		// Writing the liquid straight to the second layer avoids setLiquid's
		// displacement rules, which would decide the layer for us.
		col := tx.chunk(chunkPosFromBlockPos(pos))
		col.SetBlock(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 1, tx.World().conf.Blocks.BlockRuntimeID(tickNeighbourLiquid{log: log}))
		tx.World().updateNeighbour(pos, pos)
	})
	log.reset()
	w.AdvanceTick()

	if got := log.sources; !slices.Equal(got, []string{"block", "liquid"}) {
		t.Fatalf("neighbour update receivers = %v, want the first-layer block followed by the second-layer liquid", got)
	}
}

// TestTickNeighbourNonTickerPositionsAreSkipped pins that positions whose block
// does not implement NeighbourUpdateTicker consume a queue entry and nothing
// else, leaving the surrounding air of a SetBlock inert.
func TestTickNeighbourNonTickerPositionsAreSkipped(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	pos := cube.Pos{2, 64, 2}
	runWorld(w, func(tx *Tx) { tx.SetBlock(pos, tickNeighbourInert{}, nil) })
	log.reset()
	w.AdvanceTick()

	if got := log.updates; len(got) != 0 {
		t.Fatalf("updates recorded = %v, want none: neither the inert block nor the air around it ticks", got)
	}
	runWorld(w, func(tx *Tx) {
		if got := len(tx.World().neighbourUpdates); got != 0 {
			t.Fatalf("queued neighbour updates after the pass = %d, want the queue drained", got)
		}
	})
}

// TestTickScheduledOnlyDueUpdatesRun pins that a tick runs the updates scheduled
// for it or earlier and leaves later ones queued, and that a delay is rounded up
// to at least one tick.
func TestTickScheduledOnlyDueUpdatesRun(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	soon, late := cube.Pos{1, 64, 1}, cube.Pos{2, 64, 2}
	runWorld(w, func(tx *Tx) {
		tickNeighbourPlant(tx, soon, tickNeighbourScheduled{log: log})
		tickNeighbourPlant(tx, late, tickNeighbourScheduled{log: log})
		tx.ScheduleBlockUpdate(soon, tickNeighbourScheduled{log: log}, 0)
		tx.ScheduleBlockUpdate(late, tickNeighbourScheduled{log: log}, time.Second/20*3)
	})
	log.reset()

	start := w.currentTick()
	for range 3 {
		w.AdvanceTick()
	}

	want := []cube.Pos{soon, late}
	if got := log.scheduled; !slices.Equal(got, want) {
		t.Fatalf("scheduled positions run = %v, want %v", got, want)
	}
	if got := log.ticks; !slices.Equal(got, []int64{start + 1, start + 3}) {
		t.Fatalf("scheduled ticks = %v, want %v", got, []int64{start + 1, start + 3})
	}
}

// TestTickScheduledSameTickRunInQueueOrder pins that updates due on the same
// tick run in the order they were added to the queue, not in position order.
func TestTickScheduledSameTickRunInQueueOrder(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	// Descending coordinates, so a position-ordered run would reverse this.
	order := []cube.Pos{{9, 64, 9}, {5, 64, 5}, {1, 64, 1}}
	runWorld(w, func(tx *Tx) {
		for _, pos := range order {
			tickNeighbourPlant(tx, pos, tickNeighbourScheduled{log: log})
			tx.ScheduleBlockUpdate(pos, tickNeighbourScheduled{log: log}, time.Second/20)
		}
	})
	log.reset()
	w.AdvanceTick()

	if got := log.scheduled; !slices.Equal(got, order) {
		t.Fatalf("same-tick scheduled order = %v, want queue insertion order %v", got, order)
	}
}

// TestTickScheduledRescheduleRunsNextTick pins that an update rescheduled from
// inside its own ScheduledTick never runs twice in one tick: the queue is ranged
// over the slice length captured at entry, and the shortest delay still resolves
// to the following tick.
func TestTickScheduledRescheduleRunsNextTick(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	pos := cube.Pos{4, 64, 4}
	runWorld(w, func(tx *Tx) {
		tickNeighbourPlant(tx, pos, tickNeighbourScheduled{log: log, reschedule: 2})
		tx.ScheduleBlockUpdate(pos, tickNeighbourScheduled{log: log, reschedule: 2}, 0)
	})
	log.reset()

	start := w.currentTick()
	for range 4 {
		w.AdvanceTick()
	}

	want := []int64{start + 1, start + 2, start + 3}
	if got := log.ticks; !slices.Equal(got, want) {
		t.Fatalf("reschedule ticks = %v, want one run per tick %v", got, want)
	}
}

// TestTickScheduledSkipsChangedBlock pins that a scheduled update is matched
// against the block hash it was scheduled for, so replacing the block cancels
// the update, and that the stale entry is dropped from the queue regardless.
func TestTickScheduledSkipsChangedBlock(t *testing.T) {
	log := &tickNeighbourLog{}
	w := tickNeighbourWorld(t, log)

	pos := cube.Pos{7, 64, 7}
	runWorld(w, func(tx *Tx) {
		tickNeighbourPlant(tx, pos, tickNeighbourScheduled{log: log})
		tx.ScheduleBlockUpdate(pos, tickNeighbourScheduled{log: log}, time.Second/20)
		tickNeighbourPlant(tx, pos, tickNeighbourInert{})
	})
	log.reset()
	w.AdvanceTick()

	if got := log.scheduled; len(got) != 0 {
		t.Fatalf("scheduled updates run after the block changed = %v, want none", got)
	}
	runWorld(w, func(tx *Tx) {
		if got := tx.World().scheduledUpdates.fromChunk(chunkPosFromBlockPos(pos)); len(got) != 0 {
			t.Fatalf("queued scheduled ticks after the due tick = %v, want the entry dropped", got)
		}
	})
}

// currentTick reads the World's tick counter from outside a transaction.
func (w *World) currentTick() int64 {
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.CurrentTick
}

// tickNeighbourWorld returns a Synchronous World whose registry holds the
// neighbour and scheduled update markers.
func tickNeighbourWorld(t *testing.T, log *tickNeighbourLog) *World {
	t.Helper()

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_neighbour_ticker", Properties: map[string]any{}})
	registry.RegisterBlock(tickNeighbourTicker{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_neighbour_liquid", Properties: map[string]any{}})
	registry.RegisterBlock(tickNeighbourLiquid{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_neighbour_inert", Properties: map[string]any{}})
	registry.RegisterBlock(tickNeighbourInert{})
	for _, next := range tickNeighbourChainTargets {
		registry.RegisterBlockState(BlockState{Name: "test:tick_neighbour_chain", Properties: map[string]any{"target": next}})
		registry.RegisterBlock(tickNeighbourChain{log: log, next: tickNeighbourChainPos(next)})
	}
	for _, n := range []int32{0, 2} {
		registry.RegisterBlockState(BlockState{Name: "test:tick_neighbour_scheduled", Properties: map[string]any{"reschedule": n}})
		registry.RegisterBlock(tickNeighbourScheduled{log: log, reschedule: int(n)})
	}

	w := Config{Synchronous: true, Blocks: registry, RandSource: rand.NewPCG(11, 11)}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// tickNeighbourPlant writes b straight into the Column, so planting a marker
// registers no neighbour, redstone or block update of its own.
func tickNeighbourPlant(tx *Tx, pos cube.Pos, b Block) {
	col := tx.chunk(chunkPosFromBlockPos(pos))
	col.SetBlock(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0, tx.World().conf.Blocks.BlockRuntimeID(b))
}

// tickNeighbourLog records what the neighbour and scheduled passes did. The
// markers writing to it are registry values shared by one World, so a log is
// only safe to use from the goroutine driving that World.
type tickNeighbourLog struct {
	// updates holds (updated position, changed neighbour) pairs in call order.
	updates [][2]cube.Pos
	// sources records whether a call came from the first-layer block or from the
	// second-layer liquid at the same position.
	sources []string
	// scheduled holds the positions whose ScheduledTick ran, in call order.
	scheduled []cube.Pos
	// ticks holds the world tick each recorded call ran on.
	ticks []int64
}

func (l *tickNeighbourLog) reset() {
	l.updates, l.sources, l.scheduled, l.ticks = nil, nil, nil, nil
}

type tickNeighbourModel struct{}

func (tickNeighbourModel) BBox(cube.Pos, BlockSource) []cube.BBox          { return nil }
func (tickNeighbourModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool { return false }

type tickNeighbourTicker struct{ log *tickNeighbourLog }

func (b tickNeighbourTicker) NeighbourUpdateTick(pos, changed cube.Pos, tx *Tx) {
	b.log.updates = append(b.log.updates, [2]cube.Pos{pos, changed})
	b.log.sources = append(b.log.sources, "block")
	b.log.ticks = append(b.log.ticks, tx.CurrentTick())
}
func (tickNeighbourTicker) EncodeBlock() (string, map[string]any) {
	return "test:tick_neighbour_ticker", nil
}
func (tickNeighbourTicker) Hash() (uint64, uint64) { return 1 << 36, 0 }
func (tickNeighbourTicker) Model() BlockModel      { return tickNeighbourModel{} }

// tickNeighbourChainTargets enumerates the chain marker's states. The target of a
// chained update has to be encodable as a block state, so the positions it may
// point at are fixed up front.
var tickNeighbourChainTargets = []int32{5}

// tickNeighbourChainPos maps a chain marker's state to the position it registers
// an update for.
func tickNeighbourChainPos(target int32) cube.Pos {
	return cube.Pos{int(target), 64, int(target)}
}

// tickNeighbourChain registers a second neighbour update while the neighbour
// pass is running, which is how the deferral to the next tick is observed.
type tickNeighbourChain struct {
	log  *tickNeighbourLog
	next cube.Pos
}

func (b tickNeighbourChain) NeighbourUpdateTick(pos, changed cube.Pos, tx *Tx) {
	b.log.updates = append(b.log.updates, [2]cube.Pos{pos, changed})
	b.log.sources = append(b.log.sources, "block")
	b.log.ticks = append(b.log.ticks, tx.CurrentTick())
	tx.World().updateNeighbour(b.next, pos)
	// Re-arm this position too, so the chain is visible on every tick rather
	// than only the first.
	tx.World().updateNeighbour(pos, pos)
}
func (b tickNeighbourChain) EncodeBlock() (string, map[string]any) {
	return "test:tick_neighbour_chain", map[string]any{"target": int32(b.next[0])}
}
func (b tickNeighbourChain) Hash() (uint64, uint64) { return 1 << 37, uint64(b.next[0]) }
func (tickNeighbourChain) Model() BlockModel        { return tickNeighbourModel{} }

// tickNeighbourScheduled records its scheduled ticks and, when reschedule is
// non-zero, re-arms itself that many times.
type tickNeighbourScheduled struct {
	log        *tickNeighbourLog
	reschedule int
}

func (b tickNeighbourScheduled) ScheduledTick(pos cube.Pos, tx *Tx, _ *rand.Rand) {
	b.log.scheduled = append(b.log.scheduled, pos)
	b.log.ticks = append(b.log.ticks, tx.CurrentTick())
	if b.reschedule > 0 && len(b.log.scheduled) < b.reschedule+1 {
		tx.ScheduleBlockUpdate(pos, b, 0)
	}
}
func (b tickNeighbourScheduled) EncodeBlock() (string, map[string]any) {
	return "test:tick_neighbour_scheduled", map[string]any{"reschedule": int32(b.reschedule)}
}
func (b tickNeighbourScheduled) Hash() (uint64, uint64) { return 1 << 38, uint64(b.reschedule) }
func (tickNeighbourScheduled) Model() BlockModel        { return tickNeighbourModel{} }

// tickNeighbourInert is registered but implements none of the ticking
// interfaces, standing in for a block a queued update has to skip.
type tickNeighbourInert struct{}

func (tickNeighbourInert) EncodeBlock() (string, map[string]any) {
	return "test:tick_neighbour_inert", nil
}
func (tickNeighbourInert) Hash() (uint64, uint64) { return 1 << 39, 0 }
func (tickNeighbourInert) Model() BlockModel      { return tickNeighbourModel{} }

// tickNeighbourLiquid is the second-layer receiver of neighbour updates.
type tickNeighbourLiquid struct{ log *tickNeighbourLog }

func (b tickNeighbourLiquid) NeighbourUpdateTick(pos, changed cube.Pos, tx *Tx) {
	b.log.updates = append(b.log.updates, [2]cube.Pos{pos, changed})
	b.log.sources = append(b.log.sources, "liquid")
	b.log.ticks = append(b.log.ticks, tx.CurrentTick())
}
func (tickNeighbourLiquid) LiquidDepth() int                       { return 8 }
func (tickNeighbourLiquid) SpreadDecay() int                       { return 1 }
func (b tickNeighbourLiquid) WithDepth(int, bool) Liquid           { return b }
func (tickNeighbourLiquid) LiquidFalling() bool                    { return false }
func (tickNeighbourLiquid) BlastResistance() float64               { return 100 }
func (tickNeighbourLiquid) LiquidType() string                     { return "test_neighbour" }
func (tickNeighbourLiquid) Harden(cube.Pos, *Tx, *cube.Pos) bool   { return false }
func (tickNeighbourLiquid) LiquidRemoveBlock(cube.Pos, *Tx, Block) {}
func (tickNeighbourLiquid) EncodeBlock() (string, map[string]any) {
	return "test:tick_neighbour_liquid", nil
}
func (tickNeighbourLiquid) Hash() (uint64, uint64) { return 1 << 47, 0 }
func (tickNeighbourLiquid) Model() BlockModel      { return tickNeighbourModel{} }
