package enchantment

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Sharpness is an enchantment applied to a sword or axe that increases melee
// damage.
var Sharpness sharpness

type sharpness struct{}

// Name ...
func (sharpness) Name() string {
	return "Sharpness"
}

// MaxLevel ...
func (sharpness) MaxLevel() int {
	return 5
}

// Cost ...
func (sharpness) Cost(level int) (int, int) {
	minCost := 1 + (level-1)*11
	return minCost, minCost + 20
}

// Rarity ...
func (sharpness) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityCommon
}

// Addend returns the additional damage when attacking with sharpness.
func (sharpness) Addend(level int) float64 {
	return float64(level) * 1.25
}

// CompatibleWithEnchantment ...
func (sharpness) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (sharpness) CompatibleWithItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	if !ok {
		return false
	}
	// ToolType is a constant accessor, so reading it once yields the same two
	// comparisons as the two reads it replaces.
	typ := t.ToolType()
	return typ == item.TypeSword || typ == item.TypeAxe
}
