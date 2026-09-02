package session

import (
	"bytes"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The chunk send path is the one place where what goes on the wire depends on a
// client capability (the client-side blob cache) rather than on world state.
// What is pinned here is which of the two encodings a session picks and what it
// puts in the packet, because CLAUDE.md 2.4 rewrites this path.

// TestSubChunkRequestsIsEnabled pins the package constant that decides the
// encoding. With it set, both send paths return a header-only LevelChunk and
// the client asks for sub-chunks separately.
//
// PINNED SCOPE: subChunkRequests is a compile-time const true, so the full
// chunk encodings below it in sendNetworkChunk and sendBlobHashes are dead
// code that no test can reach. Out of scope per CLAUDE.md 2.6.
func TestSubChunkRequestsIsEnabled(t *testing.T) {
	if !subChunkRequests {
		t.Fatal("subChunkRequests = false; the pinned behaviour below assumes it is true")
	}
}

// TestDimensionID pins the dimension IDs the client is told, and the fallback
// for a dimension that is not registered.
func TestDimensionID(t *testing.T) {
	s := newStubSession(t, nil)

	tests := []struct {
		name string
		dim  world.Dimension
		want int32
	}{
		{name: "overworld", dim: world.Overworld, want: 0},
		{name: "nether", dim: world.Nether, want: 1},
		{name: "end", dim: world.End, want: 2},
		{
			// PINNED BUG: the lookup's ok result is discarded, so an
			// unregistered dimension is reported as the overworld rather than
			// refused. Pinned as-is; decide in stage 1.
			name: "an unregistered dimension falls back to the overworld",
			dim:  unregisteredDimension{},
			want: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := s.dimensionID(test.dim); got != test.want {
				t.Fatalf("dimensionID(%v) = %d, want %d", test.name, got, test.want)
			}
		})
	}
}

// unregisteredDimension is a world.Dimension that was never registered, which
// is the only way to reach the lookup's failure branch.
type unregisteredDimension struct{}

func (unregisteredDimension) Range() cube.Range                 { return cube.Range{0, 15} }
func (unregisteredDimension) WaterEvaporates() bool             { return false }
func (unregisteredDimension) LavaSpreadDuration() time.Duration { return 0 }
func (unregisteredDimension) WeatherCycle() bool                { return false }
func (unregisteredDimension) TimeCycle() bool                   { return false }
func (unregisteredDimension) String() string                    { return "Unregistered" }

// TestTrackBlob pins the blob store: a tracked blob is kept under its hash and
// reported as accepted.
func TestTrackBlob(t *testing.T) {
	s := newStubSession(t, nil)

	if !s.trackBlob(7, []byte{1, 2, 3}) {
		t.Fatal("trackBlob rejected the first blob")
	}
	if got, ok := s.blobs[7]; !ok || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("stored blob = %v (present: %v), want [1 2 3]", got, ok)
	}

	// The same hash overwrites rather than being rejected as a duplicate.
	if !s.trackBlob(7, []byte{4}) {
		t.Fatal("trackBlob rejected a repeated hash")
	}
	if got := s.blobs[7]; !bytes.Equal(got, []byte{4}) {
		t.Fatalf("stored blob = %v, want the overwriting [4]", got)
	}
}

// TestTrackBlobLimit pins the cap on pending blobs, which is the guard against
// a client that never acknowledges what it was sent.
//
// PINNED BUG: the check is `len(s.blobs) > 4096`, so the store is allowed to
// reach 4097 entries before anything is refused, and the doc comment claims the
// connection is closed when it is not: trackBlob only logs and returns false.
// Pinned as-is; decide in stage 1.
func TestTrackBlobLimit(t *testing.T) {
	s := newStubSession(t, nil)
	for i := range uint64(4097) {
		if !s.trackBlob(i, []byte{byte(i)}) {
			t.Fatalf("trackBlob refused blob %d, want it accepted up to 4097", i)
		}
	}
	if got := len(s.blobs); got != 4097 {
		t.Fatalf("stored %d blobs, want 4097", got)
	}
	if s.trackBlob(9999, []byte{1}) {
		t.Fatal("trackBlob accepted a blob past the limit")
	}
	if _, ok := s.blobs[9999]; ok {
		t.Fatal("a refused blob was stored anyway")
	}
	if s.conn.(*stubConn).closed {
		t.Fatal("trackBlob closed the connection; its comment says it does, the code does not")
	}
}

// emptyChunk builds a chunk of air over the overworld range, which is what a
// freshly generated column looks like before anything is placed in it.
func emptyChunk(t *testing.T) *chunk.Chunk {
	t.Helper()
	return chunk.New(testRegistry(t), world.Overworld.Range())
}

// TestViewChunkWithoutClientCache pins the packet a client with no blob cache
// receives: a header-only LevelChunk carrying the biome payload with a trailing
// border-block count of zero, and no blob hashes.
func TestViewChunkWithoutClientCache(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	c := emptyChunk(t)

	s.ViewChunk(world.ChunkPos{3, -4}, world.Nether, nil, c)

	got := onlyPacket(t, s).(*packet.LevelChunk)
	if got.Dimension != 1 {
		t.Fatalf("dimension = %d, want the nether 1", got.Dimension)
	}
	if got.Position != (protocol.ChunkPos{3, -4}) {
		t.Fatalf("position = %v, want {3 -4}", got.Position)
	}
	if got.SubChunkCount != 0 {
		t.Fatalf("sub chunk count = %d, want 0", got.SubChunkCount)
	}
	if limit, ok := got.SubChunkLimit.Value(); !ok || limit != int32(c.HighestFilledSubChunk()) {
		t.Fatalf("sub chunk limit = %v (set: %v), want %d", limit, ok, c.HighestFilledSubChunk())
	}
	if got.CacheEnabled {
		t.Fatal("cache enabled was set for a client without the blob cache")
	}
	if len(got.BlobHashes) != 0 {
		t.Fatalf("blob hashes = %v, want none", got.BlobHashes)
	}

	want := append(chunk.EncodeBiomes(c, chunk.NetworkEncoding), 0)
	if !bytes.Equal(got.RawPayload, want) {
		t.Fatalf("raw payload = %v, want the biomes plus a zero border count %v", got.RawPayload, want)
	}
	if len(s.blobs) != 0 {
		t.Fatal("a non-caching send tracked a blob")
	}
}

// TestViewChunkWithClientCache pins the caching path: the biome payload is
// replaced by its hash, the payload becomes a single zero byte, and the blob is
// stored for the client to request.
func TestViewChunkWithClientCache(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: true})
	c := emptyChunk(t)

	s.ViewChunk(world.ChunkPos{0, 0}, world.End, nil, c)

	got := onlyPacket(t, s).(*packet.LevelChunk)
	if !got.CacheEnabled {
		t.Fatal("cache enabled was not set for a caching client")
	}
	if got.Dimension != 2 {
		t.Fatalf("dimension = %d, want the end 2", got.Dimension)
	}
	biomes := chunk.EncodeBiomes(c, chunk.NetworkEncoding)
	hash := xxhash.Sum64(biomes)
	if len(got.BlobHashes) != 1 || got.BlobHashes[0] != hash {
		t.Fatalf("blob hashes = %v, want [%d]", got.BlobHashes, hash)
	}
	if !bytes.Equal(got.RawPayload, []byte{0}) {
		t.Fatalf("raw payload = %v, want a single zero byte", got.RawPayload)
	}
	if stored, ok := s.blobs[hash]; !ok || !bytes.Equal(stored, biomes) {
		t.Fatalf("blob for %d was not stored", hash)
	}
	if len(s.openChunkTransactions) != 0 {
		t.Fatalf("open chunk transactions = %v, want none on the sub-chunk request path",
			s.openChunkTransactions)
	}
}

// TestViewChunkWithClientCacheAtTheBlobLimit pins what a caching client is sent
// once its blob store is full.
//
// PINNED BUG: when trackBlob refuses the biome blob, sendBlobHashes falls
// through to the full chunk encoding, immediately hits the same limit again and
// returns having written no packet at all. The chunk is silently dropped and
// the loader still counts it as sent. Pinned as-is; decide in stage 1.
func TestViewChunkWithClientCacheAtTheBlobLimit(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: true})
	for i := range uint64(4097) {
		s.blobs[i] = []byte{byte(i)}
	}

	s.ViewChunk(world.ChunkPos{0, 0}, world.Overworld, nil, emptyChunk(t))

	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("wrote %d packets at the blob limit, want none: %#v", len(pks), pks)
	}
	if len(s.openChunkTransactions) != 1 {
		t.Fatalf("open chunk transactions = %d, want the leaked 1", len(s.openChunkTransactions))
	}
}

// TestViewChunkPositionAxes pins that the chunk position keeps its X and Z in
// order and that negative coordinates survive.
func TestViewChunkPositionAxes(t *testing.T) {
	for _, pos := range []world.ChunkPos{{0, 0}, {1, 2}, {-1, -2}, {2147483647, -2147483648}} {
		s := newStubSession(t, &stubConn{})
		s.ViewChunk(pos, world.Overworld, nil, emptyChunk(t))

		got := onlyPacket(t, s).(*packet.LevelChunk)
		if got.Position != (protocol.ChunkPos{pos.X(), pos.Z()}) {
			t.Fatalf("position = %v, want %v", got.Position, pos)
		}
	}
}

// TestSendNetworkChunkIgnoresBlockEntities pins that the sub-chunk request path
// drops the block entities it is handed: they are re-sent per sub-chunk later.
//
// PINNED SCOPE: sendNetworkChunk takes a blockEntities map and, with
// subChunkRequests on, never reads it. Out of scope per CLAUDE.md 2.6.
func TestSendNetworkChunkIgnoresBlockEntities(t *testing.T) {
	s := newStubSession(t, &stubConn{})
	c := emptyChunk(t)

	withEntities := newStubSession(t, &stubConn{})
	withEntities.sendNetworkChunk(world.ChunkPos{0, 0}, world.Overworld, c,
		map[cube.Pos]world.Block{{1, 2, 3}: nil})
	s.sendNetworkChunk(world.ChunkPos{0, 0}, world.Overworld, c, nil)

	a := onlyPacket(t, s).(*packet.LevelChunk)
	b := onlyPacket(t, withEntities).(*packet.LevelChunk)
	if !bytes.Equal(a.RawPayload, b.RawPayload) {
		t.Fatalf("block entities changed the payload: %v vs %v", a.RawPayload, b.RawPayload)
	}
}
