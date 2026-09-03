package world

import (
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
)

// TestTickEntityEveryEntityTicksOncePerTick pins the only guarantee the entity
// pass gives about which entities run: each of them runs exactly once per tick.
func TestTickEntityEveryEntityTicksOncePerTick(t *testing.T) {
	log := &tickEntityLog{}
	w := tickEntityWorld(t)

	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	runWorld(w, func(tx *Tx) {
		for _, name := range names {
			tickEntitySpawn(tx, log, name, mgl64.Vec3{0.5, 64, 0.5}, nil)
		}
	})
	log.reset()
	for range 3 {
		w.AdvanceTick()
	}

	for _, name := range names {
		if got := countOccurrences(log.names, name); got != 3 {
			t.Fatalf("entity %q ticked %d time(s) over three ticks, want 3", name, got)
		}
	}
}

// TestTickEntityOrderIsNotGuaranteed pins that the entity pass is a map range
// over World.entities and therefore has no defined order. Callers must not
// depend on one, so the ordering that happens to come out today is deliberately
// not asserted; what is asserted is that it moves.
func TestTickEntityOrderIsNotGuaranteed(t *testing.T) {
	log := &tickEntityLog{}
	w := tickEntityWorld(t)

	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	runWorld(w, func(tx *Tx) {
		for _, name := range names {
			tickEntitySpawn(tx, log, name, mgl64.Vec3{0.5, 64, 0.5}, nil)
		}
	})

	seen := map[string]struct{}{}
	for range 60 {
		log.reset()
		w.AdvanceTick()
		seen[joinNames(log.names)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("entity tick order was identical across 60 ticks (%d distinct orders); "+
			"a map range over eight entities was expected to vary", len(seen))
	}
}

// TestTickEntityAddedDuringPassTicksFromTheNextTick pins what an entity spawned
// from inside another entity's Tick can rely on. Go leaves it undefined whether
// a key added during a map range is visited, so only the state afterwards and
// the following tick are pinned.
func TestTickEntityAddedDuringPassTicksFromTheNextTick(t *testing.T) {
	log := &tickEntityLog{}
	w := tickEntityWorld(t)

	spawned := false
	runWorld(w, func(tx *Tx) {
		tickEntitySpawn(tx, log, "parent", mgl64.Vec3{0.5, 64, 0.5}, func(tx *Tx, _ Entity) {
			if spawned {
				return
			}
			spawned = true
			tickEntitySpawn(tx, log, "child", mgl64.Vec3{0.5, 64, 0.5}, nil)
		})
	})
	log.reset()
	w.AdvanceTick()

	runWorld(w, func(tx *Tx) {
		if got := len(tx.World().entities); got != 2 {
			t.Fatalf("entities after the spawning tick = %d, want 2", got)
		}
	})

	log.reset()
	w.AdvanceTick()
	if got := countOccurrences(log.names, "child"); got != 1 {
		t.Fatalf("child ticked %d time(s) on the tick after it was spawned, want 1", got)
	}
	if got := countOccurrences(log.names, "parent"); got != 1 {
		t.Fatalf("parent ticked %d time(s), want 1", got)
	}
}

// TestTickEntityRemovedDuringPassStopsTicking pins that removing an entity from
// inside the pass takes it out of both World.entities and its Column, and that
// it never ticks again, whether it removes itself or another entity.
func TestTickEntityRemovedDuringPassStopsTicking(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "removes itself", target: "remover"},
		{name: "removes another entity", target: "victim"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &tickEntityLog{}
			w := tickEntityWorld(t)

			var victim *EntityHandle
			removed := false
			runWorld(w, func(tx *Tx) {
				victim = tickEntitySpawn(tx, log, "victim", mgl64.Vec3{0.5, 64, 0.5}, nil).H()
				tickEntitySpawn(tx, log, "remover", mgl64.Vec3{0.5, 64, 0.5}, func(tx *Tx, self Entity) {
					if removed {
						return
					}
					removed = true
					target := self
					if test.target == "victim" {
						target = victim.mustEntity(tx)
					}
					tx.RemoveEntity(target)
				})
			})
			log.reset()
			w.AdvanceTick()

			runWorld(w, func(tx *Tx) {
				gone := victim
				if test.target == "remover" {
					gone = nil
				}
				if got := len(tx.World().entities); got != 1 {
					t.Fatalf("entities after the removal = %d, want 1", got)
				}
				col := tx.chunk(ChunkPos{0, 0})
				if got := len(col.Entities); got != 1 {
					t.Fatalf("column entities after the removal = %d, want 1", got)
				}
				if gone != nil && slices.Contains(col.Entities, gone) {
					t.Fatal("removed entity is still listed in its column")
				}
			})

			log.reset()
			for range 2 {
				w.AdvanceTick()
			}
			if got := countOccurrences(log.names, test.target); got != 0 {
				t.Fatalf("removed entity %q ticked %d time(s) after removal, want 0", test.target, got)
			}
		})
	}
}

// TestTickEntityViewerGating pins that a non-Synchronous World only ticks
// entities whose chunk has at least one viewer, and that a Synchronous World
// ignores that condition entirely.
func TestTickEntityViewerGating(t *testing.T) {
	t.Run("synchronous ticks viewerless entities", func(t *testing.T) {
		log := &tickEntityLog{}
		w := tickEntityWorld(t)
		runWorld(w, func(tx *Tx) {
			tickEntitySpawn(tx, log, "lonely", mgl64.Vec3{0.5, 64, 0.5}, nil)
		})
		log.reset()
		for range 4 {
			w.AdvanceTick()
		}
		if got := countOccurrences(log.names, "lonely"); got != 4 {
			t.Fatalf("viewerless entity ticked %d time(s) over four ticks, want 4", got)
		}
	})
	t.Run("asynchronous needs a chunk viewer", func(t *testing.T) {
		log := &tickEntityLog{}
		// The World is non-Synchronous but has no world viewers, so its own tick
		// loop returns at the viewer guard; the entity pass is driven directly.
		w := Config{RandSource: rand.NewPCG(5, 5), Log: tickEntityDiscardLog()}.New()
		t.Cleanup(func() { _ = w.Close() })

		runWorld(w, func(tx *Tx) {
			tickEntitySpawn(tx, log, "watched", mgl64.Vec3{0.5, 64, 0.5}, nil)
			tickEntitySpawn(tx, log, "unwatched", mgl64.Vec3{20.5, 64, 0.5}, nil)
			tx.World().addViewer(tx, tx.chunk(ChunkPos{0, 0}), &Loader{viewer: &tickEntityViewer{}})
		})
		log.reset()
		runWorld(w, func(tx *Tx) { ticker{}.tickEntities(tx, 1) })

		if got := countOccurrences(log.names, "watched"); got != 1 {
			t.Fatalf("entity in a viewed chunk ticked %d time(s), want 1", got)
		}
		if got := countOccurrences(log.names, "unwatched"); got != 0 {
			t.Fatalf("entity in an unviewed chunk ticked %d time(s), want 0", got)
		}
	})
}

// TestTickEntityChunkTransition pins the bookkeeping the entity pass does when an
// entity's position has moved into another chunk: the index and both Columns are
// updated, and only viewers that gained or lost sight of the entity are told.
func TestTickEntityChunkTransition(t *testing.T) {
	log := &tickEntityLog{}
	w := tickEntityWorld(t)

	oldOnly, newOnly, both := &tickEntityViewer{}, &tickEntityViewer{}, &tickEntityViewer{}
	var handle *EntityHandle
	runWorld(w, func(tx *Tx) {
		handle = tickEntitySpawn(tx, log, "traveller", mgl64.Vec3{0.5, 64, 0.5}, nil).H()
		from, to := tx.chunk(ChunkPos{0, 0}), tx.chunk(ChunkPos{1, 0})
		tx.World().addViewer(tx, from, &Loader{viewer: oldOnly})
		tx.World().addViewer(tx, to, &Loader{viewer: newOnly})
		tx.World().addViewer(tx, from, &Loader{viewer: both})
		tx.World().addViewer(tx, to, &Loader{viewer: both})
	})
	oldOnly.reset()
	newOnly.reset()
	both.reset()

	runWorld(w, func(tx *Tx) { handle.data.Pos = mgl64.Vec3{16.5, 64, 0.5} })
	w.AdvanceTick()

	runWorld(w, func(tx *Tx) {
		if got := tx.World().entities[handle]; got != (ChunkPos{1, 0}) {
			t.Fatalf("entity index after the move = %v, want (1, 0)", got)
		}
		if from := tx.chunk(ChunkPos{0, 0}); slices.Contains(from.Entities, handle) {
			t.Fatal("entity still listed in the chunk it left")
		}
		if to := tx.chunk(ChunkPos{1, 0}); !slices.Contains(to.Entities, handle) {
			t.Fatal("entity not listed in the chunk it entered")
		}
	})

	if got := oldOnly.calls; !slices.Equal(got, []string{"HideEntity"}) {
		t.Fatalf("viewer of the old chunk saw %v, want a single HideEntity", got)
	}
	if got := newOnly.calls; !slices.Equal(got, []string{"ViewEntity", "ViewEntityItems", "ViewEntityArmour"}) {
		t.Fatalf("viewer of the new chunk saw %v, want the full show sequence", got)
	}
	if got := both.calls; len(got) != 0 {
		t.Fatalf("viewer of both chunks saw %v, want nothing", got)
	}
}

// TestTickEntityInUnloadedChunkIsSkipped pins that an entity whose chunk is not
// loaded is skipped without its index being repaired: it stops ticking and keeps
// pointing at the chunk it was last seen in.
//
// PINNED BUG: the entity stays in World.entities forever while its chunk is
// unloaded, so it neither ticks nor is reachable, and its stale index entry is
// never reconciled.
func TestTickEntityInUnloadedChunkIsSkipped(t *testing.T) {
	log := &tickEntityLog{}
	w := tickEntityWorld(t)

	var handle *EntityHandle
	runWorld(w, func(tx *Tx) {
		handle = tickEntitySpawn(tx, log, "stranded", mgl64.Vec3{0.5, 64, 0.5}, nil).H()
	})
	// Moving far away puts the entity in a chunk that was never loaded.
	runWorld(w, func(tx *Tx) { handle.data.Pos = mgl64.Vec3{10000.5, 64, 0.5} })
	log.reset()
	for range 3 {
		w.AdvanceTick()
	}

	if got := countOccurrences(log.names, "stranded"); got != 0 {
		t.Fatalf("entity in an unloaded chunk ticked %d time(s), want 0", got)
	}
	runWorld(w, func(tx *Tx) {
		if got := tx.World().entities[handle]; got != (ChunkPos{0, 0}) {
			t.Fatalf("index of the skipped entity = %v, want the stale (0, 0)", got)
		}
	})
}

// TestTickEntityOutsideVerticalRangeStillTicks pins that only the horizontal
// position decides whether an entity ticks: the chunk lookup ignores Y, so an
// entity far above the build limit keeps running.
func TestTickEntityOutsideVerticalRangeStillTicks(t *testing.T) {
	log := &tickEntityLog{}
	w := tickEntityWorld(t)

	var handle *EntityHandle
	runWorld(w, func(tx *Tx) {
		handle = tickEntitySpawn(tx, log, "high", mgl64.Vec3{0.5, 64, 0.5}, nil).H()
	})
	runWorld(w, func(tx *Tx) { handle.data.Pos = mgl64.Vec3{0.5, 100000, 0.5} })
	log.reset()
	for range 2 {
		w.AdvanceTick()
	}

	if got := countOccurrences(log.names, "high"); got != 2 {
		t.Fatalf("entity above the build limit ticked %d time(s), want 2", got)
	}
}

// joinNames renders a tick's entity order as one comparable string.
func joinNames(names []string) string {
	out := ""
	for _, name := range names {
		out += name + ","
	}
	return out
}

// tickEntityWorld returns a Synchronous World with a fixed random source.
func tickEntityWorld(t *testing.T) *World {
	t.Helper()

	w := Config{Synchronous: true, RandSource: rand.NewPCG(0x454e5449, 0x54590000), Log: tickEntityDiscardLog()}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func tickEntityDiscardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// tickEntitySpawn adds an entity that records its ticks under name and runs
// onTick, if given, on every tick.
func tickEntitySpawn(tx *Tx, log *tickEntityLog, name string, pos mgl64.Vec3, onTick func(*Tx, Entity)) Entity {
	conf := tickEntityConfig{b: &tickEntityBehaviour{log: log, name: name, onTick: onTick}}
	return tx.AddEntity(EntitySpawnOpts{Position: pos}.New(tickEntityType{}, conf))
}

// tickEntityLog records which entities ticked, in call order. It is only safe to
// use from the goroutine driving the Synchronous World the entities live in.
type tickEntityLog struct {
	names []string
}

func (l *tickEntityLog) reset() { l.names = nil }

// tickEntityBehaviour is stashed in EntityData.Data, giving each entity its own
// identity and side effect without any package-level state.
type tickEntityBehaviour struct {
	log    *tickEntityLog
	name   string
	onTick func(*Tx, Entity)
}

type tickEntityConfig struct{ b *tickEntityBehaviour }

func (c tickEntityConfig) Apply(data *EntityData) { data.Data = c.b }

type tickEntityType struct{}

func (tickEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &tickEntityEntity{handle: handle, data: data}
}
func (tickEntityType) EncodeEntity() string                  { return "test:tick_entity" }
func (tickEntityType) BBox(Entity) cube.BBox                 { return cube.BBox{} }
func (tickEntityType) DecodeNBT(map[string]any, *EntityData) {}
func (tickEntityType) EncodeNBT(*EntityData) map[string]any  { return nil }

type tickEntityEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (e *tickEntityEntity) Close() error            { return nil }
func (e *tickEntityEntity) H() *EntityHandle        { return e.handle }
func (e *tickEntityEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *tickEntityEntity) Rotation() cube.Rotation { return e.data.Rot }
func (e *tickEntityEntity) Tick(tx *Tx, _ int64) {
	b := e.data.Data.(*tickEntityBehaviour)
	b.log.names = append(b.log.names, b.name)
	if b.onTick != nil {
		b.onTick(tx, e)
	}
}

// tickEntityViewer records the entity visibility calls made to it, in order.
type tickEntityViewer struct {
	NopViewer
	calls []string
}

func (v *tickEntityViewer) reset()                  { v.calls = nil }
func (v *tickEntityViewer) ViewEntity(Entity)       { v.calls = append(v.calls, "ViewEntity") }
func (v *tickEntityViewer) HideEntity(Entity)       { v.calls = append(v.calls, "HideEntity") }
func (v *tickEntityViewer) ViewEntityItems(Entity)  { v.calls = append(v.calls, "ViewEntityItems") }
func (v *tickEntityViewer) ViewEntityArmour(Entity) { v.calls = append(v.calls, "ViewEntityArmour") }
