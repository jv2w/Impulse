package world

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// blockingGenerator generates every chunk as empty, but holds the generation of
// the chunks in stall until release is closed. started receives one value for
// each of them that began generating.
type blockingGenerator struct {
	stall   map[ChunkPos]bool
	started chan struct{}
	release chan struct{}
}

func (g blockingGenerator) GenerateChunk(pos ChunkPos, _ *chunk.Chunk) {
	if g.stall[pos] {
		g.started <- struct{}{}
		<-g.release
	}
}

func (blockingGenerator) DefaultSpawn() cube.Pos { return cube.Pos{} }

// TestTxChunkLoadsQueuedChunkItself queues a chunk load behind two stalled
// loads that occupy both workers, then reads a block in the queued chunk from a
// transaction. The transaction must load that chunk itself rather than wait for
// the loads ahead of it in the queue. Two workers are used because with one,
// generation is serialised by a lock the stalled load holds, which a real load
// only holds for the one chunk in progress.
func TestTxChunkLoadsQueuedChunkItself(t *testing.T) {
	stalled1, stalled2, queued := ChunkPos{0, 0}, ChunkPos{1, 0}, ChunkPos{5, 0}
	g := blockingGenerator{
		stall:   map[ChunkPos]bool{stalled1: true, stalled2: true},
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	w := Config{Generator: g, ChunkLoadWorkers: 2, Log: tickEntityDiscardLog()}.New()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(g.release) }) }
	t.Cleanup(func() {
		release()
		_ = w.Close()
	})

	noop := func(*Tx, *Column) {}
	if err := w.Do(func(tx *Tx) {
		w.loadChunkAsync(tx, stalled1, noop)
		w.loadChunkAsync(tx, stalled2, noop)
		w.loadChunkAsync(tx, queued, noop)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("queue loads: %v", err)
	}
	for range 2 {
		select {
		case <-g.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the workers never started both stalled loads")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var loaded bool
	err := w.Do(func(tx *Tx) {
		tx.Block(cube.Pos{int(queued[0]) * 16, 0, int(queued[1]) * 16})
		_, loaded = w.chunks[queued]
	}).Wait(ctx)
	if err != nil {
		t.Fatalf("reading a block in the queued chunk waited on the stalled load ahead of it: %v", err)
	}
	if !loaded {
		t.Fatal("the queued chunk is not loaded after a block in it was read")
	}
}
