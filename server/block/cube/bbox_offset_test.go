package cube

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// XOffset, YOffset and ZOffset are the whole of collision resolution: the axis
// order pinned in server/entity (Y, then X, then Z) only produces the values it
// does because each of these three clamps a delta the way it does here. The
// rules they implement, pinned by the tables below:
//
//  1. If the two boxes do not strictly overlap on BOTH axes perpendicular to
//     the one being resolved, the delta is returned untouched. Boxes that touch
//     exactly on a perpendicular axis count as NOT overlapping.
//  2. A positive delta is clamped only when the box is already entirely on the
//     negative side of nearby (box.max <= nearby.min). The clamp is
//     min(delta, nearby.min-box.max), i.e. the gap.
//  3. A negative delta is clamped only when the box is already entirely on the
//     positive side of nearby (box.min >= nearby.max), to
//     max(delta, nearby.max-box.min).
//  4. A delta of 0 is returned as 0; boxes that already interpenetrate on the
//     resolved axis are NOT pushed apart, the delta passes through unchanged.
//
// The generic boundingBox has two instantiations, BBox (float64) and BBox32
// (float32). Every table below runs against both.

// offsetEpsilon64 matches physEpsilon used by the server/entity tests that sit
// on top of these functions. offsetEpsilon32 is loosened because a float32
// mantissa cannot carry the float64 golden values exactly.
const (
	offsetEpsilon64 = 1e-9
	offsetEpsilon32 = 1e-6
)

// offsetCase describes one XOffset/YOffset/ZOffset call in axis-independent
// terms: an interval along the axis being resolved, plus one interval that both
// perpendicular axes carry. Running the same case against all three functions
// is what makes an X/Z (or X/Y) mix-up in the implementation observable.
type offsetCase struct {
	name string
	// Along the axis being resolved.
	boxLo, boxHi   float64
	nearLo, nearHi float64
	// On both axes perpendicular to it.
	perpBoxLo, perpBoxHi   float64
	perpNearLo, perpNearHi float64

	delta float64
	want  float64
}

// offsetCases covers the four rules above. Unless a case says otherwise both
// perpendicular axes span [0, 1] for each box, i.e. they overlap fully.
var offsetCases = []offsetCase{
	// Rule 2: positive delta, box on the negative side of nearby.
	{
		name:  "positive_delta_clamped_to_gap",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 3, want: 1,
	},
	{
		name:  "positive_delta_smaller_than_gap_passes_through",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 0.5, want: 0.5,
	},
	{
		name:  "positive_delta_exactly_the_gap",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 1, want: 1,
	},
	{
		name:  "positive_delta_clamped_to_a_large_gap",
		boxLo: 0, boxHi: 1, nearLo: 10, nearHi: 11,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 100, want: 9,
	},
	{
		// Faces flush against each other: the gap is 0, so all motion is eaten.
		name:  "positive_delta_against_a_flush_face_gives_zero",
		boxLo: 0, boxHi: 1, nearLo: 1, nearHi: 2,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 1, want: 0,
	},
	{
		// Rule 2 requires box.max <= nearby.min, so a box moving away from a
		// nearby that sits behind it is never clamped.
		name:  "positive_delta_away_from_nearby_behind",
		boxLo: 2, boxHi: 3, nearLo: 0, nearHi: 1,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 1, want: 1,
	},

	// Rule 3: negative delta, box on the positive side of nearby.
	{
		name:  "negative_delta_clamped_to_gap",
		boxLo: 2, boxHi: 3, nearLo: 0, nearHi: 1,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: -3, want: -1,
	},
	{
		name:  "negative_delta_smaller_than_gap_passes_through",
		boxLo: 2, boxHi: 3, nearLo: 0, nearHi: 1,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: -0.5, want: -0.5,
	},
	{
		name:  "negative_delta_against_a_flush_face_gives_zero",
		boxLo: 1, boxHi: 2, nearLo: 0, nearHi: 1,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: -1, want: 0,
	},
	{
		name:  "negative_delta_away_from_nearby_ahead",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: -3, want: -3,
	},

	// Rule 4: zero delta and existing penetration.
	{
		name:  "zero_delta_stays_zero",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 0, want: 0,
	},
	{
		// Already interpenetrating on the resolved axis. Neither clamp branch
		// applies, so the delta drives the box further in rather than out.
		name:  "penetrating_positive_delta_is_not_pushed_out",
		boxLo: 0, boxHi: 2, nearLo: 1, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 1, want: 1,
	},
	{
		name:  "penetrating_negative_delta_is_not_pushed_out",
		boxLo: 1, boxHi: 3, nearLo: 0, nearHi: 2,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: -1, want: -1,
	},
	{
		name:  "identical_boxes_pass_the_delta_through",
		boxLo: 0, boxHi: 1, nearLo: 0, nearHi: 1,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 1,
		delta: 0.5, want: 0.5,
	},

	// Rule 1: the perpendicular overlap gate.
	{
		// Touching exactly on the perpendicular axes reads as no overlap, so
		// the box slides past the corner at full speed.
		name:  "perpendicular_faces_flush_disables_the_clamp",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 1, perpNearHi: 2,
		delta: 3, want: 3,
	},
	{
		name:  "perpendicular_disjoint_disables_the_clamp",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 5, perpNearHi: 6,
		delta: 3, want: 3,
	},
	{
		// The smallest overlap still counts: the gate is strict, not epsilon'd.
		name:  "perpendicular_sliver_overlap_enables_the_clamp",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0.999, perpNearHi: 2,
		delta: 3, want: 1,
	},
	{
		name:  "perpendicular_nearby_fully_inside_box_enables_the_clamp",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0.25, perpNearHi: 0.75,
		delta: 3, want: 1,
	},
	{
		// The gate is a pair of separation tests, not a positive overlap test:
		// a degenerate nearby with zero thickness still counts as overlapping
		// as long as it sits strictly inside the box on that axis.
		name:  "perpendicular_zero_thickness_nearby_inside_the_box_enables_the_clamp",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0.5, perpNearHi: 0.5,
		delta: 3, want: 1,
	},
	{
		// The same degenerate nearby laid on the box's own face is separated
		// again, because the test uses box.min >= nearby.max.
		name:  "perpendicular_zero_thickness_nearby_on_the_box_face_disables_the_clamp",
		boxLo: 0, boxHi: 1, nearLo: 2, nearHi: 3,
		perpBoxLo: 0, perpBoxHi: 1, perpNearLo: 0, perpNearHi: 0,
		delta: 3, want: 3,
	},
}

func TestBBoxOffsetRules(t *testing.T) {
	runOffsetCases[float64, mgl64.Vec3](t, offsetCases, offsetEpsilon64)
}

func TestBBox32OffsetRules(t *testing.T) {
	runOffsetCases[float32, mgl32.Vec3](t, offsetCases, offsetEpsilon32)
}

// runOffsetCases runs every case against XOffset, YOffset and ZOffset, laying
// the case out along that function's own axis each time. A body swapped between
// two of the three functions changes the answer for at least one case here.
func runOffsetCases[T number, V vec3[T]](t *testing.T, cases []offsetCase, epsilon float64) {
	t.Helper()
	for _, fn := range offsetFuncs[T, V]() {
		for _, c := range cases {
			t.Run(fn.name+"/"+c.name, func(t *testing.T) {
				box, nearby := offsetBoxes[T, V](c, fn.axis)
				got := float64(fn.call(box, nearby, T(c.delta)))
				if math.Abs(got-c.want) > epsilon {
					t.Fatalf("%v.%s(%v, %v) = %v, want %v", box, fn.name, nearby, c.delta, got, c.want)
				}
			})
		}
	}
}

type offsetFunc[T number, V vec3[T]] struct {
	name string
	axis int
	call func(box, nearby boundingBox[T, V], delta T) T
}

func offsetFuncs[T number, V vec3[T]]() []offsetFunc[T, V] {
	return []offsetFunc[T, V]{
		{"XOffset", 0, func(b, n boundingBox[T, V], d T) T { return b.XOffset(n, d) }},
		{"YOffset", 1, func(b, n boundingBox[T, V], d T) T { return b.YOffset(n, d) }},
		{"ZOffset", 2, func(b, n boundingBox[T, V], d T) T { return b.ZOffset(n, d) }},
	}
}

// offsetBoxes lays c out with the resolved interval on axis and the
// perpendicular interval on both other axes.
func offsetBoxes[T number, V vec3[T]](c offsetCase, axis int) (box, nearby boundingBox[T, V]) {
	var boxMin, boxMax, nearMin, nearMax V
	for i := range 3 {
		if i == axis {
			boxMin[i], boxMax[i] = T(c.boxLo), T(c.boxHi)
			nearMin[i], nearMax[i] = T(c.nearLo), T(c.nearHi)
			continue
		}
		boxMin[i], boxMax[i] = T(c.perpBoxLo), T(c.perpBoxHi)
		nearMin[i], nearMax[i] = T(c.perpNearLo), T(c.perpNearHi)
	}
	return boundingBox[T, V]{min: boxMin, max: boxMax}, boundingBox[T, V]{min: nearMin, max: nearMax}
}

// TestBBoxOffsetResolvesOnlyItsOwnAxis is the direct guard against two of the
// three functions being wired to the wrong axis. The unit box is given three
// separated neighbours, one per axis, each with a different gap. Only the
// function named after an axis may clamp against that axis' neighbour; the
// other two must hand the delta back untouched.
func TestBBoxOffsetResolvesOnlyItsOwnAxis(t *testing.T) {
	const delta = 100.0

	box := Box(0, 0, 0, 1, 1, 1)
	nearby := map[string]BBox{
		"X": Box(2, 0, 0, 3, 1, 1), // gap of 1 on X
		"Y": Box(0, 4, 0, 1, 5, 1), // gap of 3 on Y
		"Z": Box(0, 0, 7, 1, 1, 8), // gap of 6 on Z
	}
	// want[fn][nearby].
	want := map[string]map[string]float64{
		"XOffset": {"X": 1, "Y": delta, "Z": delta},
		"YOffset": {"X": delta, "Y": 3, "Z": delta},
		"ZOffset": {"X": delta, "Y": delta, "Z": 6},
	}

	for _, fn := range offsetFuncs[float64, mgl64.Vec3]() {
		for axis, n := range nearby {
			t.Run(fn.name+"/nearby_on_"+axis, func(t *testing.T) {
				got := fn.call(box, n, delta)
				if math.Abs(got-want[fn.name][axis]) > offsetEpsilon64 {
					t.Fatalf("%s against the %s neighbour = %v, want %v", fn.name, axis, got, want[fn.name][axis])
				}
			})
		}
	}
}

// TestBBoxOffsetChecksBothPerpendicularAxes pins that the overlap gate is an
// AND over both perpendicular axes, and that each function looks at the correct
// pair. Overlapping on only one of the two is not enough to clamp.
func TestBBoxOffsetChecksBothPerpendicularAxes(t *testing.T) {
	const delta = 3.0

	// The box always sits at [0,1] on every axis and the neighbour is separated
	// by a gap of 1 on the resolved axis. "far" pushes a perpendicular axis out
	// of the box's span.
	const far = 5.0

	tests := []struct {
		name   string
		fn     string
		nearby BBox
		want   float64
	}{
		{"XOffset/both_perpendicular_overlap", "XOffset", Box(2, 0, 0, 3, 1, 1), 1},
		{"XOffset/only_y_overlaps", "XOffset", Box(2, 0, far, 3, 1, far+1), delta},
		{"XOffset/only_z_overlaps", "XOffset", Box(2, far, 0, 3, far+1, 1), delta},
		{"XOffset/neither_overlaps", "XOffset", Box(2, far, far, 3, far+1, far+1), delta},

		{"YOffset/both_perpendicular_overlap", "YOffset", Box(0, 2, 0, 1, 3, 1), 1},
		{"YOffset/only_x_overlaps", "YOffset", Box(0, 2, far, 1, 3, far+1), delta},
		{"YOffset/only_z_overlaps", "YOffset", Box(far, 2, 0, far+1, 3, 1), delta},
		{"YOffset/neither_overlaps", "YOffset", Box(far, 2, far, far+1, 3, far+1), delta},

		{"ZOffset/both_perpendicular_overlap", "ZOffset", Box(0, 0, 2, 1, 1, 3), 1},
		{"ZOffset/only_x_overlaps", "ZOffset", Box(0, far, 2, 1, far+1, 3), delta},
		{"ZOffset/only_y_overlaps", "ZOffset", Box(far, 0, 2, far+1, 1, 3), delta},
		{"ZOffset/neither_overlaps", "ZOffset", Box(far, far, 2, far+1, far+1, 3), delta},
	}

	box := Box(0, 0, 0, 1, 1, 1)
	byName := map[string]func(b, n BBox, d float64) float64{}
	for _, fn := range offsetFuncs[float64, mgl64.Vec3]() {
		byName[fn.name] = fn.call
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := byName[test.fn](box, test.nearby, delta); math.Abs(got-test.want) > offsetEpsilon64 {
				t.Fatalf("%s(%v, %v) = %v, want %v", test.fn, test.nearby, delta, got, test.want)
			}
		})
	}
}

// TestBBoxOffsetNeverOvershootsTheDelta pins the documented contract of all
// three functions: the value returned keeps the sign of the delta (or is zero)
// and never exceeds it in magnitude, for every relative placement of two unit
// boxes on a lattice.
func TestBBoxOffsetNeverOvershootsTheDelta(t *testing.T) {
	box := Box(0, 0, 0, 1, 1, 1)
	deltas := []float64{-4, -1, -0.5, 0, 0.5, 1, 4}

	for _, fn := range offsetFuncs[float64, mgl64.Vec3]() {
		for x := -2.0; x <= 2.0; x += 0.5 {
			for y := -2.0; y <= 2.0; y += 0.5 {
				for z := -2.0; z <= 2.0; z += 0.5 {
					nearby := Box(x, y, z, x+1, y+1, z+1)
					for _, delta := range deltas {
						got := fn.call(box, nearby, delta)
						if math.Abs(got) > math.Abs(delta)+offsetEpsilon64 {
							t.Fatalf("%s(%v, %v) = %v, larger in magnitude than the delta", fn.name, nearby, delta, got)
						}
						if got != 0 && math.Signbit(got) != math.Signbit(delta) {
							t.Fatalf("%s(%v, %v) = %v, sign flipped", fn.name, nearby, delta, got)
						}
					}
				}
			}
		}
	}
}

// TestBBoxOffsetLeavesBothBoxesUnchanged pins that the offset functions are
// pure: server/entity calls them in a loop over every nearby block box and
// relies on the receiver being unmodified between calls.
func TestBBoxOffsetLeavesBothBoxesUnchanged(t *testing.T) {
	box, nearby := Box(0, 0, 0, 1, 1, 1), Box(2, 0, 0, 3, 1, 1)
	boxBefore, nearbyBefore := box, nearby

	for _, fn := range offsetFuncs[float64, mgl64.Vec3]() {
		fn.call(box, nearby, 5)
	}
	if box != boxBefore {
		t.Fatalf("receiver = %v after the offset calls, want %v", box, boxBefore)
	}
	if nearby != nearbyBefore {
		t.Fatalf("nearby = %v after the offset calls, want %v", nearby, nearbyBefore)
	}
}
