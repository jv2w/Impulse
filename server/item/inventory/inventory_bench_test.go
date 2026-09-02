package inventory

import (
	"testing"

	"github.com/jv2w/Impulse/server/item"
)

// benchInv and benchStacks keep the benchmarked results alive so the calls are
// not optimised away.
var (
	benchInv    *Inventory
	benchStacks []item.Stack
)

// benchFilled returns an inventory of the size passed with every slot holding a
// stack of diamonds.
func benchFilled(size int) *Inventory {
	inv := New(size, nil)
	for i := range size {
		_ = inv.SetItem(i, diamonds(64))
	}
	return inv
}

func BenchmarkMerge(b *testing.B) {
	left, right := benchFilled(27), benchFilled(27)

	b.ReportAllocs()
	for range b.N {
		benchInv = left.Merge(right, nil)
	}
}

func BenchmarkClear(b *testing.B) {
	inv := benchFilled(36)
	slots := inv.Slots()

	b.ReportAllocs()
	for range b.N {
		benchStacks = inv.Clear()
		b.StopTimer()
		for i, it := range slots {
			_ = inv.SetItem(i, it)
		}
		b.StartTimer()
	}
}
