package enchantment

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// SilkTouch is an enchantment that allows many blocks to drop themselves
// instead of their usual items when mined.
var SilkTouch silkTouch

type silkTouch struct{}

// Name ...
func (silkTouch) Name() string {
	return "Silk Touch"
}

// MaxLevel ...
func (silkTouch) MaxLevel() int {
	return 1
}

// Cost ...
func (silkTouch) Cost(int) (int, int) {
	return 15, 65
}

// Rarity ...
func (silkTouch) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityVeryRare
}

// CompatibleWithEnchantment ...
func (silkTouch) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Fortune
}

// CompatibleWithItem ...
func (silkTouch) CompatibleWithItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	if !ok {
		return false
	}
	// ToolType is a constant accessor, so reading it once yields the same two
	// comparisons as the two reads it replaces.
	typ := t.ToolType()
	return typ != item.TypeSword && typ != item.TypeNone
}
