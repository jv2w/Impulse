package entity

import (
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/potion"
	"github.com/jv2w/Impulse/server/world"
)

// This file pins the iteration order of tx.EntitiesWithin and every decision in
// server/entity that is settled by "the first entity yielded wins". Stage 2.1
// replaces the per-chunk entity slice with a spatial index; when it does, every
// assertion here either has to be preserved deliberately or changed
// deliberately. Silently changing one of them makes entities appear to be hit,
// picked up or merged by the wrong party, which is exactly the class of defect
// that is impossible to reproduce from a bug report.

func TestEntitiesWithinYieldOrder(t *testing.T) {
	// The order is chunk-major with X as the outer loop and Z as the inner one,
	// and insertion order into the chunk's own slice within a chunk. Entities are
	// spawned here in an order that disagrees with every axis of that rule, so a
	// pass means the rule is doing the work rather than the spawn order.
	w := syncWorld(t)
	spawns := []struct {
		name string
		pos  mgl64.Vec3
	}{
		{name: "chunk 1 0", pos: mgl64.Vec3{20.5, 64, 0.5}},
		{name: "chunk 0 1", pos: mgl64.Vec3{0.5, 64, 20.5}},
		{name: "chunk 0 0 first", pos: mgl64.Vec3{0.5, 64, 0.5}},
		{name: "chunk 1 1", pos: mgl64.Vec3{20.5, 64, 20.5}},
		{name: "chunk 0 0 second", pos: mgl64.Vec3{1.5, 64, 1.5}},
	}
	want := []string{"chunk 0 0 first", "chunk 0 0 second", "chunk 0 1", "chunk 1 0", "chunk 1 1"}

	mustDo(t, w, func(tx *world.Tx) {
		for _, s := range spawns {
			tx.AddEntity(world.EntitySpawnOpts{Position: s.pos, NameTag: s.name}.New(testMovingEntType{}, testMoveConfig{}))
		}
		if got := orderNames(tx, cube.Box(0, 60, 0, 32, 70, 32)); !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin() order = %v, want %v", got, want)
		}
	})
}

func TestEntitiesWithinTestsThePositionPointNotTheBBox(t *testing.T) {
	// The containment test is box.Vec3Within(handle.data.Pos), so an entity whose
	// body overlaps the box but whose origin does not is never yielded. Every
	// caller in this package that grows a search box is compensating for this.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}, NameTag: "inside"}.New(testMovingEntType{}, testMoveConfig{}))
		// Origin just outside the box on X, but the 0.25-wide body reaches in.
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{6.05, 64, 5.5}, NameTag: "body overlaps only"}.New(testMovingEntType{}, testMoveConfig{}))

		got := orderNames(tx, cube.Box(5, 63, 5, 6, 65, 6))
		if want := []string{"inside"}; !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin() = %v, want %v", got, want)
		}
	})
}

func TestEntitiesWithinBoundsAreExclusive(t *testing.T) {
	// Vec3Within rejects a position sitting exactly on either bound on all three
	// axes, so an entity standing precisely on the edge of a search box is
	// invisible to it.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5, 64, 5.5}, NameTag: "on the minimum"}.New(testMovingEntType{}, testMoveConfig{}))
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{6, 64, 5.5}, NameTag: "on the maximum"}.New(testMovingEntType{}, testMoveConfig{}))
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}, NameTag: "strictly inside"}.New(testMovingEntType{}, testMoveConfig{}))

		got := orderNames(tx, cube.Box(5, 63, 5, 6, 65, 6))
		if want := []string{"strictly inside"}; !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin() = %v, want %v", got, want)
		}
	})
}

func TestEntitiesWithinSnapshotsTheChunkBeforeYielding(t *testing.T) {
	// The chunk's entity slice is cloned before it is walked, so a caller may add
	// and close entities while iterating. ItemBehaviour.merge and
	// ProjectileBehaviour.tryPickup both rely on this. Any replacement index has
	// to keep the same snapshot semantics or those two corrupt their own
	// iteration.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		for _, name := range []string{"first", "second"} {
			tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}, NameTag: name}.New(testMovingEntType{}, testMoveConfig{}))
		}

		var seen []string
		for e := range tx.EntitiesWithin(cube.Box(5, 63, 5, 6, 65, 6)) {
			seen = append(seen, e.(*Ent).NameTag())
			// Both mutations target the very chunk being iterated.
			tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}, NameTag: "added while iterating"}.New(testMovingEntType{}, testMoveConfig{}))
			_ = e.Close()
		}
		if want := []string{"first", "second"}; !slices.Equal(seen, want) {
			t.Fatalf("EntitiesWithin() yielded %v, want %v: the snapshot must not see entities added during the walk", seen, want)
		}
	})
}

func TestEntitiesWithinSkipsUnloadedChunks(t *testing.T) {
	// An unloaded chunk is treated as empty rather than being loaded on demand,
	// so a search box reaching into ungenerated terrain silently returns fewer
	// entities than it should.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}, NameTag: "loaded"}.New(testMovingEntType{}, testMoveConfig{}))

		// A box reaching far beyond any chunk this transaction has touched.
		got := orderNames(tx, cube.Box(0, 60, 0, 2000, 70, 2000))
		if want := []string{"loaded"}; !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin() = %v, want %v", got, want)
		}
	})
}

func TestItemMergePartnerIsTheFirstYielded(t *testing.T) {
	// checkNearby merges with the first item entity the iteration offers, not the
	// nearest one. Both partners are destroyed and the combined stack respawns at
	// the PARTNER's position, so the winner is directly observable.
	//
	// The two candidates below sit in the same chunk at equal distance on
	// opposite sides, so nothing but insertion order decides this today.
	// Stage 2.1 will decide it differently.
	tests := []struct {
		name       string
		spawnOrder []string
		wantMerge  mgl64.Vec3
	}{
		{name: "the earlier insertion wins", spawnOrder: []string{"west", "east"}, wantMerge: mgl64.Vec3{5.1, 64, 5.5}},
		{name: "reversing the insertion reverses the winner", spawnOrder: []string{"east", "west"}, wantMerge: mgl64.Vec3{5.9, 64, 5.5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			positions := map[string]mgl64.Vec3{
				"west": {5.1, 64, 5.5},
				"east": {5.9, 64, 5.5},
			}
			stack := item.NewStack(item.Apple{}, 1)
			subject := NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}}, stack)
			candidates := make(map[string]*world.EntityHandle, len(positions))

			mustDo(t, w, func(tx *world.Tx) {
				e := tx.AddEntity(subject).(*Ent)
				for _, name := range test.spawnOrder {
					h := NewItem(world.EntitySpawnOpts{Position: positions[name]}, stack)
					candidates[name] = h
					tx.AddEntity(h)
				}

				// checkNearby rather than a tick loop: the default pickup delay
				// of ten ticks is not what this test is about.
				e.Behaviour().(*ItemBehaviour).checkNearby(e, tx)

				merged := orderItemsAt(tx, test.wantMerge)
				if len(merged) != 1 {
					t.Fatalf("item entities at the merge position %v = %d, want 1", test.wantMerge, len(merged))
				}
				if got := merged[0].Count(); got != 2 {
					t.Fatalf("merged stack count = %d, want 2", got)
				}
				if _, ok := subject.Entity(tx); ok {
					t.Fatal("the merging entity survived the merge, want it closed")
				}
				for name, h := range candidates {
					_, alive := h.Entity(tx)
					if want := positions[name] != test.wantMerge; alive != want {
						t.Fatalf("candidate %q alive = %v, want %v", name, alive, want)
					}
				}
			})
		})
	}
}

func TestItemCollectorAndMergePartnerCompeteOnlyByOrder(t *testing.T) {
	// checkNearby returns on the first candidate it can act on, whichever kind it
	// is. A collector does NOT take precedence over a merge partner: if the merge
	// partner is yielded first, the item merges and the collector goes home empty
	// handed on that tick.
	tests := []struct {
		name         string
		collectFirst bool
		wantCollect  bool
	}{
		{name: "collector yielded first collects", collectFirst: true, wantCollect: true},
		{name: "merge partner yielded first merges", wantCollect: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			stack := item.NewStack(item.Apple{}, 1)
			subject := NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}}, stack)
			collector := world.EntitySpawnOpts{Position: mgl64.Vec3{5.9, 64, 5.5}}.New(orderCollectorType{}, orderCollectorConfig{})
			partner := NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{5.1, 64, 5.5}}, stack)

			mustDo(t, w, func(tx *world.Tx) {
				e := tx.AddEntity(subject).(*Ent)
				if test.collectFirst {
					tx.AddEntity(collector)
					tx.AddEntity(partner)
				} else {
					tx.AddEntity(partner)
					tx.AddEntity(collector)
				}

				e.Behaviour().(*ItemBehaviour).checkNearby(e, tx)

				c, ok := collector.Entity(tx)
				if !ok {
					t.Fatal("the collector left the world")
				}
				collected := c.(*orderCollector).data.Data.(*orderCollectorData).collected
				if got := len(collected) > 0; got != test.wantCollect {
					t.Fatalf("collector received %d stacks, want collected = %v", len(collected), test.wantCollect)
				}
				if _, alive := partner.Entity(tx); alive == !test.wantCollect {
					t.Fatalf("merge partner alive = %v, want %v", alive, test.wantCollect)
				}
			})
		})
	}
}

func TestAreaEffectCloudRadiusShrinksPerAffectedEntity(t *testing.T) {
	// LATENT: applyEffects re-reads a.radius on every iteration of the entity
	// loop, and each entity it affects shrinks that radius by RadiusUseGrowth
	// before the next candidate is tested. So which entities near the rim of a
	// cloud receive its potion depends entirely on the order EntitiesWithin
	// yields them. Stage 2.1 changes that order, and this is the site where it
	// changes an outcome rather than merely a sequence. Pinning the per-use
	// shrink is the closest a test can get without a full Living implementation.
	behaviour := AreaEffectCloudBehaviourConfig{
		Potion:          potion.LongSwiftness(),
		RadiusUseGrowth: -0.5,
	}.New()

	if got, want := behaviour.Radius(), 3.0; got != want {
		t.Fatalf("default Radius() = %v, want %v", got, want)
	}
	behaviour.subtractUseRadius()
	if got, want := behaviour.Radius(), 2.5; got != want {
		t.Fatalf("Radius() after one affected entity = %v, want %v", got, want)
	}
	behaviour.subtractUseRadius()
	if got, want := behaviour.Radius(), 2.0; got != want {
		t.Fatalf("Radius() after two affected entities = %v, want %v", got, want)
	}
}

func TestAreaEffectCloudRadiusBoundariesDisagree(t *testing.T) {
	// PINNED BUG: subtractTickRadius closes the cloud on radius < 0.5 while
	// subtractUseRadius closes it on radius <= 0.5, so a cloud that arrives at
	// exactly 0.5 lives on if it got there by ageing and dies if it got there by
	// being used.
	tick := AreaEffectCloudBehaviourConfig{Radius: 1.0, RadiusTickGrowth: -0.5}.New()
	tick.subtractTickRadius()
	if tick.stationary.close {
		t.Fatalf("tick shrink to %v closed the cloud, want it alive at exactly 0.5", tick.Radius())
	}

	use := AreaEffectCloudBehaviourConfig{Radius: 1.0, RadiusUseGrowth: -0.5}.New()
	use.subtractUseRadius()
	if !use.stationary.close {
		t.Fatalf("use shrink to %v left the cloud alive, want it closed at exactly 0.5", use.Radius())
	}
}

// orderNames returns the name tags of the entities EntitiesWithin yields for
// box, in the order it yields them.
func orderNames(tx *world.Tx, box cube.BBox) []string {
	var names []string
	for e := range tx.EntitiesWithin(box) {
		names = append(names, e.(*Ent).NameTag())
	}
	return names
}

// orderItemsAt returns the stacks of the item entities positioned at pos.
func orderItemsAt(tx *world.Tx, pos mgl64.Vec3) []item.Stack {
	var stacks []item.Stack
	for e := range tx.EntitiesWithin(cube.Box(-0.1, -0.1, -0.1, 0.1, 0.1, 0.1).Translate(pos)) {
		if ent, ok := e.(*Ent); ok && ent.H().Type() == ItemType {
			stacks = append(stacks, ent.Behaviour().(*ItemBehaviour).Item())
		}
	}
	return stacks
}

// orderCollectorData records what a test collector was handed.
type orderCollectorData struct {
	collected []item.Stack
}

type orderCollectorConfig struct{}

func (orderCollectorConfig) Apply(data *world.EntityData) { data.Data = &orderCollectorData{} }

// orderCollector is the smallest thing that satisfies Collector: ItemBehaviour
// only needs world.Entity plus Collect, not the full Living interface.
type orderCollector struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (c *orderCollector) H() *world.EntityHandle { return c.handle }
func (c *orderCollector) Position() mgl64.Vec3   { return c.data.Pos }
func (c *orderCollector) Rotation() cube.Rotation {
	return c.data.Rot
}
func (c *orderCollector) Close() error { return nil }

func (c *orderCollector) Collect(s item.Stack) (int, bool) {
	d := c.data.Data.(*orderCollectorData)
	d.collected = append(d.collected, s)
	return s.Count(), true
}

type orderCollectorType struct{}

func (orderCollectorType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &orderCollector{handle: handle, data: data}
}

func (orderCollectorType) EncodeEntity() string { return "impulse:test_collector" }
func (orderCollectorType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (orderCollectorType) DecodeNBT(map[string]any, *world.EntityData) {}
func (orderCollectorType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
