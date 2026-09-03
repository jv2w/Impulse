package model

import (
	"fmt"
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// These are behaviour-pinning tests laid down ahead of a refactor. Every
// expected value is a golden literal recording what the package produces today,
// not what vanilla or the doc comments say it ought to produce. A failure here
// means a model changed shape; that is a signal to look, not proof of a defect.

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

// fakeBlock is a world.Block carrying nothing but a model. EncodeBlock and Hash
// are never consulted by this package, so they return fixed values.
type fakeBlock struct{ model world.BlockModel }

func (fakeBlock) EncodeBlock() (string, map[string]any) { return "impulse:test", nil }

func (fakeBlock) Hash() (uint64, uint64) { return 0, 0 }

func (b fakeBlock) Model() world.BlockModel { return b.model }

// fakeSource answers Block from the positions it holds. A position it does not
// hold reports a block modelled by Empty, which is what the real world yields
// for air: no collision boxes and no solid faces. Models that probe their
// neighbours therefore see "nothing there" outside the positions a test sets up.
type fakeSource map[cube.Pos]world.Block

func (f fakeSource) Block(pos cube.Pos) world.Block {
	if b, ok := f[pos]; ok {
		return b
	}
	return fakeBlock{model: Empty{}}
}

// emptySource holds no blocks at all; every lookup reports air.
var emptySource = fakeSource{}

// neighbours builds a source placing a block with each model at pos.Side(face).
func neighbours(pos cube.Pos, models map[cube.Face]world.BlockModel) fakeSource {
	s := fakeSource{}
	for face, m := range models {
		s[pos.Side(face)] = fakeBlock{model: m}
	}
	return s
}

// origin is where every position-independent model is queried. Only Bamboo
// reads its position, and it is given positions of its own.
var origin = cube.Pos{4, 64, -7}

const boxEpsilon = 1e-9

// float32Epsilon is looser than boxEpsilon because the offset helpers compute in
// float32, where a quantised step such as 8*0.5/15 carries only about seven
// digits. Any change of step index or formula moves a result far more than this.
const float32Epsilon = 1e-6

func vecClose(a, b mgl64.Vec3) bool {
	for i := range 3 {
		if math.Abs(a[i]-b[i]) > boxEpsilon {
			return false
		}
	}
	return true
}

func formatBoxes(boxes []cube.BBox) string {
	if len(boxes) == 0 {
		return "[]"
	}
	s := ""
	for i, b := range boxes {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%d:%v-%v", i, b.Min(), b.Max())
	}
	return "[" + s + "]"
}

func assertBoxes(t *testing.T, name string, got, want []cube.BBox) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: box count = %d, want %d\n got: %s\nwant: %s",
			name, len(got), len(want), formatBoxes(got), formatBoxes(want))
	}
	for i := range want {
		if !vecClose(got[i].Min(), want[i].Min()) || !vecClose(got[i].Max(), want[i].Max()) {
			t.Errorf("%s: box %d = %v-%v, want %v-%v",
				name, i, got[i].Min(), got[i].Max(), want[i].Min(), want[i].Max())
		}
	}
}

// allFaces lists the six faces in the order down, up, north, south, west, east.
// A fresh slice is returned so a caller may keep or extend it.
func allFaces() []cube.Face {
	return []cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast}
}

// assertSolidFaces checks FaceSolid on all six faces: the faces listed in solid
// must return true and every other face must return false.
func assertSolidFaces(t *testing.T, name string, m world.BlockModel, pos cube.Pos, s world.BlockSource, solid ...cube.Face) {
	t.Helper()
	set := map[cube.Face]bool{}
	for _, f := range solid {
		set[f] = true
	}
	for _, f := range allFaces() {
		if got := m.FaceSolid(pos, f, s); got != set[f] {
			t.Errorf("%s: FaceSolid(%v) = %v, want %v", name, f, got, set[f])
		}
	}
}

func allDirections() [4]cube.Direction {
	return [4]cube.Direction{cube.North, cube.South, cube.West, cube.East}
}

func allAxes() [3]cube.Axis {
	return [3]cube.Axis{cube.X, cube.Y, cube.Z}
}

func boolName(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// Slice ownership
// ---------------------------------------------------------------------------

func TestSolidBBoxReturnsFreshSlice(t *testing.T) {
	pristine := cube.Box(0, 0, 0, 1, 1, 1)

	first := Solid{}.BBox(origin, emptySource)
	assertBoxes(t, "Solid first call", first, []cube.BBox{pristine})

	second := Solid{}.BBox(origin, emptySource)
	assertBoxes(t, "Solid second call", second, []cube.BBox{pristine})
	if &first[0] == &second[0] {
		t.Fatalf("Solid.BBox shares its backing array between calls")
	}

	first[0] = cube.Box(0, 0, 0, 5, 5, 5)
	third := Solid{}.BBox(origin, emptySource)
	assertBoxes(t, "Solid after caller mutation", third, []cube.BBox{pristine})
	if full != pristine {
		t.Fatalf("package-level full was mutated through the returned slice: %v-%v", full.Min(), full.Max())
	}
}

func TestLeavesBBoxReturnsFreshSlice(t *testing.T) {
	pristine := cube.Box(0, 0, 0, 1, 1, 1)

	first := Leaves{}.BBox(origin, emptySource)
	assertBoxes(t, "Leaves first call", first, []cube.BBox{pristine})

	first[0] = cube.Box(0, 0, 0, 5, 5, 5)
	second := Leaves{}.BBox(origin, emptySource)
	assertBoxes(t, "Leaves after caller mutation", second, []cube.BBox{pristine})
	if full != pristine {
		t.Fatalf("package-level full was mutated through the returned slice: %v-%v", full.Min(), full.Max())
	}
}

func TestEmptyBBoxIsNil(t *testing.T) {
	if got := (Empty{}).BBox(origin, emptySource); got != nil {
		t.Fatalf("Empty.BBox = %s, want nil", formatBoxes(got))
	}
	// A second call must be equally nil; nothing is cached or reused.
	if got := (Empty{}).BBox(cube.Pos{1, 2, 3}, emptySource); got != nil {
		t.Fatalf("Empty.BBox = %s, want nil", formatBoxes(got))
	}
}

func TestPortalBBoxIsNil(t *testing.T) {
	// PINNED SCOPE: Portal.Axis is stored but never read; the model has no
	// collision box on any axis. Out of scope.
	for _, axis := range allAxes() {
		t.Run(axis.String(), func(t *testing.T) {
			if got := (Portal{Axis: axis}).BBox(origin, emptySource); got != nil {
				t.Fatalf("Portal{%v}.BBox = %s, want nil", axis, formatBoxes(got))
			}
		})
	}
}

func TestFenceGateOpenBBoxIsNil(t *testing.T) {
	for _, dir := range allDirections() {
		t.Run(dir.String(), func(t *testing.T) {
			if got := (FenceGate{Facing: dir, Open: true}).BBox(origin, emptySource); got != nil {
				t.Fatalf("FenceGate{%v, open}.BBox = %s, want nil", dir, formatBoxes(got))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Models with a fixed box set
// ---------------------------------------------------------------------------

func TestFixedBoxModels(t *testing.T) {
	for _, c := range []struct {
		name  string
		model world.BlockModel
		want  []cube.BBox
	}{
		{"Bed", Bed{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.5625, 1)}},
		{"Cactus", Cactus{}, []cube.BBox{cube.Box(0.025, 0, 0.025, 0.975, 1, 0.975)}},
		{"Campfire", Campfire{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.4375, 1)}},
		{"Carpet", Carpet{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.0625, 1)}},
		{"Chest", Chest{}, []cube.BBox{cube.Box(0.025, 0, 0.025, 0.975, 0.95, 0.975)}},
		{"DecoratedPot", DecoratedPot{}, []cube.BBox{cube.Box(0.025, 0, 0.025, 0.975, 1, 0.975)}},
		{"EnchantingTable", EnchantingTable{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.75, 1)}},
		{"EndPortalFrame", EndPortalFrame{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.8125, 1)}},
		{"Leaves", Leaves{}, []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}},
		{"Lectern", Lectern{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.9, 1)}},
		{"LilyPad", LilyPad{}, []cube.BBox{cube.Box(0.0625, 0, 0.0625, 0.9375, 0.015625, 0.9375)}},
		{"Solid", Solid{}, []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}},
		{"Stonecutter", Stonecutter{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.5625, 1)}},
		{"TilledGrass", TilledGrass{}, []cube.BBox{cube.Box(0, 0, 0, 1, 0.9375, 1)}},

		// PINNED BUG: the BrewingStand post box starts at y=-0.125, so the model
		// sticks a collision box a quarter of the way down into the block below.
		// ExtendTowards(FaceDown, 0.125) grows downwards; every other model in
		// this package shrinks with a negative argument.
		{"BrewingStand", BrewingStand{}, []cube.BBox{
			cube.Box(0, 0, 0, 1, 0.125, 1),
			cube.Box(0.4375, -0.125, 0.4375, 0.5625, 1, 0.5625),
		}},

		// Hopper appends its walls in cube.HorizontalFaces() order: north, east,
		// south, west.
		{"Hopper", Hopper{}, []cube.BBox{
			cube.Box(0, 0, 0, 1, 0.625, 1),
			cube.Box(0, 0, 0.875, 1, 1, 1),
			cube.Box(0, 0, 0, 0.125, 1, 1),
			cube.Box(0, 0, 0, 1, 1, 0.125),
			cube.Box(0.875, 0, 0, 1, 1, 1),
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertBoxes(t, c.name, c.model.BBox(origin, emptySource), c.want)
		})
	}
}

// ---------------------------------------------------------------------------
// FaceSolid, every model, all six faces
// ---------------------------------------------------------------------------

func TestFaceSolidAllModels(t *testing.T) {
	for _, c := range []struct {
		name  string
		model world.BlockModel
		solid []cube.Face
	}{
		{"Anvil", Anvil{Facing: cube.North}, nil},
		{"Bamboo", Bamboo{}, nil},
		{"Bamboo thick", Bamboo{Thick: true}, nil},
		{"Bed", Bed{}, nil},
		{"BrewingStand", BrewingStand{}, nil},
		{"Cactus", Cactus{}, nil},
		{"Cake", Cake{Bites: 3}, nil},
		{"Campfire", Campfire{}, []cube.Face{cube.FaceDown}},
		{"Candle", Candle{Count: 2}, nil},
		{"Carpet", Carpet{}, nil},
		{"Chain", Chain{Axis: cube.Y}, nil},
		{"Chest", Chest{}, nil},
		{"CocoaBean", CocoaBean{Facing: cube.North, Age: 2}, nil},
		{"Composter", Composter{Level: 4}, []cube.Face{
			cube.FaceDown, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast,
		}},
		{"DecoratedPot", DecoratedPot{}, []cube.Face{cube.FaceDown, cube.FaceUp}},
		{"Door", Door{Facing: cube.North}, nil},
		{"Door open", Door{Facing: cube.North, Open: true, Right: true}, nil},
		{"Empty", Empty{}, nil},
		{"EnchantingTable", EnchantingTable{}, nil},
		{"EndPortalFrame", EndPortalFrame{}, []cube.Face{cube.FaceDown}},
		{"EndRod", EndRod{Axis: cube.Y}, nil},
		{"Fence", Fence{}, []cube.Face{cube.FaceDown, cube.FaceUp}},
		{"Fence wood", Fence{Wood: true}, []cube.Face{cube.FaceDown, cube.FaceUp}},
		{"FenceGate", FenceGate{Facing: cube.North}, nil},
		{"FenceGate open", FenceGate{Facing: cube.North, Open: true}, nil},
		{"Grindstone", Grindstone{Axis: cube.Y}, nil},
		{"Hopper", Hopper{}, []cube.Face{cube.FaceUp}},
		{"Ladder", Ladder{Facing: cube.North}, nil},
		{"Lantern", Lantern{}, nil},
		{"Lantern hanging", Lantern{Hanging: true}, nil},
		{"Leaves", Leaves{}, nil},
		{"Lectern", Lectern{}, nil},
		{"LilyPad", LilyPad{}, nil},
		{"Portal", Portal{Axis: cube.X}, nil},
		{"Shulker", Shulker{Facing: cube.FaceUp, Progress: 10}, nil},
		{"Skull", Skull{Direction: cube.FaceNorth}, nil},
		{"Skull hanging", Skull{Direction: cube.FaceNorth, Hanging: true}, nil},
		{"Solid", Solid{}, allFaces()},
		{"Stonecutter", Stonecutter{}, nil},
		{"Thin", Thin{}, []cube.Face{cube.FaceDown}},
		{"TilledGrass", TilledGrass{}, allFaces()},
		{"Wall", Wall{}, []cube.Face{cube.FaceDown, cube.FaceUp}},
		{"Wall post", Wall{Post: true, NorthConnection: 1}, []cube.Face{cube.FaceDown, cube.FaceUp}},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertSolidFaces(t, c.name, c.model, origin, emptySource, c.solid...)
		})
	}
}

// ---------------------------------------------------------------------------
// Anvil
// ---------------------------------------------------------------------------

func TestAnvilBBox(t *testing.T) {
	// The anvil is thinned along the axis perpendicular to its facing, so north
	// and south share a shape and west and east share the other.
	for _, c := range []struct {
		facing cube.Direction
		want   cube.BBox
	}{
		{cube.North, cube.Box(0.125, 0, 0, 0.875, 1, 1)},
		{cube.South, cube.Box(0.125, 0, 0, 0.875, 1, 1)},
		{cube.West, cube.Box(0, 0, 0.125, 1, 1, 0.875)},
		{cube.East, cube.Box(0, 0, 0.125, 1, 1, 0.875)},
	} {
		t.Run(c.facing.String(), func(t *testing.T) {
			got := Anvil{Facing: c.facing}.BBox(origin, emptySource)
			assertBoxes(t, "Anvil "+c.facing.String(), got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Slab
// ---------------------------------------------------------------------------

func TestSlabBBox(t *testing.T) {
	for _, c := range []struct {
		double, top bool
		want        cube.BBox
	}{
		{false, false, cube.Box(0, 0, 0, 1, 0.5, 1)},
		{false, true, cube.Box(0, 0.5, 0, 1, 1, 1)},
		{true, false, cube.Box(0, 0, 0, 1, 1, 1)},
		{true, true, cube.Box(0, 0, 0, 1, 1, 1)},
	} {
		name := "double=" + boolName(c.double) + " top=" + boolName(c.top)
		t.Run(name, func(t *testing.T) {
			got := Slab{Double: c.double, Top: c.top}.BBox(origin, emptySource)
			assertBoxes(t, "Slab "+name, got, []cube.BBox{c.want})
		})
	}
}

func TestSlabFaceSolid(t *testing.T) {
	for _, c := range []struct {
		double, top bool
		solid       []cube.Face
	}{
		{false, false, []cube.Face{cube.FaceDown}},
		{false, true, []cube.Face{cube.FaceUp}},
		{true, false, allFaces()},
		{true, true, allFaces()},
	} {
		name := "double=" + boolName(c.double) + " top=" + boolName(c.top)
		t.Run(name, func(t *testing.T) {
			assertSolidFaces(t, "Slab "+name, Slab{Double: c.double, Top: c.top}, origin, emptySource, c.solid...)
		})
	}
}

// ---------------------------------------------------------------------------
// Stair
// ---------------------------------------------------------------------------

// stairCornerName labels the unexported corner constants for subtest names.
func stairCornerName(corner uint8) string {
	switch corner {
	case cornerRightInner:
		return "cornerRightInner"
	case cornerLeftInner:
		return "cornerLeftInner"
	case cornerRightOuter:
		return "cornerRightOuter"
	case cornerLeftOuter:
		return "cornerLeftOuter"
	}
	return "noCorner"
}

// stairSource places the single neighbouring stair that drives s at pos into the
// corner type named. cornerType looks at the block on the facing ("closed") side
// first, then the block on the opposite ("open") side.
func stairSource(pos cube.Pos, s Stair, corner uint8) fakeSource {
	f := fakeSource{}
	switch corner {
	case cornerLeftOuter:
		f[pos.Side(s.Facing.Face())] = fakeBlock{model: Stair{Facing: s.Facing.RotateRight(), UpsideDown: s.UpsideDown}}
	case cornerRightOuter:
		f[pos.Side(s.Facing.Face())] = fakeBlock{model: Stair{Facing: s.Facing.RotateLeft(), UpsideDown: s.UpsideDown}}
	case cornerRightInner:
		f[pos.Side(s.Facing.Opposite().Face())] = fakeBlock{model: Stair{Facing: s.Facing.RotateRight(), UpsideDown: s.UpsideDown}}
	case cornerLeftInner:
		f[pos.Side(s.Facing.Opposite().Face())] = fakeBlock{model: Stair{Facing: s.Facing.RotateLeft(), UpsideDown: s.UpsideDown}}
	}
	return f
}

// stairExtras holds the golden horizontal bounds {x0, z0, x1, z1} of every box
// Stair.BBox appends after its base box, in append order.
func stairExtras(corner uint8, facing cube.Direction) [][4]float64 {
	step := map[cube.Direction][4]float64{
		cube.North: {0, 0, 1, 0.5},
		cube.South: {0, 0.5, 1, 1},
		cube.West:  {0, 0, 0.5, 1},
		cube.East:  {0.5, 0, 1, 1},
	}
	rightOuter := map[cube.Direction][4]float64{
		cube.North: {0, 0, 0.5, 0.5},
		cube.South: {0.5, 0.5, 1, 1},
		cube.West:  {0, 0.5, 0.5, 1},
		cube.East:  {0.5, 0, 1, 0.5},
	}
	leftOuter := map[cube.Direction][4]float64{
		cube.North: {0.5, 0, 1, 0.5},
		cube.South: {0, 0.5, 0.5, 1},
		cube.West:  {0, 0, 0.5, 0.5},
		cube.East:  {0.5, 0.5, 1, 1},
	}
	rightInner := map[cube.Direction][4]float64{
		cube.North: {0.5, 0.5, 1, 1},
		cube.South: {0, 0, 0.5, 0.5},
		cube.West:  {0.5, 0, 1, 0.5},
		cube.East:  {0, 0.5, 0.5, 1},
	}
	leftInner := map[cube.Direction][4]float64{
		cube.North: {0, 0.5, 0.5, 1},
		cube.South: {0.5, 0, 1, 0.5},
		cube.West:  {0.5, 0.5, 1, 1},
		cube.East:  {0, 0, 0.5, 0.5},
	}
	switch corner {
	case cornerRightOuter:
		return [][4]float64{rightOuter[facing]}
	case cornerLeftOuter:
		return [][4]float64{leftOuter[facing]}
	case cornerRightInner:
		return [][4]float64{step[facing], rightInner[facing]}
	case cornerLeftInner:
		return [][4]float64{step[facing], leftInner[facing]}
	}
	return [][4]float64{step[facing]}
}

// stairWant assembles the golden box set: the base half-slab plus every step
// box. Right side up the steps occupy y 0.5-1; upside down they occupy y 0-0.5.
func stairWant(facing cube.Direction, upsideDown bool, corner uint8) []cube.BBox {
	want := []cube.BBox{cube.Box(0, 0, 0, 1, 0.5, 1)}
	y0, y1 := 0.5, 1.0
	if upsideDown {
		want[0] = cube.Box(0, 0.5, 0, 1, 1, 1)
		y0, y1 = 0, 0.5
	}
	for _, e := range stairExtras(corner, facing) {
		want = append(want, cube.Box(e[0], y0, e[1], e[2], y1, e[3]))
	}
	return want
}

func TestStairCornerType(t *testing.T) {
	for _, corner := range []uint8{noCorner, cornerRightInner, cornerLeftInner, cornerRightOuter, cornerLeftOuter} {
		for _, facing := range allDirections() {
			for _, upsideDown := range []bool{false, true} {
				s := Stair{Facing: facing, UpsideDown: upsideDown}
				name := stairCornerName(corner) + "/" + facing.String() + "/upsideDown=" + boolName(upsideDown)
				t.Run(name, func(t *testing.T) {
					if got := s.cornerType(origin, stairSource(origin, s, corner)); got != corner {
						t.Fatalf("cornerType = %s, want %s", stairCornerName(got), stairCornerName(corner))
					}
				})
			}
		}
	}
}

func TestStairBBox(t *testing.T) {
	for _, corner := range []uint8{noCorner, cornerRightInner, cornerLeftInner, cornerRightOuter, cornerLeftOuter} {
		for _, facing := range allDirections() {
			for _, upsideDown := range []bool{false, true} {
				s := Stair{Facing: facing, UpsideDown: upsideDown}
				name := stairCornerName(corner) + "/" + facing.String() + "/upsideDown=" + boolName(upsideDown)
				t.Run(name, func(t *testing.T) {
					got := s.BBox(origin, stairSource(origin, s, corner))
					assertBoxes(t, "Stair "+name, got, stairWant(facing, upsideDown, corner))
				})
			}
		}
	}
}

func TestStairFaceSolid(t *testing.T) {
	// The face at the top (upside down) or bottom is always solid. Beyond that,
	// outer corners are solid on no horizontal face at all, noCorner is solid
	// only behind the stair, and inner corners add the rotated side.
	for _, corner := range []uint8{noCorner, cornerRightInner, cornerLeftInner, cornerRightOuter, cornerLeftOuter} {
		for _, facing := range allDirections() {
			for _, upsideDown := range []bool{false, true} {
				s := Stair{Facing: facing, UpsideDown: upsideDown}
				name := stairCornerName(corner) + "/" + facing.String() + "/upsideDown=" + boolName(upsideDown)
				t.Run(name, func(t *testing.T) {
					solid := []cube.Face{cube.FaceDown}
					if upsideDown {
						solid = []cube.Face{cube.FaceUp}
					}
					switch corner {
					case noCorner:
						solid = append(solid, facing.Face())
					case cornerRightInner:
						solid = append(solid, facing.Face(), facing.RotateRight().Face())
					case cornerLeftInner:
						solid = append(solid, facing.Face(), facing.RotateLeft().Face())
					}
					assertSolidFaces(t, "Stair "+name, s, origin, stairSource(origin, s, corner), solid...)
				})
			}
		}
	}
}

func TestStairCornerSuppression(t *testing.T) {
	s := Stair{Facing: cube.North}

	t.Run("outer corner suppressed by stair on the right", func(t *testing.T) {
		// A stair on the closed side facing the opposite of the rotated facing
		// normally yields cornerRightOuter, but a matching stair to the right of
		// this one cancels it back to noCorner.
		src := stairSource(origin, s, cornerRightOuter)
		src[origin.Side(s.Facing.RotateRight().Face())] = fakeBlock{model: Stair{Facing: s.Facing}}
		if got := s.cornerType(origin, src); got != noCorner {
			t.Fatalf("cornerType = %s, want noCorner", stairCornerName(got))
		}
		assertBoxes(t, "Stair suppressed right outer", s.BBox(origin, src), stairWant(cube.North, false, noCorner))
	})

	t.Run("inner corner suppressed by stair on the right", func(t *testing.T) {
		// The same right-hand stair cancels cornerRightInner. The branch has no
		// else, so the function falls through to its final noCorner return.
		src := stairSource(origin, s, cornerRightInner)
		src[origin.Side(s.Facing.RotateRight().Face())] = fakeBlock{model: Stair{Facing: s.Facing}}
		if got := s.cornerType(origin, src); got != noCorner {
			t.Fatalf("cornerType = %s, want noCorner", stairCornerName(got))
		}
		assertBoxes(t, "Stair suppressed right inner", s.BBox(origin, src), stairWant(cube.North, false, noCorner))
	})

	t.Run("right hand stair with mismatched upside down does not suppress", func(t *testing.T) {
		src := stairSource(origin, s, cornerRightOuter)
		src[origin.Side(s.Facing.RotateRight().Face())] = fakeBlock{model: Stair{Facing: s.Facing, UpsideDown: true}}
		if got := s.cornerType(origin, src); got != cornerRightOuter {
			t.Fatalf("cornerType = %s, want cornerRightOuter", stairCornerName(got))
		}
	})

	t.Run("neighbouring stair with mismatched upside down forms no corner", func(t *testing.T) {
		src := fakeSource{
			origin.Side(s.Facing.Face()): fakeBlock{model: Stair{Facing: s.Facing.RotateRight(), UpsideDown: true}},
		}
		if got := s.cornerType(origin, src); got != noCorner {
			t.Fatalf("cornerType = %s, want noCorner", stairCornerName(got))
		}
	})

	t.Run("neighbouring stair with the same facing forms no corner", func(t *testing.T) {
		src := fakeSource{
			origin.Side(s.Facing.Face()):            fakeBlock{model: Stair{Facing: s.Facing}},
			origin.Side(s.Facing.Opposite().Face()): fakeBlock{model: Stair{Facing: s.Facing}},
		}
		if got := s.cornerType(origin, src); got != noCorner {
			t.Fatalf("cornerType = %s, want noCorner", stairCornerName(got))
		}
	})

	t.Run("non stair neighbours form no corner", func(t *testing.T) {
		src := neighbours(origin, map[cube.Face]world.BlockModel{
			cube.FaceNorth: Solid{}, cube.FaceSouth: Solid{},
			cube.FaceWest: Solid{}, cube.FaceEast: Solid{},
		})
		if got := s.cornerType(origin, src); got != noCorner {
			t.Fatalf("cornerType = %s, want noCorner", stairCornerName(got))
		}
	})
}

// ---------------------------------------------------------------------------
// Door
// ---------------------------------------------------------------------------

func TestDoorBBox(t *testing.T) {
	// A closed door ignores Right entirely, so all four closed pairs coincide.
	for _, c := range []struct {
		facing      cube.Direction
		open, right bool
		want        cube.BBox
	}{
		{cube.North, false, false, cube.Box(0, 0, 0.8125, 1, 1, 1)},
		{cube.North, false, true, cube.Box(0, 0, 0.8125, 1, 1, 1)},
		{cube.South, false, false, cube.Box(0, 0, 0, 1, 1, 0.1875)},
		{cube.South, false, true, cube.Box(0, 0, 0, 1, 1, 0.1875)},
		{cube.West, false, false, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.West, false, true, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.East, false, false, cube.Box(0, 0, 0, 0.1875, 1, 1)},
		{cube.East, false, true, cube.Box(0, 0, 0, 0.1875, 1, 1)},

		// Open and right-hinged: the door swings to the left of its facing.
		{cube.North, true, true, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.South, true, true, cube.Box(0, 0, 0, 0.1875, 1, 1)},
		{cube.West, true, true, cube.Box(0, 0, 0, 1, 1, 0.1875)},
		{cube.East, true, true, cube.Box(0, 0, 0.8125, 1, 1, 1)},

		// Open and left-hinged: the door swings to the right of its facing.
		{cube.North, true, false, cube.Box(0, 0, 0, 0.1875, 1, 1)},
		{cube.South, true, false, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.West, true, false, cube.Box(0, 0, 0.8125, 1, 1, 1)},
		{cube.East, true, false, cube.Box(0, 0, 0, 1, 1, 0.1875)},
	} {
		name := c.facing.String() + "/open=" + boolName(c.open) + "/right=" + boolName(c.right)
		t.Run(name, func(t *testing.T) {
			got := Door{Facing: c.facing, Open: c.open, Right: c.right}.BBox(origin, emptySource)
			assertBoxes(t, "Door "+name, got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Trapdoor
// ---------------------------------------------------------------------------

func TestTrapdoorBBox(t *testing.T) {
	// When open, Top is ignored: the slab stands against the side named by
	// Facing regardless of which half of the block the trapdoor sits in.
	for _, c := range []struct {
		facing    cube.Direction
		open, top bool
		want      cube.BBox
	}{
		{cube.North, false, false, cube.Box(0, 0, 0, 1, 0.1875, 1)},
		{cube.South, false, false, cube.Box(0, 0, 0, 1, 0.1875, 1)},
		{cube.West, false, false, cube.Box(0, 0, 0, 1, 0.1875, 1)},
		{cube.East, false, false, cube.Box(0, 0, 0, 1, 0.1875, 1)},

		{cube.North, false, true, cube.Box(0, 0.8125, 0, 1, 1, 1)},
		{cube.South, false, true, cube.Box(0, 0.8125, 0, 1, 1, 1)},
		{cube.West, false, true, cube.Box(0, 0.8125, 0, 1, 1, 1)},
		{cube.East, false, true, cube.Box(0, 0.8125, 0, 1, 1, 1)},

		{cube.North, true, false, cube.Box(0, 0, 0.8125, 1, 1, 1)},
		{cube.South, true, false, cube.Box(0, 0, 0, 1, 1, 0.1875)},
		{cube.West, true, false, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.East, true, false, cube.Box(0, 0, 0, 0.1875, 1, 1)},

		{cube.North, true, true, cube.Box(0, 0, 0.8125, 1, 1, 1)},
		{cube.South, true, true, cube.Box(0, 0, 0, 1, 1, 0.1875)},
		{cube.West, true, true, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.East, true, true, cube.Box(0, 0, 0, 0.1875, 1, 1)},
	} {
		name := c.facing.String() + "/open=" + boolName(c.open) + "/top=" + boolName(c.top)
		t.Run(name, func(t *testing.T) {
			got := Trapdoor{Facing: c.facing, Open: c.open, Top: c.top}.BBox(origin, emptySource)
			assertBoxes(t, "Trapdoor "+name, got, []cube.BBox{c.want})
		})
	}
}

func TestTrapdoorFaceSolid(t *testing.T) {
	for _, c := range []struct {
		facing    cube.Direction
		open, top bool
		solid     cube.Face
	}{
		{cube.North, false, false, cube.FaceDown},
		{cube.South, false, false, cube.FaceDown},
		{cube.West, false, false, cube.FaceDown},
		{cube.East, false, false, cube.FaceDown},

		{cube.North, false, true, cube.FaceUp},
		{cube.South, false, true, cube.FaceUp},
		{cube.West, false, true, cube.FaceUp},
		{cube.East, false, true, cube.FaceUp},

		// Open: solid on the face opposite the facing, which is the side the
		// box in TestTrapdoorBBox rests against.
		{cube.North, true, false, cube.FaceSouth},
		{cube.South, true, false, cube.FaceNorth},
		{cube.West, true, false, cube.FaceEast},
		{cube.East, true, false, cube.FaceWest},

		{cube.North, true, true, cube.FaceSouth},
		{cube.South, true, true, cube.FaceNorth},
		{cube.West, true, true, cube.FaceEast},
		{cube.East, true, true, cube.FaceWest},
	} {
		name := c.facing.String() + "/open=" + boolName(c.open) + "/top=" + boolName(c.top)
		t.Run(name, func(t *testing.T) {
			m := Trapdoor{Facing: c.facing, Open: c.open, Top: c.top}
			assertSolidFaces(t, "Trapdoor "+name, m, origin, emptySource, c.solid)
		})
	}
}

// ---------------------------------------------------------------------------
// FenceGate
// ---------------------------------------------------------------------------

func TestFenceGateBBox(t *testing.T) {
	for _, c := range []struct {
		facing cube.Direction
		open   bool
		want   []cube.BBox
	}{
		{cube.North, false, []cube.BBox{cube.Box(0, 0, 0.375, 1, 1.5, 0.625)}},
		{cube.South, false, []cube.BBox{cube.Box(0, 0, 0.375, 1, 1.5, 0.625)}},
		{cube.West, false, []cube.BBox{cube.Box(0.375, 0, 0, 0.625, 1.5, 1)}},
		{cube.East, false, []cube.BBox{cube.Box(0.375, 0, 0, 0.625, 1.5, 1)}},
		{cube.North, true, nil},
		{cube.South, true, nil},
		{cube.West, true, nil},
		{cube.East, true, nil},
	} {
		name := c.facing.String() + "/open=" + boolName(c.open)
		t.Run(name, func(t *testing.T) {
			got := FenceGate{Facing: c.facing, Open: c.open}.BBox(origin, emptySource)
			assertBoxes(t, "FenceGate "+name, got, c.want)
		})
	}
}

// ---------------------------------------------------------------------------
// Fence
// ---------------------------------------------------------------------------

// Golden Fence boxes. Fence appends in the order west, east, north, south, and
// falls back to a centre post only when nothing connects. fenceHeight is 1.5, so
// every box reaches half a block above the block it belongs to.
var (
	fenceWestBox  = cube.Box(0, 0, 0.375, 0.625, 1.5, 0.625)
	fenceEastBox  = cube.Box(0.375, 0, 0.375, 1, 1.5, 0.625)
	fenceNorthBox = cube.Box(0.375, 0, 0, 0.625, 1.5, 0.625)
	fenceSouthBox = cube.Box(0.375, 0, 0.375, 0.625, 1.5, 1)
	fencePostBox  = cube.Box(0.375, 0, 0.375, 0.625, 1.5, 0.625)
)

func TestFenceBBoxConnectionMatrix(t *testing.T) {
	// Solid neighbours connect through Fence.checkConnection's FaceSolid branch.
	for _, c := range []struct {
		name    string
		connect []cube.Face
		want    []cube.BBox
	}{
		{"none", nil, []cube.BBox{fencePostBox}},

		{"west", []cube.Face{cube.FaceWest}, []cube.BBox{fenceWestBox}},
		{"east", []cube.Face{cube.FaceEast}, []cube.BBox{fenceEastBox}},
		{"north", []cube.Face{cube.FaceNorth}, []cube.BBox{fenceNorthBox}},
		{"south", []cube.Face{cube.FaceSouth}, []cube.BBox{fenceSouthBox}},

		{"west east", []cube.Face{cube.FaceWest, cube.FaceEast}, []cube.BBox{fenceWestBox, fenceEastBox}},
		{"north south", []cube.Face{cube.FaceNorth, cube.FaceSouth}, []cube.BBox{fenceNorthBox, fenceSouthBox}},
		{"west north", []cube.Face{cube.FaceWest, cube.FaceNorth}, []cube.BBox{fenceWestBox, fenceNorthBox}},
		{"west south", []cube.Face{cube.FaceWest, cube.FaceSouth}, []cube.BBox{fenceWestBox, fenceSouthBox}},
		{"east north", []cube.Face{cube.FaceEast, cube.FaceNorth}, []cube.BBox{fenceEastBox, fenceNorthBox}},
		{"east south", []cube.Face{cube.FaceEast, cube.FaceSouth}, []cube.BBox{fenceEastBox, fenceSouthBox}},

		{"west east north", []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth},
			[]cube.BBox{fenceWestBox, fenceEastBox, fenceNorthBox}},
		{"west east south", []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceSouth},
			[]cube.BBox{fenceWestBox, fenceEastBox, fenceSouthBox}},
		{"west north south", []cube.Face{cube.FaceWest, cube.FaceNorth, cube.FaceSouth},
			[]cube.BBox{fenceWestBox, fenceNorthBox, fenceSouthBox}},
		{"east north south", []cube.Face{cube.FaceEast, cube.FaceNorth, cube.FaceSouth},
			[]cube.BBox{fenceEastBox, fenceNorthBox, fenceSouthBox}},

		{"all four", []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth},
			[]cube.BBox{fenceWestBox, fenceEastBox, fenceNorthBox, fenceSouthBox}},
	} {
		t.Run(c.name, func(t *testing.T) {
			models := map[cube.Face]world.BlockModel{}
			for _, f := range c.connect {
				models[f] = Solid{}
			}
			got := Fence{}.BBox(origin, neighbours(origin, models))
			assertBoxes(t, "Fence "+c.name, got, c.want)
		})
	}
}

func TestFenceConnectionRules(t *testing.T) {
	for _, c := range []struct {
		name      string
		fence     Fence
		neighbour world.BlockModel
		want      []cube.BBox
	}{
		{"matching wood fence connects", Fence{Wood: true}, Fence{Wood: true}, []cube.BBox{fenceWestBox}},
		{"matching stone fence connects", Fence{}, Fence{}, []cube.BBox{fenceWestBox}},
		// A fence of the other material fails the Wood check, and Fence.FaceSolid
		// is false on horizontal faces, so nothing connects.
		{"mismatched wood fence does not connect", Fence{Wood: true}, Fence{}, []cube.BBox{fencePostBox}},
		{"fence gate connects", Fence{Wood: true}, FenceGate{Facing: cube.North}, []cube.BBox{fenceWestBox}},
		{"open fence gate connects", Fence{Wood: true}, FenceGate{Facing: cube.North, Open: true}, []cube.BBox{fenceWestBox}},
		{"solid block connects", Fence{}, Solid{}, []cube.BBox{fenceWestBox}},
		{"empty does not connect", Fence{}, Empty{}, []cube.BBox{fencePostBox}},
		{"leaves do not connect", Fence{}, Leaves{}, []cube.BBox{fencePostBox}},
		// Slab is solid on FaceDown only; the neighbour is asked about its east
		// face, so a bottom slab to the west does not connect.
		{"bottom slab does not connect", Fence{}, Slab{}, []cube.BBox{fencePostBox}},
		{"double slab connects", Fence{}, Slab{Double: true}, []cube.BBox{fenceWestBox}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := neighbours(origin, map[cube.Face]world.BlockModel{cube.FaceWest: c.neighbour})
			assertBoxes(t, "Fence "+c.name, c.fence.BBox(origin, s), c.want)
		})
	}
}

// ---------------------------------------------------------------------------
// Thin
// ---------------------------------------------------------------------------

// Golden Thin boxes. thinInset is 7/16 and thinHeight is 1. The west/east box is
// appended before the north/south box.
var (
	thinWestEastBox   = cube.Box(0, 0, 0.4375, 1, 1, 0.5625)
	thinEastOnlyBox   = cube.Box(0.4375, 0, 0.4375, 1, 1, 0.5625)
	thinWestOnlyBox   = cube.Box(0, 0, 0.4375, 0.5625, 1, 0.5625)
	thinNorthSouthBox = cube.Box(0.4375, 0, 0, 0.5625, 1, 1)
	thinSouthOnlyBox  = cube.Box(0.4375, 0, 0.4375, 0.5625, 1, 1)
	thinNorthOnlyBox  = cube.Box(0.4375, 0, 0, 0.5625, 1, 0.5625)
	thinPostBox       = cube.Box(0.4375, 0, 0.4375, 0.5625, 1, 0.5625)
)

func TestThinBBoxConnectionMatrix(t *testing.T) {
	// Thin picks its west/east box with `if !connectWest { ... } else if
	// !connectEast { ... }`. The chain is only entered when at least one of the
	// two connects, so the "neither" arm is unreachable and all three reachable
	// states produce the expected half or full bar. The north/south chain is the
	// same shape. Every combination below pins that.
	for _, c := range []struct {
		name    string
		connect []cube.Face
		want    []cube.BBox
	}{
		{"none", nil, []cube.BBox{thinPostBox}},

		{"west", []cube.Face{cube.FaceWest}, []cube.BBox{thinWestOnlyBox}},
		{"east", []cube.Face{cube.FaceEast}, []cube.BBox{thinEastOnlyBox}},
		{"north", []cube.Face{cube.FaceNorth}, []cube.BBox{thinNorthOnlyBox}},
		{"south", []cube.Face{cube.FaceSouth}, []cube.BBox{thinSouthOnlyBox}},

		{"west east", []cube.Face{cube.FaceWest, cube.FaceEast}, []cube.BBox{thinWestEastBox}},
		{"north south", []cube.Face{cube.FaceNorth, cube.FaceSouth}, []cube.BBox{thinNorthSouthBox}},
		{"west north", []cube.Face{cube.FaceWest, cube.FaceNorth}, []cube.BBox{thinWestOnlyBox, thinNorthOnlyBox}},
		{"west south", []cube.Face{cube.FaceWest, cube.FaceSouth}, []cube.BBox{thinWestOnlyBox, thinSouthOnlyBox}},
		{"east north", []cube.Face{cube.FaceEast, cube.FaceNorth}, []cube.BBox{thinEastOnlyBox, thinNorthOnlyBox}},
		{"east south", []cube.Face{cube.FaceEast, cube.FaceSouth}, []cube.BBox{thinEastOnlyBox, thinSouthOnlyBox}},

		{"west east north", []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth},
			[]cube.BBox{thinWestEastBox, thinNorthOnlyBox}},
		{"west east south", []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceSouth},
			[]cube.BBox{thinWestEastBox, thinSouthOnlyBox}},
		{"west north south", []cube.Face{cube.FaceWest, cube.FaceNorth, cube.FaceSouth},
			[]cube.BBox{thinWestOnlyBox, thinNorthSouthBox}},
		{"east north south", []cube.Face{cube.FaceEast, cube.FaceNorth, cube.FaceSouth},
			[]cube.BBox{thinEastOnlyBox, thinNorthSouthBox}},

		{"all four", []cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth},
			[]cube.BBox{thinWestEastBox, thinNorthSouthBox}},
	} {
		t.Run(c.name, func(t *testing.T) {
			models := map[cube.Face]world.BlockModel{}
			for _, f := range c.connect {
				models[f] = Solid{}
			}
			got := Thin{}.BBox(origin, neighbours(origin, models))
			assertBoxes(t, "Thin "+c.name, got, c.want)
		})
	}
}

func TestThinConnectionRules(t *testing.T) {
	for _, c := range []struct {
		name      string
		neighbour world.BlockModel
		want      []cube.BBox
	}{
		{"thin connects", Thin{}, []cube.BBox{thinWestOnlyBox}},
		// Wall connects by type assertion, whatever its connection heights are.
		{"wall connects", Wall{}, []cube.BBox{thinWestOnlyBox}},
		{"wall with connections connects", Wall{Post: true, EastConnection: 0.875}, []cube.BBox{thinWestOnlyBox}},
		{"solid connects", Solid{}, []cube.BBox{thinWestOnlyBox}},
		{"tilled grass connects", TilledGrass{}, []cube.BBox{thinWestOnlyBox}},
		{"empty does not connect", Empty{}, []cube.BBox{thinPostBox}},
		{"leaves do not connect", Leaves{}, []cube.BBox{thinPostBox}},
		{"fence does not connect", Fence{}, []cube.BBox{thinPostBox}},
		{"bottom slab does not connect", Slab{}, []cube.BBox{thinPostBox}},
		{"double slab connects", Slab{Double: true}, []cube.BBox{thinWestOnlyBox}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := neighbours(origin, map[cube.Face]world.BlockModel{cube.FaceWest: c.neighbour})
			assertBoxes(t, "Thin "+c.name, Thin{}.BBox(origin, s), c.want)
		})
	}
}

// ---------------------------------------------------------------------------
// Wall
// ---------------------------------------------------------------------------

func TestWallBBox(t *testing.T) {
	// Wall always emits its post box first, then appends north, east, south and
	// west in that order, skipping any connection whose height is not positive.
	for _, c := range []struct {
		name string
		wall Wall
		want []cube.BBox
	}{
		{"no connections", Wall{}, []cube.BBox{cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75)}},
		{"post no connections", Wall{Post: true}, []cube.BBox{cube.Box(0.25, 0, 0.25, 0.75, 1, 0.75)}},

		{"north", Wall{NorthConnection: 0.75}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0.25, 0, 0, 0.75, 0.75, 0.25),
		}},
		{"east", Wall{EastConnection: 0.8125}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0.75, 0, 0.25, 1, 0.8125, 0.75),
		}},
		{"south", Wall{SouthConnection: 0.875}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0.25, 0, 0.75, 0.75, 0.875, 1),
		}},
		{"west", Wall{WestConnection: 1}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0, 0, 0.25, 0.25, 1, 0.75),
		}},

		{"all four distinct heights", Wall{
			NorthConnection: 0.75, EastConnection: 0.8125, SouthConnection: 0.875, WestConnection: 1,
		}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0.25, 0, 0, 0.75, 0.75, 0.25),
			cube.Box(0.75, 0, 0.25, 1, 0.8125, 0.75),
			cube.Box(0.25, 0, 0.75, 0.75, 0.875, 1),
			cube.Box(0, 0, 0.25, 0.25, 1, 0.75),
		}},
		{"post and all four", Wall{
			Post: true, NorthConnection: 1, EastConnection: 1, SouthConnection: 1, WestConnection: 1,
		}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 1, 0.75),
			cube.Box(0.25, 0, 0, 0.75, 1, 0.25),
			cube.Box(0.75, 0, 0.25, 1, 1, 0.75),
			cube.Box(0.25, 0, 0.75, 0.75, 1, 1),
			cube.Box(0, 0, 0.25, 0.25, 1, 0.75),
		}},

		{"north and south only", Wall{NorthConnection: 0.75, SouthConnection: 0.75}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0.25, 0, 0, 0.75, 0.75, 0.25),
			cube.Box(0.25, 0, 0.75, 0.75, 0.75, 1),
		}},
		{"east and west only", Wall{EastConnection: 0.75, WestConnection: 0.75}, []cube.BBox{
			cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75),
			cube.Box(0.75, 0, 0.25, 1, 0.75, 0.75),
			cube.Box(0, 0, 0.25, 0.25, 0.75, 0.75),
		}},

		// Only a strictly positive height produces a box.
		{"zero height connections are skipped", Wall{
			NorthConnection: 0, EastConnection: 0, SouthConnection: 0, WestConnection: 0,
		}, []cube.BBox{cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75)}},
		{"negative height connections are skipped", Wall{
			NorthConnection: -1, EastConnection: -0.5, SouthConnection: -0.25, WestConnection: -2,
		}, []cube.BBox{cube.Box(0.25, 0, 0.25, 0.75, 0.8125, 0.75)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertBoxes(t, "Wall "+c.name, c.wall.BBox(origin, emptySource), c.want)
		})
	}
}

// ---------------------------------------------------------------------------
// Cake
// ---------------------------------------------------------------------------

func TestCakeBBox(t *testing.T) {
	// Each bite pushes the west edge one eighth of a block east.
	for _, c := range []struct {
		bites int
		minX  float64
	}{
		{0, 0.0625},
		{1, 0.1875},
		{2, 0.3125},
		{3, 0.4375},
		{4, 0.5625},
		{5, 0.6875},
		{6, 0.8125},
		// PINNED BUG: at seven bites the west edge reaches the east edge, so the
		// box is zero-width and collides with nothing.
		{7, 0.9375},
	} {
		t.Run(fmt.Sprintf("bites=%d", c.bites), func(t *testing.T) {
			got := Cake{Bites: c.bites}.BBox(origin, emptySource)
			want := []cube.BBox{cube.Box(c.minX, 0, 0.0625, 0.9375, 0.5, 0.9375)}
			assertBoxes(t, fmt.Sprintf("Cake bites=%d", c.bites), got, want)
		})
	}
}

// ---------------------------------------------------------------------------
// Candle
// ---------------------------------------------------------------------------

func TestCandleBBox(t *testing.T) {
	// Counts outside 2..4, including the impossible 0 and 5, fall to the single
	// candle box.
	single := cube.Box(0.4375, 0, 0.4375, 0.5625, 0.375, 0.5625)
	for _, c := range []struct {
		count int
		want  cube.BBox
	}{
		{0, single},
		{1, single},
		{2, cube.Box(0.3125, 0, 0.4375, 0.6875, 0.375, 0.625)},
		{3, cube.Box(0.3125, 0, 0.375, 0.625, 0.375, 0.6875)},
		{4, cube.Box(0.3125, 0, 0.3125, 0.6875, 0.375, 0.625)},
		{5, single},
		{-1, single},
	} {
		t.Run(fmt.Sprintf("count=%d", c.count), func(t *testing.T) {
			got := Candle{Count: c.count}.BBox(origin, emptySource)
			assertBoxes(t, fmt.Sprintf("Candle count=%d", c.count), got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Composter
// ---------------------------------------------------------------------------

func TestComposterBBox(t *testing.T) {
	// The four wall boxes never change; only the height of the compost box does.
	walls := []cube.BBox{
		cube.Box(0, 0, 0, 1, 1, 0.125),
		cube.Box(0, 0, 0.875, 1, 1, 1),
		cube.Box(0.875, 0, 0, 1, 1, 1),
		cube.Box(0, 0, 0, 0.125, 1, 1),
	}
	for _, c := range []struct {
		level int
		maxY  float64
	}{
		// PINNED BUG: levels 0 and 1 give the same compost height. The
		// math.Abs around level*0.125-0.0625 folds level 0's -0.0625 up to
		// +0.0625, so an empty composter has as much compost in it as one with a
		// single layer.
		{0, 0.1875},
		{1, 0.1875},
		{2, 0.3125},
		{3, 0.4375},
		{4, 0.5625},
		{5, 0.6875},
		{6, 0.8125},
		{7, 0.9375},
		// math.Min clamps at 7, so a full composter matches level 7.
		{8, 0.9375},
	} {
		t.Run(fmt.Sprintf("level=%d", c.level), func(t *testing.T) {
			got := Composter{Level: c.level}.BBox(origin, emptySource)
			want := append(append([]cube.BBox{}, walls...), cube.Box(0.125, 0, 0.125, 0.875, c.maxY, 0.875))
			assertBoxes(t, fmt.Sprintf("Composter level=%d", c.level), got, want)
		})
	}
}

// ---------------------------------------------------------------------------
// Axis-driven models
// ---------------------------------------------------------------------------

func TestChainBBox(t *testing.T) {
	for _, c := range []struct {
		axis cube.Axis
		want cube.BBox
	}{
		{cube.X, cube.Box(0, 0.40625, 0.40625, 1, 0.59375, 0.59375)},
		{cube.Y, cube.Box(0.40625, 0, 0.40625, 0.59375, 1, 0.59375)},
		{cube.Z, cube.Box(0.40625, 0.40625, 0, 0.59375, 0.59375, 1)},
	} {
		t.Run(c.axis.String(), func(t *testing.T) {
			got := Chain{Axis: c.axis}.BBox(origin, emptySource)
			assertBoxes(t, "Chain "+c.axis.String(), got, []cube.BBox{c.want})
		})
	}
}

func TestEndRodBBox(t *testing.T) {
	for _, c := range []struct {
		axis cube.Axis
		want cube.BBox
	}{
		{cube.X, cube.Box(0, 0.375, 0.375, 1, 0.625, 0.625)},
		{cube.Y, cube.Box(0.375, 0, 0.375, 0.625, 1, 0.625)},
		{cube.Z, cube.Box(0.375, 0.375, 0, 0.625, 0.625, 1)},
	} {
		t.Run(c.axis.String(), func(t *testing.T) {
			got := EndRod{Axis: c.axis}.BBox(origin, emptySource)
			assertBoxes(t, "EndRod "+c.axis.String(), got, []cube.BBox{c.want})
		})
	}
}

func TestGrindstoneBBox(t *testing.T) {
	// PINNED BUG: the base box is 0.125..0.825, not 0.125..0.875. Stretching by
	// 0.125 therefore reaches 0.95 instead of a full 1, and the box is 0.05
	// off-centre on both untouched axes. 0.825 reads as a typo for 0.875.
	for _, c := range []struct {
		axis cube.Axis
		want cube.BBox
	}{
		{cube.X, cube.Box(0, 0.125, 0.125, 0.95, 0.825, 0.825)},
		{cube.Y, cube.Box(0.125, 0, 0.125, 0.825, 0.95, 0.825)},
		{cube.Z, cube.Box(0.125, 0.125, 0, 0.825, 0.825, 0.95)},
	} {
		t.Run(c.axis.String(), func(t *testing.T) {
			got := Grindstone{Axis: c.axis}.BBox(origin, emptySource)
			assertBoxes(t, "Grindstone "+c.axis.String(), got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Ladder
// ---------------------------------------------------------------------------

func TestLadderBBox(t *testing.T) {
	for _, c := range []struct {
		facing cube.Direction
		want   cube.BBox
	}{
		{cube.North, cube.Box(0, 0, 0.8125, 1, 1, 1)},
		{cube.South, cube.Box(0, 0, 0, 1, 1, 0.1875)},
		{cube.West, cube.Box(0.8125, 0, 0, 1, 1, 1)},
		{cube.East, cube.Box(0, 0, 0, 0.1875, 1, 1)},
	} {
		t.Run(c.facing.String(), func(t *testing.T) {
			got := Ladder{Facing: c.facing}.BBox(origin, emptySource)
			assertBoxes(t, "Ladder "+c.facing.String(), got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Lantern
// ---------------------------------------------------------------------------

func TestLanternBBox(t *testing.T) {
	for _, c := range []struct {
		hanging bool
		want    cube.BBox
	}{
		{false, cube.Box(0.3125, 0, 0.3125, 0.6875, 0.5, 0.6875)},
		{true, cube.Box(0.3125, 0.125, 0.3125, 0.6875, 0.625, 0.6875)},
	} {
		t.Run("hanging="+boolName(c.hanging), func(t *testing.T) {
			got := Lantern{Hanging: c.hanging}.BBox(origin, emptySource)
			assertBoxes(t, "Lantern hanging="+boolName(c.hanging), got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// CocoaBean
// ---------------------------------------------------------------------------

func TestCocoaBeanBBox(t *testing.T) {
	// The pod grows on every axis with age: wider across the wall, taller
	// downwards from a fixed top at 0.75, and deeper away from the wall. The
	// near face stays 1/16 clear of the block it is attached to.
	for _, c := range []struct {
		facing cube.Direction
		age    int
		want   cube.BBox
	}{
		{cube.North, 0, cube.Box(0.375, 0.4375, 0.0625, 0.625, 0.75, 0.3125)},
		{cube.North, 1, cube.Box(0.3125, 0.3125, 0.0625, 0.6875, 0.75, 0.4375)},
		{cube.North, 2, cube.Box(0.25, 0.1875, 0.0625, 0.75, 0.75, 0.5625)},
		{cube.North, 3, cube.Box(0.1875, 0.0625, 0.0625, 0.8125, 0.75, 0.6875)},

		{cube.South, 0, cube.Box(0.375, 0.4375, 0.6875, 0.625, 0.75, 0.9375)},
		{cube.South, 1, cube.Box(0.3125, 0.3125, 0.5625, 0.6875, 0.75, 0.9375)},
		{cube.South, 2, cube.Box(0.25, 0.1875, 0.4375, 0.75, 0.75, 0.9375)},
		{cube.South, 3, cube.Box(0.1875, 0.0625, 0.3125, 0.8125, 0.75, 0.9375)},

		{cube.West, 0, cube.Box(0.0625, 0.4375, 0.375, 0.3125, 0.75, 0.625)},
		{cube.West, 1, cube.Box(0.0625, 0.3125, 0.3125, 0.4375, 0.75, 0.6875)},
		{cube.West, 2, cube.Box(0.0625, 0.1875, 0.25, 0.5625, 0.75, 0.75)},
		{cube.West, 3, cube.Box(0.0625, 0.0625, 0.1875, 0.6875, 0.75, 0.8125)},

		{cube.East, 0, cube.Box(0.6875, 0.4375, 0.375, 0.9375, 0.75, 0.625)},
		{cube.East, 1, cube.Box(0.5625, 0.3125, 0.3125, 0.9375, 0.75, 0.6875)},
		{cube.East, 2, cube.Box(0.4375, 0.1875, 0.25, 0.9375, 0.75, 0.75)},
		{cube.East, 3, cube.Box(0.3125, 0.0625, 0.1875, 0.9375, 0.75, 0.8125)},
	} {
		name := fmt.Sprintf("%v/age=%d", c.facing, c.age)
		t.Run(name, func(t *testing.T) {
			got := CocoaBean{Facing: c.facing, Age: c.age}.BBox(origin, emptySource)
			assertBoxes(t, "CocoaBean "+name, got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Skull
// ---------------------------------------------------------------------------

func TestSkullBBox(t *testing.T) {
	floor := cube.Box(0.25, 0, 0.25, 0.75, 0.5, 0.75)
	for _, c := range []struct {
		dir     cube.Face
		hanging bool
		want    cube.BBox
	}{
		// A skull that is not hanging always sits in the middle of the floor,
		// whatever direction it faces.
		{cube.FaceDown, false, floor},
		{cube.FaceUp, false, floor},
		{cube.FaceNorth, false, floor},
		{cube.FaceSouth, false, floor},
		{cube.FaceWest, false, floor},
		{cube.FaceEast, false, floor},

		// PINNED BUG: hanging is only meaningful for the four horizontal
		// directions. With FaceUp the two translations cancel and the box lands
		// back on the floor; with FaceDown they add and the box is pushed to the
		// top of the block.
		{cube.FaceDown, true, cube.Box(0.25, 0.5, 0.25, 0.75, 1, 0.75)},
		{cube.FaceUp, true, cube.Box(0.25, 0, 0.25, 0.75, 0.5, 0.75)},

		{cube.FaceNorth, true, cube.Box(0.25, 0.25, 0.5, 0.75, 0.75, 1)},
		{cube.FaceSouth, true, cube.Box(0.25, 0.25, 0, 0.75, 0.75, 0.5)},
		{cube.FaceWest, true, cube.Box(0.5, 0.25, 0.25, 1, 0.75, 0.75)},
		{cube.FaceEast, true, cube.Box(0, 0.25, 0.25, 0.5, 0.75, 0.75)},
	} {
		name := fmt.Sprintf("%v/hanging=%v", c.dir, c.hanging)
		t.Run(name, func(t *testing.T) {
			got := Skull{Direction: c.dir, Hanging: c.hanging}.BBox(origin, emptySource)
			assertBoxes(t, "Skull "+name, got, []cube.BBox{c.want})
		})
	}
}

// ---------------------------------------------------------------------------
// Shulker
// ---------------------------------------------------------------------------

func TestShulkerPhysicalPeak(t *testing.T) {
	// (1 - (1-t)^3) * 0.5 with t = progress/10.
	for _, c := range []struct {
		progress int32
		want     float64
	}{
		{0, 0},
		{1, 0.1355},
		{2, 0.244},
		{3, 0.3285},
		{4, 0.392},
		{5, 0.4375},
		{6, 0.468},
		{7, 0.4865},
		{8, 0.496},
		{9, 0.4995},
		{10, 0.5},
	} {
		t.Run(fmt.Sprintf("progress=%d", c.progress), func(t *testing.T) {
			if got := ShulkerPhysicalPeak(c.progress); math.Abs(got-c.want) > boxEpsilon {
				t.Fatalf("ShulkerPhysicalPeak(%d) = %v, want %v", c.progress, got, c.want)
			}
		})
	}
}

func TestShulkerBBox(t *testing.T) {
	// The lid grows out of the full block along Facing by exactly the peak, so a
	// fully open shulker box reaches half a block into its neighbour.
	for _, c := range []struct {
		progress int32
		peak     float64
	}{
		{0, 0},
		{5, 0.4375},
		{10, 0.5},
	} {
		for _, face := range allFaces() {
			name := fmt.Sprintf("%v/progress=%d", face, c.progress)
			t.Run(name, func(t *testing.T) {
				var want cube.BBox
				switch face {
				case cube.FaceDown:
					want = cube.Box(0, -c.peak, 0, 1, 1, 1)
				case cube.FaceUp:
					want = cube.Box(0, 0, 0, 1, 1+c.peak, 1)
				case cube.FaceNorth:
					want = cube.Box(0, 0, -c.peak, 1, 1, 1)
				case cube.FaceSouth:
					want = cube.Box(0, 0, 0, 1, 1, 1+c.peak)
				case cube.FaceWest:
					want = cube.Box(-c.peak, 0, 0, 1, 1, 1)
				case cube.FaceEast:
					want = cube.Box(0, 0, 0, 1+c.peak, 1, 1)
				}
				got := Shulker{Facing: face, Progress: c.progress}.BBox(origin, emptySource)
				assertBoxes(t, "Shulker "+name, got, []cube.BBox{want})
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Bamboo and the offset helpers
// ---------------------------------------------------------------------------

// randomOffsetGolden records the offsets observed from randomOffset. The values
// are quantised into 16 steps across [-0.25, 0.25] in float32, then widened, so
// they carry float32 precision.
var randomOffsetGolden = []struct {
	pos  cube.Pos
	x, z float64
}{
	{cube.Pos{0, 0, 0}, -0.18333333730697632, -0.11666665971279144},
	{cube.Pos{1, 0, 0}, 0.25, -0.01666666567325592},
	{cube.Pos{0, 0, 1}, 0.21666666865348816, 0.050000011920928955},
	{cube.Pos{-1, 0, -1}, -0.0833333283662796, -0.18333333730697632},
	{cube.Pos{16, 70, -32}, -0.18333333730697632, -0.18333333730697632},
	{cube.Pos{123456, 0, -654321}, 0.15000000596046448, -0.25},
}

func TestRandomOffset(t *testing.T) {
	for _, c := range randomOffsetGolden {
		t.Run(c.pos.String(), func(t *testing.T) {
			got := randomOffset(c.pos, -0.25, 0.25, 16)
			want := mgl64.Vec3{c.x, 0, c.z}
			if !vecClose(got, want) {
				t.Fatalf("randomOffset(%v) = %v, want %v", c.pos, got, want)
			}
		})
	}
}

func TestRandomOffsetIgnoresY(t *testing.T) {
	// Only X and Z feed the seed, so a column of blocks shares one offset.
	base := randomOffset(cube.Pos{3, 0, 9}, -0.25, 0.25, 16)
	for _, y := range []int{-64, 1, 63, 255, 319} {
		if got := randomOffset(cube.Pos{3, y, 9}, -0.25, 0.25, 16); got != base {
			t.Fatalf("randomOffset({3, %d, 9}) = %v, want %v", y, got, base)
		}
	}
}

func TestRandomOffsetIsDeterministic(t *testing.T) {
	for _, c := range randomOffsetGolden {
		first := randomOffset(c.pos, -0.25, 0.25, 16)
		for i := range 4 {
			if got := randomOffset(c.pos, -0.25, 0.25, 16); got != first {
				t.Fatalf("randomOffset(%v) call %d = %v, want %v", c.pos, i, got, first)
			}
		}
	}
}

func TestBambooBBox(t *testing.T) {
	// The stalk box starts at the block centre and grows towards positive X and
	// Z, then the whole box is translated by the position's random offset.
	//
	// PINNED BUG: the box is not centred on the block. cube.Box(0.5, 0, 0.5,
	// size, 1, size) puts its minimum exactly at the centre, so before the
	// random offset the stalk occupies 8/16..10/16 rather than 7/16..9/16 on
	// both horizontal axes.
	for _, c := range []struct {
		pos   cube.Pos
		thick bool
		want  cube.BBox
	}{
		{cube.Pos{0, 0, 0}, false, cube.Box(0.3166666626930237, 0, 0.38333334028720856, 0.4416666626930237, 1, 0.5083333402872086)},
		{cube.Pos{0, 0, 0}, true, cube.Box(0.3166666626930237, 0, 0.38333334028720856, 0.5041666626930237, 1, 0.5708333402872086)},
		{cube.Pos{1, 0, 0}, false, cube.Box(0.75, 0, 0.4833333343267441, 0.875, 1, 0.6083333343267441)},
		{cube.Pos{1, 0, 0}, true, cube.Box(0.75, 0, 0.4833333343267441, 0.9375, 1, 0.6708333343267441)},
		{cube.Pos{-1, 0, -1}, false, cube.Box(0.4166666716337204, 0, 0.3166666626930237, 0.5416666716337204, 1, 0.4416666626930237)},
		{cube.Pos{-1, 0, -1}, true, cube.Box(0.4166666716337204, 0, 0.3166666626930237, 0.6041666716337204, 1, 0.5041666626930237)},
	} {
		name := fmt.Sprintf("%v/thick=%v", c.pos, c.thick)
		t.Run(name, func(t *testing.T) {
			got := Bamboo{Thick: c.thick}.BBox(c.pos, emptySource)
			assertBoxes(t, "Bamboo "+name, got, []cube.BBox{c.want})
		})
	}
}

func TestBambooBBoxWidth(t *testing.T) {
	// Whatever the offset, a thin stalk is 2/16 across and a thick one 3/16.
	for _, c := range []struct {
		thick bool
		width float64
	}{
		{false, 0.125},
		{true, 0.1875},
	} {
		t.Run("thick="+boolName(c.thick), func(t *testing.T) {
			for _, pos := range []cube.Pos{{0, 0, 0}, {1, 0, 0}, {-1, 0, -1}, {123456, 0, -654321}} {
				box := Bamboo{Thick: c.thick}.BBox(pos, emptySource)[0]
				if math.Abs(box.Width()-c.width) > boxEpsilon || math.Abs(box.Length()-c.width) > boxEpsilon {
					t.Errorf("Bamboo %v width/length = %v/%v, want %v", pos, box.Width(), box.Length(), c.width)
				}
				if math.Abs(box.Height()-1) > boxEpsilon {
					t.Errorf("Bamboo %v height = %v, want 1", pos, box.Height())
				}
			}
		})
	}
}

func TestOffsetSeed(t *testing.T) {
	// The seed is the vanilla mix XORed with the golden ratio constant. The
	// value is truncated to a signed 32-bit integer before that XOR, so the top
	// half of every seed here is the constant's own top half.
	for _, c := range []struct {
		x, z int32
		want uint64
	}{
		{0, 0, 0x6A09E667F3BCC909},
		{1, 0, 0x6A09E667A2E38E52},
		{0, 1, 0x6A09E667D56373F9},
		{-1, -1, 0x6A09E667A5FB4CBC},
		{16, -32, 0x6A09E667ACAA96D2},
		{123456, -654321, 0x6A09E667BC128C4B},
	} {
		t.Run(fmt.Sprintf("%d,%d", c.x, c.z), func(t *testing.T) {
			if got := offsetSeed(c.x, c.z); got != c.want {
				t.Fatalf("offsetSeed(%d, %d) = 0x%016X, want 0x%016X", c.x, c.z, got, c.want)
			}
		})
	}
}

func TestOffsetSeedDeterminism(t *testing.T) {
	// The same coordinates must give the same seed every time.
	for _, c := range [][2]int32{{0, 0}, {1, 0}, {0, 1}, {-1, -1}, {16, -32}, {123456, -654321}} {
		first := offsetSeed(c[0], c[1])
		for i := range 4 {
			if got := offsetSeed(c[0], c[1]); got != first {
				t.Fatalf("offsetSeed(%d, %d) call %d = 0x%016X, want 0x%016X", c[0], c[1], i, got, first)
			}
		}
	}
	// Different coordinates must give different seeds for these verified pairs.
	for _, c := range [][4]int32{
		{0, 0, 1, 0},
		{0, 0, 0, 1},
		{1, 0, 0, 1},
		{16, -32, 123456, -654321},
		{-1, -1, 2, 2},
		{1, 2, -1, -2},
	} {
		a, b := offsetSeed(c[0], c[1]), offsetSeed(c[2], c[3])
		if a == b {
			t.Errorf("offsetSeed(%d, %d) == offsetSeed(%d, %d) == 0x%016X", c[0], c[1], c[2], c[3], a)
		}
	}
}

func TestOffsetSeedSignSymmetry(t *testing.T) {
	// PINNED BUG: (x, z) seeds identically to (-x, -z) whenever x and z end in
	// the same number of zero bits, so bamboo at (1, 1) and at (-1, -1) draws
	// the same model offset. Both coordinate multipliers are odd, so each
	// product keeps its coordinate's trailing-zero count; when the two counts
	// match, negating both operands complements exactly the same bit range in
	// each and the XOR cancels the complement.
	for _, c := range [][2]int32{
		{1, 1}, {3, 5}, {7, -9}, {-11, 13}, {123457, -654321},
		{2, 2}, {2, 6}, {4, 12}, {0, 0},
	} {
		x, z := c[0], c[1]
		if got, mirrored := offsetSeed(x, z), offsetSeed(-x, -z); got != mirrored {
			t.Errorf("offsetSeed(%d, %d) = 0x%016X, offsetSeed(%d, %d) = 0x%016X, want equal",
				x, z, got, -x, -z, mirrored)
		}
	}
	// Differing trailing-zero counts break the symmetry.
	for _, c := range [][2]int32{{1, 2}, {2, 1}, {1, 4}, {16, -32}} {
		x, z := c[0], c[1]
		if got, mirrored := offsetSeed(x, z), offsetSeed(-x, -z); got == mirrored {
			t.Errorf("offsetSeed(%d, %d) == offsetSeed(%d, %d) == 0x%016X", x, z, -x, -z, got)
		}
	}
}

func TestRandomOffsetSignSymmetry(t *testing.T) {
	// The seed collision above reaches all the way through to the model offset.
	for _, c := range [][2]int{{1, 1}, {3, 5}, {7, -9}, {-11, 13}, {2, 2}, {4, 12}} {
		a := randomOffset(cube.Pos{c[0], 0, c[1]}, -0.25, 0.25, 16)
		b := randomOffset(cube.Pos{-c[0], 0, -c[1]}, -0.25, 0.25, 16)
		if a != b {
			t.Errorf("randomOffset(%d, %d) = %v, randomOffset(%d, %d) = %v, want equal",
				c[0], c[1], a, -c[0], -c[1], b)
		}
	}
}

func TestRandomFloat32(t *testing.T) {
	// Only the top 24 bits of the input matter; the low 40 are discarded.
	for _, c := range []struct {
		random uint64
		want   float32
	}{
		{0, 0},
		{(1 << 40) - 1, 0},
		{1 << 40, 1.0 / 16777216.0},
		{1 << 63, 0.5},
		{^uint64(0), 16777215.0 / 16777216.0},
		{3 << 62, 0.75},
		{1 << 62, 0.25},
	} {
		t.Run(fmt.Sprintf("%#016x", c.random), func(t *testing.T) {
			if got := randomFloat32(c.random); got != c.want {
				t.Fatalf("randomFloat32(%#016x) = %v, want %v", c.random, got, c.want)
			}
		})
	}
}

func TestRandomFloat32IsBelowOne(t *testing.T) {
	for _, random := range []uint64{0, 1, ^uint64(0), 1 << 63, 0x0123456789ABCDEF} {
		if got := randomFloat32(random); got < 0 || got >= 1 {
			t.Errorf("randomFloat32(%#016x) = %v, want a value in [0, 1)", random, got)
		}
	}
}

func TestOffsetValue(t *testing.T) {
	for _, c := range []struct {
		name           string
		mn, mx, random float32
		steps          int
		want           float32
	}{
		// mn >= mx short-circuits to mn without looking at anything else.
		{"equal bounds", 0.25, 0.25, 0.75, 16, 0.25},
		{"inverted bounds", 0.5, 0.25, 0.75, 16, 0.5},
		{"inverted bounds ignore steps", 0.5, 0.25, 0.75, 1, 0.5},

		// A single step always lands on the midpoint.
		{"one step", -0.25, 0.25, 0, 1, 0},
		{"one step high random", -0.25, 0.25, 0.9999, 1, 0},
		{"one step asymmetric", 0, 1, 0.3, 1, 0.5},

		// steps > 1 quantises: index = floor(steps*random).
		{"16 steps index 0", -0.25, 0.25, 0, 16, -0.25},
		{"16 steps index 15", -0.25, 0.25, 16777215.0 / 16777216.0, 16, 0.25},
		{"16 steps index 8", -0.25, 0.25, 0.5, 16, 0.016666680574417114},
		{"16 steps index 2", -0.25, 0.25, 0.125, 16, -0.18333333730697632},
		{"2 steps low", -0.25, 0.25, 0, 2, -0.25},
		{"2 steps high", -0.25, 0.25, 0.5, 2, 0.25},
		{"4 steps index 2", -0.25, 0.25, 0.5, 4, 0.083333336},

		// steps <= 0 falls through to a plain linear interpolation.
		{"zero steps", -0.25, 0.25, 0.5, 0, 0},
		{"zero steps low", -0.25, 0.25, 0, 0, -0.25},
		{"negative steps", -0.25, 0.25, 0.25, -1, -0.125},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := offsetValue(c.mn, c.mx, c.steps, c.random)
			if math.Abs(float64(got)-float64(c.want)) > float32Epsilon {
				t.Fatalf("offsetValue(%v, %v, %d, %v) = %v, want %v", c.mn, c.mx, c.steps, c.random, got, c.want)
			}
		})
	}
}
