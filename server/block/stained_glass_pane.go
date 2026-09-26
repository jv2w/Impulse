package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// StainedGlassPane is a transparent block that can be used as a more efficient alternative to glass blocks.
type StainedGlassPane struct {
	transparent
	thin
	clicksAndSticks
	sourceWaterDisplacer

	// Colour specifies the colour of the block.
	Colour item.Colour
	// Connections holds the sides that the glass pane connects to.
	Connections Connections
}

// NeighbourUpdateTick ...
func (p StainedGlassPane) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if connections := calculateConnections(p.Model().(connector), tx, pos); connections != p.Connections {
		p.Connections = connections
		tx.SetBlock(pos, p, nil)
	}
}

// DeriveState ...
func (p StainedGlassPane) DeriveState(pos cube.Pos, src world.BlockSource) world.Block {
	p.Connections = calculateConnections(p.Model().(connector), src, pos)
	return p
}

// SideClosed ...
func (p StainedGlassPane) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (p StainedGlassPane) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(p))
}

// stainedGlassPaneName gives the encoded name of this block in a colour.
var stainedGlassPaneName = colourNamer(item.Colour.String, "_stained_glass_pane")

// EncodeItem ...
func (p StainedGlassPane) EncodeItem() (name string, meta int16) {
	return stainedGlassPaneName(p.Colour), 0
}

// EncodeBlock ...
func (p StainedGlassPane) EncodeBlock() (name string, properties map[string]any) {
	return stainedGlassPaneName(p.Colour), p.Connections.properties()
}

// allStainedGlassPane returns stained-glass panes with all possible colours.
func allStainedGlassPane() []world.Block {
	// coloursByID holds the same sixteen colours in the same order, so ranging over it appends the
	// same blocks without item.Colours() building a fresh slice. Each colour pairs with every
	// connection combination, colours outermost, so the order matches the state table.
	conns := allConnections()
	b := make([]world.Block, 0, 16*len(conns))
	for _, colour := range coloursByID {
		for _, c := range conns {
			b = append(b, StainedGlassPane{Colour: colour, Connections: c})
		}
	}
	return b
}
