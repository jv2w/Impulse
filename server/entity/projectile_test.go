package entity

import (
	"fmt"
	"iter"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/cube/trace"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/potion"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/particle"
	"github.com/jv2w/Impulse/server/world/sound"
)

// Every projectile in this file uses testMovingEntType, whose BBox is
// cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125). Two derived boxes recur in
// the hand computations below:
//
//   - the entity search box of trace.Perform, BBox.Grow(1.0) translated to the
//     ray start and extended by the ray, then grown by a further 8.0 for the
//     tx.EntitiesWithin query only;
//   - the interception box of trace.EntityIntercept, BBox.Grow(0.3) translated
//     to the target's position, so a target reaches 0.425 to either side of its
//     own position on X and Z.
//
// Projectiles never call MovementComputer.CheckCollision, so MovementComputer's
// onGround is permanently false and applyHorizontalForces reduces to (1-Drag).
// Every golden below therefore follows from
//
//	vel = {vx*(1-Drag), vy*(1-Drag) - Gravity, vz*(1-Drag)}
//	rot = {RadToDeg(Atan2(vx, vz)), RadToDeg(Atan2(vy, Hypot(vx, vz)))}
//	pos = pos + vel

// ---------------------------------------------------------------------------
// 1. Hit-candidate selection order.
// ---------------------------------------------------------------------------

func TestProjectileClosestEntityWinsRegardlessOfInsertionOrder(t *testing.T) {
	// trace.Perform keeps the entity with the strictly smallest squared distance,
	// so a nearer target must win whichever order tx.EntitiesWithin yields the
	// two in. Both targets sit in chunk (0, 0), where yielding follows insertion
	// order today, so swapping the spawn order swaps the yield order.
	near, far := mgl64.Vec3{2, 100, 0.5}, mgl64.Vec3{3, 100, 0.5}
	tests := []struct {
		name  string
		order []mgl64.Vec3
	}{
		{name: "near target spawned first", order: []mgl64.Vec3{near, far}},
		{name: "far target spawned first", order: []mgl64.Vec3{far, near}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				projectileSpawnTargets(tx, test.order...)

				var hit trace.Result
				conf := ProjectileBehaviourConfig{}
				conf.Hit = func(_ *Ent, _ *world.Tx, target trace.Result) { hit = target }
				e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
				e.Behaviour().Tick(e, tx)

				assertVec3(t, "hit entity position", projectileHitEntity(t, hit).Position(), near)
				// The intercept is the west face of the near target's grown box:
				// 2 - 0.125 - 0.3 = 1.575.
				assertVec3(t, "hit position", hit.Position(), mgl64.Vec3{1.575, 100, 0.5})
			})
		})
	}
}

func TestProjectileExactDistanceTieKeepsFirstYielded(t *testing.T) {
	// LATENT: on an exact distance tie trace.Perform keeps the first entity
	// yielded by tx.EntitiesWithin, which today is chunk-major then insertion
	// order. Stage 2.1 replaces that storage with a spatial index and this
	// assertion WILL change. That is the point of pinning it.
	//
	// The ties below are exact in float64, not merely close: both targets share
	// the same coordinate on the axis the ray advances along, so BBoxIntercept
	// evaluates bit-identical expressions for both and start.Sub(pos).LenSqr()
	// returns the same float. trace.Perform's comparison is `distance < dist`,
	// so the second of two equal candidates never displaces the first.
	//
	// Note also that trace.Perform queries tx.EntitiesWithin with the search box
	// grown by a further 8.0, far wider than any candidate that can survive the
	// IntersectsWith check below it. A tighter spatial index would hand the loop
	// a different candidate set, which is exactly what these ties are sensitive
	// to. TestProjectileEntitySearchBoxIsGrownByEight pins that radius.
	t.Run("identical positions", func(t *testing.T) {
		// The degenerate tie: three targets stacked on the same point produce
		// the same interception box, so nothing but yield order can separate
		// them and the one spawned first is kept.
		w := syncWorld(t)
		mustDo(t, w, func(tx *world.Tx) {
			pos := mgl64.Vec3{3, 100, 0.5}
			spawned := projectileSpawnTargets(tx, pos, pos, pos)

			var hit trace.Result
			conf := ProjectileBehaviourConfig{}
			conf.Hit = func(_ *Ent, _ *world.Tx, target trace.Result) { hit = target }
			e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
			e.Behaviour().Tick(e, tx)

			// The three share a position, so only the spawn index tells them apart.
			if got := slices.Index(spawned, projectileHitEntity(t, hit).H()); got != 0 {
				t.Fatalf("hit entity on an identical-position tie was spawned at index %d, want index 0", got)
			}
		})
	})

	t.Run("symmetric about the ray in one chunk", func(t *testing.T) {
		// Targets at z = 0.3 and z = 0.7 straddle a ray running along +X at
		// z = 0.5. Both are crossed on their west face at x = 3-0.125-0.3 =
		// 2.575, so both intercepts are the same point and the tie is exact.
		// Both positions are in chunk (0, 0), so insertion order alone decides.
		north, south := mgl64.Vec3{3, 100, 0.3}, mgl64.Vec3{3, 100, 0.7}
		tests := []struct {
			name  string
			order []mgl64.Vec3
		}{
			{name: "north side spawned first", order: []mgl64.Vec3{north, south}},
			{name: "south side spawned first", order: []mgl64.Vec3{south, north}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				w := syncWorld(t)
				mustDo(t, w, func(tx *world.Tx) {
					projectileSpawnTargets(tx, test.order...)

					var hit trace.Result
					conf := ProjectileBehaviourConfig{}
					conf.Hit = func(_ *Ent, _ *world.Tx, target trace.Result) { hit = target }
					e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
					e.Behaviour().Tick(e, tx)

					assertVec3(t, "hit entity position", projectileHitEntity(t, hit).Position(), test.order[0])
					assertVec3(t, "hit position", hit.Position(), mgl64.Vec3{2.575, 100, 0.5})
				})
			})
		}
	})

	t.Run("symmetric across a chunk boundary", func(t *testing.T) {
		// The same construction rotated onto +Z with the ray on x = 0, the
		// boundary between chunk -1 and chunk 0. The target at x = -0.2 lives in
		// chunk (-1, 0) and the one at x = 0.2 in chunk (0, 0); both are crossed
		// on their north face at z = 3-0.125-0.3 = 2.575, so the tie is again
		// exact. entitiesWithin walks chunk X ascending, so the chunk -1 target
		// is yielded first even when it is spawned second: chunk order beats
		// insertion order.
		west, east := mgl64.Vec3{-0.2, 100, 3}, mgl64.Vec3{0.2, 100, 3}
		tests := []struct {
			name  string
			order []mgl64.Vec3
		}{
			{name: "chunk -1 target spawned first", order: []mgl64.Vec3{west, east}},
			{name: "chunk -1 target spawned second", order: []mgl64.Vec3{east, west}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				w := syncWorld(t)
				mustDo(t, w, func(tx *world.Tx) {
					projectileSpawnTargets(tx, test.order...)

					var hit trace.Result
					conf := ProjectileBehaviourConfig{}
					conf.Hit = func(_ *Ent, _ *world.Tx, target trace.Result) { hit = target }
					e, _, _ := projectileSpawn(tx, mgl64.Vec3{0, 100, 0.5}, mgl64.Vec3{0, 0, 4}, conf)
					e.Behaviour().Tick(e, tx)

					assertVec3(t, "hit entity position", projectileHitEntity(t, hit).Position(), west)
					assertVec3(t, "hit position", hit.Position(), mgl64.Vec3{0, 100, 2.575})
				})
			})
		}
	})
}

func TestProjectileEntityHitBeatsBlockHit(t *testing.T) {
	// trace.Perform traverses blocks first and clamps end to the block hit, then
	// starts the entity loop from dist = MaxFloat64 and overwrites hit for any
	// entity intercepting the clamped segment. An entity in front of a wall
	// therefore always displaces the wall, and an entity behind the wall is
	// excluded because the clamp shortened the segment before the entity loop
	// ever ran.
	tests := []struct {
		name       string
		wall       bool
		targetX    float64
		wantEntity bool
		wantHitPos mgl64.Vec3
	}{
		{
			name: "entity in front of the wall wins", wall: true, targetX: 2, wantEntity: true,
			// West face of the target's grown box: 2 - 0.425.
			wantHitPos: mgl64.Vec3{1.575, 100.5, 0.5},
		},
		{
			name: "entity behind the wall is out of the clamped range", wall: true, targetX: 5,
			// West face of the stone block at x = 3.
			wantHitPos: mgl64.Vec3{3, 100.5, 0.5},
		},
		{
			name: "the same entity is hit once the wall is gone", targetX: 5, wantEntity: true,
			wantHitPos: mgl64.Vec3{4.575, 100.5, 0.5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				if test.wall {
					tx.SetBlock(cube.Pos{3, 100, 0}, block.Stone{}, nil)
				}
				targetPos := mgl64.Vec3{test.targetX, 100.4, 0.5}
				target, _ := projectileTarget(tx, targetPos)

				var hit trace.Result
				conf := ProjectileBehaviourConfig{Hit: func(_ *Ent, _ *world.Tx, r trace.Result) { hit = r }}
				e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100.5, 0.5}, mgl64.Vec3{6, 0, 0}, conf)
				e.Behaviour().Tick(e, tx)

				if hit == nil {
					t.Fatal("Hit callback was not called, want a hit")
				}
				assertVec3(t, "hit position", hit.Position(), test.wantHitPos)
				switch r := hit.(type) {
				case trace.EntityResult:
					if !test.wantEntity {
						t.Fatalf("hit the entity at %v, want the block at {3 100 0}", r.Entity().Position())
					}
					if r.Entity().H() != target {
						t.Fatalf("hit the entity at %v, want the target at %v", r.Entity().Position(), targetPos)
					}
				case trace.BlockResult:
					if test.wantEntity {
						t.Fatalf("hit the block at %v, want the entity target", r.BlockPosition())
					}
					if got, want := r.BlockPosition(), (cube.Pos{3, 100, 0}); got != want {
						t.Fatalf("hit block position = %v, want %v", got, want)
					}
				default:
					t.Fatalf("hit result has type %T, want trace.EntityResult or trace.BlockResult", hit)
				}
			})
		})
	}
}

func TestProjectileEntitySearchBoxIsGrownByEight(t *testing.T) {
	// trace.Perform asks tx.EntitiesWithin for bb.Grow(8.0), deliberately far
	// wider than the segment it goes on to test candidates against. The grow is
	// observable through world.EntityType.Open, which entitiesWithin calls only
	// for handles whose position lies inside the queried box.
	//
	// For a ray from {0.5, 100, 0.5} along +X with velocity 4, bb spans z
	// [-0.625, 1.625]; grown by 8 it spans z [-8.625, 9.625]. A probe at z = 7 is
	// a candidate that can never intersect bb, and one at z = 10 is not a
	// candidate at all even though it shares chunk (0, 0) with the near probe.
	// Stage 2.1 decides the candidate set, so this radius is what changes.
	near, far := &projectileProbeType{}, &projectileProbeType{}
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 7}}.New(near, testMoveConfig{}))
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 10}}.New(far, testMoveConfig{}))
		near.opened, far.opened = 0, 0

		e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, ProjectileBehaviourConfig{})
		e.Behaviour().Tick(e, tx)

		if near.opened == 0 {
			t.Fatal("probe at z=7 was never opened, want it considered as a candidate inside bb.Grow(8)")
		}
		if far.opened != 0 {
			t.Fatalf("probe at z=10 was opened %d times, want 0: it lies outside bb.Grow(8)", far.opened)
		}
	})
}

func TestProjectilePickupCollectorOrderFollowsInsertion(t *testing.T) {
	// LATENT: tryPickup stops at the first collector that accepts, and that
	// first collector is decided by tx.EntitiesWithin, which today yields
	// chunk-major then insertion order. Both collectors below sit in chunk
	// (0, 0), so the one spawned first collects and the second is never asked.
	// Stage 2.1 changes which one that is.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)
		first := projectileCollectorAt(tx, mgl64.Vec3{1, 101, 0.5}, 1, true)
		second := projectileCollectorAt(tx, mgl64.Vec3{0.1, 101, 0.5}, 1, true)

		conf := ProjectileBehaviourConfig{
			CollisionPosition: cube.Pos{0, 100, 0},
			PickupItem:        item.NewStack(item.Arrow{}, 1),
		}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 101.02, 0.5}, mgl64.Vec3{}, conf)
		lt.ageCollided = 6

		e.Behaviour().Tick(e, tx)

		if len(first.collected) != 1 {
			t.Fatalf("first spawned collector received %d Collect calls, want 1", len(first.collected))
		}
		if len(second.collected) != 0 {
			t.Fatalf("second spawned collector received %d Collect calls, want 0: tryPickup returns after the first success", len(second.collected))
		}
		if !lt.close {
			t.Fatal("close after a successful pickup = false, want true")
		}
	})
}

// ---------------------------------------------------------------------------
// 2. Trajectory.
// ---------------------------------------------------------------------------

func TestProjectileTrajectorySequence(t *testing.T) {
	// Ten-tick goldens in open air with no target. They were produced by an
	// independent reimplementation of the documented force order
	// (applyVerticalForces with DragBeforeGravity, then applyHorizontalForces,
	// then pos += vel) and printed with %.17g, not by reading the production
	// code's output back.
	//
	// Arrow, hand check of the first two ticks from vel {1, 0.5, 0}:
	//   tick 1: vx = 1*0.99 = 0.99; vy = 0.5*0.99 - 0.05 = 0.445
	//           pos = {0.5+0.99, 100+0.445, 0.5} = {1.49, 100.445, 0.5}
	//   tick 2: vx = 0.99*0.99 = 0.9801; vy = 0.445*0.99 - 0.05 = 0.39055
	//           pos = {1.49+0.9801, 100.445+0.39055, 0.5} = {2.4701, 100.83555, 0.5}
	// Snowball, from vel {0, 0, 1.5}:
	//   tick 1: vz = 1.5*0.99 = 1.485; vy = 0*0.99 - 0.03 = -0.03
	//           pos = {0.5, 99.97, 0.5+1.485} = {0.5, 99.97, 1.985}
	//   tick 2: vz = 1.485*0.99 = 1.47015; vy = -0.03*0.99 - 0.03 = -0.0597
	//           pos = {0.5, 99.9103, 3.45515}
	// The yaw goldens are exact: the arrow moves on +X only, so
	// Atan2(vx, 0) = 90 degrees, and the snowball moves on +Z only, so
	// Atan2(0, vz) = 0. The pitch goldens follow Atan2(vy, Hypot(vx, vz)); the
	// arrow's first is Atan2(0.445, 0.99), an angle whose tangent is 0.4495 and
	// which is therefore just over 24 degrees.
	tests := []struct {
		name          string
		gravity, drag float64
		vel           mgl64.Vec3
		want          []projectileStep
	}{
		{
			name: "arrow", gravity: 0.05, drag: 0.01, vel: mgl64.Vec3{1, 0.5, 0},
			want: []projectileStep{
				{pos: mgl64.Vec3{1.49, 100.44499999999999, 0.5}, vel: mgl64.Vec3{0.98999999999999999, 0.44500000000000001, 0}, rot: cube.Rotation{90, 24.203676518902373}},
				{pos: mgl64.Vec3{2.4701, 100.83555, 0.5}, vel: mgl64.Vec3{0.98009999999999997, 0.39055000000000001, 0}, rot: cube.Rotation{90, 21.726280418354783}},
				{pos: mgl64.Vec3{3.4403989999999998, 101.1721945, 0.5}, vel: mgl64.Vec3{0.97029899999999991, 0.33664450000000001, 0}, rot: cube.Rotation{90, 19.134178327560932}},
				{pos: mgl64.Vec3{4.4009950099999999, 101.455472555, 0.5}, vel: mgl64.Vec3{0.96059600999999994, 0.28327805500000003, 0}, rot: cube.Rotation{90, 16.430694458123202}},
				{pos: mgl64.Vec3{5.3519850598999996, 101.68591782945001, 0.5}, vel: mgl64.Vec3{0.95099004989999991, 0.23044527445000001, 0}, rot: cube.Rotation{90, 13.621431310311545}},
				{pos: mgl64.Vec3{6.2934652093009991, 101.8640586511555, 0.5}, vel: mgl64.Vec3{0.94148014940099989, 0.1781408217055, 0}, rot: cube.Rotation{90, 10.714471754120671}},
				{pos: mgl64.Vec3{7.2255305572079891, 101.99041806464395, 0.5}, vel: mgl64.Vec3{0.93206534790698992, 0.12635941348844498, 0}, rot: cube.Rotation{90, 7.7204779954967524}},
				{pos: mgl64.Vec3{8.1482752516359085, 102.06551388399751, 0.5}, vel: mgl64.Vec3{0.92274469442791995, 0.075095819353560536, 0}, rot: cube.Rotation{90, 4.6526541282697593}},
				{pos: mgl64.Vec3{9.0617924991195498, 102.08985874515754, 0.5}, vel: mgl64.Vec3{0.91351724748364072, 0.024344861160024933, 0}, rot: cube.Rotation{90, 1.5265477823380205}},
				{pos: mgl64.Vec3{9.9661745741283543, 102.06396015770596, 0.5}, vel: mgl64.Vec3{0.9043820750088043, -0.025898587451575319, 0}, rot: cube.Rotation{90, -1.6403181474378778}},
			},
		},
		{
			name: "snowball", gravity: 0.03, drag: 0.01, vel: mgl64.Vec3{0, 0, 1.5},
			want: []projectileStep{
				{pos: mgl64.Vec3{0.5, 99.969999999999999, 1.9849999999999999}, vel: mgl64.Vec3{0, -0.029999999999999999, 1.4849999999999999}, rot: cube.Rotation{0, -1.1573330681295171}},
				{pos: mgl64.Vec3{0.5, 99.910299999999992, 3.4551499999999997}, vel: mgl64.Vec3{0, -0.059699999999999996, 1.4701499999999998}, rot: cube.Rotation{0, -2.3253951691285497}},
				{pos: mgl64.Vec3{0.5, 99.821196999999998, 4.9105984999999999}, vel: mgl64.Vec3{0, -0.089102999999999988, 1.4554484999999999}, rot: cube.Rotation{0, -3.5032927265628806}},
				{pos: mgl64.Vec3{0.5, 99.702985029999994, 6.3514925149999995}, vel: mgl64.Vec3{0, -0.11821196999999999, 1.4408940149999998}, rot: cube.Rotation{0, -4.6900828500715859}},
				{pos: mgl64.Vec3{0.5, 99.555955179699993, 7.777977589849999}, vel: mgl64.Vec3{0, -0.14702985029999999, 1.4264850748499998}, rot: cube.Rotation{0, -5.8847769831280461}},
				{pos: mgl64.Vec3{0.5, 99.380395627902999, 9.1901978139514995}, vel: mgl64.Vec3{0, -0.17555955179699997, 1.4122202241014998}, rot: cube.Rotation{0, -7.0863451103019282}},
				{pos: mgl64.Vec3{0.5, 99.176591671623967, 10.588295835811984}, vel: mgl64.Vec3{0, -0.20380395627902997, 1.3980980218604848}, rot: cube.Rotation{0, -8.2937204890538041}},
				{pos: mgl64.Vec3{0.5, 98.944825754907725, 11.972412877453863}, vel: mgl64.Vec3{0, -0.23176591671623967, 1.38411704164188}, rot: cube.Rotation{0, -9.5058048591886468}},
				{pos: mgl64.Vec3{0.5, 98.685377497358644, 13.342688748679324}, vel: mgl64.Vec3{0, -0.2594482575490773, 1.3702758712254612}, rot: cube.Rotation{0, -10.721474071829521}},
				{pos: mgl64.Vec3{0.5, 98.39852372238505, 14.699261861192531}, vel: mgl64.Vec3{0, -0.28685377497358655, 1.3565731125132066}, rot: cube.Rotation{0, -11.939584069561237}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				conf := ProjectileBehaviourConfig{Gravity: test.gravity, Drag: test.drag}
				e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, test.vel, conf)
				for i, step := range test.want {
					if m := e.Behaviour().Tick(e, tx); m == nil {
						t.Fatalf("tick %d returned a nil Movement, want a movement in open air", i+1)
					}
					assertVec3(t, fmt.Sprintf("tick %d position", i+1), e.Position(), step.pos)
					assertVec3(t, fmt.Sprintf("tick %d velocity", i+1), e.Velocity(), step.vel)
					assertProjectileRotation(t, fmt.Sprintf("tick %d rotation", i+1), e.Rotation(), step.rot)
				}
			})
		})
	}
}

func TestProjectileRotationFromVelocity(t *testing.T) {
	// With Gravity and Drag both zero the forces are the identity, so the
	// rotation is read straight off the velocity handed in. Every expectation
	// here is an exact multiple of 45 degrees.
	tests := []struct {
		name string
		vel  mgl64.Vec3
		want cube.Rotation
	}{
		{name: "purely +X", vel: mgl64.Vec3{1, 0, 0}, want: cube.Rotation{90, 0}},
		{name: "purely -X", vel: mgl64.Vec3{-1, 0, 0}, want: cube.Rotation{-90, 0}},
		{name: "purely +Z", vel: mgl64.Vec3{0, 0, 1}, want: cube.Rotation{0, 0}},
		{name: "purely -Z", vel: mgl64.Vec3{0, 0, -1}, want: cube.Rotation{180, 0}},
		{name: "purely upward", vel: mgl64.Vec3{0, 1, 0}, want: cube.Rotation{0, 90}},
		{name: "purely downward", vel: mgl64.Vec3{0, -1, 0}, want: cube.Rotation{0, -90}},
		{name: "diagonal between +X and +Z", vel: mgl64.Vec3{1, 0, 1}, want: cube.Rotation{45, 0}},
		{name: "diagonal between up and +Z", vel: mgl64.Vec3{0, 1, 1}, want: cube.Rotation{0, 45}},
		// A zero velocity performs no trace at all, and Atan2(0, 0) is 0, so the
		// rotation is reset rather than left alone.
		{name: "zero velocity", vel: mgl64.Vec3{}, want: cube.Rotation{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, test.vel, ProjectileBehaviourConfig{})
				e.data.Rot = cube.Rotation{123, -45}
				e.Behaviour().Tick(e, tx)
				assertProjectileRotation(t, "rotation", e.Rotation(), test.want)
			})
		})
	}
}

func TestProjectileRotationUsesPostForceVelocity(t *testing.T) {
	// Gravity turns a velocity of {0, 1.05, 1} into {0, 1, 1} exactly: 1.05-0.05
	// rounds to 1.0 in float64. The post-force pitch is therefore exactly 45
	// degrees, while the pre-force pitch would be Atan2(1.05, 1), about 46.4.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		conf := ProjectileBehaviourConfig{Gravity: 0.05}
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{0, 1.05, 1}, conf)
		e.Behaviour().Tick(e, tx)

		assertVec3(t, "velocity", e.Velocity(), mgl64.Vec3{0, 1, 1})
		assertProjectileRotation(t, "rotation", e.Rotation(), cube.Rotation{0, 45})
	})
}

func TestProjectileRayTracesInsteadOfSweeping(t *testing.T) {
	// tickMovement uses pos.Add(vel) and a ray trace; it never calls
	// CheckCollision. A block that a swept bounding box would collide with, but
	// that the centre ray misses, is passed straight through with the full move
	// and the velocity intact.
	//
	// The projectile runs along z = 0.95 while the block occupies z [1, 2]. Its
	// own box reaches z = 1.075, so an AABB sweep would clamp X; the ray never
	// enters the block's Z range, and TraverseBlocks never even visits it.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 100, 1}, block.Stone{}, nil)
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100.5, 0.95}, mgl64.Vec3{1, 0, 0}, ProjectileBehaviourConfig{})
		e.Behaviour().Tick(e, tx)

		assertVec3(t, "position", e.Position(), mgl64.Vec3{1.5, 100.5, 0.95})
		assertVec3(t, "velocity", e.Velocity(), mgl64.Vec3{1, 0, 0})
	})
}

func TestProjectileZeroVelocitySkipsTheTrace(t *testing.T) {
	// trace.TraverseBlocks panics on a zero direction vector, so tickMovement
	// guards the whole trace with FloatEqual(end.Sub(pos).LenSqr(), 0). A
	// motionless projectile must therefore tick without touching the tracer.
	// mgl64.FloatEqual squares its epsilon against a zero operand, so the guard
	// fires for any squared length below 1e-20.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		start := mgl64.Vec3{0.5, 100, 0.5}
		e, _, _ := projectileSpawn(tx, start, mgl64.Vec3{}, ProjectileBehaviourConfig{})
		for i := range 3 {
			e.Tick(tx, int64(i+1))
		}
		assertVec3(t, "position", e.Position(), start)
		assertVec3(t, "velocity", e.Velocity(), mgl64.Vec3{})
		// Ent.Tick adds a twentieth of a second at the end of every tick.
		if got, want := e.Age(), 3*time.Second/20; got != want {
			t.Fatalf("Age() after 3 ticks = %v, want %v", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// 3. Block hit.
// ---------------------------------------------------------------------------

func TestProjectileBlockHitFaceAndPosition(t *testing.T) {
	// One stone block at {0, 100, 0}, approached along each axis from both
	// directions with Gravity and Drag zero so the ray is a straight line and
	// every intercept lands on a half-way point.
	tests := []struct {
		name      string
		start     mgl64.Vec3
		vel       mgl64.Vec3
		wantFace  cube.Face
		wantPoint mgl64.Vec3
	}{
		{name: "from the west", start: mgl64.Vec3{-2, 100.5, 0.5}, vel: mgl64.Vec3{4, 0, 0}, wantFace: cube.FaceWest, wantPoint: mgl64.Vec3{0, 100.5, 0.5}},
		{name: "from the east", start: mgl64.Vec3{3, 100.5, 0.5}, vel: mgl64.Vec3{-4, 0, 0}, wantFace: cube.FaceEast, wantPoint: mgl64.Vec3{1, 100.5, 0.5}},
		{name: "from the north", start: mgl64.Vec3{0.5, 100.5, -2}, vel: mgl64.Vec3{0, 0, 4}, wantFace: cube.FaceNorth, wantPoint: mgl64.Vec3{0.5, 100.5, 0}},
		{name: "from the south", start: mgl64.Vec3{0.5, 100.5, 3}, vel: mgl64.Vec3{0, 0, -4}, wantFace: cube.FaceSouth, wantPoint: mgl64.Vec3{0.5, 100.5, 1}},
		{name: "from above", start: mgl64.Vec3{0.5, 103, 0.5}, vel: mgl64.Vec3{0, -3, 0}, wantFace: cube.FaceUp, wantPoint: mgl64.Vec3{0.5, 101, 0.5}},
		{name: "from below", start: mgl64.Vec3{0.5, 98, 0.5}, vel: mgl64.Vec3{0, 3, 0}, wantFace: cube.FaceDown, wantPoint: mgl64.Vec3{0.5, 100, 0.5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				var hit trace.Result
				conf := ProjectileBehaviourConfig{Hit: func(_ *Ent, _ *world.Tx, r trace.Result) { hit = r }}
				e, _, _ := projectileSpawn(tx, test.start, test.vel, conf)
				e.Behaviour().Tick(e, tx)

				r, ok := hit.(trace.BlockResult)
				if !ok {
					t.Fatalf("hit result has type %T, want trace.BlockResult", hit)
				}
				if got, want := r.BlockPosition(), (cube.Pos{0, 100, 0}); got != want {
					t.Fatalf("BlockPosition() = %v, want %v", got, want)
				}
				if got := r.Face(); got != test.wantFace {
					t.Fatalf("Face() = %v, want %v", got, test.wantFace)
				}
				assertVec3(t, "hit position", r.Position(), test.wantPoint)
				// The projectile is moved to the intercept, not through it.
				assertVec3(t, "position", e.Position(), test.wantPoint)
			})
		})
	}
}

func TestProjectileNonSurvivingBlockHitClosesOnTheFollowingTick(t *testing.T) {
	// SurviveBlockCollision false only raises the close flag. Tick returns the
	// movement first and the entity is closed at the top of the next tick, so a
	// snowball is still in the world for the tick in which it broke.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)
		e, lt, handle := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, ProjectileBehaviourConfig{})

		if m := e.Behaviour().Tick(e, tx); m == nil {
			t.Fatal("the tick that hit the block returned a nil Movement, want the movement up to the block")
		}
		if !lt.close {
			t.Fatal("close after hitting a block = false, want true")
		}
		if handle.Closed() {
			t.Fatal("the entity was closed on the tick it hit the block, want it closed on the next tick")
		}

		if m := e.Behaviour().Tick(e, tx); m != nil {
			t.Fatalf("the closing tick reported a movement to %v, want a nil Movement", m.Position())
		}
		if !handle.Closed() {
			t.Fatal("the entity was not closed on the tick after hitting a block")
		}
	})
}

func TestProjectileSurvivingBlockHitDeflection(t *testing.T) {
	// hitBlockSurviving is reached through tickMovement's BlockResult branch,
	// which first restores the Y velocity with (vel[1]+Gravity)/(1-Drag), then
	// scales the whole vector by BlockCollisionVelocityMultiplier and negates
	// the axis of the face that was hit.
	//
	// PINNED BUG: the comment at projectile.go:347 says the gravity is undone,
	// but dividing by (1-Drag) also undoes the vertical drag, so Y is restored
	// to its pre-force value while X and Z keep the drag they were just charged.
	// The "vertical drag is undone as well" case below is the proof: a pre-force
	// Y of 0.2 comes back as exactly 0.2 rather than as 0.198.
	//
	// Geometry: the projectile starts at x = -1.98 with a post-force X velocity
	// of 3.96, so the west face of the block at {0, 100, 0} is crossed at exactly
	// half the ray (1.98/3.96 = 0.5 in float64) and the intercept Y is the start
	// Y plus half the post-force Y velocity.
	tests := []struct {
		name       string
		start, vel mgl64.Vec3
		multiplier float64
		wantPos    mgl64.Vec3
		wantVel    mgl64.Vec3
		wantStuck  bool
	}{
		{
			// Gravity undone: a pre-force Y of 0 comes back as 0 after the
			// division, so the deflection is purely horizontal.
			name: "gravity undone, multiplier 0.5", start: mgl64.Vec3{-1.98, 100.5, 0.5}, vel: mgl64.Vec3{4, 0, 0}, multiplier: 0.5,
			wantPos: mgl64.Vec3{0, 100.475, 0.5}, wantVel: mgl64.Vec3{-1.98, 0, 0},
		},
		{
			// The default multiplier of 0 zeroes the velocity outright, which
			// then always satisfies the stuck test below.
			name: "multiplier 0 stops the projectile dead", start: mgl64.Vec3{-1.98, 100.5, 0.5}, vel: mgl64.Vec3{4, 0, 0},
			wantPos: mgl64.Vec3{0, 100.475, 0.5}, wantVel: mgl64.Vec3{}, wantStuck: true,
		},
		{
			// Vertical drag undone as well: post-force Y is 0.2*0.99-0.05 =
			// 0.148, and (0.148+0.05)/0.99 restores exactly 0.2, halved to 0.1.
			name: "vertical drag undone as well", start: mgl64.Vec3{-1.98, 100.5, 0.5}, vel: mgl64.Vec3{4, 0.2, 0}, multiplier: 0.5,
			wantPos: mgl64.Vec3{0, 100.574, 0.5}, wantVel: mgl64.Vec3{-1.98, 0.1, 0},
		},
		{
			// Only the collided axis flips: hitting the top face negates Y and
			// leaves the X drift running in the same direction. The intercept X
			// is 0.5 + 0.396*(2/3.02) = 0.5 + 198/755.
			name: "only the collided axis is negated", start: mgl64.Vec3{0.5, 103, 0.5}, vel: mgl64.Vec3{0.4, -3, 0}, multiplier: 0.5,
			wantPos: mgl64.Vec3{0.7622516556291391, 101, 0.5}, wantVel: mgl64.Vec3{0.19800000000000001, 1.5, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				conf := ProjectileBehaviourConfig{
					Gravity: 0.05, Drag: 0.01,
					SurviveBlockCollision:            true,
					BlockCollisionVelocityMultiplier: test.multiplier,
				}
				e, lt, handle := projectileSpawn(tx, test.start, test.vel, conf)
				e.Behaviour().Tick(e, tx)

				assertVec3(t, "position", e.Position(), test.wantPos)
				assertVec3(t, "velocity", e.Velocity(), test.wantVel)
				if got := lt.collided; got != test.wantStuck {
					t.Fatalf("collided = %v, want %v", got, test.wantStuck)
				}
				if test.wantStuck {
					if got, want := lt.collisionPos, (cube.Pos{0, 100, 0}); got != want {
						t.Fatalf("collisionPos = %v, want %v", got, want)
					}
				}
				if lt.close {
					t.Fatal("close after a surviving block collision = true, want false")
				}
				if handle.Closed() {
					t.Fatal("a projectile that survives block collision was closed")
				}
			})
		})
	}
}

func TestProjectileHitBlockSurvivingStuckThreshold(t *testing.T) {
	// hitBlockSurviving builds its epsilon as Sqrt(0.1*(1-multiplier)) and hands
	// it to mgl64.FloatEqualThreshold against 0. FloatEqualThreshold squares the
	// threshold whenever an operand is zero, so the square root is a deliberate
	// compensation and the effective cutoff on the speed is 0.1*(1-multiplier),
	// with the comparison strict.
	//
	// The multiplier-0 rows are the discriminating ones. Without the square root
	// the cutoff would be 0.01 and 0.09 would not stick; without mgl64's
	// squaring it would be 0.3162 and 0.11 would stick.
	tests := []struct {
		name       string
		multiplier float64
		speed      float64
		want       bool
	}{
		{name: "multiplier 0, below the 0.1 cutoff", speed: 0.09, want: true},
		{name: "multiplier 0, above the 0.1 cutoff", speed: 0.11},
		{name: "multiplier 0.5, below the 0.05 cutoff", multiplier: 0.5, speed: 0.049, want: true},
		{name: "multiplier 0.5, above the 0.05 cutoff", multiplier: 0.5, speed: 0.051},
		{name: "multiplier 0.9, below the 0.01 cutoff", multiplier: 0.9, speed: 0.009, want: true},
		{name: "multiplier 0.9, above the 0.01 cutoff", multiplier: 0.9, speed: 0.011},
		// A multiplier of 1 leaves a cutoff of 0, so only a speed that compares
		// equal to zero sticks, through FloatEqualThreshold's a == b shortcut.
		{name: "multiplier 1, exactly zero", multiplier: 1, speed: 0, want: true},
		{name: "multiplier 1, a small but representable speed", multiplier: 1, speed: 1e-8},
		// mgl64.Vec3.Len squares each component before taking the root, so a
		// denormal speed underflows to zero and sticks even at a cutoff of 0.
		{name: "multiplier 1, a denormal speed underflows to zero", multiplier: 1, speed: math.SmallestNonzeroFloat64, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				conf := ProjectileBehaviourConfig{
					SurviveBlockCollision:            true,
					BlockCollisionVelocityMultiplier: test.multiplier,
				}
				e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{}, conf)
				e.SetVelocity(mgl64.Vec3{test.speed, 0, 0})

				m := &Movement{v: tx.Viewers(e.Position()), e: e, pos: e.Position()}
				lt.hitBlockSurviving(e, trace.BlockResult{}, m, tx)

				if got := lt.collided; got != test.want {
					t.Fatalf("collided at speed %.17g with multiplier %v = %v, want %v", test.speed, test.multiplier, got, test.want)
				}
				if test.want {
					// Sticking zeroes the velocity outright.
					assertVec3(t, "velocity", e.Velocity(), mgl64.Vec3{})
				} else if got := e.Velocity()[0]; got != test.speed {
					t.Fatalf("velocity X after failing to stick = %.17g, want %.17g unchanged", got, test.speed)
				}
			})
		})
	}
}

func TestProjectileStuckNotifiesViewersInOrder(t *testing.T) {
	// Sticking sends exactly three updates per viewer, in this order: the
	// teleport to the intercept, the 350ms arrow shake, then the state refresh.
	w := syncWorld(t)
	v := &projectileViewer{}
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, v)
		tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

		conf := ProjectileBehaviourConfig{Gravity: 0.05, Drag: 0.01, SurviveBlockCollision: true}
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{-1.98, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
		v.reset()
		e.Behaviour().Tick(e, tx)

		want := []string{"teleport", "action", "state"}
		if !slices.Equal(v.calls, want) {
			t.Fatalf("viewer calls = %v, want %v", v.calls, want)
		}
		if len(v.teleports) != 1 {
			t.Fatalf("ViewEntityTeleport calls = %d, want 1", len(v.teleports))
		}
		assertVec3(t, "teleport position", v.teleports[0], mgl64.Vec3{0, 100.475, 0.5})
		if len(v.actions) != 1 {
			t.Fatalf("ViewEntityAction calls = %d, want 1", len(v.actions))
		}
		shake, ok := v.actions[0].(ArrowShakeAction)
		if !ok {
			t.Fatalf("action has type %T, want ArrowShakeAction", v.actions[0])
		}
		if got, want := shake.Duration, 350*time.Millisecond; got != want {
			t.Fatalf("ArrowShakeAction.Duration = %v, want %v", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// 4. Attached state.
// ---------------------------------------------------------------------------

func TestProjectileAttachedStaysWhileTheBlockIntersects(t *testing.T) {
	// tickAttached keeps the projectile attached while its own box intersects
	// any box of the block at collisionPos grown by 0.05, and does not move it.
	// The 0.05 slack is what holds an arrow whose box only touches the surface.
	tests := []struct {
		name         string
		pos          mgl64.Vec3
		wantAttached bool
	}{
		{name: "box overlapping the block", pos: mgl64.Vec3{0.5, 100.9, 0.5}, wantAttached: true},
		{name: "box inside the 0.05 slack above the block", pos: mgl64.Vec3{0.5, 101.02, 0.5}, wantAttached: true},
		{name: "box clear of the slack", pos: mgl64.Vec3{0.5, 101.2, 0.5}},
		{name: "box beside the block", pos: mgl64.Vec3{2, 100.5, 0.5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				conf := ProjectileBehaviourConfig{Gravity: 0.05, Drag: 0.01, CollisionPosition: cube.Pos{0, 100, 0}}
				e, lt, _ := projectileSpawn(tx, test.pos, mgl64.Vec3{}, conf)
				if !lt.collided {
					t.Fatal("collided after configuring a CollisionPosition = false, want true")
				}

				m := e.Behaviour().Tick(e, tx)
				if test.wantAttached {
					if m != nil {
						t.Fatalf("an attached projectile reported a movement to %v, want a nil Movement", m.Position())
					}
					assertVec3(t, "position", e.Position(), test.pos)
					if got := lt.ageCollided; got != 1 {
						t.Fatalf("ageCollided after one attached tick = %d, want 1", got)
					}
					return
				}
				if m == nil {
					t.Fatal("a detached projectile returned a nil Movement, want it to resume moving")
				}
				if lt.collided {
					t.Fatal("collided after detaching = true, want false")
				}
				if got, want := lt.collisionPos, (cube.Pos{}); got != want {
					t.Fatalf("collisionPos after detaching = %v, want %v", got, want)
				}
				if got := lt.ageCollided; got != 0 {
					t.Fatalf("ageCollided after detaching = %d, want 0", got)
				}
			})
		})
	}
}

func TestProjectileAttachedResumesWhenTheBlockIsBroken(t *testing.T) {
	// Breaking the block the projectile is stuck in leaves it with no box to
	// intersect, so the next tick falls through to normal movement.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		pos := cube.Pos{0, 100, 0}
		tx.SetBlock(pos, block.Stone{}, nil)

		conf := ProjectileBehaviourConfig{Gravity: 0.05, Drag: 0.01, CollisionPosition: pos}
		start := mgl64.Vec3{0.5, 101.02, 0.5}
		e, lt, _ := projectileSpawn(tx, start, mgl64.Vec3{}, conf)

		if m := e.Behaviour().Tick(e, tx); m != nil {
			t.Fatalf("the attached tick reported a movement to %v, want a nil Movement", m.Position())
		}
		assertVec3(t, "position while attached", e.Position(), start)

		tx.SetBlock(pos, block.Air{}, nil)
		if m := e.Behaviour().Tick(e, tx); m == nil {
			t.Fatal("the tick after breaking the block returned a nil Movement, want it to resume falling")
		}
		if lt.collided {
			t.Fatal("collided after the block was broken = true, want false")
		}
		// A stuck projectile has no velocity, so the first free tick is pure
		// gravity: vel = {0, -0.05, 0}.
		assertVec3(t, "velocity", e.Velocity(), mgl64.Vec3{0, -0.05, 0})
		assertVec3(t, "position", e.Position(), mgl64.Vec3{0.5, 100.97, 0.5})
	})
}

func TestProjectileAttachedClosesAfterTheAgeLimit(t *testing.T) {
	// ageCollided is incremented inside tickAttached and only then compared, so
	// the close flag is raised on the tick that carries it past 1200, and the
	// entity itself is closed on the tick after that.
	tests := []struct {
		name      string
		age       int
		wantAge   int
		wantClose bool
	}{
		{name: "one tick short of the limit", age: 1199, wantAge: 1200},
		{name: "the tick that passes the limit", age: 1200, wantAge: 1201, wantClose: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				conf := ProjectileBehaviourConfig{CollisionPosition: cube.Pos{0, 100, 0}, DisablePickup: true}
				e, lt, handle := projectileSpawn(tx, mgl64.Vec3{0.5, 101.02, 0.5}, mgl64.Vec3{}, conf)
				lt.ageCollided = test.age

				e.Behaviour().Tick(e, tx)
				if got := lt.ageCollided; got != test.wantAge {
					t.Fatalf("ageCollided = %d, want %d", got, test.wantAge)
				}
				if got := lt.close; got != test.wantClose {
					t.Fatalf("close = %v, want %v", got, test.wantClose)
				}
				if handle.Closed() {
					t.Fatal("the entity was closed on the tick that raised the flag, want it closed on the next tick")
				}
				if !test.wantClose {
					return
				}
				e.Behaviour().Tick(e, tx)
				if !handle.Closed() {
					t.Fatal("the entity was not closed on the tick after the age limit was passed")
				}
			})
		})
	}
}

func TestProjectileAttachedPickupWindow(t *testing.T) {
	// A pickup is only attempted once ageCollided is strictly greater than 5 and
	// only while DisablePickup is false. A collector that cannot hold the item
	// leaves the projectile where it is.
	tests := []struct {
		name          string
		age           int
		disablePickup bool
		collected     int
		accepts       bool
		wantCollects  int
		wantClose     bool
	}{
		{name: "at the boundary, no attempt", age: 5, collected: 1, accepts: true},
		{name: "one tick past the boundary", age: 6, collected: 1, accepts: true, wantCollects: 1, wantClose: true},
		{name: "pickup disabled", age: 6, disablePickup: true, collected: 1, accepts: true},
		{name: "collector refuses", age: 6, collected: 1, wantCollects: 1},
		{name: "collector accepts but takes nothing", age: 6, accepts: true, wantCollects: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			v := &projectileViewer{}
			mustDo(t, w, func(tx *world.Tx) {
				addViewer(tx, v)
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)
				collector := projectileCollectorAt(tx, mgl64.Vec3{1, 101, 0.5}, test.collected, test.accepts)

				pickup := item.NewStack(item.Arrow{}, 1)
				conf := ProjectileBehaviourConfig{
					CollisionPosition: cube.Pos{0, 100, 0},
					DisablePickup:     test.disablePickup,
					PickupItem:        pickup,
				}
				e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 101.02, 0.5}, mgl64.Vec3{}, conf)
				lt.ageCollided = test.age
				v.reset()

				e.Behaviour().Tick(e, tx)

				if got := len(collector.collected); got != test.wantCollects {
					t.Fatalf("Collect calls = %d, want %d", got, test.wantCollects)
				}
				if test.wantCollects > 0 {
					if got := collector.collected[0]; !got.Equal(pickup) {
						t.Fatalf("Collect stack = %v, want the configured PickupItem %v", got, pickup)
					}
				}
				if got := lt.close; got != test.wantClose {
					t.Fatalf("close = %v, want %v", got, test.wantClose)
				}
				if test.wantClose {
					if !slices.Contains(v.calls, "action") {
						t.Fatalf("viewer calls after a pickup = %v, want a PickedUpAction", v.calls)
					}
					if _, ok := v.actions[0].(PickedUpAction); !ok {
						t.Fatalf("action has type %T, want PickedUpAction", v.actions[0])
					}
				} else if len(v.actions) != 0 {
					t.Fatalf("viewer received %d actions, want none", len(v.actions))
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// 5. The ignores filter.
// ---------------------------------------------------------------------------

func TestProjectileIgnoresEveryClause(t *testing.T) {
	// The filter is exercised directly so that each clause can be isolated: a
	// full tick would only report which entity survived all five at once.
	//
	// The projectile itself cannot be isolated from the damageable clause:
	// ProjectileBehaviour has no Hurt, so a projectile is rejected by both the
	// itself and the !damageable clause at the same time. Its rejection is
	// pinned all the same.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, ProjectileBehaviourConfig{})

		plainHandle, _ := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})
		plain, _ := plainHandle.Entity(tx)
		spectator := projectileLivingAt(tx, mgl64.Vec3{3, 100, 0.5}, world.GameModeSpectator)
		survival := projectileLivingAt(tx, mgl64.Vec3{4, 100, 0.5}, world.GameModeSurvival)
		inertHandle := world.EntitySpawnOpts{Position: mgl64.Vec3{5, 100, 0.5}}.New(testMovingEntType{}, testMoveConfig{})
		inert := tx.AddEntity(inertHandle)
		ownerHandle, _ := projectileTarget(tx, mgl64.Vec3{6, 100, 0.5})
		owner, _ := ownerHandle.Entity(tx)
		collidedHandle, _ := projectileTarget(tx, mgl64.Vec3{7, 100, 0.5})
		collided, _ := collidedHandle.Entity(tx)

		lt.conf.Owner = ownerHandle
		lt.collidedEntities = []*world.EntityHandle{collidedHandle}
		e.data.Age = 0

		in := []world.Entity{plain, spectator, survival, inert, owner, collided, e}
		got := slices.Collect(lt.ignores(e)(slices.Values(in)))

		// Only the plain damageable target at x=2 and the survival-mode Living at
		// x=4 survive all five clauses, and the filter preserves the input order.
		want := []mgl64.Vec3{{2, 100, 0.5}, {4, 100, 0.5}}
		if gotPositions := projectilePositions(got); !slices.Equal(gotPositions, want) {
			t.Fatalf("filter kept entities at %v, want exactly %v", gotPositions, want)
		}
		if got[0].H() != plainHandle || got[1].H() != survival.H() {
			t.Fatal("the kept entities are at the right positions but are not the plain target and the survival-mode Living")
		}
	})
}

func TestProjectileIgnoresOwnerGraceWindow(t *testing.T) {
	// The owner clause is e.data.Age < time.Second/4. Ent.Tick adds
	// time.Second/20 per tick, so the owner is skipped for ticks 0 through 4 and
	// becomes a valid target from tick 5, where the age reaches exactly 250ms
	// and the strict comparison stops holding.
	tests := []struct {
		name       string
		age        time.Duration
		wantIgnore bool
	}{
		{name: "tick 0", age: 0, wantIgnore: true},
		{name: "tick 4", age: 4 * time.Second / 20, wantIgnore: true},
		{name: "tick 5, exactly 250ms", age: 5 * time.Second / 20},
		{name: "tick 6", age: 6 * time.Second / 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				ownerHandle, _ := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})
				owner, _ := ownerHandle.Entity(tx)

				conf := ProjectileBehaviourConfig{Owner: ownerHandle}
				e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, conf)
				e.data.Age = test.age

				got := slices.Collect(lt.ignores(e)(slices.Values([]world.Entity{owner})))
				if ignored := len(got) == 0; ignored != test.wantIgnore {
					t.Fatalf("owner ignored at age %v = %v, want %v", test.age, ignored, test.wantIgnore)
				}
			})
		})
	}
}

func TestProjectileIgnoresStopsTheSourceOnEarlyReturn(t *testing.T) {
	// The filter forwards a false yield back to the sequence it wraps, so a
	// consumer that stops early does not drag the whole candidate set through.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, ProjectileBehaviourConfig{})

		var entities []world.Entity
		for _, x := range []float64{2, 3, 4} {
			h, _ := projectileTarget(tx, mgl64.Vec3{x, 100, 0.5})
			ent, _ := h.Entity(tx)
			entities = append(entities, ent)
		}

		pulled := 0
		src := iter.Seq[world.Entity](func(yield func(world.Entity) bool) {
			for _, ent := range entities {
				pulled++
				if !yield(ent) {
					return
				}
			}
		})
		for range lt.ignores(e)(src) {
			break
		}
		if pulled != 1 {
			t.Fatalf("the source was pulled %d times after the consumer stopped at the first entity, want 1", pulled)
		}
	})
}

// ---------------------------------------------------------------------------
// 6. Piercing.
// ---------------------------------------------------------------------------

func TestProjectilePiercing(t *testing.T) {
	// A projectile passes through PiercingLevel entities and damages
	// PiercingLevel+1 in total: close is raised once len(collidedEntities)
	// exceeds PiercingLevel. Each hit entity joins collidedEntities, so the
	// ignores filter skips it on the following tick and the next target ahead is
	// found instead.
	//
	// The DamageableEntity guard around that append is unreachable in practice:
	// the ignores filter has already rejected everything that is not damageable,
	// using the same predicate.
	tests := []struct {
		name     string
		piercing int
		wantHurt []int
		wantVel  mgl64.Vec3
	}{
		{name: "no piercing stops at the first target", piercing: 0, wantHurt: []int{1, 0, 0}, wantVel: mgl64.Vec3{}},
		{name: "piercing 1 passes through one", piercing: 1, wantHurt: []int{1, 1, 0}, wantVel: mgl64.Vec3{4, 0, 0}},
		{name: "piercing 2 passes through two", piercing: 2, wantHurt: []int{1, 1, 1}, wantVel: mgl64.Vec3{4, 0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				var targets []*projectileTargetBehaviour
				for _, x := range []float64{2, 3, 4} {
					_, b := projectileTarget(tx, mgl64.Vec3{x, 100, 0.5})
					targets = append(targets, b)
				}

				conf := ProjectileBehaviourConfig{PiercingLevel: test.piercing}
				e, lt, handle := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)

				// One tick per target, stopping as soon as close is raised.
				for range len(test.wantHurt) {
					if e.Behaviour().Tick(e, tx); lt.close {
						break
					}
				}
				if !lt.close {
					t.Fatal("close was never raised, want it raised once the piercing level was exceeded")
				}
				// The velocity survives until the tick that actually closes.
				assertVec3(t, "velocity", e.Velocity(), test.wantVel)
				e.Behaviour().Tick(e, tx)

				for i, want := range test.wantHurt {
					if got := len(targets[i].hurts); got != want {
						t.Fatalf("target %d was hurt %d times, want %d", i, got, want)
					}
				}
				if got, want := len(lt.collidedEntities), test.piercing+1; got != want {
					t.Fatalf("collidedEntities = %d, want %d", got, want)
				}
				if !handle.Closed() {
					t.Fatal("the projectile was not closed after exceeding its piercing level")
				}
			})
		})
	}
}

func TestProjectilePiercingKeepsVelocityOnEntityHits(t *testing.T) {
	// tickMovement only zeroes the velocity on a non-block hit when
	// PiercingLevel is 0. The position is clamped to the intercept either way.
	tests := []struct {
		name     string
		piercing int
		wantVel  mgl64.Vec3
	}{
		{name: "no piercing zeroes the velocity", wantVel: mgl64.Vec3{}},
		{name: "piercing keeps the velocity", piercing: 1, wantVel: mgl64.Vec3{4, 0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})

				conf := ProjectileBehaviourConfig{PiercingLevel: test.piercing}
				e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
				e.Behaviour().Tick(e, tx)

				assertVec3(t, "velocity", e.Velocity(), test.wantVel)
				assertVec3(t, "position", e.Position(), mgl64.Vec3{1.575, 100, 0.5})
			})
		})
	}
}

func TestProjectileDamageSourceCarriesNoPiercingInformation(t *testing.T) {
	// PINNED SCOPE: projectile.go:304 records that piercing arrows should bypass
	// shield blocking once shields exist. No shield exists, and
	// ProjectileDamageSource carries only the projectile and its owner, so the
	// damage a victim receives is identical at every piercing level.
	// Intentionally out of scope; pinned so a rewrite cannot change it silently.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		var sources []world.DamageSource
		for _, piercing := range []int{0, 3} {
			handle, target := projectileTarget(tx, mgl64.Vec3{float64(2 + piercing), 100, 0.5})
			victim, _ := handle.Entity(tx)
			conf := ProjectileBehaviourConfig{Damage: 2, PiercingLevel: piercing}
			e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, conf)
			lt.hitEntity(victim, e, mgl64.Vec3{1, 0, 0})

			if len(target.hurts) != 1 {
				t.Fatalf("hurts at piercing %d = %d, want 1", piercing, len(target.hurts))
			}
			sources = append(sources, target.hurts[0].src)
		}
		for i, src := range sources {
			p, ok := src.(ProjectileDamageSource)
			if !ok {
				t.Fatalf("damage source %d has type %T, want ProjectileDamageSource", i, src)
			}
			if p.Projectile == nil {
				t.Fatalf("damage source %d has a nil Projectile, want the projectile entity", i)
			}
			if p.Owner != nil {
				t.Fatalf("damage source %d Owner = %v, want nil for an ownerless projectile", i, p.Owner)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// 7. Configuration field effects.
// ---------------------------------------------------------------------------

func TestProjectileNegativeDamageSkipsHitEntity(t *testing.T) {
	// A negative Damage suppresses the hitEntity call entirely, so the victim is
	// neither hurt nor knocked back, but the hit still counts towards piercing
	// and still closes the projectile.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		_, target := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})

		conf := ProjectileBehaviourConfig{Damage: -1}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
		e.Behaviour().Tick(e, tx)

		if len(target.hurts) != 0 {
			t.Fatalf("target was hurt %d times with Damage -1, want 0", len(target.hurts))
		}
		if len(lt.collidedEntities) != 1 {
			t.Fatalf("collidedEntities = %d, want the hit to be recorded anyway", len(lt.collidedEntities))
		}
		if !lt.close {
			t.Fatal("close after a negative-damage entity hit = false, want true")
		}
	})
}

func TestProjectileDamageUsesTheVelocityFromBeforeTheForces(t *testing.T) {
	// Tick captures e.Velocity() before tickMovement runs and passes that to
	// hitEntity, so the damage scales with the velocity the projectile carried
	// into the tick, not with the one it leaves with. A gravity of 0.5 halves a
	// straight-up velocity of 1, which Ceil would otherwise hide.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		_, target := projectileTarget(tx, mgl64.Vec3{0.5, 100.6, 0.5})

		conf := ProjectileBehaviourConfig{Gravity: 0.5, Damage: 2}
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{0, 1, 0}, conf)
		e.Behaviour().Tick(e, tx)

		if len(target.hurts) != 1 {
			t.Fatalf("target was hurt %d times, want 1", len(target.hurts))
		}
		// Ceil(2 * 1) = 2. The post-force velocity of 0.5 would give Ceil(1) = 1.
		if got, want := target.hurts[0].damage, 2.0; got != want {
			t.Fatalf("damage = %v, want %v (Damage times the pre-force speed, rounded up)", got, want)
		}
	})
}

func TestProjectileParticleCount(t *testing.T) {
	// ProjectileBehaviourConfig.New promotes a zero ParticleCount to 1 when a
	// Particle is set, and Tick emits exactly that many particles at the hit
	// position before anything else happens.
	tests := []struct {
		name     string
		particle world.Particle
		count    int
		wantConf int
		wantSeen int
	}{
		{name: "no particle configured"},
		{name: "particle with an unset count", particle: particle.SnowballPoof{}, wantConf: 1, wantSeen: 1},
		{name: "particle with an explicit count", particle: particle.SnowballPoof{}, count: 6, wantConf: 6, wantSeen: 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			v := &projectileViewer{}
			mustDo(t, w, func(tx *world.Tx) {
				addViewer(tx, v)
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				conf := ProjectileBehaviourConfig{Particle: test.particle, ParticleCount: test.count}
				e, lt, _ := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
				if got := lt.conf.ParticleCount; got != test.wantConf {
					t.Fatalf("ParticleCount after New() = %d, want %d", got, test.wantConf)
				}
				v.reset()
				e.Behaviour().Tick(e, tx)

				if got := len(v.particles); got != test.wantSeen {
					t.Fatalf("ViewParticle calls = %d, want %d", got, test.wantSeen)
				}
				for i, pos := range v.particles {
					assertVec3(t, fmt.Sprintf("particle %d position", i), pos, mgl64.Vec3{0, 100.5, 0.5})
				}
				// The particle-less row would otherwise pass on a miss.
				assertVec3(t, "position", e.Position(), mgl64.Vec3{0, 100.5, 0.5})
			})
		})
	}
}

func TestProjectileParticleCountWithoutAParticlePanics(t *testing.T) {
	// PINNED BUG: New() only fills ParticleCount in when a Particle is set, but
	// Tick loops over ParticleCount without checking Particle, so a config with
	// a count and no particle reaches World.addParticle with a nil interface and
	// panics on Particle.Spawn. A library caller can build that config and has
	// no way to prevent the panic.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

		conf := ProjectileBehaviourConfig{ParticleCount: 3}
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, conf)

		var recovered any
		func() {
			defer func() { recovered = recover() }()
			e.Behaviour().Tick(e, tx)
		}()
		if recovered == nil {
			t.Fatal("a hit with ParticleCount set and no Particle did not panic, want the nil-particle panic pinned here")
		}
	})
}

func TestProjectileSound(t *testing.T) {
	// A configured Sound is played once at the hit position, after the particles
	// and before the entity or block is dealt with.
	w := syncWorld(t)
	v := &projectileViewer{}
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, v)
		tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

		conf := ProjectileBehaviourConfig{Particle: particle.SnowballPoof{}, Sound: sound.ArrowHit{}}
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
		v.reset()
		e.Behaviour().Tick(e, tx)

		if len(v.sounds) != 1 {
			t.Fatalf("ViewSound calls = %d, want 1", len(v.sounds))
		}
		assertVec3(t, "sound position", v.sounds[0], mgl64.Vec3{0, 100.5, 0.5})
		if want := []string{"particle", "sound"}; !slices.Equal(v.calls, want) {
			t.Fatalf("viewer calls = %v, want %v", v.calls, want)
		}
	})
}

func TestProjectileSoundlessHitPlaysNothing(t *testing.T) {
	w := syncWorld(t)
	v := &projectileViewer{}
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, v)
		tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

		e, _, _ := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, ProjectileBehaviourConfig{})
		v.reset()
		e.Behaviour().Tick(e, tx)

		if len(v.sounds) != 0 {
			t.Fatalf("ViewSound calls without a configured Sound = %d, want 0", len(v.sounds))
		}
		assertVec3(t, "position", e.Position(), mgl64.Vec3{0, 100.5, 0.5})
	})
}

func TestProjectileCriticalGatedOnCollided(t *testing.T) {
	// Critical() reports the configured flag only while the projectile has not
	// stuck into a block. A multiplier of 0 guarantees the stick on the first
	// block hit.
	tests := []struct {
		name         string
		critical     bool
		wantBefore   bool
		wantAfterHit bool
	}{
		{name: "critical projectile", critical: true, wantBefore: true},
		{name: "ordinary projectile"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				conf := ProjectileBehaviourConfig{Critical: test.critical, SurviveBlockCollision: true}
				e, lt, _ := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
				if got := lt.Critical(); got != test.wantBefore {
					t.Fatalf("Critical() before hitting anything = %v, want %v", got, test.wantBefore)
				}

				e.Behaviour().Tick(e, tx)
				if !lt.collided {
					t.Fatal("the projectile did not stick into the block, want it stuck")
				}
				if got := lt.Critical(); got != test.wantAfterHit {
					t.Fatalf("Critical() after sticking = %v, want %v", got, test.wantAfterHit)
				}
			})
		})
	}
}

func TestProjectileHitCallbackReceivesTheTraceResult(t *testing.T) {
	// The Hit callback is handed the same trace.Result the tick acted on, and it
	// runs after the particles, the sound and the damage.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		target, behaviour := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})

		var (
			calls         int
			hurtsAtCall   int
			hitTarget     *world.EntityHandle
			hitProjectile *Ent
			hitTx         *world.Tx
		)
		conf := ProjectileBehaviourConfig{Damage: 2, Hit: func(e *Ent, tx *world.Tx, r trace.Result) {
			calls++
			hurtsAtCall = len(behaviour.hurts)
			hitProjectile, hitTx = e, tx
			if er, ok := r.(trace.EntityResult); ok {
				hitTarget = er.Entity().H()
			}
		}}
		e, _, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
		e.Behaviour().Tick(e, tx)

		if calls != 1 {
			t.Fatalf("Hit callback calls = %d, want 1", calls)
		}
		if hitTarget != target {
			t.Fatal("the Hit callback received a trace.EntityResult for a different entity than the target that was spawned")
		}
		if hitProjectile != e {
			t.Fatal("the Hit callback received a different *Ent than the projectile that was ticked")
		}
		if hitTx != tx {
			t.Fatal("the Hit callback received a different transaction than the tick ran in")
		}
		if hurtsAtCall != 1 {
			t.Fatalf("damage calls before the Hit callback = %d, want 1: the callback runs after hitEntity", hurtsAtCall)
		}
	})
}

func TestProjectileHitCallbackSkippedOnSurvivedBlockCollision(t *testing.T) {
	// PINNED BUG: ProjectileBehaviourConfig.Hit documents that the target is
	// "either of the type trace.EntityResult or trace.BlockResult", but the
	// SurviveBlockCollision branch returns before the callback is reached. An
	// arrow's Hit therefore never sees a block, and the piercing bookkeeping
	// below it is skipped too.
	tests := []struct {
		name      string
		survives  bool
		wantCalls int
	}{
		{name: "a breaking projectile reports the block", wantCalls: 1},
		{name: "a surviving projectile does not", survives: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 100, 0}, block.Stone{}, nil)

				calls := 0
				conf := ProjectileBehaviourConfig{
					SurviveBlockCollision: test.survives,
					Hit:                   func(*Ent, *world.Tx, trace.Result) { calls++ },
				}
				e, _, _ := projectileSpawn(tx, mgl64.Vec3{-2, 100.5, 0.5}, mgl64.Vec3{4, 0, 0}, conf)
				e.Behaviour().Tick(e, tx)

				if calls != test.wantCalls {
					t.Fatalf("Hit callback calls = %d, want %d", calls, test.wantCalls)
				}
				// Both projectiles really did hit the block: without this the
				// surviving case would pass on a projectile that missed.
				assertVec3(t, "position", e.Position(), mgl64.Vec3{0, 100.5, 0.5})
			})
		})
	}
}

func TestProjectileOwnerAndPotionAccessors(t *testing.T) {
	// Owner() and Potion() report the configuration back unchanged, including
	// the nil owner of a projectile that nobody fired.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		ownerHandle, _ := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})
		tip := potion.TurtleMaster()

		_, owned, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{}, ProjectileBehaviourConfig{Owner: ownerHandle, Potion: tip})
		if owned.Owner() != ownerHandle {
			t.Fatal("Owner() returned a different handle than the one configured")
		}
		if got := owned.Potion(); got != tip {
			t.Fatalf("Potion() = %v, want %v", got, tip)
		}

		_, ownerless, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 1.5}, mgl64.Vec3{}, ProjectileBehaviourConfig{})
		if ownerless.Owner() != nil {
			t.Fatal("Owner() without a configured owner returned a handle, want nil")
		}
		if got, want := ownerless.Potion(), (potion.Potion{}); got != want {
			t.Fatalf("Potion() without a configured potion = %v, want %v", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// 8. Living victims and the unseedable critical roll.
// ---------------------------------------------------------------------------

func TestProjectileHitEntityAppliesLivingExtras(t *testing.T) {
	// Knockback, potion effects and ignition are all gated behind the victim
	// being Living. The knockback source is the victim's position minus the
	// projectile's velocity, and the force and height are the vanilla constants
	// plus the configured addends.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		victimPos := mgl64.Vec3{2, 100, 0.5}
		victim := projectileLivingAt(tx, victimPos, world.GameModeSurvival)
		behaviour := victim.behaviour()

		conf := ProjectileBehaviourConfig{
			Damage:                2,
			Potion:                potion.TurtleMaster(),
			KnockBackForceAddend:  0.1,
			KnockBackHeightAddend: 0.2,
		}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, conf)
		e.data.FireDuration = time.Second

		vel := mgl64.Vec3{1, 0, 0}
		lt.hitEntity(victim, e, vel)

		if len(behaviour.knockBacks) != 1 {
			t.Fatalf("KnockBack calls = %d, want 1", len(behaviour.knockBacks))
		}
		kb := behaviour.knockBacks[0]
		assertVec3(t, "knockback source", kb.src, victimPos.Sub(vel))
		if got, want := kb.force, 0.45+0.1; math.Abs(got-want) > physEpsilon {
			t.Fatalf("knockback force = %.17g, want %.17g", got, want)
		}
		if got, want := kb.height, 0.3608+0.2; math.Abs(got-want) > physEpsilon {
			t.Fatalf("knockback height = %.17g, want %.17g", got, want)
		}

		// Turtle Master is Resistance 3 then Slowness 4, both 20 seconds. A
		// projectile applies an eighth of a lasting effect's duration.
		if len(behaviour.effects) != 2 {
			t.Fatalf("effects added = %d, want 2", len(behaviour.effects))
		}
		wantEffects := []struct {
			typ effect.Type
			lvl int
		}{{effect.Resistance, 3}, {effect.Slowness, 4}}
		for i, want := range wantEffects {
			got := behaviour.effects[i]
			if got.Type() != want.typ {
				t.Fatalf("effect %d type = %T, want %T", i, got.Type(), want.typ)
			}
			if got.Level() != want.lvl {
				t.Fatalf("effect %d level = %d, want %d", i, got.Level(), want.lvl)
			}
			if wantDur := 20 * time.Second / 8; got.Duration() != wantDur {
				t.Fatalf("effect %d duration = %v, want %v", i, got.Duration(), wantDur)
			}
		}

		// The projectile was on fire, so the victim is set alight for 5 seconds.
		if got, want := victim.OnFireDuration(), 5*time.Second; got != want {
			t.Fatalf("victim fire duration = %v, want %v", got, want)
		}
	})
}

func TestProjectileHitEntityInstantPotionEffectPassesThroughUnscaled(t *testing.T) {
	// Only lasting effects have their duration divided by eight; an instant
	// effect is added exactly as the potion produced it.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		victim := projectileLivingAt(tx, mgl64.Vec3{2, 100, 0.5}, world.GameModeSurvival)

		conf := ProjectileBehaviourConfig{Damage: 2, Potion: potion.Healing()}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, conf)
		lt.hitEntity(victim, e, mgl64.Vec3{1, 0, 0})

		effects := victim.behaviour().effects
		if len(effects) != 1 {
			t.Fatalf("effects added = %d, want 1", len(effects))
		}
		if got := effects[0].Type(); got != effect.Type(effect.InstantHealth) {
			t.Fatalf("effect type = %T, want %T", got, effect.InstantHealth)
		}
		if got := effects[0].Duration(); got != 0 {
			t.Fatalf("instant effect duration = %v, want 0", got)
		}
	})
}

func TestProjectileHitEntityWithoutLivingSkipsKnockbackAndEffects(t *testing.T) {
	// An *Ent whose behaviour implements Hurt is damageable without being
	// Living, so it takes the damage but hitEntity returns before knockback,
	// potion effects and ignition.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		handle, target := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})
		victim, _ := handle.Entity(tx)

		conf := ProjectileBehaviourConfig{Damage: 2, Potion: potion.TurtleMaster()}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, conf)
		e.data.FireDuration = time.Second
		lt.hitEntity(victim, e, mgl64.Vec3{1, 0, 0})

		if len(target.hurts) != 1 {
			t.Fatalf("hurts = %d, want 1", len(target.hurts))
		}
		if got := victim.(*Ent).OnFireDuration(); got != 0 {
			t.Fatalf("victim fire duration = %v, want 0: only Living victims are set alight", got)
		}
	})
}

func TestProjectileHitEntityStopsWhenTheVictimIsNotVulnerable(t *testing.T) {
	// HurtEntity reports both whether the victim could be damaged and whether it
	// was vulnerable. A victim that reports invulnerable is not knocked back and
	// receives no effects.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		victim := projectileLivingAt(tx, mgl64.Vec3{2, 100, 0.5}, world.GameModeSurvival)
		victim.behaviour().invulnerable = true

		conf := ProjectileBehaviourConfig{Damage: 2, Potion: potion.TurtleMaster()}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{1, 0, 0}, conf)
		lt.hitEntity(victim, e, mgl64.Vec3{1, 0, 0})

		if len(victim.behaviour().hurts) != 1 {
			t.Fatalf("Hurt calls = %d, want 1", len(victim.behaviour().hurts))
		}
		if got := len(victim.behaviour().knockBacks); got != 0 {
			t.Fatalf("KnockBack calls on an invulnerable victim = %d, want 0", got)
		}
		if got := len(victim.behaviour().effects); got != 0 {
			t.Fatalf("effects added to an invulnerable victim = %d, want 0", got)
		}
	})
}

func TestProjectileCriticalDamageBounds(t *testing.T) {
	// hitEntity draws its critical bonus from math/rand/v2's global source,
	// which has no seeding API, so the exact damage is NOT reproducible and only
	// its bounds can be pinned: rand.Float64 lies in [0, 1), so the damage lies
	// in [base, base*1.5). The rolls are also expected to differ from each
	// other; a constant result would mean the bonus was not being drawn at all.
	const base, rolls = 6.0, 64

	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		handle, target := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})
		victim, _ := handle.Entity(tx)

		// Ceil(3 * 2) = 6, so the uncritical damage is exactly base.
		conf := ProjectileBehaviourConfig{Damage: 3, Critical: true}
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{2, 0, 0}, conf)
		for range rolls {
			lt.hitEntity(victim, e, mgl64.Vec3{2, 0, 0})
		}

		if len(target.hurts) != rolls {
			t.Fatalf("Hurt calls = %d, want %d", len(target.hurts), rolls)
		}
		distinct := map[float64]struct{}{}
		for i, h := range target.hurts {
			if h.damage < base || h.damage >= base*1.5 {
				t.Fatalf("critical damage roll %d = %.17g, want it in [%v, %v)", i, h.damage, base, base*1.5)
			}
			distinct[h.damage] = struct{}{}
		}
		if len(distinct) < 2 {
			t.Fatalf("%d critical rolls produced %d distinct damages, want the unseeded source to vary", rolls, len(distinct))
		}
	})
}

func TestProjectileUncriticalDamageIsExact(t *testing.T) {
	// Without the Critical flag the damage is exactly Ceil(Damage * speed), with
	// no random component at all.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		handle, target := projectileTarget(tx, mgl64.Vec3{2, 100, 0.5})
		victim, _ := handle.Entity(tx)

		const rolls = 8
		e, lt, _ := projectileSpawn(tx, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{}, ProjectileBehaviourConfig{Damage: 3})
		for range rolls {
			lt.hitEntity(victim, e, mgl64.Vec3{2, 0, 0})
		}
		if len(target.hurts) != rolls {
			t.Fatalf("Hurt calls = %d, want %d", len(target.hurts), rolls)
		}
		for i, h := range target.hurts {
			if h.damage != 6 {
				t.Fatalf("damage %d = %.17g, want exactly 6 (Ceil(3 * 2))", i, h.damage)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// projectileStep is one tick of a golden projectile trajectory.
type projectileStep struct {
	pos, vel mgl64.Vec3
	rot      cube.Rotation
}

// projectileSpawn adds a projectile with conf to tx at pos with vel, using the
// same bounding box as the arrow and snowball types.
func projectileSpawn(tx *world.Tx, pos, vel mgl64.Vec3, conf ProjectileBehaviourConfig) (*Ent, *ProjectileBehaviour, *world.EntityHandle) {
	handle := world.EntitySpawnOpts{Position: pos, Velocity: vel}.New(testMovingEntType{}, conf)
	e := tx.AddEntity(handle).(*Ent)
	return e, e.Behaviour().(*ProjectileBehaviour), handle
}

// projectileTarget adds a damageable target to tx. It is an *Ent rather than a
// Living, so it satisfies DamageableEntity without receiving knockback or
// effects.
func projectileTarget(tx *world.Tx, pos mgl64.Vec3) (*world.EntityHandle, *projectileTargetBehaviour) {
	b := &projectileTargetBehaviour{BaseBehaviour: NewBaseBehaviour()}
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, projectileTargetConfig{b: b})
	tx.AddEntity(handle)
	return handle, b
}

// projectileSpawnTargets adds a damageable target at every position passed, in
// the order passed, and returns their handles in that same order.
func projectileSpawnTargets(tx *world.Tx, positions ...mgl64.Vec3) []*world.EntityHandle {
	handles := make([]*world.EntityHandle, len(positions))
	for i, pos := range positions {
		handles[i], _ = projectileTarget(tx, pos)
	}
	return handles
}

// projectileHitEntity returns the entity a trace.Result hit, failing the test
// if the result is not an entity hit.
func projectileHitEntity(t *testing.T, r trace.Result) world.Entity {
	t.Helper()
	if r == nil {
		t.Fatal("no hit was reported, want an entity hit")
	}
	er, ok := r.(trace.EntityResult)
	if !ok {
		t.Fatalf("hit result has type %T, want trace.EntityResult", r)
	}
	return er.Entity()
}

// projectilePositions maps entities to their positions for failure messages.
func projectilePositions(entities []world.Entity) []mgl64.Vec3 {
	positions := make([]mgl64.Vec3, len(entities))
	for i, e := range entities {
		positions[i] = e.Position()
	}
	return positions
}

func assertProjectileRotation(t *testing.T, name string, got, want cube.Rotation) {
	t.Helper()
	if math.Abs(got[0]-want[0]) > physEpsilon || math.Abs(got[1]-want[1]) > physEpsilon {
		t.Fatalf("%s = {%.17g, %.17g}, want {%.17g, %.17g}", name, got[0], got[1], want[0], want[1])
	}
}

// projectileHurt records one call to a test victim's damage entry point.
type projectileHurt struct {
	damage float64
	src    world.DamageSource
}

type projectileTargetConfig struct{ b *projectileTargetBehaviour }

func (c projectileTargetConfig) Apply(data *world.EntityData) { data.Data = c.b }

// projectileTargetBehaviour makes an *Ent damageable without making it Living.
type projectileTargetBehaviour struct {
	BaseBehaviour

	hurts        []projectileHurt
	invulnerable bool
}

func (*projectileTargetBehaviour) Tick(*Ent, *world.Tx) *Movement { return nil }

func (b *projectileTargetBehaviour) Hurt(_ *Ent, damage float64, src world.DamageSource) (float64, bool) {
	b.hurts = append(b.hurts, projectileHurt{damage: damage, src: src})
	return damage, !b.invulnerable
}

// projectileKnockBack records one knockback applied to a test Living victim.
type projectileKnockBack struct {
	src           mgl64.Vec3
	force, height float64
}

type projectileLivingConfig struct{ b *projectileLivingBehaviour }

func (c projectileLivingConfig) Apply(data *world.EntityData) { data.Data = c.b }

// projectileLivingBehaviour holds the state a projectileLiving records. It
// lives on the handle so that it survives the wrapper being rebuilt on every
// EntityType.Open call.
type projectileLivingBehaviour struct {
	BaseBehaviour

	mode         world.GameMode
	invulnerable bool

	hurts      []projectileHurt
	knockBacks []projectileKnockBack
	effects    []effect.Effect
}

func (*projectileLivingBehaviour) Tick(*Ent, *world.Tx) *Movement { return nil }

// projectileLivingType opens entities that satisfy entity.Living, which no
// other test entity in this package does. It shares the arrow bounding box so
// that the interception geometry matches the other targets.
type projectileLivingType struct{}

func (projectileLivingType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return projectileLiving{Ent: Open(tx, handle, data)}
}

func (projectileLivingType) EncodeEntity() string { return "impulse:test_projectile_living" }

func (projectileLivingType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (projectileLivingType) DecodeNBT(map[string]any, *world.EntityData) {}
func (projectileLivingType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// projectileLiving is a Living victim carrying a game mode, which is the only
// way to reach the spectator clause of ProjectileBehaviour.ignores.
type projectileLiving struct{ *Ent }

// projectileLivingAt adds a Living victim with the game mode passed to tx.
func projectileLivingAt(tx *world.Tx, pos mgl64.Vec3, mode world.GameMode) projectileLiving {
	b := &projectileLivingBehaviour{BaseBehaviour: NewBaseBehaviour(), mode: mode}
	handle := world.EntitySpawnOpts{Position: pos}.New(projectileLivingType{}, projectileLivingConfig{b: b})
	return tx.AddEntity(handle).(projectileLiving)
}

func (l projectileLiving) behaviour() *projectileLivingBehaviour {
	return l.Behaviour().(*projectileLivingBehaviour)
}

func (l projectileLiving) GameMode() world.GameMode { return l.behaviour().mode }

func (l projectileLiving) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	b := l.behaviour()
	b.hurts = append(b.hurts, projectileHurt{damage: damage, src: src})
	return damage, !b.invulnerable
}

func (l projectileLiving) KnockBack(src mgl64.Vec3, force, height float64) {
	b := l.behaviour()
	b.knockBacks = append(b.knockBacks, projectileKnockBack{src: src, force: force, height: height})
}

func (l projectileLiving) AddEffect(e effect.Effect) {
	b := l.behaviour()
	b.effects = append(b.effects, e)
}

func (projectileLiving) Health() float64                           { return 20 }
func (projectileLiving) MaxHealth() float64                        { return 20 }
func (projectileLiving) SetMaxHealth(float64)                      {}
func (projectileLiving) Dead() bool                                { return false }
func (projectileLiving) Heal(float64, world.HealingSource) float64 { return 0 }
func (projectileLiving) RemoveEffect(effect.Type)                  {}
func (projectileLiving) Effects() []effect.Effect                  { return nil }
func (projectileLiving) Speed() float64                            { return 0 }
func (projectileLiving) SetSpeed(float64)                          {}

type projectileCollectorConfig struct{ b *projectileCollectorBehaviour }

func (c projectileCollectorConfig) Apply(data *world.EntityData) { data.Data = c.b }

// projectileCollectorBehaviour records what tryPickup offered a collector and
// controls what the collector reports back.
type projectileCollectorBehaviour struct {
	BaseBehaviour

	collected []item.Stack
	n         int
	ok        bool
}

func (*projectileCollectorBehaviour) Tick(*Ent, *world.Tx) *Movement { return nil }

type projectileCollectorType struct{}

func (projectileCollectorType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return projectileCollector{Ent: Open(tx, handle, data)}
}

func (projectileCollectorType) EncodeEntity() string { return "impulse:test_projectile_collector" }

func (projectileCollectorType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (projectileCollectorType) DecodeNBT(map[string]any, *world.EntityData) {}
func (projectileCollectorType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// projectileCollector satisfies Collector, which tryPickup type-asserts for.
type projectileCollector struct{ *Ent }

func (c projectileCollector) Collect(stack item.Stack) (int, bool) {
	b := c.Behaviour().(*projectileCollectorBehaviour)
	b.collected = append(b.collected, stack)
	return b.n, b.ok
}

// projectileCollectorAt adds a collector to tx that reports collecting n items
// and reports ok as its ability to collect at all.
func projectileCollectorAt(tx *world.Tx, pos mgl64.Vec3, n int, ok bool) *projectileCollectorBehaviour {
	b := &projectileCollectorBehaviour{BaseBehaviour: NewBaseBehaviour(), n: n, ok: ok}
	handle := world.EntitySpawnOpts{Position: pos}.New(projectileCollectorType{}, projectileCollectorConfig{b: b})
	tx.AddEntity(handle)
	return b
}

// projectileProbeType counts the Open calls World.entitiesWithin makes, which
// is how the candidate set of trace.Perform becomes observable.
type projectileProbeType struct{ opened int }

func (t *projectileProbeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	t.opened++
	return Open(tx, handle, data)
}

func (*projectileProbeType) EncodeEntity() string { return "impulse:test_probe" }

func (*projectileProbeType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (*projectileProbeType) DecodeNBT(map[string]any, *world.EntityData) {}
func (*projectileProbeType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// projectileViewer records the viewer updates a projectile produces, keeping an
// ordered log so that the order of the updates can be asserted as well as their
// contents.
type projectileViewer struct {
	world.NopViewer

	calls     []string
	teleports []mgl64.Vec3
	actions   []world.EntityAction
	particles []mgl64.Vec3
	sounds    []mgl64.Vec3
}

func (v *projectileViewer) reset() {
	v.calls, v.teleports, v.actions, v.particles, v.sounds = nil, nil, nil, nil, nil
}

func (v *projectileViewer) ViewEntityTeleport(_ world.Entity, pos mgl64.Vec3) {
	v.calls = append(v.calls, "teleport")
	v.teleports = append(v.teleports, pos)
}

func (v *projectileViewer) ViewEntityAction(_ world.Entity, a world.EntityAction) {
	v.calls = append(v.calls, "action")
	v.actions = append(v.actions, a)
}

func (v *projectileViewer) ViewEntityState(world.Entity) {
	v.calls = append(v.calls, "state")
}

func (v *projectileViewer) ViewParticle(pos mgl64.Vec3, _ world.Particle) {
	v.calls = append(v.calls, "particle")
	v.particles = append(v.particles, pos)
}

func (v *projectileViewer) ViewSound(pos mgl64.Vec3, _ world.Sound) {
	v.calls = append(v.calls, "sound")
	v.sounds = append(v.sounds, pos)
}
