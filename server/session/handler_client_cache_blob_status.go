package session

import (
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ClientCacheBlobStatusHandler handles the ClientCacheBlobStatus packet.
type ClientCacheBlobStatusHandler struct {
}

// Handle ...
func (c *ClientCacheBlobStatusHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	pk := p.(*packet.ClientCacheBlobStatus)

	resp := &packet.ClientCacheMissResponse{Blobs: make([]protocol.CacheBlob, 0, len(pk.MissHashes))}

	s.blobMu.Lock()
	for _, hit := range pk.HitHashes {
		delete(s.blobs, hit)
		c.resolveBlob(hit, s)
	}
	for _, miss := range pk.MissHashes {
		blob, ok := s.blobs[miss]
		if !ok {
			// This is expected to happen sometimes, for example when we send the same block storage or biomes a lot of
			// times in a short timeframe. There is no need to log this, it'll just cause unnecessary noise that doesn't
			// actually aid in terms of information.
			continue
		}
		resp.Blobs = append(resp.Blobs, protocol.CacheBlob{Hash: miss, Payload: blob})
		c.resolveBlob(miss, s)
	}
	s.blobMu.Unlock()

	if len(resp.Blobs) > 0 {
		s.writePacket(resp)
	}
	return nil
}

// resolveBlob resolves a blob hash in the session passed. It assumes s.blobMu is locked upon calling.
func (c *ClientCacheBlobStatusHandler) resolveBlob(hash uint64, s *Session) {
	// Filtering in place is safe because the write index never runs ahead of
	// the read index. The dropped tail is cleared so that emptied transactions
	// are not kept alive by the spare capacity.
	leftover := s.openChunkTransactions[:0]
	for _, m := range s.openChunkTransactions {
		delete(m, hash)
		if len(m) != 0 {
			leftover = append(leftover, m)
		}
	}
	clear(s.openChunkTransactions[len(leftover):])
	s.openChunkTransactions = leftover
	delete(s.blobs, hash)
}
