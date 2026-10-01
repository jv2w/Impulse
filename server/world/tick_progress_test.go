package world

import (
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/block/cube"
)

// TestTickProgressViewerlessWorldStops pins the viewer guard: a World with no
// viewers stops ticking once it is past its first tick, unless it is
// Synchronous. The first tick is the exception that lets Config.New initialise
// a world nobody is watching yet.
func TestTickProgressViewerlessWorldStops(t *testing.T) {
	w := Config{Log: tickProgressDiscardLog()}.New()
	t.Cleanup(func() { _ = w.Close() })

	if got := w.currentTick(); got != 1 {
		t.Fatalf("current tick after Config.New = %d, want 1: the CurrentTick == 0 exception should let the first tick through", got)
	}
	// Four tick intervals with no viewers. The tick loop keeps firing; the guard
	// is what has to stop the body.
	time.Sleep(time.Second / 5)
	if got := w.currentTick(); got != 1 {
		t.Fatalf("current tick after 200ms without viewers = %d, want 1", got)
	}
}

// TestTickProgressViewerResumesTicking pins the other side of the viewer guard:
// registering a viewer lets a non-Synchronous World tick again.
func TestTickProgressViewerResumesTicking(t *testing.T) {
	w := Config{Log: tickProgressDiscardLog()}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.addWorldViewer(&Loader{viewer: &tickProgressViewer{}})

	deadline := time.Now().Add(time.Second * 5)
	for w.currentTick() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("current tick stuck at %d with a viewer registered, want it to advance", w.currentTick())
		}
		time.Sleep(time.Millisecond * 10)
	}
}

// TestTickProgressSecondWorldOnSharedSettingsDoesNotAdvance pins that only the
// first World over a Settings owns the counters: World.advance is decided by the
// Settings reference count at construction, so a second World sharing them ticks
// its pipeline but never moves CurrentTick, Time or the weather counters.
func TestTickProgressSecondWorldOnSharedSettingsDoesNotAdvance(t *testing.T) {
	set := defaultSettings()
	set.RainTime, set.ThunderTime = 5000, 6000
	provider := NopProvider{Set: set}

	first := Config{Synchronous: true, Provider: provider, Log: tickProgressDiscardLog()}.New()
	t.Cleanup(func() { _ = first.Close() })
	second := Config{Synchronous: true, Provider: provider, Log: tickProgressDiscardLog()}.New()
	t.Cleanup(func() { _ = second.Close() })

	if !first.advance || second.advance {
		t.Fatalf("advance flags = %t, %t; want the first World to own the Settings", first.advance, second.advance)
	}
	before := tickProgressCounters(set)
	for range 3 {
		second.AdvanceTick()
	}
	if got := tickProgressCounters(set); got != before {
		t.Fatalf("counters after three ticks of the non-advancing World = %v, want %v", got, before)
	}

	first.AdvanceTick()
	want := tickProgressCounterSet{tick: before.tick + 1, time: before.time + 1, rain: before.rain - 1, thunder: before.thunder - 1}
	if got := tickProgressCounters(set); got != want {
		t.Fatalf("counters after one tick of the advancing World = %v, want %v", got, want)
	}
}

// TestTickProgressTimeCycle pins that stopping the time cycle freezes Time while
// leaving CurrentTick running: the two counters advance independently.
func TestTickProgressTimeCycle(t *testing.T) {
	tests := []struct {
		name      string
		cycle     bool
		wantTime  int64
		wantTicks int64
	}{
		{name: "cycle on", cycle: true, wantTime: 5, wantTicks: 5},
		{name: "cycle off", cycle: false, wantTime: 0, wantTicks: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := tickProgressWorld(t)
			if !test.cycle {
				w.StopTime()
			}
			w.SetTime(0)

			startTick := w.currentTick()
			for range 5 {
				w.AdvanceTick()
			}
			if got := int64(w.Time()); got != test.wantTime {
				t.Fatalf("time after five ticks = %d, want %d", got, test.wantTime)
			}
			if got := w.currentTick() - startTick; got != test.wantTicks {
				t.Fatalf("ticks elapsed = %d, want %d", got, test.wantTicks)
			}
		})
	}
}

// TestTickProgressWeatherCycle pins that the weather counters only move while
// the weather cycle is enabled, and that they move by exactly one per tick.
func TestTickProgressWeatherCycle(t *testing.T) {
	tests := []struct {
		name  string
		cycle bool
		want  int64
	}{
		{name: "cycle on", cycle: true, want: 5000 - 5},
		{name: "cycle off", cycle: false, want: 5000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := tickProgressWorld(t)
			if !test.cycle {
				w.StopWeatherCycle()
			}
			w.set.Lock()
			w.set.RainTime, w.set.ThunderTime = 5000, 6000
			w.set.Unlock()

			for range 5 {
				w.AdvanceTick()
			}
			w.set.Lock()
			got := w.set.RainTime
			w.set.Unlock()
			if got != test.want {
				t.Fatalf("rain time after five ticks = %d, want %d", got, test.want)
			}
		})
	}
}

// TestTickProgressWeatherTogglesAtZero pins that a weather counter reaching zero
// flips the state it guards and is reset to a positive value drawn from the
// World's random source.
func TestTickProgressWeatherTogglesAtZero(t *testing.T) {
	w := tickProgressWorld(t)
	w.set.Lock()
	w.set.RainTime, w.set.Raining = 1, false
	w.set.ThunderTime = 100000
	w.set.Unlock()

	w.AdvanceTick()

	w.set.Lock()
	raining, rainTime := w.set.Raining, w.set.RainTime
	w.set.Unlock()
	if !raining {
		t.Fatal("rain did not start when the rain counter reached zero")
	}
	if rainTime <= 0 {
		t.Fatalf("rain time after the toggle = %d, want a positive reset value", rainTime)
	}
}

// TestTickProgressSpawnHeightCorrection pins the spawn clamp: a spawn Y above
// the build limit is rewritten to one above the highest obstructing block.
func TestTickProgressSpawnHeightCorrection(t *testing.T) {
	t.Run("an obstructing block", func(t *testing.T) {
		w := tickProgressWorld(t)
		runWorld(w, func(tx *Tx) { tx.SetBlock(cube.Pos{0, 70, 0}, tickProgressSolid{}, nil) })
		w.SetSpawn(cube.Pos{0, 1000, 0})

		w.AdvanceTick()
		if got := w.Spawn(); got != (cube.Pos{0, 71, 0}) {
			t.Fatalf("corrected spawn = %v, want one above the obstructing block at y 70", got)
		}
	})
	t.Run("an empty column", func(t *testing.T) {
		w := tickProgressWorld(t)
		w.SetSpawn(cube.Pos{0, 1000, 0})

		w.AdvanceTick()
		if got := w.Spawn(); got != (cube.Pos{0, w.Range()[0] + 1, 0}) {
			t.Fatalf("corrected spawn in a void column = %v, want the world floor plus one", got)
		}
	})
}

// TestTickProgressRequiredSleepTicksCountdown pins that the sleep countdown
// drops by exactly one per tick and stops at zero rather than going negative.
func TestTickProgressRequiredSleepTicksCountdown(t *testing.T) {
	w := tickProgressWorld(t)
	w.SetRequiredSleepDuration(time.Millisecond * 150)

	var got []int64
	for range 4 {
		w.AdvanceTick()
		w.set.Lock()
		got = append(got, w.set.RequiredSleepTicks)
		w.set.Unlock()
	}
	if want := []int64{2, 1, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("required sleep ticks per tick = %v, want %v", got, want)
	}
}

// TestTickProgressSleepCountdownAdvancesDayWithoutSleepers pins what happens
// when the countdown expires: tryAdvanceDay runs and, with nobody left sleeping,
// skips straight past its time-of-day guards.
//
// PINNED BUG: tryAdvanceDay's guards live inside the loop over Sleepers, so a
// World whose sleepers all left before the countdown expired advances the day
// unconditionally, at any time of day.
func TestTickProgressSleepCountdownAdvancesDayWithoutSleepers(t *testing.T) {
	w := tickProgressWorld(t)
	w.SetTime(500)
	w.SetRequiredSleepDuration(time.Millisecond * 50)

	w.AdvanceTick()

	// The tick increments Time to 501 before tryAdvanceDay runs, and the day is
	// then rounded up to the next multiple of TimeFull.
	if got := w.Time(); got != TimeFull {
		t.Fatalf("time after the sleep countdown expired = %d, want %d", got, TimeFull)
	}
}

// TestTickProgressViewerUpdatesEveryTwentyTicks pins that time and weather are
// pushed to viewers only on ticks that are a multiple of 20.
func TestTickProgressViewerUpdatesEveryTwentyTicks(t *testing.T) {
	w := tickProgressWorld(t)
	viewer := &tickProgressViewer{}
	w.addWorldViewer(&Loader{viewer: viewer})

	var updatedOn, wantUpdatedOn []int64
	viewer.reset()
	for range 41 {
		before := viewer.times
		w.AdvanceTick()
		tick := w.currentTick()
		if viewer.times > before {
			updatedOn = append(updatedOn, tick)
		}
		if tick%20 == 0 {
			wantUpdatedOn = append(wantUpdatedOn, tick)
		}
	}
	if !slices.Equal(updatedOn, wantUpdatedOn) {
		t.Fatalf("ViewTime called on ticks %v, want %v", updatedOn, wantUpdatedOn)
	}
	if viewer.weathers != viewer.times {
		t.Fatalf("ViewWeather calls = %d, ViewTime calls = %d; want both on every twentieth tick", viewer.weathers, viewer.times)
	}
}

// TestTickProgressViewerUpdatesFollowTimeCycle pins the extra guard on the
// twenty-tick broadcast: time is only sent when the World cycles it, while
// weather is always sent.
func TestTickProgressViewerUpdatesFollowTimeCycle(t *testing.T) {
	t.Run("stopped time cycle drops time but keeps weather", func(t *testing.T) {
		w := tickProgressWorld(t)
		viewer := &tickProgressViewer{}
		w.addWorldViewer(&Loader{viewer: viewer})
		w.StopTime()

		viewer.reset()
		for range 40 {
			w.AdvanceTick()
		}
		if viewer.times != 0 {
			t.Fatalf("ViewTime calls with the time cycle stopped = %d, want 0", viewer.times)
		}
		if viewer.weathers == 0 {
			t.Fatal("ViewWeather was not called; stopping the time cycle should not stop weather updates")
		}
	})
}

// tickProgressWorld returns a Synchronous World with a fixed random source and
// a registry holding the solid marker block.
func tickProgressWorld(t *testing.T) *World {
	t.Helper()

	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_progress_solid", Properties: map[string]any{}})
	registry.RegisterBlock(tickProgressSolid{})

	w := Config{
		Synchronous: true,
		Blocks:      registry,
		RandSource:  rand.NewPCG(0x50524f47, 0x52455353),
		Log:         tickProgressDiscardLog(),
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func tickProgressDiscardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// tickProgressCounterSet is the set of Settings fields that only advance while
// World.advance is set.
type tickProgressCounterSet struct {
	tick, time, rain, thunder int64
}

func tickProgressCounters(set *Settings) tickProgressCounterSet {
	set.Lock()
	defer set.Unlock()
	return tickProgressCounterSet{tick: set.CurrentTick, time: set.Time, rain: set.RainTime, thunder: set.ThunderTime}
}

// tickProgressViewer counts the periodic time and weather broadcasts. It is only
// driven from the goroutine running the Synchronous World it is registered with.
type tickProgressViewer struct {
	NopViewer
	times    int
	weathers int
}

func (v *tickProgressViewer) ViewTime(int)           { v.times++ }
func (v *tickProgressViewer) ViewWeather(bool, bool) { v.weathers++ }
func (v *tickProgressViewer) reset()                 { v.times, v.weathers = 0, 0 }

// tickProgressSolid stands in for an obstructing block: unknownModel reports
// every face as solid, which is what the spawn clamp searches for.
type tickProgressSolid struct{}

func (tickProgressSolid) EncodeBlock() (string, map[string]any) {
	return "test:tick_progress_solid", nil
}
func (tickProgressSolid) Hash() (uint64, uint64) { return 1 << 58, 0 }
func (tickProgressSolid) Model() BlockModel      { return unknownModel{} }
