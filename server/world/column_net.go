package world

import (
	"github.com/jv2w/Impulse/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// subChunkNetCache holds the parts of the network payload of one sub chunk
// that are the same for every viewer. The two parts are filled separately
// because a viewer that only needs the height map must not pay for encoding
// the sub chunk.
type subChunkNetCache struct {
	// version is the Column's netVersion the entry was built from. A zero
	// version is not special: the two valid flags decide whether anything in
	// the entry has been built at all.
	version uint64

	payload      []byte
	payloadValid bool

	heightMapType byte
	heightMap     []int8
	heightValid   bool
}

// invalidateNetCache marks the cached network payloads of the Column stale. It
// must be called from every path that edits the blocks of Chunk: a viewer
// requesting a sub chunk afterwards would otherwise be sent the terrain from
// before the edit. Changes to the entity list need no call, as no part of a
// cached payload is derived from it.
func (col *Column) invalidateNetCache() {
	col.netVersion++
}

// netCacheEntry returns the cache entry of sub chunk ind, clearing it first if
// it was built from an older version of the Column.
func (col *Column) netCacheEntry(ind int16) *subChunkNetCache {
	if col.netCache == nil {
		col.netCache = make([]subChunkNetCache, len(col.Sub()))
	}
	e := &col.netCache[ind]
	if e.version != col.netVersion {
		*e = subChunkNetCache{version: col.netVersion}
	}
	return e
}

// NetworkSubChunk returns the network encoding of sub chunk ind of the Column.
// The bytes depend on the chunk's blocks alone, so they are encoded once and
// shared by every viewer of the Column.
//
// The slice returned aliases the Column's cache. It is read-only to the caller
// and stops being current the moment the chunk is edited, so a caller handing
// it to anything that outlives the call must copy it first.
//
// NetworkSubChunk must be called on the world goroutine, under the Tx that
// owns the Column.
func (col *Column) NetworkSubChunk(ind int16) []byte {
	e := col.netCacheEntry(ind)
	if !e.payloadValid {
		e.payload, e.payloadValid = chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind)), true
	}
	return e.payload
}

// NetworkHeightMap returns the height map type and data of sub chunk ind as the
// sub-chunk request system sends them. The data is nil for the types that carry
// none. Like NetworkSubChunk, the result depends on the chunk's blocks alone
// and the slice returned aliases the Column's cache.
func (col *Column) NetworkHeightMap(ind int16) (byte, []int8) {
	e := col.netCacheEntry(ind)
	if !e.heightValid {
		e.heightMapType, e.heightMap = col.encodeHeightMap(ind)
		e.heightValid = true
	}
	return e.heightMapType, e.heightMap
}

// encodeHeightMap builds the height map of sub chunk ind: for every column of
// the chunk, the height of the highest light blocker relative to the bottom of
// the sub chunk, or a marker saying the whole chunk lies above or below it.
func (col *Column) encodeHeightMap(ind int16) (byte, []int8) {
	chunkMap := col.HeightMap()
	subMapType, subMap := byte(protocol.HeightMapDataHasData), make([]int8, 256)
	higher, lower := true, true
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			y, i := chunkMap.At(x, z), (uint16(z)<<4)|uint16(x)
			otherInd := col.SubIndex(y)
			switch {
			case otherInd > ind:
				subMap[i], lower = 16, false
			case otherInd < ind:
				subMap[i], higher = -1, false
			default:
				subMap[i], lower, higher = int8(y-col.SubY(otherInd)), false, false
			}
		}
	}
	if higher {
		subMapType, subMap = protocol.HeightMapDataTooHigh, nil
	} else if lower {
		subMapType, subMap = protocol.HeightMapDataTooLow, nil
	}
	return subMapType, subMap
}
