package block

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Placer represents an entity that is able to place a block at a specific position in the world.
type Placer interface {
	item.User
	PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext)
}
