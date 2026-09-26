package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// TestSubChunkRequestRejectsTooManyOffsets pins the per-packet bound: a
// SubChunkRequest carrying more offsets than a legitimate client ever sends
// must be rejected before any chunk encode runs. Before the fix the offsets
// reached ViewSubChunks unchecked, so one small packet could force thousands
// of sub-chunk encodes on the tick path.
func TestSubChunkRequestRejectsTooManyOffsets(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := newStubSession(t, nil)
	if err := w.Do(func(tx *world.Tx) {
		s.chunkLoader = world.NewLoader(1, tx.World(), s)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("create chunk loader: %v", err)
	}
	h := &SubChunkRequestHandler{}
	offsets := make([]protocol.SubChunkOffset, 2048)
	var handleErr error
	if err := w.Do(func(tx *world.Tx) {
		dim, _ := world.DimensionID(tx.World().Dimension())
		pk := &packet.SubChunkRequest{Dimension: int32(dim), Position: protocol.SubChunkPos{0, 0, 0}, Offsets: offsets}
		handleErr = h.Handle(pk, s, tx, nil)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("world tx: %v", err)
	}
	if handleErr == nil {
		t.Fatalf("SubChunkRequest with %d offsets succeeded, want rejection", len(offsets))
	}
}

// TestClientCacheBlobStatusRejectsTooManyHashes pins the per-packet bound:
// hit+miss hashes beyond the cap must be rejected before the quadratic
// resolve loop runs under blobMu. Before the fix one 64KB packet (≈8000
// hashes) stalled chunk streaming for the session.
func TestClientCacheBlobStatusRejectsTooManyHashes(t *testing.T) {
	s := newStubSession(t, nil)
	s.openChunkTransactions = nil
	s.blobs = map[uint64][]byte{}
	h := &ClientCacheBlobStatusHandler{}
	pk := &packet.ClientCacheBlobStatus{
		HitHashes:  make([]uint64, 4096),
		MissHashes: make([]uint64, 4096),
	}
	if err := h.Handle(pk, s, nil, nil); err == nil {
		t.Fatalf("ClientCacheBlobStatus with %d hashes succeeded, want rejection", len(pk.HitHashes)+len(pk.MissHashes))
	}
}
