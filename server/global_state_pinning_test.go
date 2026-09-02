package server

import (
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/world"
)

// PINNED SCOPE: Config.New() is not a constructor of an isolated Server. It is
// also the only place in the tree that boots four process-wide registries. The
// tests in this file record exactly what it writes outside the Server it
// returns, because that list is what has to move onto the instance before two
// Impulse servers can run in one process.
//
// The state Config.New() touches, in the order conf.go touches it:
//   - world.DefaultBlockRegistry is finalized (conf.go:178), irreversibly, and
//     also when the caller supplied their own Config.Blocks.
//   - item/creative's creativeGroups and creativeItemStacks are appended to
//     (conf.go:203).
//   - item/recipe's recipes, dynamicRecipes, index and reagent are appended to
//     (conf.go:204).
//
// It does not touch world's item registry (world.RegisterItem is reached only
// from item package inits) nor cmd's command table.

func TestNothingIsRegisteredBeforeTheFirstServer(t *testing.T) {
	want := globals{}
	if gPristine != want {
		t.Fatalf("pristine process globals = %+v, want %+v", gPristine, want)
	}
	if pristineAirPanic != "BlockRegistry.AirRuntimeID called on non finalized BlockRegistry" {
		t.Fatalf("AirRuntimeID on a pristine registry panicked with %v", pristineAirPanic)
	}
}

// PINNED BUG: world.NewBlockRegistry() is what Config.Blocks documents as the
// way to avoid the global registry, and a registry it returns cannot be
// finalized until world.DefaultBlockRegistry has been finalized first. Composite
// blocks hash through the global: block.Stairs.Hash (server/block/hash.go:1020)
// calls world.BlockHash, which shifts by DefaultBlockRegistry's bitSize. While
// that is still 0, oak and spruce stairs hash to the same value and Finalize
// panics on the collision.
//
// Config.New() finalizes conf.Blocks (conf.go:177) before the default
// (conf.go:178), so passing a custom registry panics rather than isolating
// anything. Swapping those two lines is the fix; it is not made here.
func TestCustomBlockRegistryCannotBeFinalizedFirst(t *testing.T) {
	msg, ok := cloneFinalizePanic.(string)
	if !ok {
		t.Fatalf("finalizing a fresh world.NewBlockRegistry() did not panic: %v", cloneFinalizePanic)
	}
	if !strings.Contains(msg, "already registered by") || !strings.Contains(msg, "block.Stairs") {
		t.Fatalf("panic = %q, want a Stairs hash collision", msg)
	}

	msg, ok = customRegistryNewPanic.(string)
	if !ok {
		t.Fatalf("Config{Blocks: world.NewBlockRegistry()}.New() did not panic: %v", customRegistryNewPanic)
	}
	if !strings.Contains(msg, "already registered by") {
		t.Fatalf("Config.New panic = %q, want the same collision", msg)
	}

	// The failed attempt is not a partial start: it panics before any registry
	// is written to, so the process is still pristine afterwards.
	if gAfterProbes != gPristine {
		t.Fatalf("globals after the failed New = %+v, want the pristine %+v", gAfterProbes, gPristine)
	}

	// Once the default has been finalized the same call succeeds, which is why
	// this only ever bites the first server in a process.
	if cloneAfterFinalizePanic != nil {
		t.Fatalf("finalizing world.NewBlockRegistry() after the default panicked: %v", cloneAfterFinalizePanic)
	}
}

// The counts here are the whole of what one Config.New() adds to the four
// registries. They move whenever the vanilla data files or item coverage move,
// which is the point: a silent change in what a server boots is visible.
func TestFirstServerRegistersVanillaContentGlobally(t *testing.T) {
	want := globals{
		recipes:         2813,
		dynamicRecipes:  1,
		creativeItems:   1413,
		creativeGroups:  123,
		blocksFinalized: true,
	}
	if gAfterFirst != want {
		t.Fatalf("globals after the first Config.New() = %+v, want %+v", gAfterFirst, want)
	}
	// Custom items, custom dimensions and commands are registered by callers,
	// never by Config.New itself.
	if gAfterFirst.customItems != 0 || gAfterFirst.customDimensions != 0 || gAfterFirst.commands != 0 {
		t.Fatalf("Config.New registered custom items/dimensions/commands: %+v", gAfterFirst)
	}
}

// PINNED BUG: a second Config.New() in the same process re-registers every
// vanilla recipe and every creative item and group. Nothing deduplicates and
// nothing can be unregistered, so the second server in a process ships two of
// everything to its clients and every crafting lookup scans a doubled table.
func TestSecondServerDoublesEveryRegistry(t *testing.T) {
	for _, c := range []struct {
		name       string
		first, got int
	}{
		{"recipes", gAfterFirst.recipes, gAfterSecond.recipes},
		{"dynamic recipes", gAfterFirst.dynamicRecipes, gAfterSecond.dynamicRecipes},
		{"creative items", gAfterFirst.creativeItems, gAfterSecond.creativeItems},
		{"creative groups", gAfterFirst.creativeGroups, gAfterSecond.creativeGroups},
	} {
		if c.got != c.first*2 {
			t.Fatalf("%v after the second New = %v, want %v (exactly doubled)", c.name, c.got, c.first*2)
		}
	}
	if want := (globals{recipes: 5626, dynamicRecipes: 2, creativeItems: 2826, creativeGroups: 246, blocksFinalized: true}); gAfterSecond != want {
		t.Fatalf("globals after the second Config.New() = %+v, want %+v", gAfterSecond, want)
	}
}

// PINNED BUG: item/creative's vanilla data addresses groups by index into the
// package-global group slice, so a group registered before the first
// Config.New() shifts every vanilla group by one and 13 vanilla items are filed
// under the caller's group instead of their own. The itemGroup.name.planks
// group, which owns 32 items when nothing is registered first, is one of the
// groups that loses its contents to its neighbour.
//
// This is the first Config.New() of a process, so it is measured in a
// subprocess rather than here; see the scenario support in
// pinning_main_test.go.
func TestCreativeGroupsShiftWhenACallerRegistersFirst(t *testing.T) {
	got := scenario(t, scenarioCreativeGroupShift)
	for k, want := range map[string]string{
		"groups":     "124",
		"group0":     "impulse:pin",
		"group1":     "itemGroup.name.planks",
		"items":      "1413",
		"item0group": "impulse:pin",
		"misplaced":  "13",
		"planks":     "32",
	} {
		if got[k] != want {
			t.Fatalf("%v = %q, want %q (full output %v)", k, got[k], want, got)
		}
	}
}

// PINNED CONSTRAINT: finalization is a one-way door on a process-wide value.
// Once any server exists, no further block or block state can be registered
// anywhere in the process, so a plugin that registers blocks must do so before
// the first Config.New() and there is no way for it to find out whether it is
// too late other than by panicking.
func TestBlockRegistrationIsClosedOnceAServerExists(t *testing.T) {
	if got := airRIDAfterFirst; got != 13629 {
		t.Fatalf("air runtime ID = %v, want 13629", got)
	}
	name, _, ok := world.DefaultBlockRegistry.RuntimeIDToState(airRIDAfterFirst)
	if !ok || name != "minecraft:air" {
		t.Fatalf("runtime ID %v is %q (found %v), want minecraft:air", airRIDAfterFirst, name, ok)
	}
	if registerBlockPanic != "BlockRegistry.RegisterBlock called on finalized BlockRegistry" {
		t.Fatalf("world.RegisterBlock after the first server panicked with %v", registerBlockPanic)
	}
	if registerBlockStatePanic != "BlockRegistry.RegisterBlockState called on finalized BlockRegistry" {
		t.Fatalf("RegisterBlockState after the first server panicked with %v", registerBlockStatePanic)
	}
	// Finalize itself is the exception: it is idempotent, which is what lets
	// Config.New call it on a registry a caller may already have finalized.
	if finalizeAgainPanic != nil {
		t.Fatalf("a second Finalize panicked: %v", finalizeAgainPanic)
	}
}
