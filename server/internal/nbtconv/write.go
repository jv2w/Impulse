package nbtconv

import (
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// WriteBlock encodes a world.Block into a map that can be encoded using NBT.
func WriteBlock(b world.Block) map[string]any {
	name, properties := b.EncodeBlock()
	return map[string]any{
		"name":    name,
		"states":  properties,
		"version": chunk.CurrentBlockVersion,
	}
}
