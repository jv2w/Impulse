package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
)

// Wool is a colourful block that can be obtained by killing/shearing sheep, or crafted using four string.
type Wool struct {
	solid

	// Colour is the colour of the wool.
	Colour item.Colour
}

// Instrument ...
func (w Wool) Instrument() sound.Instrument {
	return sound.Guitar()
}

// FlammabilityInfo ...
func (w Wool) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// BreakInfo ...
func (w Wool) BreakInfo() BreakInfo {
	return newBreakInfo(0.8, alwaysHarvestable, shearsEffective, oneOf(w))
}

// woolName gives the encoded name of this block in a colour.
var woolName = colourNamer(item.Colour.String, "_wool")

// EncodeItem ...
func (w Wool) EncodeItem() (name string, meta int16) {
	return woolName(w.Colour), 0
}

// EncodeBlock ...
func (w Wool) EncodeBlock() (name string, properties map[string]any) {
	return woolName(w.Colour), nil
}

// allWool returns wool blocks with all possible colours.
func allWool() []world.Block {
	// coloursByID holds the same sixteen colours in the same order, so ranging over it appends the
	// same blocks without item.Colours() building a fresh slice.
	b := make([]world.Block, 0, 16)
	for _, c := range coloursByID {
		b = append(b, Wool{Colour: c})
	}
	return b
}
