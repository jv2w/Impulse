package chunk

import "testing"

// benchLightChunk builds a chunk that exercises both halves of the filling stage: a solid floor and a roof
// over half of the columns give skylight something to propagate sideways under, and a row of lamps under the
// roof gives block light a source to spread from.
func benchLightChunk() *Chunk {
	c := New(newTestRegistry(), overworldRange)
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(-64); y <= 0; y++ {
				c.SetBlock(x, y, z, 0, ridSolid)
			}
			if x < 8 {
				c.SetBlock(x, 10, z, 0, ridSolid)
			}
		}
	}
	for i := uint8(0); i < 8; i++ {
		c.SetBlock(i*2, 5, i, 0, ridLamp)
	}
	return c
}

// BenchmarkLightFill measures the filling stage World.loadChunk runs for every chunk it loads. Fill starts by
// resetting every light slice, so repeating it over one chunk repeats the same work.
func BenchmarkLightFill(b *testing.B) {
	c := benchLightChunk()
	for b.Loop() {
		lightFill(c, 0, 0)
	}
}
