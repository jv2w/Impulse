package chunk

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// diskStorage returns the disk encoding of a paletted storage with bits per
// index, the words passed as its indices and palette as its entries, encoded as
// biome entries.
func diskStorage(bits byte, words []uint32, palette []uint32) *bytes.Buffer {
	buf := &bytes.Buffer{}
	buf.WriteByte(bits << 1)
	for _, w := range words {
		_ = binary.Write(buf, binary.LittleEndian, w)
	}
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(palette)))
	for _, v := range palette {
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	return buf
}

// TestDecodeRejectsIndexPastPalette decodes a storage whose first index is 3
// while its palette has two entries. Reading that block used to index the
// palette out of range and panic on the chunk worker, ending the process.
func TestDecodeRejectsIndexPastPalette(t *testing.T) {
	words := make([]uint32, paletteSize(2).uint32s())
	words[0] = 3
	_, err := decodePalettedStorage(diskStorage(2, words, []uint32{10, 11}), DiskEncoding, BiomePaletteEncoding)
	if err == nil {
		t.Fatal("decodePalettedStorage err = nil, want the index past the palette rejected")
	}
	if !strings.Contains(err.Error(), "palette") {
		t.Fatalf("error = %q, want it to name the palette", err.Error())
	}
}

// TestDecodeIgnoresPaddingPastLastIndex decodes a 3 bit storage whose last word
// holds six indices followed by four padding slots filled with 7. The padding
// is not an index, so a palette of two entries is valid and every block reads
// the value it was given.
func TestDecodeIgnoresPaddingPastLastIndex(t *testing.T) {
	words := make([]uint32, paletteSize(3).uint32s())
	for i := range words {
		// Every index is 1, in all ten 3 bit slots of each word.
		for slot := 0; slot < 10; slot++ {
			words[i] |= 1 << (slot * 3)
		}
	}
	last := len(words) - 1
	words[last] = 0
	for slot := 0; slot < 10; slot++ {
		v := uint32(1)
		if slot >= 6 {
			v = 7
		}
		words[last] |= v << (slot * 3)
	}
	s, err := decodePalettedStorage(diskStorage(3, words, []uint32{10, 11}), DiskEncoding, BiomePaletteEncoding)
	if err != nil {
		t.Fatalf("decodePalettedStorage: %v", err)
	}
	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				if v := s.At(x, y, z); v != 11 {
					t.Fatalf("At(%v, %v, %v) = %v, want 11", x, y, z, v)
				}
			}
		}
	}
}
