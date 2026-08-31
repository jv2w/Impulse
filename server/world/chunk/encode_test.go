package chunk

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// encodeNetherRange is a second vertical range used to prove that nothing in the encoders assumes the
// overworld's 24 sub chunks. It yields (127>>4)+1 = 8 sub chunks.
var encodeNetherRange = cube.Range{0, 127}

// encodeCase describes one chunk shape that must survive a full encode/decode round trip unchanged.
type encodeCase struct {
	name string
	r    cube.Range
	// layers is the highest layer index that build writes to. Every layer up to it is compared.
	layers uint8
	build  func(c *Chunk)
}

// encodeCases is shared by the disk and the network round-trip tests so that both encodings are held to
// exactly the same value-preservation contract.
var encodeCases = []encodeCase{
	{
		name: "empty", r: overworldRange, layers: 0,
		build: func(*Chunk) {},
	},
	{
		name: "single block", r: overworldRange, layers: 0,
		build: func(c *Chunk) { c.SetBlock(3, 7, 11, 0, ridSolid) },
	},
	{
		name: "single block at range minimum", r: overworldRange, layers: 0,
		// y == r[0] lands in sub chunk 0, whose encoded v9 index byte is negative (-4). It is the case
		// most likely to break the int8 round trip of the sub chunk index.
		build: func(c *Chunk) { c.SetBlock(0, int16(overworldRange[0]), 0, 0, ridChest) },
	},
	{
		name: "single block at range maximum", r: overworldRange, layers: 0,
		build: func(c *Chunk) { c.SetBlock(15, int16(overworldRange[1]), 15, 0, ridLamp) },
	},
	{
		name: "one full sub chunk", r: overworldRange, layers: 0,
		build: func(c *Chunk) {
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(0); y < 16; y++ {
						c.SetBlock(x, y, z, 0, ridSolid)
					}
				}
			}
		},
	},
	{
		name: "two layers", r: overworldRange, layers: 1,
		build: func(c *Chunk) {
			// Water over a waterlogged-style second layer: layer 1 only exists for part of the column,
			// so the sub chunk has two storages of different palette sizes.
			for y := int16(60); y < 68; y++ {
				c.SetBlock(2, y, 2, 0, ridWater)
				c.SetBlock(2, y, 2, 1, ridLeaves)
			}
			c.SetBlock(5, 61, 5, 1, ridGlass)
		},
	},
	{
		name: "all sub chunks populated", r: overworldRange, layers: 0,
		build: func(c *Chunk) {
			rnd := rand.New(rand.NewSource(0x5eed))
			for i := range c.sub {
				base := c.SubY(int16(i))
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						for dy := int16(0); dy < 16; dy++ {
							c.SetBlock(x, base+dy, z, 0, uint32(rnd.Intn(len(testBlocks))))
						}
					}
				}
			}
		},
	},
	{
		name: "biomes only", r: overworldRange, layers: 0,
		build: func(c *Chunk) {
			rnd := rand.New(rand.NewSource(7))
			for i := range c.sub {
				base := c.SubY(int16(i))
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						for dy := int16(0); dy < 16; dy++ {
							c.SetBiome(x, base+dy, z, uint32(rnd.Intn(40)))
						}
					}
				}
			}
		},
	},
	{
		name: "blocks and biomes", r: overworldRange, layers: 1,
		build: func(c *Chunk) {
			rnd := rand.New(rand.NewSource(1234))
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(-64); y < 32; y++ {
						c.SetBlock(x, y, z, 0, uint32(rnd.Intn(len(testBlocks))))
						if rnd.Intn(4) == 0 {
							c.SetBlock(x, y, z, 1, ridWater)
						}
						c.SetBiome(x, y, z, uint32(rnd.Intn(9))+1)
					}
				}
			}
		},
	},
	{
		name: "nether range", r: encodeNetherRange, layers: 0,
		build: func(c *Chunk) {
			for y := int16(0); y < 128; y += 7 {
				c.SetBlock(uint8(y%16), y, uint8((y*3)%16), 0, ridTorch)
				c.SetBiome(uint8(y%16), y, uint8((y*3)%16), uint32(y))
			}
		},
	},
}

// encodeBuild returns a chunk over r with build applied.
func encodeBuild(c encodeCase) *Chunk {
	ch := New(newTestRegistry(), c.r)
	c.build(ch)
	return ch
}

// encodeAssertSameBlocks compares every block on layers 0..layers and every biome of the two chunks. It is
// the actual round-trip contract: the encoders must preserve values at every single coordinate.
func encodeAssertSameBlocks(t *testing.T, want, got *Chunk, layers uint8) {
	t.Helper()
	if got.Range() != want.Range() {
		t.Fatalf("range = %v, want %v", got.Range(), want.Range())
	}
	if len(got.sub) != len(want.sub) {
		t.Fatalf("sub chunk count = %d, want %d", len(got.sub), len(want.sub))
	}
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(want.Range()[0]); y <= int16(want.Range()[1]); y++ {
				for l := uint8(0); l <= layers; l++ {
					if a, b := got.Block(x, y, z, l), want.Block(x, y, z, l); a != b {
						t.Fatalf("Block(%d, %d, %d, layer %d) = %d, want %d", x, y, z, l, a, b)
					}
				}
				if a, b := got.Biome(x, y, z), want.Biome(x, y, z); a != b {
					t.Fatalf("Biome(%d, %d, %d) = %d, want %d", x, y, z, a, b)
				}
			}
		}
	}
}

// encodeNetworkPayload lays out what NetworkDecode expects: every sub chunk's bytes concatenated in index
// order, immediately followed by the biome bytes. Encode returns them separately, so callers of the network
// path have to join them themselves.
func encodeNetworkPayload(d SerialisedData) []byte {
	var buf bytes.Buffer
	for _, sub := range d.SubChunks {
		buf.Write(sub)
	}
	buf.Write(d.Biomes)
	return buf.Bytes()
}

func TestEncodeConstantsAreWireFormat(t *testing.T) {
	// Both values are written into every serialised chunk, so changing them silently invalidates existing
	// worlds and packets. They are pinned here as a wire-format contract, not as an implementation detail.
	if SubChunkVersion != 9 {
		t.Errorf("SubChunkVersion = %d, want 9", SubChunkVersion)
	}
	if CurrentBlockVersion != 18040335 {
		t.Errorf("CurrentBlockVersion = %d, want 18040335", CurrentBlockVersion)
	}
}

func TestRoundTripDisk(t *testing.T) {
	for _, c := range encodeCases {
		t.Run(c.name, func(t *testing.T) {
			want := encodeBuild(c)
			data := Encode(want, DiskEncoding)
			got, err := DiskDecode(newTestRegistry(), data, c.r)
			assertNoError(t, err, "DiskDecode(%q)", c.name)
			encodeAssertSameBlocks(t, want, got, c.layers)
		})
	}
}

func TestRoundTripNetwork(t *testing.T) {
	for _, c := range encodeCases {
		t.Run(c.name, func(t *testing.T) {
			want := encodeBuild(c)
			data := Encode(want, NetworkEncoding)
			got, err := NetworkDecode(newTestRegistry(), encodeNetworkPayload(data), len(data.SubChunks), c.r)
			assertNoError(t, err, "NetworkDecode(%q)", c.name)
			encodeAssertSameBlocks(t, want, got, c.layers)
		})
	}
}

func TestRoundTripDiskBlockPaletteSizeClasses(t *testing.T) {
	// The palette size is derived from the number of unique values, and the encoded indices length is
	// derived from the size. Sizes 3, 5 and 6 are padded (one extra uint32), so they are the ones that can
	// desynchronise encoder and decoder. The test registry only holds 9 blocks, so block palettes can reach
	// sizes 0 through 4; the padded sizes 5 and 6 are covered by the biome test below.
	sizes := []struct {
		distinct int // number of non-air runtime IDs written into the sub chunk
		bits     uint16
	}{
		{distinct: 0, bits: 0}, // palette {air}
		{distinct: 1, bits: 1}, // palette {air, x}
		{distinct: 3, bits: 2}, // 4 values
		{distinct: 7, bits: 3}, // 8 values, padded size
		{distinct: 8, bits: 4}, // 9 values, the whole test registry
	}
	c := newTestChunk()
	for i, s := range sizes {
		base := c.SubY(int16(i))
		// Layer 0 must exist even when nothing is written, otherwise the sub chunk encodes zero storages
		// instead of one 0-bit storage.
		c.SubChunk(base).Layer(0)
		for j := 0; j < s.distinct; j++ {
			c.SetBlock(uint8(j), base+int16(j), uint8(j), 0, uint32(j+1))
		}
	}
	got, err := DiskDecode(newTestRegistry(), Encode(c, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode")
	for i, s := range sizes {
		storages := got.sub[i].storages
		if len(storages) != 1 {
			t.Fatalf("sub %d: storage count = %d, want 1", i, len(storages))
		}
		if bits := storages[0].bitsPerIndex; bits != s.bits {
			t.Errorf("sub %d: bitsPerIndex = %d, want %d", i, bits, s.bits)
		}
		if l := storages[0].palette.Len(); l != s.distinct+1 {
			t.Errorf("sub %d: palette length = %d, want %d", i, l, s.distinct+1)
		}
	}
	encodeAssertSameBlocks(t, c, got, 0)
}

func TestRoundTripDiskBiomePaletteSizeClasses(t *testing.T) {
	// Biome values are arbitrary uint32s, so they can drive the palette into every size class, including
	// the padded sizes 5 and 6 and the 16-bit class. Sub chunk i gets distinct[i] distinct non-zero values
	// on top of the 0 the storage starts with, hence a palette of distinct[i]+1 entries.
	sizes := []struct {
		distinct int
		bits     uint16
	}{
		// 17 values, a padded size.
		{distinct: 16, bits: 5},
		// 33 values, a padded size.
		{distinct: 32, bits: 6},
		// 65 values.
		{distinct: 64, bits: 8},
		// 257 values: there is no 9-bit class, so the size jumps straight to 16.
		{distinct: 256, bits: 16},
	}
	c := newTestChunk()
	for i, s := range sizes {
		base := c.SubY(int16(i))
		n := 0
		for x := uint8(0); x < 16 && n < s.distinct; x++ {
			for z := uint8(0); z < 16 && n < s.distinct; z++ {
				c.SetBiome(x, base, z, uint32(n+1))
				n++
			}
		}
	}
	got, err := DiskDecode(newTestRegistry(), Encode(c, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode")
	for i, s := range sizes {
		if bits := got.biomes[i].bitsPerIndex; bits != s.bits {
			t.Errorf("biome %d: bitsPerIndex = %d, want %d", i, bits, s.bits)
		}
		if l := got.biomes[i].palette.Len(); l != s.distinct+1 {
			t.Errorf("biome %d: palette length = %d, want %d", i, l, s.distinct+1)
		}
	}
	encodeAssertSameBlocks(t, c, got, 0)
}

func TestDecodeNetworkErrors(t *testing.T) {
	// The v9 sub chunk header is {version, storageCount, absoluteY}. An absolute Y of 200 decodes as
	// int8(200) = -56, which the decoder translates to index -56 - (-4) = -52, i.e. uint8 204.
	badIndex := []byte{SubChunkVersion, 0, 200}

	// These are the current error strings; they are not sentinels, so a caller can only match on text.
	cases := []struct {
		name    string
		data    []byte
		count   int
		wantSub string
	}{
		{name: "count above sub chunk count", data: nil, count: 25, wantSub: "invalid sub-chunk count 25"},
		{name: "negative count", data: nil, count: -1, wantSub: "invalid sub-chunk count -1"},
		{name: "translated index out of range", data: badIndex, count: 1, wantSub: "invalid sub-chunk index 204"},
		{name: "truncated header", data: []byte{}, count: 1, wantSub: "error reading version"},
		{name: "unknown sub chunk version", data: []byte{7, 0, 0}, count: 1, wantSub: "unknown sub chunk version 7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NetworkDecode(newTestRegistry(), c.data, c.count, overworldRange)
			if err == nil {
				t.Fatalf("NetworkDecode = %v, nil, want error", got)
			}
			if got != nil {
				t.Errorf("NetworkDecode returned a non-nil chunk alongside the error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.wantSub)
			}
		})
	}
}

func TestDecodeNetworkCountBelowSubChunkCount(t *testing.T) {
	// A count lower than the number of sub chunks is legal: the remaining sub chunks stay empty, but the
	// biome section is still read for every sub chunk in the range, so the payload must carry all 24.
	c := newTestChunk()
	c.SetBlock(1, 2, 3, 0, ridSolid)   // sub chunk 4
	c.SetBlock(1, 200, 3, 0, ridSolid) // sub chunk 16, beyond the count below
	c.SetBiome(1, 200, 3, 12)
	data := Encode(c, NetworkEncoding)

	var buf bytes.Buffer
	for _, sub := range data.SubChunks[:8] {
		buf.Write(sub)
	}
	buf.Write(data.Biomes)

	got, err := NetworkDecode(newTestRegistry(), buf.Bytes(), 8, overworldRange)
	assertNoError(t, err, "NetworkDecode")
	if b := got.Block(1, 2, 3, 0); b != ridSolid {
		t.Errorf("Block(1, 2, 3) = %d, want %d", b, ridSolid)
	}
	// Sub chunk 16 was never sent, so its block is gone even though its biome survived.
	if b := got.Block(1, 200, 3, 0); b != ridAir {
		t.Errorf("Block(1, 200, 3) = %d, want %d (sub chunk not sent)", b, ridAir)
	}
	if b := got.Biome(1, 200, 3); b != 12 {
		t.Errorf("Biome(1, 200, 3) = %d, want 12", b)
	}
}

func TestDecodeLightIsNeverEncoded(t *testing.T) {
	// Light is not part of either serialised form. A decoded chunk's sub chunks therefore have nil light
	// slices, and every light accessor indexes those slices unconditionally. Callers must run the light
	// filler before reading light off a decoded chunk; this is the current contract, pinned as-is.
	c := newTestChunk()
	c.SetBlock(0, 0, 0, 0, ridLamp)
	got, err := DiskDecode(newTestRegistry(), Encode(c, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode")

	for i, sub := range got.sub {
		if sub.blockLight != nil {
			t.Fatalf("sub %d: blockLight = %v, want nil", i, sub.blockLight)
		}
		if sub.skyLight != nil {
			t.Fatalf("sub %d: skyLight = %v, want nil", i, sub.skyLight)
		}
	}

	accessors := []struct {
		name string
		call func()
	}{
		{name: "Chunk.Light", call: func() { got.Light(0, 0, 0) }},
		{name: "Chunk.SkyLight", call: func() { got.SkyLight(0, 0, 0) }},
		{name: "SubChunk.BlockLight", call: func() { got.sub[0].BlockLight(0, 0, 0) }},
	}
	for _, a := range accessors {
		t.Run(a.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s on a decoded chunk did not panic", a.name)
				}
				err, ok := r.(error)
				if !ok {
					t.Fatalf("recovered %v (%T), want an error", r, r)
				}
				if !strings.Contains(err.Error(), "index out of range") {
					t.Errorf("panic = %q, want it to mention an out of range index", err.Error())
				}
			}()
			a.call()
		})
	}
}

func TestDecodeHeightMapIsNeverEncoded(t *testing.T) {
	// The height map is derived data and is not serialised, so a decoded chunk always comes back marked
	// dirty and recomputes on the first HeightMap call.
	c := newTestChunk()
	c.SetBlock(4, 40, 4, 0, ridSolid)
	c.HeightMap()
	if c.recalculateHeightMap {
		t.Fatalf("source chunk still marked dirty after HeightMap()")
	}

	got, err := DiskDecode(newTestRegistry(), Encode(c, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode")
	if !got.recalculateHeightMap {
		t.Errorf("recalculateHeightMap = false, want true on a freshly decoded chunk")
	}
	// The recomputed map still matches, because it is a pure function of the blocks that did survive.
	if a, b := got.HeightMap().At(4, 4), c.HeightMap().At(4, 4); a != b {
		t.Errorf("recomputed HeightMap.At(4, 4) = %d, want %d", a, b)
	}
}

func TestEncodeDoesNotDropEmptySubChunks(t *testing.T) {
	// Encode emits one entry per sub chunk index unconditionally: even a completely empty sub chunk gets a
	// three byte v9 header with a storage count of 0. SerialisedData documents empty sub chunks as "an empty
	// slice (or technically, nil)", but Encode never produces one.
	c := newTestChunk()
	data := Encode(c, DiskEncoding)
	if len(data.SubChunks) != 24 {
		t.Fatalf("len(SubChunks) = %d, want 24", len(data.SubChunks))
	}
	for i, sub := range data.SubChunks {
		// The third byte is the absolute sub chunk Y, i.e. index + (-64 >> 4) = index - 4, truncated to a
		// byte, so index 0 encodes as 252.
		want := []byte{SubChunkVersion, 0, byte(int8(i - 4))}
		if !bytes.Equal(sub, want) {
			t.Errorf("SubChunks[%d] = %v, want %v", i, sub, want)
		}
	}
}

func TestRoundTripDiskEmptySubChunkLosesNilStorages(t *testing.T) {
	// A round trip turns the nil storages slice of an untouched sub chunk into a non-nil, zero length one.
	// Value-wise nothing changes, so Equals and Empty still hold, but the identity of the slice does not
	// survive. Anything comparing storages == nil will see a different answer after a round trip.
	c := newTestChunk()
	if c.sub[0].storages != nil {
		t.Fatalf("fresh sub chunk storages = %v, want nil", c.sub[0].storages)
	}
	got, err := DiskDecode(newTestRegistry(), Encode(c, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode")

	if got.sub[0].storages == nil {
		t.Errorf("decoded sub chunk storages = nil, want a non-nil empty slice")
	}
	if l := len(got.sub[0].storages); l != 0 {
		t.Errorf("len(decoded storages) = %d, want 0", l)
	}
	if !got.sub[0].Empty() {
		t.Errorf("decoded sub chunk Empty() = false, want true")
	}
	if !got.Equals(c) {
		t.Errorf("decoded chunk Equals(original) = false, want true")
	}
}

func TestEncodeCompactChangesBytesButNotBlocks(t *testing.T) {
	// Encode does not normalise storages. A layer left holding nothing but air is written out in full and
	// decoded back, so Encode(c) and Encode(compacted c) differ in bytes while decoding to the same blocks.
	build := func() *Chunk {
		c := newTestChunk()
		c.SetBlock(1, 20, 1, 0, ridSolid)
		c.SetBlock(1, 20, 1, 1, ridWater)
		c.SetBlock(1, 20, 1, 1, ridAir) // layer 1 now exists but holds only air
		return c
	}
	plain, compacted := build(), build()
	compacted.Compact()

	const ind = 5 // sub chunk holding y == 20 for the overworld range: (20 - -64) >> 4
	plainSub := EncodeSubChunk(plain, DiskEncoding, ind)
	compactedSub := EncodeSubChunk(compacted, DiskEncoding, ind)

	if plainSub[1] != 2 {
		t.Errorf("uncompacted storage count = %d, want 2", plainSub[1])
	}
	if compactedSub[1] != 1 {
		t.Errorf("compacted storage count = %d, want 1", compactedSub[1])
	}
	if len(plainSub) <= len(compactedSub) {
		t.Errorf("uncompacted sub chunk is %d bytes, compacted %d: want the uncompacted one to be larger", len(plainSub), len(compactedSub))
	}
	if bytes.Equal(plainSub, compactedSub) {
		t.Errorf("compacting did not change the encoded bytes")
	}

	gotPlain, err := DiskDecode(newTestRegistry(), Encode(plain, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode(plain)")
	gotCompacted, err := DiskDecode(newTestRegistry(), Encode(compacted, DiskEncoding), overworldRange)
	assertNoError(t, err, "DiskDecode(compacted)")
	encodeAssertSameBlocks(t, gotPlain, gotCompacted, 1)
	// The air-only layer itself does survive the round trip when Compact was not called.
	if l := len(gotPlain.sub[ind].storages); l != 2 {
		t.Errorf("uncompacted round trip storage count = %d, want 2", l)
	}
	if l := len(gotCompacted.sub[ind].storages); l != 1 {
		t.Errorf("compacted round trip storage count = %d, want 1", l)
	}
}

// encodeDedupBiomes replaces the biome storages at indices a and b of c with two distinct but value-equal
// storages, chosen so that PalettedStorage.Equal returns true for them: both need a non-empty indices slice
// and a first palette value that is not 0, which is why the palette starts at 5 rather than at 0. Biome
// storages built through Chunk.SetBiome can never satisfy that, as they always keep 0 at palette index 0.
func encodeDedupBiomes(c *Chunk, a, b int) {
	build := func() *PalettedStorage {
		// One bit per index over 4096 cells is 128 uint32s. Alternating bits make the storage non-uniform.
		indices := make([]uint32, 128)
		for i := range indices {
			indices[i] = 0xaaaaaaaa
		}
		return newPalettedStorage(indices, newPalette(1, []uint32{5, 6}))
	}
	c.biomes[a], c.biomes[b] = build(), build()
}

func TestRoundTripDiskBiomeDedupCopiesStorages(t *testing.T) {
	// encodePalettedStorage collapses a biome storage equal to its predecessor into the single byte
	// 0x7f<<1|network. Restoring that flag must give the sub chunk its own storage: sharing the previous
	// pointer makes one sub chunk's SetBiome silently rewrite the others.
	c := newTestChunk()
	encodeDedupBiomes(c, 0, 1)

	data := Encode(c, DiskEncoding)
	// The deduplicated storage costs exactly one byte: 0x7f<<1|0 == 0xfe for the disk encoding.
	if !bytes.Contains(data.Biomes, []byte{0xfe}) {
		t.Fatalf("biome data does not contain the 0xfe same-as-previous flag: dedup was not reached")
	}

	got, err := DiskDecode(newTestRegistry(), data, overworldRange)
	assertNoError(t, err, "DiskDecode")
	encodeAssertSameBlocks(t, c, got, 0)

	if got.biomes[0] == got.biomes[1] {
		t.Fatalf("decoded biome storages 0 and 1 are the same pointer, want distinct ones")
	}
	// Cell (0, 1, 0) of a storage sits at bit 1 of the first uint32, which is set in 0xaaaaaaaa, so it
	// holds palette entry 1, the value 6. y == -63 is that cell in sub chunk 0, y == -47 in sub chunk 1.
	if before := got.Biome(0, -47, 0); before != 6 {
		t.Fatalf("Biome(0, -47, 0) = %d, want 6", before)
	}
	got.SetBiome(0, -63, 0, 5)
	if after := got.Biome(0, -47, 0); after != 6 {
		t.Errorf("Biome(0, -47, 0) = %d after writing only to sub chunk 0, want it left at 6", after)
	}
	// The palettes must be independent too, not just the indices: SetBiome to a value the palette does not
	// hold appends to it.
	got.SetBiome(0, -63, 0, 40)
	if after := got.Biome(0, -47, 0); after != 6 {
		t.Errorf("Biome(0, -47, 0) = %d after a palette-growing write to sub chunk 0, want it left at 6", after)
	}
}

func TestRoundTripNetworkBiomeDedupCopiesStorages(t *testing.T) {
	// The network path restores the same flag as the disk path; only the byte differs (0x7f<<1|1).
	c := newTestChunk()
	encodeDedupBiomes(c, 3, 4)

	data := Encode(c, NetworkEncoding)
	if !bytes.Contains(data.Biomes, []byte{0xff}) {
		t.Fatalf("biome data does not contain the 0xff same-as-previous flag: dedup was not reached")
	}
	got, err := NetworkDecode(newTestRegistry(), encodeNetworkPayload(data), len(data.SubChunks), overworldRange)
	assertNoError(t, err, "NetworkDecode")
	if got.biomes[3] == got.biomes[4] {
		t.Fatalf("decoded biome storages 3 and 4 are the same pointer, want distinct ones")
	}
	encodeAssertSameBlocks(t, c, got, 0)

	// Sub chunk 3 covers y -16..-1, sub chunk 4 y 0..15; cell (0, 1, 0) holds the value 6 in both.
	if before := got.Biome(0, 1, 0); before != 6 {
		t.Fatalf("Biome(0, 1, 0) = %d, want 6", before)
	}
	got.SetBiome(0, -15, 0, 5)
	if after := got.Biome(0, 1, 0); after != 6 {
		t.Errorf("Biome(0, 1, 0) = %d after writing only to sub chunk 3, want it left at 6", after)
	}
}

func TestDecodeFirstBiomeStoragePointingToPrevious(t *testing.T) {
	// A biome section that opens with the same-as-previous flag has nothing to inherit from. Both decoders
	// reject it rather than defaulting to an empty storage.
	t.Run("disk", func(t *testing.T) {
		_, err := DiskDecode(newTestRegistry(), SerialisedData{Biomes: []byte{0xfe}}, overworldRange)
		if err == nil || !strings.Contains(err.Error(), "first biome storage pointed to previous one") {
			t.Fatalf("err = %v, want it to mention the first biome storage", err)
		}
	})
	t.Run("network", func(t *testing.T) {
		_, err := NetworkDecode(newTestRegistry(), []byte{0xff}, 0, overworldRange)
		if err == nil || !strings.Contains(err.Error(), "first biome storage pointed to previous one") {
			t.Fatalf("err = %v, want it to mention the first biome storage", err)
		}
	})
}

func TestDecodeDiskWithoutBiomesLeavesDefaults(t *testing.T) {
	// decodeBiomes skips the whole biome section when the buffer is empty, which is the only way a decoded
	// chunk keeps the all-zero storages New installed. An empty Biomes slice is not an error.
	got, err := DiskDecode(newTestRegistry(), SerialisedData{}, overworldRange)
	assertNoError(t, err, "DiskDecode")
	if b := got.Biome(0, 0, 0); b != 0 {
		t.Errorf("Biome(0, 0, 0) = %d, want 0", b)
	}
	if len(got.sub) != 24 {
		t.Errorf("sub chunk count = %d, want 24", len(got.sub))
	}
}

func TestEncodeSubChunkHeader(t *testing.T) {
	c := newTestChunk()
	c.SetBlock(0, 20, 0, 0, ridSolid)
	const ind = 5

	cases := []struct {
		name     string
		e        Encoding
		wantFlag byte
	}{
		// The storage flag byte is bitsPerIndex<<1 | network. One bit per index for a two entry palette.
		{name: "disk", e: DiskEncoding, wantFlag: 1<<1 | 0},
		{name: "network", e: NetworkEncoding, wantFlag: 1<<1 | 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := EncodeSubChunk(c, tc.e, ind)
			if sub[0] != SubChunkVersion {
				t.Errorf("version byte = %d, want %d", sub[0], SubChunkVersion)
			}
			if sub[1] != 1 {
				t.Errorf("storage count = %d, want 1", sub[1])
			}
			// Absolute Y of sub chunk 5 in the overworld range: 5 + (-64 >> 4) = 1.
			if sub[2] != 1 {
				t.Errorf("index byte = %d, want 1", sub[2])
			}
			if sub[3] != tc.wantFlag {
				t.Errorf("storage flag = %#x, want %#x", sub[3], tc.wantFlag)
			}
			// 3 header bytes plus the flag plus 128 uint32s of indices, then the palette.
			if fixed := 4 + 128*4; len(sub) <= fixed {
				t.Errorf("len = %d, want more than %d (indices plus a palette)", len(sub), fixed)
			}
		})
	}
}

func TestEncodeBiomesOfEmptyChunk(t *testing.T) {
	// Every biome storage of a fresh chunk is 0 bits per index with the single palette value 0. Dedup never
	// applies, because Equal rejects storages whose first palette value is 0, so all 24 are written out.
	c := newTestChunk()

	t.Run("disk", func(t *testing.T) {
		// Per sub chunk: the flag byte 0<<1|0, no indices, no palette count (size 0), one little endian
		// uint32 value of 0.
		got := EncodeBiomes(c, DiskEncoding)
		want := bytes.Repeat([]byte{0x00, 0, 0, 0, 0}, 24)
		if !bytes.Equal(got, want) {
			t.Errorf("EncodeBiomes = %v, want %v", got, want)
		}
	})
	t.Run("network", func(t *testing.T) {
		// Per sub chunk: the flag byte 0<<1|1, then a single varint32 of 0.
		got := EncodeBiomes(c, NetworkEncoding)
		want := bytes.Repeat([]byte{0x01, 0x00}, 24)
		if !bytes.Equal(got, want) {
			t.Errorf("EncodeBiomes = %v, want %v", got, want)
		}
	})
}

func TestDecodeBlockState(t *testing.T) {
	bpe := BlockPaletteEncoding{Blocks: newTestRegistry()}

	t.Run("round trip through EncodeBlockState", func(t *testing.T) {
		for rid := uint32(0); rid < uint32(len(testBlocks)); rid++ {
			entry := bpe.EncodeBlockState(rid)
			if entry.Version != CurrentBlockVersion {
				t.Fatalf("rid %d: version = %d, want %d", rid, entry.Version, CurrentBlockVersion)
			}
			if entry.Name != testBlocks[rid].name {
				t.Fatalf("rid %d: name = %q, want %q", rid, entry.Name, testBlocks[rid].name)
			}
			got, err := bpe.DecodeBlockState(map[string]any{
				"name": entry.Name, "states": entry.State, "version": entry.Version,
			})
			assertNoError(t, err, "DecodeBlockState(%q)", entry.Name)
			if got != rid {
				t.Fatalf("DecodeBlockState(%q) = %d, want %d", entry.Name, got, rid)
			}
		}
	})

	// These are the current error strings. None of them are sentinels, so callers can only match on text.
	cases := []struct {
		name    string
		entry   map[string]any
		wantSub string
	}{
		{
			name:    "unknown state",
			entry:   map[string]any{"name": "impulse:not_a_block", "states": map[string]any{}, "version": CurrentBlockVersion},
			wantSub: "cannot get runtime ID of block state impulse:not_a_block",
		},
		{
			name: "known name with unknown properties",
			// The registry matches on name and properties together, so a wrong property value misses.
			entry:   map[string]any{"name": "impulse:torch", "states": map[string]any{"facing": int32(9)}, "version": CurrentBlockVersion},
			wantSub: "cannot get runtime ID of block state impulse:torch",
		},
		{
			name:    "states field is not a compound",
			entry:   map[string]any{"name": "impulse:solid", "states": "nonsense", "version": CurrentBlockVersion},
			wantSub: "invalid state in block entry",
		},
		{
			name: "legacy entry without a mapping",
			// version < 17694723 selects the pre-1.13 path, which looks the name up in legacy_states.nbt.
			entry:   map[string]any{"name": "minecraft:not_a_legacy_block", "val": int16(3), "version": int32(0)},
			wantSub: "cannot find mapping for legacy block entry: minecraft:not_a_legacy_block, 3",
		},
		{
			name: "legacy entry with a mapping the registry does not hold",
			// minecraft:wool meta 1 upgrades to minecraft:orange_wool. The test registry holds no
			// minecraft:* block, so the error naming the upgraded block proves the legacy path ran.
			entry:   map[string]any{"name": "minecraft:wool", "val": int16(1), "version": int32(0)},
			wantSub: "cannot get runtime ID of block state minecraft:orange_wool",
		},
		{
			name: "missing version selects the legacy path",
			// A missing version field decodes to the zero int32, which is below 17694723, so the same
			// legacy path runs without any version field being present at all.
			entry:   map[string]any{"name": "minecraft:wool", "val": int16(4)},
			wantSub: "cannot get runtime ID of block state minecraft:yellow_wool",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := bpe.DecodeBlockState(c.entry)
			if err == nil {
				t.Fatalf("DecodeBlockState = %d, nil, want error", got)
			}
			if got != 0 {
				t.Errorf("DecodeBlockState returned %d alongside the error, want 0", got)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.wantSub)
			}
		})
	}

	t.Run("missing states field defaults to empty", func(t *testing.T) {
		// A post-1.13 entry without a states field is treated as having no properties rather than
		// rejected, to survive broken world conversions.
		got, err := bpe.DecodeBlockState(map[string]any{"name": "impulse:solid", "version": CurrentBlockVersion})
		assertNoError(t, err, "DecodeBlockState")
		if got != ridSolid {
			t.Errorf("DecodeBlockState = %d, want %d", got, ridSolid)
		}
	})
}

func TestDecodePaletteDiskZeroCount(t *testing.T) {
	// The disk palette decoder returns a non-nil palette together with a non-nil error when the stream
	// declares zero entries. A caller that only checks the palette against nil will happily use an empty,
	// unusable palette, so the error must not be ignored.
	buf := bytes.NewBuffer([]byte{0, 0, 0, 0}) // a little endian uint32 count of 0
	p, err := DiskEncoding.decodePalette(buf, 1, BiomePaletteEncoding)
	if err == nil {
		t.Fatalf("decodePalette err = nil, want an error")
	}
	if !strings.Contains(err.Error(), "invalid palette entry count: found 0") {
		t.Errorf("error = %q, want it to mention the entry count", err.Error())
	}
	if p == nil {
		t.Fatalf("decodePalette palette = nil, want a non-nil palette alongside the error")
	}
	if p.Len() != 0 {
		t.Errorf("palette length = %d, want 0", p.Len())
	}
}

func TestDecodePaletteNetworkZeroCount(t *testing.T) {
	// The network decoder rejects the same input earlier and returns a nil palette, so the two encodings
	// disagree on what they hand back with the error.
	buf := bytes.NewBuffer([]byte{0x00}) // varint32 of 0
	p, err := NetworkEncoding.decodePalette(buf, 1, BiomePaletteEncoding)
	if err == nil {
		t.Fatalf("decodePalette err = nil, want an error")
	}
	if !strings.Contains(err.Error(), "invalid palette entry count 0") {
		t.Errorf("error = %q, want it to mention the entry count", err.Error())
	}
	if p != nil {
		t.Errorf("decodePalette palette = %v, want nil", p)
	}
}

func TestDecodePaletteMissesValueZero(t *testing.T) {
	// Palette.last must start at math.MaxUint32, or Index takes its fast path for the value 0 and answers
	// lastIndex (0) even when 0 is not in the palette. Air is runtime ID 0, so both encodings have to agree
	// on -1 here. Neither round-trip test above catches this, because they only read.
	//
	// Network palettes are zigzag varint32s, so 2, 5 and 6 encode as 0x04, 0x0a and 0x0c.
	network, err := NetworkEncoding.decodePalette(bytes.NewBuffer([]byte{0x04, 0x0a, 0x0c}), 1, BiomePaletteEncoding)
	assertNoError(t, err, "network decodePalette")
	if got, want := network.values, []uint32{5, 6}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("network palette values = %v, want %v", got, want)
	}
	if got := network.Index(0); got != -1 {
		t.Errorf("network palette Index(0) = %d, want -1", got)
	}

	disk, err := DiskEncoding.decodePalette(bytes.NewBuffer([]byte{2, 0, 0, 0, 5, 0, 0, 0, 6, 0, 0, 0}), 1, BiomePaletteEncoding)
	assertNoError(t, err, "disk decodePalette")
	if got := disk.Index(0); got != -1 {
		t.Errorf("disk palette Index(0) = %d, want -1", got)
	}
}

func TestDecodePaletteNetworkStorageAcceptsValueZero(t *testing.T) {
	// The consequence of a stale last cache: writing air into a network-decoded storage resolves to palette
	// index 0 and silently stores whatever value that index happens to hold.
	data := append([]byte{1<<1 | 1}, make([]byte, 128*4)...)
	data = append(data, 0x04, 0x0a, 0x0c) // a palette of 5 and 6, neither of which is 0
	s, err := decodePalettedStorage(bytes.NewBuffer(data), NetworkEncoding, BiomePaletteEncoding)
	assertNoError(t, err, "decodePalettedStorage")
	if got := s.At(0, 0, 0); got != 5 {
		t.Fatalf("At(0, 0, 0) = %d, want 5", got)
	}
	s.Set(0, 0, 0, 0)
	if got := s.At(0, 0, 0); got != 0 {
		t.Errorf("At(0, 0, 0) = %d after Set to 0, want 0", got)
	}
}

func TestDecodePalettedStorageSameAsPrevious(t *testing.T) {
	// The 0x7f size flag means "identical to the previous storage". decodePalettedStorage signals it by
	// returning (nil, nil); the two nils are not an error and every caller has to special case them.
	for _, tc := range []struct {
		name string
		e    Encoding
		flag byte
	}{
		{name: "disk", e: DiskEncoding, flag: 0x7f<<1 | 0},
		{name: "network", e: NetworkEncoding, flag: 0x7f<<1 | 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBuffer([]byte{tc.flag, 0xde, 0xad})
			s, err := decodePalettedStorage(buf, tc.e, BiomePaletteEncoding)
			if err != nil {
				t.Fatalf("decodePalettedStorage err = %v, want nil", err)
			}
			if s != nil {
				t.Fatalf("decodePalettedStorage storage = %v, want nil", s)
			}
			// Only the flag byte is consumed; the rest is left for the next storage.
			if buf.Len() != 2 {
				t.Errorf("remaining bytes = %d, want 2", buf.Len())
			}
		})
	}
}

func TestDecodePalettedStorageErrors(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		wantSub string
	}{
		{name: "empty buffer", data: nil, wantSub: "error reading block size"},
		// A size byte of 0xfc >> 1 == 0x7e is far past the largest size the format defines.
		{name: "size too large", data: []byte{0x7e << 1}, wantSub: "invalid palette size"},
		// 1 bit per index needs 128 uint32s, i.e. 512 bytes of indices.
		{name: "truncated indices", data: append([]byte{1 << 1}, make([]byte, 511)...), wantSub: "not enough block data present: expected 512 bytes, got 511"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := decodePalettedStorage(bytes.NewBuffer(c.data), DiskEncoding, BiomePaletteEncoding)
			if err == nil {
				t.Fatalf("decodePalettedStorage = %v, nil, want error", s)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.wantSub)
			}
		})
	}
}

func TestDecodePalettedStorageRejectsNonCanonicalSizes(t *testing.T) {
	// Only the sizes listed in `sizes` have a defined bit layout. For the others, uint32s reports a count
	// that paletteIndex and setPaletteIndex then read and write past: both index the storage through unsafe
	// pointer arithmetic, so an overrun is neither a panic nor a race report, just silent heap corruption.
	// The decoder is the only place an untrusted size byte can still be turned away.
	//
	// Each payload below is complete: a size byte, a full set of indices and a one entry palette. The size
	// check is the only thing that can reject them. Nothing here reads from a decoded storage on purpose,
	// since that read is the out of bounds access being prevented.
	for _, size := range []paletteSize{7, 9, 10, 15, 17, 32} {
		t.Run(fmt.Sprintf("size %d", size), func(t *testing.T) {
			data := append([]byte{byte(size) << 1}, make([]byte, size.uint32s()*4)...)
			data = append(data, 1, 0, 0, 0, 0, 0, 0, 0) // one palette entry, holding the value 0
			if _, err := decodePalettedStorage(bytes.NewBuffer(data), DiskEncoding, BiomePaletteEncoding); err != nil {
				if !strings.Contains(err.Error(), "invalid palette size") {
					t.Errorf("error = %q, want it to mention the palette size", err.Error())
				}
				return
			}
			t.Fatalf("decodePalettedStorage err = nil, want the size to be rejected")
		})
	}
}

func TestDecodePalettedStorageAcceptsEveryCanonicalSize(t *testing.T) {
	// The counterpart of the rejection above: every size the encoder can emit must still decode.
	for _, size := range sizes {
		t.Run(fmt.Sprintf("size %d", size), func(t *testing.T) {
			data := append([]byte{byte(size) << 1}, make([]byte, size.uint32s()*4)...)
			if size != 0 {
				data = append(data, 1, 0, 0, 0)
			}
			data = append(data, 0, 0, 0, 0)
			s, err := decodePalettedStorage(bytes.NewBuffer(data), DiskEncoding, BiomePaletteEncoding)
			assertNoError(t, err, "decodePalettedStorage(size=%d)", size)
			if got := s.At(0, 0, 0); got != 0 {
				t.Errorf("At(0, 0, 0) = %d, want 0", got)
			}
		})
	}
}

func TestDecodePalettedStorageReturnsStorageWithPaletteError(t *testing.T) {
	// decodePalettedStorage passes the palette decoder's error straight through but still wraps whatever
	// palette came back into a storage. With the disk encoding's zero count path that yields a non-nil
	// storage with an empty palette alongside a non-nil error: reading from it would panic.
	data := append([]byte{1 << 1}, make([]byte, 512)...)
	data = append(data, 0, 0, 0, 0) // palette entry count of 0
	s, err := decodePalettedStorage(bytes.NewBuffer(data), DiskEncoding, BiomePaletteEncoding)
	if err == nil {
		t.Fatalf("decodePalettedStorage err = nil, want an error")
	}
	if s == nil {
		t.Fatalf("decodePalettedStorage storage = nil, want a non-nil storage alongside the error")
	}
	if s.palette.Len() != 0 {
		t.Errorf("palette length = %d, want 0", s.palette.Len())
	}
}
