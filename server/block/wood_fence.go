package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"time"
)

// WoodFence are blocks similar to Walls, which cannot normally be jumped over. Unlike walls however,
// they allow the player (but not mobs) to see through them, making for excellent barriers.
type WoodFence struct {
	transparent
	bass
	sourceWaterDisplacer

	// Wood is the type of wood of the fence. This field must have one of the values found in the wood
	// package.
	Wood WoodType
	// Connections holds the sides that the fence connects to.
	Connections Connections
}

// NeighbourUpdateTick ...
func (w WoodFence) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if connections := calculateConnections(w.Model().(connector), tx, pos); connections != w.Connections {
		w.Connections = connections
		tx.SetBlock(pos, w, nil)
	}
}

// DeriveState ...
func (w WoodFence) DeriveState(pos cube.Pos, src world.BlockSource) world.Block {
	w.Connections = calculateConnections(w.Model().(connector), src, pos)
	return w
}

// BreakInfo ...
func (w WoodFence) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(w)).withBlastResistance(3)
}

// SideClosed ...
func (WoodFence) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// FlammabilityInfo ...
func (w WoodFence) FlammabilityInfo() FlammabilityInfo {
	if !w.Wood.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(5, 20, true)
}

// FuelInfo ...
func (w WoodFence) FuelInfo() item.FuelInfo {
	if !w.Wood.Flammable() {
		return item.FuelInfo{}
	}
	return newFuelInfo(time.Second * 15)
}

// EncodeBlock ...
func (w WoodFence) EncodeBlock() (name string, properties map[string]any) {
	return woodFenceName(w.Wood), w.Connections.properties()
}

// Model ...
func (w WoodFence) Model() world.BlockModel {
	return model.Fence{Wood: true}
}

// woodFenceName gives the encoded name of a fence of a wood type.
var woodFenceName = enumNamer(WoodTypes(), func(w WoodType) string { return w.String() + "_fence" })

// EncodeItem ...
func (w WoodFence) EncodeItem() (name string, meta int16) {
	return woodFenceName(w.Wood), 0
}

// allFence ...
func allFence() (fence []world.Block) {
	// The final count is known, so the slice is presized and grows without reallocating. The same
	// blocks are appended in the same order into a slice still built fresh per call: every wood
	// pairs with every connection combination, woods outermost.
	woods, conns := WoodTypes(), allConnections()
	fence = make([]world.Block, 0, len(woods)*len(conns))
	for _, w := range woods {
		for _, c := range conns {
			fence = append(fence, WoodFence{Wood: w, Connections: c})
		}
	}
	return
}
