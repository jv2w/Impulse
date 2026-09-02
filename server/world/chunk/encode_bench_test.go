package chunk

import (
	"math/rand"
	"testing"
)

// benchChunk builds a chunk shaped like loaded terrain: a solid floor, a mixed band above it and a biome per
// sub chunk. The mix gives the encoders storages of several palette sizes rather than a single one.
func benchChunk() *Chunk {
	c := New(newTestRegistry(), overworldRange)
	rnd := rand.New(rand.NewSource(0xbe4c))
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(-64); y < 0; y++ {
				c.SetBlock(x, y, z, 0, ridSolid)
			}
			for y := int16(0); y < 64; y++ {
				c.SetBlock(x, y, z, 0, uint32(rnd.Intn(len(testBlocks))))
			}
			for y := int16(-64); y < 320; y += 16 {
				c.SetBiome(x, y, z, uint32(y>>4)&7)
			}
		}
	}
	return c
}

var (
	encodeBenchData SerialisedData
	decodeBenchSink *Chunk
)

func BenchmarkEncodeDisk(b *testing.B) {
	c := benchChunk()
	for b.Loop() {
		encodeBenchData = Encode(c, DiskEncoding)
	}
}

func BenchmarkEncodeNetwork(b *testing.B) {
	c := benchChunk()
	for b.Loop() {
		encodeBenchData = Encode(c, NetworkEncoding)
	}
}

func BenchmarkDiskDecode(b *testing.B) {
	br := newTestRegistry()
	d := Encode(benchChunk(), DiskEncoding)
	for b.Loop() {
		c, err := DiskDecode(br, d, overworldRange)
		if err != nil {
			b.Fatal(err)
		}
		decodeBenchSink = c
	}
}

func BenchmarkNetworkDecode(b *testing.B) {
	br := newTestRegistry()
	payload := encodeNetworkPayload(Encode(benchChunk(), NetworkEncoding))
	count := len(New(br, overworldRange).sub)
	for b.Loop() {
		c, err := NetworkDecode(br, payload, count, overworldRange)
		if err != nil {
			b.Fatal(err)
		}
		decodeBenchSink = c
	}
}
