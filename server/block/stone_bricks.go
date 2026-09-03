package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// StoneBricks are materials found in structures such as strongholds, igloo basements, jungle temples, ocean ruins
// and ruined portals.
type StoneBricks struct {
	solid
	bassDrum

	// Type is the type of stone bricks of the block.
	Type StoneBricksType
}

// BreakInfo ...
func (s StoneBricks) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(s)).withBlastResistance(6)
}

// SmeltInfo ...
func (s StoneBricks) SmeltInfo() item.SmeltInfo {
	if s.Type == NormalStoneBricks() {
		return newSmeltInfo(item.NewStack(StoneBricks{Type: CrackedStoneBricks()}, 1), 0.1)
	}
	return item.SmeltInfo{}
}

// stoneBricksName gives the encoded name of stone bricks of a type.
var stoneBricksName = enumNamer(StoneBricksTypes(), StoneBricksType.String)

// EncodeItem ...
func (s StoneBricks) EncodeItem() (name string, meta int16) {
	return stoneBricksName(s.Type), 0
}

// EncodeBlock ...
func (s StoneBricks) EncodeBlock() (string, map[string]any) {
	return stoneBricksName(s.Type), nil
}

// allStoneBricks returns a list of all stoneBricks block variants.
func allStoneBricks() (s []world.Block) {
	// The final count is known, so the slice is presized and grows without reallocating. The same
	// blocks are appended in the same order into a slice still built fresh per call.
	types := StoneBricksTypes()
	s = make([]world.Block, 0, len(types))
	for _, t := range types {
		s = append(s, StoneBricks{Type: t})
	}
	return
}
