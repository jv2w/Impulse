package dialogue

import (
	"encoding/json"
	"github.com/go-gl/mathgl/mgl64"
)

// DisplaySettings holds optional fields that change the way the dialogue,
// particularly the entity shown in it, is displayed.
type DisplaySettings struct {
	// EntityScale specifies the scale of the entity displayed in the dialogue.
	EntityScale mgl64.Vec3
	// EntityOffset specifies the offset of the entity shown in the dialogue.
	EntityOffset mgl64.Vec3
	// EntityRotation is the rotation of the entity shown in the dialogue. This
	// rotation functions a bit differently to the normal entity rotation in
	// Minecraft: The values are still degrees, but pitch (rot[1]) values are
	// whole-body pitch instead of head-specific, and rot[2] is whole-body roll.
	EntityRotation mgl64.Vec3
}

// MarshalJSON encodes the DisplaySettings to JSON.
func (d DisplaySettings) MarshalJSON() ([]byte, error) {
	// Yaw and pitch are swapped in this display.
	d.EntityRotation[0], d.EntityRotation[1] = d.EntityRotation[1], d.EntityRotation[0]-32
	// encoding/json writes map keys in ascending order, so a struct with the
	// fields declared in that order writes the same bytes without the
	// temporary map and its boxed values. A mgl64.Vec3 encodes exactly like
	// the [3]float64 the default scale used.
	s := displayJSON{
		// Entity is rotated by 32 degrees by default.
		Rotate: d.EntityRotation,
		Scale:  mgl64.Vec3{1, 1, 1},
		// Translate needs to be multiplied by -32 to get a rough block
		// equivalent.
		Translate: d.EntityOffset.Mul(-32),
	}
	if (d.EntityScale != mgl64.Vec3{}) {
		s.Scale = d.EntityScale
	}
	return json.Marshal(s)
}

// displayJSON is the JSON shape of DisplaySettings. The field order matches
// the ascending key order encoding/json used for the map it replaces.
type displayJSON struct {
	Rotate    mgl64.Vec3 `json:"rotate"`
	Scale     mgl64.Vec3 `json:"scale"`
	Translate mgl64.Vec3 `json:"translate"`
}
