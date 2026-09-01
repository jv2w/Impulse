package cube

import (
	"math"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

func TestPosAccessorsAndString(t *testing.T) {
	p := Pos{1, -2, 3}
	if got := p.X(); got != 1 {
		t.Fatalf("X() = %v, want 1", got)
	}
	if got := p.Y(); got != -2 {
		t.Fatalf("Y() = %v, want -2", got)
	}
	if got := p.Z(); got != 3 {
		t.Fatalf("Z() = %v, want 3", got)
	}
	if got := p.String(); got != "(1,-2,3)" {
		t.Fatalf("String() = %q, want %q", got, "(1,-2,3)")
	}
}

// TestPosArithmeticReturnsValues pins that Add, Sub and Side all return a new
// Pos and leave the receiver alone. Pos is an array, so a caller that drops the
// result gets no compiler warning and no effect at all — server/entity found
// exactly that mistake in lightning.go.
func TestPosArithmeticReturnsValues(t *testing.T) {
	p := Pos{1, 2, 3}

	p.Add(Pos{10, 10, 10})
	p.Sub(Pos{10, 10, 10})
	for _, f := range Faces() {
		p.Side(f)
	}
	if p != (Pos{1, 2, 3}) {
		t.Fatalf("receiver = %v after discarding the results, want (1,2,3)", p)
	}

	if got := p.Add(Pos{1, -1, 2}); got != (Pos{2, 1, 5}) {
		t.Fatalf("Add() = %v, want (2,1,5)", got)
	}
	if got := p.Sub(Pos{1, -1, 2}); got != (Pos{0, 3, 1}) {
		t.Fatalf("Sub() = %v, want (0,3,1)", got)
	}
	if got := p.Add(Pos{0, 0, 0}); got != p {
		t.Fatalf("Add() of the zero position = %v, want %v", got, p)
	}
}

func TestPosSide(t *testing.T) {
	p := Pos{5, 10, 15}
	tests := []struct {
		face Face
		want Pos
	}{
		{FaceDown, Pos{5, 9, 15}},
		{FaceUp, Pos{5, 11, 15}},
		{FaceNorth, Pos{5, 10, 14}},
		{FaceSouth, Pos{5, 10, 16}},
		{FaceWest, Pos{4, 10, 15}},
		{FaceEast, Pos{6, 10, 15}},
		// The switch has no default: an out-of-range Face is a no-op.
		{Face(9), Pos{5, 10, 15}},
	}
	for _, test := range tests {
		if got := p.Side(test.face); got != test.want {
			t.Fatalf("Side(%v) = %v, want %v", test.face, got, test.want)
		}
	}
	// Stepping to a side and back to its opposite returns the original.
	for _, f := range Faces() {
		if got := p.Side(f).Side(f.Opposite()); got != p {
			t.Fatalf("Side(%v).Side(%v) = %v, want %v", f, f.Opposite(), got, p)
		}
	}
}

func TestPosNeighbourFace(t *testing.T) {
	p := Pos{5, 10, 15}
	for _, f := range Faces() {
		neighbour := p.Side(f)
		gotFace, ok := p.NeighbourFace(neighbour)
		if !ok || gotFace != f {
			t.Fatalf("NeighbourFace(%v) = (%v, %v), want (%v, true)", neighbour, gotFace, ok, f)
		}
		if got := p.Face(neighbour); got != f {
			t.Fatalf("Face(%v) = %v, want %v", neighbour, got, f)
		}
	}

	// Anything that is not a direct neighbour reports FaceUp with ok false, so
	// Face alone cannot distinguish a real up neighbour from a miss.
	for _, other := range []Pos{p, {6, 11, 15}, {7, 10, 15}, {5, 10, 17}} {
		gotFace, ok := p.NeighbourFace(other)
		if ok {
			t.Fatalf("NeighbourFace(%v) = (%v, true), want ok false", other, gotFace)
		}
		if gotFace != FaceUp {
			t.Fatalf("NeighbourFace(%v) = %v on failure, want up", other, gotFace)
		}
		if got := p.Face(other); got != FaceUp {
			t.Fatalf("Face(%v) = %v, want up (the failure value)", other, got)
		}
	}
}

func TestPosVec3Conversions(t *testing.T) {
	p := Pos{1, 2, 3}
	if got := p.Vec3(); got != (mgl64.Vec3{1, 2, 3}) {
		t.Fatalf("Vec3() = %v, want {1 2 3}", got)
	}
	// Vec3Middle only centres the horizontal axes; Y stays at the block floor.
	if got := p.Vec3Middle(); got != (mgl64.Vec3{1.5, 2, 3.5}) {
		t.Fatalf("Vec3Middle() = %v, want {1.5 2 3.5}", got)
	}
	if got := p.Vec3Centre(); got != (mgl64.Vec3{1.5, 2.5, 3.5}) {
		t.Fatalf("Vec3Centre() = %v, want {1.5 2.5 3.5}", got)
	}
}

func TestPosFromVec3FloorsTowardsNegativeInfinity(t *testing.T) {
	tests := []struct {
		vec  mgl64.Vec3
		want Pos
	}{
		{mgl64.Vec3{0, 0, 0}, Pos{0, 0, 0}},
		{mgl64.Vec3{0.9, 0.9, 0.9}, Pos{0, 0, 0}},
		{mgl64.Vec3{1, 1, 1}, Pos{1, 1, 1}},
		// Truncation towards zero would give (0,0,0) here; flooring gives -1.
		{mgl64.Vec3{-0.1, -0.1, -0.1}, Pos{-1, -1, -1}},
		{mgl64.Vec3{-1, -1, -1}, Pos{-1, -1, -1}},
		{mgl64.Vec3{-1.5, 2.5, -0.5}, Pos{-2, 2, -1}},
	}
	for _, test := range tests {
		if got := PosFromVec3(test.vec); got != test.want {
			t.Fatalf("PosFromVec3(%v) = %v, want %v", test.vec, got, test.want)
		}
	}
	// Round trip: the position of a block always floors back to itself.
	for _, p := range []Pos{{0, 0, 0}, {-1, -1, -1}, {16, 320, -16}} {
		if got := PosFromVec3(p.Vec3()); got != p {
			t.Fatalf("PosFromVec3(%v.Vec3()) = %v, want %v", p, got, p)
		}
		if got := PosFromVec3(p.Vec3Centre()); got != p {
			t.Fatalf("PosFromVec3(%v.Vec3Centre()) = %v, want %v", p, got, p)
		}
	}
}

func TestPosOutOfBoundsAndWithin(t *testing.T) {
	r := Range{-64, 319}

	// OutOfBounds only looks at Y, and both ends are inclusive.
	tests := []struct {
		pos  Pos
		want bool
	}{
		{Pos{0, -64, 0}, false},
		{Pos{0, 319, 0}, false},
		{Pos{0, 0, 0}, false},
		{Pos{0, -65, 0}, true},
		{Pos{0, 320, 0}, true},
		{Pos{1000000, 0, -1000000}, false},
	}
	for _, test := range tests {
		if got := test.pos.OutOfBounds(r); got != test.want {
			t.Fatalf("%v.OutOfBounds(%v) = %v, want %v", test.pos, r, got, test.want)
		}
	}

	minimum, maximum := Pos{0, 0, 0}, Pos{2, 2, 2}
	withinTests := []struct {
		pos  Pos
		want bool
	}{
		{Pos{1, 1, 1}, true},
		{Pos{0, 0, 0}, true},
		{Pos{2, 2, 2}, true},
		{Pos{-1, 1, 1}, false},
		{Pos{1, -1, 1}, false},
		{Pos{1, 1, -1}, false},
		{Pos{3, 1, 1}, false},
		{Pos{1, 3, 1}, false},
		{Pos{1, 1, 3}, false},
	}
	for _, test := range withinTests {
		if got := test.pos.Within(minimum, maximum); got != test.want {
			t.Fatalf("%v.Within(%v, %v) = %v, want %v", test.pos, minimum, maximum, got, test.want)
		}
	}
}

// TestPosNeighboursOrder pins both the order the callback is invoked in and the
// Y clipping, which is the only bounds check Neighbours performs.
func TestPosNeighboursOrder(t *testing.T) {
	collect := func(p Pos, r Range) []Pos {
		var got []Pos
		p.Neighbours(func(n Pos) { got = append(got, n) }, r)
		return got
	}

	t.Run("in_the_middle_of_the_range", func(t *testing.T) {
		want := []Pos{{2, 2, 3}, {0, 2, 3}, {1, 3, 3}, {1, 1, 3}, {1, 2, 4}, {1, 2, 2}}
		if got := collect(Pos{1, 2, 3}, Range{0, 10}); !slices.Equal(got, want) {
			t.Fatalf("Neighbours() = %v, want %v (+x, -x, +y, -y, +z, -z)", got, want)
		}
	})
	t.Run("on_the_floor_drops_the_lower_neighbour", func(t *testing.T) {
		want := []Pos{{2, 0, 3}, {0, 0, 3}, {1, 1, 3}, {1, 0, 4}, {1, 0, 2}}
		if got := collect(Pos{1, 0, 3}, Range{0, 10}); !slices.Equal(got, want) {
			t.Fatalf("Neighbours() = %v, want %v", got, want)
		}
	})
	t.Run("on_the_ceiling_drops_the_upper_neighbour", func(t *testing.T) {
		want := []Pos{{2, 10, 3}, {0, 10, 3}, {1, 9, 3}, {1, 10, 4}, {1, 10, 2}}
		if got := collect(Pos{1, 10, 3}, Range{0, 10}); !slices.Equal(got, want) {
			t.Fatalf("Neighbours() = %v, want %v", got, want)
		}
	})
	t.Run("out_of_bounds_calls_nothing", func(t *testing.T) {
		if got := collect(Pos{1, 11, 3}, Range{0, 10}); got != nil {
			t.Fatalf("Neighbours() from outside the range = %v, want nothing", got)
		}
	})
	t.Run("a_single_layer_range_yields_only_the_horizontal_four", func(t *testing.T) {
		want := []Pos{{2, 5, 3}, {0, 5, 3}, {1, 5, 4}, {1, 5, 2}}
		if got := collect(Pos{1, 5, 3}, Range{5, 5}); !slices.Equal(got, want) {
			t.Fatalf("Neighbours() = %v, want %v", got, want)
		}
	})
}

// TestPosNeighboursLeavesTheReceiverAlone pins that Neighbours restores the
// position it walks: it mutates its own copy in place while iterating.
func TestPosNeighboursLeavesTheReceiverAlone(t *testing.T) {
	p := Pos{1, 2, 3}
	p.Neighbours(func(Pos) {}, Range{0, 10})
	if p != (Pos{1, 2, 3}) {
		t.Fatalf("receiver = %v after Neighbours(), want (1,2,3)", p)
	}
}

func TestPosMinMax(t *testing.T) {
	a, b := Pos{1, -5, 3}, Pos{-2, 4, 3}
	if got := Min(a, b); got != (Pos{-2, -5, 3}) {
		t.Fatalf("Min(%v, %v) = %v, want (-2,-5,3)", a, b, got)
	}
	if got := Max(a, b); got != (Pos{1, 4, 3}) {
		t.Fatalf("Max(%v, %v) = %v, want (1,4,3)", a, b, got)
	}
	if got := Min(b, a); got != Min(a, b) {
		t.Fatalf("Min() is not symmetric: %v vs %v", got, Min(a, b))
	}
	if got := Max(b, a); got != Max(a, b) {
		t.Fatalf("Max() is not symmetric: %v vs %v", got, Max(a, b))
	}
}

func TestRange3D(t *testing.T) {
	t.Run("order_is_x_then_y_then_z", func(t *testing.T) {
		want := []Pos{
			{0, 0, 0}, {0, 0, 1}, {0, 1, 0}, {0, 1, 1},
			{1, 0, 0}, {1, 0, 1}, {1, 1, 0}, {1, 1, 1},
		}
		got := slices.Collect(Range3D(Pos{0, 0, 0}, Pos{1, 1, 1}))
		if !slices.Equal(got, want) {
			t.Fatalf("Range3D() = %v, want %v", got, want)
		}
	})
	t.Run("bounds_are_inclusive_and_order_independent", func(t *testing.T) {
		forwards := slices.Collect(Range3D(Pos{-1, -1, -1}, Pos{1, 1, 1}))
		backwards := slices.Collect(Range3D(Pos{1, 1, 1}, Pos{-1, -1, -1}))
		if len(forwards) != 27 {
			t.Fatalf("Range3D() over a 3x3x3 volume yielded %d positions, want 27", len(forwards))
		}
		if !slices.Equal(forwards, backwards) {
			t.Fatal("Range3D() with the corners swapped yielded a different sequence")
		}
	})
	t.Run("a_single_position_yields_itself", func(t *testing.T) {
		want := []Pos{{4, 5, 6}}
		if got := slices.Collect(Range3D(Pos{4, 5, 6}, Pos{4, 5, 6})); !slices.Equal(got, want) {
			t.Fatalf("Range3D() = %v, want %v", got, want)
		}
	})
	t.Run("stops_when_the_caller_breaks", func(t *testing.T) {
		var got []Pos
		for p := range Range3D(Pos{0, 0, 0}, Pos{5, 5, 5}) {
			got = append(got, p)
			if len(got) == 3 {
				break
			}
		}
		want := []Pos{{0, 0, 0}, {0, 0, 1}, {0, 0, 2}}
		if !slices.Equal(got, want) {
			t.Fatalf("Range3D() before breaking = %v, want %v", got, want)
		}
	})
}

func TestRange(t *testing.T) {
	tests := []struct {
		r                      Range
		minimum, maximum, size int
	}{
		{Range{-64, 319}, -64, 319, 383},
		{Range{0, 255}, 0, 255, 255},
		{Range{5, 5}, 5, 5, 0},
		// Range is a bare array with no constructor, so an inverted one is
		// possible and simply yields a negative height.
		{Range{10, 0}, 10, 0, -10},
	}
	for _, test := range tests {
		if got := test.r.Min(); got != test.minimum {
			t.Fatalf("%v.Min() = %v, want %v", test.r, got, test.minimum)
		}
		if got := test.r.Max(); got != test.maximum {
			t.Fatalf("%v.Max() = %v, want %v", test.r, got, test.maximum)
		}
		if got := test.r.Height(); got != test.size {
			t.Fatalf("%v.Height() = %v, want %v", test.r, got, test.size)
		}
	}
}

// TestPosFromVec3OnHugeValues pins that the conversion is an unchecked int
// cast.
//
// PINNED BUG: a non-finite or out-of-int64 coordinate produces an
// implementation-defined position rather than an error, which is how an
// infinite velocity reaches the block iteration loop in server/entity. Pinned
// as-is; decide in stage 1.
func TestPosFromVec3OnHugeValues(t *testing.T) {
	// Only the fact that this does not panic is pinned; the value itself is
	// platform-defined, so it is deliberately not asserted.
	_ = PosFromVec3(mgl64.Vec3{math.Inf(1), 0, 0})
	_ = PosFromVec3(mgl64.Vec3{math.NaN(), 0, 0})
}
