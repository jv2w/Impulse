package block

// Air is the block present in otherwise empty space.
type Air struct {
	empty
	replaceable
	transparent
}

// HasLiquidDrops ...
func (Air) HasLiquidDrops() bool {
	return false
}

// PortalInterior returns true if air may occupy the inside of a nether portal frame before activation.
func (Air) PortalInterior() bool {
	return true
}

// EncodeItem ...
func (Air) EncodeItem() (name string, meta int16) {
	return "minecraft:air", 0
}

// EncodeBlock ...
func (Air) EncodeBlock() (string, map[string]any) {
	return "minecraft:air", nil
}
