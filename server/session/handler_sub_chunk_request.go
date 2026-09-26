package session

import (
	"fmt"

	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// maxSubChunkOffsets bounds the offsets a single SubChunkRequest may carry.
// A legitimate client requests the missing sub-chunks around its position,
// tens at most: 512 is several times that while keeping one packet from
// forcing thousands of sub-chunk encodes on the tick path.
const maxSubChunkOffsets = 512

// SubChunkRequestHandler handles sub-chunk requests from the client. The server will respond with a packet containing
// the requested sub-chunks.
type SubChunkRequestHandler struct{}

// Handle ...
func (*SubChunkRequestHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*packet.SubChunkRequest)
	// Which input was the problem: pk.Offsets reached ViewSubChunks with no
	// length check, so one packet fanned out to one encode per offset.
	if len(pk.Offsets) > maxSubChunkOffsets {
		return fmt.Errorf("too many sub-chunk offsets: %d", len(pk.Offsets))
	}
	if dimID, _ := world.DimensionID(tx.World().Dimension()); pk.Dimension != int32(dimID) {
		// Outdated sub chunk request from a previous dimension.
		s.writePacket(&packet.SubChunk{
			Dimension:       pk.Dimension,
			Position:        pk.Position,
			CacheEnabled:    s.conn.ClientCacheEnabled(),
			SubChunkEntries: []protocol.SubChunkEntry{},
		})
		return nil
	}
	s.ViewSubChunks(world.SubChunkPos(pk.Position), pk.Offsets, tx)
	return nil
}
