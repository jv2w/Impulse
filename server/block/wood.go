package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"time"
)

// Wood is a block that has the log's "bark" texture on all six sides. It comes in 8 types: oak, spruce, birch, jungle,
// acacia, dark oak, crimson, and warped.
// Stripped wood is a variant obtained by using an axe on the wood.
type Wood struct {
	solid
	bass

	// Wood is the type of wood.
	Wood WoodType
	// Stripped specifies if the wood is stripped or not.
	Stripped bool
	// Axis is the axis which the wood block faces.
	Axis cube.Axis
}

// FlammabilityInfo ...
func (w Wood) FlammabilityInfo() FlammabilityInfo {
	if !w.Wood.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(5, 5, true)
}

// BreakInfo ...
func (w Wood) BreakInfo() BreakInfo {
	return newBreakInfo(2.0, alwaysHarvestable, axeEffective, oneOf(w))
}

// SmeltInfo ...
func (Wood) SmeltInfo() item.SmeltInfo {
	return newSmeltInfo(item.NewStack(item.Charcoal{}, 1), 0.15)
}

// FuelInfo ...
func (w Wood) FuelInfo() item.FuelInfo {
	if !w.Wood.Flammable() {
		return item.FuelInfo{}
	}
	return newFuelInfo(time.Second * 15)
}

// UseOnBlock ...
func (w Wood) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, w)
	if !used {
		return
	}
	w.Axis = face.Axis()

	place(tx, pos, w, user, ctx)
	return placed(ctx)
}

// Strip ...
func (w Wood) Strip() (world.Block, world.Sound, bool) {
	return Wood{Axis: w.Axis, Wood: w.Wood, Stripped: true}, nil, !w.Stripped
}

// woodKey identifies a wood block name by wood type and stripped state. The
// axis does not appear in the name.
type woodKey struct {
	wood     WoodType
	stripped bool
}

// woodName gives the encoded name of a wood block of a wood type and stripped
// state. The joins run once at start instead of on every encode, and enumNamer
// joins the same strings on a miss, so a wood type outside the table panics in
// String exactly as it did before.
var woodName = func() func(woodKey) string {
	woods := WoodTypes()
	keys := make([]woodKey, 0, len(woods)*2)
	for _, w := range woods {
		keys = append(keys, woodKey{w, false}, woodKey{w, true})
	}
	return enumNamer(keys, func(k woodKey) string {
		suffix := "_wood"
		switch k.wood {
		case CrimsonWood(), WarpedWood():
			suffix = "_hyphae"
		}
		if k.stripped {
			return "stripped_" + k.wood.String() + suffix
		}
		return k.wood.String() + suffix
	})
}()

// EncodeItem ...
func (w Wood) EncodeItem() (name string, meta int16) {
	return woodName(woodKey{w.Wood, w.Stripped}), 0
}

// EncodeBlock ...
func (w Wood) EncodeBlock() (name string, properties map[string]any) {
	// The four branches only differed in the name; the property map was the same in all of them.
	return woodName(woodKey{w.Wood, w.Stripped}), map[string]any{"pillar_axis": w.Axis.String()}
}

// allWood returns all possible Wood block states, excluding bamboo blocks,
// which are registered separately through allBambooBlocks.
func allWood() (wood []world.Block) {
	// Three axes with two stripped states per wood type, so the slice is presized. Bamboo is
	// skipped, so the buffer holds six spare entries; capacity only sizes the buffer and the same
	// blocks are appended in the same order.
	woods := WoodTypes()
	wood = make([]world.Block, 0, len(woods)*6)
	for _, w := range woods {
		if w == BambooWood() {
			continue
		}
		for axis := cube.Axis(0); axis < 3; axis++ {
			wood = append(wood, Wood{Axis: axis, Stripped: true, Wood: w})
			wood = append(wood, Wood{Axis: axis, Stripped: false, Wood: w})
		}
	}
	return
}
