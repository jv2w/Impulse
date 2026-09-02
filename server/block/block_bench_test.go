package block_test

import (
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
)

var benchEmpty bool

// BenchmarkWoolStackEmpty times item.Stack.Empty on a coloured block, which is
// the call that reaches EncodeItem on nearly every method of Stack.
func BenchmarkWoolStackEmpty(b *testing.B) {
	s := item.NewStack(block.Wool{Colour: item.ColourRed()}, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchEmpty = s.Empty()
	}
}
