package world

import (
	"testing"

	"github.com/jv2w/Impulse/server/world/chunk"
)

// encodingProvider encodes every column it is asked to store, standing in for
// the cost a real Provider pays before it writes.
type encodingProvider struct{ NopProvider }

func (encodingProvider) StoreColumn(_ ChunkPos, _ Dimension, col *chunk.Column) error {
	_ = chunk.Encode(col.Chunk, chunk.DiskEncoding)
	return nil
}

// benchmarkStoreChunk measures what storing one filled, modified chunk costs
// the goroutine that saves it.
func benchmarkStoreChunk(b *testing.B, synchronous bool) {
	w := Config{Synchronous: synchronous, Provider: encodingProvider{}, Log: tickEntityDiscardLog()}.New()
	b.Cleanup(func() { _ = w.Close() })
	c := w.columnFrom(&chunk.Column{Chunk: chunk.New(w.conf.Blocks, w.Range())}, ChunkPos{})
	stone := uint32(1)
	for y := int16(0); y < 128; y++ {
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				c.SetBlock(x, y, z, 0, stone+uint32(x^z))
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.modified = true
		w.storeChunk(ChunkPos{}, c, false)
	}
	b.StopTimer()
	if w.saver != nil {
		w.saver.flush()
	}
}

func BenchmarkStoreChunkDirect(b *testing.B) { benchmarkStoreChunk(b, true) }
func BenchmarkStoreChunkSaver(b *testing.B)  { benchmarkStoreChunk(b, false) }
