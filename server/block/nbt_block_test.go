package block_test

import (
	"fmt"
	"image/color"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
)

// The tests in this file pin the on-disk block entity format. Every key name asserted here is a save-format contract:
// a rename that keeps EncodeNBT and DecodeNBT in agreement round-trips perfectly while making every existing world
// unreadable, so the key strings are written out literally rather than derived from the code under test.

// nbtFixture is one block entity in a deliberately non-default state, paired with the exact map its EncodeNBT must
// produce. wantNBT is compared key-for-key, so a new key, a dropped key or a changed value type all fail.
type nbtFixture struct {
	name    string
	block   world.Block
	wantNBT map[string]any
}

// nbtFixtures returns one fixture per block entity in package block whose state is expressible without a world
// transaction. Containers appear here too; their "Items" lists are populated by TestContainerItemsKeyAndSlots.
func nbtFixtures(t *testing.T) []nbtFixture {
	t.Helper()
	return []nbtFixture{
		{
			name:  "Banner",
			block: block.Banner{Colour: item.ColourRed(), Illager: true},
			// Base stores the *inverted* colour id: ^id & 0xf. Red is 14, so Base is 1.
			wantNBT: map[string]any{"id": "Banner", "Patterns": []any{}, "Type": int32(1), "Base": int32(1)},
		},
		{
			name:    "Bed",
			block:   block.Bed{Colour: item.ColourLime()},
			wantNBT: map[string]any{"id": "Bed", "color": item.ColourLime().Uint8()},
		},
		{
			name:  "Beacon",
			block: block.Beacon{Primary: effect.Speed, Secondary: effect.Haste},
			// Effect ids come from entity/effect/register.go: Speed 1, Haste 3.
			wantNBT: map[string]any{"id": "Beacon", "Levels": int32(0), "Primary": int32(1), "Secondary": int32(3)},
		},
		{
			name:    "CopperGolemStatue",
			block:   block.CopperGolemStatue{Pose: block.SittingPose()},
			wantNBT: map[string]any{"id": "CopperGolemStatue", "Pose": int32(1)},
		},
		{
			name:    "EnchantingTable",
			block:   block.EnchantingTable{},
			wantNBT: map[string]any{"id": "EnchantTable"},
		},
		{
			name:    "EnderChest",
			block:   block.EnderChest{},
			wantNBT: map[string]any{"id": "EnderChest"},
		},
		{
			name:    "EndPortal",
			block:   block.EndPortal{},
			wantNBT: map[string]any{"id": "EndPortal"},
		},
		{
			name:  "EndPortalFrame",
			block: block.EndPortalFrame{},
			// PINNED BUG: EndPortalFrame writes the id of a different block entity, "EndPortal".
			wantNBT: map[string]any{"id": "EndPortal"},
		},
		{
			name:  "ItemFrame",
			block: block.ItemFrame{DropChance: 0.25, Rotations: 3},
			wantNBT: map[string]any{
				"id": "ItemFrame", "ItemDropChance": float32(0.25), "ItemRotation": uint8(3),
			},
		},
		{
			name:    "Jukebox",
			block:   block.Jukebox{},
			wantNBT: map[string]any{"id": "Jukebox"},
		},
		{
			name:  "Lectern",
			block: block.Lectern{Page: 7},
			// hasBook is written but never read back; see TestLecternWriteOnlyKeys.
			wantNBT: map[string]any{"id": "Lectern", "page": int32(7), "hasBook": uint8(0)},
		},
		{
			name:  "Note",
			block: block.Note{Pitch: 9, Powered: true},
			// PINNED BUG: Note is the only block entity here that writes no "id" key at all.
			wantNBT: map[string]any{"note": byte(9), "powered": uint8(1)},
		},
		{
			name:  "Skull",
			block: block.Skull{Type: block.SkeletonSkull()},
			// SkullType is pinned at the 255 sentinel: the real type lives in the block state, not the block entity.
			wantNBT: map[string]any{"id": "Skull", "SkullType": uint8(255), "Rotation": float32(0)},
		},
	}
}

// TestBlockEntityEncodeNBTGoldenKeys pins the exact key set, key spelling and Go value type that every block entity
// writes. These are the strings a save file is built from; nothing here may be derived from the source under test.
func TestBlockEntityEncodeNBTGoldenKeys(t *testing.T) {
	for _, f := range nbtFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			n, ok := f.block.(world.NBTer)
			if !ok {
				t.Fatalf("%T does not implement world.NBTer", f.block)
			}
			got := n.EncodeNBT()
			if !reflect.DeepEqual(got, f.wantNBT) {
				t.Errorf("EncodeNBT()\n got %#v\nwant %#v", got, f.wantNBT)
			}
			// Key spelling is case-sensitive on disk; assert the sorted key list separately so a rename is reported
			// as a key-set difference rather than buried in a value diff.
			gotKeys, wantKeys := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(f.wantNBT))
			if !slices.Equal(gotKeys, wantKeys) {
				t.Errorf("keys = %v, want %v", gotKeys, wantKeys)
			}
		})
	}
}

// TestBlockEntityNBTRoundTrip pins that encoding and decoding a block entity is stable: the second encode is identical
// to the first. This catches a key that is written but never read, but only in combination with the golden key set
// above - agreeing read and write sides make a rename invisible to a round trip alone.
func TestBlockEntityNBTRoundTrip(t *testing.T) {
	for _, f := range nbtFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			n := f.block.(world.NBTer)
			first := n.EncodeNBT()
			decoded, ok := n.DecodeNBT(first).(world.NBTer)
			if !ok {
				t.Fatalf("DecodeNBT returned %T, which is not a world.NBTer", n.DecodeNBT(first))
			}
			if second := decoded.EncodeNBT(); !reflect.DeepEqual(first, second) {
				t.Errorf("round trip changed the NBT\nfirst  %#v\nsecond %#v", first, second)
			}
		})
	}
}

// TestBlockEntityIDIsWriteOnly pins that no DecodeNBT in this package reads the "id" key. A save whose id names an
// entirely different block entity decodes without complaint, because the id is carried by the chunk's block state and
// the compound's own id is never cross-checked.
//
// PINNED BUG: an "id" mismatch is undetectable, so a corrupt or hand-edited save silently loads chest data into a
// furnace-shaped compound and vice versa. The id key exists purely to satisfy the vanilla format.
func TestBlockEntityIDIsWriteOnly(t *testing.T) {
	for _, f := range nbtFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			n := f.block.(world.NBTer)
			clean := n.EncodeNBT()
			if _, ok := clean["id"]; !ok {
				// Note writes no id at all; nothing to falsify.
				t.Skipf("%s writes no id key", f.name)
			}
			tampered := maps.Clone(clean)
			tampered["id"] = "SomethingElseEntirely"
			got := n.DecodeNBT(tampered).(world.NBTer).EncodeNBT()
			if !reflect.DeepEqual(got, clean) {
				t.Errorf("a falsified id changed the decode result\n got %#v\nwant %#v", got, clean)
			}
		})
	}
}

// TestDecodeNBTOnEmptyMap pins what each block entity becomes when every key is absent. The scalar readers in nbtconv
// return zero values rather than errors, so an empty compound is a valid save that yields a blanked block entity.
//
// PINNED BUG: two block entities panic on an empty map rather than defaulting. Bed indexes item.Colours() with the
// missing "color" (0, in range, so it survives), but Banner and DecoratedPot are listed in TestDecodeNBTPanics because
// an *empty list* is not the same as an absent one.
func TestDecodeNBTOnEmptyMap(t *testing.T) {
	for _, f := range nbtFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			n := f.block.(world.NBTer)
			decoded := n.DecodeNBT(map[string]any{})
			if decoded == nil {
				t.Fatal("DecodeNBT returned nil")
			}
			// The result must still encode, i.e. the decoded state is not left in a shape the encoder rejects.
			if got := decoded.(world.NBTer).EncodeNBT(); got == nil {
				t.Error("EncodeNBT after decoding an empty map returned nil")
			}
		})
	}
}

// TestDecodeNBTPanics is the catalogue of block entities whose DecodeNBT panics on input a client or a corrupt save can
// supply. Every entry here is reachable from server/session's stackToItem, which hands client-supplied NBT straight to
// DecodeNBT, and from world loading.
//
// PINNED BUG: all of these violate CLAUDE.md 2.2 - decode failure must be a value, not a panic. They are pinned, not
// fixed, so that a later fix has to delete a test deliberately rather than by accident.
func TestDecodeNBTPanics(t *testing.T) {
	tests := []struct {
		name string
		// site is the file:line of the panic, recorded so the catalogue stays traceable as the code moves.
		site  string
		block world.NBTer
		data  map[string]any
		// wantMsg is a substring of the panic value formatted with %v.
		wantMsg string
	}{
		{
			name:    "bed colour index out of range",
			site:    "bed.go:220",
			block:   block.Bed{},
			data:    map[string]any{"color": uint8(16)},
			wantMsg: "index out of range [16] with length 16",
		},
		{
			name:    "bed colour 255",
			site:    "bed.go:220",
			block:   block.Bed{},
			data:    map[string]any{"color": uint8(255)},
			wantMsg: "index out of range [255] with length 16",
		},
		{
			name:  "beacon primary effect is instant, not lasting",
			site:  "beacon.go:54",
			block: block.Beacon{},
			// Effect 6 is InstantHealth, which is registered but does not implement effect.LastingType.
			data:    map[string]any{"Primary": int32(6)},
			wantMsg: "interface conversion",
		},
		{
			name:    "beacon secondary effect is instant, not lasting",
			site:    "beacon.go:57",
			block:   block.Beacon{},
			data:    map[string]any{"Secondary": int32(7)},
			wantMsg: "interface conversion",
		},
		{
			name:    "banner pattern id is unknown",
			site:    "banner_pattern_layer.go:31",
			block:   block.Banner{},
			data:    map[string]any{"Patterns": []any{map[string]any{"Pattern": "not_a_pattern"}}},
			wantMsg: `unknown banner pattern id "not_a_pattern"`,
		},
		{
			name:  "banner pattern id is absent",
			site:  "banner_pattern_layer.go:31",
			block: block.Banner{},
			// An empty compound reads Pattern as "", which is not a registered id either.
			data:    map[string]any{"Patterns": []any{map[string]any{}}},
			wantMsg: `unknown banner pattern id ""`,
		},
		{
			name:    "banner pattern element is not a compound",
			site:    "banner.go:112",
			block:   block.Banner{},
			data:    map[string]any{"Patterns": []any{"not a compound"}},
			wantMsg: "interface conversion",
		},
		{
			name:    "decorated pot sherd names an unknown item",
			site:    "decorated_pot.go:194",
			block:   block.DecoratedPot{},
			data:    map[string]any{"sherds": []any{"minecraft:not_an_item"}},
			wantMsg: "unknown item minecraft:not_an_item",
		},
		{
			name:    "decorated pot sherd names a non-decoration item",
			site:    "decorated_pot.go:198",
			block:   block.DecoratedPot{},
			data:    map[string]any{"sherds": []any{"minecraft:diamond"}},
			wantMsg: "is not a pot decoration",
		},
		{
			name:    "decorated pot sherd is not a string",
			site:    "decorated_pot.go:192",
			block:   block.DecoratedPot{},
			data:    map[string]any{"sherds": []any{int32(3)}},
			wantMsg: "interface conversion",
		},
		{
			name:  "decorated pot sherd is an empty string",
			site:  "decorated_pot.go:194",
			block: block.DecoratedPot{},
			// A truncated list element decodes to "", which resolves to no item.
			data:    map[string]any{"sherds": []any{""}},
			wantMsg: "unknown item ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("DecodeNBT did not panic; the panic at %s may have been fixed", tt.site)
				}
				if msg := fmt.Sprint(r); !strings.Contains(msg, tt.wantMsg) {
					t.Errorf("panic = %q, want it to contain %q (%s)", msg, tt.wantMsg, tt.site)
				}
			}()
			tt.block.DecodeNBT(tt.data)
		})
	}
}

// TestDecodeNBTAcceptsValuesThatPanicLater pins the second class of failure: DecodeNBT itself accepts an out-of-range
// value without validation, and the panic happens on the next encode or state lookup instead.
//
// PINNED BUG: the crash is displaced from the decode that caused it, so the corrupt value is already stored in the
// world by the time anything fails. These are as remotely reachable as the direct panics above.
func TestDecodeNBTAcceptsValuesThatPanicLater(t *testing.T) {
	tests := []struct {
		name    string
		site    string
		decode  func() world.Block
		trigger func(world.Block)
		wantMsg string
	}{
		{
			name: "skull type beyond the six legal types",
			site: "skull_type.go:94",
			decode: func() world.Block {
				// 255 is the sentinel meaning "type lives in the block state"; 7 is simply not a skull.
				return block.Skull{}.DecodeNBT(map[string]any{"SkullType": uint8(7)}).(world.Block)
			},
			trigger: func(b world.Block) { b.(block.Skull).EncodeItem() },
			wantMsg: "unknown skull type",
		},
		{
			name: "copper golem pose beyond the four legal poses",
			site: "copper_golem_pose.go:47",
			decode: func() world.Block {
				return block.CopperGolemStatue{}.DecodeNBT(map[string]any{"Pose": int32(9)}).(world.Block)
			},
			trigger: func(b world.Block) { b.(block.CopperGolemStatue).Pose.Name() },
			wantMsg: "unknown copper golem pose",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b world.Block
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("DecodeNBT itself panicked with %v; it was expected to accept the value", r)
					}
				}()
				b = tt.decode()
			}()
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("the deferred panic at %s did not fire", tt.site)
				}
				if msg := fmt.Sprint(r); !strings.Contains(msg, tt.wantMsg) {
					t.Errorf("panic = %q, want it to contain %q", msg, tt.wantMsg)
				}
			}()
			tt.trigger(b)
		})
	}
}

// TestShulkerBoxFacingIsUnvalidated pins that ShulkerBox.DecodeNBT casts the "facing" byte straight to a cube.Face with
// no range check, so a value above 5 is stored verbatim.
//
// PINNED BUG: cube.Face has six legal values; Face.String and Face.Direction panic outside that range
// (cube/face.go:108). ShulkerBox.EncodeBlock does not use Facing, so the corrupt value survives a save/load cycle and
// only detonates on an interaction path.
func TestShulkerBoxFacingIsUnvalidated(t *testing.T) {
	for _, v := range []uint8{6, 200, 255} {
		got := block.ShulkerBox{}.DecodeNBT(map[string]any{"facing": v}).(block.ShulkerBox)
		if got.Facing != cube.Face(v) {
			t.Errorf("facing %d decoded to %d, want it stored verbatim", v, got.Facing)
		}
		// The value round-trips through EncodeNBT, so it persists to disk.
		if enc := got.EncodeNBT()["facing"]; enc != v {
			t.Errorf("facing %d re-encoded as %#v, want it preserved", v, enc)
		}
	}
	// A legal face is stored unchanged, so the cast is a pure widening for valid input.
	if got := (block.ShulkerBox{}.DecodeNBT(map[string]any{"facing": uint8(cube.FaceEast)}).(block.ShulkerBox)); got.Facing != cube.FaceEast {
		t.Errorf("facing = %v, want %v", got.Facing, cube.FaceEast)
	}
}

// TestBannerBaseKeyPresenceDecidesColour pins that Banner.DecodeNBT branches on the *presence* of the "Base" key rather
// than its value, because banner items carry no Base while banner blocks do. Absent Base leaves the existing colour
// alone; a Base of the wrong Go type is present, reads as 0 through nbtconv.Int32 and therefore sets white.
//
// PINNED BUG: a type drift on Base is indistinguishable from Base=0, so a red banner loaded from a save that stored
// Base as int16 comes back white. The stored value is the inverted colour id (^id & 0xf).
func TestBannerBaseKeyPresenceDecidesColour(t *testing.T) {
	red := block.Banner{Colour: item.ColourRed()}
	if got := red.DecodeNBT(map[string]any{}).(block.Banner); got.Colour != item.ColourRed() {
		t.Errorf("absent Base changed the colour to %v, want it left as red", got.Colour)
	}
	// int16 rather than int32: the key is present, so the branch is taken, but the value reads as 0.
	got := red.DecodeNBT(map[string]any{"Base": int16(1)}).(block.Banner)
	if got.Colour == item.ColourRed() {
		t.Fatal("Base of the wrong type preserved the colour; expected it to be reset")
	}
	// Base 0 inverts to colour id 15, which is black.
	if want := (block.Banner{}.DecodeNBT(map[string]any{"Base": int32(0)}).(block.Banner).Colour); got.Colour != want {
		t.Errorf("Base of the wrong type gave %v, want the same as Base=0 (%v)", got.Colour, want)
	}
	// The inversion is its own inverse, so every colour survives an honest round trip.
	for _, c := range item.Colours() {
		b := block.Banner{Colour: c}
		if out := b.DecodeNBT(b.EncodeNBT()).(block.Banner); out.Colour != c {
			t.Errorf("colour %v round-tripped to %v", c, out.Colour)
		}
	}
}

// TestBannerPatternsAbsentKeyKeepsExistingPatterns pins that an absent or wrongly typed "Patterns" key leaves the
// receiver's patterns untouched rather than clearing them, because nbtconv.Slice returns nil and the assignment is
// guarded on non-nil.
//
// PINNED BUG: decoding is therefore not idempotent against a fresh value - the result depends on what the receiver
// already held. Every other block entity in this package overwrites unconditionally.
func TestBannerPatternsAbsentKeyKeepsExistingPatterns(t *testing.T) {
	layer := block.BannerPatternLayer{Type: block.BorderBannerPattern(), Colour: item.ColourRed()}
	b := block.Banner{Patterns: []block.BannerPatternLayer{layer}}

	for _, data := range []map[string]any{{}, {"Patterns": "not a list"}, {"Patterns": int32(0)}} {
		got := b.DecodeNBT(data).(block.Banner)
		if len(got.Patterns) != 1 || got.Patterns[0] != layer {
			t.Errorf("DecodeNBT(%v) patterns = %v, want the receiver's own patterns preserved", data, got.Patterns)
		}
	}
	// An explicitly empty list does clear them.
	if got := b.DecodeNBT(map[string]any{"Patterns": []any{}}).(block.Banner); len(got.Patterns) != 0 {
		t.Errorf("empty Patterns list left %d patterns, want 0", len(got.Patterns))
	}
	// The layer's own keys are the golden pair "Pattern" (string id) and "Color" (int32).
	m := layer.EncodeNBT()
	// Color, like the banner's own Base, stores the inverted colour id: red is 14, so ^14 & 0xf is 1.
	if !reflect.DeepEqual(m, map[string]any{"Pattern": "bo", "Color": int32(1)}) {
		t.Errorf("BannerPatternLayer.EncodeNBT = %#v", m)
	}
}

// TestSignLegacyFormatShortCircuits pins the pre-1.19.80 sign format still accepted by DecodeNBT, and the fact that
// taking that branch returns early.
//
// PINNED BUG: the legacy branch is chosen whenever the top-level "Text" key is a non-empty string. A modern sign that
// also happens to carry a stray "Text" key loses its back side, its waxed flag and its text owner, because the early
// return at sign.go:198 skips FrontText, BackText and IsWaxed entirely.
func TestSignLegacyFormatShortCircuits(t *testing.T) {
	modern := block.Sign{
		Waxed: true,
		Front: block.SignText{Text: "front", Owner: "owner", Glowing: true, BaseColour: color.RGBA{A: 0xff, R: 0xff}},
		Back:  block.SignText{Text: "back"},
	}.EncodeNBT()

	// The modern format is symmetric on its own.
	if got := (block.Sign{}.DecodeNBT(modern).(block.Sign)); got.Front.Text != "front" || got.Back.Text != "back" || !got.Waxed || got.Front.Owner != "owner" {
		t.Errorf("modern decode = %+v, want front/back/waxed/owner preserved", got)
	}

	poisoned := maps.Clone(modern)
	poisoned["Text"] = "legacy"
	got := block.Sign{}.DecodeNBT(poisoned).(block.Sign)
	if got.Front.Text != "legacy" {
		t.Errorf("front text = %q, want the legacy Text to win", got.Front.Text)
	}
	if got.Back.Text != "" {
		t.Errorf("back text = %q, want it dropped by the legacy short circuit", got.Back.Text)
	}
	if got.Waxed {
		t.Error("waxed = true, want it dropped by the legacy short circuit")
	}
	if got.Front.Owner != "" {
		t.Errorf("front owner = %q, want it dropped by the legacy short circuit", got.Front.Owner)
	}

	// In the legacy format glowing text needs both IgnoreLighting and TextIgnoreLegacyBugResolved; the modern format
	// needs only IgnoreLighting. TextIgnoreLegacyBugResolved is read-only: EncodeNBT never writes it.
	legacy := map[string]any{"Text": "x", "IgnoreLighting": uint8(1)}
	if got := (block.Sign{}.DecodeNBT(legacy).(block.Sign)); got.Front.Glowing {
		t.Error("legacy glowing = true with IgnoreLighting alone, want false")
	}
	legacy["TextIgnoreLegacyBugResolved"] = uint8(1)
	if got := (block.Sign{}.DecodeNBT(legacy).(block.Sign)); !got.Front.Glowing {
		t.Error("legacy glowing = false with both flags set, want true")
	}
	if _, ok := modern["TextIgnoreLegacyBugResolved"]; ok {
		t.Error("EncodeNBT wrote TextIgnoreLegacyBugResolved; it is expected to be read-only")
	}
}

// TestSignNBTGoldenKeys pins the nested sign compound: the two side keys and the four keys inside each.
func TestSignNBTGoldenKeys(t *testing.T) {
	m := block.Sign{Waxed: true, Front: block.SignText{Text: "hi", Owner: "me", Glowing: true}}.EncodeNBT()
	if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, []string{"BackText", "FrontText", "IsWaxed", "id"}) {
		t.Errorf("Sign keys = %v", got)
	}
	if m["IsWaxed"] != uint8(1) {
		t.Errorf("IsWaxed = %#v, want uint8(1)", m["IsWaxed"])
	}
	front := m["FrontText"].(map[string]any)
	if got := slices.Sorted(maps.Keys(front)); !slices.Equal(got, []string{"IgnoreLighting", "SignTextColor", "Text", "TextOwner"}) {
		t.Errorf("FrontText keys = %v", got)
	}
	if front["Text"] != "hi" || front["TextOwner"] != "me" || front["IgnoreLighting"] != uint8(1) {
		t.Errorf("FrontText = %#v", front)
	}
	// A zero BaseColour is written as the opaque-black sentinel, never as 0, because 0 is transparent and invisible.
	if front["SignTextColor"] != int32(-0x1000000) {
		t.Errorf("SignTextColor = %#v, want %d", front["SignTextColor"], int32(-0x1000000))
	}
}

// TestLecternWriteOnlyKeys pins that Lectern writes "hasBook" and "totalPages" but reads neither back: the presence of
// a book is rederived from the "book" compound alone.
//
// PINNED SCOPE: the two keys exist for the vanilla client, not for Impulse. Deleting them from a save changes nothing
// on load, which this test states outright.
func TestLecternWriteOnlyKeys(t *testing.T) {
	m := block.Lectern{Page: 3}.EncodeNBT()
	if m["hasBook"] != uint8(0) {
		t.Errorf("hasBook = %#v, want uint8(0)", m["hasBook"])
	}
	if _, ok := m["totalPages"]; ok {
		t.Error("totalPages written for a lectern with no book, want it omitted")
	}
	lying := map[string]any{"page": int32(3), "hasBook": uint8(1), "totalPages": int32(50)}
	got := block.Lectern{}.DecodeNBT(lying).(block.Lectern)
	if !got.Book.Empty() {
		t.Errorf("book = %v, want empty despite hasBook=1", got.Book)
	}
	if got.Page != 3 {
		t.Errorf("page = %d, want 3", got.Page)
	}
}

// TestItemFrameGlowingLivesInTheBlockState pins that the ItemFrame "id" key is the only place EncodeNBT records
// glowing, and that DecodeNBT ignores it - glowing survives only because EncodeBlock uses a different block name.
func TestItemFrameGlowingLivesInTheBlockState(t *testing.T) {
	glowing := block.ItemFrame{Glowing: true}
	if id := glowing.EncodeNBT()["id"]; id != "GlowItemFrame" {
		t.Errorf("glowing frame id = %#v, want \"GlowItemFrame\"", id)
	}
	if name, _ := glowing.EncodeBlock(); name != "minecraft:glow_frame" {
		t.Errorf("glowing frame block name = %q, want \"minecraft:glow_frame\"", name)
	}
	if name, _ := (block.ItemFrame{}).EncodeBlock(); name != "minecraft:frame" {
		t.Errorf("plain frame block name = %q, want \"minecraft:frame\"", name)
	}
	// Decoding a GlowItemFrame compound onto a plain frame does not make it glow.
	got := block.ItemFrame{}.DecodeNBT(glowing.EncodeNBT()).(block.ItemFrame)
	if got.Glowing {
		t.Error("decoding a GlowItemFrame compound set Glowing; the id is expected to be ignored")
	}
}

// TestFurnaceDurationKeysAreTicks pins the four furnace timing keys, their int16 storage type, and the 50ms-per-tick
// conversion in both directions. All three furnace variants share the layout and differ only in "id".
func TestFurnaceDurationKeysAreTicks(t *testing.T) {
	tests := []struct {
		name string
		id   string
		make func() world.NBTer
	}{
		{name: "Furnace", id: "Furnace", make: func() world.NBTer { return block.NewFurnace(cube.North) }},
		{name: "BlastFurnace", id: "BlastFurnace", make: func() world.NBTer { return block.NewBlastFurnace(cube.North) }},
		{name: "Smoker", id: "Smoker", make: func() world.NBTer { return block.NewSmoker(cube.North) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.make()
			m := f.EncodeNBT()
			wantKeys := []string{"BurnDuration", "BurnTime", "CookTime", "Items", "StoredXPInt", "id"}
			if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, wantKeys) {
				t.Errorf("keys = %v, want %v", got, wantKeys)
			}
			if m["id"] != tt.id {
				t.Errorf("id = %#v, want %q", m["id"], tt.id)
			}
			for _, k := range []string{"BurnTime", "CookTime", "BurnDuration", "StoredXPInt"} {
				if _, ok := m[k].(int16); !ok {
					t.Errorf("%s = %#v, want int16", k, m[k])
				}
			}

			// 40 ticks is two seconds; the decode multiplies by 50ms and the encode divides by it.
			in := maps.Clone(m)
			in["BurnTime"], in["BurnDuration"], in["CookTime"], in["StoredXPInt"] = int16(40), int16(60), int16(20), int16(7)
			out := f.DecodeNBT(in).(world.NBTer).EncodeNBT()
			for k, want := range map[string]int16{"BurnTime": 40, "BurnDuration": 60, "CookTime": 20, "StoredXPInt": 7} {
				if out[k] != want {
					t.Errorf("%s round-tripped to %#v, want int16(%d)", k, out[k], want)
				}
			}
		})
	}
}

// TestFurnaceDurationTypeDriftVoidsProgress pins the consequence of the nbtconv reader contract on a real save: the
// timings are read as int16, so a compound storing them as int32 decodes to zero.
//
// PINNED BUG: a furnace loaded from such a save loses all burn and cook progress with no diagnostic. This is the
// save-format failure mode the scalar readers make unavoidable.
func TestFurnaceDurationTypeDriftVoidsProgress(t *testing.T) {
	f := block.NewFurnace(cube.North)
	drifted := map[string]any{
		"BurnTime": int32(40), "BurnDuration": int32(60), "CookTime": int32(20), "StoredXPInt": int32(7),
	}
	out := f.DecodeNBT(drifted).(world.NBTer).EncodeNBT()
	for _, k := range []string{"BurnTime", "BurnDuration", "CookTime", "StoredXPInt"} {
		if out[k] != int16(0) {
			t.Errorf("%s = %#v, want int16(0) - int32 input is expected to be dropped", k, out[k])
		}
	}
}

// TestContainerItemsKeyAndSlots pins the "Items" list key shared by every inventory-bearing block entity, the byte
// "Slot" index inside each entry, and that empty slots produce no entry at all.
func TestContainerItemsKeyAndSlots(t *testing.T) {
	tests := []struct {
		name string
		id   string
		fill func(item.Stack) world.NBTer
	}{
		{name: "Chest", id: "Chest", fill: func(s item.Stack) world.NBTer {
			c := block.NewChest()
			mustSet(nil, c.Inventory(nil, cube.Pos{}), 0, s)
			return c
		}},
		{name: "Barrel", id: "Barrel", fill: func(s item.Stack) world.NBTer {
			b := block.NewBarrel()
			mustSet(nil, b.Inventory(nil, cube.Pos{}), 0, s)
			return b
		}},
		{name: "Hopper", id: "Hopper", fill: func(s item.Stack) world.NBTer {
			h := block.NewHopper()
			mustSet(nil, h.Inventory(nil, cube.Pos{}), 0, s)
			return h
		}},
	}
	stack := item.NewStack(item.Diamond{}, 3)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.fill(stack).EncodeNBT()
			if m["id"] != tt.id {
				t.Errorf("id = %#v, want %q", m["id"], tt.id)
			}
			items, ok := m["Items"].([]any)
			if !ok {
				t.Fatalf("Items = %#v, want []any", m["Items"])
			}
			if len(items) != 1 {
				t.Fatalf("Items has %d entries, want 1 (empty slots omitted)", len(items))
			}
			entry := items[0].(map[string]any)
			if slot, ok := entry["Slot"].(byte); !ok || slot != 0 {
				t.Errorf("Slot = %#v, want byte(0)", entry["Slot"])
			}
			if entry["Name"] != "minecraft:diamond" {
				t.Errorf("Name = %#v, want \"minecraft:diamond\"", entry["Name"])
			}
			if entry["Count"] != byte(3) {
				t.Errorf("Count = %#v, want byte(3)", entry["Count"])
			}
		})
	}
}

// TestContainerDropsUnresolvableItems pins that a container entry naming an item this build cannot resolve is dropped
// silently, leaving the slot empty.
//
// PINNED BUG: this is unrecoverable data loss on the next save. nbtconv.InvFromNBT now reports the entry it could not
// read, but DecodeNBT has no channel to carry that report on and drops it. An unknown item is exactly what a save
// written by a newer build looks like to an older one.
func TestContainerDropsUnresolvableItems(t *testing.T) {
	tests := []struct {
		name  string
		items []any
	}{
		{name: "unknown item name", items: []any{
			map[string]any{"Name": "minecraft:not_an_item", "Count": byte(1), "Slot": byte(0)},
		}},
		{name: "count of the wrong type", items: []any{
			map[string]any{"Name": "minecraft:diamond", "Count": int32(1), "Slot": byte(0)},
		}},
		{name: "entry that is not a compound", items: []any{"junk"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := block.NewChest().DecodeNBT(map[string]any{"Items": tt.items}).(block.Chest)
			m := c.EncodeNBT()
			if items, _ := m["Items"].([]any); len(items) != 0 {
				t.Errorf("Items = %#v, want the entry dropped", items)
			}
		})
	}

	// A valid entry alongside a bad one still loads: the loss is per-entry, not per-container.
	c := block.NewChest().DecodeNBT(map[string]any{"Items": []any{
		map[string]any{"Name": "minecraft:not_an_item", "Count": byte(1), "Slot": byte(0)},
		map[string]any{"Name": "minecraft:diamond", "Count": byte(2), "Damage": int16(0), "Slot": byte(1)},
	}}).(block.Chest)
	items, _ := c.EncodeNBT()["Items"].([]any)
	if len(items) != 1 {
		t.Fatalf("Items has %d entries, want 1", len(items))
	}
	if got := items[0].(map[string]any)["Slot"]; got != byte(1) {
		t.Errorf("surviving entry Slot = %#v, want byte(1)", got)
	}
}

// TestChestPairKeysRequireBothAndInt32 pins that "pairx" and "pairz" are honoured only when both are present and both
// are int32, and that a partial or drifted pair silently unpairs the chest.
func TestChestPairKeysRequireBothAndInt32(t *testing.T) {
	tests := []struct {
		name       string
		data       map[string]any
		wantPaired bool
	}{
		{name: "both int32", data: map[string]any{"pairx": int32(4), "pairz": int32(5)}, wantPaired: true},
		{name: "pairz missing", data: map[string]any{"pairx": int32(4)}},
		{name: "pairx missing", data: map[string]any{"pairz": int32(5)}},
		{name: "pairx drifted to int16", data: map[string]any{"pairx": int16(4), "pairz": int32(5)}},
		{name: "both drifted to int64", data: map[string]any{"pairx": int64(4), "pairz": int64(5)}},
		{name: "neither", data: map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := block.NewChest().DecodeNBT(tt.data).(block.Chest).EncodeNBT()
			_, gotX := m["pairx"]
			_, gotZ := m["pairz"]
			if gotX != tt.wantPaired || gotZ != tt.wantPaired {
				t.Errorf("re-encoded pairx=%v pairz=%v, want both %v", gotX, gotZ, tt.wantPaired)
			}
			if tt.wantPaired && (m["pairx"] != int32(4) || m["pairz"] != int32(5)) {
				t.Errorf("pair coordinates = %#v/%#v, want 4/5", m["pairx"], m["pairz"])
			}
		})
	}
}

// TestCustomNameKeyIsOmittedWhenEmpty pins that "CustomName" is written only for a named container, so an unnamed one
// produces no key at all rather than an empty string.
func TestCustomNameKeyIsOmittedWhenEmpty(t *testing.T) {
	plain := block.NewChest().EncodeNBT()
	if _, ok := plain["CustomName"]; ok {
		t.Error("CustomName written for an unnamed chest, want it omitted")
	}
	named := block.NewChest()
	named.CustomName = "Loot"
	m := named.EncodeNBT()
	if m["CustomName"] != "Loot" {
		t.Errorf("CustomName = %#v, want \"Loot\"", m["CustomName"])
	}
	if got := named.DecodeNBT(m).(block.Chest); got.CustomName != "Loot" {
		t.Errorf("decoded CustomName = %q, want \"Loot\"", got.CustomName)
	}
	// A CustomName of the wrong type reads as empty, dropping the name.
	m["CustomName"] = int32(1)
	if got := named.DecodeNBT(m).(block.Chest); got.CustomName != "" {
		t.Errorf("decoded CustomName = %q, want empty for a drifted type", got.CustomName)
	}
}

// TestCampfireSlotKeysAreOneBased pins the four numbered campfire slot key pairs and that EncodeNBT omits the pair for
// an empty slot while DecodeNBT always reads all four.
func TestCampfireSlotKeysAreOneBased(t *testing.T) {
	empty := block.Campfire{}.EncodeNBT()
	if got := slices.Sorted(maps.Keys(empty)); !slices.Equal(got, []string{"id"}) {
		t.Errorf("empty campfire keys = %v, want only id", got)
	}
	c := block.Campfire{}
	c.Items[0] = block.CampfireItem{Item: item.NewStack(item.Beef{}, 1), Time: 2 * time.Second}
	c.Items[3] = block.CampfireItem{Item: item.NewStack(item.BakedPotato{}, 1), Time: time.Second}
	m := c.EncodeNBT()
	wantKeys := []string{"Item1", "Item4", "ItemTime1", "ItemTime4", "id"}
	if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, wantKeys) {
		t.Errorf("campfire keys = %v, want %v", got, wantKeys)
	}
	// Two seconds is 40 ticks, stored as int32.
	if m["ItemTime1"] != int32(40) {
		t.Errorf("ItemTime1 = %#v, want int32(40)", m["ItemTime1"])
	}
	if m["ItemTime4"] != int32(20) {
		t.Errorf("ItemTime4 = %#v, want int32(20)", m["ItemTime4"])
	}
	got := block.Campfire{}.DecodeNBT(m).(block.Campfire)
	if got.Items[0].Time != 2*time.Second || got.Items[3].Time != time.Second {
		t.Errorf("decoded times = %v/%v, want 2s/1s", got.Items[0].Time, got.Items[3].Time)
	}
	if got.Items[1].Time != 0 || !got.Items[1].Item.Empty() {
		t.Errorf("slot 2 = %+v, want zeroed", got.Items[1])
	}
}

// TestJukeboxRejectsNonDiscs pins that Jukebox.DecodeNBT keeps its existing item unless the decoded RecordItem is an
// item.MusicDisc, so a save naming an ordinary item leaves the jukebox holding whatever it already had.
//
// PINNED BUG: the guard is on the decoded value, not on the receiver, so decoding a bad save onto a loaded jukebox
// preserves a stale disc rather than clearing it - decode is not idempotent against a fresh value.
func TestJukeboxRejectsNonDiscs(t *testing.T) {
	disc := item.NewStack(item.MusicDisc{DiscType: sound.DiscCat()}, 1)
	loaded := block.Jukebox{Item: disc}
	if _, ok := loaded.EncodeNBT()["RecordItem"]; !ok {
		t.Fatal("RecordItem missing for a jukebox holding a disc")
	}
	if got := (block.Jukebox{}.DecodeNBT(loaded.EncodeNBT()).(block.Jukebox)); got.Item.Empty() {
		t.Error("decoding a disc onto an empty jukebox left it empty")
	}
	// An ordinary item is refused, and the receiver's existing disc survives.
	nonDisc := map[string]any{"RecordItem": map[string]any{"Name": "minecraft:diamond", "Count": byte(1), "Damage": int16(0)}}
	if got := loaded.DecodeNBT(nonDisc).(block.Jukebox); got.Item.Empty() {
		t.Error("a non-disc RecordItem cleared the jukebox; expected the previous disc to be kept")
	}
	if got := (block.Jukebox{}.DecodeNBT(nonDisc).(block.Jukebox)); !got.Item.Empty() {
		t.Errorf("a non-disc RecordItem loaded %v onto an empty jukebox, want empty", got.Item)
	}
}

// TestDecoratedPotSherdsAreCappedAtFour pins that a sherd list longer than the four decoration slots is truncated
// rather than rejected, and that the golden key is the lower-case "sherds".
func TestDecoratedPotSherdsAreCappedAtFour(t *testing.T) {
	m := block.DecoratedPot{}.EncodeNBT()
	sherds, ok := m["sherds"].([]any)
	if !ok {
		t.Fatalf("sherds = %#v, want []any", m["sherds"])
	}
	if len(sherds) != 4 {
		t.Fatalf("sherds has %d entries, want 4", len(sherds))
	}
	// A nil decoration is written as a plain brick, so the list is always exactly four names.
	for i, s := range sherds {
		if s != "minecraft:brick" {
			t.Errorf("sherd %d = %#v, want \"minecraft:brick\"", i, s)
		}
	}
	long := map[string]any{"sherds": []any{"minecraft:brick", "minecraft:brick", "minecraft:brick", "minecraft:brick", "minecraft:brick", "minecraft:brick"}}
	got := block.DecoratedPot{}.DecodeNBT(long).(block.DecoratedPot)
	if len(got.Decorations) != 4 {
		t.Errorf("decorations = %d, want 4", len(got.Decorations))
	}
	// The cap is checked before the name lookup, so a sixth entry naming an unknown item does not panic.
	safe := map[string]any{"sherds": []any{"minecraft:brick", "minecraft:brick", "minecraft:brick", "minecraft:brick", "minecraft:not_an_item"}}
	if got := (block.DecoratedPot{}).DecodeNBT(safe).(block.DecoratedPot); len(got.Decorations) != 4 {
		t.Errorf("decorations = %d, want 4", len(got.Decorations))
	}
}

// TestBeaconLevelsKeyIsWriteOnlyForCallers pins that "Levels" round-trips through an unexported field, and that an
// effect id this build does not register leaves the slot untouched rather than clearing it.
func TestBeaconLevelsKeyIsWriteOnlyForCallers(t *testing.T) {
	b := block.Beacon{Primary: effect.Speed}
	m := b.EncodeNBT()
	if m["Levels"] != int32(0) {
		t.Errorf("Levels = %#v, want int32(0)", m["Levels"])
	}
	if got := b.DecodeNBT(map[string]any{"Levels": int32(4)}).(block.Beacon).EncodeNBT(); got["Levels"] != int32(4) {
		t.Errorf("Levels round-tripped to %#v, want int32(4)", got["Levels"])
	}
	// An unregistered id is skipped by the ok guard, so the receiver's effect survives.
	got := b.DecodeNBT(map[string]any{"Primary": int32(9999)}).(block.Beacon)
	if got.Primary != effect.Speed {
		t.Errorf("Primary = %v, want the receiver's Speed preserved for an unknown id", got.Primary)
	}
	// A drifted type reads as 0, which is also unregistered, so it behaves the same way.
	if got := (b.DecodeNBT(map[string]any{"Primary": int16(1)}).(block.Beacon)); got.Primary != effect.Speed {
		t.Errorf("Primary = %v, want Speed preserved for a drifted type", got.Primary)
	}
	// EncodeNBT omits an unset effect entirely rather than writing a sentinel.
	if _, ok := (block.Beacon{}).EncodeNBT()["Primary"]; ok {
		t.Error("Primary written for a beacon with no effect, want it omitted")
	}
}

// TestNoteWritesNoIDKey pins the one block entity in this package that emits no "id", and the golden lower-case key
// pair it uses instead. Pitch is stored as a bare byte, so the 25 legal pitches survive but so does every other value.
//
// PINNED BUG: Note.DecodeNBT does not range-check the pitch. Activate keeps it inside 0..24 with a modulus, but a save
// carrying note=200 loads a note block whose pitch is 200.
func TestNoteWritesNoIDKey(t *testing.T) {
	m := block.Note{Pitch: 9, Powered: true}.EncodeNBT()
	if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, []string{"note", "powered"}) {
		t.Errorf("Note keys = %v, want [note powered]", got)
	}
	if m["note"] != byte(9) || m["powered"] != uint8(1) {
		t.Errorf("Note NBT = %#v", m)
	}
	if got := (block.Note{}.DecodeNBT(map[string]any{"note": uint8(200)}).(block.Note)); got.Pitch != 200 {
		t.Errorf("pitch = %d, want 200 stored verbatim", got.Pitch)
	}
	// Pitch is truncated to a byte on the way out, so a pitch above 255 does not round-trip.
	if got := (block.Note{Pitch: 300}).EncodeNBT()["note"]; got != byte(44) {
		t.Errorf("pitch 300 encoded as %#v, want byte(44) - 300 mod 256", got)
	}
}

// TestSkullTypeSentinel pins the 255 sentinel: EncodeNBT always writes it, and DecodeNBT treats it as "leave the type
// alone", so only a pre-1.21.40 save can set the type through NBT at all.
func TestSkullTypeSentinel(t *testing.T) {
	s := block.Skull{Type: block.WitherSkeletonSkull()}
	if got := s.EncodeNBT()["SkullType"]; got != uint8(255) {
		t.Errorf("SkullType = %#v, want uint8(255)", got)
	}
	if got := s.DecodeNBT(s.EncodeNBT()).(block.Skull); got.Type != block.WitherSkeletonSkull() {
		t.Errorf("type = %v, want it preserved through the sentinel", got.Type)
	}
	// A legacy value below 255 overrides the receiver's type.
	if got := (s.DecodeNBT(map[string]any{"SkullType": uint8(0)}).(block.Skull)); got.Type != block.SkeletonSkull() {
		t.Errorf("legacy SkullType 0 gave %v, want SkeletonSkull", got.Type)
	}
	// An absent SkullType reads as 0, which is not the sentinel, so it silently rewrites the type to skeleton.
	if got := (s.DecodeNBT(map[string]any{}).(block.Skull)); got.Type != block.SkeletonSkull() {
		t.Errorf("absent SkullType gave %v, want SkeletonSkull - the missing key is indistinguishable from 0", got.Type)
	}
}

// mustSet writes a stack into an inventory, failing the test if the slot is rejected.
func mustSet(t *testing.T, inv interface{ SetItem(int, item.Stack) error }, slot int, s item.Stack) {
	if err := inv.SetItem(slot, s); err != nil && t != nil {
		t.Fatalf("set slot %d: %v", slot, err)
	}
}

// TestContainerItemBlockTagOddStateType covers the most remotely reachable path into the block registry: an item
// inside a container carrying a "Block" tag whose "states" compound holds a value of a type no block state uses. The
// chain is Chest.DecodeNBT -> nbtconv.InvFromNBT -> item.ReadNBT -> nbtBlock -> world.BlockByName -> hashProperties,
// and it is open to every inventory-bearing block entity and to server/session's stackToItem, which hands
// client-supplied NBT straight to DecodeNBT. The state is a lookup miss; the item still resolves by its own name, so
// the slot survives.
func TestContainerItemBlockTagOddStateType(t *testing.T) {
	// The lookup refuses to run on an unfinalized registry and reports every name as missing; Finalize is idempotent.
	world.DefaultBlockRegistry.Finalize()

	for _, v := range []any{int16(1), int64(1), float32(1), float64(1), []any{}, map[string]any{}, true, uint8(1), int32(1), "s"} {
		t.Run(fmt.Sprintf("%T", v), func(t *testing.T) {
			c := block.NewChest().DecodeNBT(map[string]any{"Items": []any{map[string]any{
				"Name": "minecraft:stone", "Count": byte(1), "Damage": int16(0), "Slot": byte(0),
				"Block": map[string]any{"name": "minecraft:stone", "states": map[string]any{"x": v}},
			}}}).(block.Chest)
			if items, _ := c.EncodeNBT()["Items"].([]any); len(items) != 1 {
				t.Errorf("a %T state gave %d items, want 1", v, len(items))
			}
		})
	}
}
