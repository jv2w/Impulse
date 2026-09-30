package playerdb

import (
	"log/slog"
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/player"
	"github.com/jv2w/Impulse/server/world"
)

// TestSaveReplacesNonFiniteFloats stores a record whose position, velocity,
// health and fall distance are non-finite. encoding/json refuses those, so the
// whole record used to go unwritten and everything the player did was lost. The
// record must be written, with the bad values replaced and the rest intact.
func TestSaveReplacesNonFiniteFloats(t *testing.T) {
	p := newTestProvider(t)
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	lookup := func(world.Dimension) *world.World { return w }

	id := uuid.New()
	conf := player.Config{
		UUID:                id,
		Name:                "Steve",
		Position:            mgl64.Vec3{math.NaN(), 70, 0},
		Velocity:            mgl64.Vec3{0, math.Inf(-1), 0},
		Health:              math.NaN(),
		MaxHealth:           20,
		FallDistance:        math.Inf(1),
		Experience:          123,
		Inventory:           inventory.New(36, nil),
		OffHand:             inventory.New(1, nil),
		EnderChestInventory: inventory.New(27, nil),
		Armour:              inventory.NewArmour(nil),
	}
	if err := p.Save(id, conf, w); err != nil {
		t.Fatalf("Save with non-finite floats: %v", err)
	}

	got, _, err := p.Load(id, lookup)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := w.Spawn().Vec3Centre(); got.Position != want {
		t.Errorf("position = %v, want the world spawn %v", got.Position, want)
	}
	if got.Velocity != (mgl64.Vec3{}) {
		t.Errorf("velocity = %v, want the zero vector", got.Velocity)
	}
	if got.Health != 20 {
		t.Errorf("health = %v, want the max health 20", got.Health)
	}
	if got.FallDistance != 0 {
		t.Errorf("fall distance = %v, want 0", got.FallDistance)
	}
	// The fields that were fine must come back untouched: the point of writing
	// the record at all is that the rest of it survives.
	if got.Name != "Steve" {
		t.Errorf("name = %q, want Steve", got.Name)
	}
	if got.Experience != 123 {
		t.Errorf("experience = %v, want 123", got.Experience)
	}
}
