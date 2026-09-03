package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// NetherBricks are blocks used to form nether fortresses in the Nether.
// Red Nether bricks, Cracked Nether bricks and Chiseled Nether bricks are decorative variants that do not naturally generate.
type NetherBricks struct {
	solid
	bassDrum

	// NetherBricksType is the type of nether bricks of the block.
	Type NetherBricksType
}

// BreakInfo ...
func (n NetherBricks) BreakInfo() BreakInfo {
	return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(n)).withBlastResistance(6)
}

// SmeltInfo ...
func (n NetherBricks) SmeltInfo() item.SmeltInfo {
	if n.Type == NormalNetherBricks() {
		return newSmeltInfo(item.NewStack(NetherBricks{Type: CrackedNetherBricks()}, 1), 0.1)
	}
	return item.SmeltInfo{}
}

// netherBricksName gives the encoded name of nether bricks of a type.
var netherBricksName = enumNamer(NetherBricksTypes(), NetherBricksType.String)

// EncodeItem ...
func (n NetherBricks) EncodeItem() (id string, meta int16) {
	// enumNamer prepends "minecraft:" to the same String the concatenation used,
	// so the name is unchanged; it is now looked up instead of joined per call.
	return netherBricksName(n.Type), 0
}

// EncodeBlock ...
func (n NetherBricks) EncodeBlock() (name string, properties map[string]any) {
	return netherBricksName(n.Type), nil
}

// allNetherBricks returns a list of all nether bricks variants.
func allNetherBricks() (netherBricks []world.Block) {
	// One block per type, so the final length is known before the loop.
	types := NetherBricksTypes()
	netherBricks = make([]world.Block, 0, len(types))
	for _, t := range types {
		netherBricks = append(netherBricks, NetherBricks{Type: t})
	}
	return
}
