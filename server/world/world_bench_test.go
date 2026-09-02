package world

import (
	"math/rand/v2"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// BenchmarkSetBlock measures placing and breaking a block on a chunk with no
// viewers, with the nil SetOpts every ordinary caller passes.
func BenchmarkSetBlock(b *testing.B) {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_random_inert", Properties: map[string]any{}})
	registry.RegisterBlock(tickRandomInert{})
	w := Config{Synchronous: true, Blocks: registry, RandSource: rand.NewPCG(3, 3)}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		pos := cube.Pos{8, 64, 8}
		solid, air := Block(tickRandomInert{}), tx.World().conf.Blocks.Air()
		tx.SetBlock(pos, solid, nil)

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			tx.SetBlock(pos, air, nil)
			tx.SetBlock(pos, solid, nil)
			// Nothing drains the neighbour queue within a single transaction, so
			// it is truncated here to keep its unbounded growth out of the
			// measurement.
			tx.World().neighbourUpdates = tx.World().neighbourUpdates[:0]
		}
		b.StopTimer()
	})
}
