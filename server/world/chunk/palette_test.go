package chunk

import (
	"math"
	"testing"
)

// paletteNew builds a Palette through the package constructor so that the tests observe the same
// initial memo state (last = MaxUint32) production code gets.
func paletteNew(size paletteSize, values ...uint32) *Palette {
	return newPalette(size, values)
}

// paletteFill returns n distinct values starting at 1, so that 0 stays available as a value that is
// guaranteed absent from the palette.
func paletteFill(n int) []uint32 {
	v := make([]uint32, n)
	for i := range v {
		v[i] = uint32(i + 1)
	}
	return v
}

func TestPaletteSizeTables(t *testing.T) {
	wantSizes := []paletteSize{0, 1, 2, 3, 4, 5, 6, 8, 16}
	if len(sizes) != len(wantSizes) {
		t.Fatalf("len(sizes) = %d, want %d", len(sizes), len(wantSizes))
	}
	for i, want := range wantSizes {
		if sizes[i] != want {
			t.Errorf("sizes[%d] = %d, want %d", i, sizes[i], want)
		}
	}

	// offsets is indexed by the size itself, so it is sparse: every index that is not a canonical size
	// holds the implicit zero. increaseSize depends on that inverse relation holding exactly.
	if len(offsets) != 17 {
		t.Fatalf("len(offsets) = %d, want 17", len(offsets))
	}
	for i, size := range sizes {
		if offsets[size] != i {
			t.Errorf("offsets[%d] = %d, want %d", size, offsets[size], i)
		}
	}
	for _, gap := range []int{7, 9, 10, 11, 12, 13, 14, 15} {
		if offsets[gap] != 0 {
			t.Errorf("offsets[%d] = %d, want 0 (unset gap)", gap, offsets[gap])
		}
	}
}

func TestPaletteSizePadded(t *testing.T) {
	// Padded sizes are those that do not divide 32 evenly: 4096 indices do not fill a whole number of
	// uint32s, so one extra word is needed.
	for _, tc := range []struct {
		size paletteSize
		want bool
	}{
		{0, false}, {1, false}, {2, false}, {3, true}, {4, false},
		{5, true}, {6, true}, {8, false}, {16, false},
	} {
		if got := tc.size.padded(); got != tc.want {
			t.Errorf("paletteSize(%d).padded() = %v, want %v", tc.size, got, tc.want)
		}
	}
}

func TestPaletteSizeUint32s(t *testing.T) {
	for _, tc := range []struct {
		size paletteSize
		want int
	}{
		{0, 0},    // a single-value storage needs no index words at all
		{1, 128},  // 32 indices per word
		{2, 256},  // 16 per word
		{3, 410},  // 10 per word -> 409 words + 1 pad
		{4, 512},  // 8 per word
		{5, 683},  // 6 per word -> 682 + 1 pad
		{6, 820},  // 5 per word -> 819 + 1 pad
		{8, 1024}, // 4 per word
		{16, 2048},
	} {
		if got := tc.size.uint32s(); got != tc.want {
			t.Errorf("paletteSize(%d).uint32s() = %d, want %d", tc.size, got, tc.want)
		}
	}
}

func TestPaletteSizeFor(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want paletteSize
	}{
		{0, 0}, {1, 0}, // 1<<0 == 1 value fits in size 0
		{2, 1}, {3, 2}, {4, 2},
		{5, 3}, {8, 3},
		{9, 4}, {16, 4},
		{17, 5}, {32, 5},
		{33, 6}, {64, 6},
		{65, 8}, {256, 8}, // size 7 is not a canonical size, so 8 is the next step up from 6
		{257, 16}, {65536, 16},
		// Current behaviour: n above the largest representable count falls out of the loop and yields 0,
		// the smallest size, rather than an error. Pinned as-is.
		{65537, 0},
		// Negative n satisfies n <= 1 on the first iteration and yields 0.
		{-1, 0},
	} {
		if got := paletteSizeFor(tc.n); got != tc.want {
			t.Errorf("paletteSizeFor(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

func TestPaletteNewAliasesValues(t *testing.T) {
	values := []uint32{ridAir, ridSolid, ridGlass}
	p := newPalette(2, values)

	// newPalette stores the caller's slice directly: writes through either side are visible to the other.
	values[1] = ridLamp
	if got := p.Value(1); got != ridLamp {
		t.Errorf("Value(1) after caller wrote through the source slice = %d, want %d", got, ridLamp)
	}
	p.Replace(func(v uint32) uint32 { return v + 100 })
	if values[0] != ridAir+100 {
		t.Errorf("source slice [0] after Replace = %d, want %d", values[0], ridAir+100)
	}
}

func TestPaletteLenAndValue(t *testing.T) {
	if got := paletteNew(0).Len(); got != 0 {
		t.Errorf("Len() of an empty palette = %d, want 0", got)
	}
	p := paletteNew(2, ridAir, ridSolid, ridGlass)
	if got := p.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
	for i, want := range []uint32{ridAir, ridSolid, ridGlass} {
		if got := p.Value(uint16(i)); got != want {
			t.Errorf("Value(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestPaletteAddGrowthThresholds(t *testing.T) {
	// needsResize compares len(values) > 1<<size AFTER the append, so the palette grows on the value that
	// pushes the count one past the current capacity. Keys are the 1-based ordinal of that value.
	growth := map[int]paletteSize{2: 1, 3: 2, 5: 3, 9: 4, 17: 5, 33: 6, 65: 8, 257: 16}

	p := paletteNew(0)
	size := paletteSize(0)
	for n := 1; n <= 300; n++ {
		index, resize := p.Add(uint32(n))
		if want := int16(n - 1); index != want {
			t.Fatalf("Add #%d returned index %d, want %d", n, index, want)
		}
		want, grew := growth[n]
		if resize != grew {
			t.Fatalf("Add #%d returned resize %v, want %v", n, resize, grew)
		}
		if grew {
			size = want
		}
		if p.size != size {
			t.Fatalf("size after Add #%d = %d, want %d", n, p.size, size)
		}
		if p.Len() != n {
			t.Fatalf("Len() after Add #%d = %d, want %d", n, p.Len(), n)
		}
	}
	if p.size != 16 {
		t.Errorf("final size = %d, want 16", p.size)
	}
}

func TestPaletteAddDoesNotDeduplicate(t *testing.T) {
	// Add is documented as not checking for an existing entry: the same value added twice occupies two
	// indices and counts twice towards the resize threshold.
	p := paletteNew(0)
	if index, resize := p.Add(ridSolid); index != 0 || resize {
		t.Fatalf("first Add = %d, %v, want 0, false", index, resize)
	}
	if index, resize := p.Add(ridSolid); index != 1 || !resize {
		t.Fatalf("duplicate Add = %d, %v, want 1, true", index, resize)
	}
	if p.Len() != 2 {
		t.Errorf("Len() = %d, want 2", p.Len())
	}
	// Index still reports the first occurrence, since indexSlow scans forwards.
	if got := p.Index(ridSolid); got != 0 {
		t.Errorf("Index(%d) = %d, want 0", ridSolid, got)
	}
}

func TestPaletteAddIndexOverflowsInt16(t *testing.T) {
	// Add derives the returned index from int16(len(values)). At 32768 existing values that conversion
	// wraps to the negative range while the palette itself stays valid. Current behaviour, pinned as-is:
	// a caller storing this index would address the wrong entry.
	p := newPalette(16, paletteFill(32768))
	index, resize := p.Add(ridSolid)
	if index != -32768 {
		t.Errorf("Add at length 32768 returned index %d, want -32768", index)
	}
	if resize {
		t.Errorf("Add at length 32768 returned resize true, want false (32769 <= 1<<16)")
	}
	if p.Len() != 32769 {
		t.Errorf("Len() = %d, want 32769", p.Len())
	}
}

func TestPaletteAddPanicsPastLargestSize(t *testing.T) {
	// Growing past size 16 indexes sizes[9], which does not exist. Current behaviour is a runtime panic
	// rather than a returned error. Pinned as-is.
	p := newPalette(16, paletteFill(65536))
	defer func() {
		if recover() == nil {
			t.Errorf("Add beyond 65536 values did not panic")
		}
	}()
	p.Add(ridSolid)
}

func TestPaletteIndex(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []uint32
		lookup uint32
		want   int16
	}{
		{"empty palette", nil, ridSolid, -1},
		{"single value present", []uint32{ridAir}, ridAir, 0},
		{"single value absent", []uint32{ridAir}, ridSolid, -1},
		{"first of several", []uint32{ridAir, ridSolid, ridGlass}, ridAir, 0},
		{"last of several", []uint32{ridAir, ridSolid, ridGlass}, ridGlass, 2},
		{"absent among several", []uint32{ridAir, ridSolid, ridGlass}, ridLamp, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := newPalette(2, tc.values).Index(tc.lookup); got != tc.want {
				t.Errorf("Index(%d) = %d, want %d", tc.lookup, got, tc.want)
			}
		})
	}
}

func TestPaletteIndexMemoisesLastHit(t *testing.T) {
	p := paletteNew(2, ridAir, ridSolid, ridGlass)
	if got := p.Index(ridGlass); got != 2 {
		t.Fatalf("Index(%d) = %d, want 2", ridGlass, got)
	}
	// The memo is keyed on the value alone and is never invalidated by writes to values. Overwriting the
	// entry it points at leaves the stale answer in place: current behaviour, pinned as-is.
	p.values[2] = ridLamp
	if got := p.Index(ridGlass); got != 2 {
		t.Errorf("Index(%d) after its entry was overwritten = %d, want 2 (memoised)", ridGlass, got)
	}
	// A different value takes the slow path and is found correctly.
	if got := p.Index(ridLamp); got != 2 {
		t.Errorf("Index(%d) = %d, want 2", ridLamp, got)
	}
	// A miss on the slow path does not disturb the memo.
	if got := p.Index(ridChest); got != -1 {
		t.Errorf("Index(%d) = %d, want -1", ridChest, got)
	}
	if p.last != ridLamp || p.lastIndex != 2 {
		t.Errorf("memo after a miss = (%d, %d), want (%d, 2)", p.last, p.lastIndex, ridLamp)
	}
}

func TestPaletteIndexMaxUint32Quirk(t *testing.T) {
	// newPalette seeds last with MaxUint32 as an "impossible" sentinel, but leaves lastIndex at zero. The
	// fast path therefore answers 0 for MaxUint32 without scanning, even when the palette does not hold
	// it and even when the palette is empty. Current behaviour, pinned as-is.
	for _, tc := range []struct {
		name   string
		values []uint32
	}{
		{"empty", nil},
		{"populated without MaxUint32", []uint32{ridAir, ridSolid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPalette(2, tc.values)
			if got := p.Index(math.MaxUint32); got != 0 {
				t.Errorf("Index(MaxUint32) = %d, want 0 (sentinel collision)", got)
			}
		})
	}
}

func TestPaletteReplace(t *testing.T) {
	p := paletteNew(2, ridAir, ridSolid, ridGlass)
	p.Replace(func(v uint32) uint32 { return v * 2 })
	for i, want := range []uint32{ridAir * 2, ridSolid * 2, ridGlass * 2} {
		if got := p.Value(uint16(i)); got != want {
			t.Errorf("Value(%d) after Replace = %d, want %d", i, got, want)
		}
	}
	if p.Len() != 3 {
		t.Errorf("Len() after Replace = %d, want 3", p.Len())
	}
	if got := p.Index(ridGlass * 2); got != 2 {
		t.Errorf("Index of a replaced value = %d, want 2", got)
	}
}

func TestPaletteReplaceLeavesLastIndexStale(t *testing.T) {
	p := paletteNew(2, ridAir, ridSolid, ridGlass)
	if got := p.Index(ridGlass); got != 2 {
		t.Fatalf("Index(%d) = %d, want 2", ridGlass, got)
	}
	p.Replace(func(v uint32) uint32 { return v + 100 })

	// Replace resets last to MaxUint32 but does not touch lastIndex, so the MaxUint32 sentinel now
	// resolves to whatever index the previous lookup happened to leave behind. Current behaviour,
	// pinned as-is.
	if p.last != math.MaxUint32 {
		t.Errorf("last after Replace = %d, want MaxUint32", p.last)
	}
	if p.lastIndex != 2 {
		t.Errorf("lastIndex after Replace = %d, want 2 (left stale)", p.lastIndex)
	}
	if got := p.Index(math.MaxUint32); got != 2 {
		t.Errorf("Index(MaxUint32) after Replace = %d, want 2 (stale memo)", got)
	}
	// The pre-replacement value is genuinely gone.
	if got := p.Index(ridGlass); got != -1 {
		t.Errorf("Index of a pre-Replace value = %d, want -1", got)
	}
}

func TestPaletteClone(t *testing.T) {
	p := paletteNew(2, ridAir, ridSolid, ridGlass)
	if got := p.Index(ridSolid); got != 1 {
		t.Fatalf("Index(%d) = %d, want 1", ridSolid, got)
	}
	c := p.Clone()

	if c.Len() != p.Len() || c.size != p.size {
		t.Fatalf("Clone() = len %d size %d, want len %d size %d", c.Len(), c.size, p.Len(), p.size)
	}
	for i := range uint16(3) {
		if c.Value(i) != p.Value(i) {
			t.Errorf("clone Value(%d) = %d, want %d", i, c.Value(i), p.Value(i))
		}
	}
	// The memo travels with the clone: the clone answers from the fast path without ever scanning, which
	// is observable because overwriting the entry does not change the answer.
	if c.last != ridSolid || c.lastIndex != 1 {
		t.Errorf("clone memo = (%d, %d), want (%d, 1)", c.last, c.lastIndex, ridSolid)
	}
	c.values[1] = ridLamp
	if got := c.Index(ridSolid); got != 1 {
		t.Errorf("clone Index(%d) = %d, want 1 (memo carried over)", ridSolid, got)
	}
	// values is deep-copied, so that write did not reach the original.
	if got := p.Value(1); got != ridSolid {
		t.Errorf("original Value(1) after clone was mutated = %d, want %d", got, ridSolid)
	}
	// Nor does appending to the clone.
	c.Add(ridChest)
	if p.Len() != 3 {
		t.Errorf("original Len() after clone Add = %d, want 3", p.Len())
	}
}

func TestPaletteCloneEmpty(t *testing.T) {
	c := paletteNew(0).Clone()
	if c.Len() != 0 {
		t.Errorf("Len() of a cloned empty palette = %d, want 0", c.Len())
	}
	if index, resize := c.Add(ridSolid); index != 0 || resize {
		t.Errorf("Add on a cloned empty palette = %d, %v, want 0, false", index, resize)
	}
}

func TestPaletteIncreaseSize(t *testing.T) {
	for _, tc := range []struct {
		from, want paletteSize
	}{
		{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}, {6, 8}, {8, 16},
		// 7 is not a canonical size, so offsets[7] is the array's implicit zero and the step lands on
		// sizes[1] — a size decrease. Only reachable from a decoded palette; pinned as-is.
		{7, 1},
	} {
		p := paletteNew(tc.from)
		p.increaseSize()
		if p.size != tc.want {
			t.Errorf("increaseSize() from %d gave %d, want %d", tc.from, p.size, tc.want)
		}
	}
}
