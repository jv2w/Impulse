package session

import (
	"bytes"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The chunk send path is the one place where what goes on the wire depends on a
// client capability (the client-side blob cache) rather than on world state.
// What is pinned here is which of the two encodings a session picks and what it
// puts in the packet.

// TestSubChunkRequestsIsEnabled pins the package constant that decides the
// encoding. With it set, both send paths return a header-only LevelChunk and
// the client asks for sub-chunks separately.
//
// PINNED SCOPE: subChunkRequests is a compile-time const true, so the full
// chunk encodings below it in sendNetworkChunk and sendBlobHashes are dead
// code that no test can reach. Out of scope.
func TestSubChunkRequestsIsEnabled(t *testing.T) {
	if !subChunkRequests {
		t.Fatal("subChunkRequests = false; the pinned behaviour below assumes it is true")
	}
}

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
// a client that never acknowledges what it was sent: past 4096 tracked blobs
// the next one is refused and never stored. A refusal is only logged, the
// connection stays open.
func TestTrackBlobLimit(t *testing.T) {
	s := newStubSession(t, nil)
	for i := range uint64(4096) {
		if !s.trackBlob(i, []byte{byte(i)}) {
			t.Fatalf("trackBlob refused blob %d, want it accepted up to 4096", i)
		}
	}
	if got := len(s.blobs); got != 4096 {
		t.Fatalf("stored %d blobs, want 4096", got)
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
	return chunk.New(testRegistry(t), world.Range())
}

// TestViewChunkWithoutClientCache pins the packet a client with no blob cache
// receives: a header-only LevelChunk carrying the biome payload with a trailing
// border-block count of zero, and no blob hashes.
func TestViewChunkWithoutClientCache(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	c := emptyChunk(t)

	s.ViewChunk(world.ChunkPos{3, -4}, nil, c)

	got := onlyPacket(t, s).(*packet.LevelChunk)
	if got.Dimension != 0 {
		t.Fatalf("dimension = %d, want the overworld 0", got.Dimension)
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

	s.ViewChunk(world.ChunkPos{0, 0}, nil, c)

	got := onlyPacket(t, s).(*packet.LevelChunk)
	if !got.CacheEnabled {
		t.Fatal("cache enabled was not set for a caching client")
	}
	if got.Dimension != 0 {
		t.Fatalf("dimension = %d, want the overworld 0", got.Dimension)
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
// once its blob store is full: with nowhere to keep the hashes, the chunk
// falls back to the uncached encoding instead of being dropped. Nothing is
// stored, and no chunk transaction is opened, since no blob hashes were sent.
func TestViewChunkWithClientCacheAtTheBlobLimit(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: true})
	for i := range uint64(4096) {
		s.blobs[i] = []byte{byte(i)}
	}

	s.ViewChunk(world.ChunkPos{0, 0}, nil, emptyChunk(t))

	got := onlyPacket(t, s).(*packet.LevelChunk)
	if got.CacheEnabled {
		t.Fatal("chunk at the blob limit was sent cache-enabled with no tracked blobs")
	}
	if len(got.BlobHashes) != 0 {
		t.Fatalf("blob hashes = %v, want none on the uncached fallback", got.BlobHashes)
	}
	if len(s.blobs) != 4096 {
		t.Fatalf("stored %d blobs, want the untouched 4096", len(s.blobs))
	}
	if len(s.openChunkTransactions) != 0 {
		t.Fatalf("open chunk transactions = %d, want none: no hashes were sent", len(s.openChunkTransactions))
	}
}

// TestViewChunkPositionAxes pins that the chunk position keeps its X and Z in
// order and that negative coordinates survive.
func TestViewChunkPositionAxes(t *testing.T) {
	for _, pos := range []world.ChunkPos{{0, 0}, {1, 2}, {-1, -2}, {2147483647, -2147483648}} {
		s := newStubSession(t, &stubConn{})
		s.ViewChunk(pos, nil, emptyChunk(t))

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
// subChunkRequests on, never reads it. Out of scope.
func TestSendNetworkChunkIgnoresBlockEntities(t *testing.T) {
	s := newStubSession(t, &stubConn{})
	c := emptyChunk(t)

	withEntities := newStubSession(t, &stubConn{})
	withEntities.sendNetworkChunk(world.ChunkPos{0, 0}, c,
		map[cube.Pos]world.Block{{1, 2, 3}: nil})
	s.sendNetworkChunk(world.ChunkPos{0, 0}, c, nil)

	a := onlyPacket(t, s).(*packet.LevelChunk)
	b := onlyPacket(t, withEntities).(*packet.LevelChunk)
	if !bytes.Equal(a.RawPayload, b.RawPayload) {
		t.Fatalf("block entities changed the payload: %v vs %v", a.RawPayload, b.RawPayload)
	}
}

// stoneColumn builds a column holding a single stone block at the bottom of the
// overworld, so that the sub-chunk containing it does not report itself empty,
// and returns the index of that sub-chunk.
func stoneColumn(t *testing.T) (*world.Column, int16) {
	t.Helper()
	c := emptyChunk(t)
	c.SetBlock(0, 0, 0, 0, testRegistry(t).BlockRuntimeID(block.Stone{}))
	return &world.Column{Chunk: c}, c.SubIndex(0)
}

// decodeBlockEntity decodes the network NBT that subChunkEntry appends after a
// sub-chunk payload. The bytes themselves cannot be compared: EncodeNBT hands
// back a map, so the order the fields are written in differs per run.
func decodeBlockEntity(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var d map[string]any
	if err := nbt.NewDecoderWithEncoding(bytes.NewReader(data), nbt.NetworkLittleEndian).Decode(&d); err != nil {
		t.Fatalf("decode block entity: %v", err)
	}
	return d
}

// TestSubChunkEntryPayloadWithoutBlockEntities pins that a sub-chunk with no
// block entities in it carries the encoded sub-chunk and nothing after it.
func TestSubChunkEntryPayloadWithoutBlockEntities(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	col, ind := stoneColumn(t)

	entry := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	if entry.Result != protocol.SubChunkResultSuccess {
		t.Fatalf("result = %d, want success %d", entry.Result, protocol.SubChunkResultSuccess)
	}
	got, ok := entry.RawPayload.Value()
	if !ok {
		t.Fatal("entry carries no raw payload")
	}
	if want := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind)); !bytes.Equal(got, want) {
		t.Fatalf("payload = %v, want the bare sub-chunk %v", got, want)
	}
}

// TestSubChunkEntryPayloadWithBlockEntities pins that a block entity inside the
// requested sub-chunk is appended to the payload as network NBT.
func TestSubChunkEntryPayloadWithBlockEntities(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	col, ind := stoneColumn(t)
	pos := cube.Pos{1, 2, 3}
	col.BlockEntities = map[cube.Pos]world.Block{pos: block.Chest{}}

	entry := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	got, ok := entry.RawPayload.Value()
	if !ok {
		t.Fatal("entry carries no raw payload")
	}
	sub := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind))
	if !bytes.HasPrefix(got, sub) {
		t.Fatal("payload does not start with the encoded sub-chunk")
	}
	d := decodeBlockEntity(t, got[len(sub):])
	if d["id"] != "Chest" {
		t.Fatalf("block entity id = %v, want Chest", d["id"])
	}
	if d["x"] != int32(pos[0]) || d["y"] != int32(pos[1]) || d["z"] != int32(pos[2]) {
		t.Fatalf("block entity position = (%v, %v, %v), want %v", d["x"], d["y"], d["z"], pos)
	}
}

// TestSubChunkEntryDropsOtherSubChunkBlockEntities pins that a block entity of
// the same column but a different sub-chunk index is left out of the payload.
func TestSubChunkEntryDropsOtherSubChunkBlockEntities(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	col, ind := stoneColumn(t)
	col.BlockEntities = map[cube.Pos]world.Block{{1, 40, 3}: block.Chest{}}

	entry := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	got, ok := entry.RawPayload.Value()
	if !ok {
		t.Fatal("entry carries no raw payload")
	}
	if want := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind)); !bytes.Equal(got, want) {
		t.Fatalf("a block entity outside the sub-chunk was written into the payload")
	}
}

// TestSubChunkEntrySharedBetweenSessions pins that two sessions requesting the
// same sub-chunk are sent identical bytes, and that each is still handed arrays
// of its own: the sub-chunk bytes outlive the call inside a per-session blob
// map, and the height map is written into a packet on the session's own
// goroutine.
func TestSubChunkEntrySharedBetweenSessions(t *testing.T) {
	col, ind := stoneColumn(t)
	a := newStubSession(t, &stubConn{cacheEnabled: false})
	b := newStubSession(t, &stubConn{cacheEnabled: false})

	first := a.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	second := b.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})

	payloadA, ok := first.RawPayload.Value()
	if !ok {
		t.Fatal("the first entry carries no raw payload")
	}
	payloadB, ok := second.RawPayload.Value()
	if !ok {
		t.Fatal("the second entry carries no raw payload")
	}
	if !bytes.Equal(payloadA, payloadB) {
		t.Fatalf("payloads differ between sessions: %v vs %v", payloadA, payloadB)
	}
	if want := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind)); !bytes.Equal(payloadA, want) {
		t.Fatalf("payload = %v, want the encoded sub-chunk %v", payloadA, want)
	}
	if &payloadA[0] == &payloadB[0] {
		t.Error("both sessions were handed the same payload array")
	}

	mapA, ok := first.HeightMapData.Value()
	if !ok {
		t.Fatal("the first entry carries no height map")
	}
	mapB, ok := second.HeightMapData.Value()
	if !ok {
		t.Fatal("the second entry carries no height map")
	}
	if mapA != mapB {
		t.Fatalf("height maps differ between sessions: %v vs %v", mapA, mapB)
	}
	if &mapA[0] == &mapB[0] {
		t.Error("both sessions were handed the same height map array")
	}
}

// TestSubChunkEntryFollowsAnEditMadeOutsideTheWorld sends a sub-chunk, edits the
// chunk the way any caller holding the Column can - world.Column embeds
// *chunk.Chunk, so chunk.Chunk.SetBlock is a method on the Column itself - and
// sends it again. The second send has to describe the chunk as it is now.
//
// The edit here goes through none of the World paths, which is the whole point:
// a payload cached against a counter those paths maintain is stale from here on
// and the viewer is sent terrain that no longer exists.
func TestSubChunkEntryFollowsAnEditMadeOutsideTheWorld(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	col, ind := stoneColumn(t)

	first := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	if _, ok := first.RawPayload.Value(); !ok {
		t.Fatal("the first entry carries no raw payload")
	}

	col.SetBlock(1, 0, 1, 0, testRegistry(t).BlockRuntimeID(block.Stone{}))

	second := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	got, ok := second.RawPayload.Value()
	if !ok {
		t.Fatal("the second entry carries no raw payload")
	}
	if want := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind)); !bytes.Equal(got, want) {
		t.Fatalf("payload after the edit = %v, want the chunk as it is now %v", got, want)
	}
}

// TestSubChunkEntryHeightMapFollowsAnEditMadeOutsideTheWorld is the same for the
// height map, which is derived from the same blocks and was cached beside them.
func TestSubChunkEntryHeightMapFollowsAnEditMadeOutsideTheWorld(t *testing.T) {
	s := newStubSession(t, &stubConn{cacheEnabled: false})
	col, ind := stoneColumn(t)

	first := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	before, ok := first.HeightMapData.Value()
	if !ok {
		t.Fatal("the first entry carries no height map")
	}
	// The map holds the height of the first free space, so the single stone
	// stoneColumn placed at y=0 reads as 1.
	if before[0][0] != 1 {
		t.Fatalf("height at the stone column = %v, want 1", before[0][0])
	}

	// A block two above the one stoneColumn placed lifts that column to 3.
	col.SetBlock(0, 2, 0, 0, testRegistry(t).BlockRuntimeID(block.Stone{}))
	// An off-axis block pins the map's [z][x] layout: transposed writes would
	// land this column's height at [2][5] instead of [5][2].
	col.SetBlock(2, 0, 5, 0, testRegistry(t).BlockRuntimeID(block.Stone{}))

	second := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, map[uint64]struct{}{})
	after, ok := second.HeightMapData.Value()
	if !ok {
		t.Fatal("the second entry carries no height map")
	}
	if after[0][0] != 3 {
		t.Fatalf("height at the stone column after the edit = %v, want 3", after[0][0])
	}
	if after[5][2] != 1 {
		t.Fatalf("height at the off-axis column = %v, want 1", after[5][2])
	}
}
