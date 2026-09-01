package session

import (
	"reflect"
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// TestStackToItemHostileNBTNoPanic feeds stackToItem the NBT payloads a client
// can put on the wire. Every case reaches a DecodeNBT implementation that
// panics on malformed input; stackToItem must absorb that instead of killing
// the goroutine reading the packet.
//
// The payloads are reachable through packet.MobEquipment,
// packet.InventoryTransaction and packet.PlayerAuthInput, the last of which
// converts a held item every tick.
func TestStackToItemHostileNBTNoPanic(t *testing.T) {
	br := testRegistry(t)

	tests := []struct {
		name     string
		itemName string
		itemMeta int16
		nbt      map[string]any
		// want is the item type stackToItem must return once the hostile NBT
		// has been rejected: the type resolved from the network ID, untouched.
		want world.Item
	}{
		{
			name:     "pot sherd names an unregistered item",
			itemName: "minecraft:decorated_pot",
			nbt:      map[string]any{"sherds": []any{"minecraft:not_a_real_item"}},
			want:     block.DecoratedPot{},
		},
		{
			name:     "pot sherd names a registered item that is not a decoration",
			itemName: "minecraft:decorated_pot",
			nbt:      map[string]any{"sherds": []any{"minecraft:cobblestone"}},
			want:     block.DecoratedPot{},
		},
		{
			name:     "pot sherd is not a string",
			itemName: "minecraft:decorated_pot",
			nbt:      map[string]any{"sherds": []any{int32(7)}},
			want:     block.DecoratedPot{},
		},
		{
			name:     "pot sherd is nil",
			itemName: "minecraft:decorated_pot",
			nbt:      map[string]any{"sherds": []any{nil}},
			want:     block.DecoratedPot{},
		},
		{
			name:     "firework star explosion tag is empty",
			itemName: "minecraft:firework_star",
			nbt:      map[string]any{"FireworksItem": map[string]any{}},
			want:     mustItem(t, "minecraft:firework_star", 0),
		},
		{
			name:     "firework star shape is out of range",
			itemName: "minecraft:firework_star",
			nbt: map[string]any{"FireworksItem": map[string]any{
				"FireworkType":    uint8(200),
				"FireworkFlicker": uint8(0),
				"FireworkTrail":   uint8(0),
			}},
			want: mustItem(t, "minecraft:firework_star", 0),
		},
		{
			name:     "firework explosion list holds a non-compound",
			itemName: "minecraft:firework_rocket",
			nbt:      map[string]any{"Fireworks": map[string]any{"Explosions": []any{int32(0)}}},
			want:     item.Firework{},
		},
		{
			name:     "firework explosion compound is empty",
			itemName: "minecraft:firework_rocket",
			nbt:      map[string]any{"Fireworks": map[string]any{"Explosions": []any{map[string]any{}}}},
			want:     item.Firework{},
		},
		{
			name:     "written book page list holds a non-compound",
			itemName: "minecraft:written_book",
			nbt:      map[string]any{"pages": []any{int32(0)}},
			want:     item.WrittenBook{},
		},
		{
			name:     "written book page has no text",
			itemName: "minecraft:written_book",
			nbt:      map[string]any{"pages": []any{map[string]any{}}},
			want:     item.WrittenBook{},
		},
		{
			name:     "bed colour is out of range",
			itemName: "minecraft:bed",
			nbt:      map[string]any{"color": uint8(16)},
			want:     mustItem(t, "minecraft:bed", 0),
		},
		{
			name:     "beacon primary effect is not a lasting one",
			itemName: "minecraft:beacon",
			nbt:      map[string]any{"Primary": int32(6)},
			want:     block.Beacon{},
		},
		{
			name:     "beacon secondary effect is not a lasting one",
			itemName: "minecraft:beacon",
			nbt:      map[string]any{"Secondary": int32(7)},
			want:     block.Beacon{},
		},
		{
			name:     "banner pattern list holds a non-compound",
			itemName: "minecraft:banner",
			nbt:      map[string]any{"Patterns": []any{int32(0)}},
			want:     mustItem(t, "minecraft:banner", 0),
		},
		{
			name:     "banner pattern names an unknown pattern",
			itemName: "minecraft:banner",
			nbt:      map[string]any{"Patterns": []any{map[string]any{"Pattern": "nope"}}},
			want:     mustItem(t, "minecraft:banner", 0),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := itemStackOf(t, tc.itemName, tc.itemMeta, 1, tc.nbt)

			s := stackToItem(br, st)
			if s.Empty() {
				t.Fatalf("stackToItem returned an empty stack")
			}
			if !reflect.DeepEqual(s.Item(), tc.want) {
				t.Errorf("item = %#v, want %#v", s.Item(), tc.want)
			}
			if s.Count() != 1 {
				t.Errorf("count = %v, want 1", s.Count())
			}
		})
	}
}

// TestStackToItemHostileNBTKeepsTags pins what a rejected DecodeNBT leaves
// behind: the item type resolved from the network ID, with the tags item.ReadNBT
// applies still read from the same map.
func TestStackToItemHostileNBTKeepsTags(t *testing.T) {
	br := testRegistry(t)

	st := itemStackOf(t, "minecraft:decorated_pot", 0, 3, map[string]any{
		"sherds":  []any{"minecraft:not_a_real_item"},
		"display": map[string]any{"Name": "hostile", "Lore": []any{"line"}},
	})

	s := stackToItem(br, st)
	pot, ok := s.Item().(block.DecoratedPot)
	if !ok {
		t.Fatalf("item type = %T, want block.DecoratedPot", s.Item())
	}
	if pot.Decorations != [4]block.PotDecoration{} {
		t.Errorf("decorations = %#v, want the zero array", pot.Decorations)
	}
	if !pot.Item.Empty() {
		t.Errorf("stored item = %#v, want an empty stack", pot.Item)
	}
	if s.Count() != 3 {
		t.Errorf("count = %v, want 3", s.Count())
	}
	if s.CustomName() != "hostile" {
		t.Errorf("custom name = %q, want %q", s.CustomName(), "hostile")
	}
	if got := s.Lore(); len(got) != 1 || got[0] != "line" {
		t.Errorf("lore = %q, want [line]", got)
	}
}

// TestStackToItemWellFormedNBTStillDecodes guards the hardening against
// swallowing valid data: NBT a client may legitimately send must still reach
// the item type.
func TestStackToItemWellFormedNBTStillDecodes(t *testing.T) {
	br := testRegistry(t)

	t.Run("decorated pot sherds", func(t *testing.T) {
		st := itemStackOf(t, "minecraft:decorated_pot", 0, 1, map[string]any{
			"sherds": []any{"minecraft:brick", "minecraft:angler_pottery_sherd"},
		})

		pot, ok := stackToItem(br, st).Item().(block.DecoratedPot)
		if !ok {
			t.Fatalf("item type is not block.DecoratedPot")
		}
		if pot.Decorations[1] == nil {
			t.Fatalf("decoration 1 = nil, want the angler sherd")
		}
		name, _ := pot.Decorations[1].EncodeItem()
		if name != "minecraft:angler_pottery_sherd" {
			t.Errorf("decoration 1 = %v, want minecraft:angler_pottery_sherd", name)
		}
	})

	// A sherd list longer than the four decorations a pot holds is truncated
	// rather than rejected, so a fifth entry naming an unregistered item never
	// reaches the panic that a first entry naming it would.
	t.Run("decorated pot sherd list is truncated at four", func(t *testing.T) {
		st := itemStackOf(t, "minecraft:decorated_pot", 0, 1, map[string]any{
			"sherds": []any{
				"minecraft:brick", "minecraft:brick", "minecraft:brick",
				"minecraft:brick", "minecraft:not_a_real_item",
			},
		})

		pot, ok := stackToItem(br, st).Item().(block.DecoratedPot)
		if !ok {
			t.Fatalf("item type is not block.DecoratedPot")
		}
		want := [4]block.PotDecoration{item.Brick{}, item.Brick{}, item.Brick{}, item.Brick{}}
		if pot.Decorations != want {
			t.Errorf("decorations = %#v, want %#v", pot.Decorations, want)
		}
	})

	t.Run("written book pages", func(t *testing.T) {
		st := itemStackOf(t, "minecraft:written_book", 0, 1, map[string]any{
			"title":  "t",
			"author": "a",
			"pages":  []any{map[string]any{"text": "page one"}},
		})

		b, ok := stackToItem(br, st).Item().(item.WrittenBook)
		if !ok {
			t.Fatalf("item type is not item.WrittenBook")
		}
		if b.Title != "t" || b.Author != "a" {
			t.Errorf("title/author = %q/%q, want t/a", b.Title, b.Author)
		}
		if len(b.Pages) != 1 || b.Pages[0] != "page one" {
			t.Errorf("pages = %q, want [page one]", b.Pages)
		}
	})

	t.Run("bed colour", func(t *testing.T) {
		st := itemStackOf(t, "minecraft:bed", 0, 1, map[string]any{"color": uint8(4)})

		b, ok := stackToItem(br, st).Item().(block.Bed)
		if !ok {
			t.Fatalf("item type is not block.Bed")
		}
		if b.Colour != item.Colours()[4] {
			t.Errorf("colour = %v, want %v", b.Colour, item.Colours()[4])
		}
	})
}

// TestDecodeItemNBTPassthrough pins the cases decodeItemNBT must not touch: an
// item that reads no NBT and a stack that carries none.
func TestDecodeItemNBTPassthrough(t *testing.T) {
	tests := []struct {
		name string
		i    world.Item
		data map[string]any
	}{
		{name: "item does not read NBT", i: item.Stick{}, data: map[string]any{"sherds": []any{"x"}}},
		{name: "data is nil", i: block.DecoratedPot{}, data: nil},
		{name: "data is empty", i: block.DecoratedPot{}, data: map[string]any{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeItemNBT(tc.i, tc.data); !reflect.DeepEqual(got, tc.i) {
				t.Errorf("decodeItemNBT = %#v, want %#v", got, tc.i)
			}
		})
	}
}

// nonItemNBTer is a world.Item whose DecodeNBT hands back a value that is not a
// world.Item at all, the failure mode the comma-ok assertion in decodeItemNBT
// guards against. No registered vanilla item does this today, so only a stub
// can cover it.
type nonItemNBTer struct{}

func (nonItemNBTer) EncodeItem() (string, int16)  { return "impulse:non_item_nbter", 0 }
func (nonItemNBTer) EncodeNBT() map[string]any    { return map[string]any{} }
func (nonItemNBTer) DecodeNBT(map[string]any) any { return 42 }

// TestDecodeItemNBTRejectsNonItem pins that a DecodeNBT returning a foreign
// type costs the item its tags and nothing else.
func TestDecodeItemNBTRejectsNonItem(t *testing.T) {
	got := decodeItemNBT(nonItemNBTer{}, map[string]any{"any": "thing"})
	if _, ok := got.(nonItemNBTer); !ok {
		t.Errorf("decodeItemNBT = %#v, want the item unchanged", got)
	}
}
