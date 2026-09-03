package block

import "github.com/jv2w/Impulse/server/item"

// PolishedBlackstoneBrick are a brick variant of polished blackstone and can spawn in bastion remnants and ruined portals.
type PolishedBlackstoneBrick struct {
	solid
	bassDrum

	// Cracked specifies if the polished blackstone bricks is its cracked variant.
	Cracked bool
}

// BreakInfo ...
func (b PolishedBlackstoneBrick) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(b)).withBlastResistance(6)
}

// SmeltInfo ...
func (b PolishedBlackstoneBrick) SmeltInfo() item.SmeltInfo {
	if b.Cracked {
		return item.SmeltInfo{}
	}
	return newSmeltInfo(item.NewStack(PolishedBlackstoneBrick{Cracked: true}, 1), 0.1)
}

// EncodeItem ...
func (b PolishedBlackstoneBrick) EncodeItem() (name string, meta int16) {
	// Only two names can ever come out of the joins below, so both are spelled out as constants.
	// That drops the string allocation the concatenation made on every call.
	if b.Cracked {
		return "minecraft:cracked_polished_blackstone_bricks", 0
	}
	return "minecraft:polished_blackstone_bricks", 0
}

// EncodeBlock ...
func (b PolishedBlackstoneBrick) EncodeBlock() (string, map[string]any) {
	// Same two constant names as EncodeItem, so the concatenation and its allocation are gone.
	if b.Cracked {
		return "minecraft:cracked_polished_blackstone_bricks", nil
	}
	return "minecraft:polished_blackstone_bricks", nil
}
