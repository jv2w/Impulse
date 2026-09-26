package chunk

import "testing"

// Regression for the HeightMap aliasing defect: HeightMap must hand out a
// copy, so a caller that writes to the result cannot change the chunk.
func TestChunkHeightMapReturnsCopy(t *testing.T) {
	c := newTestChunk()
	c.SetBlock(1, 10, 2, 0, ridSolid)
	h := c.HeightMap()
	h.Set(0, 0, 999)
	if got := c.HeightMap().At(0, 0); got == 999 {
		t.Fatalf("HeightMap().At(0,0) = %d, want the recomputed height: a caller write leaked into the chunk", got)
	}
	if got := c.HeightMap().At(1, 2); got != 11 {
		t.Fatalf("HeightMap().At(1,2) = %d, want 11", got)
	}
}
