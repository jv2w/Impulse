package session

import (
	"fmt"

	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// maxBlobHashes bounds the hit+miss hashes a single ClientCacheBlobStatus may
// carry. A LevelChunk advertises tens of hashes at most: 1024 is an order of
// magnitude above that while keeping one packet from running the quadratic
// resolve loop under blobMu thousands of times.
const maxBlobHashes = 1024

// ClientCacheBlobStatusHandler handles the ClientCacheBlobStatus packet.
type ClientCacheBlobStatusHandler struct {
}

// Handle ...
func (c *ClientCacheBlobStatusHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	pk := p.(*packet.ClientCacheBlobStatus)
	// Which input was the problem: HitHashes and MissHashes were looped with
	// no length check, each hash scanning every open transaction under blobMu.
	if l := len(pk.HitHashes) + len(pk.MissHashes); l > maxBlobHashes {
		return fmt.Errorf("too many blob hashes: %d", l)
	}

	// The response is only built when a blob is actually going to be sent. The
	// blob list is collected the same way either way, so the packet carries
	// the same slice it did; the run that sends nothing now allocates no
	// packet at all.
	blobs := make([]protocol.CacheBlob, 0, len(pk.MissHashes))

	s.blobMu.Lock()
	for _, hit := range pk.HitHashes {
		// resolveBlob ends with the same delete on the same hash, and a delete
		// of a key that is already gone does nothing, so dropping the one here
		// leaves the map in the same state.
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
		blobs = append(blobs, protocol.CacheBlob{Hash: miss, Payload: blob})
		c.resolveBlob(miss, s)
	}
	s.blobMu.Unlock()

	if len(blobs) > 0 {
		s.writePacket(&packet.ClientCacheMissResponse{Blobs: blobs})
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
