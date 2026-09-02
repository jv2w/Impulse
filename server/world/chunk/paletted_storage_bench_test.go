package chunk

import (
	"fmt"
	"testing"
)

// benchStorageSizes covers the two padded-free powers of two that dominate real sub chunks, the widest size,
// and the narrowest one. Padded sizes divide by 30 instead of 32 but take the same code path.
var benchStorageSizes = []paletteSize{1, 4, 8, 16}

// storageBenchSink keeps the read benchmarks from having their result discarded.
var storageBenchSink uint16

// atBenchSink is the equivalent for the benchmarks that read through the palette.
var atBenchSink uint32

// BenchmarkPalettedStorageAt walks all 4096 positions through At, the read path Chunk.Block, the encoders and
// the light code go through. Unlike the paletteIndex benchmarks it also dereferences the Palette.
func BenchmarkPalettedStorageAt(b *testing.B) {
	for _, size := range benchStorageSizes {
		b.Run(fmt.Sprintf("bits%d", size), func(b *testing.B) {
			storage, sink := storageOfSize(size), uint32(0)
			for b.Loop() {
				for linear := 0; linear < 4096; linear++ {
					x, y, z := storageCoords(linear)
					sink ^= storage.At(x, y, z)
				}
			}
			atBenchSink = sink
		})
	}
}

func BenchmarkPalettedStoragePaletteIndex(b *testing.B) {
	for _, size := range benchStorageSizes {
		b.Run(fmt.Sprintf("bits%d", size), func(b *testing.B) {
			storage := storageOfSize(size)
			linear, sink := 0, uint16(0)
			for b.Loop() {
				x, y, z := storageCoords(linear)
				sink ^= storage.paletteIndex(x, y, z)
				linear = (linear + 1) & 4095
			}
			storageBenchSink = sink
		})
	}
}

func BenchmarkPalettedStorageSetPaletteIndex(b *testing.B) {
	for _, size := range benchStorageSizes {
		b.Run(fmt.Sprintf("bits%d", size), func(b *testing.B) {
			storage := storageOfSize(size)
			mask := uint16(storage.indexMask)
			linear := 0
			for b.Loop() {
				x, y, z := storageCoords(linear)
				storage.setPaletteIndex(x, y, z, uint16(linear)&mask)
				linear = (linear + 1) & 4095
			}
		})
	}
}

// BenchmarkPalettedStorageScan walks all 4096 positions of a storage in one iteration, the access pattern
// compact and the encoders use.
func BenchmarkPalettedStorageScan(b *testing.B) {
	for _, size := range benchStorageSizes {
		b.Run(fmt.Sprintf("bits%d", size), func(b *testing.B) {
			storage, sink := storageOfSize(size), uint16(0)
			for b.Loop() {
				for linear := 0; linear < 4096; linear++ {
					x, y, z := storageCoords(linear)
					sink ^= storage.paletteIndex(x, y, z)
				}
			}
			storageBenchSink = sink
		})
	}
}

// BenchmarkPalettedStorageFill writes all 4096 positions in one iteration, the access pattern resize uses when
// a storage grows.
func BenchmarkPalettedStorageFill(b *testing.B) {
	for _, size := range benchStorageSizes {
		b.Run(fmt.Sprintf("bits%d", size), func(b *testing.B) {
			storage := storageOfSize(size)
			mask := uint16(storage.indexMask)
			for b.Loop() {
				for linear := 0; linear < 4096; linear++ {
					x, y, z := storageCoords(linear)
					storage.setPaletteIndex(x, y, z, uint16(linear)&mask)
				}
			}
		})
	}
}
