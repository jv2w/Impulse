package item

import (
	"bytes"
	"encoding/gob"
	"maps"
	"reflect"
	"testing"

	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/chunk"
)

// The tests below assert NBT key names and value types as literals rather than
// only round-tripping. A round trip passes as long as the reader and the writer
// agree, so it cannot see a renamed key or a changed integer width; both are
// save-format breaks.

// blockItem is a world.Item that is also a world.Block, which is the only way
// to reach the "Block" branch of writeItemStack. server/block cannot be
// imported here: it depends on this package.
type blockItem struct{}

func (blockItem) EncodeItem() (string, int16) { return "minecraft:stone", 0 }
func (blockItem) EncodeBlock() (string, map[string]any) {
	return "minecraft:stone", map[string]any{"stone_type": "granite"}
}
func (blockItem) Hash() (uint64, uint64)  { return 0, 0 }
func (blockItem) Model() world.BlockModel { return nil }

// badDamageItem writes a Damage tag of the wrong integer width from EncodeNBT.
type badDamageItem struct{}

func (badDamageItem) EncodeItem() (string, int16)  { return "minecraft:stick", 0 }
func (badDamageItem) EncodeNBT() map[string]any    { return map[string]any{"Damage": int32(1)} }
func (badDamageItem) DecodeNBT(map[string]any) any { return badDamageItem{} }

// notAnItem is returned by badDecodeItem.DecodeNBT, which readItemStack asserts
// back to a world.Item without a comma-ok.
type notAnItem struct{}

// badDecodeItem has to be registered for readItemStack to reach its DecodeNBT
// call. It claims a vanilla item name that server/item does not itself
// register, since world.RegisterItem panics both on a duplicate name and on a
// name with no runtime ID.
type badDecodeItem struct{}

func (badDecodeItem) EncodeItem() (string, int16)  { return "minecraft:dirt", 0 }
func (badDecodeItem) EncodeNBT() map[string]any    { return map[string]any{} }
func (badDecodeItem) DecodeNBT(map[string]any) any { return notAnItem{} }

func init() { world.RegisterItem(badDecodeItem{}) }

func TestWriteNBTDiskGolden(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
		want map[string]any
	}{
		{
			name: "empty stack writes nothing",
			s:    Stack{},
			want: map[string]any{},
		},
		{
			name: "plain item carries no tag",
			s:    NewStack(Stick{}, 2),
			want: map[string]any{
				"Name":   "minecraft:stick",
				"Damage": int16(0),
				"Count":  byte(2),
			},
		},
		{
			name: "metadata goes in the top level Damage key",
			s:    NewStack(Helmet{Tier: ArmourTierGold{}}, 1),
			want: map[string]any{
				"Name":   "minecraft:golden_helmet",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag":    map[string]any{"Damage": int16(0)},
			},
		},
		{
			name: "custom name and lore live under display",
			s:    NewStack(Stick{}, 1).WithCustomName("a name").WithLore("line one", "line two"),
			want: map[string]any{
				"Name":   "minecraft:stick",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag": map[string]any{
					"display": map[string]any{
						"Name": "a name",
						"Lore": []string{"line one", "line two"},
					},
				},
			},
		},
		{
			name: "durability damage is an int16 inside the tag",
			s:    NewStack(Sword{Tier: ToolTierWood}, 1).Damage(4),
			want: map[string]any{
				"Name":   "minecraft:wooden_sword",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag":    map[string]any{"Damage": int16(4)},
			},
		},
		{
			name: "anvil cost is RepairCost as an int32",
			s:    NewStack(Sword{Tier: ToolTierWood}, 1).WithAnvilCost(7),
			want: map[string]any{
				"Name":   "minecraft:wooden_sword",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag":    map[string]any{"Damage": int16(0), "RepairCost": int32(7)},
			},
		},
		{
			name: "unbreakable is a byte flag",
			s:    NewStack(Sword{Tier: ToolTierWood}, 1).AsUnbreakable(),
			want: map[string]any{
				"Name":   "minecraft:wooden_sword",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag":    map[string]any{"Damage": int16(0), "Unbreakable": byte(1)},
			},
		},
		{
			name: "enchantments are ench entries of int16 id and lvl",
			s: NewStack(Stick{}, 1).WithEnchantments(
				NewEnchantment(enchA, 2),
				NewEnchantment(enchB, 3),
			),
			want: map[string]any{
				"Name":   "minecraft:stick",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag": map[string]any{
					"ench": []map[string]any{
						{"id": int16(enchBID), "lvl": int16(3)},
						{"id": int16(enchAID), "lvl": int16(2)},
					},
				},
			},
		},
		{
			name: "an item's own NBT is merged into the tag",
			s:    NewStack(WrittenBook{Title: "T", Author: "A", Pages: []string{"p"}}, 1),
			want: map[string]any{
				"Name":   "minecraft:written_book",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag": map[string]any{
					"title":      "T",
					"author":     "A",
					"generation": uint8(0),
					"pages":      []any{map[string]any{"text": "p"}},
				},
			},
		},
		{
			name: "block items carry a Block submap",
			s:    NewStack(blockItem{}, 1),
			want: map[string]any{
				"Name":   "minecraft:stone",
				"Damage": int16(0),
				"Count":  byte(1),
				"Block": map[string]any{
					"name":    "minecraft:stone",
					"states":  map[string]any{"stone_type": "granite"},
					"version": chunk.CurrentBlockVersion,
				},
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := WriteNBT(c.s, true)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("WriteNBT(disk) =\n%#v\nwant\n%#v", got, c.want)
			}
		})
	}
}

func TestWriteNBTNetworkGolden(t *testing.T) {
	t.Run("tags sit at the top level with no Name or Count", func(t *testing.T) {
		got := WriteNBT(NewStack(Stick{}, 5).WithCustomName("n"), false)
		want := map[string]any{"display": map[string]any{"Name": "n"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("WriteNBT(network) =\n%#v\nwant\n%#v", got, want)
		}
	})
	t.Run("durability damage widens to an int32", func(t *testing.T) {
		got := WriteNBT(NewStack(Sword{Tier: ToolTierWood}, 1).Damage(4), false)
		want := map[string]any{"Damage": int32(4)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("WriteNBT(network) =\n%#v\nwant\n%#v", got, want)
		}
	})
	t.Run("empty stack writes nothing", func(t *testing.T) {
		if got := WriteNBT(Stack{}, false); len(got) != 0 {
			t.Fatalf("WriteNBT(network) = %#v, want an empty map", got)
		}
	})
}

func TestWriteNBTTagIsOmittedWhenEmpty(t *testing.T) {
	got := WriteNBT(NewStack(Stick{}, 1), true)
	if _, ok := got["tag"]; ok {
		t.Fatalf(`WriteNBT wrote an empty "tag" key: %#v`, got)
	}
}

func TestNBTRoundTripDisk(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
	}{
		{"plain item", NewStack(Stick{}, 42)},
		{"single item", NewStack(Diamond{}, 1)},
		{"custom name", NewStack(Stick{}, 1).WithCustomName("a name")},
		{"lore", NewStack(Stick{}, 1).WithLore("one", "two", "three")},
		{"name and lore", NewStack(Stick{}, 1).WithCustomName("n").WithLore("l")},
		{"damaged tool", NewStack(Sword{Tier: ToolTierIron}, 1).Damage(37)},
		{"unbreakable tool", NewStack(Sword{Tier: ToolTierIron}, 1).AsUnbreakable()},
		{"anvil cost", NewStack(Sword{Tier: ToolTierIron}, 1).WithAnvilCost(11)},
		{"one enchantment", NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1))},
		{
			"several enchantments",
			NewStack(Stick{}, 1).WithEnchantments(
				NewEnchantment(enchA, 3),
				NewEnchantment(enchB, 5),
			),
		},
		{"enchanted book", NewStack(Book{}, 1).WithEnchantments(NewEnchantment(enchA, 2))},
		{"user data", NewStack(Stick{}, 1).WithValue("num", 7).WithValue("str", "v")},
		{"item NBT", NewStack(WrittenBook{Title: "T", Author: "A", Pages: []string{"p1", "p2"}}, 1)},
		{"armour with metadata", NewStack(Helmet{Tier: ArmourTierGold{}}, 1)},
		{
			"everything at once",
			NewStack(Sword{Tier: ToolTierNetherite}, 1).
				Damage(100).
				WithCustomName("The Sword").
				WithLore("first", "second").
				WithAnvilCost(3).
				WithEnchantments(NewEnchantment(enchA, 2), NewEnchantment(enchB, 4)).
				WithValue("owner", "someone").
				AsUnbreakable(),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ReadNBT(WriteNBT(c.s, true), nil)
			assertStackRoundTripped(t, got, c.s)
		})
	}
}

func TestNBTRoundTripNetwork(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
	}{
		{"custom name", NewStack(Stick{}, 1).WithCustomName("a name")},
		{"lore", NewStack(Stick{}, 1).WithLore("one", "two")},
		{"damaged tool", NewStack(Sword{Tier: ToolTierIron}, 1).Damage(37)},
		{"enchantments", NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 2))},
		{"user data", NewStack(Stick{}, 1).WithValue("k", 3)},
		{"anvil cost", NewStack(Sword{Tier: ToolTierIron}, 1).WithAnvilCost(2)},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The network format carries no item name or count, so the caller
			// supplies the stack the tags are applied to.
			base := NewStack(c.s.Item(), c.s.Count())
			got := ReadNBT(WriteNBT(c.s, false), &base)
			assertStackRoundTripped(t, got, c.s)
		})
	}
}

func assertStackRoundTripped(t *testing.T, got, want Stack) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("round trip produced an unequal stack:\ngot  %v\nwant %v", got, want)
	}
	if got.Count() != want.Count() {
		t.Fatalf("Count() = %v, want %v", got.Count(), want.Count())
	}
	if got.CustomName() != want.CustomName() {
		t.Fatalf("CustomName() = %q, want %q", got.CustomName(), want.CustomName())
	}
	if !reflect.DeepEqual(got.Lore(), want.Lore()) {
		t.Fatalf("Lore() = %#v, want %#v", got.Lore(), want.Lore())
	}
	if got.AnvilCost() != want.AnvilCost() {
		t.Fatalf("AnvilCost() = %v, want %v", got.AnvilCost(), want.AnvilCost())
	}
	if got.Durability() != want.Durability() {
		t.Fatalf("Durability() = %v, want %v", got.Durability(), want.Durability())
	}
	if got.Unbreakable() != want.Unbreakable() {
		t.Fatalf("Unbreakable() = %v, want %v", got.Unbreakable(), want.Unbreakable())
	}
	if !reflect.DeepEqual(got.Values(), want.Values()) {
		t.Fatalf("Values() = %#v, want %#v", got.Values(), want.Values())
	}
	ge, we := got.Enchantments(), want.Enchantments()
	if len(ge) != len(we) {
		t.Fatalf("len(Enchantments()) = %v, want %v", len(ge), len(we))
	}
	for i := range ge {
		if ge[i] != we[i] {
			t.Fatalf("Enchantments()[%v] = %+v, want %+v", i, ge[i], we[i])
		}
	}
	if !reflect.DeepEqual(got.Item(), want.Item()) {
		t.Fatalf("Item() = %#v, want %#v", got.Item(), want.Item())
	}
}

// The gob stream is the on-disk representation of Stack.WithValue data. Its
// type name and field names are baked into every world ever saved, so they are
// asserted as literal bytes rather than only through a round trip.
func TestDragonflyDataGolden(t *testing.T) {
	tag := diskTag(t, NewStack(Stick{}, 1).WithValue("b", "second").WithValue("a", 1))

	raw, ok := tag["dragonflyData"]
	if !ok {
		t.Fatalf(`no "dragonflyData" key in the tag: %#v`, tag)
	}
	d, ok := raw.([]byte)
	if !ok {
		t.Fatalf("dragonflyData = %T, want []byte", raw)
	}
	// The gob stream carries its own type descriptor: the type name mapValue
	// and the field names K and V are written into every saved world, so a
	// rename is a save-format break even though a round trip would not notice.
	// They are length-prefixed in the stream, so the lengths are asserted with
	// them; "K" alone would also match a field renamed to "Key".
	for _, lit := range [][]byte{
		{0x08, 'm', 'a', 'p', 'V', 'a', 'l', 'u', 'e'},
		{0x01, 'K'},
		{0x01, 'V'},
	} {
		if !bytes.Contains(d, lit) {
			t.Fatalf("the gob stream does not carry %q; the on-disk type or field names changed:\n%#v", lit, d)
		}
	}
	// The whole stream is pinned as well: gob's wire format is frozen by the
	// Go 1 compatibility promise, so any difference here is a change in what
	// Impulse writes, not in how Go writes it.
	want := []byte{
		0x0d, 0xff, 0x81, 0x02, 0x01, 0x02, 0xff, 0x82, 0x00, 0x01, 0xff, 0x80,
		0x00, 0x00, 0x21, 0x7f, 0x03, 0x01, 0x01, 0x08, 0x6d, 0x61, 0x70, 0x56,
		0x61, 0x6c, 0x75, 0x65, 0x01, 0xff, 0x80, 0x00, 0x01, 0x02, 0x01, 0x01,
		0x4b, 0x01, 0x0c, 0x00, 0x01, 0x01, 0x56, 0x01, 0x10, 0x00, 0x00, 0x00,
		0x27, 0xff, 0x82, 0x00, 0x02, 0x01, 0x01, 0x61, 0x01, 0x03, 0x69, 0x6e,
		0x74, 0x04, 0x02, 0x00, 0x02, 0x00, 0x01, 0x01, 0x62, 0x01, 0x06, 0x73,
		0x74, 0x72, 0x69, 0x6e, 0x67, 0x0c, 0x08, 0x00, 0x06, 0x73, 0x65, 0x63,
		0x6f, 0x6e, 0x64, 0x00,
	}
	if !bytes.Equal(d, want) {
		t.Fatalf("the gob stream changed:\ngot  %#v\nwant %#v", d, want)
	}

	var values []mapValue
	if err := gob.NewDecoder(bytes.NewReader(d)).Decode(&values); err != nil {
		t.Fatalf("decoding the gob stream: %v", err)
	}
	// mapToSlice sorts by key so that the same values always produce the same
	// bytes.
	wantValues := []mapValue{{K: "a", V: 1}, {K: "b", V: "second"}}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("decoded %#v, want %#v", values, wantValues)
	}
}

func TestDragonflyDataIsDeterministic(t *testing.T) {
	a := NewStack(Stick{}, 1).WithValue("z", 1).WithValue("a", 2).WithValue("m", 3)
	b := NewStack(Stick{}, 1).WithValue("a", 2).WithValue("m", 3).WithValue("z", 1)
	if !bytes.Equal(diskTag(t, a)["dragonflyData"].([]byte), diskTag(t, b)["dragonflyData"].([]byte)) {
		t.Fatal("the same values written in a different order produced different bytes")
	}
}

func TestDragonflyDataOmittedWithoutValues(t *testing.T) {
	if got := WriteNBT(NewStack(Stick{}, 1), true); got["tag"] != nil {
		t.Fatalf("a stack with no values wrote a tag: %#v", got)
	}
	s := NewStack(Stick{}, 1).WithValue("k", 1).WithValue("k", nil)
	if got := WriteNBT(s, true); got["tag"] != nil {
		t.Fatalf("a stack whose only value was cleared wrote a tag: %#v", got)
	}
}

func TestReadDragonflyDataFromByteSlice(t *testing.T) {
	// Some NBT decoders hand back a byte array as []any rather than []byte.
	tag := diskTag(t, NewStack(Stick{}, 1).WithValue("k", 9))
	asAny := make([]any, 0, len(tag["dragonflyData"].([]byte)))
	for _, b := range tag["dragonflyData"].([]byte) {
		asAny = append(asAny, b)
	}
	got := ReadNBT(map[string]any{
		"Name":   "minecraft:stick",
		"Damage": int16(0),
		"Count":  byte(1),
		"tag":    map[string]any{"dragonflyData": asAny},
	}, nil)
	if v, ok := got.Value("k"); !ok || v != 9 {
		t.Fatalf(`Value("k") = %v, %v, want 9, true`, v, ok)
	}
}

// PINNED BUG: readDragonflyData panics on anything it cannot decode. A single
// item whose dragonflyData is truncated, corrupt, or of an unexpected NBT type
// takes down the goroutine that is loading the inventory, and nothing above it
// recovers. This violates CLAUDE.md 2.2. Pinned as-is; decide in stage 1.
func TestReadDragonflyDataPanicsOnBadInput(t *testing.T) {
	for _, c := range []struct {
		name string
		val  any
	}{
		{"wrong NBT type", int32(5)},
		{"empty byte slice", []byte{}},
		{"truncated stream", diskTag(t, NewStack(Stick{}, 1).WithValue("k", 1))["dragonflyData"].([]byte)[:4]},
		{"garbage bytes", []byte{0xff, 0xfe, 0xfd, 0xfc}},
		{"any slice of non-bytes", []any{"not a byte"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("readDragonflyData did not panic; it now reports the failure as a value")
				}
			}()
			ReadNBT(map[string]any{
				"Name":   "minecraft:stick",
				"Damage": int16(0),
				"Count":  byte(1),
				"tag":    map[string]any{"dragonflyData": c.val},
			}, nil)
		})
	}
}

func TestReadItemStackUnknownItem(t *testing.T) {
	// PINNED BUG: an unrecognised item name yields a zero Stack. The count, the
	// enchantments and the user data in the same entry are dropped with it and
	// no error reaches the caller, so a world saved by a build that knows an
	// item silently loses it when loaded by one that does not. Pinned as-is;
	// decide in stage 1.
	got := ReadNBT(map[string]any{
		"Name":   "minecraft:not_a_real_item",
		"Damage": int16(0),
		"Count":  byte(64),
		"tag":    map[string]any{"display": map[string]any{"Name": "a name"}},
	}, nil)
	if !got.Empty() {
		t.Fatalf("ReadNBT = %v, want an empty stack", got)
	}
	if got.Count() != 0 {
		t.Fatalf("Count() = %v, want 0", got.Count())
	}
}

// PINNED BUG: readItemStack asserts the result of DecodeNBT back to a
// world.Item without a comma-ok, so an item type whose DecodeNBT returns
// anything else panics the reader mid-load. Pinned as-is; decide in stage 1.
func TestReadItemStackPanicsWhenDecodeNBTReturnsANonItem(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("readItemStack did not panic on a DecodeNBT returning a non-item")
		}
	}()
	ReadNBT(map[string]any{"Name": "minecraft:dirt", "Damage": int16(0), "Count": byte(1)}, nil)
}

func TestReadItemStackMissingFields(t *testing.T) {
	for _, c := range []struct {
		name string
		m    map[string]any
	}{
		{"empty map", map[string]any{}},
		{"no name", map[string]any{"Count": byte(1)}},
		{"name of the wrong type", map[string]any{"Name": 5, "Count": byte(1)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ReadNBT(c.m, nil); !got.Empty() {
				t.Fatalf("ReadNBT = %v, want an empty stack", got)
			}
		})
	}
	t.Run("missing count yields a zero count", func(t *testing.T) {
		got := ReadNBT(map[string]any{"Name": "minecraft:stick"}, nil)
		if got.Count() != 0 || !got.Empty() {
			t.Fatalf("ReadNBT = %v, want an empty stack", got)
		}
	})
	t.Run("count of the wrong type yields a zero count", func(t *testing.T) {
		got := ReadNBT(map[string]any{"Name": "minecraft:stick", "Count": int32(5)}, nil)
		if got.Count() != 0 {
			t.Fatalf("Count() = %v, want 0", got.Count())
		}
	})
	t.Run("metadata falls back to the meta zero item", func(t *testing.T) {
		// ItemByName retries with a metadata of 0, so a stick with a stray
		// metadata value still resolves.
		got := ReadNBT(map[string]any{
			"Name": "minecraft:stick", "Damage": int16(7), "Count": byte(1),
		}, nil)
		if _, ok := got.Item().(Stick); !ok {
			t.Fatalf("Item() = %T, want Stick", got.Item())
		}
	})
}

// PINNED BUG: the count is written through byte(), so any stack above 255 wraps
// on save. Inventories cap stacks well below this, but Stack itself does not,
// and nothing reports the loss. Pinned as-is; decide in stage 1.
func TestWriteNBTCountWrapsAbove255(t *testing.T) {
	for _, c := range []struct {
		count int
		want  byte
	}{
		{1, 1},
		{64, 64},
		{255, 255},
		{256, 0},
		{300, 44},
	} {
		got := WriteNBT(NewStack(Stick{}, c.count), true)
		if got["Count"] != c.want {
			t.Fatalf("Count for a stack of %v = %v, want %v", c.count, got["Count"], c.want)
		}
	}
}

func TestReadDamage(t *testing.T) {
	read := func(disk bool, damage any) Stack {
		s := NewStack(Sword{Tier: ToolTierWood}, 1) // 59 durability
		if disk {
			return ReadNBT(map[string]any{
				"Name": "minecraft:wooden_sword", "Damage": int16(0), "Count": byte(1),
				"tag": map[string]any{"Damage": damage},
			}, nil)
		}
		return ReadNBT(map[string]any{"Damage": damage}, &s)
	}
	t.Run("disk reads an int16", func(t *testing.T) {
		if got := read(true, int16(10)).Durability(); got != 49 {
			t.Fatalf("Durability() = %v, want 49", got)
		}
	})
	t.Run("disk ignores an int32", func(t *testing.T) {
		if got := read(true, int32(10)).Durability(); got != 59 {
			t.Fatalf("Durability() = %v, want 59: the disk path now reads an int32", got)
		}
	})
	t.Run("network reads an int32", func(t *testing.T) {
		if got := read(false, int32(10)).Durability(); got != 49 {
			t.Fatalf("Durability() = %v, want 49", got)
		}
	})
	t.Run("network ignores an int16", func(t *testing.T) {
		if got := read(false, int16(10)).Durability(); got != 59 {
			t.Fatalf("Durability() = %v, want 59: the network path now reads an int16", got)
		}
	})
	// PINNED BUG: readDamage goes through Stack.Damage, which returns the broken
	// item once the durability reaches zero. A stored damage at or above the
	// item's maximum therefore loads as an empty stack: the item is deleted on
	// read rather than clamped. Pinned as-is; decide in stage 1.
	t.Run("damage at the maximum deletes the item", func(t *testing.T) {
		if got := read(true, int16(59)); !got.Empty() {
			t.Fatalf("ReadNBT = %v, want an empty stack", got)
		}
		if got := read(true, int16(1000)); !got.Empty() {
			t.Fatalf("ReadNBT = %v, want an empty stack", got)
		}
	})
	t.Run("negative damage is clamped to full durability", func(t *testing.T) {
		if got := read(true, int16(-10)).Durability(); got != 59 {
			t.Fatalf("Durability() = %v, want 59", got)
		}
	})
}

func TestReadEnchantments(t *testing.T) {
	read := func(ench any) Stack {
		return ReadNBT(map[string]any{
			"Name": "minecraft:stick", "Damage": int16(0), "Count": byte(1),
			"tag": map[string]any{"ench": ench},
		}, nil)
	}
	t.Run("map slice", func(t *testing.T) {
		got := read([]map[string]any{{"id": int16(enchAID), "lvl": int16(3)}})
		if e, ok := got.Enchantment(enchA); !ok || e.Level() != 3 {
			t.Fatalf("Enchantment(enchA) = %+v, %v, want level 3, true", e, ok)
		}
	})
	t.Run("any slice", func(t *testing.T) {
		got := read([]any{map[string]any{"id": int16(enchAID), "lvl": int16(3)}})
		if e, ok := got.Enchantment(enchA); !ok || e.Level() != 3 {
			t.Fatalf("Enchantment(enchA) = %+v, %v, want level 3, true", e, ok)
		}
	})
	t.Run("non map entries in an any slice are skipped", func(t *testing.T) {
		got := read([]any{"junk", map[string]any{"id": int16(enchAID), "lvl": int16(1)}})
		if _, ok := got.Enchantment(enchA); !ok {
			t.Fatal("a valid entry beside a junk one was dropped")
		}
	})
	t.Run("compatibility is bypassed on read", func(t *testing.T) {
		// enchSword only accepts swords, but readEnchantments uses
		// WithForcedEnchantments, so a stick keeps it.
		got := read([]map[string]any{{"id": int16(enchSwordID), "lvl": int16(1)}})
		if _, ok := got.Enchantment(enchSword); !ok {
			t.Fatal("an item-incompatible enchantment was dropped on read")
		}
	})
	for _, c := range []struct {
		name string
		lvl  any
		want int
	}{
		{"level zero is raised to one", int16(0), 1},
		{"negative level is raised to one", int16(-5), 1},
		{"missing level is raised to one", nil, 1},
		{"level of the wrong type is raised to one", int32(4), 1},
		// PINNED BUG: nothing clamps to EnchantmentType.MaxLevel, so a level far
		// above it survives a save and a load. Pinned as-is; decide in stage 1.
		{"level above MaxLevel is kept", int16(100), 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := map[string]any{"id": int16(enchAID)}
			if c.lvl != nil {
				m["lvl"] = c.lvl
			}
			e, ok := read([]map[string]any{m}).Enchantment(enchA)
			if !ok {
				t.Fatal("the enchantment was dropped")
			}
			if e.Level() != c.want {
				t.Fatalf("Level() = %v, want %v", e.Level(), c.want)
			}
		})
	}
	// PINNED BUG: an id that is not in the registry is dropped without a trace,
	// on both the read and the write side. An enchantment removed from or not
	// yet added to the registry is therefore erased from every item that carries
	// it the next time the world is saved. Pinned as-is; decide in stage 1.
	t.Run("an unregistered id is dropped on read", func(t *testing.T) {
		got := read([]map[string]any{{"id": int16(31000), "lvl": int16(1)}})
		if n := len(got.Enchantments()); n != 0 {
			t.Fatalf("len(Enchantments()) = %v, want 0", n)
		}
	})
	t.Run("an unregistered type is dropped on write", func(t *testing.T) {
		s := NewStack(Stick{}, 1).WithForcedEnchantments(NewEnchantment(enchUnreg, 1))
		tag := diskTag(t, s)
		// The enchantment itself is gone, but writeEnchantments still writes the
		// key: the list it builds is nil, so the item is saved carrying an empty
		// ench tag rather than none at all.
		want := map[string]any{"ench": []map[string]any(nil)}
		if !reflect.DeepEqual(tag, want) {
			t.Fatalf("tag = %#v, want %#v", tag, want)
		}
		if n := len(ReadNBT(WriteNBT(s, true), nil).Enchantments()); n != 0 {
			t.Fatalf("len(Enchantments()) after a round trip = %v, want 0", n)
		}
	})
	t.Run("ench of the wrong type is ignored", func(t *testing.T) {
		if n := len(read("junk").Enchantments()); n != 0 {
			t.Fatalf("len(Enchantments()) = %v, want 0", n)
		}
	})
}

func TestReadDisplay(t *testing.T) {
	read := func(display any) Stack {
		return ReadNBT(map[string]any{
			"Name": "minecraft:stick", "Damage": int16(0), "Count": byte(1),
			"tag": map[string]any{"display": display},
		}, nil)
	}
	t.Run("name and string lore", func(t *testing.T) {
		got := read(map[string]any{"Name": "n", "Lore": []string{"a", "b"}})
		if got.CustomName() != "n" {
			t.Fatalf("CustomName() = %q, want %q", got.CustomName(), "n")
		}
		if want := []string{"a", "b"}; !reflect.DeepEqual(got.Lore(), want) {
			t.Fatalf("Lore() = %#v, want %#v", got.Lore(), want)
		}
	})
	t.Run("any slice lore", func(t *testing.T) {
		got := read(map[string]any{"Lore": []any{"a", "b"}})
		if want := []string{"a", "b"}; !reflect.DeepEqual(got.Lore(), want) {
			t.Fatalf("Lore() = %#v, want %#v", got.Lore(), want)
		}
	})
	t.Run("empty lore", func(t *testing.T) {
		if got := read(map[string]any{"Lore": []any{}}).Lore(); len(got) != 0 {
			t.Fatalf("Lore() = %#v, want empty", got)
		}
	})
	for _, c := range []struct {
		name    string
		display any
	}{
		{"display of the wrong type", "junk"},
		{"name of the wrong type", map[string]any{"Name": 5}},
		{"lore of the wrong type", map[string]any{"Lore": 5}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := read(c.display)
			if got.CustomName() != "" {
				t.Fatalf("CustomName() = %q, want empty", got.CustomName())
			}
			if len(got.Lore()) != 0 {
				t.Fatalf("Lore() = %#v, want empty", got.Lore())
			}
		})
	}
	// PINNED BUG: the []any branch asserts each line to a string without a
	// comma-ok, so a lore list holding anything else panics the reader. Pinned
	// as-is; decide in stage 1.
	t.Run("a non string lore line panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("readDisplay did not panic on a non-string lore line")
			}
		}()
		read(map[string]any{"Lore": []any{"a", 5}})
	})
}

func TestReadUnbreakable(t *testing.T) {
	read := func(v any) Stack {
		return ReadNBT(map[string]any{
			"Name": "minecraft:wooden_sword", "Damage": int16(0), "Count": byte(1),
			"tag": map[string]any{"Unbreakable": v},
		}, nil)
	}
	for _, c := range []struct {
		name string
		v    any
		want bool
	}{
		{"byte one", byte(1), true},
		{"byte zero", byte(0), false},
		// Only the value 1 counts: nbtBool compares for equality, not for a
		// non-zero value.
		{"byte two", byte(2), false},
		{"int32 one", int32(1), false},
		{"bool true", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := read(c.v).Unbreakable(); got != c.want {
				t.Fatalf("Unbreakable() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestReadAnvilCost(t *testing.T) {
	read := func(item string, v any) Stack {
		return ReadNBT(map[string]any{
			"Name": item, "Damage": int16(0), "Count": byte(1),
			"tag": map[string]any{"RepairCost": v},
		}, nil)
	}
	if got := read("minecraft:wooden_sword", int32(6)).AnvilCost(); got != 6 {
		t.Fatalf("AnvilCost() = %v, want 6", got)
	}
	if got := read("minecraft:wooden_sword", int16(6)).AnvilCost(); got != 0 {
		t.Fatalf("AnvilCost() = %v, want 0: RepairCost is now read as an int16", got)
	}
	if got := read("minecraft:enchanted_book", int32(6)).AnvilCost(); got != 6 {
		t.Fatalf("AnvilCost() = %v, want 6", got)
	}
	// A stored cost on an item that cannot hold one is dropped by
	// Stack.WithAnvilCost rather than by the reader.
	if got := read("minecraft:stick", int32(6)).AnvilCost(); got != 0 {
		t.Fatalf("AnvilCost() = %v, want 0", got)
	}
}

func TestMapNBT(t *testing.T) {
	inner := WriteNBT(NewStack(Stick{}, 3), true)
	t.Run("present", func(t *testing.T) {
		got := MapNBT(map[string]any{"Item": inner}, "Item")
		if got.Count() != 3 {
			t.Fatalf("Count() = %v, want 3", got.Count())
		}
		if _, ok := got.Item().(Stick); !ok {
			t.Fatalf("Item() = %T, want Stick", got.Item())
		}
	})
	for _, c := range []struct {
		name string
		x    map[string]any
		k    string
	}{
		{"missing key", map[string]any{"Item": inner}, "Other"},
		{"wrong type", map[string]any{"Item": "junk"}, "Item"},
		{"nil map", nil, "Item"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := MapNBT(c.x, c.k); !got.Empty() {
				t.Fatalf("MapNBT = %v, want an empty stack", got)
			}
		})
	}
}

// PINNED BUG: writeDamage asserts an existing Damage tag to an int16 without a
// comma-ok. Any item whose EncodeNBT writes Damage at another width panics the
// writer before the stack is ever saved. Pinned as-is; decide in stage 1.
func TestWriteDamagePanicsOnAWideDamageTag(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("writeDamage did not panic on an int32 Damage tag")
		}
	}()
	WriteNBT(NewStack(badDamageItem{}, 1), true)
}

func TestReadNBTLeavesTheCallersStackAlone(t *testing.T) {
	// The network path takes a pointer, so it has to be clear whether the caller
	// keeps its stack. It does not: ReadNBT writes through the pointer.
	s := NewStack(Stick{}, 1)
	ReadNBT(map[string]any{"display": map[string]any{"Name": "n"}}, &s)
	if got := s.CustomName(); got != "n" {
		t.Fatalf("CustomName() = %q, want %q: ReadNBT no longer writes through its pointer", got, "n")
	}
}

// The map WriteNBT hands to an NBT encoder must not reach back into the stack:
// an encoder that sorts or rewrites the Lore slice would otherwise edit an item
// sitting in a live inventory.
func TestWriteNBTDoesNotAliasTheStack(t *testing.T) {
	s := NewStack(Stick{}, 1).WithLore("original")
	tag := diskTag(t, s)
	lore := tag["display"].(map[string]any)["Lore"].([]string)
	lore[0] = "rewritten"
	if got := s.Lore()[0]; got != "original" {
		t.Fatalf("Lore()[0] = %q, want %q: WriteNBT aliases the stack's lore", got, "original")
	}
}

func diskTag(t *testing.T, s Stack) map[string]any {
	t.Helper()
	m := WriteNBT(s, true)
	tag, ok := m["tag"].(map[string]any)
	if !ok {
		t.Fatalf(`no "tag" map in %#v`, m)
	}
	return maps.Clone(tag)
}
