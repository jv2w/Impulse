package world

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// BenchmarkLoadChunkLighting measures what Tuning.FixedLight takes off the
// chunk load path. The vanilla arm runs the flood fill; the fixed arm only hands
// out the shared light slices. On top of the difference measured here, a fixed
// light world also skips World.calculateLight, which spreads light over nine
// columns per loaded neighbour on the transaction goroutine.
func BenchmarkLoadChunkLighting(b *testing.B) {
	for _, bc := range []struct {
		name  string
		rules Tuning
	}{
		{name: "vanilla", rules: Tuning{}},
		{name: "fixed light", rules: Tuning{FixedLight: 15}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			w := benchLightWorld(b, bc.rules)

			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				// A fresh position each time: loadChunk generates and lights a
				// column without consulting or filling the chunk cache.
				if _, err := w.loadChunk(ChunkPos{int32(i), 0}); err != nil {
					b.Fatalf("load chunk: %v", err)
				}
			}
		})
	}
}

// benchLightWorld returns a World whose generator produces a column with a
// solid roof and lamps under it, so that both sky and block light have
// something to propagate through.
func benchLightWorld(b *testing.B, rules Tuning) *World {
	b.Helper()

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:game_rule_lamp", Properties: map[string]any{}})
	registry.RegisterBlock(tuningLamp{})
	registry.RegisterBlockState(BlockState{Name: "test:game_rule_roof", Properties: map[string]any{}})
	registry.RegisterBlock(benchRoof{})
	// Air blocks light in this package unless something says otherwise: no
	// block package is imported, so nothing registers an implementation for it.
	registry.RegisterBlock(benchAir{})
	registry.Finalize()

	w := Config{
		Synchronous: true,
		Blocks:      registry,
		Generator: benchLightGenerator{
			lamp: registry.BlockRuntimeID(tuningLamp{}),
			roof: registry.BlockRuntimeID(benchRoof{}),
		},
		Tuning: rules,
	}.New()
	b.Cleanup(func() { _ = w.Close() })
	return w
}

// benchLightGenerator lays a solid roof at y 64 and a grid of lamps below it.
type benchLightGenerator struct{ lamp, roof uint32 }

func (g benchLightGenerator) GenerateChunk(_ ChunkPos, c *chunk.Chunk) {
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			c.SetBlock(x, 64, z, 0, g.roof)
		}
	}
	for x := uint8(0); x < 16; x += 4 {
		for z := uint8(0); z < 16; z += 4 {
			c.SetBlock(x, 32, z, 0, g.lamp)
		}
	}
}

func (benchLightGenerator) DefaultSpawn() cube.Pos { return cube.Pos{} }

// benchAir is a transparent air implementation, so light actually spreads.
type benchAir struct{}

func (benchAir) EncodeBlock() (string, map[string]any) { return "minecraft:air", nil }
func (benchAir) Hash() (uint64, uint64)                { return 1 << 40, 0 }
func (benchAir) Model() BlockModel                     { return tuningLampModel{} }
func (benchAir) LightDiffusionLevel() uint8            { return 0 }

// benchRoof is fully opaque and emits nothing.
type benchRoof struct{}

func (benchRoof) EncodeBlock() (string, map[string]any) { return "test:game_rule_roof", nil }
func (benchRoof) Hash() (uint64, uint64)                { return 1 << 41, 0 }
func (benchRoof) Model() BlockModel                     { return tuningLampModel{} }
