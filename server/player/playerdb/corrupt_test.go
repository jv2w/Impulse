package playerdb

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"

	"encoding/json"
)

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := NewProvider(t.TempDir())
	if err != nil {
		t.Fatalf("open provider: %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close provider: %v", err)
		}
	})
	return p
}

// putRaw stores an arbitrary player record under id, bypassing Save, so a test
// can plant the corrupt bytes a torn write or a hand edit would leave behind.
func putRaw(t *testing.T, p *Provider, id uuid.UUID, d jsonData) {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := p.db.Put(id[:], b, nil); err != nil {
		t.Fatalf("put record: %v", err)
	}
}

func nilWorld(world.Dimension) *world.World { return nil }

// TestLoadCorruptUUIDFallsBackToKey pins that a record whose embedded UUID
// string no longer parses still loads: the LevelDB key is the authoritative
// identity. Before the fix uuid.MustParse panicked, bricking login for that
// player over a single corrupt field.
func TestLoadCorruptUUIDFallsBackToKey(t *testing.T) {
	p := newTestProvider(t)
	id := uuid.New()
	putRaw(t, p, id, jsonData{UUID: "not-a-uuid", Username: "Steve"})

	conf, _, err := p.Load(id, nilWorld)
	if err != nil {
		t.Fatalf("Load with corrupt UUID: %v", err)
	}
	if conf.UUID != id {
		t.Fatalf("UUID = %v, want key %v", conf.UUID, id)
	}
	if conf.Name != "Steve" {
		t.Fatalf("Name = %q, want Steve: rest of the record must survive", conf.Name)
	}
}

// TestLoadValidUUIDKeepsEmbeddedValue pins behavioral identity for good input:
// a record whose UUID parses loads exactly as before, embedded value intact.
func TestLoadValidUUIDKeepsEmbeddedValue(t *testing.T) {
	p := newTestProvider(t)
	id := uuid.New()
	other := uuid.New()
	putRaw(t, p, id, jsonData{UUID: other.String(), Username: "Alex"})

	conf, _, err := p.Load(id, nilWorld)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if conf.UUID != other {
		t.Fatalf("UUID = %v, want embedded %v", conf.UUID, other)
	}
}

// TestLoadCorruptSlotsDropped pins that slot numbers outside the inventory no
// longer panic the load: the bad entries are dropped and the good ones load.
// Before the fix decodeItems indexed items[i.Slot] unchecked, so Slot 99 or a
// negative Slot crashed the server while opening the save.
func TestLoadCorruptSlotsDropped(t *testing.T) {
	p := newTestProvider(t)
	id := uuid.New()
	good := encodeItem(item.NewStack(item.Diamond{}, 5))
	putRaw(t, p, id, jsonData{
		UUID: id.String(),
		Inventory: jsonInventoryData{
			Items: []jsonSlot{
				{Slot: 99, Item: good},
				{Slot: -5, Item: good},
				{Slot: 1, Item: good},
			},
		},
		EnderChestInventory: []jsonSlot{{Slot: 40, Item: good}},
	})

	conf, _, err := p.Load(id, nilWorld)
	if err != nil {
		t.Fatalf("Load with corrupt slots: %v", err)
	}
	kept, err := conf.Inventory.Item(1)
	if err != nil {
		t.Fatalf("Item(1): %v", err)
	}
	if kept.Empty() || kept.Count() != 5 {
		t.Fatalf("slot 1 = %v, want 5 diamonds: the good entry must survive", kept)
	}
	for _, slot := range []int{0, 2, 35} {
		s, err := conf.Inventory.Item(slot)
		if err != nil {
			t.Fatalf("Item(%d): %v", slot, err)
		}
		if !s.Empty() {
			t.Fatalf("slot %d = %v, want empty", slot, s)
		}
	}
}

// TestLoadMainHandSlotBounds pins that a MainHandSlot outside the 0-8 hotbar
// falls back to slot 0 while a valid value passes through unchanged.
func TestLoadMainHandSlotBounds(t *testing.T) {
	p := newTestProvider(t)

	corruptID := uuid.New()
	putRaw(t, p, corruptID, jsonData{
		UUID:      corruptID.String(),
		Inventory: jsonInventoryData{MainHandSlot: 1<<32 - 1},
	})
	conf, _, err := p.Load(corruptID, nilWorld)
	if err != nil {
		t.Fatalf("Load with corrupt MainHandSlot: %v", err)
	}
	if conf.HeldSlot != 0 {
		t.Fatalf("HeldSlot = %d, want 0", conf.HeldSlot)
	}

	validID := uuid.New()
	putRaw(t, p, validID, jsonData{
		UUID:      validID.String(),
		Inventory: jsonInventoryData{MainHandSlot: 5},
	})
	conf, _, err = p.Load(validID, nilWorld)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if conf.HeldSlot != 5 {
		t.Fatalf("HeldSlot = %d, want 5", conf.HeldSlot)
	}
}
