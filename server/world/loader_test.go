package world

import (
	"slices"
	"testing"
)

// TestLoaderLoadQueueOrder pins the order populateLoadQueue produces: every
// chunk within the radius once, ordered by ascending rounded distance from the
// centre, with equal distances left in the x-then-z order they were scanned in.
func TestLoaderLoadQueueOrder(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer func() { _ = w.Close() }()

	const r = 8
	l := NewLoader(r, w, NopViewer{})

	var want []ChunkPos
	for dist := int32(0); dist <= r; dist++ {
		for x := int32(-r); x <= r; x++ {
			for z := int32(-r); z <= r; z++ {
				if pos := (ChunkPos{x, z}); chunkDistance(pos, l.pos) == dist {
					want = append(want, pos)
				}
			}
		}
	}
	if got := l.loadQueue; !slices.Equal(got, want) {
		t.Fatalf("load queue = %v, want %v", got, want)
	}
}
