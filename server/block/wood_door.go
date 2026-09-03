package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
	"time"
)

// WoodDoor is a block that can be used as an openable 1x2 barrier.
type WoodDoor struct {
	transparent
	bass
	sourceWaterDisplacer

	// Wood is the type of wood of the door. This field must have one of the values found in the material
	// package.
	Wood WoodType
	// Facing is the direction that the door opens towards. When closed, the door sits on the side of its
	// block on the opposite direction.
	Facing cube.Direction
	// Open is whether the door is open.
	Open bool
	// Top is whether the block is the top or bottom half of a door.
	Top bool
	// Right is whether the door hinge is on the right side.
	Right bool
}

// FlammabilityInfo ...
func (d WoodDoor) FlammabilityInfo() FlammabilityInfo {
	if !d.Wood.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(0, 0, true)
}

// FuelInfo ...
func (d WoodDoor) FuelInfo() item.FuelInfo {
	if !d.Wood.Flammable() {
		return item.FuelInfo{}
	}
	return newFuelInfo(time.Second * 10)
}

// Model ...
func (d WoodDoor) Model() world.BlockModel {
	return model.Door{Facing: d.Facing, Open: d.Open, Right: d.Right}
}

// NeighbourUpdateTick ...
func (d WoodDoor) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	// pos.Side(cube.FaceDown) is pure and both branches need it, the lower one twice; one value
	// serves every use.
	below := pos.Side(cube.FaceDown)
	if d.Top {
		if _, ok := tx.Block(below).(WoodDoor); !ok {
			breakBlockNoDrops(d, pos, tx)
		}
	} else if solid := tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx); !solid {
		breakBlock(d, pos, tx)
	} else if _, ok := tx.Block(pos.Side(cube.FaceUp)).(WoodDoor); !ok {
		breakBlockNoDrops(d, pos, tx)
	}
}

// UseOnBlock handles the directional placing of doors
func (d WoodDoor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if face != cube.FaceUp {
		// Doors can only be placed when clicking the top face.
		return false
	}
	below := pos
	pos = pos.Side(cube.FaceUp)
	if !replaceableWith(tx, pos, d) || !replaceableWith(tx, pos.Side(cube.FaceUp), d) {
		return false
	}
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	d.Facing = user.Rotation().Direction()
	left := tx.Block(pos.Side(d.Facing.RotateLeft().Face()))
	right := tx.Block(pos.Side(d.Facing.RotateRight().Face()))
	if _, ok := left.Model().(model.Door); ok {
		d.Right = true
	}
	// The side the door hinge is on can be affected by the blocks to the left and right of the door. In particular,
	// opaque blocks on the right side of the door with transparent blocks on the left side result in a right sided
	// door hinge.
	if diffuser, ok := right.(LightDiffuser); !ok || diffuser.LightDiffusionLevel() != 0 {
		if diffuser, ok := left.(LightDiffuser); ok && diffuser.LightDiffusionLevel() == 0 {
			d.Right = true
		}
	}

	ctx.IgnoreBBox = true
	place(tx, pos, d, user, ctx)
	place(tx, pos.Side(cube.FaceUp), WoodDoor{Wood: d.Wood, Facing: d.Facing, Top: true, Right: d.Right}, user, ctx)
	ctx.CountSub = 1
	return placed(ctx)
}

// Activate ...
func (d WoodDoor) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	d.Open = !d.Open
	tx.SetBlock(pos, d, nil)

	otherPos := pos.Side(cube.Face(boolByte(!d.Top)))
	other := tx.Block(otherPos)
	if door, ok := other.(WoodDoor); ok {
		door.Open = d.Open
		tx.SetBlock(otherPos, door, nil)
	}
	if d.Open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: d})
		return true
	}
	tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: d})
	return true
}

// BreakInfo ...
func (d WoodDoor) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, axeEffective, oneOf(d))
}

// SideClosed ...
func (d WoodDoor) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// woodDoorName gives the encoded name of a door of a wood type, oak keeping its
// own legacy name. The join runs once at start instead of on every encode, and
// enumNamer joins the same strings on a miss, so a wood type outside the table
// panics in String exactly as it did before.
var woodDoorName = enumNamer(WoodTypes(), func(w WoodType) string {
	if w == OakWood() {
		return "wooden_door"
	}
	return w.String() + "_door"
})

// EncodeItem ...
func (d WoodDoor) EncodeItem() (name string, meta int16) {
	return woodDoorName(d.Wood), 0
}

// EncodeBlock ...
func (d WoodDoor) EncodeBlock() (name string, properties map[string]any) {
	// Both branches built the same property map; only the name differed.
	return woodDoorName(d.Wood), map[string]any{"minecraft:cardinal_direction": d.Facing.RotateRight().String(), "door_hinge_bit": d.Right, "open_bit": d.Open, "upper_block_bit": d.Top}
}

// allDoors returns a list of all door types
func allDoors() (doors []world.Block) {
	// Eight doors per direction and four directions per wood type, so the slice is presized to its
	// final length and grows without reallocating. The same blocks are appended in the same order.
	woods := WoodTypes()
	doors = make([]world.Block, 0, len(woods)*4*8)
	for _, w := range woods {
		for i := cube.Direction(0); i <= 3; i++ {
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: false, Top: false, Right: false})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: false, Top: true, Right: false})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: true, Top: true, Right: false})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: true, Top: false, Right: false})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: false, Top: false, Right: true})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: false, Top: true, Right: true})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: true, Top: true, Right: true})
			doors = append(doors, WoodDoor{Wood: w, Facing: i, Open: true, Top: false, Right: true})
		}
	}
	return
}
