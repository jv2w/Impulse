package item

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Dye is an item that comes in 16 colours which allows you to colour blocks like concrete and sheep.
type Dye struct {
	// Colour is the colour of the dye.
	Colour Colour
}

// UseOnBlock implements the colouring behaviour of signs.
func (d Dye) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if dy, ok := tx.Block(pos).(dyeable); ok {
		if res, ok := dy.Dye(pos, user.Position(), d.Colour); ok {
			tx.SetBlock(pos, res, nil)
			ctx.SubtractFromCount(1)
			return true
		}
	}
	return false
}

// dyeable represents a block that may be dyed by clicking it with a dye item.
type dyeable interface {
	// Dye uses a dye with the Colour passed on the block. The resulting block is returned. A bool is returned to
	// indicate if dyeing the block was successful.
	Dye(pos cube.Pos, userPos mgl64.Vec3, c Colour) (world.Block, bool)
}

// dyeNames holds the encoded name of every dye colour, in colour ID order.
var dyeNames = [...]string{
	"minecraft:white_dye", "minecraft:orange_dye", "minecraft:magenta_dye", "minecraft:light_blue_dye",
	"minecraft:yellow_dye", "minecraft:lime_dye", "minecraft:pink_dye", "minecraft:gray_dye",
	"minecraft:light_gray_dye", "minecraft:cyan_dye", "minecraft:purple_dye", "minecraft:blue_dye",
	"minecraft:brown_dye", "minecraft:green_dye", "minecraft:red_dye", "minecraft:black_dye",
}

// EncodeItem ...
func (d Dye) EncodeItem() (name string, meta int16) {
	// The table holds exactly what the join produced for every colour ID it
	// covers, so a covered colour skips the string join and its allocation. An
	// ID outside the table still goes through the join, which Colour.String
	// answers with "white" as before.
	if c := d.Colour.Uint8(); int(c) < len(dyeNames) {
		return dyeNames[c], 0
	}
	return "minecraft:" + d.Colour.String() + "_dye", 0
}
