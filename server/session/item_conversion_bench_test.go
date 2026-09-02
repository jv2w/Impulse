package session

import (
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// BenchmarkStackFromItem measures the conversion that runs once per item stack
// sent to a client, for a block item and for a plain one.
func BenchmarkStackFromItem(b *testing.B) {
	br := sharedRegistry()

	for _, bc := range []struct {
		name string
		it   item.Stack
	}{
		{"Block", item.NewStack(block.Stone{}, 64)},
		{"Item", item.NewStack(item.Diamond{}, 1)},
	} {
		b.Run(bc.name, func(b *testing.B) {
			var st protocol.ItemStack
			b.ReportAllocs()
			for range b.N {
				st = stackFromItem(br, bc.it)
			}
			if st.Count == 0 {
				b.Fatal("stack is empty")
			}
		})
	}
}
