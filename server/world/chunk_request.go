package world

import (
	"sync"
	"sync/atomic"

	"github.com/jv2w/Impulse/server/world/chunk"
)

// chunkRequest tracks a chunk that is being loaded or generated in the
// background. All callers waiting for the same chunk share a single request.
type chunkRequest struct {
	pos       ChunkPos
	callbacks []chunkCallback
	signalled bool

	// claimed is set by whichever of a worker, a transaction needing the chunk
	// right away and the shutdown drain takes the request first. Only that one
	// loads it or aborts it and closes done.
	claimed atomic.Bool
	done    chan struct{}
	col     *chunk.Column
	err     error
	result  *Column
}

// defaultChunkLoadWorkers is the number of chunk load workers started when
// Config.ChunkLoadWorkers is not set.
const defaultChunkLoadWorkers = 1

// chunkCallback is called with a chunk once it has been added to the world.
type chunkCallback = func(tx *Tx, col *Column)

// chunkWorkerPool runs chunk requests on a fixed number of background workers.
type chunkWorkerPool struct {
	w     *World
	queue chan *chunkRequest
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

func newChunkWorkerPool(w *World) *chunkWorkerPool {
	return &chunkWorkerPool{w: w, queue: make(chan *chunkRequest, 4096)}
}

// doImmediate blocks until the chunk is ready and returns it. A request no
// worker has taken yet is loaded right here.
//
// Which input was the problem: a transaction reading a chunk whose load was
// queued but not started, as when a player moves into chunks it just requested.
// It waited until the workers had worked through every load queued ahead of it,
// from every player, stalling the tick for seconds. Now it waits for at most
// this one chunk. The chunk loaded and the callbacks run are the same, as
// signal below is what a worker's load would have run.
func (r *chunkRequest) doImmediate(tx *Tx) *Column {
	if r.claimed.CompareAndSwap(false, true) {
		r.col, r.err = tx.World().loadChunk(r.pos)
		close(r.done)
	} else {
		<-r.done
	}
	r.signal(tx)
	return r.result
}

// load loads or generates the chunk and hands it back to the world to be
// added. It does nothing if a transaction took the request first.
func (r *chunkRequest) load(w *World) {
	if !r.claimed.CompareAndSwap(false, true) {
		return
	}
	r.col, r.err = w.loadChunk(r.pos)
	close(r.done)
	w.Do(r.signal)
}

// abort cancels a request that will never be carried out because the world is
// closing, releasing any callers waiting on it. It does nothing if the request
// was already taken, as whoever took it closes done.
func (r *chunkRequest) abort() {
	if r.claimed.CompareAndSwap(false, true) {
		close(r.done)
	}
}

// schedule hands r to the workers without blocking. It returns false if the
// request cannot be accepted, e.g. when the world is closing.
func (p *chunkWorkerPool) schedule(r *chunkRequest) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.w.closed.Load() {
		p.closed = true
		return false
	}
	select {
	case p.queue <- r:
		return true
	default:
		return false
	}
}

// handle continuously processes chunk requests until the world starts closing.
func (p *chunkWorkerPool) handle() {
	defer p.wg.Done()
	for {
		if p.w.closed.Load() {
			p.drainAndAbort()
			return
		}
		select {
		case r := <-p.queue:
			r.load(p.w)
		case <-p.w.closeStarted:
			p.drainAndAbort()
			return
		}
	}
}

// drainAndAbort cancels all remaining requests and stops accepting new ones.
func (p *chunkWorkerPool) drainAndAbort() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for {
		select {
		case r := <-p.queue:
			r.abort()
		default:
			return
		}
	}
}

// signal adds the finished chunk to the world and calls all callers waiting
// for it. It always runs inside a world transaction.
func (r *chunkRequest) signal(tx *Tx) {
	if r.signalled {
		return
	}
	r.signalled = true

	w := tx.World()
	pos := r.pos

	delete(w.chunkRequests, pos)
	if w.closed.Load() {
		return
	}
	if r.err != nil {
		w.conf.Log.Error("load chunk: "+r.err.Error(), "X", pos[0], "Z", pos[1])
		for _, recv := range r.callbacks {
			recv(tx, nil)
		}
		return
	}
	r.result = w.addChunk(pos, r.col)
	if w.closed.Load() {
		return
	}
	for _, recv := range r.callbacks {
		recv(tx, r.result)
	}
}
