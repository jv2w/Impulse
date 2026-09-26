package recipe

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// PINNED CONSTRAINT: registerVanilla reaches world.BlockByName, which panics
// unless world.DefaultBlockRegistry has been finalized. Nothing in this package
// finalizes it; in production that happens inside Config.New. A test binary for
// this package therefore has to finalize the global itself before any recipe
// data can be loaded, and once finalized no further block may be registered.
// Both are process-wide, so the order of this init decides whether the whole
// package's tests can run at all.
//
// server/block is not linked into this binary either, so every recipe whose
// input or output resolves to a block is silently skipped. The counts asserted
// below are for the item-only subset that survives.
func init() {
	world.DefaultBlockRegistry.Finalize()
	// registerVanilla appends to package globals with no way to undo it, so it
	// is called once here rather than from a test body, which would re-register
	// on every iteration of go test -count=n.
	registerVanilla()
	vanilla = Recipes()
	vanillaBrewing = len(index["brewing_stand"])
	vanillaReagents = len(reagent)
}

// The registry has no way to unregister, so the tests below that call Register
// leave their recipes behind for every later test and for every later iteration
// of go test -count=n. What the vanilla data alone produced is snapshotted here
// so that the census stays exact regardless of what ran before it.
var (
	vanilla         []Recipe
	vanillaBrewing  int
	vanillaReagents int
)

func TestShape(t *testing.T) {
	for _, c := range []struct {
		name          string
		s             Shape
		width, height int
	}{
		{"square", NewShape(3, 3), 3, 3},
		{"rectangle", NewShape(2, 3), 2, 3},
		{"composite literal", Shape{2, 3}, 2, 3},
		// NewShape validates nothing: a shape that no crafting grid could hold
		// is constructed just as happily as a valid one.
		{"zero", NewShape(0, 0), 0, 0},
		{"negative", NewShape(-1, -2), -1, -2},
		{"huge", NewShape(1<<30, 1<<30), 1 << 30, 1 << 30},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.Width(); got != c.width {
				t.Fatalf("Width() = %v, want %v", got, c.width)
			}
			if got := c.s.Height(); got != c.height {
				t.Fatalf("Height() = %v, want %v", got, c.height)
			}
		})
	}
}

// NewShaped's doc comment promises that the inputs match width*height and that
// an absent block becomes a crafting table. Neither is implemented.
func TestNewShapedValidatesNothing(t *testing.T) {
	r := NewShaped([]Item{item.NewStack(item.Stick{}, 1)}, item.NewStack(item.Diamond{}, 1), NewShape(3, 3), "")
	if got := len(r.Input()); got != 1 {
		t.Fatalf("len(Input()) = %v, want 1 for a 3x3 shape", got)
	}
	if got := r.Block(); got != "" {
		t.Fatalf("Block() = %q, want the empty string", got)
	}
	if got := r.Shape(); got != NewShape(3, 3) {
		t.Fatalf("Shape() = %v, want %v", got, NewShape(3, 3))
	}
}

func TestConstructors(t *testing.T) {
	stick := item.NewStack(item.Stick{}, 1)
	diamond := item.NewStack(item.Diamond{}, 1)

	t.Run("Shapeless", func(t *testing.T) {
		r := NewShapeless([]Item{stick}, diamond, "crafting_table")
		assertRecipe(t, r, []Item{stick}, []item.Stack{diamond}, "crafting_table")
	})
	t.Run("UserDataShapeless", func(t *testing.T) {
		r := NewUserDataShapeless([]Item{stick}, diamond, "crafting_table")
		assertRecipe(t, r, []Item{stick}, []item.Stack{diamond}, "crafting_table")
	})
	t.Run("Shaped", func(t *testing.T) {
		r := NewShaped([]Item{stick}, diamond, NewShape(1, 1), "stonecutter")
		assertRecipe(t, r, []Item{stick}, []item.Stack{diamond}, "stonecutter")
	})
	t.Run("Multi", func(t *testing.T) {
		id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
		r := NewMulti(id)
		if got := r.UUID(); got != id {
			t.Fatalf("UUID() = %v, want %v", got, id)
		}
		// A multi recipe carries no items at all: the client holds the
		// behaviour and only the UUID identifies it.
		assertRecipe(t, r, nil, nil, "crafting_table")
	})
	t.Run("SmithingTransform", func(t *testing.T) {
		r := NewSmithingTransform(stick, diamond, stick, diamond, "smithing_table")
		assertRecipe(t, r, []Item{stick, diamond, stick}, []item.Stack{diamond}, "smithing_table")
	})
	t.Run("SmithingTrim", func(t *testing.T) {
		// A trim recipe has inputs but no output: the trim is applied to the
		// base item rather than producing a new one.
		r := NewSmithingTrim(stick, diamond, stick, "smithing_table")
		assertRecipe(t, r, []Item{stick, diamond, stick}, nil, "smithing_table")
	})
	t.Run("Potion", func(t *testing.T) {
		r := NewPotion(stick, diamond, diamond)
		assertRecipe(t, r, []Item{stick, diamond}, []item.Stack{diamond}, "brewing_stand")
	})
	t.Run("PotionContainerChange", func(t *testing.T) {
		// The reagent is the second input; the output item is wrapped in a
		// stack of one rather than being taken as a stack.
		r := NewPotionContainerChange(item.Stick{}, item.Diamond{}, diamond)
		if got := len(r.Input()); got != 2 {
			t.Fatalf("len(Input()) = %v, want 2", got)
		}
		if _, ok := r.Input()[0].(item.Stack).Item().(item.Stick); !ok {
			t.Fatalf("Input()[0] = %T, want a Stick", r.Input()[0])
		}
		if !reflect.DeepEqual(r.Input()[1], Item(diamond)) {
			t.Fatal("Input()[1] is not the reagent")
		}
		if got := r.Output()[0].Count(); got != 1 {
			t.Fatalf("Output()[0].Count() = %v, want 1", got)
		}
		if got := r.Block(); got != "brewing_stand" {
			t.Fatalf("Block() = %q, want %q", got, "brewing_stand")
		}
	})
	t.Run("DecoratedPotRecipe", func(t *testing.T) {
		if got := NewDecoratedPotRecipe().Block(); got != "crafting_table" {
			t.Fatalf("Block() = %q, want %q", got, "crafting_table")
		}
	})
}

// No constructor sets a priority. Every non-zero priority in the registry comes
// from the vanilla data files.
func TestConstructorsLeavePriorityZero(t *testing.T) {
	stick := item.NewStack(item.Stick{}, 1)
	for _, r := range []Recipe{
		NewShapeless([]Item{stick}, stick, "b"),
		NewUserDataShapeless([]Item{stick}, stick, "b"),
		NewShaped([]Item{stick}, stick, NewShape(1, 1), "b"),
		NewMulti(uuid.Nil),
		NewSmithingTransform(stick, stick, stick, stick, "b"),
		NewSmithingTrim(stick, stick, stick, "b"),
		NewPotion(stick, stick, stick),
	} {
		if got := r.Priority(); got != 0 {
			t.Fatalf("%T.Priority() = %v, want 0", r, got)
		}
	}
}

func assertRecipe(t *testing.T, r Recipe, input []Item, output []item.Stack, block string) {
	t.Helper()
	if len(r.Input()) != len(input) {
		t.Fatalf("len(Input()) = %v, want %v", len(r.Input()), len(input))
	}
	for i := range input {
		// Items are compared with reflect.DeepEqual rather than ==: item.Stack
		// holds maps, so == on a recipe.Item panics at runtime.
		if !reflect.DeepEqual(r.Input()[i], input[i]) {
			t.Fatalf("Input()[%v] = %v, want %v", i, r.Input()[i], input[i])
		}
	}
	if len(r.Output()) != len(output) {
		t.Fatalf("len(Output()) = %v, want %v", len(r.Output()), len(output))
	}
	for i := range output {
		if !r.Output()[i].Equal(output[i]) {
			t.Fatalf("Output()[%v] = %v, want %v", i, r.Output()[i], output[i])
		}
	}
	if r.Block() != block {
		t.Fatalf("Block() = %q, want %q", r.Block(), block)
	}
}

// Input and Output hand out copies: a caller that writes to one cannot edit
// the registered recipe.
func TestInputAndOutputAliasTheRecipe(t *testing.T) {
	stick := item.NewStack(item.Stick{}, 1)
	diamond := item.NewStack(item.Diamond{}, 1)
	r := NewShapeless([]Item{stick}, diamond, "crafting_table")

	r.Input()[0] = diamond
	if !reflect.DeepEqual(r.Input()[0], Item(stick)) {
		t.Fatal("write through Input() leaked into the recipe")
	}
	r.Output()[0] = stick
	if !r.Output()[0].Equal(diamond) {
		t.Fatal("write through Output() leaked into the recipe")
	}
}

func TestRecipesClonesTheSliceOnly(t *testing.T) {
	a, b := Recipes(), Recipes()
	if len(a) == 0 {
		t.Fatal("the registry is empty; registerVanilla did not run")
	}
	a[0] = nil
	if b[0] == nil {
		t.Fatal("Recipes() returned the package slice rather than a clone")
	}
	d := DynamicRecipes()
	if len(d) != 1 {
		t.Fatalf("len(DynamicRecipes()) = %v, want 1", len(d))
	}
	d[0] = nil
	if DynamicRecipes()[0] == nil {
		t.Fatal("DynamicRecipes() returned the package slice rather than a clone")
	}
}

func TestItemTag(t *testing.T) {
	for _, c := range []struct {
		name  string
		tag   string
		count int
		want  int
		empty bool
	}{
		{"known tag", "minecraft:planks", 1, 1, false},
		{"count zero", "minecraft:planks", 0, 0, true},
		{"negative count is clamped", "minecraft:planks", -5, 0, true},
		{"empty tag", "", 1, 1, true},
		{"unknown tag", "minecraft:not_a_tag", 1, 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			i := NewItemTag(c.tag, c.count)
			if got := i.Count(); got != c.want {
				t.Fatalf("Count() = %v, want %v", got, c.want)
			}
			if got := i.Empty(); got != c.empty {
				t.Fatalf("Empty() = %v, want %v", got, c.empty)
			}
			if got := i.Tag(); got != c.tag {
				t.Fatalf("Tag() = %q, want %q", got, c.tag)
			}
		})
	}
	t.Run("Contains", func(t *testing.T) {
		planks := NewItemTag("minecraft:planks", 1)
		if !planks.Contains("minecraft:oak_planks") {
			t.Fatal(`Contains("minecraft:oak_planks") = false`)
		}
		if planks.Contains("minecraft:stick") {
			t.Fatal(`Contains("minecraft:stick") = true`)
		}
		// An unknown tag has no items, so Contains is always false even though
		// Empty is not.
		unknown := NewItemTag("minecraft:not_a_tag", 1)
		if unknown.Empty() {
			t.Fatal("Empty() = true for an unknown tag with a count")
		}
		if unknown.Contains("minecraft:oak_planks") {
			t.Fatal("an unknown tag matched an item")
		}
	})
}

// The tag table is embedded data read at init. Its size and a few of its
// contents are pinned so a regenerated item_tags.json is visible.
func TestItemTagTable(t *testing.T) {
	if got, want := len(itemTags), 58; got != want {
		t.Fatalf("len(itemTags) = %v, want %v", got, want)
	}
	for tag, want := range map[string]int{
		"minecraft:planks":                   12,
		"minecraft:logs":                     44,
		"minecraft:digger":                   24,
		"minecraft:is_sword":                 7,
		"minecraft:decorated_pot_sherds":     24,
		"minecraft:stone_crafting_materials": 3,
	} {
		if got := len(itemTags[tag]); got != want {
			t.Fatalf("len(itemTags[%q]) = %v, want %v", tag, got, want)
		}
	}
}

// PINNED BUG: NewItemTag stores the global tag slice by reference, so every tag
// with the same name shares one backing array and a write through any of them
// rewrites the embedded table for the rest of the process.
func TestItemTagAliasesTheGlobalTable(t *testing.T) {
	a := NewItemTag("minecraft:planks", 1)
	original := a.items[0]
	defer func() { a.items[0] = original }()

	a.items[0] = "minecraft:not_a_plank"
	if got := NewItemTag("minecraft:planks", 1).items[0]; got != "minecraft:not_a_plank" {
		t.Fatalf("items[0] = %q: NewItemTag no longer aliases the global table", got)
	}
	if got := itemTags["minecraft:planks"][0]; got != "minecraft:not_a_plank" {
		t.Fatalf("itemTags[...][0] = %q: the write did not reach the global table", got)
	}
}

func TestHashItems(t *testing.T) {
	stick, _ := world.ItemByName("minecraft:stick", 0)
	diamond, _ := world.ItemByName("minecraft:diamond", 0)

	t.Run("order does not matter", func(t *testing.T) {
		if hashItems([]world.Item{stick, diamond}, true) != hashItems([]world.Item{diamond, stick}, true) {
			t.Fatal("the hash depends on the input order")
		}
	})
	t.Run("nil entries are dropped", func(t *testing.T) {
		if hashItems([]world.Item{stick, nil}, true) != hashItems([]world.Item{stick}, true) {
			t.Fatal("a nil input changed the hash")
		}
		if hashItems(nil, true) != "" {
			t.Fatal("a nil slice did not hash to the empty string")
		}
	})
	t.Run("the caller's slice is not reordered", func(t *testing.T) {
		in := []world.Item{diamond, stick}
		hashItems(in, true)
		if in[0] != diamond {
			t.Fatal("hashItems sorted the caller's slice in place")
		}
	})
	t.Run("meta is included only when asked", func(t *testing.T) {
		potionOne, _ := world.ItemByName("minecraft:potion", 1)
		potionTwo, _ := world.ItemByName("minecraft:potion", 2)
		if potionOne == nil || potionTwo == nil {
			t.Fatal("the potion items are not registered")
		}
		if hashItems([]world.Item{potionOne}, true) == hashItems([]world.Item{potionTwo}, true) {
			t.Fatal("the metadata-sensitive hash ignored the metadata")
		}
		if hashItems([]world.Item{potionOne}, false) != hashItems([]world.Item{potionTwo}, false) {
			t.Fatal("the metadata-insensitive hash used the metadata")
		}
	})
	t.Run("names are concatenated without a separator", func(t *testing.T) {
		// Two different item sets can therefore collide. No pair of vanilla
		// names does, but nothing in the hash prevents it.
		if got, want := hashItems([]world.Item{stick, diamond}, false), "minecraft:diamondminecraft:stick"; got != want {
			t.Fatalf("hashItems = %q, want %q", got, want)
		}
	})
}

func TestVanillaRegistryCensus(t *testing.T) {
	counts := map[string]int{}
	for _, r := range vanilla {
		switch r.(type) {
		case Multi:
			counts["Multi"]++
		case Shaped:
			counts["Shaped"]++
		case Shapeless:
			counts["Shapeless"]++
		case UserDataShapeless:
			counts["UserDataShapeless"]++
		case SmithingTransform:
			counts["SmithingTransform"]++
		case SmithingTrim:
			counts["SmithingTrim"]++
		case Potion:
			counts["Potion"]++
		case PotionContainerChange:
			counts["PotionContainerChange"]++
		default:
			t.Fatalf("unexpected recipe type %T in the registry", r)
		}
	}
	// A recipe whose items or blocks do not resolve is skipped without a word,
	// so these counts are the item-only subset that survives in a binary
	// without server/block. Every one of them moves when item coverage changes;
	// they are pinned so that a change in what silently disappears is visible.
	for name, want := range map[string]int{
		"Multi":                 14,
		"Shaped":                70,
		"Shapeless":             184,
		"UserDataShapeless":     0,
		"SmithingTransform":     9,
		"SmithingTrim":          1,
		"Potion":                141,
		"PotionContainerChange": 2,
	} {
		if counts[name] != want {
			t.Fatalf("%v recipes = %v, want %v", name, counts[name], want)
		}
	}
	if got, want := len(index), 1; got != want {
		t.Fatalf("len(index) = %v, want %v: only brewing recipes are indexed", got, want)
	}
	if got, want := vanillaBrewing, 143; got != want {
		t.Fatalf(`len(index["brewing_stand"]) = %v, want %v`, got, want)
	}
	if got, want := vanillaReagents, 13; got != want {
		t.Fatalf("len(reagent) = %v, want %v", got, want)
	}
}

// Only Potion and PotionContainerChange are indexed, so Perform finds nothing
// for any other kind of recipe no matter what is registered.
func TestPerformOnlyKnowsBrewing(t *testing.T) {
	stick, _ := world.ItemByName("minecraft:stick", 0)
	for _, block := range []string{"crafting_table", "smithing_table", "stonecutter", ""} {
		if _, ok := Perform(block, stick); ok {
			t.Fatalf("Perform(%q) found a recipe", block)
		}
	}
}

func TestPerformPotion(t *testing.T) {
	awkward, _ := world.ItemByName("minecraft:potion", 4)
	blazePowder, _ := world.ItemByName("minecraft:blaze_powder", 0)
	gunpowder, _ := world.ItemByName("minecraft:gunpowder", 0)
	stick, _ := world.ItemByName("minecraft:stick", 0)
	if awkward == nil || blazePowder == nil || gunpowder == nil {
		t.Fatal("the brewing items are not registered")
	}

	t.Run("a potion recipe resolves on the metadata-sensitive hash", func(t *testing.T) {
		out, ok := Perform("brewing_stand", awkward, blazePowder)
		if !ok {
			t.Fatal("Perform found no recipe")
		}
		if len(out) != 1 {
			t.Fatalf("len(output) = %v, want 1", len(out))
		}
		name, meta := out[0].Item().EncodeItem()
		if name != "minecraft:potion" || meta != 31 {
			t.Fatalf("output = %v/%v, want minecraft:potion/31", name, meta)
		}
	})
	t.Run("the input order does not matter for matching", func(t *testing.T) {
		out, ok := Perform("brewing_stand", blazePowder, awkward)
		if !ok {
			t.Fatal("Perform found no recipe with the inputs reversed")
		}
		name, meta := out[0].Item().EncodeItem()
		if name != "minecraft:potion" || meta != 31 {
			t.Fatalf("output = %v/%v, want minecraft:potion/31", name, meta)
		}
	})
	t.Run("an unknown block finds nothing", func(t *testing.T) {
		if _, ok := Perform("not_a_block", awkward, blazePowder); ok {
			t.Fatal("Perform found a recipe for an unknown block")
		}
	})
	t.Run("an unknown combination finds nothing", func(t *testing.T) {
		if _, ok := Perform("brewing_stand", stick, stick); ok {
			t.Fatal("Perform found a recipe for an unknown combination")
		}
	})
	t.Run("no inputs finds nothing", func(t *testing.T) {
		if _, ok := Perform("brewing_stand"); ok {
			t.Fatal("Perform found a recipe with no inputs")
		}
	})

	t.Run("a container change re-derives the output metadata", func(t *testing.T) {
		// The recipe is registered on the metadata-insensitive hash, so any
		// potion matches and the output takes the metadata of the input.
		for _, meta := range []int16{0, 4, 31} {
			p, _ := world.ItemByName("minecraft:potion", meta)
			out, ok := Perform("brewing_stand", p, gunpowder)
			if !ok {
				t.Fatalf("Perform found no container change for meta %v", meta)
			}
			name, gotMeta := out[0].Item().EncodeItem()
			if name != "minecraft:splash_potion" || gotMeta != meta {
				t.Fatalf("output = %v/%v, want minecraft:splash_potion/%v", name, gotMeta, meta)
			}
		}
	})
	// PINNED BUG: the container change branch reads input[ind], indexing the
	// caller's argument list by the position of the output rather than by the
	// position of the potion. Matching is order-independent, but the metadata
	// the output inherits is not, so the same two items in the other order
	// brew the wrong potion.
	t.Run("a container change depends on the argument order", func(t *testing.T) {
		p, _ := world.ItemByName("minecraft:potion", 31)
		out, ok := Perform("brewing_stand", gunpowder, p)
		if !ok {
			t.Fatal("Perform found no container change with the inputs reversed")
		}
		name, meta := out[0].Item().EncodeItem()
		if name != "minecraft:splash_potion" || meta != 0 {
			t.Fatalf("output = %v/%v, want minecraft:splash_potion/0", name, meta)
		}
	})
}

func TestValidBrewingReagent(t *testing.T) {
	for _, c := range []struct {
		name string
		want bool
	}{
		{"minecraft:blaze_powder", true},
		{"minecraft:fermented_spider_eye", true},
		{"minecraft:glowstone_dust", true},
		{"minecraft:sugar", true},
		{"minecraft:stick", false},
		{"minecraft:diamond", false},
		// Gunpowder and dragon's breath drive container changes rather than
		// potion mixes, and only potion mixes populate the reagent table.
		{"minecraft:gunpowder", false},
		{"minecraft:dragon_breath", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			it, ok := world.ItemByName(c.name, 0)
			if !ok {
				t.Fatalf("%v is not registered", c.name)
			}
			if got := ValidBrewingReagent(it); got != c.want {
				t.Fatalf("ValidBrewingReagent(%v) = %v, want %v", c.name, got, c.want)
			}
		})
	}
	// PINNED BUG: the argument is dereferenced without a nil check. Its one
	// caller happens to guard the call, so nothing reaches this today.
	t.Run("a nil item panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("ValidBrewingReagent did not panic on a nil item")
			}
		}()
		ValidBrewingReagent(nil)
	})
}

func TestRegisterAppendsWithoutDeduplicating(t *testing.T) {
	before := len(Recipes())
	r := NewShapeless([]Item{item.NewStack(item.Stick{}, 1)}, item.NewStack(item.Diamond{}, 1), "crafting_table")
	Register(r)
	Register(r)
	if got, want := len(Recipes()), before+2; got != want {
		t.Fatalf("len(Recipes()) = %v, want %v: the same recipe was deduplicated", got, want)
	}
	// A non-brewing recipe is appended and nothing else: it never reaches the
	// lookup index.
	if _, ok := Perform("crafting_table", item.Stick{}); ok {
		t.Fatal("a shapeless recipe became reachable through Perform")
	}
}

func TestRegisterIndexIsLastWins(t *testing.T) {
	input := item.NewStack(item.Stick{}, 1)
	// The reagent is an emerald rather than one of the items asserted on in
	// TestValidBrewingReagent: registering it is permanent, so a reused item
	// would make that test's expectations depend on execution order.
	reagentStack := item.NewStack(item.Emerald{}, 1)
	first := NewPotion(input, reagentStack, item.NewStack(item.Diamond{}, 1))
	second := NewPotion(input, reagentStack, item.NewStack(item.GoldIngot{}, 1))
	Register(first)
	Register(second)

	out, ok := Perform("brewing_stand", item.Stick{}, item.Emerald{})
	if !ok {
		t.Fatal("Perform found no recipe")
	}
	if _, ok := out[0].Item().(item.GoldIngot); !ok {
		t.Fatalf("output = %T, want the second registration to win", out[0].Item())
	}
	// The reagent table is keyed by name, so it is last-wins too, and it now
	// reports an emerald as a brewing reagent.
	if !ValidBrewingReagent(item.Emerald{}) {
		t.Fatal("the reagent table did not pick up the registration")
	}
}

// A Potion is indexed under the metadata-sensitive hash and a
// PotionContainerChange under the metadata-insensitive one, so the same pair of
// items can land in both. Perform tries the metadata-sensitive key first, which
// decides which of the two wins. No vanilla pair collides, so nothing else
// makes that precedence observable.
func TestPerformPrefersTheMetadataSensitiveHash(t *testing.T) {
	base, reagentItem := item.NetherStar{}, item.Feather{}
	Register(NewPotion(
		item.NewStack(base, 1),
		item.NewStack(reagentItem, 1),
		item.NewStack(item.Diamond{}, 1),
	))
	Register(NewPotionContainerChange(base, item.GoldIngot{}, item.NewStack(reagentItem, 1)))

	out, ok := Perform("brewing_stand", base, reagentItem)
	if !ok {
		t.Fatal("Perform found no recipe")
	}
	if _, ok := out[0].Item().(item.Diamond); !ok {
		t.Fatalf("output = %T, want the potion recipe to win over the container change", out[0].Item())
	}
}

// recipe.Item is satisfied by item.Stack, which holds maps and is therefore not
// comparable. Comparing two Items with == compiles and panics at runtime, so
// any matching built on it has to type switch first.
func TestItemsAreNotComparable(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("comparing two item.Stack recipe items did not panic")
		}
	}()
	var a, b Item = item.NewStack(item.Stick{}, 1), item.NewStack(item.Stick{}, 1)
	if a == b {
		t.Fatal("unreachable")
	}
}

// PINNED BUG: Register indexes a Potion's reagent through Input()[1] with no
// length check and asserts it to an item.Stack with no comma-ok. A Potion built
// any other way than by NewPotion with two stacks takes the whole process down.
func TestRegisterPanicsOnAMalformedPotion(t *testing.T) {
	for _, c := range []struct {
		name string
		r    Recipe
	}{
		{"zero value has no inputs", Potion{}},
		{"a tag reagent is not a stack", NewPotion(item.NewStack(item.Stick{}, 1), NewItemTag("minecraft:planks", 1), item.NewStack(item.Diamond{}, 1))},
		{"an empty reagent has no item", NewPotion(item.NewStack(item.Stick{}, 1), item.Stack{}, item.NewStack(item.Diamond{}, 1))},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register did not panic")
				}
			}()
			Register(c.r)
		})
	}
}

func TestDecoratedPotRecipeMatch(t *testing.T) {
	r := NewDecoratedPotRecipe()
	sherd := item.NewStack(item.PotterySherd{Type: item.SherdTypeAngler()}, 1)
	brick := item.NewStack(item.Brick{}, 1)
	empty := item.Stack{}
	stick := item.NewStack(item.Stick{}, 1)

	grid := func(items ...Item) []Item { return items }
	valid := grid(empty, sherd, empty, brick, empty, sherd, empty, brick, empty)

	for _, c := range []struct {
		name  string
		input []Item
	}{
		{"nil input", nil},
		{"too few slots", grid(empty, sherd, empty, brick)},
		{"too many slots", append(append([]Item{}, valid...), empty)},
		{"an even slot is occupied", grid(sherd, sherd, empty, brick, empty, sherd, empty, brick, empty)},
		{"the centre is occupied", grid(empty, sherd, empty, brick, sherd, sherd, empty, brick, empty)},
		{"an odd slot is empty", grid(empty, empty, empty, brick, empty, sherd, empty, brick, empty)},
		{"a decoration is not a decoration", grid(empty, stick, empty, brick, empty, sherd, empty, brick, empty)},
		{"a decoration is a tag", grid(empty, NewItemTag("minecraft:decorated_pot_sherds", 1), empty, brick, empty, sherd, empty, brick, empty)},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, ok := r.Match(c.input)
			if ok {
				t.Fatalf("Match = %v, true, want false", out)
			}
			if out != nil {
				t.Fatalf("Match returned %v alongside false, want nil", out)
			}
		})
	}
	// PINNED CONSTRAINT: the success path needs minecraft:decorated_pot in the
	// block registry, which only server/block registers. This binary does not
	// link it, so a well-formed grid still fails to match. The failure is
	// silent: Match cannot tell "wrong shape" from "the block is missing".
	t.Run("a well formed grid still fails without the block", func(t *testing.T) {
		if _, ok := world.BlockByName("minecraft:decorated_pot", map[string]any{"direction": int32(2)}); ok {
			t.Skip("the decorated pot block is registered in this binary")
		}
		if _, ok := r.Match(valid); ok {
			t.Fatal("Match succeeded without the decorated pot block")
		}
	})
}
