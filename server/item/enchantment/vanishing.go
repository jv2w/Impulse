package enchantment

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// CurseOfVanishing is an enchantment that causes the item to disappear on
// death.
var CurseOfVanishing curseOfVanishing

type curseOfVanishing struct{}

// Name ...
func (curseOfVanishing) Name() string {
	return "Curse of Vanishing"
}

// MaxLevel ...
func (curseOfVanishing) MaxLevel() int {
	return 1
}

// Cost ...
func (curseOfVanishing) Cost(int) (int, int) {
	return 25, 50
}

// Rarity ...
func (curseOfVanishing) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityVeryRare
}

// CompatibleWithEnchantment ...
func (curseOfVanishing) CompatibleWithEnchantment(_ item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (curseOfVanishing) CompatibleWithItem(i world.Item) bool {
	// Type assertions have no side effects, so returning at the first match
	// gives the same value as arm || com || dur || rec while skipping the
	// remaining assertions.
	if _, arm := i.(item.Armour); arm {
		return true
	}
	if _, com := i.(item.Compass); com {
		return true
	}
	if _, dur := i.(item.Durable); dur {
		return true
	}
	_, rec := i.(item.RecoveryCompass)
	// TODO: Carrot on a Stick
	// TODO: Warped Fungus on a Stick
	return rec
}

// Treasure ...
func (curseOfVanishing) Treasure() bool {
	return true
}

// Curse ...
func (curseOfVanishing) Curse() bool {
	return true
}
