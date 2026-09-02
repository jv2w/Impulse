package block

import (
	"github.com/jv2w/Impulse/server/internal/nbtconv"
	"github.com/jv2w/Impulse/server/item"
)

// BannerPatternLayer is a wrapper over BannerPatternType with a colour property.
type BannerPatternLayer struct {
	// Type represents the type of banner pattern.
	Type BannerPatternType
	// Colour is the colour the pattern should be rendered in.
	Colour item.Colour
}

// EncodeNBT encodes the given BannerPatternLayer into an NBT map.
func (b BannerPatternLayer) EncodeNBT() map[string]any {
	return map[string]any{
		"Pattern": bannerPatternID(b.Type),
		"Color":   int32(invertColour(b.Colour)),
	}
}

// DecodeNBT decodes the given NBT map into a BannerPatternLayer and returns it. The bool returned is false if the map
// names a pattern this build does not know: every BannerPatternType stands for a different, real pattern, so there is
// no default to fall back on that would not write a pattern the save never held.
func (b BannerPatternLayer) DecodeNBT(data map[string]any) (BannerPatternLayer, bool) {
	pattern, exists := BannerPatternByID(nbtconv.String(data, "Pattern"))
	if !exists {
		return b, false
	}
	b.Type = pattern
	b.Colour = invertColourID(int16(nbtconv.Int32(data, "Color")))
	return b, true
}
