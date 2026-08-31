package chunk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

// Characterization tests for light.go, light_area.go and light_type.go.
//
// Lighting is fully deterministic: no maps are iterated, no goroutines are started and lightQueue is a FIFO
// ring buffer. Every expectation below is therefore an exact value, not a range.
//
// Several tests here freeze behaviour that looks wrong when compared to vanilla. Those are marked QUIRK. They
// exist so that the execution-model rewrite notices when it changes them; they are not correctness claims.

// lightSkyAt reads the skylight through the public Chunk accessor.
func lightSkyAt(c *Chunk, x, y, z int) uint8 {
	return c.SkyLight(uint8(x), int16(y), uint8(z))
}

// lightBlockAt reads the block light of a position. Chunk has no block light accessor, so the sub chunk is
// resolved the same way Chunk.Light does.
func lightBlockAt(c *Chunk, x, y, z int) uint8 {
	return c.SubChunk(int16(y)).BlockLight(uint8(x)&0xf, uint8(y&0xf), uint8(z)&0xf)
}

// lightFill runs the single-chunk filling stage exactly as World.loadChunk does.
func lightFill(c *Chunk, cx, cz int) {
	LightArea([]*Chunk{c}, cx, cz).Fill()
}

// lightLayer fills the whole 16x16 cross section at y with rid.
func lightLayer(c *Chunk, y int, rid uint32) {
	for x := range 16 {
		for z := range 16 {
			c.SetBlock(uint8(x), int16(y), uint8(z), 0, rid)
		}
	}
}

// lightRecover runs f and reports the recovered panic value rendered as a string.
func lightRecover(f func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg, panicked = fmt.Sprint(r), true
		}
	}()
	f()
	return "", false
}

// lightAssertPanic fails unless f panics with a message containing want.
func lightAssertPanic(t *testing.T, want string, f func()) {
	t.Helper()
	msg, panicked := lightRecover(f)
	if !panicked {
		t.Fatalf("expected a panic containing %q, got none", want)
	}
	if want != "" && !lightContains(msg, want) {
		t.Fatalf("panic message = %q, want it to contain %q", msg, want)
	}
}

func lightContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestLightFreshChunkHasNilLightSlices pins that a chunk straight out of New is unusable for light reads.
// NewSubChunk leaves blockLight and skyLight nil, and none of the accessors below check the length before
// indexing, so each one panics with an index-out-of-range. This is the trap that makes a decoded chunk
// unusable until Fill has run over it; it is current behaviour, not a claim that it is desirable.
func TestLightFreshChunkHasNilLightSlices(t *testing.T) {
	cases := []struct {
		name string
		call func(c *Chunk)
	}{
		{"Chunk.Light", func(c *Chunk) { _ = c.Light(0, 0, 0) }},
		{"Chunk.SkyLight", func(c *Chunk) { _ = c.SkyLight(0, 0, 0) }},
		{"SubChunk.SkyLight", func(c *Chunk) { _ = c.SubChunk(0).SkyLight(0, 0, 0) }},
		{"SubChunk.BlockLight", func(c *Chunk) { _ = c.SubChunk(0).BlockLight(0, 0, 0) }},
		{"SubChunk.SetBlockLight", func(c *Chunk) { c.SubChunk(0).SetBlockLight(0, 0, 0, 5) }},
		{"SubChunk.SetSkyLight", func(c *Chunk) { c.SubChunk(0).SetSkyLight(0, 0, 0, 5) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChunk()
			lightAssertPanic(t, "index out of range", func() { tc.call(c) })
		})
	}

	// After Fill the same calls are safe, which is what makes the panic above a lifecycle trap rather than a
	// permanent property of the type.
	t.Run("safe after Fill", func(t *testing.T) {
		c := newTestChunk()
		lightFill(c, 0, 0)
		if got := c.SkyLight(0, 0, 0); got != 15 {
			t.Fatalf("SkyLight after Fill = %d, want 15", got)
		}
	})
}

// TestLightFillEmptyChunk pins that an empty chunk ends up lit by full skylight over its entire vertical
// range and with no block light at all. initialiseLightSlices hands every empty sub chunk the shared
// fullLight slice, so this holds from the bottom of the range to the top.
func TestLightFillEmptyChunk(t *testing.T) {
	c := newTestChunk()
	lightFill(c, 0, 0)

	for _, y := range []int{-64, -63, -1, 0, 1, 15, 16, 63, 64, 200, 318, 319} {
		if got := lightSkyAt(c, 8, y, 8); got != 15 {
			t.Errorf("skylight at y=%d = %d, want 15", y, got)
		}
		if got := lightBlockAt(c, 8, y, 8); got != 0 {
			t.Errorf("block light at y=%d = %d, want 0", y, got)
		}
	}
	// Corners of the chunk are lit identically; nothing about the column loop treats edges differently in a
	// single-chunk area.
	for _, p := range [][2]int{{0, 0}, {0, 15}, {15, 0}, {15, 15}} {
		if got := lightSkyAt(c, p[0], 100, p[1]); got != 15 {
			t.Errorf("skylight at x=%d z=%d = %d, want 15", p[0], p[1], got)
		}
	}
}

// TestLightFillSolidFloor pins the skylight cut-off produced by a full solid floor. The height map stores
// highestLightBlocker+1, so the floor at y=0 gives a height of 1: y=1 is the lowest lit level, the floor
// itself and everything under it stay at 0.
func TestLightFillSolidFloor(t *testing.T) {
	c := newTestChunk()
	lightLayer(c, 0, ridSolid)
	lightFill(c, 0, 0)

	cases := []struct {
		y    int
		want uint8
	}{
		{-64, 0}, // no node is ever inserted below the height map value.
		{-1, 0},
		{0, 0},  // the floor block itself: filtering 15 blocks all light.
		{1, 15}, // the height map value, set to 15 directly by insertSkyLightNodes.
		{2, 15},
		{15, 15},
		{16, 15}, // sub chunks above the highest non-empty one keep the shared fullLight slice.
		{319, 15},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("y=%d", tc.y), func(t *testing.T) {
			if got := lightSkyAt(c, 8, tc.y, 8); got != tc.want {
				t.Fatalf("skylight = %d, want %d", got, tc.want)
			}
			if got := lightBlockAt(c, 8, tc.y, 8); got != 0 {
				t.Fatalf("block light = %d, want 0 (no emitter present)", got)
			}
		})
	}
}

// TestLightFillEmitterFalloff pins the block light falloff around a single emitter in an otherwise empty
// chunk. propagateNeighbour subtracts FilteringBlock(destination)+1, so through air every step costs exactly
// 1 and the level at Manhattan distance d is source-d. The emitter's own filtering value is irrelevant: the
// filter is looked up at the destination, which is why ridLamp (filtering 15) still lights its neighbours.
func TestLightFillEmitterFalloff(t *testing.T) {
	emitters := []struct {
		name   string
		rid    uint32
		source uint8
	}{
		{"lamp", ridLamp, 15},   // LightBlock 15, FilteringBlock 15
		{"torch", ridTorch, 14}, // LightBlock 14, FilteringBlock 0
	}
	offsets := []struct {
		dx, dy, dz int
	}{
		{0, 0, 0},
		{1, 0, 0}, {2, 0, 0}, {3, 0, 0},
		{-1, 0, 0}, {-2, 0, 0}, {-3, 0, 0},
		{0, 1, 0}, {0, 2, 0}, {0, 3, 0},
		{0, -1, 0}, {0, -2, 0}, {0, -3, 0},
		{0, 0, 1}, {0, 0, 2}, {0, 0, 3},
		{0, 0, -1}, {0, 0, -2}, {0, 0, -3},
		{1, 1, 0}, {1, 1, 1}, // diagonals cost the Manhattan distance, not the Euclidean one.
	}
	for _, e := range emitters {
		t.Run(e.name, func(t *testing.T) {
			c := newTestChunk()
			c.SetBlock(8, 0, 8, 0, e.rid)
			lightFill(c, 0, 0)

			for _, o := range offsets {
				d := lightAbs(o.dx) + lightAbs(o.dy) + lightAbs(o.dz)
				want := uint8(0)
				if int(e.source) > d {
					want = e.source - uint8(d)
				}
				got := lightBlockAt(c, 8+o.dx, 0+o.dy, 8+o.dz)
				if got != want {
					t.Errorf("block light at (%d,%d,%d) = %d, want %d (source %d - distance %d)",
						8+o.dx, o.dy, 8+o.dz, got, want, e.source, d)
				}
			}
		})
	}
}

func lightAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestLightFillFiltering pins how FilteringBlock reduces block light. The cost of entering a block is
// FilteringBlock+1, so glass (0) costs 1 exactly like air, leaves (1) cost 2 and water (2) cost 3. Solid (15)
// costs 16, which is more than the maximum level, so it never receives any light.
//
// The value two blocks away is the maximum over every path, including the detour one block up and back down,
// which is why the "beyond" column does not simply keep subtracting the filter.
func TestLightFillFiltering(t *testing.T) {
	cases := []struct {
		name             string
		rid              uint32
		atFilter, beyond uint8
	}{
		// lamp(15) -> +x neighbour -> +x again. Detour path is 15-1-1-1-1 = 11 at the "beyond" position.
		{"air", ridAir, 14, 13},
		{"glass", ridGlass, 14, 13},   // filtering 0: identical to air.
		{"leaves", ridLeaves, 13, 12}, // 15-2 = 13, then 13-1 = 12 which beats the detour's 11.
		{"water", ridWater, 12, 11},   // 15-3 = 12, then 12-1 = 11, tying with the detour.
		{"solid", ridSolid, 0, 11},    // 15 < 16, so nothing enters; the beyond value is the detour's 11.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChunk()
			c.SetBlock(8, 0, 8, 0, ridLamp)
			if tc.rid != ridAir {
				c.SetBlock(9, 0, 8, 0, tc.rid)
			}
			lightFill(c, 0, 0)

			if got := lightBlockAt(c, 8, 0, 8); got != 15 {
				t.Fatalf("block light at the lamp = %d, want 15", got)
			}
			if got := lightBlockAt(c, 9, 0, 8); got != tc.atFilter {
				t.Errorf("block light inside the %s = %d, want %d", tc.name, got, tc.atFilter)
			}
			if got := lightBlockAt(c, 10, 0, 8); got != tc.beyond {
				t.Errorf("block light beyond the %s = %d, want %d", tc.name, got, tc.beyond)
			}
		})
	}
}

// TestLightFillSkylightUnderOverhang pins skylight actually propagating sideways rather than merely being
// blanket-set. A solid floor at y=0 spans the chunk and a solid roof at y=10 covers x=0..7, so the height map
// is 11 under the roof and 1 in the open half. insertSkyLightNodes queues nodes for the open column x=8 only
// where a neighbouring column is higher, and propagation carries them west at one level per block.
func TestLightFillSkylightUnderOverhang(t *testing.T) {
	c := newTestChunk()
	lightLayer(c, 0, ridSolid)
	for x := range 8 {
		for z := range 16 {
			c.SetBlock(uint8(x), 10, uint8(z), 0, ridSolid)
		}
	}
	lightFill(c, 0, 0)

	// At y=5, under the roof, light comes only from the open column at x=8 which holds a full 15.
	cases := []struct {
		x    int
		want uint8
	}{
		{8, 15}, {7, 14}, {6, 13}, {5, 12}, {4, 11}, {3, 10}, {2, 9}, {1, 8}, {0, 7},
	}
	for _, tc := range cases {
		if got := lightSkyAt(c, tc.x, 5, 8); got != tc.want {
			t.Errorf("skylight at x=%d y=5 = %d, want %d (15 minus %d blocks from the open column)",
				tc.x, got, tc.want, 8-tc.x)
		}
	}
	// The roof block itself costs 16 to enter, so it stays dark, while the column directly above it is the
	// height map value and is set to 15.
	if got := lightSkyAt(c, 3, 10, 8); got != 0 {
		t.Errorf("skylight inside the roof = %d, want 0", got)
	}
	if got := lightSkyAt(c, 3, 11, 8); got != 15 {
		t.Errorf("skylight above the roof = %d, want 15", got)
	}
}

// TestLightFillSkylightIgnoresDiffusingBlocks is a QUIRK. Leaves diffuse light in vanilla, but the height map
// only records blocks with FilteringBlock == 15. A leaves layer therefore leaves the height map at the bottom
// of the range and insertSkyLightNodes blanket-sets the whole column to 15, so the leaves cost nothing at all
// for skylight. Frozen as current behaviour; this is not a vanilla-correctness claim.
func TestLightFillSkylightIgnoresDiffusingBlocks(t *testing.T) {
	c := newTestChunk()
	lightLayer(c, 5, ridLeaves)
	lightFill(c, 0, 0)

	for _, y := range []int{-64, 0, 4, 5, 6, 15, 16, 319} {
		if got := lightSkyAt(c, 8, y, 8); got != 15 {
			t.Errorf("skylight at y=%d = %d, want 15", y, got)
		}
	}

	// Glass has the same outcome, but for the honest reason that its filtering value really is 0.
	g := newTestChunk()
	lightLayer(g, 5, ridGlass)
	lightFill(g, 0, 0)
	for _, y := range []int{0, 5, 6, 319} {
		if got := lightSkyAt(g, 8, y, 8); got != 15 {
			t.Errorf("skylight through glass at y=%d = %d, want 15", y, got)
		}
	}
}

// lightNewArea builds n*n empty chunks and returns them in the order LightArea expects: index = cx + cz*w,
// the same order World.spreadLight appends them in.
func lightNewArea(n int) []*Chunk {
	c := make([]*Chunk, n*n)
	for i := range c {
		c[i] = newTestChunk()
	}
	return c
}

// lightFillEach runs the filling stage on every chunk individually, which is what the world does before it
// ever calls Spread.
func lightFillEach(c []*Chunk, w, baseCX, baseCZ int) {
	for i, ch := range c {
		lightFill(ch, baseCX+i%w, baseCZ+i/w)
	}
}

// TestLightSpreadBlockLightAcrossChunkBorder pins the multi-chunk path: a torch one block from the +X border
// of chunk 0 lights the first columns of chunk 1 only after Spread runs. Fill alone cannot cross a border
// because a lightArea of one chunk clamps propagation to its own bounds.
func TestLightSpreadBlockLightAcrossChunkBorder(t *testing.T) {
	c := lightNewArea(3)
	c[0].SetBlock(15, 0, 8, 0, ridTorch)
	lightFillEach(c, 3, 0, 0)

	// Fill leaves the neighbour completely dark: propagateNeighbour rejects anything outside the area.
	if got := lightBlockAt(c[1], 0, 0, 8); got != 0 {
		t.Fatalf("block light in the neighbour after Fill = %d, want 0", got)
	}

	LightArea(c, 0, 0).Spread()

	cases := []struct {
		name string
		c    *Chunk
		x    int
		want uint8
	}{
		{"torch itself", c[0], 15, 14},
		{"one west of the torch", c[0], 14, 13},
		// The edge node is pushed at level la-filter with filter 1 for air, so the first column of the
		// neighbour is 14-1 = 13 and each further column costs another 1.
		{"neighbour x=0", c[1], 0, 13},
		{"neighbour x=1", c[1], 1, 12},
		{"neighbour x=2", c[1], 2, 11},
		{"neighbour x=3", c[1], 3, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lightBlockAt(tc.c, tc.x, 0, 8); got != tc.want {
				t.Fatalf("block light = %d, want %d", got, tc.want)
			}
		})
	}
	// Chunk 3 is the +Z neighbour of chunk 0. The torch sits 8 blocks from that border and its light has
	// dropped to 14-7 = 7 by z=15, so 6 crosses into chunk 3 at z=0 and dies out 6 columns in.
	if got := lightBlockAt(c[3], 15, 0, 0); got != 6 {
		t.Errorf("block light across the Z border = %d, want 6", got)
	}
	// Spread is not a single border hop: once a node has been pushed, propagate runs over the whole area, so
	// the diagonal chunk is reached the long way around. c[4] starts at (16,16) in block space and the torch
	// sits at (15,8), a Manhattan distance of 9, leaving 14-9 = 5.
	if got := lightBlockAt(c[4], 0, 0, 0); got != 5 {
		t.Errorf("block light in the diagonal chunk = %d, want 5", got)
	}
}

// TestLightSpreadSkyLightAcrossChunkBorder pins skylight crossing a border into a fully roofed chunk. The
// centre chunk of the 3x3 is covered at y=10, so after Fill everything below the roof is dark; the eight
// neighbours are empty and hold 15 everywhere. Spread pushes la-1 = 14 into the first column of the roofed
// chunk and propagation takes it inwards at one level per block.
func TestLightSpreadSkyLightAcrossChunkBorder(t *testing.T) {
	c := lightNewArea(3)
	lightLayer(c[4], 10, ridSolid)
	lightFillEach(c, 3, 0, 0)

	if got := lightSkyAt(c[4], 0, 5, 8); got != 0 {
		t.Fatalf("skylight under the roof after Fill = %d, want 0", got)
	}

	LightArea(c, 0, 0).Spread()

	// Along +X from the west border at z=8, where the north and south borders are 8 blocks away and so
	// contribute less than the west one.
	cases := []struct {
		x    int
		want uint8
	}{
		{0, 14}, {1, 13}, {2, 12}, {3, 11},
	}
	for _, tc := range cases {
		if got := lightSkyAt(c[4], tc.x, 5, 8); got != tc.want {
			t.Errorf("skylight at x=%d = %d, want %d", tc.x, got, tc.want)
		}
	}
	// The roof itself costs 16 to enter and stays dark even after spreading.
	if got := lightSkyAt(c[4], 3, 10, 8); got != 0 {
		t.Errorf("skylight inside the roof = %d, want 0", got)
	}
	// Above the roof both sides already hold 15, so no node is ever queued there and nothing changes.
	if got := lightSkyAt(c[4], 3, 11, 8); got != 15 {
		t.Errorf("skylight above the roof = %d, want 15", got)
	}
}

// TestLightSpreadTopSubChunkIsSkipped is a PINNED BUG. iterEdges runs cy over [r[0]>>4, r[1]>>4), and for the
// overworld range that is -4..18, while sub chunk indices reach 19 (y 304..319). The topmost sub chunk is
// therefore never considered for spreading. It is invisible for skylight, which is 15 up there anyway, but a
// block light source in the top sub chunk does not cross a chunk border at all. Frozen as current behaviour.
func TestLightSpreadTopSubChunkIsSkipped(t *testing.T) {
	// Each case uses its own area: a source below the top sub chunk would otherwise leak upwards through
	// propagate and hide the missing edge pass.
	t.Run("y=310 does not cross", func(t *testing.T) {
		c := lightNewArea(3)
		c[0].SetBlock(15, 310, 8, 0, ridTorch)
		lightFillEach(c, 3, 0, 0)
		LightArea(c, 0, 0).Spread()

		if got := lightBlockAt(c[0], 15, 310, 8); got != 14 {
			t.Fatalf("block light at the torch = %d, want 14", got)
		}
		if got := lightBlockAt(c[0], 14, 310, 8); got != 13 {
			t.Fatalf("block light next to the torch = %d, want 13", got)
		}
		if got := lightBlockAt(c[1], 0, 310, 8); got != 0 {
			t.Errorf("block light across the border = %d, want 0 (top sub chunk is never iterated)", got)
		}
	})
	t.Run("y=300 crosses normally", func(t *testing.T) {
		c := lightNewArea(3)
		c[0].SetBlock(15, 300, 8, 0, ridTorch)
		lightFillEach(c, 3, 0, 0)
		LightArea(c, 0, 0).Spread()

		if got := lightBlockAt(c[1], 0, 300, 8); got != 13 {
			t.Errorf("block light across the border = %d, want 13", got)
		}
		// The same source does reach y=310 in the neighbour, but only by climbing 10 blocks after it had
		// already crossed lower down.
		if got := lightBlockAt(c[1], 0, 310, 8); got != 3 {
			t.Errorf("block light at y=310 in the neighbour = %d, want 3 (13 minus 10 blocks of climb)", got)
		}
	})
}

// TestLightAreaPanics pins the two ways LightArea refuses a chunk slice.
func TestLightAreaPanics(t *testing.T) {
	t.Run("non-square length", func(t *testing.T) {
		for _, n := range []int{2, 3, 5, 8} {
			c := make([]*Chunk, n)
			for i := range c {
				c[i] = newTestChunk()
			}
			lightAssertPanic(t, "area must have a square chunk area", func() { LightArea(c, 0, 0) })
		}
	})
	// An empty slice passes the square check, because 0 == 0*0, and then falls over on c[0]. The message is
	// an index-out-of-range rather than the intended one; current behaviour.
	t.Run("empty slice", func(t *testing.T) {
		lightAssertPanic(t, "index out of range", func() { LightArea(nil, 0, 0) })
		lightAssertPanic(t, "index out of range", func() { LightArea([]*Chunk{}, 0, 0) })
	})
	// Square lengths are accepted and keep the base coordinates in block space.
	t.Run("square lengths accepted", func(t *testing.T) {
		for _, n := range []int{1, 4, 9, 16} {
			c := make([]*Chunk, n)
			for i := range c {
				c[i] = newTestChunk()
			}
			a := LightArea(c, 2, -3)
			if a.w*a.w != n {
				t.Fatalf("w = %d for %d chunks, want w*w == %d", a.w, n, n)
			}
			if a.baseX != 32 || a.baseZ != -48 {
				t.Fatalf("baseX, baseZ = %d, %d, want 32, -48 (chunk coordinates shifted left by 4)", a.baseX, a.baseZ)
			}
		}
	})
}

// TestLightFillMultiChunkUsesOnlyChunkZeroHeightmap is a PINNED BUG, and the most important one in this file.
// iterHeightmap reads the height map, the highest and lowest Y and the 16x16 column loop exclusively from
// a.c[0]. On a multi-chunk area, Fill therefore inserts skylight nodes only for chunk 0's columns: every other
// chunk keeps whatever initialiseLightSlices left behind, which is zero skylight for every sub chunk at or
// below the highest non-empty one.
//
// Block light does not share the defect: insertBlockLightNodes goes through iterSubChunks, which visits all
// chunks in the area.
//
// The production code only ever calls Fill with a single chunk, so this is latent. It stops being latent the
// moment the execution model batches chunks into one Fill: every chunk but the first comes out unlit. Frozen
// here as current behaviour, not as a vanilla-correctness claim.
func TestLightFillMultiChunkUsesOnlyChunkZeroHeightmap(t *testing.T) {
	c := lightNewArea(2)
	// Chunk 0 stays empty. Chunk 1 gets a floor and a lamp, so its content differs from chunk 0's.
	lightLayer(c[1], 0, ridSolid)
	c[1].SetBlock(8, 5, 8, 0, ridLamp)

	LightArea(c, 0, 0).Fill()

	t.Run("chunk 0 is lit normally", func(t *testing.T) {
		for _, y := range []int{-64, 0, 1, 100, 319} {
			if got := lightSkyAt(c[0], 8, y, 8); got != 15 {
				t.Errorf("chunk 0 skylight at y=%d = %d, want 15", y, got)
			}
		}
	})
	t.Run("chunk 1 skylight is wrong", func(t *testing.T) {
		cases := []struct {
			y    int
			want uint8
		}{
			// Open sky sits directly above the floor, so 15 would be correct. No node was ever inserted for
			// this column, so the sub chunk keeps the zeroed slice initialiseLightSlices gave it.
			{1, 0},
			{5, 0},
			{15, 0},
			// Sub chunks above the highest non-empty one were handed the shared fullLight slice, so those are
			// 15 by accident rather than by calculation.
			{16, 15},
			{319, 15},
		}
		for _, tc := range cases {
			if got := lightSkyAt(c[1], 8, tc.y, 8); got != tc.want {
				t.Errorf("chunk 1 skylight at y=%d = %d, want %d", tc.y, got, tc.want)
			}
		}
	})
	t.Run("chunk 1 block light is correct", func(t *testing.T) {
		if got := lightBlockAt(c[1], 8, 5, 8); got != 15 {
			t.Errorf("block light at the lamp = %d, want 15", got)
		}
		if got := lightBlockAt(c[1], 9, 5, 8); got != 14 {
			t.Errorf("block light next to the lamp = %d, want 14", got)
		}
		if got := lightBlockAt(c[1], 11, 5, 8); got != 12 {
			t.Errorf("block light three blocks from the lamp = %d, want 12", got)
		}
	})
}

// TestLightGlobalsAreCopyOnWrite pins that the process-wide fullLight and noLight slices, which every sub
// chunk aliases after Fill, are never written through. Both public setters copy before their first write, so
// two independently created chunks cannot observe each other's light.
//
// Nothing here writes into sub.skyLight or sub.blockLight directly on purpose: doing so would mutate the
// shared globals and corrupt every other test in the binary.
func TestLightGlobalsAreCopyOnWrite(t *testing.T) {
	c1, c2 := newTestChunk(), newTestChunk()
	lightFill(c1, 0, 0)
	lightFill(c2, 1, 0)

	// y=100 is well above any content, so both chunks alias fullLight for skylight and noLight for block
	// light there.
	sub1, sub2 := c1.SubChunk(100), c2.SubChunk(100)
	if &sub1.skyLight[0] != fullLightPtr || &sub2.skyLight[0] != fullLightPtr {
		t.Fatalf("expected both chunks to alias fullLight before any write")
	}
	if &sub1.blockLight[0] != noLightPtr || &sub2.blockLight[0] != noLightPtr {
		t.Fatalf("expected both chunks to alias noLight before any write")
	}

	sub1.SetSkyLight(3, 4, 5, 7)
	sub1.SetBlockLight(3, 4, 5, 9)

	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"written skylight", sub1.SkyLight(3, 4, 5), 7},
		{"written block light", sub1.BlockLight(3, 4, 5), 9},
		{"other chunk skylight", sub2.SkyLight(3, 4, 5), 15},
		{"other chunk block light", sub2.BlockLight(3, 4, 5), 0},
		{"untouched position in the same sub chunk", sub1.SkyLight(3, 5, 5), 15},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	// A chunk filled after the writes above still sees pristine globals.
	c3 := newTestChunk()
	lightFill(c3, 2, 0)
	if got := c3.SkyLight(3, 100, 5); got != 15 {
		t.Errorf("skylight of a chunk filled afterwards = %d, want 15", got)
	}
	if got := c3.SubChunk(100).BlockLight(3, 4, 5); got != 0 {
		t.Errorf("block light of a chunk filled afterwards = %d, want 0", got)
	}

	// SetBlockLight only copies when the slice is noLight, not when it is fullLight, whereas SetSkyLight
	// checks both. Nothing assigns fullLight to blockLight today, so the asymmetry is unreachable and cannot
	// be asserted without corrupting the global for the whole test binary. It is reported, not exercised.
}

// TestLightCloneAliasesGlobals pins that Clone deliberately does not copy light that points at the shared
// globals: cloneLight returns the global slice itself. The clone stays correct only because the setters copy
// on write, which is asserted below.
func TestLightCloneAliasesGlobals(t *testing.T) {
	c := newTestChunk()
	lightLayer(c, 0, ridSolid)
	lightFill(c, 0, 0)
	clone := c.Clone()

	t.Run("global light is aliased", func(t *testing.T) {
		orig, cl := c.SubChunk(100), clone.SubChunk(100)
		if &orig.skyLight[0] != fullLightPtr {
			t.Fatalf("original does not alias fullLight, test setup is wrong")
		}
		if &cl.skyLight[0] != fullLightPtr {
			t.Errorf("clone skylight was copied, want it to alias the fullLight global")
		}
		if &cl.blockLight[0] != noLightPtr {
			t.Errorf("clone block light was copied, want it to alias the noLight global")
		}
	})
	t.Run("calculated light is copied", func(t *testing.T) {
		// Sub chunk 4 holds the floor, so its skylight was calculated and no longer aliases a global.
		orig, cl := c.SubChunk(0), clone.SubChunk(0)
		if &orig.skyLight[0] == &cl.skyLight[0] {
			t.Errorf("calculated skylight is shared between chunk and clone, want an independent copy")
		}
		if got, want := cl.SkyLight(8, 1, 8), orig.SkyLight(8, 1, 8); got != want {
			t.Errorf("clone skylight = %d, want %d", got, want)
		}
	})
	t.Run("writing through the clone does not affect the original", func(t *testing.T) {
		// y=100 lands at local y=4 inside its sub chunk, so the setter and the reader must agree on that.
		clone.SubChunk(100).SetSkyLight(1, 4, 3, 4)
		if got := clone.SkyLight(1, 100, 3); got != 4 {
			t.Errorf("clone skylight after write = %d, want 4", got)
		}
		if got := c.SkyLight(1, 100, 3); got != 15 {
			t.Errorf("original skylight after writing through the clone = %d, want 15", got)
		}
	})
}

// lightDigest hashes the raw light nibbles of every sub chunk in order, skylight before block light. The sub
// chunk slices are read, never written, so aliased globals are safe to touch here.
func lightDigest(c *Chunk) string {
	h := sha256.New()
	for _, sub := range c.Sub() {
		h.Write(sub.skyLight)
		h.Write(sub.blockLight)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestLightFillGolden is the regression anchor for the execution-model rewrite: one fixed scenario, hashed
// whole. Lighting has no maps, no goroutines and a FIFO queue, so the byte image is reproducible.
//
// If this digest changes, the lighting result changed. Regenerate it only after deciding deliberately that
// the new result is the intended one, never by pasting whatever the failure printed.
func TestLightFillGolden(t *testing.T) {
	const want = "24a6bd4b49ab14c1760aa3da148b4fc737b5adf7c69c681bfd948f43a1c7abac"

	c := newTestChunk()
	lightLayer(c, 0, ridSolid) // floor across the whole chunk
	for x := range 8 {         // roof over the western half
		for z := range 16 {
			c.SetBlock(uint8(x), 10, uint8(z), 0, ridSolid)
		}
	}
	c.SetBlock(12, 5, 3, 0, ridLamp)  // emitter in the open half
	c.SetBlock(2, 3, 10, 0, ridTorch) // emitter under the roof
	c.SetBlock(5, 4, 5, 0, ridLeaves) // diffusing block next to the torch
	c.SetBlock(6, 4, 5, 0, ridGlass)  // transparent block behind the leaves
	c.SetBlock(9, 2, 9, 0, ridWater)  // second diffusing value
	lightFill(c, 0, 0)

	// A handful of readable anchors so a digest failure can be diagnosed without a hex diff.
	anchors := []struct {
		name       string
		x, y, z    int
		sky, block uint8
	}{
		// The lamp filters 15, so it is itself a height map entry: its own skylight is 0 while the block one
		// beside it still sees open sky.
		{"lamp", 12, 5, 3, 0, 15},
		{"next to the lamp", 13, 5, 3, 15, 14},
		// Under the roof, skylight arrives from the open column at x=8: 15 - (8-2) = 9.
		{"torch under the roof", 2, 3, 10, 9, 14},
		// The floor is solid, costing 16 to enter, so neither light type gets into it.
		{"floor", 8, 0, 8, 0, 0},
		{"far above", 8, 300, 8, 15, 0},
	}
	for _, a := range anchors {
		if got := lightSkyAt(c, a.x, a.y, a.z); got != a.sky {
			t.Errorf("%s: skylight = %d, want %d", a.name, got, a.sky)
		}
		if got := lightBlockAt(c, a.x, a.y, a.z); got != a.block {
			t.Errorf("%s: block light = %d, want %d", a.name, got, a.block)
		}
	}

	if got := lightDigest(c); got != want {
		t.Fatalf("light digest = %s, want %s", got, want)
	}
}
