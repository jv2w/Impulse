package effect

import (
	"github.com/jv2w/Impulse/server/world"
	"image/color"
)

// Saturation is a lasting effect that causes the affected player to regain
// food and saturation.
var Saturation saturation

type saturation struct {
	nopLasting
}

// Apply ...
func (saturation) Apply(e world.Entity, eff Effect) {
	if i, ok := e.(interface {
		Saturate(food int, saturation float64)
	}); ok {
		// Read the level once. Level only returns the level stored in the
		// Effect and eff is a local copy that nothing here writes to, so both
		// reads returned the same value.
		lvl := eff.Level()
		i.Saturate(lvl, 2*float64(lvl))
	}
}

// RGBA ...
func (saturation) RGBA() color.RGBA {
	return color.RGBA{R: 0xf8, G: 0x24, B: 0x23, A: 0xff}
}
