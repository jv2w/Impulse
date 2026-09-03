package enchantment

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Efficiency is an enchantment that increases mining speed.
var Efficiency efficiency

type efficiency struct{}

// Name ...
func (efficiency) Name() string {
	return "Efficiency"
}

// MaxLevel ...
func (efficiency) MaxLevel() int {
	return 5
}

// Cost ...
func (efficiency) Cost(level int) (int, int) {
	minCost := 1 + 10*(level-1)
	return minCost, minCost + 50
}

// Rarity ...
func (efficiency) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityCommon
}

// Addend returns the mining speed addend from efficiency.
func (efficiency) Addend(level int) float64 {
	return float64(level*level + 1)
}

// CompatibleWithEnchantment ...
func (efficiency) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (efficiency) CompatibleWithItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	if !ok {
		return false
	}
	// ToolType is a constant accessor, so reading it once yields the same two
	// comparisons as the two reads it replaces.
	typ := t.ToolType()
	return typ != item.TypeSword && typ != item.TypeNone
}
