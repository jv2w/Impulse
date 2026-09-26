package server

import (
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
//   - world.DefaultBlockRegistry is finalized, irreversibly, and also when the
//     caller supplied their own Config.Blocks.
//   - item/creative's creativeGroups and creativeItemStacks are filled, by the
//     first Config.New() of the process and by no later one.
//   - item/recipe's recipes, dynamicRecipes, index and reagent are filled, by
//     the first Config.New() that does not set VanillaRecipesDisabled.
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

// A registry from world.NewBlockRegistry() can be finalized by hand before any
// server exists: compound block hashes no longer shift by the default registry,
// so there is no Stairs collision left to panic on. The probe asserts the clone
// actually works by reading vanilla air back out of it.
func TestACustomRegistryCanBeFinalizedByHandFirst(t *testing.T) {
	if cloneFinalizePanic != nil {
		t.Fatalf("finalizing a fresh world.NewBlockRegistry() panicked: %v", cloneFinalizePanic)
	}
	if air := cloneFirst.AirRuntimeID(); air != 13629 {
		t.Fatalf("hand-finalized clone air runtime ID = %v, want 13629", air)
	}

	// The probe is a Finalize on a registry of its own and nothing else: no
	// process-wide registry was written to, so the process is still pristine
	// afterwards.
	if gAfterProbes != gPristine {
		t.Fatalf("globals after the probe = %+v, want the pristine %+v", gAfterProbes, gPristine)
	}

	// Once the default has been finalized the same call succeeds.
	if cloneAfterFinalizePanic != nil {
		t.Fatalf("finalizing world.NewBlockRegistry() after the default panicked: %v", cloneAfterFinalizePanic)
	}
}

// Config.Blocks is documented as the way to keep a server off the global block
// registry, so handing one to the first Config.New() of a process has to work.
// It is the first call that matters, which is why this runs in a process of its
// own.
func TestCustomBlockRegistryWorksAsTheFirstServer(t *testing.T) {
	got := scenario(t, scenarioCustomBlockRegistryFirst)
	if got["panic"] != "<nil>" {
		t.Fatalf("Config{Blocks: world.NewBlockRegistry()}.New() panicked with %q", got["panic"])
	}
	// Both registries end up finalized and holding the same vanilla air, which
	// is what says the caller's registry was filled rather than skipped.
	if got["air"] != "13629" || got["defaultair"] != "13629" {
		t.Fatalf("air runtime IDs = %q (custom) and %q (default), want 13629 for both", got["air"], got["defaultair"])
	}
}

// The counts here are the whole of what one Config.New() adds to the four
// registries. They move whenever the vanilla data files or item coverage move,
// which is the point: a silent change in what a server boots is visible.
func TestFirstServerRegistersVanillaContentGlobally(t *testing.T) {
	want := globals{
		recipes:         2814,
		dynamicRecipes:  1,
		creativeItems:   1415,
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

// The recipe and creative tables are filled once per process. A second and a
// third Config.New() find them filled and add nothing, so every server in the
// process ships one of each and every crafting lookup scans one table.
func TestLaterServersAddNothingToTheRegistries(t *testing.T) {
	for _, c := range []struct {
		name                 string
		first, second, third int
	}{
		{"recipes", gAfterFirst.recipes, gAfterSecond.recipes, gAfterRecipesDisabled.recipes},
		{"dynamic recipes", gAfterFirst.dynamicRecipes, gAfterSecond.dynamicRecipes, gAfterRecipesDisabled.dynamicRecipes},
		{"creative items", gAfterFirst.creativeItems, gAfterSecond.creativeItems, gAfterRecipesDisabled.creativeItems},
		{"creative groups", gAfterFirst.creativeGroups, gAfterSecond.creativeGroups, gAfterRecipesDisabled.creativeGroups},
	} {
		if c.second != c.first {
			t.Errorf("%v after the second New = %v, want the %v the first left", c.name, c.second, c.first)
		}
		if c.third != c.first {
			t.Errorf("%v after the third New = %v, want the %v the first left", c.name, c.third, c.first)
		}
	}
	if gAfterSecond != gAfterFirst {
		t.Fatalf("globals after the second Config.New() = %+v, want the %+v the first left", gAfterSecond, gAfterFirst)
	}
	if gAfterRecipesDisabled != gAfterFirst {
		t.Fatalf("globals after the third Config.New() = %+v, want the %+v the first left", gAfterRecipesDisabled, gAfterFirst)
	}
}

// Config.VanillaRecipesDisabled is the one way to keep the recipe tables empty,
// and with the fill kept to one it only bites on the Config.New() that would
// have done the filling. That is the first one in the process, so this runs in
// a process of its own. The creative registries are the control: they are
// written by that same server, so an empty recipe table is the flag working and
// not a Server that failed to boot.
func TestVanillaRecipesDisabledOnTheFirstServer(t *testing.T) {
	got := scenario(t, scenarioVanillaRecipesDisabledFirst)
	if got["recipes"] != "0" || got["dynamic"] != "0" {
		t.Errorf("recipes = %q and dynamic recipes = %q, want 0 for both", got["recipes"], got["dynamic"])
	}
	if got["creativeitems"] != "1415" {
		t.Errorf("creative items = %q, want the usual 1415", got["creativeitems"])
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
		"items":      "1415",
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
