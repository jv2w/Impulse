package world

import (
	"math/rand/v2"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// BenchmarkTickNeighbourUpdates measures draining a full neighbour update queue.
// The queued positions hold a block that implements none of the ticking
// interfaces, so the cost measured is the queue machinery rather than the
// callbacks it dispatches to.
func BenchmarkTickNeighbourUpdates(b *testing.B) {
	const updates = 512

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_neighbour_inert", Properties: map[string]any{}})
	registry.RegisterBlock(tickNeighbourInert{})
	w := Config{Synchronous: true, Blocks: registry, RandSource: rand.NewPCG(11, 11)}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		positions := make([]cube.Pos, updates)
		for i := range positions {
			positions[i] = cube.Pos{i & 15, 64 + i>>8, i >> 4 & 15}
			tickNeighbourPlant(tx, positions[i], tickNeighbourInert{})
		}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			for _, pos := range positions {
				tx.World().updateNeighbour(pos, pos)
			}
			ticker{}.performNeighbourUpdates(tx)
		}
		b.StopTimer()
	})
}

// BenchmarkTickBlocksRandomlyBlockEntities measures the block entity half of
// tickBlocksRandomly over a set of loaded chunks. RandomTickSpeed is zero so the
// random block pass contributes nothing.
func BenchmarkTickBlocksRandomlyBlockEntities(b *testing.B) {
	const (
		chunks                = 8
		blockEntitiesPerChunk = 16
	)

	log := &tickRandomLog{}
	w := Config{
		Synchronous:     true,
		Blocks:          tickRandomRegistry(log),
		RandSource:      rand.NewPCG(0x52414e44, 0x5449434b),
		RandomTickSpeed: 0,
	}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		for c := range chunks {
			for i := range blockEntitiesPerChunk {
				tx.SetBlock(cube.Pos{c << 4, 100 + i, 0}, tickRandomBlockEntity{log: log}, nil)
			}
		}
		log.reset()

		b.ReportAllocs()
		b.ResetTimer()
		for tick := range b.N {
			ticker{}.tickBlocksRandomly(tx, nil, int64(tick))
			log.reset()
		}
		b.StopTimer()
	})
}
