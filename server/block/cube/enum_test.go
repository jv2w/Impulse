package cube

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// TestFacesOrder pins the order of the package's enumerators. Block models and
// the packet layer both index into these, so the order is behaviour.
//
// Note that none of the three slices is in constant order: Faces() runs
// down, up, then north-east-south-west (a rotation), while the Face constants
// run down, up, north, south, west, east. Faces()[i] is NOT Face(i) for i >= 3.
func TestFacesOrder(t *testing.T) {
	wantFaces := []Face{FaceDown, FaceUp, FaceNorth, FaceEast, FaceSouth, FaceWest}
	if got := Faces(); !equalFaces(got, wantFaces) {
		t.Fatalf("Faces() = %v, want %v", got, wantFaces)
	}
	// Spelled out so a reordering shows up as a value change, not just a
	// slice mismatch.
	if got := Faces()[3]; got != FaceEast {
		t.Fatalf("Faces()[3] = %v, want east (NOT Face(3), which is south)", got)
	}

	wantHorizontal := []Face{FaceNorth, FaceEast, FaceSouth, FaceWest}
	if got := HorizontalFaces(); !equalFaces(got, wantHorizontal) {
		t.Fatalf("HorizontalFaces() = %v, want %v", got, wantHorizontal)
	}

	wantDirections := []Direction{North, East, South, West}
	got := Directions()
	if len(got) != len(wantDirections) {
		t.Fatalf("Directions() = %v, want %v", got, wantDirections)
	}
	for i := range wantDirections {
		if got[i] != wantDirections[i] {
			t.Fatalf("Directions() = %v, want %v", got, wantDirections)
		}
	}

	wantAxes := []Axis{X, Y, Z}
	gotAxes := Axes()
	if len(gotAxes) != len(wantAxes) {
		t.Fatalf("Axes() = %v, want %v", gotAxes, wantAxes)
	}
	for i := range wantAxes {
		if gotAxes[i] != wantAxes[i] {
			t.Fatalf("Axes() = %v, want %v", gotAxes, wantAxes)
		}
	}
}

func equalFaces(got, want []Face) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestFacesAliasesAPackageArray pins that Faces, HorizontalFaces and Directions
// all hand back a window onto a package-level array rather than a copy.
//
// PINNED BUG: a caller writing into any of these three results silently
// rewrites the enumeration for every other caller in the process, for the rest
// of its life. Axes() is the odd one out and returns a fresh slice. Pinned
// as-is; decide in stage 1.
func TestFacesAliasesAPackageArray(t *testing.T) {
	t.Run("Faces", func(t *testing.T) {
		defer func() { faces = [...]Face{FaceDown, FaceUp, FaceNorth, FaceEast, FaceSouth, FaceWest} }()

		Faces()[0] = FaceEast
		if got := Faces()[0]; got != FaceEast {
			t.Fatalf("Faces()[0] = %v after writing to an earlier result, want east", got)
		}
		if faces[0] != FaceEast {
			t.Fatalf("the package array = %v, want the write to have reached it", faces)
		}
	})
	t.Run("HorizontalFaces", func(t *testing.T) {
		defer func() { hFaces = [...]Face{FaceNorth, FaceEast, FaceSouth, FaceWest} }()

		HorizontalFaces()[0] = FaceUp
		if got := HorizontalFaces()[0]; got != FaceUp {
			t.Fatalf("HorizontalFaces()[0] = %v after writing to an earlier result, want up", got)
		}
	})
	t.Run("Directions", func(t *testing.T) {
		defer func() { directions = [...]Direction{North, East, South, West} }()

		Directions()[0] = East
		if got := Directions()[0]; got != East {
			t.Fatalf("Directions()[0] = %v after writing to an earlier result, want east", got)
		}
	})
	t.Run("Axes_returns_a_fresh_slice", func(t *testing.T) {
		Axes()[0] = Y
		if got := Axes()[0]; got != X {
			t.Fatalf("Axes()[0] = %v after writing to an earlier result, want x", got)
		}
	})
}

func TestFaceProperties(t *testing.T) {
	tests := []struct {
		face                    Face
		str                     string
		opposite                Face
		axis                    Axis
		direction               Direction
		rotateRight, rotateLeft Face
	}{
		{FaceDown, "down", FaceUp, Y, Direction(-2), FaceDown, FaceDown},
		{FaceUp, "up", FaceDown, Y, Direction(-1), FaceUp, FaceUp},
		{FaceNorth, "north", FaceSouth, Z, North, FaceEast, FaceWest},
		{FaceSouth, "south", FaceNorth, Z, South, FaceWest, FaceEast},
		{FaceWest, "west", FaceEast, X, West, FaceNorth, FaceSouth},
		{FaceEast, "east", FaceWest, X, East, FaceSouth, FaceNorth},
	}
	for _, test := range tests {
		t.Run(test.str, func(t *testing.T) {
			if got := test.face.String(); got != test.str {
				t.Fatalf("String() = %q, want %q", got, test.str)
			}
			if got := test.face.Opposite(); got != test.opposite {
				t.Fatalf("Opposite() = %v, want %v", got, test.opposite)
			}
			if got := test.face.Axis(); got != test.axis {
				t.Fatalf("Axis() = %v, want %v", got, test.axis)
			}
			if got := test.face.Direction(); got != test.direction {
				t.Fatalf("Direction() = %v, want %v", got, test.direction)
			}
			if got := test.face.RotateRight(); got != test.rotateRight {
				t.Fatalf("RotateRight() = %v, want %v", got, test.rotateRight)
			}
			if got := test.face.RotateLeft(); got != test.rotateLeft {
				t.Fatalf("RotateLeft() = %v, want %v", got, test.rotateLeft)
			}
		})
	}
}

// TestFaceIdentities pins the round trips that the rest of the tree assumes.
func TestFaceIdentities(t *testing.T) {
	for _, f := range Faces() {
		if got := f.Opposite().Opposite(); got != f {
			t.Fatalf("%v.Opposite().Opposite() = %v, want %v", f, got, f)
		}
		if got := f.RotateRight().RotateLeft(); got != f {
			t.Fatalf("%v.RotateRight().RotateLeft() = %v, want %v", f, got, f)
		}
		if got := f.RotateLeft().RotateRight(); got != f {
			t.Fatalf("%v.RotateLeft().RotateRight() = %v, want %v", f, got, f)
		}
		turned := f
		for range 4 {
			turned = turned.RotateRight()
		}
		if turned != f {
			t.Fatalf("%v rotated right four times = %v, want %v", f, turned, f)
		}
		// The axis of a face always names that face as one of its two.
		negative, positive := f.Axis().Faces()
		if negative != f && positive != f {
			t.Fatalf("%v.Axis().Faces() = (%v, %v), neither of which is %v", f, negative, positive, f)
		}
	}
	for _, f := range HorizontalFaces() {
		// Face and Direction are mutual inverses on the horizontal faces only.
		if got := f.Direction().Face(); got != f {
			t.Fatalf("%v.Direction().Face() = %v, want %v", f, got, f)
		}
		if got := f.Direction().Opposite().Face(); got != f.Opposite() {
			t.Fatalf("%v.Direction().Opposite().Face() = %v, want %v", f, got, f.Opposite())
		}
		if got := f.Direction().RotateRight().Face(); got != f.RotateRight() {
			t.Fatalf("%v.Direction().RotateRight().Face() = %v, want %v", f, got, f.RotateRight())
		}
	}
}

// TestFaceOutOfRange pins what the Face methods do with a value outside the six
// constants. Only String rejects it.
//
// PINNED BUG: Opposite folds every unknown Face onto FaceUp and Direction hands
// back an equally meaningless Direction, so a corrupt face propagates silently
// instead of being caught. String is the one that panics. Pinned as-is;
// decide in stage 1.
func TestFaceOutOfRange(t *testing.T) {
	unknown := Face(9)

	if got := unknown.Opposite(); got != FaceUp {
		t.Fatalf("Face(9).Opposite() = %v, want up (the default branch)", got)
	}
	if got := unknown.Axis(); got != Y {
		t.Fatalf("Face(9).Axis() = %v, want y (the default branch)", got)
	}
	if got := unknown.Direction(); got != Direction(7) {
		t.Fatalf("Face(9).Direction() = %v, want Direction(7)", got)
	}
	if got := unknown.RotateRight(); got != unknown {
		t.Fatalf("Face(9).RotateRight() = %v, want it unchanged", got)
	}
	if got := unknown.RotateLeft(); got != unknown {
		t.Fatalf("Face(9).RotateLeft() = %v, want it unchanged", got)
	}
	assertPanics(t, "Face(9).String()", func() { _ = unknown.String() })
}

func TestDirectionProperties(t *testing.T) {
	tests := []struct {
		direction               Direction
		str                     string
		face                    Face
		opposite                Direction
		rotateRight, rotateLeft Direction
	}{
		{North, "north", FaceNorth, South, East, West},
		{South, "south", FaceSouth, North, West, East},
		{West, "west", FaceWest, East, North, South},
		{East, "east", FaceEast, West, South, North},
	}
	for _, test := range tests {
		t.Run(test.str, func(t *testing.T) {
			if got := test.direction.String(); got != test.str {
				t.Fatalf("String() = %q, want %q", got, test.str)
			}
			if got := test.direction.Face(); got != test.face {
				t.Fatalf("Face() = %v, want %v", got, test.face)
			}
			if got := test.direction.Opposite(); got != test.opposite {
				t.Fatalf("Opposite() = %v, want %v", got, test.opposite)
			}
			if got := test.direction.RotateRight(); got != test.rotateRight {
				t.Fatalf("RotateRight() = %v, want %v", got, test.rotateRight)
			}
			if got := test.direction.RotateLeft(); got != test.rotateLeft {
				t.Fatalf("RotateLeft() = %v, want %v", got, test.rotateLeft)
			}
		})
	}
}

func TestDirectionIdentities(t *testing.T) {
	for _, d := range Directions() {
		if got := d.Opposite().Opposite(); got != d {
			t.Fatalf("%v.Opposite().Opposite() = %v, want %v", d, got, d)
		}
		if got := d.RotateRight().RotateLeft(); got != d {
			t.Fatalf("%v.RotateRight().RotateLeft() = %v, want %v", d, got, d)
		}
		if got := d.Face().Direction(); got != d {
			t.Fatalf("%v.Face().Direction() = %v, want %v", d, got, d)
		}
		// Two right turns are the opposite direction.
		if got := d.RotateRight().RotateRight(); got != d.Opposite() {
			t.Fatalf("%v rotated right twice = %v, want %v", d, got, d.Opposite())
		}
		turned := d
		for range 4 {
			turned = turned.RotateRight()
		}
		if turned != d {
			t.Fatalf("%v rotated right four times = %v, want %v", d, turned, d)
		}
	}
}

// TestDirectionOutOfRangePanics pins that every Direction method panics on an
// unknown value.
//
// PINNED BUG: Direction is an int with no validation at its boundaries, so a
// bad value from a packet or a saved world takes down the goroutine instead of
// being reported. Pinned as-is; decide in stage 1.
func TestDirectionOutOfRangePanics(t *testing.T) {
	unknown := Direction(9)

	assertPanics(t, "Direction(9).Opposite()", func() { _ = unknown.Opposite() })
	assertPanics(t, "Direction(9).RotateRight()", func() { _ = unknown.RotateRight() })
	assertPanics(t, "Direction(9).RotateLeft()", func() { _ = unknown.RotateLeft() })
	assertPanics(t, "Direction(9).String()", func() { _ = unknown.String() })

	// Face is the one method that does not: it is plain arithmetic.
	if got := unknown.Face(); got != Face(11) {
		t.Fatalf("Direction(9).Face() = %v, want Face(11)", got)
	}
}

func TestAxisProperties(t *testing.T) {
	tests := []struct {
		axis               Axis
		str                string
		rotated            Axis
		negative, positive Face
		vec                mgl64.Vec3
	}{
		{X, "x", Z, FaceWest, FaceEast, mgl64.Vec3{1, 0, 0}},
		{Y, "y", Y, FaceDown, FaceUp, mgl64.Vec3{0, 1, 0}},
		{Z, "z", X, FaceNorth, FaceSouth, mgl64.Vec3{0, 0, 1}},
		// PINNED BUG: every Axis method falls through to the Z branch on an
		// unknown value, except RotateLeft which falls through to Y. An invalid
		// axis is therefore silently read as two different axes depending on
		// which method is called. Pinned as-is; decide in stage 1.
		{Axis(9), "z", Y, FaceNorth, FaceSouth, mgl64.Vec3{0, 0, 1}},
	}
	for _, test := range tests {
		t.Run(test.str+"_"+string(rune('0'+int(test.axis))), func(t *testing.T) {
			if got := test.axis.String(); got != test.str {
				t.Fatalf("String() = %q, want %q", got, test.str)
			}
			if got := test.axis.RotateLeft(); got != test.rotated {
				t.Fatalf("RotateLeft() = %v, want %v", got, test.rotated)
			}
			// RotateRight is documented to be the same operation as RotateLeft.
			if got := test.axis.RotateRight(); got != test.rotated {
				t.Fatalf("RotateRight() = %v, want %v", got, test.rotated)
			}
			negative, positive := test.axis.Faces()
			if negative != test.negative || positive != test.positive {
				t.Fatalf("Faces() = (%v, %v), want (%v, %v)", negative, positive, test.negative, test.positive)
			}
			if got := test.axis.Vec3(); got != test.vec {
				t.Fatalf("Vec3() = %v, want %v", got, test.vec)
			}
		})
	}
}

func TestAxisRotationIsAnInvolution(t *testing.T) {
	for _, a := range Axes() {
		if got := a.RotateLeft().RotateLeft(); got != a {
			t.Fatalf("%v rotated left twice = %v, want %v", a, got, a)
		}
		if got := a.RotateLeft().RotateRight(); got != a {
			t.Fatalf("%v.RotateLeft().RotateRight() = %v, want %v", a, got, a)
		}
	}
	// Rotating an axis three times is therefore NOT the identity: it is one
	// more rotation, because left and right are the same operation.
	if got := X.RotateLeft().RotateLeft().RotateLeft(); got != Z {
		t.Fatalf("x rotated left three times = %v, want z", got)
	}
}

func TestOrientationFromYaw(t *testing.T) {
	tests := []struct {
		yaw  float64
		want Orientation
	}{
		{0, 0},
		{22.5, 1},
		{45, 2},
		{90, 4},
		{135, 6},
		{180, 8},
		{270, 12},
		{337.5, 15},
		{360, 0},
		{720, 0},
		// Rounding is half away from zero, so the quadrant boundary lands on
		// the higher orientation.
		{11.25, 1},
		{11.24, 0},
		// PINNED BUG: the yaw is reduced with math.Mod, which keeps the sign
		// and does not wrap into [0, 360). A yaw just under a full turn rounds
		// up to 16 and a negative yaw produces a negative Orientation, both
		// outside the documented 16 options. Pinned as-is; decide in stage 1.
		{359.9, 16},
		{-11.25, -1},
		{-90, -4},
		{-180, -8},
		{-360, 0},
	}
	for _, test := range tests {
		if got := OrientationFromYaw(test.yaw); got != test.want {
			t.Fatalf("OrientationFromYaw(%v) = %v, want %v", test.yaw, got, test.want)
		}
	}
}

func TestOrientationRotationAndOpposite(t *testing.T) {
	tests := []struct {
		orientation                       Orientation
		yaw                               float64
		opposite, rotateLeft, rotateRight Orientation
	}{
		// PINNED BUG: RotateLeft on a low orientation subtracts 90 degrees and
		// then reduces with math.Mod, which yields a negative Orientation
		// rather than wrapping to 12. Pinned as-is; decide in stage 1.
		{0, 0, 8, -4, 4},
		{4, 90, 12, 0, 8},
		{8, 180, 0, 4, 12},
		{12, 270, 4, 8, 0},
	}
	for _, test := range tests {
		if got := test.orientation.Yaw(); got != test.yaw {
			t.Fatalf("Orientation(%v).Yaw() = %v, want %v", test.orientation, got, test.yaw)
		}
		if got := test.orientation.Opposite(); got != test.opposite {
			t.Fatalf("Orientation(%v).Opposite() = %v, want %v", test.orientation, got, test.opposite)
		}
		if got := test.orientation.RotateLeft(); got != test.rotateLeft {
			t.Fatalf("Orientation(%v).RotateLeft() = %v, want %v", test.orientation, got, test.rotateLeft)
		}
		if got := test.orientation.RotateRight(); got != test.rotateRight {
			t.Fatalf("Orientation(%v).RotateRight() = %v, want %v", test.orientation, got, test.rotateRight)
		}
	}
	// Opposite is an involution across the whole documented range even though
	// RotateLeft is not.
	for o := Orientation(0); o < 16; o++ {
		if got := o.Opposite().Opposite(); got != o {
			t.Fatalf("Orientation(%v).Opposite().Opposite() = %v, want %v", o, got, o)
		}
	}
}

func assertPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic, want a panic", name)
		}
	}()
	f()
}
