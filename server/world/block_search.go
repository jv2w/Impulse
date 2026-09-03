package world

import (
	"errors"
	"iter"
	"slices"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// blocksWithin implements Tx.BlocksWithin. It must only be called during a transaction.
func (w *World) blocksWithin(pos cube.Pos, radius int, blocks ...Block) iter.Seq[cube.Pos] {
	return func(yield func(cube.Pos) bool) {
		if radius <= 0 || len(blocks) == 0 {
			return
		}
		targets := make([]uint32, 0, len(blocks))
		for _, b := range blocks {
			targets = append(targets, w.conf.Blocks.BlockRuntimeID(b))
		}

		// Horizontal bounds of the search: min inclusive, max exclusive.
		minX, minZ := pos.X()-radius, pos.Z()-radius
		maxX, maxZ := pos.X()+radius, pos.Z()+radius

		minChunk := chunkPosFromBlockPos(cube.Pos{minX, 0, minZ})
		maxChunk := chunkPosFromBlockPos(cube.Pos{maxX - 1, 0, maxZ - 1})
		var logged bool
		// The chunk bounds are read once instead of once per loop step. minChunk and maxChunk are local values
		// that nothing in the loop writes to, so the same chunks are visited in the same order.
		maxChunkX, maxChunkZ := maxChunk.X(), maxChunk.Z()
		minChunkZ := minChunk.Z()
		for chunkX := minChunk.X(); chunkX <= maxChunkX; chunkX++ {
			for chunkZ := minChunkZ; chunkZ <= maxChunkZ; chunkZ++ {
				chunkPos := ChunkPos{chunkX, chunkZ}
				var c *chunk.Chunk
				if col, ok := w.chunks[chunkPos]; ok {
					c = col.Chunk
				} else {
					col, err := w.conf.Provider.LoadColumn(chunkPos, w.conf.Dim)
					if err != nil {
						if !errors.Is(err, leveldb.ErrNotFound) && !logged {
							// Log only the first error: a systemic provider failure would otherwise log once per chunk.
							w.conf.Log.Error("blocks within: "+err.Error(), "X", chunkX, "Z", chunkZ)
							logged = true
						}
						continue
					}
					c = col.Chunk
				}
				if !yieldMatchingBlocks(yield, chunkPos, c, targets, minX, minZ, maxX, maxZ) {
					return
				}
			}
		}
	}
}

// yieldMatchingBlocks yields the positions of blocks in the primary layer of a chunk that match one of the target
// runtime IDs and fall within the horizontal bounds passed. It returns false if the iteration was stopped.
func yieldMatchingBlocks(yield func(cube.Pos) bool, chunkPos ChunkPos, c *chunk.Chunk, targets []uint32, minX, minZ, maxX, maxZ int) bool {
	baseX, baseZ := int(chunkPos.X())<<4, int(chunkPos.Z())<<4
	// Clip the block iteration bounds to the search area once per chunk.
	x0, x1 := max(minX-baseX, 0), min(maxX-baseX, 16)
	z0, z1 := max(minZ-baseZ, 0), min(maxZ-baseZ, 16)
	for i, sub := range c.Sub() {
		if sub.Empty() {
			continue
		}
		layers := sub.Layers()
		if len(layers) == 0 {
			continue
		}
		storage := layers[0]
		indices := matchingPaletteIndices(storage.Palette(), targets)
		if len(indices) == 0 {
			continue
		}
		baseY := int(c.SubY(int16(i)))
		for x := x0; x < x1; x++ {
			for z := z0; z < z1; z++ {
				for y := range 16 {
					if !slices.Contains(indices, storage.PaletteIndex(byte(x), byte(y), byte(z))) {
						continue
					}
					if !yield(cube.Pos{baseX + x, baseY + y, baseZ + z}) {
						return false
					}
				}
			}
		}
	}
	return true
}

// matchingPaletteIndices returns the indices in the palette that hold one of the target runtime IDs.
func matchingPaletteIndices(palette *chunk.Palette, targets []uint32) []uint16 {
	var indices []uint16
	// The palette length is read once instead of once per step; the loop does not modify the palette, so the
	// same indices are collected in the same order.
	for i, n := 0, palette.Len(); i < n; i++ {
		if slices.Contains(targets, palette.Value(uint16(i))) {
			indices = append(indices, uint16(i))
		}
	}
	return indices
}
