package block_test

import (
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
)

var benchDuration time.Duration

func BenchmarkBreakDurationStone(b *testing.B) {
	pick := item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1)
	bl := block.Stone{}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchDuration = block.BreakDuration(bl, pick, block.BreakContext{})
	}
}

func BenchmarkBreakDurationWall(b *testing.B) {
	pick := item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1)
	bl := block.Wall{Block: block.Cobblestone{}}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchDuration = block.BreakDuration(bl, pick, block.BreakContext{})
	}
}
