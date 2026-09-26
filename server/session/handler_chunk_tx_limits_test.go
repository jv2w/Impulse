package session

import (
	"testing"
)

// TestOpenChunkTransactionsBounded pins the transaction bound: a client that
// never acknowledges chunk data must not grow the open-transaction list
// without bound. Before the fix every LevelChunk and SubChunk reply appended
// one transaction nothing would ever resolve.
func TestOpenChunkTransactionsBounded(t *testing.T) {
	s := newStubSession(t, nil)
	s.blobMu.Lock()
	defer s.blobMu.Unlock()
	const extra = 64
	for i := 0; i < maxOpenChunkTransactions+extra; i++ {
		s.appendChunkTransactionLocked(map[uint64]struct{}{uint64(i): {}})
	}
	if len(s.openChunkTransactions) > maxOpenChunkTransactions {
		t.Fatalf("open transactions = %d, want at most %d", len(s.openChunkTransactions), maxOpenChunkTransactions)
	}
	for _, m := range s.openChunkTransactions {
		if _, ok := m[0]; ok {
			t.Fatal("oldest transaction retained, want it evicted")
		}
	}
	found := false
	for _, m := range s.openChunkTransactions {
		if _, ok := m[uint64(maxOpenChunkTransactions+extra-1)]; ok {
			found = true
		}
	}
	if !found {
		t.Fatal("newest transaction missing, want it retained")
	}
}
