package block

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// TestHashBlockPacksMaterialState pins the registry-cloning-panic fix:
// compound blocks hash their material with hashBlock, which packs the
// material's own type and state into 32 bits instead of a registry runtime
// ID, so the value cannot shift under the compound when the registry is
// cloned or grows.
func TestHashBlockPacksMaterialState(t *testing.T) {
	// Smooth stone carries state bit 0: with a zero state any shift would
	// pass, so a non-zero state is needed to pin the 16-bit packing.
	base, state := Stone{Smooth: true}.Hash()
	if state == 0 {
		t.Fatal("test material has no state bits to pin the packing with")
	}
	if got, want := hashBlock(Stone{Smooth: true}), base|state<<16; got != want {
		t.Fatalf("hashBlock = %d, want %d", got, want)
	}
}

// TestHashBlockRejectsOversizedState pins the guard: a material whose state
// does not fit 16 bits (here Stairs facing south, whose facing sits at bit
// 33) must panic instead of being packed into a value that collides after a
// registry clone.
func TestHashBlockRejectsOversizedState(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("hashBlock did not panic on a state exceeding 16 bits")
		}
	}()
	hashBlock(Stairs{Block: Stone{}, Facing: cube.South})
}
