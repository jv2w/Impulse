package session

import (
	"reflect"
	"sync"
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// sharedRegistry builds the registry the conversion tests run against.
//
// world.DefaultBlockRegistry has to be finalized before world.NewBlockRegistry
// can be: the Hash method of a composite block such as block.Stairs hashes its
// inner block through the package-level world.BlockHash, which reads the
// default registry's bit size. While that is still zero the inner hashes are
// shifted by nothing and collide, and Finalize panics on the collision. The
// registry is built once because each one holds every vanilla block state.
var sharedRegistry = sync.OnceValue(func() world.BlockRegistry {
	world.DefaultBlockRegistry.Finalize()
	br := world.NewBlockRegistry()
	br.Finalize()
	return br
})

func testRegistry(t *testing.T) world.BlockRegistry {
	t.Helper()
	return sharedRegistry()
}

// itemStackOf builds the network representation of the item registered under
// the name and meta passed, so that a test can hand stackToItem a stack whose
// NetworkID resolves to a specific item type.
func itemStackOf(t *testing.T, name string, meta int16, count uint16, nbt map[string]any) protocol.ItemStack {
	t.Helper()
	i, ok := world.ItemByName(name, meta)
	if !ok {
		t.Fatalf("no item registered as %v with meta %v", name, meta)
	}
	rid, _, ok := world.ItemRuntimeID(i)
	if !ok {
		t.Fatalf("item %v has no runtime ID", name)
	}
	return protocol.ItemStack{
		ItemType: protocol.ItemType{NetworkID: rid, MetadataValue: uint32(uint16(meta))},
		Count:    count,
		NBTData:  nbt,
	}
}

// mustItem resolves a registered item by name so that a table entry can name
// the item type it expects without spelling out its zero value.
func mustItem(t *testing.T, name string, meta int16) world.Item {
	t.Helper()
	i, ok := world.ItemByName(name, meta)
	if !ok {
		t.Fatalf("no item registered as %v with meta %v", name, meta)
	}
	return i
}

// The network IDs below are golden values read out of the embedded vanilla
// tables (server/world/vanilla_items.nbt for item network IDs, the block state
// list for block runtime IDs). They are asserted literally so that a change in
// which field of protocol.ItemStack each one lands in fails the test; a round
// trip alone would not, because both directions would move together. A
// Minecraft version bump is expected to change the numbers themselves.
const (
	ridStick        = 323
	ridStone        = 1
	ridDiamondPick  = 320
	ridDecoratedPot = -551
	ridAir          = -158

	bridStone = 2736
)

// TestStackFromItemGolden pins every field of the protocol.ItemStack that
// stackFromItem produces, including the three it never fills.
func TestStackFromItemGolden(t *testing.T) {
	br := testRegistry(t)

	tests := []struct {
		name string
		in   item.Stack
		want protocol.ItemStack
	}{
		{
			name: "empty stack is the zero value",
			in:   item.Stack{},
			want: protocol.ItemStack{},
		},
		{
			name: "plain item carries no block runtime ID",
			in:   item.NewStack(item.Stick{}, 1),
			want: protocol.ItemStack{
				ItemType: protocol.ItemType{NetworkID: ridStick, MetadataValue: 0},
				Count:    1,
				NBTData:  map[string]any{},
			},
		},
		{
			name: "block item carries its block runtime ID",
			in:   item.NewStack(block.Stone{}, 64),
			want: protocol.ItemStack{
				ItemType:       protocol.ItemType{NetworkID: ridStone, MetadataValue: 0},
				Count:          64,
				BlockRuntimeID: bridStone,
				NBTData:        map[string]any{},
			},
		},
		{
			name: "an item that writes NBT of its own has it inlined, not nested in a tag",
			in:   item.NewStack(block.DecoratedPot{}, 1),
			want: protocol.ItemStack{
				ItemType:       protocol.ItemType{NetworkID: ridDecoratedPot, MetadataValue: 0},
				Count:          1,
				BlockRuntimeID: 13694,
				NBTData: map[string]any{
					"id": "DecoratedPot",
					"sherds": []any{
						"minecraft:brick", "minecraft:brick",
						"minecraft:brick", "minecraft:brick",
					},
				},
			},
		},
		{
			name: "every tag the network format carries",
			in: item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).
				Damage(5).
				WithCustomName("name").
				WithLore("a", "b").
				WithAnvilCost(7).
				AsUnbreakable().
				WithEnchantments(item.NewEnchantment(enchantment.Unbreaking, 3)),
			want: protocol.ItemStack{
				ItemType: protocol.ItemType{NetworkID: ridDiamondPick, MetadataValue: 0},
				Count:    1,
				NBTData: map[string]any{
					// Damage is int32 on the network and int16 on disk.
					"Damage":      int32(5),
					"RepairCost":  int32(7),
					"Unbreakable": byte(1),
					"display":     map[string]any{"Name": "name", "Lore": []string{"a", "b"}},
					"ench":        []map[string]any{{"id": int16(17), "lvl": int16(3)}},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stackFromItem(br, tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("stackFromItem = %#v\nwant %#v", got, tc.want)
			}
			// The fields stackFromItem never writes are asserted separately so
			// that giving one of them a value is a named failure.
			if got.CanBePlacedOn != nil {
				t.Errorf("CanBePlacedOn = %v, want nil", got.CanBePlacedOn)
			}
			if got.CanBreak != nil {
				t.Errorf("CanBreak = %v, want nil", got.CanBreak)
			}
			if got.BlockingTick != 0 {
				t.Errorf("BlockingTick = %v, want 0", got.BlockingTick)
			}
		})
	}
}

// TestStackToItemGolden pins what stackToItem builds out of a literal network
// stack, field by field rather than by comparing against stackFromItem.
func TestStackToItemGolden(t *testing.T) {
	br := testRegistry(t)

	t.Run("plain item", func(t *testing.T) {
		s := stackToItem(br, protocol.ItemStack{
			ItemType: protocol.ItemType{NetworkID: ridStick},
			Count:    16,
		})
		if _, ok := s.Item().(item.Stick); !ok {
			t.Errorf("item = %#v, want item.Stick", s.Item())
		}
		if s.Count() != 16 {
			t.Errorf("count = %v, want 16", s.Count())
		}
	})

	t.Run("every tag the network format carries", func(t *testing.T) {
		s := stackToItem(br, protocol.ItemStack{
			ItemType: protocol.ItemType{NetworkID: ridDiamondPick},
			Count:    1,
			NBTData: map[string]any{
				"Damage":      int32(5),
				"RepairCost":  int32(7),
				"Unbreakable": byte(1),
				"display":     map[string]any{"Name": "name", "Lore": []any{"a", "b"}},
				"ench":        []any{map[string]any{"id": int16(17), "lvl": int16(3)}},
			},
		})
		if _, ok := s.Item().(item.Pickaxe); !ok {
			t.Fatalf("item = %#v, want item.Pickaxe", s.Item())
		}
		if got, want := s.MaxDurability()-s.Durability(), 5; got != want {
			t.Errorf("damage = %v, want %v", got, want)
		}
		if s.AnvilCost() != 7 {
			t.Errorf("anvil cost = %v, want 7", s.AnvilCost())
		}
		if !s.Unbreakable() {
			t.Errorf("unbreakable = false, want true")
		}
		if s.CustomName() != "name" {
			t.Errorf("custom name = %q, want %q", s.CustomName(), "name")
		}
		if got := s.Lore(); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("lore = %q, want [a b]", got)
		}
		e, ok := s.Enchantment(enchantment.Unbreaking)
		if !ok {
			t.Fatalf("enchantment Unbreaking is missing")
		}
		if e.Level() != 3 {
			t.Errorf("enchantment level = %v, want 3", e.Level())
		}
	})

	t.Run("metadata value selects the variant", func(t *testing.T) {
		// Coloured beds share one network ID and are told apart by the
		// metadata value, which is the one thing MetadataValue is read for.
		s := stackToItem(br, itemStackOf(t, "minecraft:bed", 4, 1, nil))
		b, ok := s.Item().(block.Bed)
		if !ok {
			t.Fatalf("item = %#v, want block.Bed", s.Item())
		}
		if b.Colour != item.Colours()[4] {
			t.Errorf("colour = %v, want %v", b.Colour, item.Colours()[4])
		}
	})
}

// TestStackConversionRoundTrip pins that stackToItem inverts stackFromItem for
// the stacks the network path carries.
func TestStackConversionRoundTrip(t *testing.T) {
	br := testRegistry(t)

	tests := []struct {
		name string
		in   item.Stack
	}{
		{name: "plain item", in: item.NewStack(item.Stick{}, 1)},
		{name: "full stack", in: item.NewStack(item.Stick{}, 64)},
		{name: "block item", in: item.NewStack(block.Stone{}, 32)},
		{name: "custom name", in: item.NewStack(item.Stick{}, 1).WithCustomName("name")},
		{name: "lore", in: item.NewStack(item.Stick{}, 1).WithLore("a", "b")},
		{
			name: "damaged tool",
			in:   item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).Damage(100),
		},
		{
			name: "enchanted tool",
			in: item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).
				WithEnchantments(item.NewEnchantment(enchantment.Unbreaking, 3)),
		},
		{
			name: "unbreakable tool",
			in:   item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).AsUnbreakable(),
		},
		{
			name: "anvil cost",
			in:   item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).WithAnvilCost(7),
		},
		{name: "item that reads and writes NBT", in: item.NewStack(block.DecoratedPot{}, 1)},
		{
			name: "count at the byte boundary the disk format clamps at",
			in:   item.NewStack(item.Stick{}, 255),
		},
		{name: "count past the byte boundary", in: item.NewStack(item.Stick{}, 300)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stackToItem(br, stackFromItem(br, tc.in))
			if !got.Equal(tc.in) {
				t.Errorf("round trip = %#v, want %#v", got, tc.in)
			}
			if got.Count() != tc.in.Count() {
				// Stack.Equal ignores the count, so it is asserted separately.
				t.Errorf("count = %v, want %v", got.Count(), tc.in.Count())
			}
		})
	}
}

// TestStackToItemUnknownItem pins the fallback taken when the network ID names
// nothing this build knows.
func TestStackToItemUnknownItem(t *testing.T) {
	br := testRegistry(t)

	tests := []struct {
		name string
		id   int32
	}{
		{name: "network ID is not in the table", id: 0x7fffffff},
		{name: "network ID is zero", id: 0},
		{name: "network ID is negative and unknown", id: -0x7fffffff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stackToItem(br, protocol.ItemStack{
				ItemType: protocol.ItemType{NetworkID: tc.id},
				Count:    5,
				NBTData:  map[string]any{"display": map[string]any{"Name": "name"}},
			})
			// An unknown item resolves to air, and Stack.Empty reports a stack
			// of air as empty however many of it the client claimed to send.
			if !s.Empty() {
				t.Errorf("Empty() = false, want true")
			}
			if s.Item() != nil {
				t.Errorf("Item() = %#v, want nil", s.Item())
			}
			// PINNED BUG: the count and the tags of an unresolvable item are
			// kept on a stack that reports itself empty, so Count and
			// CustomName disagree with Empty and Item.
			if s.Count() != 5 {
				t.Errorf("count = %v, want 5", s.Count())
			}
			if s.CustomName() != "name" {
				t.Errorf("custom name = %q, want %q", s.CustomName(), "name")
			}
		})
	}
}

// TestStackToItemCount pins how the client's count reaches the stack.
func TestStackToItemCount(t *testing.T) {
	br := testRegistry(t)

	tests := []struct {
		name  string
		count uint16
		want  int
		empty bool
	}{
		{name: "zero", count: 0, want: 0, empty: true},
		{name: "one", count: 1, want: 1},
		{name: "a full vanilla stack", count: 64, want: 64},
		{name: "past the maximum stack size", count: 65, want: 65},
		{name: "the last value a byte holds", count: 255, want: 255},
		{name: "past what a byte holds", count: 256, want: 256},
		{name: "the largest count the field carries", count: 65535, want: 65535},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// PINNED BUG: the network path applies no maximum. A client that
			// claims 65535 sticks gets a stack of 65535, well past the 64 that
			// item.Stack.MaxCount reports.
			s := stackToItem(br, protocol.ItemStack{
				ItemType: protocol.ItemType{NetworkID: ridStick},
				Count:    tc.count,
			})
			if s.Count() != tc.want {
				t.Errorf("count = %v, want %v", s.Count(), tc.want)
			}
			if s.Empty() != tc.empty {
				t.Errorf("Empty() = %v, want %v", s.Empty(), tc.empty)
			}
		})
	}
}

// TestStackFromItemCountWraps pins the other direction of the same boundary.
func TestStackFromItemCountWraps(t *testing.T) {
	br := testRegistry(t)

	tests := []struct {
		name  string
		count int
		want  uint16
	}{
		{name: "the largest count the field carries", count: 65535, want: 65535},
		// PINNED BUG: item.WriteNBT clamps an oversized count to fit the byte
		// the disk format stores it in, but the network conversion converts
		// straight to uint16 and wraps. A stack of 65536 is sent as a count of
		// zero, which reads back as an empty slot.
		{name: "one past the field wraps to zero", count: 65536, want: 0},
		{name: "well past the field wraps around", count: 70000, want: 4464},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stackFromItem(br, item.NewStack(item.Stick{}, tc.count))
			if got.Count != tc.want {
				t.Errorf("Count = %v, want %v", got.Count, tc.want)
			}
		})
	}
}

// TestStackToItemBlockRuntimeID pins how the block runtime ID a client sends
// overrides the item resolved from the network ID.
func TestStackToItemBlockRuntimeID(t *testing.T) {
	br := testRegistry(t)

	// notAnItem is the runtime ID of a block that is not registered as an item,
	// the case the world.Item assertion in stackToItem exists for.
	notAnItem := -1
	for rid, b := range br.Blocks() {
		if _, ok := b.(world.Item); !ok {
			notAnItem = rid
			break
		}
	}
	if notAnItem < 0 {
		t.Fatalf("no registered block is unavailable as an item")
	}

	tests := []struct {
		name    string
		brid    int32
		wantAir bool
		want    world.Item
	}{
		{
			name: "zero leaves the item from the network ID alone",
			brid: 0,
			want: item.Stick{},
		},
		{
			name: "negative leaves the item from the network ID alone",
			brid: -1,
			want: item.Stick{},
		},
		{
			name: "the most negative value leaves the item alone",
			brid: -0x80000000,
			want: item.Stick{},
		},
		{
			name: "a block that is also an item replaces it",
			brid: int32(br.BlockRuntimeID(block.Stone{})),
			want: block.Stone{},
		},
		{
			name:    "a block that is not an item falls back to air",
			brid:    int32(notAnItem),
			wantAir: true,
		},
		{
			name:    "out of range falls back to air",
			brid:    0x7fffffff,
			wantAir: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stackToItem(br, protocol.ItemStack{
				ItemType:       protocol.ItemType{NetworkID: ridStick},
				Count:          1,
				BlockRuntimeID: tc.brid,
			})
			if tc.wantAir {
				if !s.Empty() {
					t.Errorf("Empty() = false, want true for an air fallback")
				}
				return
			}
			if !reflect.DeepEqual(s.Item(), tc.want) {
				t.Errorf("item = %#v, want %#v", s.Item(), tc.want)
			}
		})
	}
}

// TestStackToItemNBTShape pins the shapes of NBTData that carry no tags, which
// gophertunnel produces for a stack the client sent without a compound.
func TestStackToItemNBTShape(t *testing.T) {
	br := testRegistry(t)

	for _, nbt := range []map[string]any{nil, {}} {
		s := stackToItem(br, protocol.ItemStack{
			ItemType: protocol.ItemType{NetworkID: ridDecoratedPot},
			Count:    1,
			NBTData:  nbt,
		})
		if !reflect.DeepEqual(s.Item(), block.DecoratedPot{}) {
			t.Errorf("item for NBTData %v = %#v, want the zero pot", nbt, s.Item())
		}
		if s.CustomName() != "" || len(s.Lore()) != 0 {
			t.Errorf("item for NBTData %v picked up display tags", nbt)
		}
	}
}

// TestDeleteDamage pins what deleteDamage strips and what it leaves, including
// that it edits the map the stack passed in still points at.
func TestDeleteDamage(t *testing.T) {
	in := protocol.ItemStack{
		ItemType: protocol.ItemType{NetworkID: ridDiamondPick},
		Count:    1,
		NBTData: map[string]any{
			"Damage":      int32(5),
			"RepairCost":  int32(7),
			"Unbreakable": byte(1),
			"display":     map[string]any{"Name": "name"},
		},
	}
	shared := in.NBTData

	got := deleteDamage(in)
	want := map[string]any{
		"RepairCost":  int32(7),
		"Unbreakable": byte(1),
		"display":     map[string]any{"Name": "name"},
	}
	if !reflect.DeepEqual(got.NBTData, want) {
		t.Errorf("NBTData = %#v, want %#v", got.NBTData, want)
	}
	if got.NetworkID != ridDiamondPick || got.Count != 1 {
		t.Errorf("deleteDamage changed a field other than NBTData: %#v", got)
	}
	// deleteDamage takes and returns a value, but NBTData is a map: the
	// argument's map is the one that loses the tag.
	if _, ok := shared["Damage"]; ok {
		t.Errorf("the argument's map kept Damage, so deleteDamage no longer edits in place")
	}
}

// TestStacksToRecipeStacksStripsDamage pins that the recipe conversion applies
// deleteDamage to every entry.
func TestStacksToRecipeStacksStripsDamage(t *testing.T) {
	br := testRegistry(t)

	got := stacksToRecipeStacks(br, []item.Stack{
		item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).Damage(5),
		item.NewStack(item.Stick{}, 1),
	})
	if len(got) != 2 {
		t.Fatalf("len = %v, want 2", len(got))
	}
	if _, ok := got[0].NBTData["Damage"]; ok {
		t.Errorf("entry 0 kept its Damage tag: %#v", got[0].NBTData)
	}
	if got[0].NetworkID != ridDiamondPick || got[1].NetworkID != ridStick {
		t.Errorf("network IDs = %v, %v, want %v, %v", got[0].NetworkID, got[1].NetworkID, ridDiamondPick, ridStick)
	}
}

// TestInstanceFromItem pins that the instance wrapper adds the stack network ID
// and changes nothing else.
func TestInstanceFromItem(t *testing.T) {
	br := testRegistry(t)

	s := item.NewStack(item.Stick{}, 4)
	got := instanceFromItem(br, s)
	if got.StackNetworkID != item_id(s) {
		t.Errorf("StackNetworkID = %v, want %v", got.StackNetworkID, item_id(s))
	}
	if !reflect.DeepEqual(got.Stack, stackFromItem(br, s)) {
		t.Errorf("Stack = %#v, want the plain conversion", got.Stack)
	}
	if empty := instanceFromItem(br, item.Stack{}); !reflect.DeepEqual(empty.Stack, protocol.ItemStack{}) {
		t.Errorf("empty stack instance = %#v, want a zero stack", empty.Stack)
	}
}

// unregisteredItem is a world.Item this build never registers, which is what a
// third-party item looks like before it is passed to world.RegisterItem.
type unregisteredItem struct{}

func (unregisteredItem) EncodeItem() (string, int16) { return "impulse:unregistered", 0 }

// TestStackFromItemUnregisteredItem pins what an item with no runtime ID is
// sent as.
func TestStackFromItemUnregisteredItem(t *testing.T) {
	br := testRegistry(t)

	// PINNED BUG: stackFromItem discards the ok of world.ItemRuntimeID, so an
	// unregistered item is sent with network ID 0 and the metadata value it
	// encoded to, indistinguishable from a real item at that ID rather than
	// reported as unsendable.
	got := stackFromItem(br, item.NewStack(unregisteredItem{}, 2))
	if got.NetworkID != 0 {
		t.Errorf("NetworkID = %v, want 0", got.NetworkID)
	}
	if got.Count != 2 {
		t.Errorf("Count = %v, want 2", got.Count)
	}
	if _, ok := world.ItemByRuntimeID(0, 0); ok {
		t.Errorf("network ID 0 resolves to an item, so the fallback is ambiguous")
	}
}

// TestStackFromItemAirIsEmpty pins that a stack of air never reaches the wire
// as an item, because Stack.Empty already treats it as an empty slot.
func TestStackFromItemAirIsEmpty(t *testing.T) {
	br := testRegistry(t)

	if got := stackFromItem(br, item.NewStack(block.Air{}, 1)); !reflect.DeepEqual(got, protocol.ItemStack{}) {
		t.Errorf("stackFromItem of air = %#v, want the zero value", got)
	}
	if _, _, ok := world.ItemRuntimeID(block.Air{}); !ok {
		t.Errorf("air has no runtime ID, so the constant below is stale")
	}
	if rid, _, _ := world.ItemRuntimeID(block.Air{}); rid != ridAir {
		t.Errorf("air network ID = %v, want %v", rid, ridAir)
	}
}
