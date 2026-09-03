package chunk

import (
	"math"
	"runtime"
	"strings"
	"testing"
)

// heightMapRamp returns a 256 entry HeightMap where every column holds its own backing index as a value. That
// makes every column distinguishable, so a test can tell exactly which entry a lookup reached.
func heightMapRamp() HeightMap {
	h := make(HeightMap, 256)
	for i := range h {
		h[i] = int16(i)
	}
	return h
}

// heightMapPanicOf runs f and returns the value it panicked with, or nil if it returned normally.
func heightMapPanicOf(f func()) (recovered any) {
	defer func() { recovered = recover() }()
	f()
	return nil
}

func TestHeightMapAtAndSetIndexing(t *testing.T) {
	// The backing index is (x<<4)|z, so x selects a 16 entry row and z the offset within it.
	cases := []struct {
		x, z uint8
		want int
	}{
		{x: 0, z: 0, want: 0},
		{x: 0, z: 1, want: 1},
		{x: 0, z: 15, want: 15},
		{x: 1, z: 0, want: 16},
		{x: 3, z: 7, want: 55},
		{x: 15, z: 0, want: 240},
		{x: 15, z: 15, want: 255},
	}
	for _, tc := range cases {
		h := make(HeightMap, 256)
		h.Set(tc.x, tc.z, 1234)
		if got := h[tc.want]; got != 1234 {
			t.Errorf("Set(%d, %d) wrote to a different index; h[%d] = %d", tc.x, tc.z, tc.want, got)
		}
		if got := h.At(tc.x, tc.z); got != 1234 {
			t.Errorf("At(%d, %d) = %d, want 1234", tc.x, tc.z, got)
		}
		// Exactly one entry changed.
		for i, v := range h {
			if i != tc.want && v != 0 {
				t.Errorf("Set(%d, %d) also wrote h[%d] = %d", tc.x, tc.z, i, v)
			}
		}
	}
	// Reading back the ramp confirms the index formula in the other direction.
	ramp := heightMapRamp()
	for _, tc := range cases {
		if got := ramp.At(tc.x, tc.z); got != int16(tc.want) {
			t.Errorf("At(%d, %d) = %d, want %d", tc.x, tc.z, got, tc.want)
		}
	}
}

func TestHeightMapAtAndSetDoNotMask(t *testing.T) {
	// PINNED BUG: neither At nor Set masks x or z to 0-15. The index is computed on uint16, so an out of range z
	// bleeds into the row bits and silently aliases another column, while anything that pushes the index to
	// 256 or beyond panics on the slice.
	t.Run("out of range z aliases another column", func(t *testing.T) {
		cases := []struct {
			name             string
			x, z             uint8
			aliasX, aliasZ   uint8
			wantBackingIndex int
		}{
			// (15<<4)|17 = 240|17 = 241, which is also (15<<4)|1.
			{name: "z=17 aliases z=1", x: 15, z: 17, aliasX: 15, aliasZ: 1, wantBackingIndex: 241},
			// (0<<4)|255 = 255, which is also (15<<4)|15.
			{name: "x=0 z=255 aliases (15,15)", x: 0, z: 255, aliasX: 15, aliasZ: 15, wantBackingIndex: 255},
			// (2<<4)|33 = 32|33 = 33, which is also (2<<4)|1.
			{name: "z=33 aliases z=1", x: 2, z: 33, aliasX: 2, aliasZ: 1, wantBackingIndex: 33},
			// (15<<4)|16 = 240|16 = 240: bit 4 is already set by x, so z=16 is swallowed entirely and the
			// write lands on (15,0) rather than out of range.
			{name: "z=16 aliases z=0", x: 15, z: 16, aliasX: 15, aliasZ: 0, wantBackingIndex: 240},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				h := make(HeightMap, 256)
				h.Set(tc.x, tc.z, 77)
				if got := h[tc.wantBackingIndex]; got != 77 {
					t.Fatalf("h[%d] = %d, want 77", tc.wantBackingIndex, got)
				}
				if got := h.At(tc.aliasX, tc.aliasZ); got != 77 {
					t.Fatalf("At(%d, %d) = %d, want 77", tc.aliasX, tc.aliasZ, got)
				}
			})
		}
	})
	t.Run("index past the end panics", func(t *testing.T) {
		cases := []struct {
			name string
			x, z uint8
		}{
			{name: "x=16 z=0", x: 16, z: 0},   // (16<<4)|0 = 256
			{name: "x=16 z=15", x: 16, z: 15}, // 256|15 = 271
			{name: "x=255 z=0", x: 255, z: 0}, // (255<<4)|0 = 4080
			// z alone can never overflow the slice: x is only 8 bits wide, so (x<<4)|z stays below 256 for
			// every z whenever x < 16. Only an x of 16 or more can push the index out of range.
			{name: "x=16 z=255", x: 16, z: 255}, // 256|255 = 511
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				for _, op := range []struct {
					name string
					fn   func(h HeightMap)
				}{
					{name: "At", fn: func(h HeightMap) { h.At(tc.x, tc.z) }},
					{name: "Set", fn: func(h HeightMap) { h.Set(tc.x, tc.z, 1) }},
				} {
					h := make(HeightMap, 256)
					v := heightMapPanicOf(func() { op.fn(h) })
					if v == nil {
						t.Fatalf("%s(%d, %d): expected a panic, got none", op.name, tc.x, tc.z)
					}
					err, ok := v.(runtime.Error)
					if !ok || !strings.Contains(err.Error(), "index out of range") {
						t.Fatalf("%s(%d, %d): panic %v, want an out of range index error", op.name, tc.x, tc.z, v)
					}
				}
			})
		}
	})
}

func TestHeightMapHighestNeighbour(t *testing.T) {
	// The four guards (x!=15, x!=0, z!=15, z!=0) mean a column on an edge loses one neighbour and a corner
	// loses two: corners sample only 2 of the 4 directions. The column itself is never sampled.
	ramp := heightMapRamp()
	cases := []struct {
		name string
		x, z uint8
		want int16
	}{
		// (0,0): only (1,0)=16 and (0,1)=1 are sampled; 16 wins.
		{name: "corner 0,0", x: 0, z: 0, want: 16},
		// (15,15): only (14,15)=239 and (15,14)=254; 254 wins.
		{name: "corner 15,15", x: 15, z: 15, want: 254},
		// (15,0): only (14,0)=224 and (15,1)=241; 241 wins.
		{name: "corner 15,0", x: 15, z: 0, want: 241},
		// (0,15): only (1,15)=31 and (0,14)=14; 31 wins.
		{name: "corner 0,15", x: 0, z: 15, want: 31},
		// (0,5): (1,5)=21, (0,6)=6, (0,4)=4; x-1 is skipped.
		{name: "edge x=0", x: 0, z: 5, want: 21},
		// (15,5): (14,5)=229, (15,6)=246, (15,4)=244; x+1 is skipped.
		{name: "edge x=15", x: 15, z: 5, want: 246},
		// (5,5): (6,5)=101, (4,5)=69, (5,6)=86, (5,4)=84; all four sampled.
		{name: "interior", x: 5, z: 5, want: 101},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ramp.HighestNeighbour(tc.x, tc.z); got != tc.want {
				t.Fatalf("HighestNeighbour(%d, %d) = %d, want %d", tc.x, tc.z, got, tc.want)
			}
		})
	}

	t.Run("own value is ignored", func(t *testing.T) {
		h := make(HeightMap, 256)
		h.Set(5, 5, 1000)
		h.Set(6, 5, 12)
		if got := h.HighestNeighbour(5, 5); got != 12 {
			t.Fatalf("HighestNeighbour(5, 5) = %d, want 12", got)
		}
	})

	t.Run("counts sampled directions", func(t *testing.T) {
		// Probe each direction in isolation: a direction is sampled iff raising only that neighbour raises
		// the result.
		type dir struct {
			name   string
			dx, dz int
		}
		dirs := []dir{{"x+1", 1, 0}, {"x-1", -1, 0}, {"z+1", 0, 1}, {"z-1", 0, -1}}
		cases := []struct {
			name  string
			x, z  uint8
			count int
		}{
			{name: "corner 0,0", x: 0, z: 0, count: 2},
			{name: "corner 15,0", x: 15, z: 0, count: 2},
			{name: "corner 0,15", x: 0, z: 15, count: 2},
			{name: "corner 15,15", x: 15, z: 15, count: 2},
			{name: "edge", x: 0, z: 7, count: 3},
			{name: "interior", x: 7, z: 7, count: 4},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				count := 0
				for _, d := range dirs {
					nx, nz := int(tc.x)+d.dx, int(tc.z)+d.dz
					if nx < 0 || nx > 15 || nz < 0 || nz > 15 {
						continue
					}
					h := make(HeightMap, 256)
					h.Set(uint8(nx), uint8(nz), 500)
					if h.HighestNeighbour(tc.x, tc.z) == 500 {
						count++
					}
				}
				if count != tc.count {
					t.Fatalf("HighestNeighbour(%d, %d) sampled %d directions, want %d", tc.x, tc.z, count, tc.count)
				}
			})
		}
	})

	t.Run("uniform map", func(t *testing.T) {
		// Nothing beats the seed value unless a neighbour is read, so a flat map returns that flat value and
		// never math.MinInt16.
		h := make(HeightMap, 256)
		for i := range h {
			h[i] = -64
		}
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				if got := h.HighestNeighbour(x, z); got != -64 {
					t.Fatalf("HighestNeighbour(%d, %d) = %d, want -64", x, z, got)
				}
			}
		}
	})

	t.Run("all neighbours at the seed value", func(t *testing.T) {
		// PINNED BUG: highest starts at math.MinInt16 and the comparison is strictly greater, so neighbours that
		// already hold math.MinInt16 never replace it. The seed is returned either way, making the two cases
		// indistinguishable to a caller.
		h := make(HeightMap, 256)
		for i := range h {
			h[i] = math.MinInt16
		}
		if got := h.HighestNeighbour(5, 5); got != math.MinInt16 {
			t.Fatalf("HighestNeighbour(5, 5) = %d, want %d", got, int16(math.MinInt16))
		}
	})
}

func TestHeightMapFromChunkIsUsableWithHighestNeighbour(t *testing.T) {
	// The height map a chunk hands out is a plain 256 entry HeightMap, so HighestNeighbour works on it
	// directly. Column (5,5) holds a solid block at y=70: sub chunk 8, SubY 64, local Y 6, so (6|64)+1 = 71.
	c := newTestChunk()
	c.SetBlock(5, 70, 5, 0, ridSolid)
	h := c.HeightMap()
	if got := h.At(5, 5); got != 71 {
		t.Fatalf("At(5, 5) = %d, want 71", got)
	}
	// Its neighbours are all air, so they sit at the minimum height.
	if got := h.HighestNeighbour(5, 5); got != -64 {
		t.Fatalf("HighestNeighbour(5, 5) = %d, want -64", got)
	}
	// And the block's own column is the highest neighbour of the column beside it.
	if got := h.HighestNeighbour(4, 5); got != 71 {
		t.Fatalf("HighestNeighbour(4, 5) = %d, want 71", got)
	}
}
