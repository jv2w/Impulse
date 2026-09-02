package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// StainedGlass is a decorative, fully transparent solid block that is dyed into a different colour.
type StainedGlass struct {
	transparent
	solid
	clicksAndSticks

	// Colour specifies the colour of the block.
	Colour item.Colour
}

// BreakInfo ...
func (g StainedGlass) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(g))
}

// stainedGlassName gives the encoded name of this block in a colour.
var stainedGlassName = colourNamer(item.Colour.String, "_stained_glass")

// EncodeItem ...
func (g StainedGlass) EncodeItem() (name string, meta int16) {
	return stainedGlassName(g.Colour), 0
}

// EncodeBlock ...
func (g StainedGlass) EncodeBlock() (name string, properties map[string]any) {
	return stainedGlassName(g.Colour), nil
}

// allStainedGlass returns stained-glass blocks with all possible colours.
func allStainedGlass() []world.Block {
	b := make([]world.Block, 0, 16)
	for _, c := range item.Colours() {
		b = append(b, StainedGlass{Colour: c})
	}
	return b
}
