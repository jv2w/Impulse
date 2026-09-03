package entity

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// gameRuleScanWindow is the number of ticks scans are counted over. It is a
// multiple of every period the test uses, and counting starts on the tick after
// a scan, so the expected count is exact rather than off by the phase.
const gameRuleScanWindow = 60

// TestPickupScanPeriodDividesTheScans pins the whole point of the rule. The
// collector refuses everything, so it is called once per nearby scan and never
// removes the item: the call count over a fixed window is the number of scans
// that actually ran. A period of P must divide that count by P, which is only
// true if the scan is skipped outright on the ticks in between rather than run
// and its result thrown away.
func TestPickupScanPeriodDividesTheScans(t *testing.T) {
	for _, test := range []struct {
		name   string
		period int
		want   int
	}{
		{name: "unset scans every tick", period: 0, want: gameRuleScanWindow},
		{name: "one scans every tick", period: 1, want: gameRuleScanWindow},
		{name: "three", period: 3, want: gameRuleScanWindow / 3},
		{name: "twenty", period: 20, want: gameRuleScanWindow / 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := gameRuleScansPerWindow(t, test.period); got != test.want {
				t.Fatalf("scans in %d ticks at period %d = %d, want %d",
					gameRuleScanWindow, test.period, got, test.want)
			}
		})
	}
}

// gameRuleScansPerWindow counts the nearby scans a resting item performs over
// gameRuleScanWindow ticks, starting from the tick after a scan.
func gameRuleScansPerWindow(t *testing.T, period int) int {
	t.Helper()

	w := world.Config{Synchronous: true, GameRules: world.GameRules{PickupScanPeriod: period}}.New()
	t.Cleanup(func() { _ = w.Close() })

	pos := mgl64.Vec3{0.5, 64, 0.5}
	c := &gameRuleCollector{}
	w.Do(func(tx *world.Tx) {
		// A floor keeps the item where it was dropped. A falling item drifts out
		// of the collector's range within a few ticks and stops being scanned
		// for reasons that have nothing to do with the period.
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 1)))
		tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(gameRuleCollectorType{c: c}, gameRuleCollectorConf{}))
	})

	// The item is not collectable for the first half second, so scanning has to
	// be waited out rather than assumed to start on tick 1.
	for range 64 {
		w.AdvanceTick()
		if c.calls > 0 {
			break
		}
	}
	if c.calls == 0 {
		t.Fatal("the item never scanned, so the window would count nothing")
	}

	c.calls = 0
	for range gameRuleScanWindow {
		w.AdvanceTick()
	}
	return c.calls
}

// gameRuleCollector counts the Collect calls made to it and refuses every one.
// It is held by the EntityType because world.EntityType.Open builds a fresh
// Entity per transaction, so the Entity itself cannot carry the count.
type gameRuleCollector struct {
	calls int
}

type gameRuleCollectorType struct{ c *gameRuleCollector }

func (t gameRuleCollectorType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &gameRuleCollectorEnt{h: h, d: d, c: t.c}
}
func (gameRuleCollectorType) EncodeEntity() string { return "impulse:pickup_scan_test_collector" }
func (gameRuleCollectorType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (gameRuleCollectorType) DecodeNBT(map[string]any, *world.EntityData) {}
func (gameRuleCollectorType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type gameRuleCollectorConf struct{}

func (gameRuleCollectorConf) Apply(*world.EntityData) {}

type gameRuleCollectorEnt struct {
	h *world.EntityHandle
	d *world.EntityData
	c *gameRuleCollector
}

func (e *gameRuleCollectorEnt) H() *world.EntityHandle  { return e.h }
func (e *gameRuleCollectorEnt) Position() mgl64.Vec3    { return e.d.Pos }
func (e *gameRuleCollectorEnt) Rotation() cube.Rotation { return e.d.Rot }
func (e *gameRuleCollectorEnt) Close() error            { return nil }

// Collect refuses the stack. ItemBehaviour.collect returns without closing the
// item when no items are taken, so the item stays and keeps scanning.
func (e *gameRuleCollectorEnt) Collect(item.Stack) (int, bool) {
	e.c.calls++
	return 0, false
}
