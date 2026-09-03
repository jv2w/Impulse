package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Froglight is a luminous natural block that can be obtained if a frog eats a tiny magma cube.
type Froglight struct {
	solid

	// Type is the type of froglight.
	Type FroglightType
	// Axis is the axis which the froglight block faces.
	Axis cube.Axis
}

// LightEmissionLevel ...
func (f Froglight) LightEmissionLevel() uint8 {
	return 15
}

// UseOnBlock ...
func (f Froglight) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, f)
	if !used {
		return
	}
	f.Axis = face.Axis()

	place(tx, pos, f, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (f Froglight) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, oneOf(f))
}

// froglightName gives the encoded name of a froglight of a type.
var froglightName = enumNamer(FroglightTypes(), func(t FroglightType) string { return t.String() + "_froglight" })

// EncodeItem ...
func (f Froglight) EncodeItem() (name string, meta int16) {
	return froglightName(f.Type), 0
}

// EncodeBlock ...
func (f Froglight) EncodeBlock() (name string, properties map[string]any) {
	return froglightName(f.Type), map[string]any{"pillar_axis": f.Axis.String()}
}

// allFrogLight ...
func allFroglight() (froglight []world.Block) {
	// FroglightTypes does not depend on the axis and hands back a freshly built
	// slice on every call, so it is read once. The block list itself is still
	// built per call, only sized up front.
	axes, types := cube.Axes(), FroglightTypes()
	froglight = make([]world.Block, 0, len(axes)*len(types))
	for _, axis := range axes {
		for _, t := range types {
			froglight = append(froglight, Froglight{Type: t, Axis: axis})
		}
	}
	return
}
