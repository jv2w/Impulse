package trace

import (
	"math"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/world"
)

// All three result types satisfy Result, which is what lets Perform hand back
// either a block or an entity hit through one value.
var (
	_ Result = BBoxResult{}
	_ Result = BlockResult{}
	_ Result = EntityResult{}
)

// collectTraversal records every position TraverseBlocks visits, stopping after
// limit positions when limit is positive.
func collectTraversal(start, end mgl64.Vec3, limit int) []cube.Pos {
	var got []cube.Pos
	TraverseBlocks(start, end, func(pos cube.Pos) bool {
		got = append(got, pos)
		return limit <= 0 || len(got) < limit
	})
	return got
}

// TestTraverseBlocksAlongEachAxis pins the voxel sequence for an axis-aligned
// ray in both senses on all three axes. The sequences are exact mirrors, so an
// axis wired up wrong shows here.
func TestTraverseBlocksAlongEachAxis(t *testing.T) {
	tests := []struct {
		name       string
		start, end mgl64.Vec3
		want       []cube.Pos
	}{
		{
			"east", mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{3.5, 0.5, 0.5},
			[]cube.Pos{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {3, 0, 0}},
		},
		{
			"west", mgl64.Vec3{3.5, 0.5, 0.5}, mgl64.Vec3{0.5, 0.5, 0.5},
			[]cube.Pos{{3, 0, 0}, {2, 0, 0}, {1, 0, 0}, {0, 0, 0}},
		},
		{
			"up", mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{0.5, 3.5, 0.5},
			[]cube.Pos{{0, 0, 0}, {0, 1, 0}, {0, 2, 0}, {0, 3, 0}},
		},
		{
			"down", mgl64.Vec3{0.5, 3.5, 0.5}, mgl64.Vec3{0.5, 0.5, 0.5},
			[]cube.Pos{{0, 3, 0}, {0, 2, 0}, {0, 1, 0}, {0, 0, 0}},
		},
		{
			"south", mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{0.5, 0.5, 3.5},
			[]cube.Pos{{0, 0, 0}, {0, 0, 1}, {0, 0, 2}, {0, 0, 3}},
		},
		{
			"north", mgl64.Vec3{0.5, 0.5, 3.5}, mgl64.Vec3{0.5, 0.5, 0.5},
			[]cube.Pos{{0, 0, 3}, {0, 0, 2}, {0, 0, 1}, {0, 0, 0}},
		},
		{
			// Negative coordinates use the same flooring as cube.PosFromVec3.
			"east_across_the_origin", mgl64.Vec3{-1.5, 0.5, 0.5}, mgl64.Vec3{1.5, 0.5, 0.5},
			[]cube.Pos{{-2, 0, 0}, {-1, 0, 0}, {0, 0, 0}, {1, 0, 0}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := collectTraversal(test.start, test.end, 0); !slices.Equal(got, test.want) {
				t.Fatalf("TraverseBlocks(%v, %v) visited %v, want %v", test.start, test.end, got, test.want)
			}
		})
	}
}

// TestTraverseBlocksStartsAtTheStartPosition pins that the block containing the
// start point is always the first one handed to the callback, before any step
// is taken.
func TestTraverseBlocksStartsAtTheStartPosition(t *testing.T) {
	for _, start := range []mgl64.Vec3{{0.5, 0.5, 0.5}, {-0.5, 70.9, 12.01}, {16, 0, 16}} {
		got := collectTraversal(start, start.Add(mgl64.Vec3{5, 5, 5}), 1)
		want := cube.PosFromVec3(start)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("TraverseBlocks() from %v first visited %v, want %v", start, got, want)
		}
	}
}

// TestTraverseBlocksStopsWhenTheCallbackReturnsFalse pins the early exit that
// Perform relies on to stop at the first block it collides with.
func TestTraverseBlocksStopsWhenTheCallbackReturnsFalse(t *testing.T) {
	got := collectTraversal(mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{20.5, 0.5, 0.5}, 3)
	want := []cube.Pos{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}}
	if !slices.Equal(got, want) {
		t.Fatalf("TraverseBlocks() before the callback stopped it visited %v, want %v", got, want)
	}
}

// TestTraverseBlocksDiagonalPrefersYOnATie pins the step selection when two
// axis boundaries fall at exactly the same distance. The switch tests
// max[0] < max[1] && max[0] < max[2] first with a strict comparison, so an
// X/Y tie steps on Y, not X.
func TestTraverseBlocksDiagonalPrefersYOnATie(t *testing.T) {
	got := collectTraversal(mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{2.5, 2.5, 0.5}, 0)
	want := []cube.Pos{{0, 0, 0}, {0, 1, 0}, {1, 1, 0}, {1, 2, 0}, {2, 2, 0}}
	if !slices.Equal(got, want) {
		t.Fatalf("TraverseBlocks() along the XY diagonal visited %v, want %v", got, want)
	}
}

// TestTraverseBlocksPanicsOnAZeroLengthRay pins the one panic in this package.
//
// PINNED BUG: a degenerate ray is a caller mistake that TraverseBlocks cannot
// fix, but it takes the goroutine down instead of reporting the failure as a
// value. Perform inherits it, so an entity whose eye position equals its target
// kills the world goroutine.
func TestTraverseBlocksPanicsOnAZeroLengthRay(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TraverseBlocks() with equal start and end did not panic, want a panic")
		}
	}()
	TraverseBlocks(mgl64.Vec3{1, 2, 3}, mgl64.Vec3{1, 2, 3}, func(cube.Pos) bool { return true })
}

func TestTraverseBlocksHelpers(t *testing.T) {
	t.Run("sign", func(t *testing.T) {
		tests := []struct{ in, want float64 }{
			{5, 1}, {0.0001, 1}, {-5, -1}, {-0.0001, -1}, {0, 0},
		}
		for _, test := range tests {
			if got := sign(test.in); got != test.want {
				t.Fatalf("sign(%v) = %v, want %v", test.in, got, test.want)
			}
		}
	})
	t.Run("safeDivide", func(t *testing.T) {
		if got := safeDivide(6, 3); got != 2 {
			t.Fatalf("safeDivide(6, 3) = %v, want 2", got)
		}
		// Guarding against a zero divisor is what keeps an axis-aligned ray
		// from producing a NaN step.
		if got := safeDivide(6, 0); got != 0 {
			t.Fatalf("safeDivide(6, 0) = %v, want 0", got)
		}
		if got := safeDivide(0, 0); got != 0 {
			t.Fatalf("safeDivide(0, 0) = %v, want 0", got)
		}
	})
	t.Run("boundary", func(t *testing.T) {
		tests := []struct {
			start, dir, want float64
		}{
			// Distance to the next boundary going forwards.
			{0.5, 1, 0.5},
			{0.0, 1, 1},
			{0.25, 0.5, 1.5},
			// Going backwards the start is mirrored, so the distance is to the
			// boundary below.
			{0.5, -1, 0.5},
			{0.75, -1, 0.75},
			// A start exactly on a boundary going backwards is already there.
			{1.0, -1, 0},
			{0.0, -1, 0},
			// No movement on this axis means the boundary is never crossed.
			{0.5, 0, math.Inf(1)},
		}
		for _, test := range tests {
			if got := boundary(test.start, test.dir); got != test.want {
				t.Fatalf("boundary(%v, %v) = %v, want %v", test.start, test.dir, got, test.want)
			}
		}
	})
}

// testBlock is a world.Block whose only interesting property is its model.
type testBlock struct{ model world.BlockModel }

func (testBlock) EncodeBlock() (string, map[string]any) { return "impulse:trace_test", nil }
func (testBlock) Hash() (uint64, uint64)                { return 0, 0 }
func (b testBlock) Model() world.BlockModel             { return b.model }

// testSource is a world.BlockSource backed by a map, answering with an empty
// block for any position it does not hold.
type testSource map[cube.Pos]world.Block

func (s testSource) Block(pos cube.Pos) world.Block {
	if b, ok := s[pos]; ok {
		return b
	}
	return testBlock{model: model.Empty{}}
}

// twoBoxModel returns a fixed pair of boxes, in the order given, so the nearest
// selection inside BlockIntercept can be observed independently of any real
// block model.
type twoBoxModel struct{ boxes []cube.BBox }

func (m twoBoxModel) BBox(cube.Pos, world.BlockSource) []cube.BBox { return m.boxes }
func (twoBoxModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}

func TestBlockIntercept(t *testing.T) {
	src := testSource{}
	start, end := mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{20, 0.5, 0.5}

	t.Run("an_empty_model_never_hits", func(t *testing.T) {
		_, ok := BlockIntercept(cube.Pos{0, 0, 0}, src, testBlock{model: model.Empty{}}, start, end)
		if ok {
			t.Fatal("BlockIntercept() on an empty model reported a hit, want none")
		}
	})

	t.Run("a_solid_model_hits_its_west_face", func(t *testing.T) {
		result, ok := BlockIntercept(cube.Pos{0, 0, 0}, src, testBlock{model: model.Solid{}}, start, end)
		if !ok {
			t.Fatal("BlockIntercept() on a solid model reported no hit, want one")
		}
		if got := result.Face(); got != cube.FaceWest {
			t.Fatalf("Face() = %v, want west", got)
		}
		assertVec3(t, "Position()", result.Position(), mgl64.Vec3{0, 0.5, 0.5})
		if got := result.BlockPosition(); got != (cube.Pos{0, 0, 0}) {
			t.Fatalf("BlockPosition() = %v, want (0,0,0)", got)
		}
		if got := result.BBox(); got != cube.Box(0, 0, 0, 1, 1, 1) {
			t.Fatalf("BBox() = %v, want the model box translated to the block, %v", got, cube.Box(0, 0, 0, 1, 1, 1))
		}
	})

	t.Run("the_model_box_is_translated_by_the_block_position", func(t *testing.T) {
		result, ok := BlockIntercept(cube.Pos{5, 0, 0}, src, testBlock{model: model.Solid{}}, start, end)
		if !ok {
			t.Fatal("BlockIntercept() reported no hit, want one")
		}
		assertVec3(t, "Position()", result.Position(), mgl64.Vec3{5, 0.5, 0.5})
		if got := result.BBox(); got != cube.Box(5, 0, 0, 6, 1, 1) {
			t.Fatalf("BBox() = %v, want (5,0,0)-(6,1,1)", got)
		}
	})

	t.Run("the_nearest_of_several_boxes_wins_whatever_the_order", func(t *testing.T) {
		near, far := cube.Box(0, 0, 0, 0.25, 1, 1), cube.Box(0.75, 0, 0, 1, 1, 1)
		for _, boxes := range [][]cube.BBox{{near, far}, {far, near}} {
			result, ok := BlockIntercept(cube.Pos{0, 0, 0}, src, testBlock{model: twoBoxModel{boxes: boxes}}, start, end)
			if !ok {
				t.Fatalf("BlockIntercept() with the boxes %v reported no hit, want one", boxes)
			}
			assertVec3(t, "Position()", result.Position(), mgl64.Vec3{0, 0.5, 0.5})
		}
	})

	t.Run("an_exact_tie_between_boxes_keeps_the_first", func(t *testing.T) {
		// Two boxes that produce the identical hit point. The comparison in
		// BlockIntercept is strictly less than, so the earlier box's result is
		// the one kept. Only the reported BBox distinguishes them.
		first := cube.Box(0, 0, 0, 1, 1, 1)
		second := cube.Box(0, 0, 0, 1, 0.75, 1)

		result, ok := BlockIntercept(cube.Pos{0, 0, 0}, src, testBlock{model: twoBoxModel{boxes: []cube.BBox{first, second}}}, start, end)
		if !ok {
			t.Fatal("BlockIntercept() reported no hit, want one")
		}
		if got := result.BBox(); got != first {
			t.Fatalf("BBox() = %v, want the first of two equally distant boxes, %v", got, first)
		}
	})

	t.Run("a_ray_that_misses_the_block_reports_nothing", func(t *testing.T) {
		result, ok := BlockIntercept(cube.Pos{0, 50, 0}, src, testBlock{model: model.Solid{}}, start, end)
		if ok {
			t.Fatalf("BlockIntercept() = %v, want no hit", result)
		}
		if result != (BlockResult{}) {
			t.Fatalf("BlockIntercept() returned %v alongside ok false, want the zero result", result)
		}
	})
}

func TestBlockIntersects(t *testing.T) {
	src := testSource{}
	pos := cube.Pos{0, 0, 0}
	start, end := mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{2, 0.5, 0.5}

	if BlockIntersects(pos, src, testBlock{model: model.Empty{}}, start, end) {
		t.Fatal("BlockIntersects() on an empty model = true, want false")
	}
	if !BlockIntersects(pos, src, testBlock{model: model.Solid{}}, start, end) {
		t.Fatal("BlockIntersects() on a solid model = false, want true")
	}
	// A model whose boxes are all missed reports false even though the block
	// itself is on the ray.
	sliver := twoBoxModel{boxes: []cube.BBox{cube.Box(0, 0.9, 0, 1, 1, 1)}}
	if BlockIntersects(pos, src, testBlock{model: sliver}, start, end) {
		t.Fatal("BlockIntersects() against a box the ray passes under = true, want false")
	}
	if !BlockIntersects(pos, src, testBlock{model: sliver}, mgl64.Vec3{-1, 0.95, 0.5}, mgl64.Vec3{2, 0.95, 0.5}) {
		t.Fatal("BlockIntersects() against a box the ray passes through = false, want true")
	}
	// Solid takes a shortcut that ignores the model's own boxes; the result is
	// the same full block either way.
	if !BlockIntersects(cube.Pos{7, 8, 9}, src, testBlock{model: model.Solid{}}, mgl64.Vec3{7.5, 0, 9.5}, mgl64.Vec3{7.5, 20, 9.5}) {
		t.Fatal("BlockIntersects() through a solid block at (7,8,9) = false, want true")
	}
}

// testEntityType is a world.EntityType with a fixed bounding box, 0.5 wide,
// 1 tall, origin at the centre of its bottom face.
type testEntityType struct{}

func (testEntityType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	return nil
}
func (testEntityType) EncodeEntity() string { return "impulse:trace_test_entity" }
func (testEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 1, 0.25)
}
func (testEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (testEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type testEntityConfig struct{}

func (testEntityConfig) Apply(*world.EntityData) {}

// testEntity is a world.Entity that reports a fixed position and is never added
// to a world; EntityIntercept only reads its handle's type and its position.
type testEntity struct {
	handle *world.EntityHandle
	pos    mgl64.Vec3
}

func newTestEntity(pos mgl64.Vec3) testEntity {
	return testEntity{handle: world.EntitySpawnOpts{Position: pos}.New(testEntityType{}, testEntityConfig{}), pos: pos}
}

func (e testEntity) H() *world.EntityHandle { return e.handle }
func (e testEntity) Position() mgl64.Vec3   { return e.pos }
func (testEntity) Rotation() cube.Rotation  { return cube.Rotation{} }
func (testEntity) Close() error             { return nil }

// TestEntityInterceptGrowsTheBoxBy0Point3 pins the hit box an entity presents
// to a ray: its type's box, translated to its position, then grown by 0.3 on
// every axis. server/entity's hit tests are built on exactly this margin.
func TestEntityInterceptGrowsTheBoxBy0Point3(t *testing.T) {
	e := newTestEntity(mgl64.Vec3{10, 0, 0})
	wantBox := cube.Box(9.45, -0.3, -0.55, 10.55, 1.3, 0.55)

	result, ok := EntityIntercept(e, mgl64.Vec3{0, 0.5, 0}, mgl64.Vec3{20, 0.5, 0})
	if !ok {
		t.Fatal("EntityIntercept() reported no hit, want one")
	}
	if got := result.BBox(); got != wantBox {
		t.Fatalf("BBox() = %v, want %v", got, wantBox)
	}
	if got := result.Face(); got != cube.FaceWest {
		t.Fatalf("Face() = %v, want west", got)
	}
	assertVec3(t, "Position()", result.Position(), mgl64.Vec3{9.45, 0.5, 0})
	if result.Entity() != world.Entity(e) {
		t.Fatalf("Entity() = %v, want the entity that was passed in", result.Entity())
	}

	// The margin is what makes this a hit: without the 0.3 the entity's own box
	// stops at 9.75, and a ray offset by 0.4 on Z would miss it entirely.
	if _, ok := EntityIntercept(e, mgl64.Vec3{0, 0.5, 0.4}, mgl64.Vec3{20, 0.5, 0.4}); !ok {
		t.Fatal("EntityIntercept() 0.4 off the entity's own box reported no hit, want one inside the grown box")
	}
	if _, ok := EntityIntercept(e, mgl64.Vec3{0, 0.5, 0.6}, mgl64.Vec3{20, 0.5, 0.6}); ok {
		t.Fatal("EntityIntercept() 0.6 off the entity reported a hit, want none outside the grown box")
	}
}

func TestEntityInterceptMisses(t *testing.T) {
	e := newTestEntity(mgl64.Vec3{10, 0, 0})

	result, ok := EntityIntercept(e, mgl64.Vec3{0, 50, 0}, mgl64.Vec3{20, 50, 0})
	if ok {
		t.Fatalf("EntityIntercept() = %v, want no hit", result)
	}
	if result != (EntityResult{}) {
		t.Fatalf("EntityIntercept() returned %v alongside ok false, want the zero result", result)
	}
	// A segment that stops before reaching the entity is a miss as well.
	if _, ok := EntityIntercept(e, mgl64.Vec3{0, 0.5, 0}, mgl64.Vec3{5, 0.5, 0}); ok {
		t.Fatal("EntityIntercept() with a segment stopping short reported a hit, want none")
	}
}
