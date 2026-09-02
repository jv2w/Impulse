package world

import "testing"

// BenchmarkLoaderPopulateLoadQueue measures rebuilding the load queue of a
// loader with nothing loaded yet, the work a player pays for every chunk
// boundary it crosses.
func BenchmarkLoaderPopulateLoadQueue(b *testing.B) {
	w := Config{Synchronous: true}.New()
	defer func() { _ = w.Close() }()

	l := NewLoader(32, w, NopViewer{})

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.populateLoadQueue()
	}
	b.StopTimer()
}
