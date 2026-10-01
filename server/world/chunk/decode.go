package chunk

import (
	"bytes"
	"fmt"

	"github.com/jv2w/Impulse/server/block/cube"
)

// NetworkDecode decodes the network serialised data passed into a Chunk if successful. If not, the chunk
// returned is nil and the error non-nil.
// The sub chunk count passed must be that found in the LevelChunk packet.
// NetworkDecode creates a new buffer and calls NetworkDecodeBuffer.
//
// The BlockRegistry passed must be finalized and must correspond to the runtime IDs used in the chunk data.
// noinspection GoUnusedExportedFunction
func NetworkDecode(br BlockRegistry, data []byte, count int, r cube.Range) (*Chunk, error) {
	return NetworkDecodeBuffer(br, bytes.NewBuffer(data), count, r)
}

// NetworkDecodeBuffer decodes the network serialised data from buf passed into a Chunk if successful. If not, the chunk
// returned is nil and the error non-nil.
// The sub chunk count passed must be that found in the LevelChunk packet.
// noinspection GoUnusedExportedFunction
func NetworkDecodeBuffer(br BlockRegistry, buf *bytes.Buffer, count int, r cube.Range) (*Chunk, error) {
	c := New(br, r)
	// The declared count may exceed the number of sub-chunks supported by the
	// chunk's vertical range, so validate it before indexing c.sub.
	if count < 0 || count > len(c.sub) {
		return nil, fmt.Errorf("invalid sub-chunk count %d: chunk range has %d sub-chunks", count, len(c.sub))
	}
	for i := 0; i < count; i++ {
		index := uint8(i)
		sub, err := decodeSubChunk(buf, c, &index, NetworkEncoding)
		if err != nil {
			return nil, err
		}
		// Version 9 sub-chunks replace index with an absolute Y from the payload.
		// Validate that translated index before using it to access the chunk.
		if int(index) >= len(c.sub) {
			return nil, fmt.Errorf("invalid sub-chunk index %d: chunk range has %d sub-chunks", index, len(c.sub))
		}
		c.sub[index] = sub
	}
	var last *PalettedStorage
	for i := 0; i < len(c.sub); i++ {
		b, err := decodePalettedStorage(buf, NetworkEncoding, BiomePaletteEncoding)
		if err != nil {
			return nil, err
		}
		if b == nil {
			// b == nil means this paletted storage had the flag pointing to the previous one. It basically means we should
			// inherit whatever palette we decoded last.
			if i == 0 {
				// This should never happen and there is no way to handle this.
				return nil, fmt.Errorf("first biome storage pointed to previous one")
			}
			// A copy, not the pointer: sub chunks that happened to encode identically must still have
			// independent biomes afterwards.
			b = last.Clone()
		} else {
			last = b
		}
		c.biomes[i] = b
	}
	return c, nil
}

// DiskDecode decodes the data from a SerialisedData object into a chunk and returns it. If the data was invalid,
// an error is returned.
//
// The BlockRegistry passed must be finalized and must correspond to the runtime IDs used in the chunk data.
func DiskDecode(br BlockRegistry, data SerialisedData, r cube.Range) (*Chunk, error) {
	c := New(br, r)

	err := decodeBiomes(bytes.NewBuffer(data.Biomes), c, DiskEncoding)
	if err != nil {
		return nil, err
	}
	// One buffer re-seated onto each sub chunk's bytes. A bytes.Buffer only wraps the slice it is given and
	// nothing here keeps a reference to it past the call, so this is a fresh buffer per sub chunk without
	// allocating one per sub chunk.
	var buf bytes.Buffer
	for i, sub := range data.SubChunks {
		if len(sub) == 0 {
			// No data for this sub chunk.
			continue
		}
		buf = *bytes.NewBuffer(sub)
		index := uint8(i)
		s, err := decodeSubChunk(&buf, c, &index, DiskEncoding)
		if err != nil {
			return nil, err
		}
		// Version 9 sub-chunks replace index with an absolute Y from the payload.
		// Validate that translated index before using it to access the chunk.
		if int(index) >= len(c.sub) {
			return nil, fmt.Errorf("invalid sub-chunk index %d: chunk range has %d sub-chunks", index, len(c.sub))
		}
		c.sub[index] = s
	}
	return c, nil
}

// decodeSubChunk decodes a SubChunk from a bytes.Buffer. The Encoding passed defines how the block storages of the
// SubChunk are decoded.
func decodeSubChunk(buf *bytes.Buffer, c *Chunk, index *byte, e Encoding) (*SubChunk, error) {
	ver, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading version: %w", err)
	}
	sub := NewSubChunk(c.air)
	switch ver {
	default:
		return nil, fmt.Errorf("unknown sub chunk version %v: can't decode", ver)
	case 1:
		// Version 1 only has one layer for each sub chunk, but uses the format with palettes.
		storage, err := decodePalettedStorage(buf, e, BlockPaletteEncoding{Blocks: c.br, legacy: &c.legacyStates})
		if err != nil {
			return nil, err
		}
		sub.storages = append(sub.storages, storage)
	case 8, 9:
		// Version 8 allows up to 256 layers for one sub chunk.
		storageCount, err := buf.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("error reading storage count: %w", err)
		}
		if ver == 9 {
			uIndex, err := buf.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("error reading sub-chunk index: %w", err)
			}
			// The index as written here isn't the actual index of the sub-chunk within the chunk. Rather, it is the Y
			// value of the sub-chunk. This means that we need to translate it to an index.
			*index = uint8(int8(uIndex) - int8(c.r[0]>>4))
		}
		sub.storages = make([]*PalettedStorage, storageCount)

		if storageCount != 0 {
			// One boxed palette encoding for the whole sub chunk instead of one per layer: the value is
			// the same for every layer, and decodePalettedStorage only calls methods on it and prints its
			// type, which does not change either.
			pe := paletteEncoding(BlockPaletteEncoding{Blocks: c.br})
			for i := byte(0); i < storageCount; i++ {
				sub.storages[i], err = decodePalettedStorage(buf, e, pe)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return sub, nil
}

// decodeBiomes reads the paletted storages holding biomes from buf and stores it into the Chunk passed.
func decodeBiomes(buf *bytes.Buffer, c *Chunk, e Encoding) error {
	var last *PalettedStorage
	if buf.Len() != 0 {
		for i := 0; i < len(c.sub); i++ {
			b, err := decodePalettedStorage(buf, e, BiomePaletteEncoding)
			if err != nil {
				return err
			}
			// b == nil means this paletted storage had the flag pointing to the previous one. It basically means we should
			// inherit whatever palette we decoded last.
			if i == 0 && b == nil {
				// This should never happen and there is no way to handle this.
				return fmt.Errorf("first biome storage pointed to previous one")
			}
			if b == nil {
				// This means this paletted storage had the flag pointing to the previous one. It basically means we should
				// inherit whatever palette we decoded last. A copy, not the pointer: sub chunks that happened to
				// encode identically must still have independent biomes afterwards.
				b = last.Clone()
			} else {
				last = b
			}
			c.biomes[i] = b
		}
	}
	return nil
}

// decodePalettedStorage decodes a PalettedStorage from a bytes.Buffer. The Encoding passed is used to read either a
// network or disk block storage.
func decodePalettedStorage(buf *bytes.Buffer, e Encoding, pe paletteEncoding) (*PalettedStorage, error) {
	blockSize, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading block size: %w", err)
	}
	blockSize >>= 1
	if blockSize == 0x7f {
		return nil, nil
	}

	size := paletteSize(blockSize)
	if !size.valid() {
		return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: invalid palette size", blockSize, pe)
	}
	uint32Count := size.uint32s()

	uint32s := make([]uint32, uint32Count)
	byteCount := uint32Count * 4

	data := buf.Next(byteCount)
	if len(data) != byteCount {
		return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: not enough block data present: expected %v bytes, got %v", blockSize, pe, byteCount, len(data))
	}
	for i := 0; i < uint32Count; i++ {
		// Explicitly don't use the binary package to greatly improve performance of reading the uint32s.
		// Reading through a window of exactly four bytes costs one bounds check where indexing data four
		// times costs four of them. The length of data was checked against byteCount above.
		d := data[i*4 : i*4+4 : i*4+4]
		uint32s[i] = uint32(d[0]) | uint32(d[1])<<8 | uint32(d[2])<<16 | uint32(d[3])<<24
	}
	p, err := e.decodePalette(buf, paletteSize(blockSize), pe)
	storage := newPalettedStorage(uint32s, p)
	if err != nil {
		return storage, err
	}
	// Which input was the problem: a storage with an index past the end of its
	// palette, such as index 3 with two entries. It decoded, and the first read
	// of that block indexed the palette out of range and panicked, on a chunk
	// worker with nothing to recover it, ending the process.
	if maxIndex := storage.maxPaletteIndex(); int(maxIndex) >= p.Len() {
		return nil, fmt.Errorf("paletted storage index %v is past the end of its palette of %v entries", maxIndex, p.Len())
	}
	return storage, nil
}
