package item

// BannerPattern is an item used to customise banners inside looms.
type BannerPattern struct {
	// Type represents the type of banner pattern. These types do not include all patterns that can be applied to a
	// banner.
	Type BannerPatternType
}

// MaxCount always returns 1.
func (b BannerPattern) MaxCount() int {
	return 1
}

// bannerPatternNames holds the encoded item name of every BannerPatternType,
// indexed by its Uint8 value (0..9, contiguous), so that EncodeItem stops
// concatenating one on every call. A type outside the table falls back to
// that concatenation.
var bannerPatternNames = func() []string {
	patterns := BannerPatterns()
	names := make([]string, len(patterns))
	for _, p := range patterns {
		names[p.Uint8()] = "minecraft:" + p.String() + "_banner_pattern"
	}
	return names
}()

// EncodeItem ...
func (b BannerPattern) EncodeItem() (name string, meta int16) {
	// The table entry is the exact string the fallback builds for every type
	// BannerPatterns returns; any other value takes the fallback as before.
	if t := b.Type.Uint8(); int(t) < len(bannerPatternNames) {
		return bannerPatternNames[t], 0
	}
	return "minecraft:" + b.Type.String() + "_banner_pattern", 0
}
