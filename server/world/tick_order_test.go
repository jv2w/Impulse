package world

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
)

// tickOrderStages is the golden execution order of ticker.tick. Every name is
// emitted by a marker planted in exactly one pipeline stage, so the slice
// records the order in which tick runs its stages, and the split inside
// tickBlocksRandomly between random block ticks and block entity ticks.
//
// Stage 2.3 (tick decomposition) and 2.4 (queue dissolution) rewrite the
// pipeline. This order is the contract they must preserve: reordering any two
// stages changes redstone, liquid flow and gravity block behaviour in ways only
// a built contraption reveals.
var tickOrderStages = []string{
	"entity",      // ticker.tickEntities
	"scheduled",   // World.scheduledUpdates.tick
	"random",      // ticker.tickBlocksRandomly, random block pass
	"blockEntity", // ticker.tickBlocksRandomly, block entity pass
	"neighbour",   // ticker.performNeighbourUpdates
	"redstone",    // World.redstone.tick
}

// TestTickStageOrder pins the order in which ticker.tick runs its stages by
// planting one marker per stage and asserting the sequence they record during a
// single AdvanceTick.
func TestTickStageOrder(t *testing.T) {
	log := &tickOrderLog{}
	w := tickOrderWorld(t, log)

	runWorld(w, func(tx *Tx) { tickOrderPlant(tx, log) })
	log.events = nil
	w.AdvanceTick()

	if got := log.events; !slices.Equal(got, tickOrderStages) {
		t.Fatalf("tick stage order = %v, want %v", got, tickOrderStages)
	}
}

// TestTickStageOrderStableAcrossTicks verifies the stage order does not depend
// on which tick it is: markers re-armed before every tick record the same
// sequence on each of several consecutive ticks.
func TestTickStageOrderStableAcrossTicks(t *testing.T) {
	log := &tickOrderLog{}
	w := tickOrderWorld(t, log)

	var scheduledPos, neighbourPos cube.Pos
	runWorld(w, func(tx *Tx) { scheduledPos, neighbourPos = tickOrderPlant(tx, log) })
	// Consume the one-shot arms left by planting so each loop iteration starts
	// from an empty scheduled queue, neighbour queue and redstone dirty set.
	w.AdvanceTick()

	for tick := range 3 {
		// The redstone action only runs on a power change, so the source is
		// toggled rather than merely invalidated. It starts out at 15.
		power := 0
		if tick%2 == 1 {
			power = 15
		}
		runWorld(w, func(tx *Tx) {
			tx.ScheduleBlockUpdate(scheduledPos, tickOrderScheduled{log: log}, time.Second/20)
			tx.World().updateNeighbour(neighbourPos, neighbourPos)
			tx.SetBlock(tickOrderSourcePos, tickOrderRedstoneSource{Power: power}, nil)
		})
		log.events = nil
		w.AdvanceTick()

		if got := log.events; !slices.Equal(got, tickOrderStages) {
			t.Fatalf("tick %d stage order = %v, want %v", tick, got, tickOrderStages)
		}
	}
}

// TestTickDeferredCallbacksRunAfterAllStages pins where work handed to Tx.Defer
// lands: the tick transaction drains its deferred queue only after ticker.tick
// has returned, so deferred callbacks run after every stage rather than between
// the stage that registered them and the next one.
func TestTickDeferredCallbacksRunAfterAllStages(t *testing.T) {
	log := &tickOrderLog{}
	w := tickOrderWorld(t, log)

	runWorld(w, func(tx *Tx) { tickOrderPlant(tx, log) })
	log.events, log.deferNext = nil, true
	w.AdvanceTick()

	want := append(slices.Clone(tickOrderStages), "deferred")
	if got := log.events; !slices.Equal(got, want) {
		t.Fatalf("tick order with deferred work = %v, want %v", got, want)
	}
}

// TestTickStageMarkersFireExactlyOnce guards TestTickStageOrder itself: if a
// marker stopped firing, the order assertion would silently cover fewer stages.
func TestTickStageMarkersFireExactlyOnce(t *testing.T) {
	log := &tickOrderLog{}
	w := tickOrderWorld(t, log)

	runWorld(w, func(tx *Tx) { tickOrderPlant(tx, log) })
	log.events = nil
	w.AdvanceTick()

	for _, stage := range tickOrderStages {
		if got := countOccurrences(log.events, stage); got != 1 {
			t.Fatalf("stage %q ran %d time(s) in one tick, want 1; events=%v", stage, got, log.events)
		}
	}
}

// tickOrderSourcePos and tickOrderActionPos are the positions of the redstone
// stage markers. They are package-level so re-arming the redstone stage does not
// need to thread them through.
var (
	tickOrderSourcePos = cube.Pos{9, 64, 9}
	tickOrderActionPos = cube.Pos{10, 64, 9}
)

// tickOrderWorld returns a Synchronous World with a registry holding the six
// stage markers, a fixed random source and exactly one random block tick per
// chunk per tick.
func tickOrderWorld(t *testing.T, log *tickOrderLog) *World {
	t.Helper()

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_order_scheduled", Properties: map[string]any{}})
	registry.RegisterBlock(tickOrderScheduled{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_order_random", Properties: map[string]any{}})
	registry.RegisterBlock(tickOrderRandom{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_order_block_entity", Properties: map[string]any{}})
	registry.RegisterBlock(tickOrderBlockEntity{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_order_neighbour", Properties: map[string]any{}})
	registry.RegisterBlock(tickOrderNeighbour{log: log})
	for _, power := range []int32{0, 15} {
		registry.RegisterBlockState(BlockState{Name: "test:tick_order_source", Properties: map[string]any{"power": power}})
		registry.RegisterBlock(tickOrderRedstoneSource{Power: int(power)})
	}
	registry.RegisterBlockState(BlockState{Name: "test:tick_order_action", Properties: map[string]any{}})
	registry.RegisterBlock(tickOrderRedstoneAction{log: log})

	w := Config{
		Synchronous:     true,
		Blocks:          registry,
		RandSource:      rand.NewPCG(0x54494b, 0x4f524445),
		RandomTickSpeed: 1,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// tickOrderPlant places one marker per pipeline stage and arms the one-shot
// stages. It returns the positions of the scheduled and neighbour markers so
// callers can re-arm them.
func tickOrderPlant(tx *Tx, log *tickOrderLog) (scheduledPos, neighbourPos cube.Pos) {
	scheduledPos, neighbourPos = cube.Pos{1, 64, 1}, cube.Pos{7, 64, 7}

	tx.AddEntity(EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(tickOrderEntityType{}, tickOrderEntityConfig{log: log}))
	tx.SetBlock(scheduledPos, tickOrderScheduled{log: log}, nil)
	tx.ScheduleBlockUpdate(scheduledPos, tickOrderScheduled{log: log}, time.Second/20)
	tx.SetBlock(cube.Pos{5, 64, 5}, tickOrderBlockEntity{log: log}, nil)
	tx.SetBlock(neighbourPos, tickOrderNeighbour{log: log}, nil)
	tx.SetBlock(tickOrderSourcePos, tickOrderRedstoneSource{Power: 15}, nil)
	tx.SetBlock(tickOrderActionPos, tickOrderRedstoneAction{log: log}, nil)

	// The random pass picks a uniformly random position per sub chunk, so a
	// single planted block would only be hit by luck. Filling one sub chunk of a
	// separate chunk makes the hit certain, and RandomTickSpeed 1 makes it
	// happen exactly once for that chunk.
	rid := tx.World().conf.Blocks.BlockRuntimeID(tickOrderRandom{log: log})
	col := tx.chunk(ChunkPos{2, 0})
	for x := range 16 {
		for z := range 16 {
			for y := 64; y < 80; y++ {
				col.SetBlock(uint8(x), int16(y), uint8(z), 0, rid)
			}
		}
	}
	return scheduledPos, neighbourPos
}

// countOccurrences returns how often v appears in s.
func countOccurrences[E comparable](s []E, v E) int {
	n := 0
	for _, e := range s {
		if e == v {
			n++
		}
	}
	return n
}

// tickOrderLog records the stages a tick ran through. The markers writing to it
// are registry values shared by one World, so a log is only safe to use from the
// goroutine driving that Synchronous World.
type tickOrderLog struct {
	events []string
	// deferNext arms the entity marker to hand one callback to Tx.Defer on its
	// next tick, so the position of deferred work relative to the stages can be
	// observed.
	deferNext bool
}

func (l *tickOrderLog) record(stage string) {
	l.events = append(l.events, stage)
}

type tickOrderModel struct{}

func (tickOrderModel) BBox(cube.Pos, BlockSource) []cube.BBox          { return nil }
func (tickOrderModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool { return false }

type tickOrderScheduled struct{ log *tickOrderLog }

func (b tickOrderScheduled) ScheduledTick(cube.Pos, *Tx, *rand.Rand) { b.log.record("scheduled") }
func (tickOrderScheduled) EncodeBlock() (string, map[string]any) {
	return "test:tick_order_scheduled", nil
}
func (tickOrderScheduled) Hash() (uint64, uint64) { return 1 << 48, 0 }
func (tickOrderScheduled) Model() BlockModel      { return tickOrderModel{} }

type tickOrderRandom struct{ log *tickOrderLog }

func (b tickOrderRandom) RandomTick(cube.Pos, *Tx, *rand.Rand) { b.log.record("random") }
func (tickOrderRandom) EncodeBlock() (string, map[string]any) {
	return "test:tick_order_random", nil
}
func (tickOrderRandom) Hash() (uint64, uint64) { return 1 << 49, 0 }
func (tickOrderRandom) Model() BlockModel      { return tickOrderModel{} }

type tickOrderBlockEntity struct{ log *tickOrderLog }

func (b tickOrderBlockEntity) Tick(int64, cube.Pos, *Tx) { b.log.record("blockEntity") }
func (b tickOrderBlockEntity) DecodeNBT(map[string]any) any {
	return b
}
func (tickOrderBlockEntity) EncodeNBT() map[string]any { return nil }
func (tickOrderBlockEntity) EncodeBlock() (string, map[string]any) {
	return "test:tick_order_block_entity", nil
}
func (tickOrderBlockEntity) Hash() (uint64, uint64) { return 1 << 50, 0 }
func (tickOrderBlockEntity) Model() BlockModel      { return tickOrderModel{} }

type tickOrderNeighbour struct{ log *tickOrderLog }

func (b tickOrderNeighbour) NeighbourUpdateTick(cube.Pos, cube.Pos, *Tx) { b.log.record("neighbour") }
func (tickOrderNeighbour) EncodeBlock() (string, map[string]any) {
	return "test:tick_order_neighbour", nil
}
func (tickOrderNeighbour) Hash() (uint64, uint64) { return 1 << 51, 0 }
func (tickOrderNeighbour) Model() BlockModel      { return tickOrderModel{} }

type tickOrderRedstoneSource struct{ Power int }

func (b tickOrderRedstoneSource) RedstonePower(cube.Pos, *Tx, cube.Face) int { return b.Power }
func (b tickOrderRedstoneSource) EncodeBlock() (string, map[string]any) {
	return "test:tick_order_source", map[string]any{"power": int32(b.Power)}
}
func (b tickOrderRedstoneSource) Hash() (uint64, uint64) { return 1 << 52, uint64(b.Power) }
func (tickOrderRedstoneSource) Model() BlockModel        { return tickOrderModel{} }

type tickOrderRedstoneAction struct{ log *tickOrderLog }

func (b tickOrderRedstoneAction) RedstonePowerAction(cube.Pos, *Tx, int, int) {
	b.log.record("redstone")
}
func (tickOrderRedstoneAction) EncodeBlock() (string, map[string]any) {
	return "test:tick_order_action", nil
}
func (tickOrderRedstoneAction) Hash() (uint64, uint64) { return 1 << 53, 0 }
func (tickOrderRedstoneAction) Model() BlockModel      { return tickOrderModel{} }

type tickOrderEntityConfig struct{ log *tickOrderLog }

func (c tickOrderEntityConfig) Apply(data *EntityData) { data.Data = c.log }

type tickOrderEntityType struct{}

func (tickOrderEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &tickOrderEntity{handle: handle, data: data}
}
func (tickOrderEntityType) EncodeEntity() string                  { return "test:tick_order_entity" }
func (tickOrderEntityType) BBox(Entity) cube.BBox                 { return cube.BBox{} }
func (tickOrderEntityType) DecodeNBT(map[string]any, *EntityData) {}
func (tickOrderEntityType) EncodeNBT(*EntityData) map[string]any  { return nil }

type tickOrderEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (e *tickOrderEntity) Close() error            { return nil }
func (e *tickOrderEntity) H() *EntityHandle        { return e.handle }
func (e *tickOrderEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *tickOrderEntity) Rotation() cube.Rotation { return e.data.Rot }
func (e *tickOrderEntity) Tick(tx *Tx, _ int64) {
	log := e.data.Data.(*tickOrderLog)
	log.record("entity")
	if log.deferNext {
		log.deferNext = false
		tx.Defer(func(*Tx) { log.record("deferred") })
	}
}
