package mcdb

import (
	"encoding/binary"
	"fmt"

	"github.com/df-mc/goleveldb/leveldb/iterator"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// ColumnIterator iterates over a DB's position/column pairs in key order.
//
// When an error is encountered, any call to Next will return false and will
// yield no position/chunk pairs. The error can be queried by calling the Error
// method. Calling Release is still necessary.
//
// An iterator must be released after use, but it is not necessary to read
// an iterator until exhaustion.
// Also, an iterator is not necessarily safe for concurrent use, but it is
// safe to use multiple iterators concurrently, with each in a dedicated
// goroutine.
type ColumnIterator struct {
	dbIter iterator.Iterator
	db     *DB
	r      *IteratorRange

	err error

	current *chunk.Column
	pos     world.ChunkPos
	seen    map[dbKey]struct{}
}

func newColumnIterator(db *DB, r *IteratorRange) *ColumnIterator {
	return &ColumnIterator{
		db:     db,
		dbIter: db.ldb.NewIterator(nil, nil),
		seen:   make(map[dbKey]struct{}),
		r:      r,
	}
}

// Next moves the iterator to the next key/value pair.
// It returns false if the iterator is exhausted.
func (iter *ColumnIterator) Next() bool {
	if iter.err != nil || !iter.dbIter.Next() {
		iter.current = nil
		return false
	}
	k := iter.dbIter.Key()
	// A chunk version key is the 8 byte chunk position followed by the key
	// type. The 13 byte version keys of a vanilla world carry a dimension ID
	// before the key type and hold the chunks of the Nether and the End, which
	// are skipped and left untouched.
	if len(k) != 9 || (k[8] != keyVersion && k[8] != keyVersionOld) {
		return iter.Next()
	}
	iter.pos = world.ChunkPos{
		int32(binary.LittleEndian.Uint32(k[:4])),
		int32(binary.LittleEndian.Uint32(k[4:8])),
	}
	if !iter.r.within(iter.pos) {
		return iter.Next()
	}
	key := dbKey{pos: iter.pos}
	if _, ok := iter.seen[key]; ok {
		// Already encountered this chunk. This might happen if there are
		// multiple version keys.
		return iter.Next()
	}
	iter.current, iter.err = iter.db.LoadColumn(iter.pos)
	if iter.err != nil {
		iter.err = fmt.Errorf("load chunk %v: %w", iter.pos, iter.err)
		return false
	}
	iter.seen[key] = struct{}{}
	return true
}

// Column returns the value of the current position/column pair, or nil if none.
func (iter *ColumnIterator) Column() *chunk.Column {
	return iter.current
}

// Position returns the position of the current position/column pair.
func (iter *ColumnIterator) Position() world.ChunkPos {
	return iter.pos
}

// Release releases associated resources. Release should always success
// and can be called multiple times without causing error.
func (iter *ColumnIterator) Release() {
	iter.dbIter.Release()
}

// Error returns any accumulated error. Exhausting all the key/value pairs
// is not considered to be an error.
func (iter *ColumnIterator) Error() error {
	return iter.err
}

// IteratorRange is a range used to limit what columns are accumulated by a
// ColumnIterator.
type IteratorRange struct {
	// Min and Max limit what chunk positions are returned by a ColumnIterator.
	// A zero value for both Min and Max causes all positions to be within the
	// range.
	Min, Max world.ChunkPos
}

// within checks if a position is within the IteratorRange.
func (r *IteratorRange) within(pos world.ChunkPos) bool {
	return ((r.Min == world.ChunkPos{}) && (r.Max == world.ChunkPos{})) ||
		pos[0] >= r.Min[0] && pos[0] < r.Max[0] && pos[1] >= r.Min[1] && pos[1] < r.Max[1]
}
