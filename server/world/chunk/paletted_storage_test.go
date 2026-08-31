package chunk

import (
	"fmt"
	"testing"
)

// storageSize describes one canonical PalettedStorage size class. filled is 32/bits*bits, which is 32 for the
// power-of-two sizes but 30 for 3, 5 and 6: those leave the top two bits of every uint32 unused, and pay for it
// with one extra trailing word.
type storageSize struct {
	size    paletteSize
	uint32s int
	filled  uint16
	mask    uint32
}

var storageSizes = []storageSize{
	{size: 1, uint32s: 128, filled: 32, mask: 0x1},
	{size: 2, uint32s: 256, filled: 32, mask: 0x3},
	{size: 3, uint32s: 410, filled: 30, mask: 0x7},
	{size: 4, uint32s: 512, filled: 32, mask: 0xf},
	{size: 5, uint32s: 683, filled: 30, mask: 0x1f},
	{size: 6, uint32s: 820, filled: 30, mask: 0x3f},
	{size: 8, uint32s: 1024, filled: 32, mask: 0xff},
	{size: 16, uint32s: 2048, filled: 32, mask: 0xffff},
}

// storageOfSize builds a storage whose backing slice has exactly the length a palette of size s requires. The
// palette holds a single dummy value: tests built on this one only drive paletteIndex/setPaletteIndex, which
// never dereference the palette.
func storageOfSize(s paletteSize) *PalettedStorage {
	return newPalettedStorage(make([]uint32, s.uint32s()), newPalette(s, []uint32{ridSolid}))
}

// storageCoords splits a linear index back into the x, y, z that produce it. The storage packs indices as
// (x<<8)|(z<<4)|y, so x is the slowest axis and y the fastest.
func storageCoords(linear int) (x, y, z byte) {
	return byte(linear >> 8), byte(linear & 15), byte((linear >> 4) & 15)
}

// storageAssertWords fails unless every word of the storage matches want, which maps word index to value. Words
// absent from want must be zero, so a golden case pins the whole slice, not just the words it names.
func storageAssertWords(t *testing.T, storage *PalettedStorage, want map[int]uint32) {
	t.Helper()
	for i, got := range storage.indices {
		if got != want[i] {
			t.Fatalf("indices[%d] = %#08x, want %#08x", i, got, want[i])
		}
	}
}

func TestPalettedStorageEmptyStorage(t *testing.T) {
	storage := emptyStorage(ridSolid)

	if storage.bitsPerIndex != 0 || storage.filledBitsPerIndex != 0 {
		t.Fatalf("bitsPerIndex, filledBitsPerIndex = %d, %d, want 0, 0", storage.bitsPerIndex, storage.filledBitsPerIndex)
	}
	if storage.indexMask != 0 {
		t.Fatalf("indexMask = %#x, want 0", storage.indexMask)
	}
	if len(storage.indices) != 0 {
		t.Fatalf("len(indices) = %d, want 0", len(storage.indices))
	}
	if storage.palette.Len() != 1 || storage.palette.size != 0 {
		t.Fatalf("palette Len, size = %d, %d, want 1, 0", storage.palette.Len(), storage.palette.size)
	}

	// With 0 bits per index there is nothing to read: every coordinate resolves to palette index 0.
	for _, c := range [][3]byte{{0, 0, 0}, {15, 15, 15}, {7, 3, 11}, {1, 14, 2}} {
		if got := storage.At(c[0], c[1], c[2]); got != ridSolid {
			t.Fatalf("At%v = %d, want %d", c, got, ridSolid)
		}
		if got := storage.PaletteIndex(c[0], c[1], c[2]); got != 0 {
			t.Fatalf("PaletteIndex%v = %d, want 0", c, got)
		}
	}
}

func TestPalettedStorageEmptyStorageSetSameValueIsNoOp(t *testing.T) {
	storage := emptyStorage(ridSolid)
	storage.Set(4, 5, 6, ridSolid)

	if len(storage.indices) != 0 || storage.bitsPerIndex != 0 {
		t.Fatalf("len(indices), bitsPerIndex = %d, %d, want 0, 0", len(storage.indices), storage.bitsPerIndex)
	}
	if storage.palette.Len() != 1 {
		t.Fatalf("palette.Len() = %d, want 1", storage.palette.Len())
	}
	if got := storage.At(4, 5, 6); got != ridSolid {
		t.Fatalf("At(4, 5, 6) = %d, want %d", got, ridSolid)
	}
}

func TestPalettedStorageEmptyStorageSetNewValueGrows(t *testing.T) {
	storage := emptyStorage(ridSolid)
	storage.Set(4, 5, 6, ridGlass)

	if storage.bitsPerIndex != 1 || storage.filledBitsPerIndex != 32 || storage.indexMask != 1 {
		t.Fatalf("bits, filled, mask = %d, %d, %#x, want 1, 32, 0x1", storage.bitsPerIndex, storage.filledBitsPerIndex, storage.indexMask)
	}
	if len(storage.indices) != 128 {
		t.Fatalf("len(indices) = %d, want 128", len(storage.indices))
	}
	if got := storage.At(4, 5, 6); got != ridGlass {
		t.Fatalf("At(4, 5, 6) = %d, want %d", got, ridGlass)
	}
	// Everything not written keeps palette index 0, which is still the value emptyStorage was built with.
	if got := storage.At(0, 0, 0); got != ridSolid {
		t.Fatalf("At(0, 0, 0) = %d, want %d", got, ridSolid)
	}
}

func TestPalettedStorageDerivesSizeFromIndicesLength(t *testing.T) {
	for _, c := range storageSizes {
		t.Run(fmt.Sprintf("bits%d", c.size), func(t *testing.T) {
			if got := c.size.uint32s(); got != c.uint32s {
				t.Fatalf("paletteSize(%d).uint32s() = %d, want %d", c.size, got, c.uint32s)
			}
			storage := storageOfSize(c.size)
			// bitsPerIndex is len(indices)/128, never passed in, so the slice length is the only thing that
			// decides how the storage reads itself back.
			if got := storage.bitsPerIndex; got != uint16(c.size) {
				t.Fatalf("bitsPerIndex = %d, want %d", got, c.size)
			}
			if got := storage.filledBitsPerIndex; got != c.filled {
				t.Fatalf("filledBitsPerIndex = %d, want %d", got, c.filled)
			}
			if got := storage.indexMask; got != c.mask {
				t.Fatalf("indexMask = %#x, want %#x", got, c.mask)
			}
		})
	}
}

func TestPalettedStorageZeroSizeDerivation(t *testing.T) {
	storage := newPalettedStorage([]uint32{}, newPalette(0, []uint32{ridSolid}))
	if storage.bitsPerIndex != 0 || storage.filledBitsPerIndex != 0 {
		t.Fatalf("bits, filled = %d, %d, want 0, 0", storage.bitsPerIndex, storage.filledBitsPerIndex)
	}
	// (1 << 0) - 1 == 0, so a 0-bit storage has an all-zero mask rather than an all-ones one.
	if storage.indexMask != 0 {
		t.Fatalf("indexMask = %#x, want 0", storage.indexMask)
	}
}

func TestPalettedStorageBitPackingGoldenWords(t *testing.T) {
	type write struct {
		linear int
		index  uint16
	}
	cases := []struct {
		size   paletteSize
		writes []write
		want   map[int]uint32
	}{
		{
			// 1 bit, 32 per word. Linear indices 0, 3, 15 and 16 all land in word 0 at bit == linear.
			size:   1,
			writes: []write{{0, 1}, {3, 1}, {15, 1}, {16, 1}},
			want:   map[int]uint32{0: 1<<0 | 1<<3 | 1<<15 | 1<<16},
		},
		{
			// 2 bits, 16 per word. Bit offset is linear*2, so linear 15 occupies bits 30..31.
			size:   2,
			writes: []write{{0, 3}, {1, 2}, {15, 1}},
			want:   map[int]uint32{0: 3<<0 | 2<<2 | 1<<30},
		},
		{
			// 3 bits, padded: filledBitsPerIndex is 30, so linear 9 ends exactly at bit 30 and linear 10
			// starts word 1. Bits 30 and 31 of word 0 are never written.
			size:   3,
			writes: []write{{0, 7}, {9, 5}, {10, 7}},
			want:   map[int]uint32{0: 7<<0 | 5<<27, 1: 7},
		},
		{
			// 4 bits, 8 per word. Linear 7 sits in the top nibble.
			size:   4,
			writes: []write{{0, 15}, {7, 9}},
			want:   map[int]uint32{0: 15<<0 | 9<<28},
		},
		{
			// 5 bits, padded: 6 indices per word cover bits 0..29, so linear 6 rolls to word 1.
			size:   5,
			writes: []write{{0, 31}, {5, 21}, {6, 31}},
			want:   map[int]uint32{0: 31<<0 | 21<<25, 1: 31},
		},
		{
			// 6 bits, padded: 5 indices per word cover bits 0..29, so linear 5 rolls to word 1.
			size:   6,
			writes: []write{{0, 63}, {4, 42}, {5, 63}},
			want:   map[int]uint32{0: 63<<0 | 42<<24, 1: 63},
		},
		{
			// 8 bits, 4 per word, little-endian byte order within the word.
			size:   8,
			writes: []write{{0, 0xab}, {3, 0xcd}},
			want:   map[int]uint32{0: 0xab | 0xcd<<24},
		},
		{
			// 16 bits, 2 per word: the first index is the low half.
			size:   16,
			writes: []write{{0, 0xbeef}, {1, 0xcafe}},
			want:   map[int]uint32{0: 0xbeef | 0xcafe<<16},
		},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("bits%d", c.size), func(t *testing.T) {
			storage := storageOfSize(c.size)
			for _, w := range c.writes {
				x, y, z := storageCoords(w.linear)
				storage.setPaletteIndex(x, y, z, w.index)
			}
			storageAssertWords(t, storage, c.want)
			for _, w := range c.writes {
				x, y, z := storageCoords(w.linear)
				if got := storage.paletteIndex(x, y, z); got != w.index {
					t.Fatalf("paletteIndex(%d, %d, %d) = %d, want %d", x, y, z, got, w.index)
				}
			}
		})
	}
}

func TestPalettedStorageSaturatedWordLayout(t *testing.T) {
	// Writing the maximum index everywhere exposes the padding directly: the padded sizes cap every full word
	// at 0x3FFFFFFF (30 bits) and leave a short trailing word, while the power-of-two sizes fill 32 bits.
	cases := []struct {
		size     paletteSize
		full     uint32
		fullUpTo int    // number of completely filled words
		tail     uint32 // value of the single trailing word, 0 if there is none
	}{
		{size: 1, full: 0xffffffff, fullUpTo: 128},
		{size: 2, full: 0xffffffff, fullUpTo: 256},
		// 10 indices per word: words 0..408 hold 4090 indices, the last 6 occupy bits 0..17.
		{size: 3, full: 0x3fffffff, fullUpTo: 409, tail: 0x0003ffff},
		{size: 4, full: 0xffffffff, fullUpTo: 512},
		// 6 per word: words 0..681 hold 4092 indices, the last 4 occupy bits 0..19.
		{size: 5, full: 0x3fffffff, fullUpTo: 682, tail: 0x000fffff},
		// 5 per word: words 0..818 hold 4095 indices, the last one occupies bits 0..5.
		{size: 6, full: 0x3fffffff, fullUpTo: 819, tail: 0x0000003f},
		{size: 8, full: 0xffffffff, fullUpTo: 1024},
		{size: 16, full: 0xffffffff, fullUpTo: 2048},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("bits%d", c.size), func(t *testing.T) {
			storage := storageOfSize(c.size)
			maxIndex := uint16(storage.indexMask)
			for linear := 0; linear < 4096; linear++ {
				x, y, z := storageCoords(linear)
				storage.setPaletteIndex(x, y, z, maxIndex)
			}
			for i, got := range storage.indices {
				want := c.full
				if i >= c.fullUpTo {
					want = c.tail
				}
				if got != want {
					t.Fatalf("indices[%d] = %#08x, want %#08x", i, got, want)
				}
			}
			for linear := 0; linear < 4096; linear++ {
				x, y, z := storageCoords(linear)
				if got := storage.paletteIndex(x, y, z); got != maxIndex {
					t.Fatalf("paletteIndex at linear %d = %d, want %d", linear, got, maxIndex)
				}
			}
		})
	}
}

func TestPalettedStorageFullRoundTrip(t *testing.T) {
	// Each cardinality lands on exactly one size class: the palette grows when its length exceeds 1<<size.
	cases := []struct {
		values   int
		wantBits uint16
		uint32s  int
	}{
		{values: 2, wantBits: 1, uint32s: 128},
		{values: 4, wantBits: 2, uint32s: 256},
		{values: 8, wantBits: 3, uint32s: 410},
		{values: 16, wantBits: 4, uint32s: 512},
		{values: 32, wantBits: 5, uint32s: 683},
		{values: 64, wantBits: 6, uint32s: 820},
		{values: 256, wantBits: 8, uint32s: 1024},
		{values: 300, wantBits: 16, uint32s: 2048},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("values%d", c.values), func(t *testing.T) {
			// Values 1..n, assigned as linear%n+1. Iterating x, then z, then y walks linear order, so the
			// palette collects the values in ascending order and palette index i holds value i+1.
			storage := emptyStorage(1)
			for x := byte(0); x < 16; x++ {
				for z := byte(0); z < 16; z++ {
					for y := byte(0); y < 16; y++ {
						linear := int(x)<<8 | int(z)<<4 | int(y)
						storage.Set(x, y, z, uint32(linear%c.values)+1)
					}
				}
			}
			if got := storage.palette.Len(); got != c.values {
				t.Fatalf("palette.Len() = %d, want %d", got, c.values)
			}
			if got := storage.bitsPerIndex; got != c.wantBits {
				t.Fatalf("bitsPerIndex = %d, want %d", got, c.wantBits)
			}
			if got := len(storage.indices); got != c.uint32s {
				t.Fatalf("len(indices) = %d, want %d", got, c.uint32s)
			}
			for linear := 0; linear < 4096; linear++ {
				x, y, z := storageCoords(linear)
				wantValue := uint32(linear%c.values) + 1
				if got := storage.At(x, y, z); got != wantValue {
					t.Fatalf("At(%d, %d, %d) = %d, want %d", x, y, z, got, wantValue)
				}
				if got := storage.PaletteIndex(x, y, z); got != uint16(linear%c.values) {
					t.Fatalf("PaletteIndex(%d, %d, %d) = %d, want %d", x, y, z, got, linear%c.values)
				}
			}
		})
	}
}

func TestPalettedStorageCoordinatesWrap(t *testing.T) {
	storage := emptyStorage(ridAir)
	storage.Set(0, 0, 0, ridSolid)
	storage.Set(1, 2, 3, ridGlass)

	// Coordinates are masked with &15 rather than validated: out-of-range input silently aliases.
	cases := []struct {
		x, y, z byte
		want    uint32
	}{
		{16, 0, 0, ridSolid},
		{0, 16, 0, ridSolid},
		{0, 0, 16, ridSolid},
		{240, 240, 240, ridSolid},
		{17, 18, 19, ridGlass},
	}
	for _, c := range cases {
		if got := storage.At(c.x, c.y, c.z); got != c.want {
			t.Fatalf("At(%d, %d, %d) = %d, want %d", c.x, c.y, c.z, got, c.want)
		}
	}
	// Set wraps identically, so writing at 16 overwrites 0.
	storage.Set(16, 0, 0, ridGlass)
	if got := storage.At(0, 0, 0); got != ridGlass {
		t.Fatalf("At(0, 0, 0) after Set(16, 0, 0) = %d, want %d", got, ridGlass)
	}
}

func TestPalettedStorageResizePreservesValues(t *testing.T) {
	storage := emptyStorage(ridAir)
	written := map[int]uint32{}
	// Eight new values on top of the initial air make a palette of nine, driving the storage through
	// sizes 1, 2, 3 and 4.
	values := []uint32{ridSolid, ridGlass, ridLeaves, ridLamp, ridTorch, ridWater, ridSapling, ridChest}
	for i, v := range values {
		linear := i*457 + 13
		x, y, z := storageCoords(linear % 4096)
		storage.Set(x, y, z, v)
		written[linear%4096] = v
	}
	if got := storage.bitsPerIndex; got != 4 {
		t.Fatalf("bitsPerIndex = %d, want 4", got)
	}
	if got := len(storage.indices); got != 512 {
		t.Fatalf("len(indices) = %d, want 512", got)
	}
	for linear, want := range written {
		x, y, z := storageCoords(linear)
		if got := storage.At(x, y, z); got != want {
			t.Fatalf("At(%d, %d, %d) = %d, want %d", x, y, z, got, want)
		}
	}
	// Positions never written keep palette index 0, the value emptyStorage started from.
	for linear := 0; linear < 4096; linear++ {
		if _, ok := written[linear]; ok {
			continue
		}
		x, y, z := storageCoords(linear)
		if got := storage.At(x, y, z); got != ridAir {
			t.Fatalf("At(%d, %d, %d) = %d, want %d", x, y, z, got, ridAir)
		}
	}
}

func TestPalettedStorageResizeIsNoOpAtSameSize(t *testing.T) {
	storage := emptyStorage(ridAir)
	storage.Set(0, 0, 0, ridSolid)
	before := &storage.indices[0]

	storage.resize(1)

	if &storage.indices[0] != before {
		t.Fatal("resize to the current size reallocated the indices slice")
	}
	if got := storage.At(0, 0, 0); got != ridSolid {
		t.Fatalf("At(0, 0, 0) = %d, want %d", got, ridSolid)
	}
}

func TestPalettedStorageResizeStalesStructCopies(t *testing.T) {
	storage := emptyStorage(ridAir)
	storage.Set(0, 0, 0, ridSolid)

	// resize does *storage = *newStorage, so a struct value taken beforehand keeps the old, narrower indices
	// slice while sharing the pointer to the palette that keeps growing. Current behaviour, pinned.
	stale := *storage

	storage.Set(0, 0, 1, ridGlass) // Third palette entry: forces the 1 -> 2 bit resize.

	if storage.bitsPerIndex != 2 || len(storage.indices) != 256 {
		t.Fatalf("bits, len(indices) = %d, %d, want 2, 256", storage.bitsPerIndex, len(storage.indices))
	}
	if stale.bitsPerIndex != 1 || len(stale.indices) != 128 {
		t.Fatalf("stale bits, len(indices) = %d, %d, want 1, 128", stale.bitsPerIndex, len(stale.indices))
	}
	if stale.palette != storage.palette {
		t.Fatal("stale copy no longer shares the palette pointer")
	}
	if got := storage.At(0, 0, 1); got != ridGlass {
		t.Fatalf("At(0, 0, 1) = %d, want %d", got, ridGlass)
	}
	// The stale copy never saw the write, so it still reports the old value at that position.
	if got := stale.At(0, 0, 1); got != ridAir {
		t.Fatalf("stale.At(0, 0, 1) = %d, want %d", got, ridAir)
	}
	// Writes through the stale copy land in the abandoned array and are invisible to the live storage.
	stale.Set(0, 5, 0, ridSolid)
	if got := storage.At(0, 5, 0); got != ridAir {
		t.Fatalf("At(0, 5, 0) after stale write = %d, want %d", got, ridAir)
	}
}

func TestPalettedStorageCompactEmptyPaletteReturnsEarly(t *testing.T) {
	// Len() == 0 short-circuits before the scan. Without it the scan would index a zero-length usedIndices.
	storage := newPalettedStorage(make([]uint32, 128), newPalette(1, nil))
	storage.compact()

	if len(storage.indices) != 128 || storage.bitsPerIndex != 1 {
		t.Fatalf("len(indices), bits = %d, %d, want 128, 1", len(storage.indices), storage.bitsPerIndex)
	}
	if storage.palette.Len() != 0 || storage.palette.size != 1 {
		t.Fatalf("palette Len, size = %d, %d, want 0, 1", storage.palette.Len(), storage.palette.size)
	}
}

func TestPalettedStorageCompactSingleValueDropsIndices(t *testing.T) {
	palette := newPalette(1, []uint32{ridSolid})
	storage := newPalettedStorage(make([]uint32, 128), palette)
	storage.compact()

	if storage.indices != nil || storage.indicesStart != nil {
		t.Fatalf("indices, indicesStart = %v, %v, want nil, nil", storage.indices, storage.indicesStart)
	}
	if storage.bitsPerIndex != 0 || storage.filledBitsPerIndex != 0 || storage.indexMask != 0 {
		t.Fatalf("bits, filled, mask = %d, %d, %#x, want 0, 0, 0", storage.bitsPerIndex, storage.filledBitsPerIndex, storage.indexMask)
	}
	// The fast path mutates the existing palette in place instead of building a new one.
	if storage.palette != palette {
		t.Fatal("single-value compact replaced the palette")
	}
	if palette.size != 0 || palette.Len() != 1 {
		t.Fatalf("palette size, Len = %d, %d, want 0, 1", palette.size, palette.Len())
	}
	if got := storage.At(9, 9, 9); got != ridSolid {
		t.Fatalf("At(9, 9, 9) = %d, want %d", got, ridSolid)
	}
}

func TestPalettedStorageCompactRenumbersInAscendingOrder(t *testing.T) {
	palette := newPalette(2, []uint32{ridAir, ridSolid, ridGlass, ridLeaves})
	storage := newPalettedStorage(make([]uint32, 256), palette)
	storage.setPaletteIndex(0, 0, 0, 3) // ridLeaves
	storage.setPaletteIndex(0, 1, 0, 1) // ridSolid
	// Index 2 (ridGlass) is never used and must be dropped.

	storage.compact()

	// Used old indices 0, 1, 3 renumber to 0, 1, 2, preserving ascending old-index order.
	if got := storage.palette.Len(); got != 3 {
		t.Fatalf("palette.Len() = %d, want 3", got)
	}
	wantValues := []uint32{ridAir, ridSolid, ridLeaves}
	for i, want := range wantValues {
		if got := storage.palette.Value(uint16(i)); got != want {
			t.Fatalf("palette.Value(%d) = %d, want %d", i, got, want)
		}
	}
	// paletteSizeFor(3) is still 2, so the storage keeps its width.
	if storage.bitsPerIndex != 2 || len(storage.indices) != 256 {
		t.Fatalf("bits, len(indices) = %d, %d, want 2, 256", storage.bitsPerIndex, len(storage.indices))
	}
	if got := storage.At(0, 0, 0); got != ridLeaves {
		t.Fatalf("At(0, 0, 0) = %d, want %d", got, ridLeaves)
	}
	if got := storage.PaletteIndex(0, 0, 0); got != 2 {
		t.Fatalf("PaletteIndex(0, 0, 0) = %d, want 2", got)
	}
	if got := storage.At(0, 1, 0); got != ridSolid {
		t.Fatalf("At(0, 1, 0) = %d, want %d", got, ridSolid)
	}
	if got := storage.At(5, 5, 5); got != ridAir {
		t.Fatalf("At(5, 5, 5) = %d, want %d", got, ridAir)
	}
}

func TestPalettedStorageCompactShrinksWidth(t *testing.T) {
	palette := newPalette(2, []uint32{ridAir, ridSolid, ridGlass, ridLeaves})
	storage := newPalettedStorage(make([]uint32, 256), palette)
	storage.setPaletteIndex(3, 3, 3, 1) // Only indices 0 and 1 end up in use.

	storage.compact()

	// Two values fit in 1 bit, so the storage halves its backing slice.
	if storage.bitsPerIndex != 1 || len(storage.indices) != 128 {
		t.Fatalf("bits, len(indices) = %d, %d, want 1, 128", storage.bitsPerIndex, len(storage.indices))
	}
	if got := storage.palette.Len(); got != 2 {
		t.Fatalf("palette.Len() = %d, want 2", got)
	}
	if storage.palette.size != 1 {
		t.Fatalf("palette.size = %d, want 1", storage.palette.size)
	}
	if got := storage.At(3, 3, 3); got != ridSolid {
		t.Fatalf("At(3, 3, 3) = %d, want %d", got, ridSolid)
	}
	if got := storage.At(0, 0, 0); got != ridAir {
		t.Fatalf("At(0, 0, 0) = %d, want %d", got, ridAir)
	}
}

func TestPalettedStorageCompactKeepsOptimalStorageUntouched(t *testing.T) {
	palette := newPalette(2, []uint32{ridAir, ridSolid, ridGlass, ridLeaves})
	storage := newPalettedStorage(make([]uint32, 256), palette)
	storage.setPaletteIndex(0, 0, 0, 1)
	storage.setPaletteIndex(0, 1, 0, 2)
	storage.setPaletteIndex(0, 2, 0, 3)
	before := &storage.indices[0]

	storage.compact()

	// Every entry used and paletteSizeFor(4) == 2, so compact returns without reallocating anything.
	if &storage.indices[0] != before {
		t.Fatal("compact reallocated an already-optimal storage")
	}
	if storage.palette != palette {
		t.Fatal("compact replaced the palette of an already-optimal storage")
	}
	if got := storage.palette.Len(); got != 4 {
		t.Fatalf("palette.Len() = %d, want 4", got)
	}
}

func TestPalettedStorageCompactAfterFullFill(t *testing.T) {
	// Fill with 40 values (size 6), then overwrite everything with 3 of them so only those survive compaction.
	storage := emptyStorage(1)
	for linear := 0; linear < 4096; linear++ {
		x, y, z := storageCoords(linear)
		storage.Set(x, y, z, uint32(linear%40)+1)
	}
	if storage.bitsPerIndex != 6 {
		t.Fatalf("bitsPerIndex = %d, want 6", storage.bitsPerIndex)
	}
	for linear := 0; linear < 4096; linear++ {
		x, y, z := storageCoords(linear)
		storage.Set(x, y, z, uint32(linear%3)+1)
	}

	storage.compact()

	if got := storage.palette.Len(); got != 3 {
		t.Fatalf("palette.Len() = %d, want 3", got)
	}
	// paletteSizeFor(3) == 2.
	if storage.bitsPerIndex != 2 || len(storage.indices) != 256 {
		t.Fatalf("bits, len(indices) = %d, %d, want 2, 256", storage.bitsPerIndex, len(storage.indices))
	}
	for linear := 0; linear < 4096; linear++ {
		x, y, z := storageCoords(linear)
		want := uint32(linear%3) + 1
		if got := storage.At(x, y, z); got != want {
			t.Fatalf("At(%d, %d, %d) = %d, want %d", x, y, z, got, want)
		}
	}
}

func TestPalettedStorageCompactStalesPreviousPalettePointer(t *testing.T) {
	palette := newPalette(2, []uint32{ridAir, ridSolid, ridGlass, ridLeaves})
	storage := newPalettedStorage(make([]uint32, 256), palette)
	storage.setPaletteIndex(0, 0, 0, 1)

	storage.compact()

	// The renumbering path installs a brand new Palette, so a pointer taken from Palette() beforehand no
	// longer describes the storage. Current behaviour, pinned.
	if storage.Palette() == palette {
		t.Fatal("compact kept the old palette pointer on the renumbering path")
	}
	if palette.Len() != 4 {
		t.Fatalf("old palette.Len() = %d, want 4", palette.Len())
	}
}

func TestPalettedStorageEqualRejectsNil(t *testing.T) {
	storage := newPalettedStorage(make([]uint32, 128), newPalette(1, []uint32{ridSolid, ridGlass}))
	var nilStorage *PalettedStorage

	if nilStorage.Equal(storage) {
		t.Fatal("nil receiver compared equal")
	}
	if storage.Equal(nil) {
		t.Fatal("nil argument compared equal")
	}
	if nilStorage.Equal(nil) {
		t.Fatal("nil == nil compared equal")
	}
}

func TestPalettedStorageEqualIsFalseForIdenticalAirStorages(t *testing.T) {
	// SUSPICIOUS, pinned as current behaviour: two identical all-air storages are reported unequal, because
	// Equal bails on len(indices) == 0 and on palette.values[0] == 0. This drives the biome dedup in encoding.
	a, b := emptyStorage(ridAir), emptyStorage(ridAir)
	if a.Equal(b) {
		t.Fatal("two identical all-air storages compared equal")
	}

	// Same story once a uniform storage has been compacted: indices is nil, so Equal is false regardless of
	// the value it holds.
	c := newPalettedStorage(make([]uint32, 128), newPalette(1, []uint32{ridSolid}))
	d := newPalettedStorage(make([]uint32, 128), newPalette(1, []uint32{ridSolid}))
	if !c.Equal(d) {
		t.Fatal("identical uncompacted uniform storages compared unequal")
	}
	c.compact()
	d.compact()
	if c.Equal(d) {
		t.Fatal("two identical compacted uniform storages compared equal")
	}
}

func TestPalettedStorageEqualIsFalseWhenFirstPaletteValueIsZero(t *testing.T) {
	// SUSPICIOUS, pinned: byte-identical storages compare unequal purely because palette.values[0] == 0.
	a := newPalettedStorage(make([]uint32, 128), newPalette(1, []uint32{ridAir, ridSolid}))
	b := newPalettedStorage(make([]uint32, 128), newPalette(1, []uint32{ridAir, ridSolid}))
	a.setPaletteIndex(2, 2, 2, 1)
	b.setPaletteIndex(2, 2, 2, 1)

	if a.Equal(b) {
		t.Fatal("storages with palette.values[0] == 0 compared equal")
	}
}

func TestPalettedStorageEqualTrueAndFalseCases(t *testing.T) {
	build := func(values []uint32, index uint16) *PalettedStorage {
		s := newPalettedStorage(make([]uint32, 128), newPalette(1, values))
		s.setPaletteIndex(1, 1, 1, index)
		return s
	}
	a := build([]uint32{ridSolid, ridGlass}, 1)

	if !a.Equal(build([]uint32{ridSolid, ridGlass}, 1)) {
		t.Fatal("equal storages compared unequal")
	}
	if a.Equal(build([]uint32{ridSolid, ridGlass}, 0)) {
		t.Fatal("storages with different indices compared equal")
	}
	if a.Equal(build([]uint32{ridSolid, ridLeaves}, 1)) {
		t.Fatal("storages with different palettes compared equal")
	}
	// Differing widths differ in indices length, which bytes.Equal already rejects.
	wide := newPalettedStorage(make([]uint32, 256), newPalette(2, []uint32{ridSolid, ridGlass}))
	wide.setPaletteIndex(1, 1, 1, 1)
	if a.Equal(wide) {
		t.Fatal("storages of different widths compared equal")
	}
}

func TestPalettedStorageClone(t *testing.T) {
	storage := emptyStorage(ridSolid)
	storage.Set(0, 0, 0, ridGlass)
	storage.Set(1, 1, 1, ridLeaves)

	clone := storage.Clone()

	if clone.bitsPerIndex != storage.bitsPerIndex || len(clone.indices) != len(storage.indices) {
		t.Fatalf("clone bits, len = %d, %d, want %d, %d", clone.bitsPerIndex, len(clone.indices), storage.bitsPerIndex, len(storage.indices))
	}
	if &clone.indices[0] == &storage.indices[0] {
		t.Fatal("clone shares the indices backing array")
	}
	if clone.palette == storage.palette {
		t.Fatal("clone shares the palette")
	}
	for linear := 0; linear < 4096; linear++ {
		x, y, z := storageCoords(linear)
		if got, want := clone.At(x, y, z), storage.At(x, y, z); got != want {
			t.Fatalf("clone.At(%d, %d, %d) = %d, want %d", x, y, z, got, want)
		}
	}
	// Mutating the clone, including growing its palette, must leave the original alone.
	clone.Set(0, 0, 0, ridLamp)
	if got := storage.At(0, 0, 0); got != ridGlass {
		t.Fatalf("At(0, 0, 0) after clone write = %d, want %d", got, ridGlass)
	}
	if storage.palette.Len() != 3 {
		t.Fatalf("original palette.Len() = %d, want 3", storage.palette.Len())
	}
	if clone.palette.Len() != 4 {
		t.Fatalf("clone palette.Len() = %d, want 4", clone.palette.Len())
	}
}

func TestPalettedStorageCloneOfEmptyStorage(t *testing.T) {
	clone := emptyStorage(ridSolid).Clone()

	if clone.bitsPerIndex != 0 || len(clone.indices) != 0 {
		t.Fatalf("bits, len(indices) = %d, %d, want 0, 0", clone.bitsPerIndex, len(clone.indices))
	}
	if got := clone.At(3, 4, 5); got != ridSolid {
		t.Fatalf("At(3, 4, 5) = %d, want %d", got, ridSolid)
	}
}

func TestPalettedStoragePaletteIsLive(t *testing.T) {
	storage := emptyStorage(ridSolid)
	palette := storage.Palette()

	if palette != storage.palette {
		t.Fatal("Palette() did not return the internal palette")
	}
	storage.Set(0, 0, 0, ridGlass)
	// resize carries the same palette pointer over, so the handle taken earlier stays live across growth.
	if palette != storage.Palette() {
		t.Fatal("Palette() changed identity across a resize")
	}
	if palette.Len() != 2 || palette.Value(1) != ridGlass {
		t.Fatalf("palette Len, Value(1) = %d, %d, want 2, %d", palette.Len(), palette.Value(1), ridGlass)
	}
}

func TestPalettedStoragePaletteIndexMatchesPaletteValue(t *testing.T) {
	storage := emptyStorage(ridAir)
	values := []uint32{ridSolid, ridGlass, ridLeaves, ridLamp}
	for i, v := range values {
		storage.Set(byte(i), byte(i), byte(i), v)
	}
	for i, v := range values {
		x := byte(i)
		idx := storage.PaletteIndex(x, x, x)
		if want := uint16(i + 1); idx != want {
			t.Fatalf("PaletteIndex(%d, %d, %d) = %d, want %d", x, x, x, idx, want)
		}
		if got := storage.Palette().Value(idx); got != v {
			t.Fatalf("Palette().Value(%d) = %d, want %d", idx, got, v)
		}
	}
	// PaletteIndex masks its coordinates the same way At does.
	if got, want := storage.PaletteIndex(16, 16, 16), storage.PaletteIndex(0, 0, 0); got != want {
		t.Fatalf("PaletteIndex(16, 16, 16) = %d, want %d", got, want)
	}
}
