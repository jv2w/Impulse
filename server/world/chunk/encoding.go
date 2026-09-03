package chunk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/df-mc/worldupgrader/blockupgrader"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type (
	// Encoding is an encoding type used for Chunk encoding. Implementations of this interface are DiskEncoding and
	// NetworkEncoding, which can be used to encode a Chunk to an intermediate disk or network representation respectively.
	Encoding interface {
		encodePalette(buf *bytes.Buffer, p *Palette, e paletteEncoding)
		decodePalette(buf *bytes.Buffer, blockSize paletteSize, e paletteEncoding) (*Palette, error)
		network() byte
	}
	// paletteEncoding is an encoding type used for Chunk encoding. It is used to encode different types of palettes
	// (for example, blocks or biomes) differently.
	paletteEncoding interface {
		encode(buf *bytes.Buffer, v uint32)
		decode(buf *bytes.Buffer) (uint32, error)
	}
)

var (
	// DiskEncoding is the Encoding for writing a Chunk to disk. It writes block palettes using NBT and does not use
	// varints.
	DiskEncoding diskEncoding
	// NetworkEncoding is the Encoding used for sending a Chunk over network. It does not use NBT and writes varints.
	NetworkEncoding networkEncoding
	// BiomePaletteEncoding is the paletteEncoding used for encoding a palette of biomes.
	BiomePaletteEncoding biomePaletteEncoding
)

// biomePaletteEncoding implements the encoding of biome palettes to disk.
type biomePaletteEncoding struct{}

func (biomePaletteEncoding) encode(buf *bytes.Buffer, v uint32) {
	// These are the same four little endian bytes binary.Write writes for a uint32, without boxing v into
	// an any and walking the type switch inside binary.Write. bytes.Buffer.Write never returns an error.
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	_, _ = buf.Write(b[:])
}
func (biomePaletteEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	// binary.Read of a uint32 is exactly this io.ReadFull followed by a little endian decode, so the same
	// io.EOF or io.ErrUnexpectedEOF is returned with a zero value, and &v is no longer boxed into an any.
	var b [4]byte
	if _, err := io.ReadFull(buf, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

// BlockPaletteEncoding implements the encoding of block palettes to disk. It requires a BlockRegistry for converting
// between runtime IDs and block states.
type BlockPaletteEncoding struct {
	Blocks BlockRegistry
}

func (bpe BlockPaletteEncoding) encode(buf *bytes.Buffer, v uint32) {
	_ = nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian).Encode(bpe.EncodeBlockState(v))
}
func (bpe BlockPaletteEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	var m map[string]any
	if err := nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian).Decode(&m); err != nil {
		return 0, fmt.Errorf("error decoding block palette entry: %w", err)
	}
	return bpe.DecodeBlockState(m)
}

func (bpe BlockPaletteEncoding) EncodeBlockState(v uint32) blockEntry {
	// Get the block state registered with the runtime IDs we have in the palette of the block storage
	// as we need the name and data value to store.
	name, props, _ := bpe.Blocks.RuntimeIDToState(v)
	return blockEntry{Name: name, State: props, Version: CurrentBlockVersion}
}

func (bpe BlockPaletteEncoding) DecodeBlockState(m map[string]any) (uint32, error) {
	// Decode the name and version of the block entry.
	name, _ := m["name"].(string)
	version, _ := m["version"].(int32)

	// Now check for a state field.
	stateI, ok := m["states"]
	if version < 17694723 {
		// This entry is a pre-1.13 block state, so decode the meta value instead.
		meta, _ := m["val"].(int16)

		// Upgrade the pre-1.13 state into a post-1.13 state.
		state, ok := upgradeLegacyEntry(name, meta)
		if !ok {
			return 0, fmt.Errorf("cannot find mapping for legacy block entry: %v, %v", name, meta)
		}

		// Update the name, state, and version.
		name = state.Name
		stateI = state.State
		version = state.Version
	} else if !ok {
		// The state is a post-1.13 block state, but the states field is missing, likely due to a broken world
		// conversion.
		stateI = make(map[string]any)
	}
	state, ok := stateI.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("invalid state in block entry")
	}

	// Upgrade the block state if necessary.
	upgraded := blockupgrader.Upgrade(blockupgrader.BlockState{
		Name:       name,
		Properties: state,
		Version:    version,
	})

	v, ok := bpe.Blocks.StateToRuntimeID(upgraded.Name, upgraded.Properties)
	if !ok {
		return 0, fmt.Errorf("cannot get runtime ID of block state %v{%+v} %v", upgraded.Name, upgraded.Properties, upgraded.Version)
	}
	return v, nil
}

// diskEncoding implements the Chunk encoding for writing to disk.
type diskEncoding struct{}

func (diskEncoding) network() byte { return 0 }
func (diskEncoding) encodePalette(buf *bytes.Buffer, p *Palette, e paletteEncoding) {
	if p.size != 0 {
		// Same four little endian bytes as binary.Write for a uint32, without the any boxing and the type
		// switch inside it. bytes.Buffer.Write never returns an error.
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(p.Len()))
		_, _ = buf.Write(b[:])
	}
	for _, v := range p.values {
		e.encode(buf, v)
	}
}
func (diskEncoding) decodePalette(buf *bytes.Buffer, blockSize paletteSize, e paletteEncoding) (*Palette, error) {
	paletteCount := uint32(1)
	if blockSize != 0 {
		// binary.Read of a uint32 is this io.ReadFull plus a little endian decode, so the same error
		// reaches the wrap below and paletteCount no longer has to be boxed into an any.
		var b [4]byte
		if _, err := io.ReadFull(buf, b[:]); err != nil {
			return nil, fmt.Errorf("error reading palette entry count: %w", err)
		}
		paletteCount = binary.LittleEndian.Uint32(b[:])
		// A world file may declare any uint32 here, and the count was previously believed as far as the
		// make below: four bytes of a corrupt or crafted file asked for up to 16 GiB. blockSize bits can
		// only index 1<<blockSize entries, so a larger palette is malformed by definition.
		if paletteCount > 1<<blockSize {
			return nil, fmt.Errorf("invalid palette entry count: found %v, but palette with %v bits per block can hold at most %v values", paletteCount, blockSize, uint32(1)<<blockSize)
		}
	}

	var err error
	palette := newPalette(blockSize, make([]uint32, paletteCount))
	for i := uint32(0); i < paletteCount; i++ {
		palette.values[i], err = e.decode(buf)
		if err != nil {
			return nil, err
		}
	}
	if paletteCount == 0 {
		return palette, fmt.Errorf("invalid palette entry count: found 0, but palette with %v bits per block must have at least 1 value", blockSize)
	}
	return palette, nil
}

// networkEncoding implements the Chunk encoding for sending over network.
type networkEncoding struct{}

func (networkEncoding) network() byte { return 1 }
func (networkEncoding) encodePalette(buf *bytes.Buffer, p *Palette, _ paletteEncoding) {
	if p.size != 0 {
		_ = protocol.WriteVarint32(buf, int32(p.Len()))
	}
	for _, val := range p.values {
		_ = protocol.WriteVarint32(buf, int32(val))
	}
}
func (networkEncoding) decodePalette(buf *bytes.Buffer, blockSize paletteSize, _ paletteEncoding) (*Palette, error) {
	var paletteCount int32 = 1
	if blockSize != 0 {
		if err := protocol.Varint32(buf, &paletteCount); err != nil {
			return nil, fmt.Errorf("error reading palette entry count: %w", err)
		}
		if paletteCount <= 0 {
			return nil, fmt.Errorf("invalid palette entry count %v", paletteCount)
		}
		// The same unbounded count as in diskEncoding.decodePalette, reached from the wire through the
		// exported NetworkDecode: a varint32 of 50 million made a 190 MiB slice before the stream was
		// found to be empty. 1<<blockSize is the largest palette the indices can reach.
		if paletteCount > 1<<blockSize {
			return nil, fmt.Errorf("invalid palette entry count %v: palette with %v bits per block can hold at most %v values", paletteCount, blockSize, int32(1)<<blockSize)
		}
	}

	blocks, temp := make([]uint32, paletteCount), int32(0)
	for i := int32(0); i < paletteCount; i++ {
		if err := protocol.Varint32(buf, &temp); err != nil {
			return nil, fmt.Errorf("error decoding palette entry: %w", err)
		}
		blocks[i] = uint32(temp)
	}
	return newPalette(blockSize, blocks), nil
}
