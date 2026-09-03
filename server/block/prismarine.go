package block

import (
	"github.com/jv2w/Impulse/server/world"
)

// Prismarine is a type of stone that appears underwater in ruins and ocean monuments.
type Prismarine struct {
	solid
	bassDrum

	// Type is the type of prismarine of the block.
	Type PrismarineType
}

// BreakInfo ...
func (p Prismarine) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(p)).withBlastResistance(6)
}

// prismarineName gives the encoded name of a prismarine type.
var prismarineName = enumNamer(PrismarineTypes(), PrismarineType.String)

// EncodeItem ...
func (p Prismarine) EncodeItem() (id string, meta int16) {
	// The table holds "minecraft:" + Type.String() for every registered type, so the string is the
	// same one the join produced, without allocating. An unregistered type still joins and panics
	// in String the same way.
	return prismarineName(p.Type), 0
}

// EncodeBlock ...
func (p Prismarine) EncodeBlock() (name string, properties map[string]any) {
	// Same table as EncodeItem, so the name is identical and no longer allocated per call.
	return prismarineName(p.Type), nil
}

// allPrismarine returns a list of all prismarine block variants.
func allPrismarine() (c []world.Block) {
	// The final count is known, so the slice is presized and grows without reallocating. The same
	// blocks are appended in the same order into a slice still built fresh per call.
	types := PrismarineTypes()
	c = make([]world.Block, 0, len(types))
	for _, t := range types {
		c = append(c, Prismarine{Type: t})
	}
	return
}
