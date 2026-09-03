package cube

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

const rotationEpsilon = 1e-9

// TestRotationFix pins the normalisation every Rotation method runs first. The
// documented ranges are (-180, 180) for yaw and (-90, 90) for pitch.
func TestRotationFix(t *testing.T) {
	tests := []struct {
		in, want Rotation
	}{
		{Rotation{0, 0}, Rotation{0, 0}},
		{Rotation{45, 45}, Rotation{45, 45}},
		{Rotation{-45, -45}, Rotation{-45, -45}},
		{Rotation{190, 0}, Rotation{-170, 0}},
		{Rotation{360, 0}, Rotation{0, 0}},
		{Rotation{765, 0}, Rotation{45, 0}},
		{Rotation{-360, 0}, Rotation{0, 0}},
		{Rotation{0, 100}, Rotation{0, -80}},
		{Rotation{0, 200}, Rotation{0, 20}},
		{Rotation{0, -100}, Rotation{0, 80}},
		// PINNED BUG: the two half-turn endpoints do not fold into the
		// documented open range, they swap. A yaw of 180 becomes -180 but a yaw
		// of -180 becomes 180, which the doc comment excludes. The same holds
		// for a pitch of exactly +/-90.
		{Rotation{180, 0}, Rotation{-180, 0}},
		{Rotation{-180, 0}, Rotation{180, 0}},
		{Rotation{0, 90}, Rotation{0, -90}},
		{Rotation{0, -90}, Rotation{0, 90}},
	}
	for _, test := range tests {
		if got := test.in.fix(); got != test.want {
			t.Fatalf("%v.fix() = %v, want %v", test.in, got, test.want)
		}
		// Add against the zero rotation is the exported route to the same
		// normalisation.
		if got := test.in.Add(Rotation{}); got != test.want {
			t.Fatalf("%v.Add(zero) = %v, want %v", test.in, got, test.want)
		}
	}
}

func TestRotationAccessors(t *testing.T) {
	r := Rotation{12, -34}
	if got := r.Yaw(); got != 12 {
		t.Fatalf("Yaw() = %v, want 12", got)
	}
	if got := r.Pitch(); got != -34 {
		t.Fatalf("Pitch() = %v, want -34", got)
	}
	yaw, pitch := r.Elem()
	if yaw != 12 || pitch != -34 {
		t.Fatalf("Elem() = (%v, %v), want (12, -34)", yaw, pitch)
	}
	// Yaw, Pitch and Elem read the raw fields; only Add and the derived
	// methods normalise.
	raw := Rotation{500, 500}
	if got := raw.Yaw(); got != 500 {
		t.Fatalf("Yaw() of an unnormalised rotation = %v, want the raw 500", got)
	}
}

func TestRotationAdd(t *testing.T) {
	tests := []struct {
		a, b, want Rotation
	}{
		{Rotation{0, 0}, Rotation{0, 0}, Rotation{0, 0}},
		{Rotation{10, 10}, Rotation{20, 20}, Rotation{30, 30}},
		{Rotation{170, 80}, Rotation{20, 20}, Rotation{-170, -80}},
		{Rotation{-170, -80}, Rotation{-20, -20}, Rotation{170, 80}},
	}
	for _, test := range tests {
		if got := test.a.Add(test.b); got != test.want {
			t.Fatalf("%v.Add(%v) = %v, want %v", test.a, test.b, got, test.want)
		}
	}
}

// TestRotationNegDoesNotRenormalise pins that Neg fixes its input and then
// negates, without fixing the result.
//
// PINNED BUG: Neg of a half turn yields a yaw of exactly 180, outside the
// documented (-180, 180) range, because the negation happens after the last
// fix.
func TestRotationNegDoesNotRenormalise(t *testing.T) {
	tests := []struct {
		in, want Rotation
	}{
		{Rotation{0, 0}, Rotation{0, 0}},
		{Rotation{45, 45}, Rotation{-45, -45}},
		{Rotation{190, 0}, Rotation{170, 0}},
		{Rotation{180, 0}, Rotation{180, 0}},
		{Rotation{-180, 0}, Rotation{-180, 0}},
	}
	for _, test := range tests {
		if got := test.in.Neg(); got != test.want {
			t.Fatalf("%v.Neg() = %v, want %v", test.in, got, test.want)
		}
	}
}

func TestRotationOpposite(t *testing.T) {
	tests := []struct {
		in, want Rotation
	}{
		{Rotation{0, 0}, Rotation{-180, 0}},
		{Rotation{180, 0}, Rotation{0, 0}},
		{Rotation{-180, 0}, Rotation{0, 0}},
		{Rotation{190, 0}, Rotation{10, 0}},
		{Rotation{765, 0}, Rotation{-135, 0}},
		{Rotation{45, 45}, Rotation{-135, -45}},
		{Rotation{0, 90}, Rotation{-180, -90}},
		{Rotation{0, -90}, Rotation{-180, 90}},
	}
	for _, test := range tests {
		if got := test.in.Opposite(); got != test.want {
			t.Fatalf("%v.Opposite() = %v, want %v", test.in, got, test.want)
		}
	}

	// The documented property: a rotation and its opposite point exactly the
	// other way, so their direction vectors cancel.
	for yaw := -180.0; yaw <= 180; yaw += 15 {
		for pitch := -90.0; pitch <= 90; pitch += 15 {
			r := Rotation{yaw, pitch}
			if got := r.Vec3().Add(r.Opposite().Vec3()).Len(); got > rotationEpsilon {
				t.Fatalf("%v.Vec3() and its opposite sum to a vector of length %v, want 0", r, got)
			}
		}
	}
}

// TestRotationDirection pins the yaw quadrant boundaries. Each range is open at
// its lower bound and closed at its upper one, so a yaw of exactly -45 reads as
// East, not South.
func TestRotationDirection(t *testing.T) {
	tests := []struct {
		yaw  float64
		want Direction
	}{
		{-180, North},
		{-135.1, North},
		{-135, North},
		{-134.9, East},
		{-90, East},
		{-45.1, East},
		{-45, East},
		{-44.9, South},
		{0, South},
		{45, South},
		{45.1, West},
		{90, West},
		{135, West},
		{135.1, North},
		{180, North},
		// Beyond a full turn the yaw is normalised first.
		{360, South},
		{450, West},
	}
	for _, test := range tests {
		if got := (Rotation{test.yaw, 0}).Direction(); got != test.want {
			t.Fatalf("Rotation{%v, 0}.Direction() = %v, want %v", test.yaw, got, test.want)
		}
	}
	// The pitch never influences the direction.
	for _, pitch := range []float64{-90, -45, 0, 45, 90} {
		if got := (Rotation{90, pitch}).Direction(); got != West {
			t.Fatalf("Rotation{90, %v}.Direction() = %v, want west", pitch, got)
		}
	}
}

// TestRotationOrientation pins Rotation.Orientation, which is NOT the same
// function as OrientationFromYaw: it shifts a negative yaw up by a full turn
// first, so it stays inside 0..15 where OrientationFromYaw does not.
func TestRotationOrientation(t *testing.T) {
	tests := []struct {
		yaw  float64
		want Orientation
	}{
		{0, 0},
		{45, 2},
		{90, 4},
		{135, 6},
		{180, 8},
		{-45, 14},
		{-90, 12},
		{-135, 10},
		{-180, 8},
		{-11.26, 15},
		// PINNED BUG: the shift is guarded by yaw < -step/2, so a yaw of
		// exactly -11.25 misses it and rounds to -1 instead of 15.
		{-11.25, -1},
	}
	for _, test := range tests {
		if got := (Rotation{test.yaw, 0}).Orientation(); got != test.want {
			t.Fatalf("Rotation{%v, 0}.Orientation() = %v, want %v", test.yaw, got, test.want)
		}
	}
}

// TestRotationVec3 pins the direction vector convention: a yaw of 0 looks
// towards positive Z (south) and a positive yaw turns towards negative X
// (west), matching Direction above.
func TestRotationVec3(t *testing.T) {
	tests := []struct {
		in   Rotation
		want mgl64.Vec3
	}{
		{Rotation{0, 0}, mgl64.Vec3{0, 0, 1}},
		{Rotation{90, 0}, mgl64.Vec3{-1, 0, 0}},
		{Rotation{-90, 0}, mgl64.Vec3{1, 0, 0}},
		{Rotation{180, 0}, mgl64.Vec3{0, 0, -1}},
		// A positive pitch looks downwards, as documented.
		{Rotation{0, 45}, mgl64.Vec3{0, -math.Sqrt2 / 2, math.Sqrt2 / 2}},
		{Rotation{0, -45}, mgl64.Vec3{0, math.Sqrt2 / 2, math.Sqrt2 / 2}},
		// PINNED BUG: at exactly +/-90 the pitch is flipped by fix first, so a
		// pitch of 90 (documented as looking down) produces a vector pointing
		// straight up and vice versa.
		{Rotation{0, 90}, mgl64.Vec3{0, 1, 0}},
		{Rotation{0, -90}, mgl64.Vec3{0, -1, 0}},
	}
	for _, test := range tests {
		got := test.in.Vec3()
		for i := range 3 {
			if math.Abs(got[i]-test.want[i]) > rotationEpsilon {
				t.Fatalf("%v.Vec3() = %v, want %v", test.in, got, test.want)
			}
		}
	}

	// The vector is always a unit vector, as documented.
	for yaw := -540.0; yaw <= 540; yaw += 17 {
		for pitch := -180.0; pitch <= 180; pitch += 13 {
			r := Rotation{yaw, pitch}
			if got := r.Vec3().Len(); math.Abs(got-1) > rotationEpsilon {
				t.Fatalf("%v.Vec3() has length %v, want 1", r, got)
			}
		}
	}
}

// TestRotationVec3AgreesWithDirection pins that the horizontal component of the
// direction vector points at the same block face Direction reports.
func TestRotationVec3AgreesWithDirection(t *testing.T) {
	for yaw := -179.0; yaw <= 179; yaw += 1 {
		r := Rotation{yaw, 0}
		vec := r.Vec3()
		face := r.Direction().Face()

		// Skip the exact diagonals, where neither horizontal component
		// dominates and the quadrant boundary decides on its own.
		if math.Abs(math.Abs(vec[0])-math.Abs(vec[2])) < 1e-6 {
			continue
		}

		// The dominant horizontal component must be along the direction's axis
		// and carry its sign.
		var dominant int
		if math.Abs(vec[0]) > math.Abs(vec[2]) {
			dominant = 0
		} else {
			dominant = 2
		}
		if face.Axis() == Y {
			t.Fatalf("Rotation{%v, 0}.Direction() = %v, which is vertical", yaw, r.Direction())
		}
		if wantAxis := face.Axis(); (wantAxis == X) != (dominant == 0) {
			t.Fatalf("Rotation{%v, 0}: Vec3() = %v is dominated by axis %d but Direction() = %v", yaw, vec, dominant, r.Direction())
		}
		negative, _ := face.Axis().Faces()
		if wantNegative := face == negative; wantNegative != (vec[dominant] < 0) {
			t.Fatalf("Rotation{%v, 0}: Vec3() = %v disagrees in sign with Direction() = %v", yaw, vec, r.Direction())
		}
	}
}
