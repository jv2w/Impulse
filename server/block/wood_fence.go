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
	return woodFenceName(w.Wood), nil
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
	// blocks are appended in the same order into a slice still built fresh per call.
	woods := WoodTypes()
	fence = make([]world.Block, 0, len(woods))
	for _, w := range woods {
		fence = append(fence, WoodFence{Wood: w})
	}
	return
}
