package enchantment

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// DepthStrider is a boot enchantment that increases underwater movement speed.
var DepthStrider depthStrider

type depthStrider struct{}

// Name ...
func (depthStrider) Name() string {
	return "Depth Strider"
}

// MaxLevel ...
func (depthStrider) MaxLevel() int {
	return 3
}

// Cost ...
func (depthStrider) Cost(level int) (int, int) {
	minCost := level * 10
	return minCost, minCost + 15
}

// Rarity ...
func (depthStrider) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (depthStrider) CompatibleWithEnchantment(item.EnchantmentType) bool {
	// TODO: Frost Walker
	return true
}

// CompatibleWithItem ...
func (depthStrider) CompatibleWithItem(i world.Item) bool {
	b, ok := i.(item.BootsType)
	return ok && b.Boots()
}
