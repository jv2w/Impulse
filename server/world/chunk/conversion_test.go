package chunk

import (
	"fmt"
	"testing"
)

// conversionAssertProps compares the upgraded properties by their rendered values, so the test does not
// depend on which concrete NBT type (string, byte, int32) the embedded mapping happens to use.
func conversionAssertProps(t *testing.T, got map[string]any, want map[string]string) {
	t.Helper()
	rendered := propsOf(got)
	if len(rendered) != len(want) {
		t.Fatalf("properties = %v, want %v", rendered, want)
	}
	for k, v := range want {
		if rendered[k] != v {
			t.Fatalf("properties[%q] = %q, want %q (full: %v)", k, rendered[k], v, rendered)
		}
	}
}

func TestUpgradeLegacyEntry(t *testing.T) {
	// These expectations come from legacy_states.nbt, which is embedded in the package and never changes at
	// runtime, so upgradeLegacyEntry is fully deterministic and needs no registry.
	cases := []struct {
		name     string
		meta     int16
		wantName string
		wantHave map[string]string
	}{
		// A name that survives the upgrade and gains a property carrying the old meta.
		{name: "minecraft:stone", meta: 0, wantName: "minecraft:stone", wantHave: map[string]string{"stone_type": "stone"}},
		{name: "minecraft:stone", meta: 1, wantName: "minecraft:stone", wantHave: map[string]string{"stone_type": "granite"}},
		{name: "minecraft:stone", meta: 5, wantName: "minecraft:stone", wantHave: map[string]string{"stone_type": "andesite"}},
		// Two properties are derived from a single meta value.
		{name: "minecraft:log", meta: 0, wantName: "minecraft:log", wantHave: map[string]string{"old_log_type": "oak", "pillar_axis": "y"}},
		{name: "minecraft:log", meta: 5, wantName: "minecraft:log", wantHave: map[string]string{"old_log_type": "spruce", "pillar_axis": "x"}},
		// The meta is folded into the block name instead of a property, leaving no properties at all.
		{name: "minecraft:wool", meta: 0, wantName: "minecraft:white_wool", wantHave: map[string]string{}},
		{name: "minecraft:wool", meta: 1, wantName: "minecraft:orange_wool", wantHave: map[string]string{}},
		{name: "minecraft:wool", meta: 4, wantName: "minecraft:yellow_wool", wantHave: map[string]string{}},
		// A boolean-ish property. minecraft:dirt is deliberately not used here: see
		// TestUpgradeLegacyEntryReturnsTheSharedStateMap, which permanently mutates that entry.
		{name: "minecraft:bedrock", meta: 1, wantName: "minecraft:bedrock", wantHave: map[string]string{"infiniburn_bit": "1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := upgradeLegacyEntry(c.name, c.meta)
			if !ok {
				t.Fatalf("upgradeLegacyEntry(%q, %d): not found", c.name, c.meta)
			}
			if got.Name != c.wantName {
				t.Errorf("name = %q, want %q", got.Name, c.wantName)
			}
			// Every mapping entry is stamped with the block version the mapping table was built for.
			if got.Version != CurrentBlockVersion {
				t.Errorf("version = %d, want %d", got.Version, CurrentBlockVersion)
			}
			conversionAssertProps(t, got.State, c.wantHave)
		})
	}
}

func TestUpgradeLegacyEntryMetaZeroFallback(t *testing.T) {
	// When the exact (name, meta) pair is absent, the lookup retries with meta 0. minecraft:air only ever
	// existed at meta 0, so every meta resolves to that one entry. This is why a corrupt or out of range
	// meta silently yields the meta 0 block rather than an error.
	zero, ok := upgradeLegacyEntry("minecraft:air", 0)
	if !ok {
		t.Fatalf("upgradeLegacyEntry(minecraft:air, 0): not found")
	}
	if zero.Name != "minecraft:air" {
		t.Fatalf("name = %q, want minecraft:air", zero.Name)
	}

	for _, meta := range []int16{1, 7, 15, 1000, -3} {
		got, ok := upgradeLegacyEntry("minecraft:air", meta)
		if !ok {
			t.Errorf("upgradeLegacyEntry(minecraft:air, %d): not found, want the meta 0 fallback", meta)
			continue
		}
		if got.Name != zero.Name || got.Version != zero.Version {
			t.Errorf("upgradeLegacyEntry(minecraft:air, %d) = %v, want the meta 0 entry %v", meta, got, zero)
		}
	}
}

func TestUpgradeLegacyEntryNotFound(t *testing.T) {
	// A name absent from the mapping fails both the exact and the meta 0 lookup, and the returned entry is
	// the zero value rather than something partially filled.
	cases := []struct {
		name string
		meta int16
	}{
		{name: "minecraft:not_a_real_block", meta: 0},
		{name: "minecraft:not_a_real_block", meta: 4},
		{name: "impulse:solid", meta: 0},
		{name: "", meta: 0},
		// Post-1.13 names are not in the legacy table; only pre-1.13 names are.
		{name: "minecraft:white_wool", meta: 0},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/%d", c.name, c.meta), func(t *testing.T) {
			got, ok := upgradeLegacyEntry(c.name, c.meta)
			if ok {
				t.Fatalf("upgradeLegacyEntry(%q, %d) = %v, true, want false", c.name, c.meta, got)
			}
			// blockEntry holds a map, so it is compared field by field rather than with ==.
			if got.Name != "" || got.Version != 0 || got.State != nil {
				t.Errorf("entry = %+v, want the zero blockEntry", got)
			}
		})
	}
}

func TestUpgradeLegacyEntryReturnsTheSharedStateMap(t *testing.T) {
	// BUG, pinned as-is: upgradeLegacyEntry hands back the blockEntry stored in the package level
	// legacyMappings map, and blockEntry.State is a reference to the map inside it. The caller therefore
	// gets a live handle on the shared mapping table rather than a copy.
	//
	// This is not theoretical. BlockPaletteEncoding.DecodeBlockState feeds that very map to
	// blockupgrader.Upgrade, which mutates it in place: the first time a legacy minecraft:dirt block is
	// decoded, dirt_type is stripped from the shared entry and every later decode of that legacy block
	// upgrades differently. Decoding one chunk permanently corrupts the mapping table for the process, and
	// nothing serialises access to it either, so concurrent decodes race on it.
	const key = "impulse_test_sentinel"
	entry, ok := upgradeLegacyEntry("minecraft:log", 0)
	if !ok {
		t.Fatalf("upgradeLegacyEntry(minecraft:log, 0): not found")
	}
	entry.State[key] = "written through the returned entry"
	shared := legacyMappings[legacyBlockEntry{Name: "minecraft:log", Meta: 0}].State
	if shared[key] == nil {
		t.Errorf("the returned State is a copy; want it to alias the shared mapping table entry")
	}
	// Undo the sentinel so the rest of the suite sees the table it expects.
	delete(entry.State, key)

	// The consequence: decoding a legacy minecraft:dirt block strips dirt_type from the shared table. The
	// assertion is written to hold on a table that was already stripped by an earlier run of this test, so
	// that -count=N stays deterministic.
	bpe := BlockPaletteEncoding{Blocks: newTestRegistry()}
	if _, err := bpe.DecodeBlockState(map[string]any{"name": "minecraft:dirt", "val": int16(1), "version": int32(0)}); err == nil {
		t.Fatalf("DecodeBlockState(minecraft:dirt) = nil error, want one: the test registry holds no minecraft:* block")
	}
	after := legacyMappings[legacyBlockEntry{Name: "minecraft:dirt", Meta: 1}].State
	if _, still := after["dirt_type"]; still {
		t.Errorf("legacyMappings[dirt/1].State = %v, want dirt_type removed by the upgrader", after)
	}
}

func TestUpgradeLegacyEntryIsStable(t *testing.T) {
	// The mapping is a package level map built in init. Repeated lookups must return the same entry, and
	// the map must not be mutated by lookups.
	first, ok := upgradeLegacyEntry("minecraft:sapling", 3)
	if !ok {
		t.Fatalf("upgradeLegacyEntry(minecraft:sapling, 3): not found")
	}
	conversionAssertProps(t, first.State, map[string]string{"age_bit": "0", "sapling_type": "jungle"})

	before := len(legacyMappings)
	for i := 0; i < 3; i++ {
		again, ok := upgradeLegacyEntry("minecraft:sapling", 3)
		if !ok || again.Name != first.Name || again.Version != first.Version {
			t.Fatalf("lookup %d = %v, %v, want %v, true", i, again, ok, first)
		}
	}
	// The failing lookup path performs a second map read; it must not insert anything.
	upgradeLegacyEntry("minecraft:not_a_real_block", 0)
	if after := len(legacyMappings); after != before {
		t.Errorf("legacyMappings length = %d after lookups, want %d", after, before)
	}
}
