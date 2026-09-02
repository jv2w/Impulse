package item

import "testing"

// The fixtures used here are the ones declared in stack_test.go.

var (
	benchEnchantments []Enchantment
	benchNBT          map[string]any
	benchComparable   bool
	benchStack        Stack
)

func benchEnchantedStack() Stack {
	return NewStack(Sword{Tier: ToolTierDiamond}, 1).
		WithCustomName("bench").
		WithLore("line one", "line two").
		WithEnchantments(
			NewEnchantment(enchA, 3),
			NewEnchantment(enchB, 5),
			NewEnchantment(enchSword, 1),
		)
}

func BenchmarkStackEnchantments(b *testing.B) {
	s := benchEnchantedStack()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchEnchantments = s.Enchantments()
	}
}

func BenchmarkWriteNBTDiskEnchanted(b *testing.B) {
	s := benchEnchantedStack()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchNBT = WriteNBT(s, true)
	}
}

func BenchmarkStackComparable(b *testing.B) {
	s := benchEnchantedStack()
	s2 := benchEnchantedStack()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchComparable = s.Comparable(s2)
	}
}

func BenchmarkStackWithEnchantmentsFresh(b *testing.B) {
	s := NewStack(Sword{Tier: ToolTierDiamond}, 1)
	e := NewEnchantment(enchA, 3)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchStack = s.WithEnchantments(e)
	}
}

func BenchmarkStackWithValue(b *testing.B) {
	s := NewStack(Stick{}, 1).WithValue("a", 1).WithValue("b", 2)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchStack = s.WithValue("c", 3)
	}
}
