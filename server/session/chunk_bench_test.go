package session

import (
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// BenchmarkSubChunkEntry measures one entry of a sub-chunk request, with and
// without a block entity in the sub-chunk being sent. Terrain streaming answers
// mostly with the first shape.
func BenchmarkSubChunkEntry(b *testing.B) {
	br := sharedRegistry()
	c := chunk.New(br, world.Range())
	c.SetBlock(0, 0, 0, 0, br.BlockRuntimeID(block.Stone{}))
	ind := c.SubIndex(0)

	for _, bc := range []struct {
		name     string
		entities map[cube.Pos]world.Block
	}{
		{"NoBlockEntities", nil},
		{"OneBlockEntity", map[cube.Pos]world.Block{{1, 2, 3}: block.Chest{}}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			s := &Session{conn: &stubConn{}, blobs: map[uint64][]byte{}}
			col := &world.Column{Chunk: c, BlockEntities: bc.entities}
			transaction := map[uint64]struct{}{}

			b.ReportAllocs()
			for range b.N {
				s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, transaction)
			}
		})
	}
}
