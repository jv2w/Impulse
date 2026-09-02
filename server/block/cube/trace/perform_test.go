package trace_test

import (
	"cmp"
	"context"
	"iter"
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/cube/trace"
	"github.com/jv2w/Impulse/server/world"
)

// Perform needs a real world transaction, so these tests live in the external
// test package: server/block imports trace, which an internal test could not.
//
// The entity used throughout is performEntityType, whose BBox is
// cube.Box(-0.25, 0, -0.25, 0.25, 1, 0.25). EntityIntercept grows that by 0.3
// on every axis, so an entity at (x, y, z) presents a box reaching from
// x-0.55 to x+0.55 horizontally.

const performEpsilon = 1e-9

// searchBox is the bounding box callers hand to Perform to describe the volume
// swept by the ray. It only widens the entity search, never the ray itself.
var searchBox = cube.Box(-0.3, -0.3, -0.3, 0.3, 0.3, 0.3)

func syncWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func mustDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatalf("world task failed: %v", err)
	}
}

func assertVec3(t *testing.T, name string, got, want mgl64.Vec3) {
	t.Helper()
	for i := range 3 {
		if math.Abs(got[i]-want[i]) > performEpsilon {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestPerformFindsNothingInEmptyAir(t *testing.T) {
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		hit, ok := trace.Perform(mgl64.Vec3{0.5, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, nil)
		if ok || hit != nil {
			t.Fatalf("Perform() through air = (%v, %v), want (nil, false)", hit, ok)
		}
	})
}

// TestPerformHitsTheFirstBlock pins that the block walk stops at the first
// block whose model the ray enters, and reports it as a trace.BlockResult.
func TestPerformHitsTheFirstBlock(t *testing.T) {
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{3, 64, 0}, block.Stone{}, nil)

		hit, ok := trace.Perform(mgl64.Vec3{-4, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, nil)
		if !ok {
			t.Fatal("Perform() reported no hit, want the block at (1,64,0)")
		}
		result, isBlock := hit.(trace.BlockResult)
		if !isBlock {
			t.Fatalf("Perform() = %T, want a trace.BlockResult", hit)
		}
		if got := result.BlockPosition(); got != (cube.Pos{1, 64, 0}) {
			t.Fatalf("BlockPosition() = %v, want (1,64,0): the nearer of the two blocks", got)
		}
		if got := result.Face(); got != cube.FaceWest {
			t.Fatalf("Face() = %v, want west", got)
		}
		assertVec3(t, "Position()", result.Position(), mgl64.Vec3{1, 64.5, 0.5})
	})
}

// TestPerformPrefersAnEntityInFrontOfTheBlock pins that an entity closer to the
// start than the block wins, and that the result is an EntityResult.
func TestPerformPrefersAnEntityInFrontOfTheBlock(t *testing.T) {
	w := syncWorld(t)
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{-1, 64, 0.5}}.New(performEntityType{}, performEntityConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.AddEntity(handle)

		hit, ok := trace.Perform(mgl64.Vec3{-4, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, nil)
		if !ok {
			t.Fatal("Perform() reported no hit, want the entity")
		}
		result, isEntity := hit.(trace.EntityResult)
		if !isEntity {
			t.Fatalf("Perform() = %T, want a trace.EntityResult", hit)
		}
		if result.Entity().H() != handle {
			t.Fatalf("Entity() = %v, want the entity that was added", result.Entity().Position())
		}
		// -1 - 0.25 - 0.3: the entity's own box grown by the 0.3 margin.
		assertVec3(t, "Position()", result.Position(), mgl64.Vec3{-1.55, 64.5, 0.5})
	})
}

// TestPerformKeepsTheBlockWhenTheEntityIsBehindIt pins that the block hit
// shortens the ray: entities past the block are no longer on the segment and
// cannot overwrite it.
func TestPerformKeepsTheBlockWhenTheEntityIsBehindIt(t *testing.T) {
	w := syncWorld(t)
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{3, 64, 0.5}}.New(performEntityType{}, performEntityConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.AddEntity(handle)

		hit, ok := trace.Perform(mgl64.Vec3{-4, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, nil)
		if !ok {
			t.Fatal("Perform() reported no hit, want the block")
		}
		if _, isBlock := hit.(trace.BlockResult); !isBlock {
			t.Fatalf("Perform() = %T, want a trace.BlockResult: the entity is behind the block", hit)
		}
	})
}

// TestPerformPicksTheNearerOfTwoEntities pins the strict less-than in the
// entity loop through distinct distances, which is the rule server/entity's
// hit-selection contract is built on.
func TestPerformPicksTheNearerOfTwoEntities(t *testing.T) {
	w := syncWorld(t)
	near := world.EntitySpawnOpts{Position: mgl64.Vec3{-1, 64, 0.5}}.New(performEntityType{}, performEntityConfig{})
	far := world.EntitySpawnOpts{Position: mgl64.Vec3{2, 64, 0.5}}.New(performEntityType{}, performEntityConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(near)
		tx.AddEntity(far)

		hit, ok := trace.Perform(mgl64.Vec3{-4, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, nil)
		if !ok {
			t.Fatal("Perform() reported no hit, want the nearer entity")
		}
		result, isEntity := hit.(trace.EntityResult)
		if !isEntity {
			t.Fatalf("Perform() = %T, want a trace.EntityResult", hit)
		}
		if result.Entity().H() != near {
			t.Fatalf("Entity() = %v, want the entity at (-1, 64, 0.5)", result.Entity().Position())
		}
	})
}

// TestPerformAppliesTheEntityFilter pins that the filter is applied to the
// entity sequence before any interception, and that a filter removing every
// entity leaves only the block hit.
func TestPerformAppliesTheEntityFilter(t *testing.T) {
	w := syncWorld(t)
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{-1, 64, 0.5}}.New(performEntityType{}, performEntityConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.AddEntity(handle)

		dropAll := func(iter.Seq[world.Entity]) iter.Seq[world.Entity] {
			return func(func(world.Entity) bool) {}
		}
		hit, ok := trace.Perform(mgl64.Vec3{-4, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, dropAll)
		if !ok {
			t.Fatal("Perform() reported no hit, want the block")
		}
		if _, isBlock := hit.(trace.BlockResult); !isBlock {
			t.Fatalf("Perform() = %T, want a trace.BlockResult once the entity is filtered out", hit)
		}
	})
}

// TestPerformPanicsOnAZeroLengthRay pins that Perform inherits the panic in
// TraverseBlocks rather than reporting the mistake as a value.
//
// PINNED BUG: a caller whose start and end coincide takes down the world
// goroutine. Pinned as-is; decide in stage 1.
func TestPerformPanicsOnAZeroLengthRay(t *testing.T) {
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		defer func() {
			if recover() == nil {
				t.Error("Perform() with equal start and end did not panic, want a panic")
			}
		}()
		trace.Perform(mgl64.Vec3{0.5, 64.5, 0.5}, mgl64.Vec3{0.5, 64.5, 0.5}, tx, searchBox, nil)
	})
}

// performEntityType is a world.EntityType with a fixed 0.5 x 1 x 0.5 bounding
// box whose origin sits at the centre of its bottom face.
type performEntityType struct{}

func (performEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &performEntity{tx: tx, handle: handle, data: data}
}
func (performEntityType) EncodeEntity() string { return "impulse:trace_perform_entity" }
func (performEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 1, 0.25)
}
func (performEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (performEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type performEntityConfig struct{}

func (performEntityConfig) Apply(*world.EntityData) {}

type performEntity struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	once   sync.Once
}

func (e *performEntity) H() *world.EntityHandle  { return e.handle }
func (e *performEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *performEntity) Rotation() cube.Rotation { return e.data.Rot }

// Close mirrors entity.Ent.Close: the world calls it while closing a chunk, and
// the handle may only be closed after the entity leaves the transaction.
func (e *performEntity) Close() error {
	e.once.Do(func() {
		e.tx.RemoveEntity(e)
		_ = e.handle.Close()
	})
	return nil
}

// orderedByZ is an EntityFilter that drains the sequence and yields it back in
// a deterministic order, which is what makes the tie rule below observable: the
// world does not promise an iteration order of its own.
func orderedByZ(ascending bool) trace.EntityFilter {
	return func(seq iter.Seq[world.Entity]) iter.Seq[world.Entity] {
		return func(yield func(world.Entity) bool) {
			var all []world.Entity
			for e := range seq {
				all = append(all, e)
			}
			slices.SortFunc(all, func(a, b world.Entity) int {
				if ascending {
					return cmp.Compare(a.Position()[2], b.Position()[2])
				}
				return cmp.Compare(b.Position()[2], a.Position()[2])
			})
			for _, e := range all {
				if !yield(e) {
					return
				}
			}
		}
	}
}

// TestPerformEntityTieKeepsTheFirstYielded is the pin for the strict `<` in
// Perform's entity loop. Two entities straddle the ray symmetrically, so both
// are intercepted at exactly the same point and therefore at exactly the same
// distance. The one produced FIRST by the sequence is the one kept.
//
// server/entity's hit-selection contract is this comparison. Turning it into
// `<=` hands the tie to the last entity instead, and both halves below flip.
func TestPerformEntityTieKeepsTheFirstYielded(t *testing.T) {
	w := syncWorld(t)
	// Offset by 0.2 on Z either side of the ray. Both entities present a box
	// reaching from x-0.55, so both are entered at x = -1.55, y = 64.5, z = 0.5.
	north := world.EntitySpawnOpts{Position: mgl64.Vec3{-1, 64, 0.3}}.New(performEntityType{}, performEntityConfig{})
	south := world.EntitySpawnOpts{Position: mgl64.Vec3{-1, 64, 0.7}}.New(performEntityType{}, performEntityConfig{})

	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(north)
		tx.AddEntity(south)

		for _, test := range []struct {
			name   string
			filter trace.EntityFilter
			want   *world.EntityHandle
			loser  *world.EntityHandle
		}{
			{"lower_z_first", orderedByZ(true), north, south},
			{"higher_z_first", orderedByZ(false), south, north},
		} {
			hit, ok := trace.Perform(mgl64.Vec3{-4, 64.5, 0.5}, mgl64.Vec3{5.5, 64.5, 0.5}, tx, searchBox, test.filter)
			if !ok {
				t.Fatalf("%s: Perform() reported no hit, want one of the two entities", test.name)
			}
			result, isEntity := hit.(trace.EntityResult)
			if !isEntity {
				t.Fatalf("%s: Perform() = %T, want a trace.EntityResult", test.name, hit)
			}
			// Both entities are hit at the identical point, so the position
			// cannot distinguish them; only the identity can.
			assertVec3(t, test.name+": Position()", result.Position(), mgl64.Vec3{-1.55, 64.5, 0.5})
			if got := result.Entity().H(); got != test.want {
				if got == test.loser {
					t.Fatalf("%s: Perform() kept the LAST of two equally distant entities, so the comparison is no longer strictly less than", test.name)
				}
				t.Fatalf("%s: Entity() = %v, want the entity yielded first", test.name, result.Entity().Position())
			}
		}
	})
}
