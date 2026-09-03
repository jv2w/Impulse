package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Concrete is a solid block which comes in the 16 regular dye colors, created by placing concrete powder
// adjacent to water.
type Concrete struct {
	solid
	bassDrum

	// Colour is the colour of the concrete block.
	Colour item.Colour
}

// BreakInfo ...
func (c Concrete) BreakInfo() BreakInfo {
	return newBreakInfo(1.8, pickaxeHarvestable, pickaxeEffective, oneOf(c))
}

// concreteName gives the encoded name of this block in a colour.
var concreteName = colourNamer(item.Colour.String, "_concrete")

// EncodeItem ...
func (c Concrete) EncodeItem() (name string, meta int16) {
	return concreteName(c.Colour), 0
}

// EncodeBlock ...
func (c Concrete) EncodeBlock() (name string, properties map[string]any) {
	return concreteName(c.Colour), nil
}

// allConcrete returns concrete blocks with all possible colours.
func allConcrete() []world.Block {
	b := make([]world.Block, 0, 16)
	// coloursByID holds the same sixteen colours in the same order, so ranging over
	// it drops the freshly built list. Colours are values, so the copy is the same.
	for _, c := range coloursByID {
		b = append(b, Concrete{Colour: c})
	}
	return b
}
