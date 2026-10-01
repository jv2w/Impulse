package world

import (
	"sync"

	"github.com/jv2w/Impulse/server/world/chunk"
)

// chunkSaver writes chunk columns to the World's Provider on a goroutine of its
// own, so that encoding a column and writing it out is not done on the world's
// transaction goroutine. Columns are written in the order they were handed to
// store, so a later snapshot of a chunk is never overwritten by an earlier one.
//
// Concurrency: store, flush, waitPos, retry and close may be called from any
// goroutine. A column handed to store belongs to the saver from then on and
// must not be read or written by the caller.
type chunkSaver struct {
	w *World

	mu   sync.Mutex
	cond *sync.Cond
	// queue holds the columns not yet written, oldest first.
	queue []saveJob
	// writing is true while the goroutine is writing a job it took from queue.
	writing bool
	// pending counts the jobs of each chunk that are queued or being written.
	pending map[ChunkPos]int
	// failed holds the chunks whose last write failed, so that the next save
	// writes them again even if they were not changed since.
	failed map[ChunkPos]struct{}
	closed bool
	done   chan struct{}
}

// saveJob is one column to be written for the chunk at pos.
type saveJob struct {
	pos ChunkPos
	col *chunk.Column
}

// newChunkSaver returns a chunkSaver for w and starts its goroutine.
func newChunkSaver(w *World) *chunkSaver {
	s := &chunkSaver{w: w, pending: map[ChunkPos]int{}, failed: map[ChunkPos]struct{}{}, done: make(chan struct{})}
	s.cond = sync.NewCond(&s.mu)
	go s.run()
	return s
}

// store queues col to be written as the chunk at pos.
func (s *chunkSaver) store(pos ChunkPos, col *chunk.Column) {
	s.mu.Lock()
	s.queue = append(s.queue, saveJob{pos: pos, col: col})
	s.pending[pos]++
	s.mu.Unlock()
	s.cond.Broadcast()
}

// retry reports whether the last write of the chunk at pos failed, and forgets
// that it did.
func (s *chunkSaver) retry(pos ChunkPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, failed := s.failed[pos]
	delete(s.failed, pos)
	return failed
}

// waitPos blocks until no write of the chunk at pos is queued or in progress,
// so that loading the chunk reads what was last stored for it.
func (s *chunkSaver) waitPos(pos ChunkPos) {
	s.mu.Lock()
	for s.pending[pos] > 0 {
		s.cond.Wait()
	}
	s.mu.Unlock()
}

// flush blocks until every column queued so far has been written.
func (s *chunkSaver) flush() {
	s.mu.Lock()
	for len(s.queue) > 0 || s.writing {
		s.cond.Wait()
	}
	s.mu.Unlock()
}

// close writes every queued column and stops the goroutine. store must not be
// called afterwards.
func (s *chunkSaver) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cond.Broadcast()
	<-s.done
}

// run writes queued columns until the saver is closed and its queue is empty.
func (s *chunkSaver) run() {
	defer close(s.done)
	s.mu.Lock()
	for {
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		job := s.queue[0]
		s.queue[0] = saveJob{}
		s.queue = s.queue[1:]
		s.writing = true
		s.mu.Unlock()

		err := s.w.conf.Provider.StoreColumn(job.pos, job.col)
		if err != nil {
			s.w.conf.Log.Error("save chunk: "+err.Error(), "X", job.pos[0], "Z", job.pos[1])
		}

		s.mu.Lock()
		if err != nil {
			s.failed[job.pos] = struct{}{}
		} else {
			// A later column holds everything an earlier failed one did.
			delete(s.failed, job.pos)
		}
		if s.pending[job.pos]--; s.pending[job.pos] == 0 {
			delete(s.pending, job.pos)
		}
		s.writing = false
		s.cond.Broadcast()
	}
}
