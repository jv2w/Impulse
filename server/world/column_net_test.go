package world

import (
	"bytes"
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// The network payload of a sub chunk is the same for every viewer, so a Column
// encodes it once and hands the same bytes to all of them. What is pinned here
// is that the sharing stops the moment the chunk is edited: a viewer that asks
// afterwards must not be sent the terrain from before.
//
// The assertions are made after the transaction rather than inside it. A
// transaction runs on the world goroutine, where a t.Fatal would kill the
// goroutine mid-transaction and hang the test on Task.Wait.

// netCacheWorld builds a World with closeSeqBlock registered, so that a test
// can change the blocks of a column through a transaction.
func netCacheWorld(t *testing.T) *World {
	t.Helper()

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:close_seq_block", Properties: map[string]any{}})
	registry.RegisterBlock(closeSeqBlock{})
	w := Config{
		Blocks:     registry,
		RandSource: rand.NewPCG(0x4e455443, 0x41434845),
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestColumnNetworkSubChunkInvalidatedOnBlockChange pins that the encoded sub
// chunk is built once and reused, and that a block set through a transaction
// replaces it with a freshly encoded one.
func TestColumnNetworkSubChunkInvalidatedOnBlockChange(t *testing.T) {
	w := netCacheWorld(t)
	pos := cube.Pos{0, 64, 0}

	var before, after, want []byte
	var reused bool
	runWorld(w, func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		ind := col.SubIndex(int16(pos[1]))

		first := col.NetworkSubChunk(ind)
		before = slices.Clone(first)
		reused = &col.NetworkSubChunk(ind)[0] == &first[0]

		tx.SetBlock(pos, closeSeqBlock{}, nil)

		after = slices.Clone(col.NetworkSubChunk(ind))
		want = chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind))
	})

	if !reused {
		t.Error("the second call re-encoded the sub chunk instead of returning the cached bytes")
	}
	if bytes.Equal(after, before) {
		t.Fatalf("sub chunk bytes = %v, unchanged after a block was set", after)
	}
	if !bytes.Equal(after, want) {
		t.Fatalf("sub chunk bytes = %v, want the freshly encoded %v", after, want)
	}
}

// TestColumnNetworkHeightMapInvalidatedOnBlockChange pins the same for the
// height map. What the map holds after the edit is compared against a Column
// with an empty cache over the same chunk, which has no choice but to build it
// from the blocks as they stand.
func TestColumnNetworkHeightMapInvalidatedOnBlockChange(t *testing.T) {
	w := netCacheWorld(t)
	pos := cube.Pos{0, 64, 0}

	var beforeType, afterType, freshType byte
	var beforeData, afterData, freshData []int8
	runWorld(w, func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		ind := col.SubIndex(int16(pos[1]))

		beforeType, beforeData = col.NetworkHeightMap(ind)
		beforeData = slices.Clone(beforeData)

		tx.SetBlock(pos, closeSeqBlock{}, nil)

		afterType, afterData = col.NetworkHeightMap(ind)
		afterData = slices.Clone(afterData)
		freshType, freshData = (&Column{Chunk: col.Chunk}).NetworkHeightMap(ind)
	})

	// The type is asserted as the number that goes on the wire rather than
	// through the protocol constant: 3 is HeightMapDataTooLow, which is what a
	// column of nothing but air reports.
	if beforeType != 3 || beforeData != nil {
		t.Fatalf("height map of an empty column = (%d, %v), want (3, nil)", beforeType, beforeData)
	}
	if afterType == beforeType && slices.Equal(afterData, beforeData) {
		t.Fatalf("height map = (%d, %v), unchanged after a block was set", afterType, afterData)
	}
	if afterType != freshType || !slices.Equal(afterData, freshData) {
		t.Fatalf("height map = (%d, %v), want the freshly built (%d, %v)", afterType, afterData, freshType, freshData)
	}
}
