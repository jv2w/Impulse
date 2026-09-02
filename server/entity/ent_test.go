package entity

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
)

// entTick is the amount Ent.Tick adds to the entity age per call, and the
// amount SetOnFire subtracts from the fire duration per call.
const entTick = time.Second / 20

// entLog records observable events in call order. The viewer and the behaviour
// below share one, so a single slice pins how viewer callbacks and behaviour
// callbacks interleave inside one Ent.Tick.
type entLog struct{ events []string }

func (l *entLog) add(format string, a ...any) {
	l.events = append(l.events, fmt.Sprintf(format, a...))
}

func (l *entLog) reset() { l.events = nil }

func (l *entLog) count(event string) int {
	n := 0
	for _, e := range l.events {
		if e == event {
			n++
		}
	}
	return n
}

// entLogViewer appends the entity updates it receives to a shared entLog and
// keeps the payloads that assertions need separately.
type entLogViewer struct {
	world.NopViewer

	log       *entLog
	movements []mgl64.Vec3
	teleports []mgl64.Vec3
}

func (v *entLogViewer) ViewEntity(world.Entity)      { v.log.add("show") }
func (v *entLogViewer) HideEntity(world.Entity)      { v.log.add("hide") }
func (v *entLogViewer) ViewEntityState(world.Entity) { v.log.add("state") }

func (v *entLogViewer) ViewEntityMovement(_ world.Entity, pos mgl64.Vec3, _ cube.Rotation, _ bool) {
	v.movements = append(v.movements, pos)
	v.log.add("movement")
}

func (v *entLogViewer) ViewEntityVelocity(world.Entity, mgl64.Vec3) { v.log.add("velocity") }

func (v *entLogViewer) ViewEntityTeleport(_ world.Entity, pos mgl64.Vec3) {
	v.teleports = append(v.teleports, pos)
	v.log.add("teleport")
}

func (v *entLogViewer) ViewSound(_ mgl64.Vec3, s world.Sound) { v.log.add("sound(%T)", s) }

// entBehaviour is a Behaviour whose tick is scripted by the test. It records the
// entity state it observed on every call, which is what pins where in Ent.Tick
// the behaviour is invoked relative to the fire decrement and the age increment.
type entBehaviour struct {
	BaseBehaviour

	log   *entLog
	tick  func(b *entBehaviour, e *Ent, tx *world.Tx) *Movement
	ticks int
	ages  []time.Duration
	fires []time.Duration
}

func (b *entBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.ticks++
	b.ages = append(b.ages, e.Age())
	b.fires = append(b.fires, e.OnFireDuration())
	if b.log != nil {
		b.log.add("behaviour(age=%v, fire=%v)", e.Age(), e.OnFireDuration())
	}
	if b.tick == nil {
		return nil
	}
	return b.tick(b, e, tx)
}

// entConfig applies a behaviour built by the test, so the test keeps a pointer
// to it after the handle is created.
type entConfig struct{ b *entBehaviour }

func (c entConfig) Apply(data *world.EntityData) { data.Data = c.b }

// entDataConfig applies arbitrary entity data, including data that is not a
// Behaviour at all.
type entDataConfig struct{ data any }

func (c entDataConfig) Apply(data *world.EntityData) { data.Data = c.data }

// entSpawn returns a handle for an entity at pos driven by b.
func entSpawn(pos mgl64.Vec3, b *entBehaviour) *world.EntityHandle {
	return world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, entConfig{b})
}

// entMove returns a scripted tick that shifts the entity by dpos and reports the
// movement to the viewers of the position it left, the way every production
// behaviour that moves does.
func entMove(dpos, dvel mgl64.Vec3) func(*entBehaviour, *Ent, *world.Tx) *Movement {
	return func(_ *entBehaviour, e *Ent, tx *world.Tx) *Movement {
		old := e.data.Pos
		viewers := tx.Viewers(old)
		e.data.Pos = old.Add(dpos)
		return &Movement{v: viewers, e: e, pos: e.data.Pos, vel: e.data.Vel, dpos: dpos, dvel: dvel, rot: e.data.Rot}
	}
}

// entRecover returns the value f panicked with, or nil if f returned normally.
func entRecover(f func()) (recovered any) {
	defer func() {
		recovered = recover()
	}()
	f()
	return nil
}

func entAssertLog(t *testing.T, log *entLog, want []string) {
	t.Helper()
	if !slices.Equal(log.events, want) {
		t.Fatalf("events = %v, want %v", log.events, want)
	}
}

// entHandlesWithin collects the handles tx.EntitiesWithin yields for box, in
// iteration order.
func entHandlesWithin(tx *world.Tx, box cube.BBox) []*world.EntityHandle {
	var handles []*world.EntityHandle
	for e := range tx.EntitiesWithin(box) {
		handles = append(handles, e.H())
	}
	return handles
}

// entLabels names handles for assertion messages.
func entLabels(handles []*world.EntityHandle, named map[*world.EntityHandle]string) []string {
	labels := make([]string, len(handles))
	for i, h := range handles {
		if name, ok := named[h]; ok {
			labels[i] = name
			continue
		}
		labels[i] = "unknown"
	}
	return labels
}

// entChunkViewer records which entities a chunk viewer is shown and hidden, on
// top of the movements movementViewer already records.
type entChunkViewer struct {
	movementViewer

	shown, hidden []*world.EntityHandle
}

func (v *entChunkViewer) ViewEntity(e world.Entity) { v.shown = append(v.shown, e.H()) }
func (v *entChunkViewer) HideEntity(e world.Entity) { v.hidden = append(v.hidden, e.H()) }

// entViewChunk registers v as a viewer of exactly the one chunk containing pos.
// A radius of 0 keeps every test viewer confined to a single chunk, which is
// what makes the chunk handover assertions below observable.
func entViewChunk(tx *world.Tx, v world.Viewer, pos mgl64.Vec3) {
	l := world.NewLoader(0, tx.World(), v)
	l.Move(tx, pos)
	l.Load(tx, 4)
}

// entOpen returns the *Ent for handle in tx, failing the test if the entity is
// not in the transaction's world.
func entOpen(t *testing.T, tx *world.Tx, handle *world.EntityHandle) *Ent {
	t.Helper()
	e, ok := handle.Entity(tx)
	if !ok {
		t.Fatal("handle.Entity() ok = false, want the entity to be in this world")
	}
	ent, ok := e.(*Ent)
	if !ok {
		t.Fatalf("handle.Entity() type = %T, want *Ent", e)
	}
	return ent
}

func TestEntTickStepOrder(t *testing.T) {
	// Ent.Tick is a fixed sequence: the fire duration is decremented (and its
	// state broadcast sent) before the behaviour runs, and Movement.Send runs
	// after it. The behaviour reads Age() before Ent.Tick advances it, so the
	// first tick always sees 0 - StationaryBehaviour's spawn sounds and
	// AreaEffectCloudBehaviour both depend on that.
	tests := []struct {
		name     string
		fire     time.Duration
		tick     func(*entBehaviour, *Ent, *world.Tx) *Movement
		want     []string
		wantFire time.Duration
	}{
		{
			name: "fire going out broadcasts state before the behaviour runs",
			fire: entTick,
			tick: entMove(mgl64.Vec3{1}, mgl64.Vec3{}),
			want: []string{"state", "behaviour(age=0s, fire=0s)", "movement"},
		},
		{
			name:     "a fire decrement without a state transition is silent",
			fire:     3 * entTick,
			tick:     entMove(mgl64.Vec3{1}, mgl64.Vec3{}),
			want:     []string{"behaviour(age=0s, fire=100ms)", "movement"},
			wantFire: 2 * entTick,
		},
		{
			name: "a nil movement skips Send entirely",
			want: []string{"behaviour(age=0s, fire=0s)"},
		},
		{
			name: "velocity follows movement within one Send",
			tick: entMove(mgl64.Vec3{1}, mgl64.Vec3{0, 1}),
			want: []string{"behaviour(age=0s, fire=0s)", "movement", "velocity"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			log := &entLog{}
			b := &entBehaviour{log: log, tick: test.tick}
			handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
			mustDo(t, w, func(tx *world.Tx) {
				addViewer(tx, &entLogViewer{log: log})
				e := tx.AddEntity(handle).(*Ent)
				e.data.FireDuration = test.fire
				log.reset()

				e.Tick(tx, 1)

				entAssertLog(t, log, test.want)
				if got := e.Age(); got != entTick {
					t.Fatalf("Age() after one tick = %v, want %v", got, entTick)
				}
				if got := e.OnFireDuration(); got != test.wantFire {
					t.Fatalf("OnFireDuration() after one tick = %v, want %v", got, test.wantFire)
				}
			})
		})
	}
}

func TestEntTickIncrementsAgeAfterTheBehaviour(t *testing.T) {
	w := syncWorld(t)
	b := &entBehaviour{}
	handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		for i := range 3 {
			e.Tick(tx, int64(i+1))
		}
		want := []time.Duration{0, entTick, 2 * entTick}
		if !slices.Equal(b.ages, want) {
			t.Fatalf("ages observed by the behaviour = %v, want %v", b.ages, want)
		}
		if got := e.Age(); got != 3*entTick {
			t.Fatalf("Age() after three ticks = %v, want %v", got, 3*entTick)
		}
	})
}

func TestEntTickVoidCloseOnlyOnEveryTenthTick(t *testing.T) {
	// An entity below the world minimum is only removed on ticks where the
	// caller's tick number is a multiple of ten. On every other tick it lives on
	// and is ticked normally, so it can fall for up to nine more ticks.
	tests := []struct {
		name       string
		current    int64
		wantClosed bool
	}{
		{name: "tick zero", current: 0, wantClosed: true},
		{name: "tick one", current: 1},
		{name: "tick five", current: 5},
		{name: "tick nine", current: 9},
		{name: "tick ten", current: 10, wantClosed: true},
		{name: "tick nineteen", current: 19},
		{name: "tick twenty", current: 20, wantClosed: true},
		{name: "negative multiple of ten", current: -10, wantClosed: true},
		{name: "negative non-multiple", current: -5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			b := &entBehaviour{}
			handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
			mustDo(t, w, func(tx *world.Tx) {
				e := tx.AddEntity(handle).(*Ent)
				e.data.FireDuration = time.Second
				e.data.Pos[1] = float64(tx.Range()[0]) - 1

				e.Tick(tx, test.current)

				if got := handle.Closed(); got != test.wantClosed {
					t.Fatalf("handle.Closed() after Tick(tx, %d) below the world = %v, want %v", test.current, got, test.wantClosed)
				}
				if test.wantClosed {
					// The void check returns before SetOnFire, the behaviour and
					// the age increment: nothing else in Tick runs.
					if b.ticks != 0 {
						t.Fatalf("behaviour ticks after a void close = %d, want 0", b.ticks)
					}
					if got := e.Age(); got != 0 {
						t.Fatalf("Age() after a void close = %v, want 0", got)
					}
					if got := e.OnFireDuration(); got != time.Second {
						t.Fatalf("OnFireDuration() after a void close = %v, want %v unchanged", got, time.Second)
					}
					return
				}
				if b.ticks != 1 {
					t.Fatalf("behaviour ticks on a surviving void tick = %d, want 1", b.ticks)
				}
				if got := e.Age(); got != entTick {
					t.Fatalf("Age() on a surviving void tick = %v, want %v", got, entTick)
				}
			})
		})
	}
}

func TestEntTickVoidBoundaryIsStrictlyBelowTheRange(t *testing.T) {
	// The comparison is y < Range()[0]: an entity standing exactly on the lowest
	// buildable Y is not in the void.
	tests := []struct {
		name       string
		offset     float64
		wantClosed bool
	}{
		{name: "exactly at the world minimum", offset: 0},
		{name: "one ten-thousandth below the world minimum", offset: -0.0001, wantClosed: true},
		{name: "a block above the world minimum", offset: 1},
		{name: "far below the world minimum", offset: -100, wantClosed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, &entBehaviour{})
			mustDo(t, w, func(tx *world.Tx) {
				e := tx.AddEntity(handle).(*Ent)
				e.data.Pos[1] = float64(tx.Range()[0]) + test.offset

				e.Tick(tx, 10)

				if got := handle.Closed(); got != test.wantClosed {
					t.Fatalf("handle.Closed() at y = %v (range minimum %v) = %v, want %v",
						e.data.Pos[1], tx.Range()[0], got, test.wantClosed)
				}
			})
		})
	}
}

func TestEntTickVoidUsesTheCallerTickNotTheEntityAge(t *testing.T) {
	// current is whatever the caller passes. A world that keeps handing the same
	// tick number to Ent.Tick never removes a void entity, however old it gets.
	t.Run("twelve ticks all numbered one never close", func(t *testing.T) {
		w := syncWorld(t)
		b := &entBehaviour{}
		handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
		mustDo(t, w, func(tx *world.Tx) {
			e := tx.AddEntity(handle).(*Ent)
			e.data.Pos[1] = float64(tx.Range()[0]) - 1
			for range 12 {
				e.Tick(tx, 1)
			}
			if handle.Closed() {
				t.Fatal("handle.Closed() = true after twelve ticks numbered 1, want false")
			}
			if b.ticks != 12 {
				t.Fatalf("behaviour ticks = %d, want 12", b.ticks)
			}
			if got := e.Age(); got != 12*entTick {
				t.Fatalf("Age() = %v, want %v", got, 12*entTick)
			}
		})
	})

	t.Run("a brand new entity closes on tick ten", func(t *testing.T) {
		w := syncWorld(t)
		handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, &entBehaviour{})
		mustDo(t, w, func(tx *world.Tx) {
			e := tx.AddEntity(handle).(*Ent)
			e.data.Pos[1] = float64(tx.Range()[0]) - 1
			e.Tick(tx, 10)
			if !handle.Closed() {
				t.Fatal("handle.Closed() = false after one Tick(tx, 10) at age 0, want true")
			}
		})
	})
}

func TestEntTickFireDecrementsOneTickAndClampsAtZero(t *testing.T) {
	// SetOnFire clamps at 0 and only broadcasts on the on/off transition, so a
	// burning entity produces exactly one state broadcast when it goes out and
	// none while it keeps burning or stays extinguished.
	w := syncWorld(t)
	log := &entLog{}
	b := &entBehaviour{}
	handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, &entLogViewer{log: log})
		e := tx.AddEntity(handle).(*Ent)
		e.data.FireDuration = 2 * entTick
		log.reset()

		for i := range 4 {
			e.Tick(tx, int64(i+1))
		}

		want := []time.Duration{entTick, 0, 0, 0}
		if !slices.Equal(b.fires, want) {
			t.Fatalf("fire durations observed by the behaviour = %v, want %v", b.fires, want)
		}
		if got := log.count("state"); got != 1 {
			t.Fatalf("ViewEntityState calls over four ticks = %d, want 1 (only the on to off transition)", got)
		}

		e.SetOnFire(time.Second)
		if got := log.count("state"); got != 2 {
			t.Fatalf("ViewEntityState calls after re-igniting = %d, want 2 (off to on broadcasts too)", got)
		}
		e.SetOnFire(2 * time.Second)
		if got := log.count("state"); got != 2 {
			t.Fatalf("ViewEntityState calls after raising the duration while burning = %d, want 2 unchanged", got)
		}
	})
}

func TestEntTickContinuesAfterTheBehaviourClosesTheEntity(t *testing.T) {
	// PINNED BUG: Ent.Tick keeps going after the behaviour closed the entity, so
	// viewers receive a movement for an entity that was just removed and its age
	// still advances. Pinned as-is; decide in stage 1.
	w := syncWorld(t)
	log := &entLog{}
	b := &entBehaviour{log: log, tick: func(_ *entBehaviour, e *Ent, tx *world.Tx) *Movement {
		old := e.data.Pos
		viewers := tx.Viewers(old)
		e.data.Pos = old.Add(mgl64.Vec3{1})
		_ = e.Close()
		return &Movement{v: viewers, e: e, pos: e.data.Pos, dpos: mgl64.Vec3{1}, dvel: mgl64.Vec3{0, 1}}
	}}
	handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, &entLogViewer{log: log})
		e := tx.AddEntity(handle).(*Ent)
		log.reset()

		e.Tick(tx, 1)

		// "hide" is RemoveEntity telling the viewer the entity is gone; the
		// movement and velocity that follow it are the defect.
		entAssertLog(t, log, []string{"behaviour(age=0s, fire=0s)", "hide", "movement", "velocity"})
		if !handle.Closed() {
			t.Fatal("handle.Closed() = false after the behaviour closed the entity, want true")
		}
		if got := e.Age(); got != entTick {
			t.Fatalf("Age() after a tick that closed the entity = %v, want %v", got, entTick)
		}
	})
}

func TestEntChunkCrossingRelocatesOnTheFollowingTick(t *testing.T) {
	// Stage 2.1 replaces the per-chunk entity slice with a spatial index; these
	// assertions are exactly the contract it must preserve.
	//
	// An entity that writes a position in a new chunk stays registered in its old
	// chunk for the rest of that tick. World.tickEntities relocates it at the top
	// of the NEXT tick, which is also when the viewer handover happens. Both are
	// observable: EntitiesWithin walks chunks x ascending then z ascending and
	// yields insertion order within a chunk, so the order of two entities flips
	// exactly on the relocation tick.
	w := syncWorld(t)
	vOld, vNew := &entChunkViewer{}, &entChunkViewer{}
	mover := entSpawn(mgl64.Vec3{15.5, 100, 0.5}, &entBehaviour{
		tick: func(b *entBehaviour, e *Ent, _ *world.Tx) *Movement {
			if b.ticks == 1 {
				e.data.Pos = e.data.Pos.Add(mgl64.Vec3{1})
			}
			return nil
		},
	})
	anchor := entSpawn(mgl64.Vec3{17.5, 100, 0.5}, &entBehaviour{})
	names := map[*world.EntityHandle]string{mover: "mover", anchor: "anchor"}
	// A box spanning chunk (0,0) and chunk (1,0) only.
	box := cube.Box(0.1, 0, 0.1, 31.9, 200, 15.9)

	mustDo(t, w, func(tx *world.Tx) {
		entViewChunk(tx, vOld, mgl64.Vec3{0.5, 100, 0.5})
		entViewChunk(tx, vNew, mgl64.Vec3{17.5, 100, 0.5})
		// The anchor goes into chunk (1,0) first so that the relocated mover is
		// appended after it, flipping the iteration order.
		tx.AddEntity(anchor)
		tx.AddEntity(mover)

		if got, want := entHandlesWithin(tx, box), []*world.EntityHandle{mover, anchor}; !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin order before the crossing = %v, want [mover anchor]", entLabels(got, names))
		}
	})

	w.AdvanceTick()
	mustDo(t, w, func(tx *world.Tx) {
		e := entOpen(t, tx, mover)
		if got, want := e.Position(), (mgl64.Vec3{16.5, 100, 0.5}); !got.ApproxEqualThreshold(want, physEpsilon) {
			t.Fatalf("position after the moving tick = %v, want %v", got, want)
		}
		// The position is in chunk (1,0), but the handle is still in chunk (0,0).
		if got, want := entHandlesWithin(tx, box), []*world.EntityHandle{mover, anchor}; !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin order on the tick the entity crossed = %v, want [mover anchor] (relocation lags one tick)", entLabels(got, names))
		}
		if len(vOld.hidden) != 0 {
			t.Fatalf("old chunk viewer hid %d entities on the crossing tick, want 0", len(vOld.hidden))
		}
		if got := slices.Contains(vNew.shown, mover); got {
			t.Fatal("new chunk viewer was shown the mover on the crossing tick, want the handover one tick later")
		}
	})

	w.AdvanceTick()
	mustDo(t, w, func(tx *world.Tx) {
		if got, want := entHandlesWithin(tx, box), []*world.EntityHandle{anchor, mover}; !slices.Equal(got, want) {
			t.Fatalf("EntitiesWithin order after relocation = %v, want [anchor mover] (mover appended to chunk (1,0))", entLabels(got, names))
		}
		if got, want := vOld.hidden, []*world.EntityHandle{mover}; !slices.Equal(got, want) {
			t.Fatalf("old chunk viewer hid %v, want only the mover", entLabels(got, names))
		}
		if got, want := vNew.shown, []*world.EntityHandle{anchor, mover}; !slices.Equal(got, want) {
			t.Fatalf("new chunk viewer was shown %v, want [anchor mover]", entLabels(got, names))
		}
	})
}

func TestEntChunkCrossingSendsMovementToTheOldChunkViewers(t *testing.T) {
	// Stage 2.1 replaces the per-chunk entity slice with a spatial index; these
	// assertions are exactly the contract it must preserve.
	//
	// MovementComputer.TickMovement snapshots tx.Viewers of the position it is
	// handed, before the entity moves. The tick on which an entity crosses into a
	// chunk therefore reports to the viewers of the chunk it is leaving, and only
	// the tick after that reports to the viewers of the chunk it entered.
	w := syncWorld(t)
	vOld, vNew := &entChunkViewer{}, &entChunkViewer{}
	handle := world.EntitySpawnOpts{
		Position: mgl64.Vec3{15.5, 100, 0.5},
		Velocity: mgl64.Vec3{1, 0, 0},
	}.New(testMovingEntType{}, PassiveBehaviourConfig{})

	mustDo(t, w, func(tx *world.Tx) {
		entViewChunk(tx, vOld, mgl64.Vec3{0.5, 100, 0.5})
		entViewChunk(tx, vNew, mgl64.Vec3{17.5, 100, 0.5})
		tx.AddEntity(handle)
	})

	steps := []struct {
		name             string
		wantOld, wantNew []mgl64.Vec3
	}{
		{
			name:    "crossing tick reports to the chunk being left",
			wantOld: []mgl64.Vec3{{16.5, 100, 0.5}},
		},
		{
			name:    "the tick after the crossing reports to the chunk entered",
			wantOld: []mgl64.Vec3{{16.5, 100, 0.5}},
			wantNew: []mgl64.Vec3{{17.5, 100, 0.5}},
		},
		{
			name:    "movement inside the new chunk keeps reporting to it",
			wantOld: []mgl64.Vec3{{16.5, 100, 0.5}},
			wantNew: []mgl64.Vec3{{17.5, 100, 0.5}, {18.5, 100, 0.5}},
		},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			w.AdvanceTick()
			entAssertMovements(t, "old chunk viewer", vOld, step.wantOld)
			entAssertMovements(t, "new chunk viewer", vNew, step.wantNew)
		})
	}
}

// entAssertMovements compares the positions v was sent through
// ViewEntityMovement against want, within physEpsilon.
func entAssertMovements(t *testing.T, name string, v *entChunkViewer, want []mgl64.Vec3) {
	t.Helper()
	if len(v.movements) != len(want) {
		t.Fatalf("%s ViewEntityMovement calls = %d, want %d", name, len(v.movements), len(want))
	}
	for i := range want {
		assertVec3(t, fmt.Sprintf("%s movement %d", name, i), v.movements[i].pos, want[i])
	}
}

func TestEntTickLoadsTheChunkAtTheEntityPosition(t *testing.T) {
	// Stage 2.1 replaces the per-chunk entity slice with a spatial index; these
	// assertions are exactly the contract it must preserve.
	//
	// Ent.Tick scans the blocks inside the entity's bounding box for portals on
	// every tick. That scan uses Tx.Block, which loads or generates the chunk, so
	// a single tick pulls the chunk the entity is standing in into memory even
	// when nothing else in the world has ever touched it.
	w := syncWorld(t)
	handle := entSpawn(mgl64.Vec3{8.5, 100, 8.5}, &entBehaviour{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		e.data.Pos = mgl64.Vec3{24.5, 100, 8.5}

		if _, ok := tx.BlockLoaded(cube.Pos{24, 100, 8}); ok {
			t.Fatal("chunk (1,0) was already loaded before the tick, want the test to start with it unloaded")
		}
		e.Tick(tx, 1)
		if _, ok := tx.BlockLoaded(cube.Pos{24, 100, 8}); !ok {
			t.Fatal("chunk (1,0) is still unloaded after one Ent.Tick, want the portal insider scan to have loaded it")
		}
	})
}

func TestEntInUnloadedChunkIsNeitherRelocatedNorTicked(t *testing.T) {
	// Stage 2.1 replaces the per-chunk entity slice with a spatial index; these
	// assertions are exactly the contract it must preserve.
	//
	// World.tickEntities skips an entity whose current chunk is not loaded. It is
	// left registered in the chunk it came from and is not ticked at all, so it
	// silently freezes until something else loads the chunk it moved into.
	w := syncWorld(t)
	b := &entBehaviour{}
	handle := entSpawn(mgl64.Vec3{8.5, 100, 8.5}, b)
	// Boxes spanning chunks (0,0) and (1,0), and chunk (1,0) alone.
	both := cube.Box(0.1, 0, 0.1, 31.9, 200, 15.9)
	newChunk := cube.Box(16.1, 0, 0.1, 31.9, 200, 15.9)

	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		e.data.Pos = mgl64.Vec3{24.5, 100, 8.5}
		if _, ok := tx.BlockLoaded(cube.Pos{24, 100, 8}); ok {
			t.Fatal("chunk (1,0) was already loaded, want the test to start with it unloaded")
		}
	})

	w.AdvanceTick()
	mustDo(t, w, func(tx *world.Tx) {
		if b.ticks != 0 {
			t.Fatalf("behaviour ticks while the entity's chunk is unloaded = %d, want 0", b.ticks)
		}
		if _, ok := tx.BlockLoaded(cube.Pos{24, 100, 8}); ok {
			t.Fatal("chunk (1,0) was loaded by the world tick, want an entity in an unloaded chunk to be skipped entirely")
		}
		// Chunk (1,0) is not loaded, so the only slice this handle can be yielded
		// from is the one belonging to the chunk it started in.
		if got := entHandlesWithin(tx, both); !slices.Equal(got, []*world.EntityHandle{handle}) {
			t.Fatalf("handles in chunks (0,0)+(1,0) = %d, want the entity still registered in its old chunk", len(got))
		}
		if got := entHandlesWithin(tx, newChunk); len(got) != 0 {
			t.Fatalf("handles in chunk (1,0) = %d, want 0 while that chunk is unloaded", len(got))
		}

		// Loading the chunk by any other means unblocks the entity.
		tx.Block(cube.Pos{24, 100, 8})
		if got := entHandlesWithin(tx, newChunk); len(got) != 0 {
			t.Fatalf("handles in chunk (1,0) right after loading it = %d, want 0 until the next tick relocates the entity", len(got))
		}
	})

	w.AdvanceTick()
	mustDo(t, w, func(tx *world.Tx) {
		if b.ticks != 1 {
			t.Fatalf("behaviour ticks after the chunk was loaded = %d, want 1", b.ticks)
		}
		if got := entHandlesWithin(tx, newChunk); !slices.Equal(got, []*world.EntityHandle{handle}) {
			t.Fatalf("handles in chunk (1,0) after relocation = %d, want the entity", len(got))
		}
	})
}

func TestEntCloseIsIdempotent(t *testing.T) {
	w := syncWorld(t)
	log := &entLog{}
	handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, &entBehaviour{})
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, &entLogViewer{log: log})
		e := tx.AddEntity(handle).(*Ent)
		log.reset()

		if err := e.Close(); err != nil {
			t.Fatalf("first Close() = %v, want nil", err)
		}
		entAssertLog(t, log, []string{"hide"})

		if err := e.Close(); err != nil {
			t.Fatalf("second Close() = %v, want nil", err)
		}
		entAssertLog(t, log, []string{"hide"})
		if !handle.Closed() {
			t.Fatal("handle.Closed() = false after Close(), want true")
		}
	})
}

func TestEntReadersAfterClose(t *testing.T) {
	// Close removes the entity from the world but leaves the EntityData in place,
	// so every reader keeps answering with the state the entity had.
	w := syncWorld(t)
	b := &entBehaviour{}
	handle := world.EntitySpawnOpts{
		Position: mgl64.Vec3{8.5, 100, 8.5},
		Rotation: cube.Rotation{45, 10},
		Velocity: mgl64.Vec3{1, 2, 3},
		NameTag:  "pinned",
	}.New(testMovingEntType{}, entConfig{b})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		e.data.FireDuration = time.Second
		e.data.Age = 2 * time.Second
		if err := e.Close(); err != nil {
			t.Fatalf("Close() = %v, want nil", err)
		}

		assertVec3(t, "Position() after Close()", e.Position(), mgl64.Vec3{8.5, 100, 8.5})
		assertVec3(t, "Velocity() after Close()", e.Velocity(), mgl64.Vec3{1, 2, 3})
		if got, want := e.Rotation(), (cube.Rotation{45, 10}); got != want {
			t.Fatalf("Rotation() after Close() = %v, want %v", got, want)
		}
		if got := e.Age(); got != 2*time.Second {
			t.Fatalf("Age() after Close() = %v, want %v", got, 2*time.Second)
		}
		if got := e.NameTag(); got != "pinned" {
			t.Fatalf("NameTag() after Close() = %q, want %q", got, "pinned")
		}
		if got := e.OnFireDuration(); got != time.Second {
			t.Fatalf("OnFireDuration() after Close() = %v, want %v", got, time.Second)
		}
		if !e.AlwaysShowNameTag() {
			t.Fatal("AlwaysShowNameTag() after Close() = false, want the spawn default true")
		}
		if got := e.H(); got != handle {
			t.Fatalf("H() after Close() = %p, want the original handle %p", got, handle)
		}
		if got := e.Behaviour(); got != Behaviour(b) {
			t.Fatalf("Behaviour() after Close() = %v, want the original behaviour", got)
		}

		// The handle no longer resolves in the world it was closed out of.
		if ent, ok := handle.Entity(tx); ok || ent != nil {
			t.Fatalf("handle.Entity() after Close() = %v, %v, want nil, false", ent, ok)
		}
	})
}

func TestEntMutatorsAfterClose(t *testing.T) {
	// PINNED BUG: none of the mutators check whether the entity is still in the
	// world. They keep writing EntityData and keep resolving tx.Viewers of the
	// entity's chunk, so viewers that were told to hide the entity by Close then
	// receive teleports and state updates for it. Pinned as-is; decide in stage 1.
	tests := []struct {
		name       string
		mutate     func(e *Ent)
		wantEvents []string
		check      func(t *testing.T, e *Ent)
	}{
		{
			name:   "SetVelocity writes without notifying anyone",
			mutate: func(e *Ent) { e.SetVelocity(mgl64.Vec3{9, 9, 9}) },
			check: func(t *testing.T, e *Ent) {
				assertVec3(t, "Velocity()", e.Velocity(), mgl64.Vec3{9, 9, 9})
			},
		},
		{
			name:       "Teleport moves the entity and notifies the chunk viewers",
			mutate:     func(e *Ent) { e.Teleport(mgl64.Vec3{1.5, 100, 1.5}) },
			wantEvents: []string{"teleport"},
			check: func(t *testing.T, e *Ent) {
				assertVec3(t, "Position()", e.Position(), mgl64.Vec3{1.5, 100, 1.5})
			},
		},
		{
			name:       "SetNameTag broadcasts state",
			mutate:     func(e *Ent) { e.SetNameTag("renamed") },
			wantEvents: []string{"state"},
			check: func(t *testing.T, e *Ent) {
				if got := e.NameTag(); got != "renamed" {
					t.Fatalf("NameTag() = %q, want %q", got, "renamed")
				}
			},
		},
		{
			name:       "SetAlwaysShowNameTag broadcasts state",
			mutate:     func(e *Ent) { e.SetAlwaysShowNameTag(false) },
			wantEvents: []string{"state"},
			check: func(t *testing.T, e *Ent) {
				if e.AlwaysShowNameTag() {
					t.Fatal("AlwaysShowNameTag() = true, want false")
				}
			},
		},
		{
			name:       "SetOnFire broadcasts state on the transition",
			mutate:     func(e *Ent) { e.SetOnFire(time.Second) },
			wantEvents: []string{"state"},
			check: func(t *testing.T, e *Ent) {
				if got := e.OnFireDuration(); got != time.Second {
					t.Fatalf("OnFireDuration() = %v, want %v", got, time.Second)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			log := &entLog{}
			handle := entSpawn(mgl64.Vec3{8.5, 100, 8.5}, &entBehaviour{})
			mustDo(t, w, func(tx *world.Tx) {
				addViewer(tx, &entLogViewer{log: log})
				e := tx.AddEntity(handle).(*Ent)
				if err := e.Close(); err != nil {
					t.Fatalf("Close() = %v, want nil", err)
				}
				log.reset()

				if recovered := entRecover(func() { test.mutate(e) }); recovered != nil {
					t.Fatalf("mutator on a closed entity panicked with %v, want it to complete", recovered)
				}
				entAssertLog(t, log, test.wantEvents)
				if test.check != nil {
					test.check(t, e)
				}
			})
		})
	}
}

func TestEntTickOnAClosedEntity(t *testing.T) {
	t.Run("a closed entity is still ticked in full", func(t *testing.T) {
		w := syncWorld(t)
		log := &entLog{}
		b := &entBehaviour{log: log, tick: entMove(mgl64.Vec3{1}, mgl64.Vec3{})}
		handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, &entLogViewer{log: log})
			e := tx.AddEntity(handle).(*Ent)
			_ = e.Close()
			log.reset()

			e.Tick(tx, 1)

			entAssertLog(t, log, []string{"behaviour(age=0s, fire=0s)", "movement"})
			if got := e.Age(); got != entTick {
				t.Fatalf("Age() after ticking a closed entity = %v, want %v", got, entTick)
			}
		})
	})

	t.Run("the void check short circuits on an already closed entity", func(t *testing.T) {
		w := syncWorld(t)
		b := &entBehaviour{}
		handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, b)
		mustDo(t, w, func(tx *world.Tx) {
			e := tx.AddEntity(handle).(*Ent)
			_ = e.Close()
			e.data.Pos[1] = float64(tx.Range()[0]) - 1

			e.Tick(tx, 10)

			if b.ticks != 0 {
				t.Fatalf("behaviour ticks = %d, want 0: the void branch returns before the behaviour", b.ticks)
			}
			if got := e.Age(); got != 0 {
				t.Fatalf("Age() = %v, want 0: the void branch returns before the age increment", got)
			}
		})
	})
}

func TestEntTeleportNotifiesTheViewersOfTheOldChunk(t *testing.T) {
	// Stage 2.1 replaces the per-chunk entity slice with a spatial index; these
	// assertions are exactly the contract it must preserve.
	//
	// Teleport resolves tx.Viewers before it writes the new position, so a
	// teleport across a chunk border tells the chunk being left about the
	// destination and tells the destination chunk nothing at all.
	w := syncWorld(t)
	oldLog, newLog := &entLog{}, &entLog{}
	vOld, vNew := &entLogViewer{log: oldLog}, &entLogViewer{log: newLog}
	handle := entSpawn(mgl64.Vec3{8.5, 100, 8.5}, &entBehaviour{})
	mustDo(t, w, func(tx *world.Tx) {
		entViewChunk(tx, vOld, mgl64.Vec3{8.5, 100, 8.5})
		entViewChunk(tx, vNew, mgl64.Vec3{24.5, 100, 8.5})
		e := tx.AddEntity(handle).(*Ent)

		e.Teleport(mgl64.Vec3{24.5, 100, 8.5})

		if len(vOld.teleports) != 1 {
			t.Fatalf("old chunk viewer ViewEntityTeleport calls = %d, want 1", len(vOld.teleports))
		}
		assertVec3(t, "old chunk viewer teleport position", vOld.teleports[0], mgl64.Vec3{24.5, 100, 8.5})
		if len(vNew.teleports) != 0 {
			t.Fatalf("destination chunk viewer ViewEntityTeleport calls = %d, want 0", len(vNew.teleports))
		}
		assertVec3(t, "Position() after Teleport()", e.Position(), mgl64.Vec3{24.5, 100, 8.5})
	})
}

func TestEntUpdateStateAsymmetry(t *testing.T) {
	// SetNameTag and SetAlwaysShowNameTag broadcast unconditionally, even when the
	// value they write is the value that was already there. SetOnFire broadcasts
	// only when the entity crosses between burning and not burning.
	tests := []struct {
		name      string
		pre       func(e *Ent)
		mutate    func(e *Ent)
		wantState int
		check     func(t *testing.T, e *Ent)
	}{
		{name: "SetNameTag with a new value", mutate: func(e *Ent) { e.SetNameTag("x") }, wantState: 1},
		{name: "SetNameTag with the value already set", mutate: func(e *Ent) { e.SetNameTag("") }, wantState: 1},
		{name: "SetAlwaysShowNameTag with a new value", mutate: func(e *Ent) { e.SetAlwaysShowNameTag(false) }, wantState: 1},
		{name: "SetAlwaysShowNameTag with the value already set", mutate: func(e *Ent) { e.SetAlwaysShowNameTag(true) }, wantState: 1},
		{name: "SetOnFire from off to off", mutate: func(e *Ent) { e.SetOnFire(0) }},
		{name: "SetOnFire from off to on", mutate: func(e *Ent) { e.SetOnFire(time.Second) }, wantState: 1},
		{
			name:   "SetOnFire from on to on",
			pre:    func(e *Ent) { e.SetOnFire(time.Second) },
			mutate: func(e *Ent) { e.SetOnFire(2 * time.Second) },
		},
		{
			name:      "SetOnFire from on to off",
			pre:       func(e *Ent) { e.SetOnFire(time.Second) },
			mutate:    func(e *Ent) { e.SetOnFire(0) },
			wantState: 1,
		},
		{
			name:      "Extinguish while burning",
			pre:       func(e *Ent) { e.SetOnFire(time.Second) },
			mutate:    func(e *Ent) { e.Extinguish() },
			wantState: 1,
			check: func(t *testing.T, e *Ent) {
				if got := e.OnFireDuration(); got != 0 {
					t.Fatalf("OnFireDuration() = %v, want 0", got)
				}
			},
		},
		{
			name:   "a negative duration clamps to zero without a transition",
			mutate: func(e *Ent) { e.SetOnFire(-time.Second) },
			check: func(t *testing.T, e *Ent) {
				if got := e.OnFireDuration(); got != 0 {
					t.Fatalf("OnFireDuration() = %v, want 0", got)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			log := &entLog{}
			handle := entSpawn(mgl64.Vec3{0.5, 100, 0.5}, &entBehaviour{})
			mustDo(t, w, func(tx *world.Tx) {
				addViewer(tx, &entLogViewer{log: log})
				e := tx.AddEntity(handle).(*Ent)
				if test.pre != nil {
					test.pre(e)
				}
				log.reset()

				test.mutate(e)

				if got := log.count("state"); got != test.wantState {
					t.Fatalf("ViewEntityState calls = %d, want %d", got, test.wantState)
				}
				if test.check != nil {
					test.check(t, e)
				}
			})
		})
	}
}

func TestEntBehaviourPanicsWhenDataIsNotABehaviour(t *testing.T) {
	// PINNED BUG: Ent.Behaviour is an unchecked type assertion on caller-supplied
	// EntityData, so an EntityConfig that stores anything else takes the world
	// goroutine down with a type assertion panic the caller cannot prevent.
	// Pinned as-is; decide in stage 1.
	tests := []struct {
		name string
		data any
	}{
		{name: "nil data"},
		{name: "an empty struct", data: struct{}{}},
		{name: "an int", data: 42},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := &Ent{data: &world.EntityData{Data: test.data}}
			entAssertTypeAssertionPanic(t, entRecover(func() { e.Behaviour() }))
		})
	}

	t.Run("Ent.Tick panics through Behaviour", func(t *testing.T) {
		w := syncWorld(t)
		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{}, entDataConfig{})
		mustDo(t, w, func(tx *world.Tx) {
			e := tx.AddEntity(handle).(*Ent)
			entAssertTypeAssertionPanic(t, entRecover(func() { e.Tick(tx, 1) }))
		})
	})

	t.Run("Ent.Explode panics through Behaviour", func(t *testing.T) {
		e := &Ent{data: &world.EntityData{}}
		entAssertTypeAssertionPanic(t, entRecover(func() {
			e.Explode(world.BlockExplosionSource{Pos: cube.Pos{}}, 1)
		}))
	})
}

func entAssertTypeAssertionPanic(t *testing.T, recovered any) {
	t.Helper()
	err, ok := recovered.(error)
	if !ok {
		t.Fatalf("recovered value = %v (%T), want a runtime type assertion error", recovered, recovered)
	}
	var assertion *runtime.TypeAssertionError
	if !errors.As(err, &assertion) {
		t.Fatalf("recovered error = %v (%T), want *runtime.TypeAssertionError", err, err)
	}
}

func TestEntExplodeForwardsOnlyToBehavioursThatImplementIt(t *testing.T) {
	// Explode is forwarded through an anonymous interface assertion. A behaviour
	// without an Explode method is not an error: the explosion is silently
	// dropped for that entity.
	tests := []struct {
		name    string
		data    any
		wantVel mgl64.Vec3
	}{
		{name: "PassiveBehaviour implements Explode", data: PassiveBehaviourConfig{}.New(), wantVel: mgl64.Vec3{0, 2, 0}},
		{name: "StationaryBehaviour does not", data: StationaryBehaviourConfig{}.New()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{}, entDataConfig{test.data})
			mustDo(t, w, func(tx *world.Tx) {
				e := tx.AddEntity(handle).(*Ent)
				// The source sits half a block directly below the entity, so the
				// normalised direction is exactly +Y and the impact is the speed.
				src := world.BlockExplosionSource{Pos: cube.Pos{0, 99, 0}}
				if recovered := entRecover(func() { e.Explode(src, 2) }); recovered != nil {
					t.Fatalf("Explode() panicked with %v, want it to complete", recovered)
				}
				assertVec3(t, "Velocity() after Explode()", e.Velocity(), test.wantVel)
			})
		})
	}
}

func TestNewBaseBehaviourCarriesAPortalTravelComputer(t *testing.T) {
	b := NewBaseBehaviour()
	if b.portalTravel == nil {
		t.Fatal("NewBaseBehaviour().portalTravel = nil, want a computer built up front")
	}
	if got := b.PortalTravelComputer(); got != b.portalTravel {
		t.Fatalf("PortalTravelComputer() = %p, want the computer the constructor stored %p", got, b.portalTravel)
	}
	// NewPortalTravelComputer's defaults: instant travel, 15 second cooldown.
	if got := b.portalTravel.Cooldown; got != 15*time.Second {
		t.Fatalf("Cooldown = %v, want %v", got, 15*time.Second)
	}
	if b.portalTravel.Instantaneous == nil || !b.portalTravel.Instantaneous(world.Overworld, world.Nether) {
		t.Fatal("Instantaneous(Overworld, Nether) = false, want instant travel by default")
	}
}

func TestBaseBehaviourPortalTravelComputerIsCreatedOnceLazily(t *testing.T) {
	// A zero BaseBehaviour builds its computer on first use. Returning the same
	// pointer afterwards is load-bearing: the cooldown and the four second portal
	// timer live in it, so a second computer would reset both.
	var b BaseBehaviour
	if b.portalTravel != nil {
		t.Fatal("zero BaseBehaviour already has a portal travel computer, want nil until first use")
	}
	first := b.PortalTravelComputer()
	if first == nil {
		t.Fatal("PortalTravelComputer() = nil, want a lazily created computer")
	}
	if second := b.PortalTravelComputer(); second != first {
		t.Fatalf("second PortalTravelComputer() = %p, want the same pointer as the first %p", second, first)
	}
	if got := b.portalTravel; got != first {
		t.Fatalf("portalTravel = %p, want the returned computer %p to have been stored", got, first)
	}
}

func TestStationaryBehaviourConfigDefaults(t *testing.T) {
	if got := (StationaryBehaviourConfig{}).New().conf.ExistenceDuration; got != math.MaxInt64 {
		t.Fatalf("ExistenceDuration for a zero config = %v, want math.MaxInt64", got)
	}
	if got := (StationaryBehaviourConfig{ExistenceDuration: time.Second}).New().conf.ExistenceDuration; got != time.Second {
		t.Fatalf("ExistenceDuration = %v, want %v left alone", got, time.Second)
	}
	if !(StationaryBehaviourConfig{}).New().Immobile() {
		t.Fatal("Immobile() = false, want true unconditionally")
	}
}

func TestStationaryBehaviourSpawnSoundsOnTheFirstTickOnly(t *testing.T) {
	// The spawn sounds are gated on Age() == 0, and Ent.Tick increments the age
	// only after the behaviour returns, so the first tick is the one that plays
	// them. Any rewrite that advances the age first silences every spawn sound.
	w := syncWorld(t)
	log := &entLog{}
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{},
		StationaryBehaviourConfig{SpawnSounds: []world.Sound{sound.Pop{}, sound.Burp{}}})
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, &entLogViewer{log: log})
		e := tx.AddEntity(handle).(*Ent)
		log.reset()

		e.Tick(tx, 1)
		entAssertLog(t, log, []string{"sound(sound.Pop)", "sound(sound.Burp)"})

		for i := range 3 {
			e.Tick(tx, int64(i+2))
		}
		entAssertLog(t, log, []string{"sound(sound.Pop)", "sound(sound.Burp)"})
	})
}

func TestStationaryBehaviourClosesOneTickAfterExpiry(t *testing.T) {
	// close is only set once Age() has passed ExistenceDuration, and it is acted
	// on at the top of the following tick. With a two tick lifetime the entity is
	// still alive at the end of tick 4 and is removed by tick 5.
	w := syncWorld(t)
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{},
		StationaryBehaviourConfig{ExistenceDuration: 2 * entTick})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		for i := range 4 {
			e.Tick(tx, int64(i+1))
			if handle.Closed() {
				t.Fatalf("handle.Closed() = true after tick %d, want the entity to survive through tick 4", i+1)
			}
			if got, want := e.Age(), time.Duration(i+1)*entTick; got != want {
				t.Fatalf("Age() after tick %d = %v, want %v", i+1, got, want)
			}
		}

		e.Tick(tx, 5)
		if !handle.Closed() {
			t.Fatal("handle.Closed() = false after tick 5, want the expired entity removed")
		}
		// PINNED BUG: Ent.Tick keeps going after the behaviour closed the entity,
		// so viewers receive a movement for an entity that was just removed and
		// its age still advances. Pinned as-is; decide in stage 1.
		if got, want := e.Age(), 5*entTick; got != want {
			t.Fatalf("Age() after the closing tick = %v, want %v", got, want)
		}
	})
}

func TestPassiveBehaviourConfigDefaults(t *testing.T) {
	p := PassiveBehaviourConfig{}.New()
	if got := p.conf.ExistenceDuration; got != math.MaxInt64 {
		t.Fatalf("ExistenceDuration for a zero config = %v, want math.MaxInt64", got)
	}
	if got := p.fuse; got != math.MaxInt64 {
		t.Fatalf("fuse for a zero config = %v, want it seeded from ExistenceDuration", got)
	}
	if !p.mc.DragBeforeGravity {
		t.Fatal("DragBeforeGravity = false, want true: every passive entity applies drag first")
	}
}

func TestPassiveBehaviourUpdatesFallDistanceBeforeConfigTick(t *testing.T) {
	// fallDistance accumulates the downward part of this tick's movement and is
	// written before conf.Tick runs, so a Tick hook reading it already sees the
	// current tick's fall included.
	w := syncWorld(t)
	var p *PassiveBehaviour
	var observed []float64
	p = PassiveBehaviourConfig{Gravity: 0.04, Drag: 0.02, Tick: func(*Ent, *world.Tx) {
		observed = append(observed, p.fallDistance)
	}}.New()

	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{}, entDataConfig{p})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		e.Tick(tx, 1)
		e.Tick(tx, 2)
	})

	// Free fall from rest with drag before gravity: -0.04 then -0.0792.
	want := []float64{0.04, 0.04 + 0.0792}
	if len(observed) != len(want) {
		t.Fatalf("conf.Tick calls = %d, want %d", len(observed), len(want))
	}
	for i := range want {
		if math.Abs(observed[i]-want[i]) > physEpsilon {
			t.Fatalf("fallDistance seen by conf.Tick on tick %d = %.17g, want %.17g", i+1, observed[i], want[i])
		}
	}
}

func TestPassiveBehaviourFuse(t *testing.T) {
	t.Run("counts down from ExistenceDuration once an Expire hook is set", func(t *testing.T) {
		w := syncWorld(t)
		var p *PassiveBehaviour
		var observed []time.Duration
		p = PassiveBehaviourConfig{
			ExistenceDuration: time.Second,
			Expire:            func(*Ent, *world.Tx) {},
			Tick:              func(*Ent, *world.Tx) { observed = append(observed, p.Fuse()) },
		}.New()

		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{}, entDataConfig{p})
		mustDo(t, w, func(tx *world.Tx) {
			e := tx.AddEntity(handle).(*Ent)
			for i := range 3 {
				e.Tick(tx, int64(i+1))
			}
		})

		want := []time.Duration{time.Second, time.Second - entTick, time.Second - 2*entTick}
		if !slices.Equal(observed, want) {
			t.Fatalf("Fuse() per tick = %v, want %v", observed, want)
		}
	})

	t.Run("is -1 without an Expire hook", func(t *testing.T) {
		p := PassiveBehaviourConfig{ExistenceDuration: time.Second}.New()
		if got := p.Fuse(); got != -1 {
			t.Fatalf("Fuse() without Expire = %v, want -1", got)
		}
		// The value that makes the periodic broadcast below unreachable.
		if got := time.Duration(-1) % (time.Second / 4); got == 0 {
			t.Fatalf("time.Duration(-1) %% 250ms = %v, want a non-zero remainder", got)
		}
	})
}

func TestPassiveBehaviourPeriodicStateBroadcast(t *testing.T) {
	t.Run("without an Expire hook it never fires", func(t *testing.T) {
		// PINNED BUG: Fuse() returns -1 when no Expire is configured, so the
		// periodic ViewEntityState broadcast never fires for any entity without an
		// Expire hook. Pinned as-is; decide in stage 1.
		//
		// That covers item entities, experience orbs and falling blocks: none of
		// them ever broadcasts its state on a timer.
		w := syncWorld(t)
		log := &entLog{}
		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{},
			PassiveBehaviourConfig{ExistenceDuration: time.Second})
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, &entLogViewer{log: log})
			e := tx.AddEntity(handle).(*Ent)
			log.reset()
			for i := range 25 {
				e.Tick(tx, int64(i+1))
			}
			if got := log.count("state"); got != 0 {
				t.Fatalf("ViewEntityState calls over 25 ticks = %d, want 0", got)
			}
		})
	})

	t.Run("with an Expire hook it fires every fifth tick", func(t *testing.T) {
		// Fuse() counts down 1s, 950ms, 900ms ... and the broadcast fires whenever
		// it is a whole multiple of 250ms, so every fifth tick starting at the
		// first. Expiry then closes the entity one tick after Age passes the
		// ExistenceDuration.
		w := syncWorld(t)
		log := &entLog{}
		var currentTick, expiredOn int
		var stateOn []int
		p := PassiveBehaviourConfig{
			ExistenceDuration: time.Second,
			Expire:            func(*Ent, *world.Tx) { expiredOn = currentTick },
		}.New()
		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(testMovingEntType{}, entDataConfig{p})
		mustDo(t, w, func(tx *world.Tx) {
			addViewer(tx, &entLogViewer{log: log})
			e := tx.AddEntity(handle).(*Ent)
			log.reset()

			for i := range 23 {
				currentTick = i + 1
				before := log.count("state")
				e.Tick(tx, int64(currentTick))
				if log.count("state") > before {
					stateOn = append(stateOn, currentTick)
				}
			}

			want := []int{1, 6, 11, 16, 21}
			if !slices.Equal(stateOn, want) {
				t.Fatalf("ticks that broadcast ViewEntityState = %v, want %v", stateOn, want)
			}
			if expiredOn != 22 {
				t.Fatalf("Expire ran on tick %d, want 22 (the first tick where Age exceeds ExistenceDuration)", expiredOn)
			}
			if !handle.Closed() {
				t.Fatal("handle.Closed() = false after tick 23, want the entity closed one tick after Expire")
			}
		})
	})
}
