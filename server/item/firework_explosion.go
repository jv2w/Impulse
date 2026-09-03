package item

// FireworkExplosion represents an explosion of a firework.
type FireworkExplosion struct {
	// Shape represents the shape of the explosion.
	Shape FireworkShape
	// Colour is the colour of the explosion.
	Colour Colour
	// Fade is the colour the explosion should fade into. Fades must be set to true in order for this to function.
	Fade Colour
	// Fades is true if the explosion should fade into the fade colour.
	Fades bool
	// Twinkle is true if the explosion should twinkle on explode.
	Twinkle bool
	// Trail is true if the explosion should have a trail.
	Trail bool
}

// fireworkShapes holds the same shapes FireworkShapes returns, in the same order and with the
// same length, so a lookup by ID does not have to build the slice first.
var fireworkShapes = [5]FireworkShape{FireworkShapeSmallSphere(), FireworkShapeHugeSphere(), FireworkShapeStar(), FireworkShapeCreeperHead(), FireworkShapeBurst()}

// EncodeNBT ...
func (f FireworkExplosion) EncodeNBT() map[string]any {
	// The fade is picked before the map is built, so the fading case no longer
	// stores the empty array and then overwrites it. Both cases end up with the
	// same value under the same key.
	var fade any = [0]uint8{}
	if f.Fades {
		fade = [1]uint8{uint8(invertColour(f.Fade))}
	}
	return map[string]any{
		"FireworkType":    f.Shape.Uint8(),
		"FireworkColor":   [1]uint8{uint8(invertColour(f.Colour))},
		"FireworkFade":    fade,
		"FireworkFlicker": boolByte(f.Twinkle),
		"FireworkTrail":   boolByte(f.Trail),
	}
}

// DecodeNBT ...
func (f FireworkExplosion) DecodeNBT(data map[string]any) any {
	// The shared table has the same contents and length as the slice
	// FireworkShapes built here on every call, so an out of range ID still
	// panics the same way and the slice allocation is gone.
	f.Shape = fireworkShapes[data["FireworkType"].(uint8)]
	f.Twinkle = data["FireworkFlicker"].(uint8) == 1
	f.Trail = data["FireworkTrail"].(uint8) == 1

	colours := data["FireworkColor"]
	if diskColour, ok := colours.([1]uint8); ok {
		f.Colour = invertColourID(int16(diskColour[0]))
	} else if networkColours, ok := colours.([]any); ok {
		f.Colour = invertColourID(int16(networkColours[0].(uint8)))
	}

	if fades, ok := data["FireworkFade"].([1]uint8); ok {
		f.Fade, f.Fades = invertColourID(int16(fades[0])), true
	}
	return f
}
