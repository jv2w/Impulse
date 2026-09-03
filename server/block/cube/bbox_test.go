package cube

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// Everything below runs against both instantiations of the generic
// boundingBox: BBox (float64) and BBox32 (float32).

// assertBox compares a box against golden minimum and maximum coordinates.
func assertBox[T number, V vec3[T]](t *testing.T, name string, got boundingBox[T, V], wantMin, wantMax [3]float64, epsilon float64) {
	t.Helper()
	for i := range 3 {
		if math.Abs(float64(got.min[i])-wantMin[i]) > epsilon {
			t.Fatalf("%s = %v, want min %v max %v", name, got, wantMin, wantMax)
		}
		if math.Abs(float64(got.max[i])-wantMax[i]) > epsilon {
			t.Fatalf("%s = %v, want min %v max %v", name, got, wantMin, wantMax)
		}
	}
}

func TestBoxNormalisesSwappedCoordinates(t *testing.T) {
	testBoxConstruction[float64, mgl64.Vec3](t, offsetEpsilon64)
}

func TestBox32NormalisesSwappedCoordinates(t *testing.T) {
	testBoxConstruction[float32, mgl32.Vec3](t, offsetEpsilon32)
}

// testBoxConstruction pins that newBox orders each axis independently, so a box
// built from an inverted pair of coordinates is well-formed on that axis alone.
func testBoxConstruction[T number, V vec3[T]](t *testing.T, epsilon float64) {
	tests := []struct {
		name             string
		in               [6]float64
		wantMin, wantMax [3]float64
	}{
		{"already_ordered", [6]float64{0, 0, 0, 1, 2, 3}, [3]float64{0, 0, 0}, [3]float64{1, 2, 3}},
		{"x_inverted", [6]float64{1, 0, 0, 0, 2, 3}, [3]float64{0, 0, 0}, [3]float64{1, 2, 3}},
		{"y_inverted", [6]float64{0, 2, 0, 1, 0, 3}, [3]float64{0, 0, 0}, [3]float64{1, 2, 3}},
		{"z_inverted", [6]float64{0, 0, 3, 1, 2, 0}, [3]float64{0, 0, 0}, [3]float64{1, 2, 3}},
		{"all_inverted", [6]float64{1, 2, 3, 0, 0, 0}, [3]float64{0, 0, 0}, [3]float64{1, 2, 3}},
		{"degenerate", [6]float64{4, 5, 6, 4, 5, 6}, [3]float64{4, 5, 6}, [3]float64{4, 5, 6}},
		{"negative_coordinates", [6]float64{1, 1, 1, -1, -1, -1}, [3]float64{-1, -1, -1}, [3]float64{1, 1, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := test.in
			got := newBox[T, V](T(in[0]), T(in[1]), T(in[2]), T(in[3]), T(in[4]), T(in[5]))
			assertBox(t, "newBox", got, test.wantMin, test.wantMax, epsilon)
		})
	}
}

func TestBBoxTransforms(t *testing.T) {
	testBoxTransforms[float64, mgl64.Vec3](t, offsetEpsilon64)
}

func TestBBox32Transforms(t *testing.T) {
	testBoxTransforms[float32, mgl32.Vec3](t, offsetEpsilon32)
}

// testBoxTransforms pins the exact result of every shape-changing method for
// every Face and Axis, including the negative arguments block models rely on
// (Stair, Fence and Thin all shrink boxes with a negative ExtendTowards).
func testBoxTransforms[T number, V vec3[T]](t *testing.T, epsilon float64) {
	unit := newBox[T, V](0, 0, 0, 1, 1, 1)
	offCentre := newBox[T, V](1, 1, 1, 2, 2, 2)

	tests := []struct {
		name             string
		got              boundingBox[T, V]
		wantMin, wantMax [3]float64
	}{
		{"Grow_positive", unit.Grow(1), [3]float64{-1, -1, -1}, [3]float64{2, 2, 2}},
		{"Grow_zero", unit.Grow(0), [3]float64{0, 0, 0}, [3]float64{1, 1, 1}},
		{"Grow_negative_shrinks", unit.Grow(-0.25), [3]float64{0.25, 0.25, 0.25}, [3]float64{0.75, 0.75, 0.75}},
		// PINNED BUG: Grow does not renormalise, so shrinking a box by more than
		// half its size leaves min above max. Every later method (Volume,
		// IntersectsWith, the offsets) then reads nonsense.
		{"Grow_negative_past_the_centre_inverts", unit.Grow(-0.75), [3]float64{0.75, 0.75, 0.75}, [3]float64{0.25, 0.25, 0.25}},
		{"GrowVec3_per_axis", unit.GrowVec3(V{1, 2, 3}), [3]float64{-1, -2, -3}, [3]float64{2, 3, 4}},
		{"GrowVec3_mixed_signs", unit.GrowVec3(V{-0.1, 0, 0.5}), [3]float64{0.1, 0, -0.5}, [3]float64{0.9, 1, 1.5}},

		// Extend moves exactly one side per axis, chosen by the sign.
		{"Extend_positive_moves_max", unit.Extend(V{1, 0, 0}), [3]float64{0, 0, 0}, [3]float64{2, 1, 1}},
		{"Extend_negative_moves_min", unit.Extend(V{-1, 0, 0}), [3]float64{-1, 0, 0}, [3]float64{1, 1, 1}},
		{"Extend_zero_is_identity", unit.Extend(V{0, 0, 0}), [3]float64{0, 0, 0}, [3]float64{1, 1, 1}},
		{"Extend_mixed_signs", unit.Extend(V{1, -2, 3}), [3]float64{0, -2, 0}, [3]float64{2, 1, 4}},

		{"ExtendTowards_down", unit.ExtendTowards(FaceDown, 0.5), [3]float64{0, -0.5, 0}, [3]float64{1, 1, 1}},
		{"ExtendTowards_up", unit.ExtendTowards(FaceUp, 0.5), [3]float64{0, 0, 0}, [3]float64{1, 1.5, 1}},
		{"ExtendTowards_north", unit.ExtendTowards(FaceNorth, 0.5), [3]float64{0, 0, -0.5}, [3]float64{1, 1, 1}},
		{"ExtendTowards_south", unit.ExtendTowards(FaceSouth, 0.5), [3]float64{0, 0, 0}, [3]float64{1, 1, 1.5}},
		{"ExtendTowards_west", unit.ExtendTowards(FaceWest, 0.5), [3]float64{-0.5, 0, 0}, [3]float64{1, 1, 1}},
		{"ExtendTowards_east", unit.ExtendTowards(FaceEast, 0.5), [3]float64{0, 0, 0}, [3]float64{1.5, 1, 1}},
		{"ExtendTowards_up_negative_shrinks_the_top", unit.ExtendTowards(FaceUp, -0.25), [3]float64{0, 0, 0}, [3]float64{1, 0.75, 1}},
		{"ExtendTowards_down_negative_raises_the_bottom", unit.ExtendTowards(FaceDown, -0.25), [3]float64{0, 0.25, 0}, [3]float64{1, 1, 1}},
		// The switch has no default: an out-of-range Face silently does nothing.
		{"ExtendTowards_unknown_face_is_identity", unit.ExtendTowards(Face(9), 0.5), [3]float64{0, 0, 0}, [3]float64{1, 1, 1}},

		{"Stretch_x", unit.Stretch(X, 0.5), [3]float64{-0.5, 0, 0}, [3]float64{1.5, 1, 1}},
		{"Stretch_y", unit.Stretch(Y, 0.5), [3]float64{0, -0.5, 0}, [3]float64{1, 1.5, 1}},
		{"Stretch_z", unit.Stretch(Z, 0.5), [3]float64{0, 0, -0.5}, [3]float64{1, 1, 1.5}},
		{"Stretch_negative_shrinks_both_sides", unit.Stretch(X, -0.25), [3]float64{0.25, 0, 0}, [3]float64{0.75, 1, 1}},
		// The switch has no default: an out-of-range Axis silently does nothing.
		{"Stretch_unknown_axis_is_identity", unit.Stretch(Axis(7), 0.5), [3]float64{0, 0, 0}, [3]float64{1, 1, 1}},

		{"Translate_positive", unit.Translate(V{1, 2, 3}), [3]float64{1, 2, 3}, [3]float64{2, 3, 4}},
		{"Translate_negative", unit.Translate(V{-1, -2, -3}), [3]float64{-1, -2, -3}, [3]float64{0, -1, -2}},
		{"Translate_zero", unit.Translate(V{0, 0, 0}), [3]float64{0, 0, 0}, [3]float64{1, 1, 1}},

		{"TranslateTowards_down", unit.TranslateTowards(FaceDown, 2), [3]float64{0, -2, 0}, [3]float64{1, -1, 1}},
		{"TranslateTowards_up", unit.TranslateTowards(FaceUp, 2), [3]float64{0, 2, 0}, [3]float64{1, 3, 1}},
		{"TranslateTowards_north", unit.TranslateTowards(FaceNorth, 2), [3]float64{0, 0, -2}, [3]float64{1, 1, -1}},
		{"TranslateTowards_south", unit.TranslateTowards(FaceSouth, 2), [3]float64{0, 0, 2}, [3]float64{1, 1, 3}},
		{"TranslateTowards_west", unit.TranslateTowards(FaceWest, 2), [3]float64{-2, 0, 0}, [3]float64{-1, 1, 1}},
		{"TranslateTowards_east", unit.TranslateTowards(FaceEast, 2), [3]float64{2, 0, 0}, [3]float64{3, 1, 1}},
		{"TranslateTowards_negative_reverses", unit.TranslateTowards(FaceUp, -2), [3]float64{0, -2, 0}, [3]float64{1, -1, 1}},
		// Unlike ExtendTowards and Stretch this one has an explicit default.
		{"TranslateTowards_unknown_face_is_identity", unit.TranslateTowards(Face(9), 2), [3]float64{0, 0, 0}, [3]float64{1, 1, 1}},

		{"Mul_two", unit.Mul(2), [3]float64{0, 0, 0}, [3]float64{2, 2, 2}},
		{"Mul_two_off_centre", offCentre.Mul(2), [3]float64{2, 2, 2}, [3]float64{4, 4, 4}},
		{"Mul_zero_collapses_the_box", offCentre.Mul(0), [3]float64{0, 0, 0}, [3]float64{0, 0, 0}},
		// PINNED BUG: Mul does not renormalise either, so a negative scale
		// leaves min above max.
		{"Mul_negative_inverts", offCentre.Mul(-1), [3]float64{-1, -1, -1}, [3]float64{-2, -2, -2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertBox(t, test.name, test.got, test.wantMin, test.wantMax, epsilon)
		})
	}
}

// TestBBoxTransformsDoNotMutateTheReceiver pins that every transform is a value
// method returning a copy. Block models chain them off a single shared `full`
// box, so an in-place mutation here would corrupt every model in the process.
func TestBBoxTransformsDoNotMutateTheReceiver(t *testing.T) {
	original := Box(0, 0, 0, 1, 1, 1)
	box := original

	box.Grow(1)
	box.GrowVec3(mgl64.Vec3{1, 1, 1})
	box.Extend(mgl64.Vec3{1, 1, 1})
	box.Translate(mgl64.Vec3{1, 1, 1})
	box.Mul(3)
	for _, f := range Faces() {
		box.ExtendTowards(f, 1)
		box.TranslateTowards(f, 1)
	}
	for _, a := range Axes() {
		box.Stretch(a, 1)
	}

	if box != original {
		t.Fatalf("box = %v after chained transforms, want %v", box, original)
	}
}

func TestBBoxDimensions(t *testing.T) {
	box := Box(0, 0, 0, 2, 3, 5)
	if got := box.Width(); got != 2 {
		t.Fatalf("Width() = %v, want 2 (the X span)", got)
	}
	if got := box.Height(); got != 3 {
		t.Fatalf("Height() = %v, want 3 (the Y span)", got)
	}
	if got := box.Length(); got != 5 {
		t.Fatalf("Length() = %v, want 5 (the Z span)", got)
	}
	if got := box.Volume(); got != 30 {
		t.Fatalf("Volume() = %v, want 30", got)
	}
}

func TestBBoxVolume(t *testing.T) {
	tests := []struct {
		name string
		box  BBox
		want float64
	}{
		{"unit", Box(0, 0, 0, 1, 1, 1), 1},
		{"flat_slab", Box(0, 0, 0, 1, 0.5, 1), 0.5},
		{"zero_thickness_on_one_axis", Box(0, 0, 0, 1, 0, 1), 0},
		{"degenerate_point", Box(3, 3, 3, 3, 3, 3), 0},
		{"negative_coordinates", Box(-1, -1, -1, 1, 1, 1), 8},
		// A box inverted by Grow or Mul yields a negative volume rather than an
		// error or a clamp to zero.
		{"inverted_by_grow", Box(0, 0, 0, 1, 1, 1).Grow(-0.75), -0.125},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.box.Volume(); math.Abs(got-test.want) > offsetEpsilon64 {
				t.Fatalf("Volume() of %v = %v, want %v", test.box, got, test.want)
			}
		})
	}
}

// TestBBoxMinMaxReturnCopies pins that Min and Max hand back a value, not a
// view into the box.
func TestBBoxMinMaxReturnCopies(t *testing.T) {
	box := Box(0, 0, 0, 1, 1, 1)

	minimum, maximum := box.Min(), box.Max()
	minimum[0], maximum[0] = 100, 200

	if got := box.Min(); got != (mgl64.Vec3{0, 0, 0}) {
		t.Fatalf("Min() = %v after mutating an earlier result, want {0 0 0}", got)
	}
	if got := box.Max(); got != (mgl64.Vec3{1, 1, 1}) {
		t.Fatalf("Max() = %v after mutating an earlier result, want {1 1 1}", got)
	}
}

// TestBBoxIntersectsWithEpsilon pins the exact epsilon IntersectsWith applies.
// The overlap on an axis must be strictly greater than 1e-5 for the boxes to
// count as intersecting, so an overlap of exactly 1e-5 does not.
func TestBBoxIntersectsWithEpsilon(t *testing.T) {
	// The X overlap between these two is box.max[0]-other.min[0], which is
	// exactly the float64 nearest to 1e-5.
	other := Box(0, 0, 0, 5, 5, 5)

	if box := Box(0, 0, 0, 1e-5, 1, 1); box.IntersectsWith(other) {
		t.Fatal("IntersectsWith() with an overlap of exactly 1e-5 = true, want false")
	}
	if box := Box(0, 0, 0, math.Nextafter(1e-5, 1), 1, 1); !box.IntersectsWith(other) {
		t.Fatal("IntersectsWith() with an overlap one ulp above 1e-5 = false, want true")
	}
}

func TestBBox32IntersectsWithEpsilon(t *testing.T) {
	other := Box32(0, 0, 0, 5, 5, 5)

	if box := Box32(0, 0, 0, 1e-5, 1, 1); box.IntersectsWith(other) {
		t.Fatal("IntersectsWith() with an overlap of exactly 1e-5 = true, want false")
	}
	if box := Box32(0, 0, 0, math.Nextafter32(1e-5, 1), 1, 1); !box.IntersectsWith(other) {
		t.Fatal("IntersectsWith() with an overlap one ulp above 1e-5 = false, want true")
	}
}

func TestBBoxIntersectsWith(t *testing.T) {
	testIntersectsWith[float64, mgl64.Vec3](t)
}

func TestBBox32IntersectsWith(t *testing.T) {
	testIntersectsWith[float32, mgl32.Vec3](t)
}

// testIntersectsWith pins the overlap decision on every axis, in particular
// that flush faces do not intersect and that the relation is symmetric.
func testIntersectsWith[T number, V vec3[T]](t *testing.T) {
	unit := newBox[T, V](0, 0, 0, 1, 1, 1)

	tests := []struct {
		name  string
		other boundingBox[T, V]
		want  bool
	}{
		{"identical", newBox[T, V](0, 0, 0, 1, 1, 1), true},
		{"fully_contained", newBox[T, V](0.25, 0.25, 0.25, 0.75, 0.75, 0.75), true},
		{"fully_containing", newBox[T, V](-1, -1, -1, 2, 2, 2), true},
		{"half_overlap_on_x", newBox[T, V](0.5, 0, 0, 1.5, 1, 1), true},
		{"half_overlap_on_y", newBox[T, V](0, 0.5, 0, 1, 1.5, 1), true},
		{"half_overlap_on_z", newBox[T, V](0, 0, 0.5, 1, 1, 1.5), true},
		{"flush_on_x", newBox[T, V](1, 0, 0, 2, 1, 1), false},
		{"flush_on_y", newBox[T, V](0, 1, 0, 1, 2, 1), false},
		{"flush_on_z", newBox[T, V](0, 0, 1, 1, 1, 2), false},
		{"flush_on_negative_x", newBox[T, V](-1, 0, 0, 0, 1, 1), false},
		{"flush_on_negative_y", newBox[T, V](0, -1, 0, 1, 0, 1), false},
		{"flush_on_negative_z", newBox[T, V](0, 0, -1, 1, 1, 0), false},
		{"disjoint_on_x_only", newBox[T, V](5, 0, 0, 6, 1, 1), false},
		{"disjoint_on_y_only", newBox[T, V](0, 5, 0, 1, 6, 1), false},
		{"disjoint_on_z_only", newBox[T, V](0, 0, 5, 1, 1, 6), false},
		// Overlapping on two axes but separated on the third is still no hit;
		// the three checks are an AND.
		{"overlaps_xy_but_not_z", newBox[T, V](0.5, 0.5, 5, 1.5, 1.5, 6), false},
		{"overlaps_xz_but_not_y", newBox[T, V](0.5, 5, 0.5, 1.5, 6, 1.5), false},
		{"overlaps_yz_but_not_x", newBox[T, V](5, 0.5, 0.5, 6, 1.5, 1.5), false},
		// The test is a separating-axis test on the two penetration depths, not
		// a test on the thickness of the overlap. A degenerate box therefore
		// still intersects, as long as it sits more than the epsilon away from
		// both faces of the axis it is flat on.
		{"degenerate_point_inside", newBox[T, V](0.5, 0.5, 0.5, 0.5, 0.5, 0.5), true},
		{"flat_on_y_inside", newBox[T, V](0.25, 0.5, 0.25, 0.75, 0.5, 0.75), true},
		{"flat_on_y_lying_on_the_bottom_face", newBox[T, V](0.25, 0, 0.25, 0.75, 0, 0.75), false},
		{"flat_on_y_within_the_epsilon_of_the_bottom_face", newBox[T, V](0.25, 1e-6, 0.25, 0.75, 1e-6, 0.75), false},
		{"flat_on_y_beyond_the_epsilon_of_the_bottom_face", newBox[T, V](0.25, 1e-4, 0.25, 0.75, 1e-4, 0.75), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := unit.IntersectsWith(test.other); got != test.want {
				t.Fatalf("%v.IntersectsWith(%v) = %v, want %v", unit, test.other, got, test.want)
			}
			if got := test.other.IntersectsWith(unit); got != test.want {
				t.Fatalf("%v.IntersectsWith(%v) = %v, want %v (relation is symmetric)", test.other, unit, got, test.want)
			}
		})
	}
}

// TestAnyIntersectionsUsesAZeroEpsilon pins that AnyIntersections is NOT the
// same predicate as IntersectsWith: it passes an epsilon of 0, so an overlap
// too small for IntersectsWith still counts here. Flush faces remain a miss for
// both, because the comparison is strictly greater than.
func TestAnyIntersectionsUsesAZeroEpsilon(t *testing.T) {
	search := Box(0, 0, 0, 1, 1, 1)

	tiny := Box(1-1e-9, 0, 0, 2, 1, 1)
	if search.IntersectsWith(tiny) {
		t.Fatal("IntersectsWith() with a 1e-9 overlap = true, want false")
	}
	if !AnyIntersections([]BBox{tiny}, search) {
		t.Fatal("AnyIntersections() with a 1e-9 overlap = false, want true")
	}

	flush := Box(1, 0, 0, 2, 1, 1)
	if AnyIntersections([]BBox{flush}, search) {
		t.Fatal("AnyIntersections() with flush faces = true, want false")
	}
}

func TestAnyIntersections(t *testing.T) {
	search := Box(0, 0, 0, 1, 1, 1)
	miss, hit := Box(5, 5, 5, 6, 6, 6), Box(0.5, 0.5, 0.5, 1.5, 1.5, 1.5)

	tests := []struct {
		name  string
		boxes []BBox
		want  bool
	}{
		{"nil_slice", nil, false},
		{"empty_slice", []BBox{}, false},
		{"single_miss", []BBox{miss}, false},
		{"single_hit", []BBox{hit}, true},
		{"hit_first", []BBox{hit, miss, miss}, true},
		{"hit_last", []BBox{miss, miss, hit}, true},
		{"hit_in_the_middle", []BBox{miss, hit, miss}, true},
		{"all_miss", []BBox{miss, miss, miss}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AnyIntersections(test.boxes, search); got != test.want {
				t.Fatalf("AnyIntersections(%v, %v) = %v, want %v", test.boxes, search, got, test.want)
			}
		})
	}
}

func TestAnyIntersections32(t *testing.T) {
	search := Box32(0, 0, 0, 1, 1, 1)
	miss, hit := Box32(5, 5, 5, 6, 6, 6), Box32(0.5, 0.5, 0.5, 1.5, 1.5, 1.5)

	if AnyIntersections32(nil, search) {
		t.Fatal("AnyIntersections32(nil, ...) = true, want false")
	}
	if AnyIntersections32([]BBox32{miss}, search) {
		t.Fatal("AnyIntersections32() with a disjoint box = true, want false")
	}
	if !AnyIntersections32([]BBox32{miss, hit}, search) {
		t.Fatal("AnyIntersections32() with an overlapping box = false, want true")
	}
}

// TestVec3WithinIsExclusiveOnEveryBoundary pins the containment rule that
// server/entity relies on: a point exactly on any face of the box is NOT
// within it. Note this is the opposite of the Vec3WithinYZ/XZ/XY family below,
// which is inclusive.
func TestVec3WithinIsExclusiveOnEveryBoundary(t *testing.T) {
	box := Box(0, 0, 0, 1, 1, 1)

	tests := []struct {
		name string
		vec  mgl64.Vec3
		want bool
	}{
		{"centre", mgl64.Vec3{0.5, 0.5, 0.5}, true},
		{"just_inside_the_minimum_corner", mgl64.Vec3{1e-12, 1e-12, 1e-12}, true},
		{"just_inside_the_maximum_corner", mgl64.Vec3{1 - 1e-12, 1 - 1e-12, 1 - 1e-12}, true},
		{"on_the_minimum_corner", mgl64.Vec3{0, 0, 0}, false},
		{"on_the_maximum_corner", mgl64.Vec3{1, 1, 1}, false},
		{"on_the_min_x_face", mgl64.Vec3{0, 0.5, 0.5}, false},
		{"on_the_max_x_face", mgl64.Vec3{1, 0.5, 0.5}, false},
		{"on_the_min_y_face", mgl64.Vec3{0.5, 0, 0.5}, false},
		{"on_the_max_y_face", mgl64.Vec3{0.5, 1, 0.5}, false},
		{"on_the_min_z_face", mgl64.Vec3{0.5, 0.5, 0}, false},
		{"on_the_max_z_face", mgl64.Vec3{0.5, 0.5, 1}, false},
		{"outside_on_x", mgl64.Vec3{2, 0.5, 0.5}, false},
		{"outside_on_y", mgl64.Vec3{0.5, 2, 0.5}, false},
		{"outside_on_z", mgl64.Vec3{0.5, 0.5, 2}, false},
		{"outside_on_negative_x", mgl64.Vec3{-1, 0.5, 0.5}, false},
		{"outside_on_negative_y", mgl64.Vec3{0.5, -1, 0.5}, false},
		{"outside_on_negative_z", mgl64.Vec3{0.5, 0.5, -1}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := box.Vec3Within(test.vec); got != test.want {
				t.Fatalf("Vec3Within(%v) = %v, want %v", test.vec, got, test.want)
			}
		})
	}
}

// TestVec3WithinPlaneIsInclusive pins the two-axis containment tests used by
// BBoxIntercept to decide whether a ray/plane crossing lands on a face. All
// three are INCLUSIVE on both of their boundaries, and each ignores its third
// axis entirely — that is what lets a face hit exactly on an edge count.
func TestVec3WithinPlaneIsInclusive(t *testing.T) {
	box := Box(0, 0, 0, 1, 1, 1)

	// Each entry positions the vector by the two axes the method looks at; the
	// third axis is deliberately set far outside the box to prove it is ignored.
	tests := []struct {
		name string
		fn   func(mgl64.Vec3) bool
		// The indices of the two axes the method inspects, then the ignored one.
		a, b, ignored int
	}{
		{"Vec3WithinYZ", box.Vec3WithinYZ, 1, 2, 0},
		{"Vec3WithinXZ", box.Vec3WithinXZ, 0, 2, 1},
		{"Vec3WithinXY", box.Vec3WithinXY, 0, 1, 2},
	}
	points := []struct {
		name string
		a, b float64
		want bool
	}{
		{"centre", 0.5, 0.5, true},
		{"on_both_minimums", 0, 0, true},
		{"on_both_maximums", 1, 1, true},
		{"on_the_first_minimum", 0, 0.5, true},
		{"on_the_first_maximum", 1, 0.5, true},
		{"on_the_second_minimum", 0.5, 0, true},
		{"on_the_second_maximum", 0.5, 1, true},
		{"just_below_the_first_minimum", -1e-12, 0.5, false},
		{"just_above_the_first_maximum", 1 + 1e-12, 0.5, false},
		{"just_below_the_second_minimum", 0.5, -1e-12, false},
		{"just_above_the_second_maximum", 0.5, 1 + 1e-12, false},
		{"outside_on_both", 5, 5, false},
	}

	for _, test := range tests {
		for _, p := range points {
			t.Run(test.name+"/"+p.name, func(t *testing.T) {
				var vec mgl64.Vec3
				vec[test.a], vec[test.b] = p.a, p.b
				vec[test.ignored] = 1000
				if got := test.fn(vec); got != p.want {
					t.Fatalf("%s(%v) = %v, want %v", test.name, vec, got, p.want)
				}
			})
		}
	}
}

// TestCornersOrder pins the order Corners returns, which callers index into.
func TestCornersOrder(t *testing.T) {
	got := Box(0, 1, 2, 10, 11, 12).Corners()
	want := []mgl64.Vec3{
		{0, 1, 2},    // min
		{10, 11, 12}, // max
		{0, 1, 12},
		{0, 11, 2},
		{0, 11, 12},
		{10, 11, 2},
		{10, 1, 12},
		{10, 1, 2},
	}
	if len(got) != len(want) {
		t.Fatalf("Corners() returned %d corners, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Corners()[%d] = %v, want %v (full result %v)", i, got[i], want[i], got)
		}
	}
}

// TestCornersReturnsAFreshSlice pins that Corners allocates per call, so a
// caller that writes into the result does not disturb the next one.
func TestCornersReturnsAFreshSlice(t *testing.T) {
	box := Box(0, 0, 0, 1, 1, 1)

	first := box.Corners()
	first[0] = mgl64.Vec3{99, 99, 99}

	if second := box.Corners(); second[0] != (mgl64.Vec3{0, 0, 0}) {
		t.Fatalf("Corners()[0] = %v after mutating an earlier result, want {0 0 0}", second[0])
	}
}

// TestBoxDoesNotNormaliseNaN pins that the ordering swaps in newBox are plain
// comparisons, which NaN never satisfies.
//
// PINNED BUG: a NaN coordinate produces a box that is neither ordered nor
// detectable as invalid, and every predicate on it silently answers false.
func TestBoxDoesNotNormaliseNaN(t *testing.T) {
	nan := math.NaN()
	box := Box(1, 0, 0, nan, 1, 1)

	if !math.IsNaN(box.Max()[0]) || box.Min()[0] != 1 {
		t.Fatalf("Box() with a NaN maximum = %v, want the coordinates left in place", box)
	}
	if !math.IsNaN(box.Volume()) {
		t.Fatalf("Volume() of %v = %v, want NaN", box, box.Volume())
	}
	if box.IntersectsWith(Box(0, 0, 0, 2, 2, 2)) {
		t.Fatalf("IntersectsWith() on %v = true, want false", box)
	}
	if box.Vec3Within(mgl64.Vec3{0.5, 0.5, 0.5}) {
		t.Fatalf("Vec3Within() on %v = true, want false", box)
	}
}
