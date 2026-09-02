package session

import "testing"

// BenchmarkResolveBlobLeftover measures resolveBlob on the shape it sees while
// chunks stream: a full set of open transactions, none of which is emptied by
// the hash being resolved, so the state is unchanged between iterations.
func BenchmarkResolveBlobLeftover(b *testing.B) {
	const transactions = 8

	s := &Session{
		blobs:                 map[uint64][]byte{},
		openChunkTransactions: make([]map[uint64]struct{}, 0, transactions),
	}
	for i := range transactions {
		m := make(map[uint64]struct{}, 24)
		for j := range 24 {
			m[uint64(i*100+j)] = struct{}{}
		}
		s.openChunkTransactions = append(s.openChunkTransactions, m)
	}

	h := &ClientCacheBlobStatusHandler{}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.resolveBlob(1<<40, s)
	}
	b.StopTimer()

	if len(s.openChunkTransactions) != transactions {
		b.Fatalf("open chunk transactions = %d, want %d", len(s.openChunkTransactions), transactions)
	}
}
