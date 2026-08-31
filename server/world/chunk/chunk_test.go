package chunk

import (
	"runtime"
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// chunkPanicOf runs f and returns the value it panicked with, or nil if it returned normally. It exists so that
// the tests below can assert on the panic value instead of merely asserting that a panic happened.
func chunkPanicOf(f func()) (recovered any) {
	defer func() { recovered = recover() }()
	f()
	return nil
}

// chunkAssertIndexPanic fails unless v is a runtime error about a slice index. Every out-of-range access pinned
// below reaches the runtime through an unchecked slice index, never through an explicit panic in chunk code.
func chunkAssertIndexPanic(t *testing.T, v any) {
	t.Helper()
	if v == nil {
		t.Fatalf("expected a panic, got none")
	}
	err, ok := v.(runtime.Error)
	if !ok {
		t.Fatalf("panic value %v (%T) is not a runtime.Error", v, v)
	}
	if !strings.Contains(err.Error(), "index out of range") {
		t.Fatalf("panic %q does not mention an out of range index", err.Error())
	}
}

func TestChunkNewAllocatesEverySubChunkEagerly(t *testing.T) {
	// n is (Height()>>4)+1: Height is Max-Min, so the +1 accounts for the sub chunk holding the minimum Y
	// itself. The overworld range spans 383 blocks, which is 23 full sub chunks plus one.
	cases := []struct {
		name string
		r    cube.Range
		want int
	}{
		{name: "overworld", r: cube.Range{-64, 319}, want: 24},
		{name: "nether", r: cube.Range{0, 127}, want: 8},
		{name: "end", r: cube.Range{0, 255}, want: 16},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := New(newTestRegistry(), c.r)
			if got := len(ch.Sub()); got != c.want {
				t.Fatalf("len(Sub()) = %d, want %d", got, c.want)
			}
			if got := len(ch.biomes); got != c.want {
				t.Fatalf("len(biomes) = %d, want %d", got, c.want)
			}
			for i, sub := range ch.Sub() {
				// New allocates every sub chunk up front, so no index is ever nil, and each one starts with
				// zero storages, which is what makes it Empty.
				if sub == nil {
					t.Fatalf("Sub()[%d] is nil; New allocates eagerly", i)
				}
				if got := len(sub.Layers()); got != 0 {
					t.Fatalf("Sub()[%d]: len(Layers()) = %d, want 0", i, got)
				}
				if !sub.Empty() {
					t.Fatalf("Sub()[%d]: Empty() = false, want true", i)
				}
			}
			for i, b := range ch.biomes {
				// Biome storages are emptyStorage(0): a single palette entry, zero bits per index.
				if b == nil {
					t.Fatalf("biomes[%d] is nil", i)
				}
				if got := b.Palette().Len(); got != 1 {
					t.Fatalf("biomes[%d]: palette length = %d, want 1", i, got)
				}
				if got := b.Palette().Value(0); got != 0 {
					t.Fatalf("biomes[%d]: palette value = %d, want 0", i, got)
				}
			}
			if len(ch.heightMap) != 256 {
				t.Fatalf("len(heightMap) = %d, want 256", len(ch.heightMap))
			}
			if !ch.recalculateHeightMap {
				t.Fatalf("recalculateHeightMap = false, want true on a fresh chunk")
			}
			if ch.Range() != c.r {
				t.Fatalf("Range() = %v, want %v", ch.Range(), c.r)
			}
		})
	}
}

func TestChunkNewIsAllAirAndBiomeZero(t *testing.T) {
	c := newTestChunk()
	for y := int16(overworldRange[0]); y <= int16(overworldRange[1]); y++ {
		for _, x := range []uint8{0, 7, 15} {
			for _, z := range []uint8{0, 8, 15} {
				if got := c.Block(x, y, z, 0); got != ridAir {
					t.Fatalf("Block(%d, %d, %d, 0) = %d, want %d", x, y, z, got, ridAir)
				}
				if got := c.Block(x, y, z, 1); got != ridAir {
					t.Fatalf("Block(%d, %d, %d, 1) = %d, want %d", x, y, z, got, ridAir)
				}
				if got := c.Biome(x, y, z); got != 0 {
					t.Fatalf("Biome(%d, %d, %d) = %d, want 0", x, y, z, got)
				}
			}
		}
	}
}

func TestChunkSubIndexAndSubY(t *testing.T) {
	c := newTestChunk()
	// SubIndex is an arithmetic shift of the offset from the minimum Y, so it floors: every Y in a 16 block
	// band maps to the same index, and SubY maps that index back to the bottom of the band.
	cases := []struct {
		y         int16
		wantIndex int16
		wantSubY  int16
	}{
		{y: -64, wantIndex: 0, wantSubY: -64},
		{y: -49, wantIndex: 0, wantSubY: -64},
		{y: -48, wantIndex: 1, wantSubY: -48},
		{y: -1, wantIndex: 3, wantSubY: -16},
		{y: 0, wantIndex: 4, wantSubY: 0},
		{y: 5, wantIndex: 4, wantSubY: 0},
		{y: 15, wantIndex: 4, wantSubY: 0},
		{y: 16, wantIndex: 5, wantSubY: 16},
		{y: 70, wantIndex: 8, wantSubY: 64},
		{y: 319, wantIndex: 23, wantSubY: 304},
	}
	for _, tc := range cases {
		if got := c.SubIndex(tc.y); got != tc.wantIndex {
			t.Errorf("SubIndex(%d) = %d, want %d", tc.y, got, tc.wantIndex)
		}
		if got := c.SubY(tc.wantIndex); got != tc.wantSubY {
			t.Errorf("SubY(%d) = %d, want %d", tc.wantIndex, got, tc.wantSubY)
		}
		// SubY(SubIndex(y)) is y rounded down to the band boundary, so the difference is always 0-15.
		if got, want := c.SubY(c.SubIndex(tc.y)), tc.wantSubY; got != want {
			t.Errorf("SubY(SubIndex(%d)) = %d, want %d", tc.y, got, want)
		}
		if d := tc.y - tc.wantSubY; d < 0 || d > 15 {
			t.Errorf("y %d - SubY = %d, want 0-15", tc.y, d)
		}
	}
	// SubIndex(SubY(i)) is the identity on valid indices, since SubY always lands on a band boundary.
	for i := int16(0); i < int16(len(c.Sub())); i++ {
		if got := c.SubIndex(c.SubY(i)); got != i {
			t.Errorf("SubIndex(SubY(%d)) = %d, want %d", i, got, i)
		}
	}
}

func TestChunkRangeExtremesAreUsable(t *testing.T) {
	// -64 and 319 are the inclusive bounds of the overworld range. Both must round trip: -64 lands at local
	// Y 0 of sub chunk 0 and 319 at local Y 15 of sub chunk 23.
	cases := []struct {
		name string
		y    int16
	}{
		{name: "minimum", y: -64},
		{name: "maximum", y: 319},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChunk()
			c.SetBlock(3, tc.y, 9, 0, ridSolid)
			if got := c.Block(3, tc.y, 9, 0); got != ridSolid {
				t.Fatalf("Block(3, %d, 9, 0) = %d, want %d", tc.y, got, ridSolid)
			}
			// The neighbouring column inside the same sub chunk must be untouched.
			if got := c.Block(4, tc.y, 9, 0); got != ridAir {
				t.Fatalf("Block(4, %d, 9, 0) = %d, want %d", tc.y, got, ridAir)
			}
			c.SetBiome(3, tc.y, 9, 42)
			if got := c.Biome(3, tc.y, 9); got != 42 {
				t.Fatalf("Biome(3, %d, 9) = %d, want 42", tc.y, got)
			}
			if c.SubChunk(tc.y) == nil {
				t.Fatalf("SubChunk(%d) = nil", tc.y)
			}
		})
	}
}

func TestChunkOutOfRangePanics(t *testing.T) {
	// Pinning current behaviour: none of these methods bounds check y against the chunk's range. y below the
	// minimum makes SubIndex negative and y above the maximum makes it len(sub), so the slice index panics.
	// This is a characterization of what the code does today, not an endorsement of panicking.
	c := newTestChunk()
	ys := []struct {
		name string
		y    int16
	}{
		{name: "below minimum", y: -65},
		{name: "above maximum", y: 320},
	}
	ops := []struct {
		name string
		fn   func(y int16)
	}{
		{name: "Block", fn: func(y int16) { c.Block(0, y, 0, 0) }},
		{name: "SetBlock", fn: func(y int16) { c.SetBlock(0, y, 0, 0, ridSolid) }},
		{name: "Biome", fn: func(y int16) { c.Biome(0, y, 0) }},
		{name: "SetBiome", fn: func(y int16) { c.SetBiome(0, y, 0, 1) }},
		{name: "SubChunk", fn: func(y int16) { c.SubChunk(y) }},
	}
	for _, yc := range ys {
		for _, op := range ops {
			t.Run(op.name+" "+yc.name, func(t *testing.T) {
				chunkAssertIndexPanic(t, chunkPanicOf(func() { op.fn(yc.y) }))
			})
		}
	}
}

func TestSubChunkLayerGrowsStorages(t *testing.T) {
	// Layer is a getter with a side effect: it appends empty storages until the requested layer exists, so
	// every intermediate layer is created too.
	sub := NewSubChunk(ridAir)
	if got := len(sub.Layers()); got != 0 {
		t.Fatalf("len(Layers()) = %d, want 0 before any Layer call", got)
	}
	steps := []struct {
		layer     uint8
		wantAfter int
	}{
		{layer: 0, wantAfter: 1},
		{layer: 0, wantAfter: 1}, // Already present, so no growth.
		{layer: 3, wantAfter: 4}, // Layers 1, 2 and 3 are all appended.
		{layer: 1, wantAfter: 4}, // Below the highest layer, so no growth.
	}
	for _, s := range steps {
		storage := sub.Layer(s.layer)
		if storage == nil {
			t.Fatalf("Layer(%d) = nil", s.layer)
		}
		if got := len(sub.Layers()); got != s.wantAfter {
			t.Fatalf("after Layer(%d): len(Layers()) = %d, want %d", s.layer, got, s.wantAfter)
		}
		if storage != sub.Layers()[s.layer] {
			t.Fatalf("Layer(%d) did not return Layers()[%d]", s.layer, s.layer)
		}
		// Every freshly appended storage is emptyStorage(air): one palette entry holding air.
		if got := storage.At(0, 0, 0); got != ridAir {
			t.Fatalf("Layer(%d).At(0,0,0) = %d, want %d", s.layer, got, ridAir)
		}
	}
	// Growing layers leaves the sub chunk non-empty even though every block in it is air, because Empty
	// demands exactly one storage.
	if sub.Empty() {
		t.Fatalf("Empty() = true after growing to 4 all-air layers, want false")
	}
}

func TestSubChunkBlockMissingLayerReturnsAir(t *testing.T) {
	sub := NewSubChunk(ridAir)
	// Reading a layer that does not exist must not create it; it reports air instead.
	if got := sub.Block(1, 2, 3, 7); got != ridAir {
		t.Fatalf("Block(1,2,3,7) = %d, want %d", got, ridAir)
	}
	if got := len(sub.Layers()); got != 0 {
		t.Fatalf("len(Layers()) = %d after reading a missing layer, want 0", got)
	}
	sub.SetBlock(1, 2, 3, 0, ridSolid)
	if got := sub.Block(1, 2, 3, 0); got != ridSolid {
		t.Fatalf("Block(1,2,3,0) = %d, want %d", got, ridSolid)
	}
	if got := sub.Block(1, 2, 3, 1); got != ridAir {
		t.Fatalf("Block(1,2,3,1) = %d, want %d", got, ridAir)
	}
}

func TestSubChunkEmpty(t *testing.T) {
	// Empty only ever inspects storages[0], and only when there is exactly one storage. Anything else — more
	// layers, or a palette that still remembers a removed block — reports non-empty.
	cases := []struct {
		name string
		set  func(sub *SubChunk)
		want bool
	}{
		{name: "fresh", set: func(*SubChunk) {}, want: true},
		{name: "one all air layer", set: func(sub *SubChunk) { sub.Layer(0) }, want: true},
		{name: "one layer with a block", set: func(sub *SubChunk) { sub.SetBlock(0, 0, 0, 0, ridSolid) }, want: false},
		{
			// The palette keeps the solid entry after it is overwritten with air, so len(values) is 2 and
			// Empty reports false even though every block is air. Pinned as current behaviour.
			name: "one layer emptied again",
			set: func(sub *SubChunk) {
				sub.SetBlock(0, 0, 0, 0, ridSolid)
				sub.SetBlock(0, 0, 0, 0, ridAir)
			},
			want: false,
		},
		{name: "two all air layers", set: func(sub *SubChunk) { sub.Layer(1) }, want: false},
		{name: "block only on layer 1", set: func(sub *SubChunk) { sub.SetBlock(0, 0, 0, 1, ridSolid) }, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := NewSubChunk(ridAir)
			tc.set(sub)
			if got := sub.Empty(); got != tc.want {
				t.Fatalf("Empty() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChunkBlockOnNonZeroLayer(t *testing.T) {
	// Chunk.Block short circuits to air when the sub chunk is Empty, and Empty looks at storages[0] only.
	// That shortcut cannot hide content on a higher layer: writing layer 1 forces Layer to append layer 0 as
	// well, so len(storages) is 2 and Empty is already false.
	c := newTestChunk()
	c.SetBlock(1, 10, 2, 1, ridSolid)
	sub := c.SubChunk(10)
	if got := len(sub.Layers()); got != 2 {
		t.Fatalf("len(Layers()) = %d, want 2", got)
	}
	if sub.Empty() {
		t.Fatalf("Empty() = true, want false with content on layer 1")
	}
	if got := c.Block(1, 10, 2, 0); got != ridAir {
		t.Fatalf("Block(layer 0) = %d, want %d", got, ridAir)
	}
	if got := c.Block(1, 10, 2, 1); got != ridSolid {
		t.Fatalf("Block(layer 1) = %d, want %d", got, ridSolid)
	}
	// A layer that was never created still reads as air rather than panicking.
	if got := c.Block(1, 10, 2, 2); got != ridAir {
		t.Fatalf("Block(layer 2) = %d, want %d", got, ridAir)
	}
	if got := len(sub.Layers()); got != 2 {
		t.Fatalf("Chunk.Block created a layer: len(Layers()) = %d, want 2", got)
	}
}

func TestChunkSetBlockHeightMapDirtyFlag(t *testing.T) {
	t.Run("unchanged value still dirties", func(t *testing.T) {
		c := newTestChunk()
		c.SetBlock(1, 10, 2, 0, ridSolid)
		c.HeightMap() // Clears the flag.
		if c.recalculateHeightMap {
			t.Fatalf("recalculateHeightMap = true directly after HeightMap()")
		}
		// SetBlock does not compare against the current value, so writing the same runtime ID dirties.
		c.SetBlock(1, 10, 2, 0, ridSolid)
		if !c.recalculateHeightMap {
			t.Fatalf("recalculateHeightMap = false after re-setting the same block, want true")
		}
		// Observable through the height map itself: a value poked into the returned slice is thrown away by
		// the recomputation the dirty flag triggers.
		c.HeightMap().Set(0, 0, 1234)
		c.SetBlock(1, 10, 2, 0, ridSolid)
		if got := c.HeightMap().At(0, 0); got == 1234 {
			t.Fatalf("HeightMap().At(0,0) = 1234, want a recomputed value")
		}
	})
	t.Run("air on an absent layer is a no-op", func(t *testing.T) {
		c := newTestChunk()
		c.SetBlock(1, 10, 2, 0, ridSolid)
		c.HeightMap()
		sub := c.SubChunk(10)
		before := len(sub.Layers())
		// Layer 4 does not exist and the value is air, so SetBlock returns early: no storage is appended and
		// the height map is not marked dirty.
		c.SetBlock(1, 10, 2, 4, ridAir)
		if got := len(sub.Layers()); got != before {
			t.Fatalf("len(Layers()) = %d, want %d; SetBlock created a layer for air", got, before)
		}
		if c.recalculateHeightMap {
			t.Fatalf("recalculateHeightMap = true after a no-op SetBlock, want false")
		}
		c.HeightMap().Set(0, 0, 1234)
		c.SetBlock(1, 10, 2, 4, ridAir)
		if got := c.HeightMap().At(0, 0); got != 1234 {
			t.Fatalf("HeightMap().At(0,0) = %d, want 1234; the chunk was recomputed", got)
		}
	})
	t.Run("air on layer 0 of a fresh chunk is a no-op", func(t *testing.T) {
		c := newTestChunk()
		c.HeightMap()
		// A fresh sub chunk has zero storages, so even layer 0 counts as absent.
		c.SetBlock(1, 10, 2, 0, ridAir)
		if got := len(c.SubChunk(10).Layers()); got != 0 {
			t.Fatalf("len(Layers()) = %d, want 0", got)
		}
		if c.recalculateHeightMap {
			t.Fatalf("recalculateHeightMap = true, want false")
		}
	})
	t.Run("air on an existing layer dirties", func(t *testing.T) {
		c := newTestChunk()
		c.SetBlock(1, 10, 2, 0, ridSolid)
		c.HeightMap()
		// Layer 0 exists now, so the early return does not apply even though the value is air.
		c.SetBlock(1, 10, 2, 0, ridAir)
		if !c.recalculateHeightMap {
			t.Fatalf("recalculateHeightMap = false, want true")
		}
		if got := c.Block(1, 10, 2, 0); got != ridAir {
			t.Fatalf("Block = %d, want %d", got, ridAir)
		}
	})
}

func TestChunkBiomeRoundTrip(t *testing.T) {
	c := newTestChunk()
	// Biomes are stored per sub chunk with the same 3D indexing as blocks, so two Y values in different sub
	// chunks of the same column hold independent values.
	cases := []struct {
		x, z  uint8
		y     int16
		biome uint32
	}{
		{x: 0, y: -64, z: 0, biome: 1},
		{x: 15, y: -49, z: 15, biome: 2},
		{x: 15, y: -48, z: 15, biome: 3},
		{x: 7, y: 0, z: 8, biome: 100},
		{x: 7, y: 15, z: 8, biome: 101},
		{x: 7, y: 16, z: 8, biome: 102},
		{x: 3, y: 319, z: 4, biome: 65535},
	}
	for _, tc := range cases {
		c.SetBiome(tc.x, tc.y, tc.z, tc.biome)
	}
	for _, tc := range cases {
		if got := c.Biome(tc.x, tc.y, tc.z); got != tc.biome {
			t.Errorf("Biome(%d, %d, %d) = %d, want %d", tc.x, tc.y, tc.z, got, tc.biome)
		}
	}
	// A column never written to keeps the default of 0.
	if got := c.Biome(1, 0, 1); got != 0 {
		t.Errorf("Biome(1, 0, 1) = %d, want 0", got)
	}
	// Biomes never touch blocks.
	if got := c.Block(7, 0, 8, 0); got != ridAir {
		t.Errorf("Block(7, 0, 8, 0) = %d, want %d", got, ridAir)
	}
}

func TestChunkSetBiomeDoesNotDirtyHeightMap(t *testing.T) {
	c := newTestChunk()
	c.SetBlock(1, 10, 2, 0, ridSolid)
	c.HeightMap()
	c.HeightMap().Set(0, 0, 1234)
	c.SetBiome(1, 10, 2, 5)
	if c.recalculateHeightMap {
		t.Fatalf("recalculateHeightMap = true after SetBiome, want false")
	}
	// Observable: no recomputation happened, so the poked value survives.
	if got := c.HeightMap().At(0, 0); got != 1234 {
		t.Fatalf("HeightMap().At(0,0) = %d, want 1234", got)
	}
}

func TestChunkCloneIsDeep(t *testing.T) {
	c := newTestChunk()
	c.SetBlock(1, 10, 2, 0, ridSolid)
	c.SetBlock(1, 10, 2, 1, ridWater)
	c.SetBlock(4, 200, 5, 0, ridChest)
	c.SetBiome(1, 10, 2, 7)
	c.HeightMap() // Make the source clean so the clone copies a computed height map.

	clone := c.Clone()
	if got := len(clone.Sub()); got != len(c.Sub()) {
		t.Fatalf("len(Sub()) = %d, want %d", got, len(c.Sub()))
	}
	for i := range c.Sub() {
		if c.Sub()[i] == clone.Sub()[i] {
			t.Fatalf("Sub()[%d] is shared between chunk and clone", i)
		}
	}
	for i := range c.biomes {
		if c.biomes[i] == clone.biomes[i] {
			t.Fatalf("biomes[%d] is shared between chunk and clone", i)
		}
	}
	// The clone starts as a value-wise copy.
	if got := clone.Block(1, 10, 2, 0); got != ridSolid {
		t.Fatalf("clone Block(layer 0) = %d, want %d", got, ridSolid)
	}
	if got := clone.Block(1, 10, 2, 1); got != ridWater {
		t.Fatalf("clone Block(layer 1) = %d, want %d", got, ridWater)
	}
	if got := clone.Biome(1, 10, 2); got != 7 {
		t.Fatalf("clone Biome = %d, want 7", got)
	}

	clone.SetBlock(1, 10, 2, 0, ridGlass)
	clone.SetBlock(4, 200, 5, 0, ridAir)
	clone.SetBiome(1, 10, 2, 9)
	clone.HeightMap()
	clone.HeightMap().Set(0, 0, 4321)

	// Mutating the clone leaves the original untouched.
	if got := c.Block(1, 10, 2, 0); got != ridSolid {
		t.Fatalf("original Block(layer 0) = %d, want %d", got, ridSolid)
	}
	if got := c.Block(4, 200, 5, 0); got != ridChest {
		t.Fatalf("original Block at y=200 = %d, want %d", got, ridChest)
	}
	if got := c.Biome(1, 10, 2); got != 7 {
		t.Fatalf("original Biome = %d, want 7", got)
	}
	if got := c.HeightMap().At(0, 0); got == 4321 {
		t.Fatalf("original height map aliases the clone's")
	}
	// And the other direction.
	if got := clone.Block(1, 10, 2, 0); got != ridGlass {
		t.Fatalf("clone Block(layer 0) = %d, want %d", got, ridGlass)
	}
	if c.Range() != clone.Range() || c.air != clone.air {
		t.Fatalf("clone did not copy range/air: %v %v", clone.Range(), clone.air)
	}
}

func TestChunkEquals(t *testing.T) {
	t.Run("fresh chunk equals its clone", func(t *testing.T) {
		c := newTestChunk()
		if !c.Equals(c.Clone()) {
			t.Fatalf("Equals = false for a clone of an untouched chunk")
		}
	})
	t.Run("different range is unequal", func(t *testing.T) {
		c := newTestChunk()
		other := New(newTestRegistry(), cube.Range{0, 127})
		if c.Equals(other) {
			t.Fatalf("Equals = true for chunks with different ranges")
		}
	})
	t.Run("clean chunks with different height maps are unequal", func(t *testing.T) {
		a, b := newTestChunk(), newTestChunk()
		b.SetBlock(0, 0, 0, 0, ridSolid)
		a.HeightMap()
		b.HeightMap()
		// Both are clean, so Equals compares the height maps first and rejects immediately.
		if a.Equals(b) {
			t.Fatalf("Equals = true for chunks with different height maps")
		}
	})
	t.Run("clean chunks with equal height maps", func(t *testing.T) {
		a, b := newTestChunk(), newTestChunk()
		a.HeightMap()
		b.HeightMap()
		if !a.Equals(b) {
			t.Fatalf("Equals = false for two untouched, clean chunks")
		}
	})
	t.Run("biomes are ignored", func(t *testing.T) {
		// PINNED BUG: Equals never looks at chunk.biomes, so chunks that differ only in biome data compare
		// as equal.
		a, b := newTestChunk(), newTestChunk()
		b.SetBiome(0, 0, 0, 42)
		if !a.Equals(b) {
			t.Fatalf("Equals = false; expected biomes to be ignored")
		}
		if got := b.Biome(0, 0, 0); got != 42 {
			t.Fatalf("Biome = %d, want 42", got)
		}
	})
	t.Run("identical chunks with blocks are reported unequal", func(t *testing.T) {
		// PINNED BUG: Equals delegates to PalettedStorage.Equal, which bails out with false whenever a
		// palette's first value is 0. Air's runtime ID is 0 and every storage starts with air at index 0, so
		// a chunk with any block set never equals its own clone.
		c := newTestChunk()
		c.SetBlock(1, 10, 2, 0, ridSolid)
		if c.Equals(c.Clone()) {
			t.Fatalf("Equals = true; the air-runtime-ID-0 short circuit in PalettedStorage.Equal no longer applies")
		}
		// It is not even reflexive.
		if c.Equals(c) {
			t.Fatalf("Equals = true for a chunk compared with itself")
		}
	})
}

func TestChunkHighestFilledSubChunk(t *testing.T) {
	// The result is the topmost non-empty index plus one, i.e. the number of sub chunks that must be sent.
	cases := []struct {
		name string
		set  func(c *Chunk)
		want uint16
	}{
		{name: "fresh", set: func(*Chunk) {}, want: 0},
		{name: "air on a fresh layer is a no-op", set: func(c *Chunk) { c.SetBlock(0, 0, 0, 0, ridAir) }, want: 0},
		{name: "bottom sub chunk", set: func(c *Chunk) { c.SetBlock(0, -64, 0, 0, ridSolid) }, want: 1},
		{name: "y=0 is sub chunk 4", set: func(c *Chunk) { c.SetBlock(0, 0, 0, 0, ridSolid) }, want: 5},
		{name: "top sub chunk", set: func(c *Chunk) { c.SetBlock(0, 319, 0, 0, ridSolid) }, want: 24},
		{
			name: "highest wins",
			set: func(c *Chunk) {
				c.SetBlock(0, -64, 0, 0, ridSolid)
				c.SetBlock(0, 70, 0, 0, ridSolid)
			},
			want: 9, // y=70 is sub chunk 8.
		},
		{
			// Pinned: removing the block again leaves the palette holding two entries, so the sub chunk is
			// still counted as filled.
			name: "block removed again still counts",
			set: func(c *Chunk) {
				c.SetBlock(0, 70, 0, 0, ridSolid)
				c.SetBlock(0, 70, 0, 0, ridAir)
			},
			want: 9,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChunk()
			tc.set(c)
			if got := c.HighestFilledSubChunk(); got != tc.want {
				t.Fatalf("HighestFilledSubChunk() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestChunkCompactDropsAirLayerZero(t *testing.T) {
	// PINNED BUG: compact drops every storage whose palette is exactly [air], layer 0 included. Layers above
	// it shift down, so a block written to layer 1 is readable on layer 0 afterwards. Layer identity is not
	// preserved across a Compact.
	c := newTestChunk()
	c.SetBlock(1, 10, 2, 1, ridSolid)
	sub := c.SubChunk(10)
	if got := len(sub.Layers()); got != 2 {
		t.Fatalf("len(Layers()) = %d before Compact, want 2", got)
	}
	if got := c.Block(1, 10, 2, 0); got != ridAir {
		t.Fatalf("Block(layer 0) = %d before Compact, want %d", got, ridAir)
	}

	c.Compact()

	if got := len(sub.Layers()); got != 1 {
		t.Fatalf("len(Layers()) = %d after Compact, want 1", got)
	}
	if got := c.Block(1, 10, 2, 0); got != ridSolid {
		t.Fatalf("Block(layer 0) = %d after Compact, want %d; layer 1 shifted down", got, ridSolid)
	}
	if got := c.Block(1, 10, 2, 1); got != ridAir {
		t.Fatalf("Block(layer 1) = %d after Compact, want %d", got, ridAir)
	}
}

func TestChunkCompactDropsFullyAirSubChunks(t *testing.T) {
	c := newTestChunk()
	// Writing and then clearing a block leaves a stale palette entry that keeps the sub chunk non-empty.
	c.SetBlock(1, 10, 2, 0, ridSolid)
	c.SetBlock(1, 10, 2, 0, ridAir)
	if c.SubChunk(10).Empty() {
		t.Fatalf("Empty() = true before Compact, want false")
	}
	if got := c.HighestFilledSubChunk(); got != 5 {
		t.Fatalf("HighestFilledSubChunk() = %d before Compact, want 5", got)
	}

	c.Compact()

	// Compact shrinks the palette back to [air] and then drops the storage entirely.
	if got := len(c.SubChunk(10).Layers()); got != 0 {
		t.Fatalf("len(Layers()) = %d after Compact, want 0", got)
	}
	if !c.SubChunk(10).Empty() {
		t.Fatalf("Empty() = false after Compact, want true")
	}
	if got := c.HighestFilledSubChunk(); got != 0 {
		t.Fatalf("HighestFilledSubChunk() = %d after Compact, want 0", got)
	}
	if got := c.Block(1, 10, 2, 0); got != ridAir {
		t.Fatalf("Block = %d, want %d", got, ridAir)
	}
}

func TestChunkCompactKeepsBlocks(t *testing.T) {
	c := newTestChunk()
	// A deterministic spread of blocks: the pattern is a fixed function of the coordinates, no randomness.
	rids := []uint32{ridSolid, ridGlass, ridLeaves, ridLamp, ridTorch, ridWater, ridSapling, ridChest}
	type placed struct {
		x, z uint8
		y    int16
		rid  uint32
	}
	var want []placed
	for i, y := range []int16{-64, -33, 0, 17, 70, 200, 319} {
		p := placed{x: uint8(i * 2), z: uint8(15 - i), y: y, rid: rids[i%len(rids)]}
		c.SetBlock(p.x, p.y, p.z, 0, p.rid)
		want = append(want, p)
	}
	c.Compact()
	for _, p := range want {
		if got := c.Block(p.x, p.y, p.z, 0); got != p.rid {
			t.Errorf("Block(%d, %d, %d, 0) = %d after Compact, want %d", p.x, p.y, p.z, got, p.rid)
		}
	}
}

func TestChunkHeightMapAliasesInternalSlice(t *testing.T) {
	// PINNED: HeightMap returns the chunk's own slice, not a copy. A caller that writes to it changes the
	// chunk, and the write survives every call that does not dirty the height map.
	c := newTestChunk()
	c.SetBlock(1, 10, 2, 0, ridSolid)
	h := c.HeightMap()
	if &h[0] != &c.heightMap[0] {
		t.Fatalf("HeightMap() returned a copy, want the internal slice")
	}
	h.Set(0, 0, 999)
	if got := c.heightMap.At(0, 0); got != 999 {
		t.Fatalf("chunk.heightMap.At(0,0) = %d, want 999", got)
	}
	if got := c.HeightMap().At(0, 0); got != 999 {
		t.Fatalf("HeightMap().At(0,0) = %d, want 999 on a clean chunk", got)
	}
	// Dirtying the chunk discards the caller's write on the next call. Column (0,0) holds no blocker — the
	// solid block sits at (1,2) — so it recomputes to the minimum height.
	c.SetBlock(1, 10, 2, 0, ridSolid)
	if got := c.HeightMap().At(0, 0); got != -64 {
		t.Fatalf("HeightMap().At(0,0) = %d after recomputation, want -64", got)
	}
	if got := c.HeightMap().At(1, 2); got != 11 {
		t.Fatalf("HeightMap().At(1,2) = %d, want 11", got)
	}
}

func TestChunkHeightMapAllAir(t *testing.T) {
	// PINNED: with no light blocker anywhere, highestLightBlocker returns int16(r[0]) from its final return,
	// which is the only path that skips the +1. So an empty overworld chunk reports -64, not -63.
	c := newTestChunk()
	h := c.HeightMap()
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			if got := h.At(x, z); got != -64 {
				t.Fatalf("HeightMap().At(%d, %d) = %d, want -64", x, z, got)
			}
			if got := c.HighestLightBlocker(x, z); got != -64 {
				t.Fatalf("HighestLightBlocker(%d, %d) = %d, want -64", x, z, got)
			}
		}
	}
}

func TestChunkHeightMapAllSolid(t *testing.T) {
	c := newTestChunk()
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(overworldRange[0]); y <= int16(overworldRange[1]); y++ {
				c.SetBlock(x, y, z, 0, ridSolid)
			}
		}
	}
	// The topmost blocker is at y=319 in every column: sub chunk 23, local Y 15, so 15|304 = 319, +1 = 320.
	h := c.HeightMap()
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			if got := h.At(x, z); got != 320 {
				t.Fatalf("HeightMap().At(%d, %d) = %d, want 320", x, z, got)
			}
			if got := c.HighestLightBlocker(x, z); got != 319 {
				t.Fatalf("HighestLightBlocker(%d, %d) = %d, want 319", x, z, got)
			}
		}
	}
}

func TestChunkHighestLightBlockerArithmetic(t *testing.T) {
	// highestLightBlocker returns `int16(y) | chunk.SubY(index) + plus`. In Go, `|` and `+` share the same
	// precedence level and associate left to right, so this parses as `(y | SubY(index)) + plus` — NOT as
	// `y | (SubY(index)+plus)`. SubY is always a multiple of 16, so its low four bits are clear and the OR
	// with the local Y (0-15) is exactly an addition; the +1 is then applied on top.
	//
	// The values below are what that formula produces today. They are frozen as current behaviour, not
	// asserted to match vanilla Bedrock.
	const x, z = uint8(3), uint8(7)
	cases := []struct {
		name              string
		worldY            int16
		wantSubIndex      int16
		wantLightBlocker  int16 // addOne = false
		wantHeightMapCell int16 // addOne = true
	}{
		{name: "bottom of the world", worldY: -64, wantSubIndex: 0, wantLightBlocker: -64, wantHeightMapCell: -63},
		{name: "sub 0 local 5", worldY: -59, wantSubIndex: 0, wantLightBlocker: -59, wantHeightMapCell: -58},
		{name: "top of sub 0", worldY: -49, wantSubIndex: 0, wantLightBlocker: -49, wantHeightMapCell: -48},
		{name: "bottom of sub 1", worldY: -48, wantSubIndex: 1, wantLightBlocker: -48, wantHeightMapCell: -47},
		{name: "just below zero", worldY: -1, wantSubIndex: 3, wantLightBlocker: -1, wantHeightMapCell: 0},
		{name: "sub 4 local 0", worldY: 0, wantSubIndex: 4, wantLightBlocker: 0, wantHeightMapCell: 1},
		{name: "sub 4 local 5", worldY: 5, wantSubIndex: 4, wantLightBlocker: 5, wantHeightMapCell: 6},
		{name: "sub 8 local 6", worldY: 70, wantSubIndex: 8, wantLightBlocker: 70, wantHeightMapCell: 71},
		{name: "sub 16 local 8", worldY: 200, wantSubIndex: 16, wantLightBlocker: 200, wantHeightMapCell: 201},
		{name: "top of the world", worldY: 319, wantSubIndex: 23, wantLightBlocker: 319, wantHeightMapCell: 320},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChunk()
			if got := c.SubIndex(tc.worldY); got != tc.wantSubIndex {
				t.Fatalf("SubIndex(%d) = %d, want %d", tc.worldY, got, tc.wantSubIndex)
			}
			c.SetBlock(x, tc.worldY, z, 0, ridSolid)
			if got := c.HighestLightBlocker(x, z); got != tc.wantLightBlocker {
				t.Errorf("HighestLightBlocker = %d, want %d", got, tc.wantLightBlocker)
			}
			if got := c.HeightMap().At(x, z); got != tc.wantHeightMapCell {
				t.Errorf("HeightMap().At(%d, %d) = %d, want %d", x, z, got, tc.wantHeightMapCell)
			}
			// Only the written column has a blocker; every other column falls through to the minimum.
			if got := c.HeightMap().At(x+1, z); got != -64 {
				t.Errorf("HeightMap().At(%d, %d) = %d, want -64", x+1, z, got)
			}
		})
	}
}

func TestChunkHighestLightBlockerIgnoresNonBlockers(t *testing.T) {
	// Only blocks with a filtering value of exactly 15 count. Everything else makes the sub chunk non-empty
	// but is skipped, so the scan keeps descending.
	const x, z = uint8(3), uint8(7)
	cases := []struct {
		name             string
		rid              uint32
		wantLightBlocker int16
	}{
		{name: "solid filters 15", rid: ridSolid, wantLightBlocker: 200},
		{name: "lamp filters 15", rid: ridLamp, wantLightBlocker: 200},
		{name: "chest filters 15", rid: ridChest, wantLightBlocker: 200},
		{name: "glass filters 0", rid: ridGlass, wantLightBlocker: -64},
		{name: "leaves filter 1", rid: ridLeaves, wantLightBlocker: -64},
		{name: "water filters 2", rid: ridWater, wantLightBlocker: -64},
		{name: "torch filters 0", rid: ridTorch, wantLightBlocker: -64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChunk()
			c.SetBlock(x, 200, z, 0, tc.rid)
			if got := c.HighestLightBlocker(x, z); got != tc.wantLightBlocker {
				t.Fatalf("HighestLightBlocker = %d, want %d", got, tc.wantLightBlocker)
			}
		})
	}

	t.Run("descends past a non-empty sub chunk", func(t *testing.T) {
		c := newTestChunk()
		c.SetBlock(x, 200, z, 0, ridGlass) // Sub chunk 16, non-empty but not a blocker.
		c.SetBlock(x, 70, z, 0, ridSolid)  // Sub chunk 8.
		if got := c.HighestLightBlocker(x, z); got != 70 {
			t.Fatalf("HighestLightBlocker = %d, want 70", got)
		}
		if got := c.HeightMap().At(x, z); got != 71 {
			t.Fatalf("HeightMap().At = %d, want 71", got)
		}
	})

	t.Run("only layer 0 is scanned", func(t *testing.T) {
		// The scan reads sub.storages[0] directly, so a blocker written to layer 1 is invisible to it.
		c := newTestChunk()
		c.SetBlock(x, 200, z, 1, ridSolid)
		if got := c.HighestLightBlocker(x, z); got != -64 {
			t.Fatalf("HighestLightBlocker = %d, want -64", got)
		}
		if got := c.Block(x, 200, z, 1); got != ridSolid {
			t.Fatalf("Block(layer 1) = %d, want %d", got, ridSolid)
		}
	})
}
