package mcdb

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// markedColumn returns a column whose only non-air block is at (marker, minY, 0). The marker travels with the
// column data, so a column that comes back paired with the wrong position is detectable.
func markedColumn(marker uint8) *chunk.Column {
	col := emptyColumn()
	col.Chunk.SetBlock(marker, int16(world.Range()[0]), 0, 0, ridStone)
	return col
}

// markerOf returns the marker markedColumn wrote, searching x in [0, n). It returns -1 if the column carries
// no marker and -2 if it carries more than one.
func markerOf(col *chunk.Column, n int) int {
	y := int16(col.Chunk.Range()[0])
	found := -1
	for m := 0; m < n; m++ {
		if col.Chunk.Block(uint8(m), y, 0, 0) == ridAir {
			continue
		}
		if found >= 0 {
			return -2
		}
		found = m
	}
	return found
}

// collectColumns drains iter into a map keyed by the pair the iterator reported. A pair yielded twice is a
// failure: the seen map in Next exists to prevent exactly that.
func collectColumns(t *testing.T, iter *ColumnIterator) map[dbKey]*chunk.Column {
	t.Helper()
	out := make(map[dbKey]*chunk.Column)
	for iter.Next() {
		k := dbKey{pos: iter.Position()}
		if _, ok := out[k]; ok {
			t.Errorf("position %v was yielded twice", k.pos)
		}
		out[k] = iter.Column()
	}
	return out
}

// assertPairs fails unless got holds exactly the pairs in want.
func assertPairs(t *testing.T, want []dbKey, got map[dbKey]*chunk.Column) {
	t.Helper()
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("position %v was not yielded", k.pos)
		}
	}
	for k := range got {
		if !slices.Contains(want, k) {
			t.Errorf("position %v was yielded but was not expected", k.pos)
		}
	}
}

// aliveIters reports how many leveldb iterators the DB still has outstanding.
func aliveIters(t *testing.T, db *DB) int {
	t.Helper()
	v, err := db.ldb.GetProperty("leveldb.aliveiters")
	if err != nil {
		t.Fatalf("read leveldb.aliveiters: %v", err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("parse leveldb.aliveiters %q: %v", v, err)
	}
	return n
}

// TestColumnIteratorFullIteration fixes that a nil range yields every stored column exactly once, and that
// each column is paired with the position it was stored at.
func TestColumnIteratorFullIteration(t *testing.T) {
	db := newDB(t)
	stored := []dbKey{
		{pos: world.ChunkPos{0, 0}},
		{pos: world.ChunkPos{-1, 5}},
		{pos: world.ChunkPos{12, -7}},
	}
	for i, k := range stored {
		if err := db.StoreColumn(k.pos, markedColumn(uint8(i))); err != nil {
			t.Fatalf("store %v: %v", k.pos, err)
		}
	}

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	got := collectColumns(t, iter)
	if err := iter.Error(); err != nil {
		t.Fatalf("iterate: %v", err)
	}

	// Key order is leveldb's, not the store order, so only the set is asserted.
	assertPairs(t, stored, got)
	for i, k := range stored {
		col, ok := got[k]
		if !ok {
			continue
		}
		if m := markerOf(col, len(stored)); m != i {
			t.Errorf("position %v: marker %v, want %v (column paired with the wrong position)", k.pos, m, i)
		}
	}
}

// TestColumnIteratorNilRange fixes that NewColumnIterator(nil) substitutes the zero IteratorRange rather than
// leaving a nil pointer for within to dereference.
func TestColumnIteratorNilRange(t *testing.T) {
	db := newDB(t)
	stored := []dbKey{
		{pos: world.ChunkPos{0, 0}},
	}
	for i, k := range stored {
		if err := db.StoreColumn(k.pos, markedColumn(uint8(i))); err != nil {
			t.Fatalf("store %v: %v", k.pos, err)
		}
	}

	nilIter := db.NewColumnIterator(nil)
	defer nilIter.Release()
	if nilIter.r == nil {
		t.Fatal("range is nil; within would panic on the first key")
	}
	if *nilIter.r != (IteratorRange{}) {
		t.Errorf("range = %+v, want the zero IteratorRange", *nilIter.r)
	}
	fromNil := collectColumns(t, nilIter)

	emptyIter := db.NewColumnIterator(&IteratorRange{})
	defer emptyIter.Release()
	fromEmpty := collectColumns(t, emptyIter)

	assertPairs(t, stored, fromNil)
	assertPairs(t, stored, fromEmpty)
	if len(fromNil) != len(fromEmpty) {
		t.Errorf("nil range yielded %v columns, zero range yielded %v", len(fromNil), len(fromEmpty))
	}
}

func TestIteratorRangeWithin(t *testing.T) {
	var (
		zero    = IteratorRange{}
		box     = IteratorRange{Min: world.ChunkPos{-2, -2}, Max: world.ChunkPos{2, 2}}
		minOnly = IteratorRange{Min: world.ChunkPos{5, 5}}
		maxOnly = IteratorRange{Max: world.ChunkPos{4, 4}}
	)
	cases := []struct {
		name string
		r    IteratorRange
		pos  world.ChunkPos
		want bool
	}{
		{"zero range, origin", zero, world.ChunkPos{0, 0}, true},
		{"zero range, far negative", zero, world.ChunkPos{-1 << 20, -1 << 20}, true},
		{"zero range, int32 extremes", zero, world.ChunkPos{math.MinInt32, math.MaxInt32}, true},

		{"box, min corner is inclusive", box, world.ChunkPos{-2, -2}, true},
		{"box, interior", box, world.ChunkPos{1, 1}, true},
		{"box, last position on both axes", box, world.ChunkPos{1, 1}, true},
		{"box, max corner is exclusive", box, world.ChunkPos{2, 2}, false},
		{"box, x at max is exclusive", box, world.ChunkPos{2, -2}, false},
		{"box, z at max is exclusive", box, world.ChunkPos{-2, 2}, false},
		{"box, x below min", box, world.ChunkPos{-3, 0}, false},
		{"box, z below min", box, world.ChunkPos{0, -3}, false},

		// PINNED BUG: a range with only Min set matches nothing. The zero-value shortcut needs BOTH Min and
		// Max to be zero, so with Max left at {0,0} every position fails pos[0] < Max[0]. "Everything from
		// here on" is not expressible and silently yields an empty iteration instead of an error. Left alone:
		// fixing it changes what existing callers get back.
		{"min only, at min", minOnly, world.ChunkPos{5, 5}, false},
		{"min only, above min", minOnly, world.ChunkPos{10, 10}, false},
		{"min only, below min", minOnly, world.ChunkPos{0, 0}, false},

		// A range with only Max set behaves as if Min were {0,0}, which is the asymmetric half of the same
		// shortcut: negative positions are dropped without the caller asking for that.
		{"max only, origin", maxOnly, world.ChunkPos{0, 0}, true},
		{"max only, below max", maxOnly, world.ChunkPos{3, 3}, true},
		{"max only, at max", maxOnly, world.ChunkPos{4, 4}, false},
		{"max only, negative x", maxOnly, world.ChunkPos{-1, 0}, false},
		{"max only, negative z", maxOnly, world.ChunkPos{0, -1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.within(c.pos); got != c.want {
				t.Errorf("within(%v) with %+v = %v, want %v", c.pos, c.r, got, c.want)
			}
		})
	}
}

// TestColumnIteratorRangeFilter fixes that the range is applied to what the iterator yields, not only by
// within in isolation.
func TestColumnIteratorRangeFilter(t *testing.T) {
	db := newDB(t)
	stored := []dbKey{
		{pos: world.ChunkPos{-1, -1}},
		{pos: world.ChunkPos{0, 0}},
		{pos: world.ChunkPos{1, 1}},
		{pos: world.ChunkPos{2, 2}},
		{pos: world.ChunkPos{-2, -1}},
		{pos: world.ChunkPos{0, 2}},
	}
	for i, k := range stored {
		if err := db.StoreColumn(k.pos, markedColumn(uint8(i))); err != nil {
			t.Fatalf("store %v: %v", k.pos, err)
		}
	}

	cases := []struct {
		name string
		r    *IteratorRange
		want []dbKey
	}{
		{
			name: "box",
			r:    &IteratorRange{Min: world.ChunkPos{-1, -1}, Max: world.ChunkPos{2, 2}},
			want: []dbKey{
				{pos: world.ChunkPos{-1, -1}},
				{pos: world.ChunkPos{0, 0}},
				{pos: world.ChunkPos{1, 1}},
			},
		},
		{
			name: "box matching nothing",
			r:    &IteratorRange{Min: world.ChunkPos{100, 100}, Max: world.ChunkPos{110, 110}},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			iter := db.NewColumnIterator(c.r)
			defer iter.Release()
			got := collectColumns(t, iter)
			if err := iter.Error(); err != nil {
				t.Fatalf("iterate: %v", err)
			}
			assertPairs(t, c.want, got)
			if len(got) != len(c.want) {
				t.Errorf("yielded %v columns, want %v", len(got), len(c.want))
			}
		})
	}
}

// TestColumnIteratorColumnNotAliased fixes that Column() hands out a fresh column per iteration. Next calls
// LoadColumn, which allocates, so a pointer kept from an earlier iteration keeps its own data. Callers may
// therefore accumulate the columns they are handed without copying.
func TestColumnIteratorColumnNotAliased(t *testing.T) {
	db := newDB(t)
	const n = 3
	for i := 0; i < n; i++ {
		pos := world.ChunkPos{int32(i), 0}
		if err := db.StoreColumn(pos, markedColumn(uint8(i))); err != nil {
			t.Fatalf("store %v: %v", pos, err)
		}
	}

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	if !iter.Next() {
		t.Fatalf("first Next returned false: %v", iter.Error())
	}
	first, firstPos := iter.Column(), iter.Position()
	firstMarker := markerOf(first, n)
	if firstMarker < 0 {
		t.Fatalf("first column marker = %v, want one marked block", firstMarker)
	}

	if !iter.Next() {
		t.Fatalf("second Next returned false: %v", iter.Error())
	}
	if second := iter.Column(); first == second {
		t.Fatal("Column() returned the same pointer on two iterations; the column is reused")
	}
	if iter.Position() == firstPos {
		t.Errorf("position %v did not advance", firstPos)
	}
	if m := markerOf(first, n); m != firstMarker {
		t.Errorf("first column marker changed to %v after advancing, want %v", m, firstMarker)
	}

	for iter.Next() {
	}
	if m := markerOf(first, n); m != firstMarker {
		t.Errorf("first column marker changed to %v after exhaustion, want %v", m, firstMarker)
	}
}

// TestColumnIteratorExhaustion fixes what the accessors report once Next has returned false with nothing left
// to read. Exhaustion is not an error.
func TestColumnIteratorExhaustion(t *testing.T) {
	db := newDB(t)
	pos := world.ChunkPos{3, -4}
	if err := db.StoreColumn(pos, emptyColumn()); err != nil {
		t.Fatalf("store: %v", err)
	}

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	if !iter.Next() {
		t.Fatalf("first Next returned false: %v", iter.Error())
	}
	if iter.Next() {
		t.Fatalf("second Next returned true at %v", iter.Position())
	}
	if col := iter.Column(); col != nil {
		t.Errorf("Column() = %p, want nil", col)
	}
	if err := iter.Error(); err != nil {
		t.Errorf("Error() = %v, want nil (exhaustion is not an error)", err)
	}
	if iter.Next() {
		t.Error("Next() after exhaustion returned true")
	}

	// PINNED BUG: Column is cleared on exhaustion but Position is not, so Position keeps
	// returning the last pair's position after Next reported there is no current pair. A caller that reads
	// Position without checking Next gets a stale coordinate rather than a zero one. Left alone: clearing it
	// would equally be a guess, and the three accessors need one deliberate contract.
	if got := iter.Position(); got != pos {
		t.Fatalf("Position() after exhaustion = %v, want the stale %v", got, pos)
	}
}

// TestColumnIteratorRelease fixes that Release is idempotent and that a released iterator reports exhaustion
// rather than panicking.
func TestColumnIteratorRelease(t *testing.T) {
	db := newDB(t)
	for i := 0; i < 3; i++ {
		pos := world.ChunkPos{int32(i), 0}
		if err := db.StoreColumn(pos, emptyColumn()); err != nil {
			t.Fatalf("store %v: %v", pos, err)
		}
	}

	iter := db.NewColumnIterator(nil)
	if !iter.Next() {
		t.Fatalf("first Next returned false: %v", iter.Error())
	}
	iter.Release()
	iter.Release()

	if iter.Next() {
		t.Errorf("Next() after Release returned true at %v", iter.Position())
	}
	if col := iter.Column(); col != nil {
		t.Errorf("Column() after Release = %p, want nil", col)
	}
	if err := iter.Error(); err != nil {
		t.Errorf("Error() after Release = %v, want nil", err)
	}

	// PINNED BUG: ColumnIterator.Error only ever returns its own err field, never the underlying leveldb
	// iterator's. Here leveldb recorded ErrIterReleased and the caller sees a clean exhaustion instead. The
	// same swallowing hides a genuine read error from a corrupt table: iteration just stops early and Error
	// stays nil, so a caller copying a world cannot tell a finished walk from a truncated one. Left alone:
	// surfacing it changes what every existing loop over Error observes.
	if err := iter.dbIter.Error(); !errors.Is(err, leveldb.ErrIterReleased) {
		t.Fatalf("underlying iterator error = %v, want leveldb.ErrIterReleased", err)
	}
}

// TestColumnIteratorUnreleasedPinsLevelDBIterator fixes that reading an iterator to exhaustion does not free
// it: only Release does. goleveldb installs a finalizer on the iterator, so an abandoned ColumnIterator is
// eventually collected, but until then it pins the snapshot it was opened on and the memtables and tables
// under it. There is no Close on exhaustion and no way to observe the leak from ColumnIterator's own API.
func TestColumnIteratorUnreleasedPinsLevelDBIterator(t *testing.T) {
	db := newDB(t)
	if err := db.StoreColumn(world.ChunkPos{0, 0}, emptyColumn()); err != nil {
		t.Fatalf("store: %v", err)
	}
	if got := aliveIters(t, db); got != 0 {
		t.Fatalf("alive iterators before opening one = %v, want 0", got)
	}

	iter := db.NewColumnIterator(nil)
	if got := aliveIters(t, db); got != 1 {
		t.Fatalf("alive iterators after NewColumnIterator = %v, want 1", got)
	}
	for iter.Next() {
	}
	if got := aliveIters(t, db); got != 1 {
		t.Errorf("alive iterators after exhaustion = %v, want 1 (exhaustion does not release)", got)
	}
	iter.Release()
	if got := aliveIters(t, db); got != 0 {
		t.Errorf("alive iterators after Release = %v, want 0", got)
	}
}

// TestColumnIteratorLoadError fixes what happens when a chunk that has a version key cannot be loaded: Next
// stops, the position is named in the error, and everything after that key is dropped.
func TestColumnIteratorLoadError(t *testing.T) {
	db := newDB(t)
	good := world.ChunkPos{1, 0}
	broken := world.ChunkPos{2, 0}
	after := world.ChunkPos{3, 0}
	for _, pos := range []world.ChunkPos{good, broken, after} {
		if err := db.StoreColumn(pos, emptyColumn()); err != nil {
			t.Fatalf("store %v: %v", pos, err)
		}
	}
	// The version key stays intact, so the iterator still recognises the chunk, but the biome payload is
	// shorter than the heightmap the loader skips over.
	putRaw(t, db, dbKey{pos: broken}.Sum(key3DData), []byte{1, 2, 3})

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	if !iter.Next() {
		t.Fatalf("first Next returned false: %v", iter.Error())
	}
	if got := iter.Position(); got != good {
		t.Fatalf("first position = %v, want %v", got, good)
	}
	firstColumn := iter.Column()

	if iter.Next() {
		t.Fatalf("Next over the broken chunk returned true at %v", iter.Position())
	}
	err := iter.Error()
	if err == nil {
		t.Fatal("Error() = nil, want a load failure")
	}
	if errors.Is(err, leveldb.ErrNotFound) {
		t.Errorf("Error() = %v, want a decode failure rather than a missing key", err)
	}
	// PINNED BUG: the load failure is a bare fmt.Errorf chain with no sentinel and no error type, so a caller
	// that wants to skip a corrupt chunk and carry on has nothing to branch on and must match strings, which
	// is what this assertion is reduced to. Left alone: adding a sentinel is an API change.
	if !strings.Contains(err.Error(), broken.String()) {
		t.Errorf("Error() = %v, want it to name the position %v", err, broken)
	}
	if !strings.Contains(err.Error(), "read biomes") {
		t.Errorf("Error() = %v, want it to name the failing read", err)
	}

	// Column is nil because the failed LoadColumn assigned its nil result straight into iter.current.
	if got := iter.Column(); got != nil {
		t.Errorf("Column() after the error = %p, want nil", got)
	}
	// The column handed out before the failure is the caller's own value and survives it.
	if firstColumn == nil || firstColumn.Chunk == nil {
		t.Error("the column from the successful iteration was invalidated by the later failure")
	}

	// The error is sticky and the chunk stored past the broken one is never reached.
	if iter.Next() {
		t.Error("Next() after an error returned true")
	}
	if got := iter.Error(); got != err {
		t.Errorf("Error() changed to %v after a further Next, want %v", got, err)
	}
}

// TestColumnIteratorSkipsOtherDimensionChunks fixes that the 13-byte version keys a vanilla world writes for
// the chunks of its Nether (1) and End (2), or of any other dimension ID, are stepped over without ending the
// iteration. Each sorts before every key of the overworld chunk at the same position, so the overworld chunks
// are only yielded if the iteration carries on past them.
func TestColumnIteratorSkipsOtherDimensionChunks(t *testing.T) {
	db := newDB(t)
	stored := []dbKey{
		{pos: world.ChunkPos{0, 0}},
		{pos: world.ChunkPos{1, 0}},
	}
	for i, k := range stored {
		if err := db.StoreColumn(k.pos, markedColumn(uint8(i))); err != nil {
			t.Fatalf("store %v: %v", k.pos, err)
		}
	}
	// {-9, 4} holds no overworld chunk at all, so reading one of its keys as an overworld key would show up.
	for _, id := range []uint32{1, 2, 7} {
		for _, pos := range []world.ChunkPos{stored[0].pos, stored[1].pos, {-9, 4}} {
			key := binary.LittleEndian.AppendUint32(index(pos), id)
			putRaw(t, db, append(key, keyVersion), []byte{chunkVersion})
		}
	}

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	got := collectColumns(t, iter)
	if err := iter.Error(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	assertPairs(t, stored, got)
	for i, k := range stored {
		if col, ok := got[k]; ok {
			if m := markerOf(col, len(stored)); m != i {
				t.Errorf("position %v: marker %v, want %v", k.pos, m, i)
			}
		}
	}
}

// TestColumnIteratorDuplicateVersionKeys fixes the purpose of the seen map: a chunk carrying both keyVersion
// and keyVersionOld is one chunk, and the iterator must not hand it over twice.
func TestColumnIteratorDuplicateVersionKeys(t *testing.T) {
	db := newDB(t)
	pos := world.ChunkPos{2, -6}
	if err := db.StoreColumn(pos, markedColumn(1)); err != nil {
		t.Fatalf("store: %v", err)
	}
	// keyVersion ',' sorts before keyVersionOld 'v', so the second key is the one the seen map drops.
	putRaw(t, db, dbKey{pos: pos}.Sum(keyVersionOld), []byte{chunkVersion})

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	n := 0
	for iter.Next() {
		n++
		if got := iter.Position(); got != pos {
			t.Errorf("position = %v, want %v", got, pos)
		}
	}
	if err := iter.Error(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if n != 1 {
		t.Errorf("yielded %v columns, want 1", n)
	}
}

// TestColumnIteratorSkipsNonChunkKeys fixes that keys which are not per-chunk version keys are stepped over
// without ending the iteration. Several world-level keys are exactly 9 bytes long, the same length as an
// overworld version key, so only the trailing byte separates them.
func TestColumnIteratorSkipsNonChunkKeys(t *testing.T) {
	db := newDB(t)
	stored := []dbKey{
		{pos: world.ChunkPos{0, 0}},
	}
	for i, k := range stored {
		col := markedColumn(uint8(i))
		// Writes the digp index and an actorprefix record on top of the sub chunk and finalisation keys.
		col.Entities = []chunk.Entity{{ID: int64(i + 1), Data: map[string]any{"identifier": "impulse:test"}}}
		if err := db.StoreColumn(k.pos, col); err != nil {
			t.Fatalf("store %v: %v", k.pos, err)
		}
	}
	for _, k := range [][]byte{
		[]byte(keyOverworld),
		[]byte(keyMobEvents),
		[]byte(keyBiomeData),
		[]byte(keyScoreboard),
		[]byte(keyAutonomousEntities),
		[]byte(keyLocalPlayer),
		[]byte("player_00000000-0000-0000-0000-000000000001"),
		[]byte("player_server_00000000-0000-0000-0000-000000000001"),
	} {
		putRaw(t, db, k, []byte{1})
	}
	for _, name := range []string{keyOverworld, keyMobEvents, keyBiomeData} {
		if n := len(name); n != 9 {
			t.Fatalf("key %q is %v bytes; this test needs keys that collide with a chunk key length", name, n)
		}
	}

	iter := db.NewColumnIterator(nil)
	defer iter.Release()
	got := collectColumns(t, iter)
	if err := iter.Error(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	assertPairs(t, stored, got)
	for i, k := range stored {
		if col, ok := got[k]; ok {
			if m := markerOf(col, len(stored)); m != i {
				t.Errorf("position %v: marker %v, want %v", k.pos, m, i)
			}
		}
	}
}

// nextDepthLog records how many ColumnIterator.Next frames are on the stack whenever the DB logs. The debug
// line db.column writes for a chunk whose version is not chunkVersion is the only hook that runs inside a
// Next call.
type nextDepthLog struct{ max int }

func (*nextDepthLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *nextDepthLog) WithAttrs([]slog.Attr) slog.Handler     { return h }
func (h *nextDepthLog) WithGroup(string) slog.Handler          { return h }

func (h *nextDepthLog) Handle(context.Context, slog.Record) error {
	pcs := make([]uintptr, 8192)
	n := runtime.Callers(0, pcs)
	if n == len(pcs) {
		return errors.New("stack deeper than the pc buffer")
	}
	depth, frames := 0, runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if strings.Contains(f.Function, "(*ColumnIterator).Next") {
			depth++
		}
		if !more {
			break
		}
	}
	h.max = max(h.max, depth)
	return nil
}

// PINNED BUG: Next skips a key by returning iter.Next(), and Go
// does not eliminate that tail call, so every skipped key costs a stack frame that only unwinds once a column
// is finally yielded. Within one chunk that is bounded by its 27 keys, but a range filter rejects whole
// chunks without unwinding, so the depth grows with the number of keys walked rather than with the number of
// columns returned. A narrow range over a large world recurses once per key in it. Left alone: converting the
// recursion to a loop is a rewrite of Next, not a test's business.
func TestColumnIteratorNextRecursionDepth(t *testing.T) {
	h := &nextDepthLog{}
	conf := Config{Log: slog.New(h), Blocks: newTestRegistry()}
	db, err := conf.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Column x sorts before column x+1, so the target is reached last.
	const skipped = 30
	for x := int32(0); x < skipped; x++ {
		if err := db.StoreColumn(world.ChunkPos{x, 0}, emptyColumn()); err != nil {
			t.Fatalf("store %v: %v", x, err)
		}
	}
	target := world.ChunkPos{skipped, 0}
	if err := db.StoreColumn(target, emptyColumn()); err != nil {
		t.Fatalf("store target: %v", err)
	}
	putRaw(t, db, dbKey{pos: target}.Sum(keyVersion), []byte{chunkVersion - 1})

	iter := db.NewColumnIterator(&IteratorRange{Min: target, Max: world.ChunkPos{target[0] + 1, 1}})
	defer iter.Release()
	if !iter.Next() {
		t.Fatalf("Next returned false: %v", iter.Error())
	}
	if got := iter.Position(); got != target {
		t.Fatalf("position = %v, want %v", got, target)
	}

	// Per stored column: 3D data, version, 24 sub chunks and finalisation. Plus the target's own 3D data key
	// and the outermost Next call.
	const keysPerColumn = 27
	want := skipped*keysPerColumn + 2
	if h.max != want {
		t.Errorf("deepest Next recursion = %v frames, want %v (one frame per key walked)", h.max, want)
	}
	if h.max <= 1 {
		t.Error("Next no longer recurses per skipped key")
	}
}
