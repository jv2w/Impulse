package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// CopperTorch are non-solid blocks that emit light.
type CopperTorch struct {
	transparent
	empty

	// Facing is the direction from the torch to the block.
	Facing cube.Face
}

// BreakInfo ...
func (t CopperTorch) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// LightEmissionLevel ...
func (t CopperTorch) LightEmissionLevel() uint8 {
	return 14
}

// UseOnBlock ...
func (t CopperTorch) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if face == cube.FaceDown {
		return false
	}
	// The position behind the torch, and the position on each side tried below,
	// are each computed once. pos and face do not change in between, so the same
	// positions are tested in the same order.
	behind := pos.Side(face.Opposite())
	if !tx.Block(behind).Model().FaceSolid(behind, face, tx) {
		found := false
		for _, i := range []cube.Face{cube.FaceSouth, cube.FaceWest, cube.FaceNorth, cube.FaceEast, cube.FaceDown} {
			side := pos.Side(i)
			if tx.Block(side).Model().FaceSolid(side, i.Opposite(), tx) {
				found = true
				face = i.Opposite()
				break
			}
		}
		if !found {
			return false
		}
	}
	t.Facing = face.Opposite()

	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t CopperTorch) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	// The supporting position is computed once; pos and t.Facing do not change.
	side := pos.Side(t.Facing)
	if !tx.Block(side).Model().FaceSolid(side, t.Facing.Opposite(), tx) {
		breakBlock(t, pos, tx)
	}
}

// HasLiquidDrops ...
func (t CopperTorch) HasLiquidDrops() bool {
	return true
}

// EncodeItem ...
func (t CopperTorch) EncodeItem() (name string, meta int16) {
	return "minecraft:copper_torch", 0
}

// EncodeBlock ...
func (t CopperTorch) EncodeBlock() (name string, properties map[string]any) {
	var face string
	switch t.Facing {
	case cube.FaceDown:
		face = "top"
	case unknownFace:
		face = "unknown"
	default:
		face = t.Facing.String()
	}

	return "minecraft:copper_torch", map[string]any{"torch_facing_direction": face}
}

// allTorches ...
func allCopperTorches() (torch []world.Block) {
	// One torch per face, so the list is sized once instead of grown. Still a fresh
	// slice with the same blocks in the same order.
	faces := cube.Faces()
	torch = make([]world.Block, 0, len(faces))
	for _, face := range faces {
		if face == cube.FaceUp {
			face = unknownFace
		}

		torch = append(torch, CopperTorch{Facing: face})
	}
	return
}
