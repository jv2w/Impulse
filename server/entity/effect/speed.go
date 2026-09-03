package effect

import (
	"github.com/jv2w/Impulse/server/world"
	"image/color"
)

// Speed is a lasting effect that increases the speed of an entity by 20% for
// each level that the effect has.
var Speed speed

type speed struct {
	nopLasting
}

// Start ...
func (speed) Start(e world.Entity, lvl int) {
	if l, ok := e.(living); ok {
		// The multiplier is only used here, so computing it inside the branch
		// skips it for entities that are not living. The expression and the
		// order of its float operations are unchanged.
		speed := 1 + float64(lvl)*0.2
		l.SetSpeed(l.Speed() * speed)
	}
}

// End ...
func (speed) End(e world.Entity, lvl int) {
	if l, ok := e.(living); ok {
		// Same as in Start: the multiplier is dead unless the entity is living.
		speed := 1 + float64(lvl)*0.2
		l.SetSpeed(l.Speed() / speed)
	}
}

// RGBA ...
func (speed) RGBA() color.RGBA {
	return color.RGBA{R: 0x33, G: 0xeb, B: 0xff, A: 0xff}
}
