package trace

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
)

// traceEpsilon matches physEpsilon in server/entity, whose hit-detection tests
// sit directly on top of BBoxIntercept.
const traceEpsilon = 1e-9

// unitBox is the box every ray in this file is aimed at.
var unitBox = cube.Box(0, 0, 0, 1, 1, 1)

func assertVec3(t *testing.T, name string, got, want mgl64.Vec3) {
	t.Helper()
	for i := range 3 {
		if math.Abs(got[i]-want[i]) > traceEpsilon {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}

// TestBBoxInterceptEveryFace pins the entry point and reported face for a ray
// coming at the box along each of the six axes directions, in both senses. The
// pairs are exact mirrors of each other, so a face table wired up wrong on one
// axis but not another shows up here.
func TestBBoxInterceptEveryFace(t *testing.T) {
	tests := []struct {
		name       string
		start, end mgl64.Vec3
		wantFace   cube.Face
		wantPos    mgl64.Vec3
	}{
		{"from_the_west", mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{2, 0.5, 0.5}, cube.FaceWest, mgl64.Vec3{0, 0.5, 0.5}},
		{"from_the_east", mgl64.Vec3{2, 0.5, 0.5}, mgl64.Vec3{-1, 0.5, 0.5}, cube.FaceEast, mgl64.Vec3{1, 0.5, 0.5}},
		{"from_below", mgl64.Vec3{0.5, -1, 0.5}, mgl64.Vec3{0.5, 2, 0.5}, cube.FaceDown, mgl64.Vec3{0.5, 0, 0.5}},
		{"from_above", mgl64.Vec3{0.5, 2, 0.5}, mgl64.Vec3{0.5, -1, 0.5}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5}},
		{"from_the_north", mgl64.Vec3{0.5, 0.5, -1}, mgl64.Vec3{0.5, 0.5, 2}, cube.FaceNorth, mgl64.Vec3{0.5, 0.5, 0}},
		{"from_the_south", mgl64.Vec3{0.5, 0.5, 2}, mgl64.Vec3{0.5, 0.5, -1}, cube.FaceSouth, mgl64.Vec3{0.5, 0.5, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, ok := BBoxIntercept(unitBox, test.start, test.end)
			if !ok {
				t.Fatalf("BBoxIntercept(%v, %v) reported no hit, want one", test.start, test.end)
			}
			if got := result.Face(); got != test.wantFace {
				t.Fatalf("Face() = %v, want %v", got, test.wantFace)
			}
			assertVec3(t, "Position()", result.Position(), test.wantPos)
			if got := result.BBox(); got != unitBox {
				t.Fatalf("BBox() = %v, want the box that was passed in, %v", got, unitBox)
			}
		})
	}
}

// TestBBoxInterceptTieBreaksOnFirstCandidate is the pin for the strict `<` in
// the candidate loop: at an exactly equal distance the candidate produced FIRST
// wins. The candidates are produced in the order min-X, max-X, min-Y, max-Y,
// min-Z, max-Z, which is the face priority west, east, down, up, north, south.
//
// server/entity pins its hit-selection contract on this same rule. Turning the
// comparison into `<=` flips every case below to the last tied candidate.
func TestBBoxInterceptTieBreaksOnFirstCandidate(t *testing.T) {
	tests := []struct {
		name       string
		start, end mgl64.Vec3
		wantFace   cube.Face
		wantPos    mgl64.Vec3
		// The face a `<=` comparison would report instead, named so a failure
		// says immediately which way the rule flipped.
		lastCandidate cube.Face
	}{
		{
			// The min-X and min-Y planes are crossed at the same point.
			name:  "west_beats_down_on_the_lower_west_edge",
			start: mgl64.Vec3{-1, -1, 0.5}, end: mgl64.Vec3{1, 1, 0.5},
			wantFace: cube.FaceWest, wantPos: mgl64.Vec3{0, 0, 0.5}, lastCandidate: cube.FaceDown,
		},
		{
			name:  "down_beats_north_on_the_lower_north_edge",
			start: mgl64.Vec3{0.5, -1, -1}, end: mgl64.Vec3{0.5, 1, 1},
			wantFace: cube.FaceDown, wantPos: mgl64.Vec3{0.5, 0, 0}, lastCandidate: cube.FaceNorth,
		},
		{
			name:  "west_beats_north_on_the_north_west_edge",
			start: mgl64.Vec3{-1, 0.5, -1}, end: mgl64.Vec3{1, 0.5, 1},
			wantFace: cube.FaceWest, wantPos: mgl64.Vec3{0, 0.5, 0}, lastCandidate: cube.FaceNorth,
		},
		{
			// All three minimum planes meet at the origin corner.
			name:  "west_beats_everything_on_the_minimum_corner",
			start: mgl64.Vec3{-1, -1, -1}, end: mgl64.Vec3{1, 1, 1},
			wantFace: cube.FaceWest, wantPos: mgl64.Vec3{0, 0, 0}, lastCandidate: cube.FaceNorth,
		},
		{
			// The mirror image: all three maximum planes meet at (1,1,1), and
			// east is the first of that trio.
			name:  "east_beats_everything_on_the_maximum_corner",
			start: mgl64.Vec3{2, 2, 2}, end: mgl64.Vec3{0, 0, 0},
			wantFace: cube.FaceEast, wantPos: mgl64.Vec3{1, 1, 1}, lastCandidate: cube.FaceSouth,
		},
		{
			name:  "up_beats_south_on_the_upper_south_edge",
			start: mgl64.Vec3{0.5, 2, 2}, end: mgl64.Vec3{0.5, 0, 0},
			wantFace: cube.FaceUp, wantPos: mgl64.Vec3{0.5, 1, 1}, lastCandidate: cube.FaceSouth,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, ok := BBoxIntercept(unitBox, test.start, test.end)
			if !ok {
				t.Fatalf("BBoxIntercept(%v, %v) reported no hit, want one", test.start, test.end)
			}
			if got := result.Face(); got != test.wantFace {
				if got == test.lastCandidate {
					t.Fatalf("Face() = %v, want %v: the tie went to the LAST equally distant candidate, so the comparison is no longer strictly less than", got, test.wantFace)
				}
				t.Fatalf("Face() = %v, want %v", got, test.wantFace)
			}
			assertVec3(t, "Position()", result.Position(), test.wantPos)
		})
	}
}

func TestBBoxInterceptMisses(t *testing.T) {
	tests := []struct {
		name       string
		start, end mgl64.Vec3
	}{
		{"segment_stops_short_of_the_box", mgl64.Vec3{-3, 0.5, 0.5}, mgl64.Vec3{-2, 0.5, 0.5}},
		{"segment_starts_past_the_box", mgl64.Vec3{2, 0.5, 0.5}, mgl64.Vec3{3, 0.5, 0.5}},
		{"segment_passes_beside_the_box", mgl64.Vec3{-1, 5, 0.5}, mgl64.Vec3{2, 5, 0.5}},
		{"segment_parallel_to_a_face_but_outside", mgl64.Vec3{-1, 1.5, 0.5}, mgl64.Vec3{2, 1.5, 0.5}},
		// Every helper reports the axis as constant, so no candidate survives.
		{"zero_length_segment_at_the_centre", mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{0.5, 0.5, 0.5}},
		{"zero_length_segment_outside", mgl64.Vec3{9, 9, 9}, mgl64.Vec3{9, 9, 9}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, ok := BBoxIntercept(unitBox, test.start, test.end)
			if ok {
				t.Fatalf("BBoxIntercept(%v, %v) = %v, want no hit", test.start, test.end, result)
			}
			if result != (BBoxResult{}) {
				t.Fatalf("BBoxIntercept() returned %v alongside ok false, want the zero result", result)
			}
		})
	}
}

// TestBBoxInterceptFromInside pins that a ray starting inside the box reports
// the face it LEAVES through, not the one behind it. The near-side candidates
// are dropped because their fraction along the segment is negative.
func TestBBoxInterceptFromInside(t *testing.T) {
	tests := []struct {
		name     string
		end      mgl64.Vec3
		wantFace cube.Face
		wantPos  mgl64.Vec3
	}{
		{"leaving_east", mgl64.Vec3{2, 0.5, 0.5}, cube.FaceEast, mgl64.Vec3{1, 0.5, 0.5}},
		{"leaving_west", mgl64.Vec3{-2, 0.5, 0.5}, cube.FaceWest, mgl64.Vec3{0, 0.5, 0.5}},
		{"leaving_up", mgl64.Vec3{0.5, 2, 0.5}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5}},
		{"leaving_down", mgl64.Vec3{0.5, -2, 0.5}, cube.FaceDown, mgl64.Vec3{0.5, 0, 0.5}},
		{"leaving_south", mgl64.Vec3{0.5, 0.5, 2}, cube.FaceSouth, mgl64.Vec3{0.5, 0.5, 1}},
		{"leaving_north", mgl64.Vec3{0.5, 0.5, -2}, cube.FaceNorth, mgl64.Vec3{0.5, 0.5, 0}},
	}
	start := mgl64.Vec3{0.5, 0.5, 0.5}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, ok := BBoxIntercept(unitBox, start, test.end)
			if !ok {
				t.Fatalf("BBoxIntercept(%v, %v) reported no hit, want one", start, test.end)
			}
			if got := result.Face(); got != test.wantFace {
				t.Fatalf("Face() = %v, want %v", got, test.wantFace)
			}
			assertVec3(t, "Position()", result.Position(), test.wantPos)
		})
	}
}

// TestBBoxInterceptGrazesAFace pins that a ray running exactly along a face
// plane still hits, because the Vec3Within*/plane tests in cube are inclusive
// on their boundaries.
func TestBBoxInterceptGrazesAFace(t *testing.T) {
	result, ok := BBoxIntercept(unitBox, mgl64.Vec3{-1, 0, 0.5}, mgl64.Vec3{2, 0, 0.5})
	if !ok {
		t.Fatal("BBoxIntercept() along the bottom face reported no hit, want one")
	}
	if got := result.Face(); got != cube.FaceWest {
		t.Fatalf("Face() = %v, want west", got)
	}
	assertVec3(t, "Position()", result.Position(), mgl64.Vec3{0, 0, 0.5})

	// Running along an edge of the box works too.
	if _, ok := BBoxIntercept(unitBox, mgl64.Vec3{-1, 0, 0}, mgl64.Vec3{2, 0, 0}); !ok {
		t.Fatal("BBoxIntercept() along the bottom north edge reported no hit, want one")
	}
	// One step further out and it misses.
	if _, ok := BBoxIntercept(unitBox, mgl64.Vec3{-1, -1e-9, 0.5}, mgl64.Vec3{2, -1e-9, 0.5}); ok {
		t.Fatal("BBoxIntercept() just below the bottom face reported a hit, want none")
	}
}

// TestBBoxInterceptEndsExactlyOnTheFace pins the inclusive end of the segment:
// a segment whose far endpoint lands exactly on the box surface counts.
func TestBBoxInterceptEndsExactlyOnTheFace(t *testing.T) {
	if _, ok := BBoxIntercept(unitBox, mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{0, 0.5, 0.5}); !ok {
		t.Fatal("a segment ending exactly on the west face reported no hit, want one")
	}
	if _, ok := BBoxIntercept(unitBox, mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{-1e-9, 0.5, 0.5}); ok {
		t.Fatal("a segment ending just short of the west face reported a hit, want none")
	}
}

func TestVec3OnLineHelpers(t *testing.T) {
	a, b := mgl64.Vec3{0, 0, 0}, mgl64.Vec3{10, 20, 30}

	// The three helpers are the same function on three axes; run the same
	// table through each so a copy-paste slip on one axis is visible.
	tests := []struct {
		name string
		fn   func(a, b mgl64.Vec3, v float64) *mgl64.Vec3
		// The value handed to the helper and the point it must produce.
		at   float64
		want *mgl64.Vec3
	}{
		{"X_at_the_start", vec3OnLineWithX, 0, &mgl64.Vec3{0, 0, 0}},
		{"X_halfway", vec3OnLineWithX, 5, &mgl64.Vec3{5, 10, 15}},
		{"X_at_the_end", vec3OnLineWithX, 10, &mgl64.Vec3{10, 20, 30}},
		{"X_before_the_start", vec3OnLineWithX, -1, nil},
		{"X_past_the_end", vec3OnLineWithX, 11, nil},

		{"Y_at_the_start", vec3OnLineWithY, 0, &mgl64.Vec3{0, 0, 0}},
		{"Y_halfway", vec3OnLineWithY, 10, &mgl64.Vec3{5, 10, 15}},
		{"Y_at_the_end", vec3OnLineWithY, 20, &mgl64.Vec3{10, 20, 30}},
		{"Y_before_the_start", vec3OnLineWithY, -1, nil},
		{"Y_past_the_end", vec3OnLineWithY, 21, nil},

		{"Z_at_the_start", vec3OnLineWithZ, 0, &mgl64.Vec3{0, 0, 0}},
		{"Z_halfway", vec3OnLineWithZ, 15, &mgl64.Vec3{5, 10, 15}},
		{"Z_at_the_end", vec3OnLineWithZ, 30, &mgl64.Vec3{10, 20, 30}},
		{"Z_before_the_start", vec3OnLineWithZ, -1, nil},
		{"Z_past_the_end", vec3OnLineWithZ, 31, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.fn(a, b, test.at)
			if test.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %v", *test.want)
			}
			assertVec3(t, "result", *got, *test.want)
		})
	}
}

// TestVec3OnLineHelpersTreatNearlyEqualEndpointsAsParallel pins that the
// "constant on this axis" test is mgl64.FloatEqual, which is a RELATIVE
// comparison, not an exact one. A ray whose endpoints differ by a relative
// amount below mgl64.Epsilon produces no candidate at all on that axis.
func TestVec3OnLineHelpersTreatNearlyEqualEndpointsAsParallel(t *testing.T) {
	fns := []struct {
		name string
		axis int
		fn   func(a, b mgl64.Vec3, v float64) *mgl64.Vec3
	}{
		{"vec3OnLineWithX", 0, vec3OnLineWithX},
		{"vec3OnLineWithY", 1, vec3OnLineWithY},
		{"vec3OnLineWithZ", 2, vec3OnLineWithZ},
	}
	for _, f := range fns {
		t.Run(f.name, func(t *testing.T) {
			var a, b mgl64.Vec3
			a[f.axis], b[f.axis] = 1, 1
			if got := f.fn(a, b, 1); got != nil {
				t.Fatalf("identical endpoints gave %v, want nil", *got)
			}

			b[f.axis] = 1 + 1e-11
			if got := f.fn(a, b, 1); got != nil {
				t.Fatalf("endpoints 1e-11 apart gave %v, want nil (read as parallel)", *got)
			}

			b[f.axis] = 1 + 1e-8
			if got := f.fn(a, b, 1); got == nil {
				t.Fatal("endpoints 1e-8 apart gave nil, want a point")
			}
		})
	}
}

func TestBBoxIntersects(t *testing.T) {
	tests := []struct {
		name       string
		start, end mgl64.Vec3
		want       bool
	}{
		{"crossing_on_x", mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{2, 0.5, 0.5}, true},
		{"crossing_on_y", mgl64.Vec3{0.5, -1, 0.5}, mgl64.Vec3{0.5, 2, 0.5}, true},
		{"crossing_on_z", mgl64.Vec3{0.5, 0.5, -1}, mgl64.Vec3{0.5, 0.5, 2}, true},
		{"diagonal_through_the_corners", mgl64.Vec3{-1, -1, -1}, mgl64.Vec3{2, 2, 2}, true},
		{"contained_entirely", mgl64.Vec3{0.25, 0.25, 0.25}, mgl64.Vec3{0.75, 0.75, 0.75}, true},
		{"ends_exactly_on_a_face", mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{0, 0.5, 0.5}, true},
		{"running_along_a_face_plane", mgl64.Vec3{-1, 0, 0.5}, mgl64.Vec3{2, 0, 0.5}, true},
		{"stops_short", mgl64.Vec3{-3, 0.5, 0.5}, mgl64.Vec3{-2, 0.5, 0.5}, false},
		{"starts_past", mgl64.Vec3{2, 0.5, 0.5}, mgl64.Vec3{3, 0.5, 0.5}, false},
		{"parallel_and_outside", mgl64.Vec3{-1, 5, 0.5}, mgl64.Vec3{2, 5, 0.5}, false},
		{"misses_on_z_only", mgl64.Vec3{-1, 0.5, 5}, mgl64.Vec3{2, 0.5, 5}, false},
		// Unlike BBoxIntercept, the slab test treats a degenerate segment as a
		// point-in-box query and answers true inside the box.
		{"zero_length_inside", mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{0.5, 0.5, 0.5}, true},
		{"zero_length_outside", mgl64.Vec3{9, 9, 9}, mgl64.Vec3{9, 9, 9}, false},
		{"zero_length_on_a_face", mgl64.Vec3{0, 0.5, 0.5}, mgl64.Vec3{0, 0.5, 0.5}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := BBoxIntersects(unitBox, test.start, test.end); got != test.want {
				t.Fatalf("BBoxIntersects(%v, %v) = %v, want %v", test.start, test.end, got, test.want)
			}
		})
	}
}

// TestBBoxIntersectsAgreesWithIntercept pins that the cheap predicate and the
// full intercept agree on ordinary segments. They deliberately disagree on a
// degenerate segment, which the case list above pins separately.
func TestBBoxIntersectsAgreesWithIntercept(t *testing.T) {
	for x := -2.0; x <= 2.0; x += 0.5 {
		for y := -2.0; y <= 2.0; y += 0.5 {
			start := mgl64.Vec3{x, y, -2}
			end := mgl64.Vec3{x, y, 3}

			_, intercepted := BBoxIntercept(unitBox, start, end)
			if intersects := BBoxIntersects(unitBox, start, end); intersects != intercepted {
				t.Fatalf("for the segment %v -> %v, BBoxIntersects() = %v but BBoxIntercept() = %v", start, end, intersects, intercepted)
			}
		}
	}
}
