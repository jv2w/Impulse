package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Sandstone is a solid block commonly found in deserts and beaches underneath sand.
type Sandstone struct {
	solid
	bassDrum

	// Type is the type of sandstone of the block.
	Type SandstoneType

	// Red specifies if the sandstone type is red or not. When set to true, the sandstone type will represent its
	// red variant, for example red sandstone.
	Red bool
}

// BreakInfo ...
func (s Sandstone) BreakInfo() BreakInfo {
	if s.Type == SmoothSandstone() {
		return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(s)).withBlastResistance(6)
	}
	return newBreakInfo(0.8, pickaxeHarvestable, pickaxeEffective, oneOf(s))
}

// sandstoneName gives the encoded name of a sandstone block of a type and red
// state. The joins that built the name on every encode run once at start
// instead. enumNamer prepends "minecraft:" on a hit and on a miss alike, so a
// type outside the table joins the same strings and panics in String the same
// way it did before.
var sandstoneName = func() func(Sandstone) string {
	types := SandstoneTypes()
	keys := make([]Sandstone, 0, len(types)*2)
	for _, t := range types {
		keys = append(keys, Sandstone{Type: t}, Sandstone{Type: t, Red: true})
	}
	return enumNamer(keys, func(s Sandstone) string {
		var prefix string
		if s.Type != NormalSandstone() {
			prefix = s.Type.String() + "_"
		}
		if s.Red {
			return prefix + "red_sandstone"
		}
		return prefix + "sandstone"
	})
}()

// EncodeItem ...
func (s Sandstone) EncodeItem() (name string, meta int16) {
	return sandstoneName(s), 0
}

// EncodeBlock ...
func (s Sandstone) EncodeBlock() (string, map[string]any) {
	return sandstoneName(s), nil
}

// SmeltInfo ...
func (s Sandstone) SmeltInfo() item.SmeltInfo {
	if s.Type == NormalSandstone() {
		return newSmeltInfo(item.NewStack(Sandstone{Red: s.Red, Type: SmoothSandstone()}, 1), 0.1)
	}
	return item.SmeltInfo{}
}

// allSandstones returns a list of all sandstone block variants.
func allSandstones() (c []world.Block) {
	// The two calls of the helper are spelled out, which drops the closure that captured c and lets
	// the slice be presized to its final length. The same blocks are appended in the same order:
	// every red variant first, then every non-red one, into a slice still built fresh per call.
	types := SandstoneTypes()
	c = make([]world.Block, 0, 2*len(types))
	for _, t := range types {
		c = append(c, Sandstone{Type: t, Red: true})
	}
	for _, t := range types {
		c = append(c, Sandstone{Type: t, Red: false})
	}
	return
}
