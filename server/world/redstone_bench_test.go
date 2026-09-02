package world

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

var redstoneDirtyTickBenchmarkPower int

func BenchmarkRedstoneDirtyTickLongLineWithClocks(b *testing.B) {
	const lineLength = 96

	w := Config{Synchronous: true, Blocks: redstoneSignalLossTestRegistry()}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		clockA := cube.Pos{-1, 64, 0}
		clockB := cube.Pos{lineLength / 2, 64, 1}
		line := make([]cube.Pos, lineLength)
		for x := range lineLength {
			line[x] = cube.Pos{x, 64, 0}
			tx.SetBlock(line[x], redstoneLossRelayer{}, nil)
		}
		tx.SetBlock(clockA, redstoneLossSource{Power: 15}, nil)
		tx.SetBlock(clockB, redstoneLossSource{Power: 15}, nil)
		tx.SetBlock(cube.Pos{lineLength, 64, 0}, redstoneLossConsumer{}, nil)
		tx.World().redstone.tick(tx, 0)

		b.ReportAllocs()
		b.ResetTimer()
		for tick := range b.N {
			tx.World().redstone.invalidateAround(clockA, clockA, RedstoneUpdateCauseBlockUpdate, tx.Range())
			tx.World().redstone.invalidateAround(clockB, clockB, RedstoneUpdateCauseBlockUpdate, tx.Range())
			tx.World().redstone.tick(tx, int64(tick+1))
		}
		b.StopTimer()

		if consumer, ok := tx.Block(cube.Pos{lineLength, 64, 0}).(redstoneLossConsumer); ok {
			redstoneDirtyTickBenchmarkPower = consumer.Power
		}
	})
}

// BenchmarkRedstoneCompileOneWayRelayerChain measures graph compilation over a
// chain of relayers that each point at the next but not back at the previous, so
// every step reaches its predecessor through the incoming candidate scan rather
// than through its own neighbour list.
func BenchmarkRedstoneCompileOneWayRelayerChain(b *testing.B) {
	const steps = 64

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:redstone_stair_relayer", Properties: map[string]any{}})
	registry.RegisterBlock(redstoneStairRelayer{})
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		start := cube.Pos{0, 8, 0}
		for i := range steps {
			tx.SetBlock(cube.Pos{start[0] + i, start[1] + i, start[2]}, redstoneStairRelayer{}, nil)
		}
		engine, candidates := tx.World().redstone, []cube.Pos{start}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			engine.compile(tx, candidates)
		}
		b.StopTimer()
	})
}

// redstoneStairRelayer relays power one block up and along +X only, standing in
// for the stepping geometry of redstone wire on a staircase.
type redstoneStairRelayer struct{}

func (redstoneStairRelayer) RedstoneSignalLoss(cube.Pos, *Tx) int { return 1 }
func (redstoneStairRelayer) RedstoneRelayerNeighbours(pos cube.Pos, _ *Tx) []cube.Pos {
	return []cube.Pos{{pos[0] + 1, pos[1] + 1, pos[2]}}
}
func (redstoneStairRelayer) EncodeBlock() (string, map[string]any) {
	return "test:redstone_stair_relayer", nil
}
func (redstoneStairRelayer) Hash() (uint64, uint64) { return 1 << 59, 0 }
func (redstoneStairRelayer) Model() BlockModel      { return redstoneCancellationModel{} }
