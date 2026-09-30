package effect

import (
	"github.com/jv2w/Impulse/server/world"
	"image/color"
)

// Absorption is a lasting effect that increases the health of an entity over
// the maximum. Once this extra health is lost, it will not regenerate.
var Absorption absorption

type absorption struct {
	nopLasting
}

// Start ...
func (absorption) Start(e world.Entity, lvl int) {
	if i, ok := e.(interface {
		SetAbsorption(health float64)
	}); ok {
		i.SetAbsorption(4 * float64(lvl))
	}
}

// End ...
func (absorption) End(e world.Entity, lvl int) {
	// Which input was the problem: absorption held from another source, such as
	// a totem or a second effect. Setting it to zero took all of it away, so
	// one absorption effect ending wiped the rest. Only what this level granted
	// is taken back, as vanilla does. SetAbsorption clamps at zero.
	if i, ok := e.(interface {
		Absorption() float64
		SetAbsorption(health float64)
	}); ok {
		i.SetAbsorption(i.Absorption() - 4*float64(lvl))
	}
}

// RGBA ...
func (absorption) RGBA() color.RGBA {
	return color.RGBA{R: 0x25, G: 0x52, B: 0xa5, A: 0xff}
}
