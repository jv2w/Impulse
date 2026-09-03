package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Lantern is a light emitting block.
type Lantern struct {
	transparent
	sourceWaterDisplacer

	// Hanging determines if a lantern is hanging off a block.
	Hanging bool
	// Type of fire lighting the lantern.
	Type FireType
}

// Model ...
func (l Lantern) Model() world.BlockModel {
	return model.Lantern{Hanging: l.Hanging}
}

// NeighbourUpdateTick ...
func (l Lantern) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if l.Hanging {
		up := pos.Side(cube.FaceUp)
		// Nothing runs between the two reads of the block above, so one lookup
		// answers both the type check and the model check.
		above := tx.Block(up)
		if _, ok := above.(IronChain); !ok && !above.Model().FaceSolid(up, cube.FaceDown, tx) {
			breakBlock(l, pos, tx)
		}
	} else {
		down := pos.Side(cube.FaceDown)
		if !tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx) {
			breakBlock(l, pos, tx)
		}
	}
}

// LightEmissionLevel ...
func (l Lantern) LightEmissionLevel() uint8 {
	return l.Type.LightLevel()
}

// UseOnBlock ...
func (l Lantern) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, l)
	if !used {
		return false
	}
	if face == cube.FaceDown {
		upPos := pos.Side(cube.FaceUp)
		// Nothing runs between the two reads of the block above, so one lookup
		// answers both the type check and the model check.
		upBlock := tx.Block(upPos)
		if _, ok := upBlock.(IronChain); !ok && !upBlock.Model().FaceSolid(upPos, cube.FaceDown, tx) {
			face = cube.FaceUp
		}
	}
	if face != cube.FaceDown {
		downPos := pos.Side(cube.FaceDown)
		if !tx.Block(downPos).Model().FaceSolid(downPos, cube.FaceUp, tx) {
			return false
		}
	}
	l.Hanging = face == cube.FaceDown

	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// SideClosed ...
func (l Lantern) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (l Lantern) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(l))
}

// EncodeItem ...
func (l Lantern) EncodeItem() (name string, meta int16) {
	switch l.Type {
	case NormalFire():
		return "minecraft:lantern", 0
	case SoulFire():
		return "minecraft:soul_lantern", 0
	}
	panic("invalid fire type")
}

// EncodeBlock ...
func (l Lantern) EncodeBlock() (name string, properties map[string]any) {
	switch l.Type {
	case NormalFire():
		return "minecraft:lantern", map[string]any{"hanging": l.Hanging}
	case SoulFire():
		return "minecraft:soul_lantern", map[string]any{"hanging": l.Hanging}
	}
	panic("invalid fire type")
}

// allLanterns ...
func allLanterns() (lanterns []world.Block) {
	// Two lanterns are appended per fire type, so the final length is known.
	types := FireTypes()
	lanterns = make([]world.Block, 0, len(types)*2)
	for _, f := range types {
		lanterns = append(lanterns, Lantern{Hanging: false, Type: f})
		lanterns = append(lanterns, Lantern{Hanging: true, Type: f})
	}
	return
}
