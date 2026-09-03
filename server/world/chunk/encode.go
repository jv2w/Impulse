package chunk

import (
	"bytes"
	"sync"
)

const (
	// SubChunkVersion is the current version of the written sub chunks, specifying the format they are
	// written on disk and over network.
	SubChunkVersion = 9
	// CurrentBlockVersion is the current version of blocks (states) of the game. This version is composed
	// of 4 bytes indicating a version, interpreted as a big endian int. The current version represents
	// 1.16.0.14 {1, 16, 0, 14}.
	CurrentBlockVersion int32 = 18040335
)

var (
	// pool is used to pool byte buffers used for encoding chunks.
	pool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}
)

type (
	// SerialisedData holds the serialised data of a chunk. It consists of the chunk's block data itself, a height
	// map, the biomes and entities and block entities.
	SerialisedData struct {
		// sub holds the data of the serialised sub chunks in a chunk. Sub chunks that are empty or that otherwise
		// don't exist are represented as an empty slice (or technically, nil).
		SubChunks [][]byte
		// Biomes is the biome data of the chunk, which is composed of a biome storage for each sub-chunk.
		Biomes []byte
	}
	// blockEntry represents a block as found in a disk save of a world.
	blockEntry struct {
		Name    string         `nbt:"name"`
		State   map[string]any `nbt:"states"`
		Version int32          `nbt:"version"`
	}
)

// Encode encodes Chunk to an intermediate representation SerialisedData. An Encoding may be passed to encode either for
// network or disk purposed, the most notable difference being that the network encoding generally uses varints and no
// NBT.
func Encode(c *Chunk, e Encoding) SerialisedData {
	d := SerialisedData{SubChunks: make([][]byte, len(c.sub))}
	// One pooled buffer for all sub chunks instead of one per sub chunk. encodeSubChunk drains everything
	// it wrote into a freshly allocated slice, so the reset after each one leaves the buffer in exactly the
	// state a newly taken buffer is in, and every sub chunk is encoded from the same empty buffer as before.
	buf := pool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		pool.Put(buf)
	}()
	for i := range c.sub {
		d.SubChunks[i] = encodeSubChunk(buf, c, e, i)
		buf.Reset()
	}
	d.Biomes = EncodeBiomes(c, e)
	return d
}

// EncodeSubChunk encodes a sub-chunk from a chunk into bytes. An Encoding may be passed to encode either for network or
// disk purposed, the most notable difference being that the network encoding generally uses varints and no NBT.
func EncodeSubChunk(c *Chunk, e Encoding, ind int) []byte {
	buf := pool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		pool.Put(buf)
	}()
	return encodeSubChunk(buf, c, e, ind)
}

// encodeSubChunk encodes sub chunk ind of the Chunk into buf and returns the bytes read back out of it. The
// returned slice is freshly allocated and does not alias buf. buf must be empty on entry; it is drained but
// not reset, so a caller reusing it has to reset it.
func encodeSubChunk(buf *bytes.Buffer, c *Chunk, e Encoding, ind int) []byte {
	s := c.sub[ind]
	_, _ = buf.Write([]byte{SubChunkVersion, byte(len(s.storages)), uint8(ind + (c.r[0] >> 4))})
	if len(s.storages) != 0 {
		// One boxed palette encoding for the whole sub chunk instead of one per layer: the value is the
		// same for every layer and encodePalettedStorage only calls methods on it. Sub chunks without any
		// layer still box nothing at all.
		pe := paletteEncoding(BlockPaletteEncoding{Blocks: c.br})
		for _, storage := range s.storages {
			encodePalettedStorage(buf, storage, nil, e, pe)
		}
	}
	sub := make([]byte, buf.Len())
	_, _ = buf.Read(sub)
	return sub
}

// EncodeBiomes encodes the biomes of a chunk into bytes. An Encoding may be passed to encode either for network or
// disk purposed, the most notable difference being that the network encoding generally uses varints and no NBT.
func EncodeBiomes(c *Chunk, e Encoding) []byte {
	buf := pool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		pool.Put(buf)
	}()

	var previous *PalettedStorage
	for _, b := range c.biomes {
		encodePalettedStorage(buf, b, previous, e, BiomePaletteEncoding)
		previous = b
	}
	biomes := make([]byte, buf.Len())
	_, _ = buf.Read(biomes)
	return biomes
}

// encodePalettedStorage encodes a PalettedStorage into a bytes.Buffer. The Encoding passed is used to write the Palette
// of the PalettedStorage.
func encodePalettedStorage(buf *bytes.Buffer, storage, previous *PalettedStorage, e Encoding, pe paletteEncoding) {
	if storage.Equal(previous) {
		// The same single byte, written without building a one byte slice for it. bytes.Buffer.WriteByte
		// only ever returns a nil error.
		_ = buf.WriteByte(0x7f<<1 | e.network())
		return
	}
	n := len(storage.indices)*4 + 1
	// Fill the buffer's own spare capacity rather than a scratch slice: the Write below then copies the
	// bytes onto themselves, so encoding a storage costs no allocation.
	buf.Grow(n)
	b := buf.AvailableBuffer()[:n]
	b[0] = byte(storage.bitsPerIndex<<1) | e.network()

	for i, v := range storage.indices {
		// Explicitly don't use the binary package to greatly improve performance of writing the uint32s.
		// Writing through a window of exactly four bytes costs one bounds check where indexing b four
		// times costs four of them.
		w := b[i*4+1 : i*4+5 : i*4+5]
		w[0], w[1], w[2], w[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	_, _ = buf.Write(b)

	e.encodePalette(buf, storage.palette, pe)
}
