package session

import (
	"bytes"

	"github.com/cespare/xxhash/v2"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// subChunkRequests is set to true to enable the sub-chunk request system. This can (likely) cause unexpected issues,
// but also solves issues with block entities such as item frames and lecterns as of v1.19.10.
const subChunkRequests = true

// maxOpenChunkTransactions bounds the chunk transactions awaiting client
// acknowledgement. A legitimate client holds a handful of in-flight chunk or
// sub-chunk transactions: 256 is an order of magnitude above that while
// keeping a client that never acknowledges from growing the list without
// bound. Eviction drops bookkeeping only: every hash stays in s.blobs until
// the client answers it, so delivery is unaffected.
const maxOpenChunkTransactions = 256

// appendChunkTransactionLocked records a chunk transaction, evicting the
// oldest one when at the cap. The caller must hold s.blobMu.
func (s *Session) appendChunkTransactionLocked(m map[uint64]struct{}) {
	if len(s.openChunkTransactions) >= maxOpenChunkTransactions {
		// Which input was the problem: a client that never sends
		// ClientCacheBlobStatus. Every LevelChunk and SubChunk reply appended
		// a transaction nothing would ever resolve, growing the slice without
		// bound. The evicted slot is nilled so the dropped map is not kept
		// alive by the backing array.
		s.openChunkTransactions[0] = nil
		s.openChunkTransactions = s.openChunkTransactions[1:]
	}
	s.openChunkTransactions = append(s.openChunkTransactions, m)
}

// ViewChunk ...
func (s *Session) ViewChunk(pos world.ChunkPos, dim world.Dimension, blockEntities map[cube.Pos]world.Block, c *chunk.Chunk) {
	if !s.conn.ClientCacheEnabled() {
		s.sendNetworkChunk(pos, dim, c, blockEntities)
		return
	}
	s.sendBlobHashes(pos, dim, c, blockEntities)
}

// ViewSubChunks ...
func (s *Session) ViewSubChunks(centre world.SubChunkPos, offsets []protocol.SubChunkOffset, tx *world.Tx) {
	r := tx.Range()

	// The cache flag is fixed for the lifetime of the connection, so the two
	// reads below see the same value the single read here does.
	cacheEnabled := s.conn.ClientCacheEnabled()

	entries := make([]protocol.SubChunkEntry, 0, len(offsets))
	transaction := make(map[uint64]struct{})
	for _, offset := range offsets {
		ind := int16(centre.Y()) + int16(offset[1]) - int16(r[0])>>4
		if ind < 0 || ind > int16(r.Height()>>4) {
			entries = append(entries, protocol.SubChunkEntry{Result: protocol.SubChunkResultIndexOutOfBounds, Offset: offset})
			continue
		}
		col, ok := s.chunkLoader.Chunk(world.ChunkPos{
			centre.X() + int32(offset[0]),
			centre.Z() + int32(offset[2]),
		})
		if !ok {
			entries = append(entries, protocol.SubChunkEntry{Result: protocol.SubChunkResultChunkNotFound, Offset: offset})
			continue
		}
		entries = append(entries, s.subChunkEntry(offset, ind, col, transaction))
	}
	if cacheEnabled && len(transaction) > 0 {
		s.blobMu.Lock()
		s.appendChunkTransactionLocked(transaction)
		s.blobMu.Unlock()
	}
	dim, _ := world.DimensionID(tx.World().Dimension())
	s.writePacket(&packet.SubChunk{
		Dimension:       int32(dim),
		Position:        protocol.SubChunkPos(centre),
		CacheEnabled:    cacheEnabled,
		SubChunkEntries: entries,
	})
}

func (s *Session) subChunkEntry(offset protocol.SubChunkOffset, ind int16, col *world.Column, transaction map[uint64]struct{}) protocol.SubChunkEntry {
	// The height map and the sub chunk below are derived from the chunk's
	// blocks and built fresh on every call. Neither may be cached on the
	// Column: world.Column embeds *chunk.Chunk, and chunk.Chunk and its
	// SubChunks both export block setters, so there is no edit path a cache
	// could be reliably invalidated from.
	chunkMap := col.HeightMap()
	subMapType, subMap := byte(protocol.HeightMapDataHasData), protocol.HeightMap{}
	higher, lower := true, true
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			y := chunkMap.At(x, z)
			otherInd := col.SubIndex(y)
			switch {
			case otherInd > ind:
				subMap[z][x], lower = 16, false
			case otherInd < ind:
				subMap[z][x], higher = -1, false
			default:
				subMap[z][x], lower, higher = int8(y-col.SubY(otherInd)), false, false
			}
		}
	}
	if higher {
		subMapType = protocol.HeightMapDataTooHigh
	} else if lower {
		subMapType = protocol.HeightMapDataTooLow
	}
	var subMapData protocol.Optional[protocol.HeightMap]
	if !higher && !lower {
		subMapData = protocol.Option(subMap)
	}

	sub := col.Sub()[ind]
	if sub.Empty() {
		return protocol.SubChunkEntry{
			Result:              protocol.SubChunkResultSuccessAllAir,
			HeightMapType:       subMapType,
			HeightMapData:       subMapData,
			RenderHeightMapType: subMapType,
			RenderHeightMapData: subMapData,
			Offset:              offset,
		}
	}

	serialisedSubChunk := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind))

	// Most sub-chunks hold no block entity at all. The buffer and the encoder
	// are built on the first one that lands in this sub-chunk, so that the
	// common case allocates neither. An untouched buffer returns a nil slice,
	// which is what the two payload writes below saw before.
	var blockEntityBuf *bytes.Buffer
	var enc *nbt.Encoder
	for pos, b := range col.BlockEntities {
		if n, ok := b.(world.NBTer); ok && col.SubIndex(int16(pos.Y())) == ind {
			if blockEntityBuf == nil {
				blockEntityBuf = bytes.NewBuffer(nil)
				enc = nbt.NewEncoderWithEncoding(blockEntityBuf, nbt.NetworkLittleEndian)
			}
			d := n.EncodeNBT()
			d["x"], d["y"], d["z"] = int32(pos[0]), int32(pos[1]), int32(pos[2])
			_ = enc.Encode(d)
		}
	}
	var blockEntityData []byte
	if blockEntityBuf != nil {
		blockEntityData = blockEntityBuf.Bytes()
	}

	entry := protocol.SubChunkEntry{
		Result:              protocol.SubChunkResultSuccess,
		RawPayload:          protocol.Option(append(serialisedSubChunk, blockEntityData...)),
		HeightMapType:       subMapType,
		HeightMapData:       subMapData,
		RenderHeightMapType: subMapType,
		RenderHeightMapData: subMapData,
		Offset:              offset,
	}
	if s.conn.ClientCacheEnabled() {
		if hash := xxhash.Sum64(serialisedSubChunk); s.trackBlob(hash, serialisedSubChunk) {
			transaction[hash] = struct{}{}

			entry.BlobHash = protocol.Option(hash)
			entry.RawPayload = protocol.Option(blockEntityData)
		}
	}
	return entry
}

// dimensionID returns the dimension ID of the world that the session is in.
func (s *Session) dimensionID(dim world.Dimension) int32 {
	d, _ := world.DimensionID(dim)
	return int32(d)
}

// sendBlobHashes sends chunk blob hashes of the data of the chunk and stores the data in a map of blobs. Only
// data that the client doesn't yet have will be sent over the network.
func (s *Session) sendBlobHashes(pos world.ChunkPos, dim world.Dimension, c *chunk.Chunk, blockEntities map[cube.Pos]world.Block) {
	if subChunkRequests {
		biomes := chunk.EncodeBiomes(c, chunk.NetworkEncoding)
		if hash := xxhash.Sum64(biomes); s.trackBlob(hash, biomes) {
			s.writePacket(&packet.LevelChunk{
				Dimension:     s.dimensionID(dim),
				SubChunkCount: 0,
				Position:      protocol.ChunkPos(pos),
				SubChunkLimit: protocol.Option(int32(c.HighestFilledSubChunk())),
				BlobHashes:    []uint64{hash},
				RawPayload:    []byte{0},
				CacheEnabled:  true,
			})
			return
		}
	}

	var (
		data   = chunk.Encode(c, chunk.NetworkEncoding)
		count  = uint32(len(data.SubChunks))
		blobs  = append(data.SubChunks, data.Biomes)
		hashes = make([]uint64, len(blobs))
		m      = make(map[uint64]struct{}, len(blobs))
	)
	for i, blob := range blobs {
		h := xxhash.Sum64(blob)
		hashes[i], m[h] = h, struct{}{}
	}

	s.blobMu.Lock()
	// Which input was the problem: a blob store already at the cap. The
	// transaction was opened before the limit was checked, so a refused
	// chunk leaked a transaction the client would never answer, and no
	// packet went out while the loader still recorded the chunk as sent.
	// The check runs first now, and a refused chunk falls back to the
	// uncached encoding: the client still receives it, and the server
	// stores nothing, so the cap keeps bounding memory.
	// Why the behaviour is identical: on the accepted path the same lock
	// still opens exactly one transaction and stores the same hashes
	// before the same packet is written below.
	if l := len(s.blobs); l >= 4096 {
		s.blobMu.Unlock()
		s.conf.Log.Error("too many blobs pending", "n", l)
		s.sendNetworkChunk(pos, dim, c, blockEntities)
		return
	}
	s.appendChunkTransactionLocked(m)
	for i := range hashes {
		s.blobs[hashes[i]] = blobs[i]
	}
	s.blobMu.Unlock()

	// Length of 1 byte for the border block count.
	raw := bytes.NewBuffer(make([]byte, 1, 32))
	// Most chunks hold no block entity that writes NBT. The encoder is built on
	// the first one that does, so the common case allocates none. The encoder
	// keeps no state between Encode calls that the eager one carried over.
	var enc *nbt.Encoder
	for bp, b := range blockEntities {
		if n, ok := b.(world.NBTer); ok {
			if enc == nil {
				enc = nbt.NewEncoderWithEncoding(raw, nbt.NetworkLittleEndian)
			}
			d := n.EncodeNBT()
			d["x"], d["y"], d["z"] = int32(bp[0]), int32(bp[1]), int32(bp[2])
			_ = enc.Encode(d)
		}
	}

	s.writePacket(&packet.LevelChunk{
		Dimension:     s.dimensionID(dim),
		Position:      protocol.ChunkPos{pos.X(), pos.Z()},
		SubChunkCount: count,
		CacheEnabled:  true,
		BlobHashes:    hashes,
		RawPayload:    raw.Bytes(),
	})
}

// sendNetworkChunk sends a network encoded chunk to the client.
func (s *Session) sendNetworkChunk(pos world.ChunkPos, dim world.Dimension, c *chunk.Chunk, blockEntities map[cube.Pos]world.Block) {
	if subChunkRequests {
		s.writePacket(&packet.LevelChunk{
			Dimension:     s.dimensionID(dim),
			SubChunkCount: 0,
			Position:      protocol.ChunkPos(pos),
			SubChunkLimit: protocol.Option(int32(c.HighestFilledSubChunk())),
			RawPayload:    append(chunk.EncodeBiomes(c, chunk.NetworkEncoding), 0),
		})
		return
	}

	data := chunk.Encode(c, chunk.NetworkEncoding)
	chunkBuf := bytes.NewBuffer(nil)
	for _, s := range data.SubChunks {
		_, _ = chunkBuf.Write(s)
	}
	_, _ = chunkBuf.Write(data.Biomes)

	// Length of 1 byte for the border block count.
	chunkBuf.WriteByte(0)

	enc := nbt.NewEncoderWithEncoding(chunkBuf, nbt.NetworkLittleEndian)
	for bp, b := range blockEntities {
		if n, ok := b.(world.NBTer); ok {
			d := n.EncodeNBT()
			d["x"], d["y"], d["z"] = int32(bp[0]), int32(bp[1]), int32(bp[2])
			_ = enc.Encode(d)
		}
	}

	s.writePacket(&packet.LevelChunk{
		Dimension:     s.dimensionID(dim),
		Position:      protocol.ChunkPos{pos.X(), pos.Z()},
		SubChunkCount: uint32(len(data.SubChunks)),
		RawPayload:    append([]byte(nil), chunkBuf.Bytes()...),
	})
}

// trackBlob attempts to track the given blob. If the player has too many pending blobs, it returns false.
func (s *Session) trackBlob(hash uint64, blob []byte) bool {
	s.blobMu.Lock()
	// Which input was the problem: the 4097th blob. The check read
	// len(s.blobs) > 4096, so the store grew one entry past the cap it was
	// meant to enforce. Refusing at >= keeps the store at 4096 entries.
	if l := len(s.blobs); l >= 4096 {
		s.blobMu.Unlock()
		s.conf.Log.Error("too many blobs pending", "n", l)
		return false
	}
	s.blobs[hash] = blob
	s.blobMu.Unlock()
	return true
}
