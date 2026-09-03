package world

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// TestTickRandomAnyWithinDistance pins the chunk selection predicate as a
// squared Euclidean test on chunk coordinates: the simulation distance describes
// a circle, not a square, so a chunk on the diagonal at r,r is out of range.
func TestTickRandomAnyWithinDistance(t *testing.T) {
	loaded := []ChunkPos{{0, 0}}
	tests := []struct {
		name string
		pos  ChunkPos
		r    int32
		want bool
	}{
		{name: "same chunk", pos: ChunkPos{0, 0}, r: 6, want: true},
		{name: "on axis at r", pos: ChunkPos{6, 0}, r: 6, want: true},
		{name: "on axis past r", pos: ChunkPos{7, 0}, r: 6, want: false},
		{name: "negative axis at r", pos: ChunkPos{0, -6}, r: 6, want: true},
		{name: "diagonal inside circle", pos: ChunkPos{4, 4}, r: 6, want: true},
		{name: "diagonal outside circle", pos: ChunkPos{5, 5}, r: 6, want: false},
		{name: "radius zero same chunk", pos: ChunkPos{0, 0}, r: 0, want: true},
		{name: "radius zero neighbour", pos: ChunkPos{1, 0}, r: 0, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := (ticker{}).anyWithinDistance(test.pos, loaded, test.r); got != test.want {
				t.Fatalf("anyWithinDistance(%v, %v, %d) = %t, want %t", test.pos, loaded, test.r, got, test.want)
			}
		})
	}
}

// TestTickRandomZeroSimulationDistanceSkipsBlockEntities pins that a simulation
// distance of 0 stops block entity ticking as well as random ticking: the early
// return sits above the block entity collection, so the two passes share one
// switch even though only one of them is random.
func TestTickRandomZeroSimulationDistanceSkipsBlockEntities(t *testing.T) {
	log := &tickRandomLog{}
	w := tickRandomWorld(t, log, 3)
	w.SetTickRange(0)

	runWorld(w, func(tx *Tx) {
		tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomTicker{log: log})
		tx.SetBlock(cube.Pos{2, 100, 2}, tickRandomBlockEntity{log: log}, nil)
	})
	log.reset()
	w.AdvanceTick()

	if len(log.random) != 0 {
		t.Fatalf("random ticks at simulation distance 0 = %v, want none", log.random)
	}
	if len(log.blockEntity) != 0 {
		t.Fatalf("block entity ticks at simulation distance 0 = %v, want none", log.blockEntity)
	}
}

// TestTickRandomSpeedControlsCountPerChunk pins that a chunk yields exactly
// RandomTickSpeed random ticks when every block of its only non-empty sub chunk
// is random-tickable, and that the picked positions stay inside that sub chunk.
func TestTickRandomSpeedControlsCountPerChunk(t *testing.T) {
	for _, speed := range []int{1, 3, 5} {
		t.Run(map[int]string{1: "speed 1", 3: "speed 3", 5: "speed 5"}[speed], func(t *testing.T) {
			log := &tickRandomLog{}
			w := tickRandomWorld(t, log, speed)
			runWorld(w, func(tx *Tx) { tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomTicker{log: log}) })
			log.reset()
			w.AdvanceTick()

			if got := len(log.random); got != speed {
				t.Fatalf("random ticks = %d, want %d; positions=%v", got, speed, log.random)
			}
			for _, pos := range log.random {
				if pos[0] < 0 || pos[0] > 15 || pos[2] < 0 || pos[2] > 15 || pos[1] < 64 || pos[1] > 79 {
					t.Fatalf("random tick at %v, want a position inside the filled sub chunk", pos)
				}
			}
		})
	}
}

// TestTickRandomOnlyRandomTickerBlocksAreTicked pins that the random pass keys
// off the registry's random-tick flag: a sub chunk filled with a registered
// block that does not implement RandomTicker produces no ticks at all.
func TestTickRandomOnlyRandomTickerBlocksAreTicked(t *testing.T) {
	log := &tickRandomLog{}
	w := tickRandomWorld(t, log, 5)

	runWorld(w, func(tx *Tx) { tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomInert{}) })
	log.reset()
	w.AdvanceTick()

	if len(log.random) != 0 {
		t.Fatalf("random ticks on non-RandomTicker blocks = %v, want none", log.random)
	}
}

// TestTickRandomBlockEntitiesTickAfterRandomBlocks pins the split inside
// tickBlocksRandomly: every block entity of a selected chunk is ticked, and all
// of them run after the last random block tick.
func TestTickRandomBlockEntitiesTickAfterRandomBlocks(t *testing.T) {
	log := &tickRandomLog{}
	w := tickRandomWorld(t, log, 3)

	entityPositions := []cube.Pos{{1, 100, 1}, {2, 100, 2}, {3, 100, 3}}
	runWorld(w, func(tx *Tx) {
		tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomTicker{log: log})
		for _, pos := range entityPositions {
			tx.SetBlock(pos, tickRandomBlockEntity{log: log}, nil)
		}
	})
	log.reset()
	w.AdvanceTick()

	if got := len(log.random); got != 3 {
		t.Fatalf("random ticks = %d, want 3", got)
	}
	if got := len(log.blockEntity); got != len(entityPositions) {
		t.Fatalf("block entity ticks = %v, want one per planted block entity %v", log.blockEntity, entityPositions)
	}
	// Block entity ticking is a map range over Column.BlockEntities, so only the
	// set is pinned here, not the order within the pass.
	got := slices.Clone(log.blockEntity)
	slices.SortFunc(got, compareBlockPos)
	if !slices.Equal(got, entityPositions) {
		t.Fatalf("block entity ticks = %v, want %v", got, entityPositions)
	}
	firstBlockEntity, lastRandom := slices.Index(log.stages, "blockEntity"), -1
	for i, stage := range log.stages {
		if stage == "random" {
			lastRandom = i
		}
	}
	if firstBlockEntity < lastRandom {
		t.Fatalf("block entity pass started before the random pass finished: %v", log.stages)
	}
}

// TestTickRandomSynchronousUsesAllLoadedChunks pins that a Synchronous World
// derives its loaded set from every loaded chunk rather than from loaders, so a
// chunk far beyond any simulation distance still ticks.
func TestTickRandomSynchronousUsesAllLoadedChunks(t *testing.T) {
	log := &tickRandomLog{}
	w := tickRandomWorld(t, log, 1)

	far := ChunkPos{100, 100}
	runWorld(w, func(tx *Tx) { tickRandomFillSub(tx, far, tickRandomTicker{log: log}) })
	log.reset()
	w.AdvanceTick()

	if got := len(log.random); got != 1 {
		t.Fatalf("random ticks in a chunk with no loader = %d, want 1", got)
	}
	if got := chunkPosFromBlockPos(log.random[0]); got != far {
		t.Fatalf("random tick in chunk %v, want %v", got, far)
	}
}

// TestTickRandomLoaderDistanceSelectsChunks pins the non-Synchronous path: the
// loaded set is built from loader positions, so a loaded chunk outside every
// loader's simulation distance is skipped.
//
// The World is non-Synchronous but has no viewers, so its own tick loop returns
// before running the pipeline; tickBlocksRandomly is driven directly instead,
// with a Loader that is only ever read for its position.
func TestTickRandomLoaderDistanceSelectsChunks(t *testing.T) {
	log := &tickRandomLog{}
	registry := tickRandomRegistry(log)
	w := Config{Blocks: registry, RandSource: rand.NewPCG(7, 7), RandomTickSpeed: 1}.New()
	t.Cleanup(func() { _ = w.Close() })

	near, far := ChunkPos{0, 0}, ChunkPos{20, 0}
	runWorld(w, func(tx *Tx) {
		tickRandomFillSub(tx, near, tickRandomTicker{log: log})
		tickRandomFillSub(tx, far, tickRandomTicker{log: log})
	})
	log.reset()
	runWorld(w, func(tx *Tx) {
		ticker{}.tickBlocksRandomly(tx, []*Loader{{pos: near}}, 1)
	})

	if got := len(log.random); got != 1 {
		t.Fatalf("random ticks = %d, want 1; positions=%v", got, log.random)
	}
	if got := chunkPosFromBlockPos(log.random[0]); got != near {
		t.Fatalf("random tick in chunk %v, want only the chunk within the loader's range %v", got, near)
	}
}

// TestTickRandomSingleChunkIsReproducibleFromSeed pins that the coordinates
// picked by the random pass are a pure function of Config.RandSource as long as
// exactly one chunk is loaded. Without it there is no way to tell a rewrite of
// the pass from a behaviour change.
func TestTickRandomSingleChunkIsReproducibleFromSeed(t *testing.T) {
	want := []cube.Pos{{12, 66, 3}, {4, 67, 11}, {10, 67, 13}, {2, 72, 7}, {12, 77, 6}}

	run := func() []cube.Pos {
		log := &tickRandomLog{}
		w := tickRandomWorld(t, log, 1)
		w.StopWeatherCycle()
		runWorld(w, func(tx *Tx) { tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomTicker{log: log}) })
		log.reset()
		for range 5 {
			w.AdvanceTick()
		}
		return log.random
	}

	first, second := run(), run()
	if !slices.Equal(first, want) {
		t.Fatalf("random tick positions = %v, want golden %v", first, want)
	}
	if !slices.Equal(first, second) {
		t.Fatalf("two Worlds on the same seed picked %v and %v, want identical sequences", first, second)
	}
}

// TestTickRandomMultipleChunksAreNotReproducibleFromSeed pins that seeding only
// fixes the random pass while a single chunk is loaded. Coordinates come from
// one shared stream consumed in World.chunks map order, and the number of draws
// a chunk takes depends on whether it held a tickable block, so which chunk gets
// which coordinates changes from run to run.
//
// PINNED BUG: random block selection is not reproducible from Config.RandSource
// once two or more chunks are loaded, and the coordinates a chunk receives
// depend on Go map iteration order.
func TestTickRandomMultipleChunksAreNotReproducibleFromSeed(t *testing.T) {
	// Chunk empty holds a non-empty sub chunk of blocks that never tick, so it
	// consumes one coordinate triple and no more, while chunk full consumes two.
	// Visiting them in the opposite order therefore hands full a different
	// coordinate.
	run := func() []cube.Pos {
		log := &tickRandomLog{}
		w := tickRandomWorld(t, log, 1)
		w.StopWeatherCycle()
		runWorld(w, func(tx *Tx) {
			tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomTicker{log: log})
			tickRandomFillSub(tx, ChunkPos{1, 0}, tickRandomInert{})
		})
		log.reset()
		for range 100 {
			w.AdvanceTick()
		}
		return log.random
	}

	// Both runs share a seed and a world layout. Agreeing on all 100 ticks would
	// require the two-element map range to pick the same start 100 times in a
	// row, which the runtime randomises per range.
	if first, second := run(), run(); slices.Equal(first, second) {
		t.Fatalf("two Worlds on the same seed produced identical sequences over 100 ticks; "+
			"chunk iteration order was expected to perturb the shared random stream: %v", first)
	}
}

// TestTickRandomNibbleBufferIsPerTick pins how the random pass consumes the
// World's random source: randUint4 refills from one Uint64 per 16 nibbles and is
// created fresh inside tickBlocksRandomly, so leftover nibbles are dropped at the
// end of every tick rather than carried into the next one.
func TestTickRandomNibbleBufferIsPerTick(t *testing.T) {
	tests := []struct {
		name      string
		speed     int
		wantDraws int
	}{
		// Each iteration draws a coordinate triple, hits a tickable block, then
		// redraws: 6 nibbles per iteration, 16 nibbles per Uint64.
		{name: "6 nibbles", speed: 1, wantDraws: 1},
		{name: "18 nibbles", speed: 3, wantDraws: 2},
		{name: "30 nibbles", speed: 5, wantDraws: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &tickRandomLog{}
			src := &tickRandomCountingSource{src: rand.NewPCG(3, 3)}
			w := Config{
				Synchronous:     true,
				Blocks:          tickRandomRegistry(log),
				RandSource:      src,
				RandomTickSpeed: test.speed,
			}.New()
			t.Cleanup(func() { _ = w.Close() })
			w.StopWeatherCycle()

			runWorld(w, func(tx *Tx) { tickRandomFillSub(tx, ChunkPos{0, 0}, tickRandomTicker{log: log}) })
			before := src.draws
			w.AdvanceTick()

			if got := src.draws - before; got != test.wantDraws {
				t.Fatalf("random source draws in one tick = %d, want %d", got, test.wantDraws)
			}
		})
	}
}

// tickRandomWorld returns a Synchronous World whose registry holds the random
// tick markers, with a fixed random source and the RandomTickSpeed passed.
func tickRandomWorld(t *testing.T, log *tickRandomLog, speed int) *World {
	t.Helper()

	w := Config{
		Synchronous:     true,
		Blocks:          tickRandomRegistry(log),
		RandSource:      rand.NewPCG(0x52414e44, 0x5449434b),
		RandomTickSpeed: speed,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// tickRandomRegistry returns a registry holding the three marker blocks, each
// carrying log so calls can be observed without package-level state.
func tickRandomRegistry(log *tickRandomLog) BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_random_ticker", Properties: map[string]any{}})
	registry.RegisterBlock(tickRandomTicker{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_random_block_entity", Properties: map[string]any{}})
	registry.RegisterBlock(tickRandomBlockEntity{log: log})
	registry.RegisterBlockState(BlockState{Name: "test:tick_random_inert", Properties: map[string]any{}})
	registry.RegisterBlock(tickRandomInert{})
	return registry
}

// tickRandomFillSub fills the sub chunk covering y 64 to 79 of the chunk at pos
// with b. It writes through the Column so no block, redstone or neighbour update
// is registered, leaving the tick pipeline observing nothing but the fill.
func tickRandomFillSub(tx *Tx, pos ChunkPos, b Block) {
	rid := tx.World().conf.Blocks.BlockRuntimeID(b)
	col := tx.chunk(pos)
	for x := range 16 {
		for z := range 16 {
			for y := 64; y < 80; y++ {
				col.SetBlock(uint8(x), int16(y), uint8(z), 0, rid)
			}
		}
	}
}

// tickRandomLog records the positions the two passes of tickBlocksRandomly
// visited. The markers writing to it are registry values shared by one World, so
// a log is only safe to use from the goroutine driving that World.
type tickRandomLog struct {
	random      []cube.Pos
	blockEntity []cube.Pos
	// stages records the pass each call belonged to, in call order, so the
	// boundary between the two passes can be asserted.
	stages []string
}

func (l *tickRandomLog) reset() {
	l.random, l.blockEntity, l.stages = nil, nil, nil
}

type tickRandomModel struct{}

func (tickRandomModel) BBox(cube.Pos, BlockSource) []cube.BBox          { return nil }
func (tickRandomModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool { return false }

type tickRandomTicker struct{ log *tickRandomLog }

func (b tickRandomTicker) RandomTick(pos cube.Pos, _ *Tx, _ *rand.Rand) {
	b.log.random = append(b.log.random, pos)
	b.log.stages = append(b.log.stages, "random")
}
func (tickRandomTicker) EncodeBlock() (string, map[string]any) {
	return "test:tick_random_ticker", nil
}
func (tickRandomTicker) Hash() (uint64, uint64) { return 1 << 33, 0 }
func (tickRandomTicker) Model() BlockModel      { return tickRandomModel{} }

type tickRandomBlockEntity struct{ log *tickRandomLog }

func (b tickRandomBlockEntity) Tick(_ int64, pos cube.Pos, _ *Tx) {
	b.log.blockEntity = append(b.log.blockEntity, pos)
	b.log.stages = append(b.log.stages, "blockEntity")
}
func (b tickRandomBlockEntity) DecodeNBT(map[string]any) any { return b }
func (tickRandomBlockEntity) EncodeNBT() map[string]any      { return nil }
func (tickRandomBlockEntity) EncodeBlock() (string, map[string]any) {
	return "test:tick_random_block_entity", nil
}
func (tickRandomBlockEntity) Hash() (uint64, uint64) { return 1 << 34, 0 }
func (tickRandomBlockEntity) Model() BlockModel      { return tickRandomModel{} }

// tickRandomInert is registered but implements neither RandomTicker nor
// TickerBlock, so a sub chunk filled with it is non-empty yet never ticks.
type tickRandomInert struct{}

func (tickRandomInert) EncodeBlock() (string, map[string]any) {
	return "test:tick_random_inert", nil
}
func (tickRandomInert) Hash() (uint64, uint64) { return 1 << 35, 0 }
func (tickRandomInert) Model() BlockModel      { return tickRandomModel{} }

// tickRandomCountingSource counts how often the World pulls from its random
// source, so the nibble buffering of randUint4 can be observed.
type tickRandomCountingSource struct {
	src   rand.Source
	draws int
}

func (s *tickRandomCountingSource) Uint64() uint64 {
	s.draws++
	return s.src.Uint64()
}
