package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/portal"
)

// EndPortal is the translucent block of an End portal. It is created by inserting an Eye of Ender into all twelve frame
// blocks of an End portal frame ring.
type EndPortal struct {
	transparent
}

// Model ...
func (EndPortal) Model() world.BlockModel {
	return model.Empty{}
}

// LightEmissionLevel returns 15.
func (EndPortal) LightEmissionLevel() uint8 {
	return 15
}

// HasLiquidDrops ...
func (EndPortal) HasLiquidDrops() bool {
	return false
}

// EncodeNBT encodes the End portal block actor.
func (EndPortal) EncodeNBT() map[string]any {
	return map[string]any{"id": "EndPortal"}
}

// DecodeNBT decodes the End portal block actor.
func (e EndPortal) DecodeNBT(map[string]any) any {
	return e
}

// NeighbourUpdateTick removes the connected portal blocks if the surrounding frame ring is no longer complete,
// like breaking the frame of a nether portal.
func (EndPortal) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if portal.EndPortalRingIntact(tx, pos) {
		return
	}
	portal.DeactivateEndPortal(tx, pos)
}

// EncodeBlock ...
func (EndPortal) EncodeBlock() (string, map[string]any) {
	return "minecraft:end_portal", nil
}
