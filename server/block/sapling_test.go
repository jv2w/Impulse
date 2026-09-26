package block

import (
	"testing"
)

// TestSaplingHashPacksTypeAndAged pins the hash state layout: the type in the
// low four bits (matching the 4-bit SaplingType in cmd/blockhash) and the
// aged flag at bit four. Shifting either changes runtime IDs. PoplarSapling is
// type 8, which is exactly why three bits no longer suffice: it would collide
// with the aged flag.
func TestSaplingHashPacksTypeAndAged(t *testing.T) {
	base, state := Sapling{Type: JungleSapling(), Aged: true}.Hash()
	if base != hashSapling {
		t.Fatalf("material = %v, want hashSapling", base)
	}
	if want := uint64(3) | 1<<4; state != want {
		t.Fatalf("state = %v, want %v", state, want)
	}
	if _, state := (Sapling{Type: PoplarSapling(), Aged: true}).Hash(); state != uint64(8)|1<<4 {
		t.Fatalf("poplar state = %v, want %v", state, uint64(8)|1<<4)
	}
}

// TestSaplingEncodeBlockKeys pins the block name and the age_bit key directly:
// a round-trip through Decode would still pass if both sides renamed the key
// together, leaving the world unreadable by vanilla.
func TestSaplingEncodeBlockKeys(t *testing.T) {
	name, props := Sapling{Type: JungleSapling(), Aged: true}.EncodeBlock()
	if name != "minecraft:jungle_sapling" {
		t.Fatalf("name = %q, want minecraft:jungle_sapling", name)
	}
	if props["age_bit"] != true {
		t.Fatalf("props = %#v, want age_bit true", props)
	}
}
