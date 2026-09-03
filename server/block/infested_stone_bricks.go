package block

import (
	"github.com/jv2w/Impulse/server/world"
)

// InfestedStoneBricks is a block that hides a silverfish. It looks identical to stone bricks.
// TODO: spawn a silverfish on break (without silk touch) once silverfish are implemented.
type InfestedStoneBricks struct {
	solid
	flute

	// Type is the type of stone bricks of the block.
	Type StoneBricksType
}

// BreakInfo ...
func (i InfestedStoneBricks) BreakInfo() BreakInfo {
	return newBreakInfo(0.75, pickaxeHarvestable, pickaxeEffective, silkTouchOnlyDrop(i)).withBlastResistance(0.75)
}

// infestedStoneBricksName gives the encoded name of infested stone bricks of a type.
var infestedStoneBricksName = enumNamer(StoneBricksTypes(), func(t StoneBricksType) string {
	return "infested_" + t.String()
})

// EncodeItem ...
func (i InfestedStoneBricks) EncodeItem() (name string, meta int16) {
	// enumNamer prepends "minecraft:" to the same suffix the concatenation built,
	// so the name is unchanged; it is now looked up instead of joined per call.
	return infestedStoneBricksName(i.Type), 0
}

// EncodeBlock ...
func (i InfestedStoneBricks) EncodeBlock() (string, map[string]any) {
	return infestedStoneBricksName(i.Type), nil
}

// allInfestedStoneBricks returns a list of all infested stone bricks variants.
func allInfestedStoneBricks() (s []world.Block) {
	// One block per type, so the final length is known before the loop.
	types := StoneBricksTypes()
	s = make([]world.Block, 0, len(types))
	for _, t := range types {
		s = append(s, InfestedStoneBricks{Type: t})
	}
	return
}
