package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/portal"
)

// Portal is the translucent part of the nether portal.
type Portal struct {
	transparent

	// Axis is the axis which the portal faces.
	Axis cube.Axis
}

// Model ...
func (p Portal) Model() world.BlockModel {
	return model.Portal{Axis: p.Axis}
}

// LightEmissionLevel returns 11.
func (Portal) LightEmissionLevel() uint8 {
	return 11
}

// HasLiquidDrops ...
func (p Portal) HasLiquidDrops() bool {
	return false
}

// EncodeBlock ...
func (p Portal) EncodeBlock() (string, map[string]any) {
	return "minecraft:portal", map[string]any{"portal_axis": p.Axis.String()}
}

// NeighbourUpdateTick ...
func (p Portal) NeighbourUpdateTick(pos, neighbour cube.Pos, tx *world.Tx) {
	face, ok := pos.NeighbourFace(neighbour)
	if !ok {
		return
	}
	axis := face.Axis()
	if axis != cube.Y && axis != p.Axis {
		return
	}
	if n, ok := portal.NetherPortalFromPos(tx, pos); ok && n.Framed() && n.Activated() {
		return
	}
	portal.DeactivateNetherPortal(tx, pos)
}
