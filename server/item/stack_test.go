package item

import (
	"slices"
	"testing"

	"github.com/jv2w/Impulse/server/world"
)

// The fixtures below are shared with nbt_test.go.

// testEnchant is an EnchantmentType with no behaviour beyond what Stack reads
// from it. It has to stay comparable: RegisterEnchantment and Stack both use
// EnchantmentType values as map keys, so a func or slice field would panic at
// runtime.
type testEnchant struct {
	name     string
	maxLevel int
	// swordOnly restricts CompatibleWithItem to Sword, exercising the item
	// compatibility branch of Stack.WithEnchantments.
	swordOnly bool
	// rejects is the Name of an enchantment this one refuses to sit beside,
	// exercising the enchantment compatibility branch.
	rejects string
}

func (e testEnchant) Name() string              { return e.name }
func (e testEnchant) MaxLevel() int             { return e.maxLevel }
func (e testEnchant) Cost(level int) (int, int) { return level, level * 2 }
func (e testEnchant) Rarity() EnchantmentRarity { return EnchantmentRarityCommon }
func (e testEnchant) CompatibleWithItem(i world.Item) bool {
	if !e.swordOnly {
		return true
	}
	_, ok := i.(Sword)
	return ok
}

func (e testEnchant) CompatibleWithEnchantment(t EnchantmentType) bool {
	return e.rejects == "" || t.Name() != e.rejects
}

var (
	// enchA and enchB are registered with IDs in the reverse of their
	// declaration order so that Stack.Enchantments cannot pass by accident.
	enchA = testEnchant{name: "TestA", maxLevel: 3}
	enchB = testEnchant{name: "TestB", maxLevel: 5}
	// enchSword only accepts Sword items.
	enchSword = testEnchant{name: "TestSword", maxLevel: 1, swordOnly: true}
	// enchNoA refuses to be applied alongside enchA.
	enchNoA = testEnchant{name: "TestNoA", maxLevel: 2, rejects: "TestA"}
	// enchUnreg is deliberately never registered.
	enchUnreg = testEnchant{name: "TestUnregistered", maxLevel: 1}
)

// Enchantment IDs are registered in init rather than in a test body because
// the registry is a package global: a test body would re-register on every
// iteration of go test -count=n.
func init() {
	RegisterEnchantment(enchAID, enchA)
	RegisterEnchantment(enchBID, enchB)
	RegisterEnchantment(enchSwordID, enchSword)
	RegisterEnchantment(enchNoAID, enchNoA)
}

const (
	enchAID     = 111
	enchBID     = 110
	enchSwordID = 112
	enchNoAID   = 113
)

// airItem carries the one item name Stack.Empty treats as empty regardless of
// count. It is not registered: NewStack does not require registration.
type airItem struct{}

func (airItem) EncodeItem() (string, int16) { return "minecraft:air", 0 }

func TestStackEmpty(t *testing.T) {
	for _, c := range []struct {
		name  string
		stack Stack
		empty bool
	}{
		{"zero value", Stack{}, true},
		{"count zero", NewStack(Stick{}, 0), true},
		{"nil item with count", Stack{count: 3}, true},
		{"air with count", NewStack(airItem{}, 64), true},
		{"air with count zero", NewStack(airItem{}, 0), true},
		{"one stick", NewStack(Stick{}, 1), false},
		{"full stack", NewStack(Stick{}, 64), false},
		{"count over max", NewStack(Stick{}, 1000), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.stack.Empty(); got != c.empty {
				t.Fatalf("Empty() = %v, want %v", got, c.empty)
			}
			if c.empty && c.stack.Item() != nil {
				t.Fatalf("Item() on an empty stack = %v, want nil", c.stack.Item())
			}
		})
	}
}

func TestNewStackPanics(t *testing.T) {
	t.Run("negative count", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("NewStack with a negative count did not panic")
			}
		}()
		NewStack(Stick{}, -1)
	})
	t.Run("nil item", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("NewStack with a nil item did not panic")
			}
		}()
		NewStack(nil, 1)
	})
}

func TestStackMaxCount(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
		want int
	}{
		{"default", NewStack(Stick{}, 1), 64},
		{"MaxCounter sword", NewStack(Sword{Tier: ToolTierWood}, 1), 1},
		{"MaxCounter written book", NewStack(WrittenBook{}, 1), 16},
		{"zero value stack", Stack{}, 64},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.MaxCount(); got != c.want {
				t.Fatalf("MaxCount() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStackGrow(t *testing.T) {
	for _, c := range []struct {
		name  string
		start int
		n     int
		want  int
	}{
		{"zero", 5, 0, 5},
		{"positive", 5, 3, 8},
		{"negative", 5, -3, 2},
		{"to exactly zero", 5, -5, 0},
		{"clamped at zero", 5, -100, 0},
		// Grow does not clamp at MaxCount: only AddStack and inventories do.
		{"past max count", 64, 10, 74},
		{"far past max count", 1, 1 << 30, 1<<30 + 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := NewStack(Stick{}, c.start)
			grown := s.Grow(c.n)
			if got := grown.Count(); got != c.want {
				t.Fatalf("Grow(%v).Count() = %v, want %v", c.n, got, c.want)
			}
			if got := s.Count(); got != c.start {
				t.Fatalf("Grow mutated the receiver: Count() = %v, want %v", got, c.start)
			}
			if grown.id == s.id {
				t.Fatal("Grow reused the stack ID; it must issue a new one")
			}
		})
	}
}

func TestStackGrowOnSwordKeepsMaxCountOne(t *testing.T) {
	s := NewStack(Sword{Tier: ToolTierIron}, 1).Grow(5)
	if got, want := s.Count(), 6; got != want {
		t.Fatalf("Count() = %v, want %v", got, want)
	}
	if got, want := s.MaxCount(), 1; got != want {
		t.Fatalf("MaxCount() = %v, want %v", got, want)
	}
}

func TestStackAddStack(t *testing.T) {
	stick := func(n int) Stack { return NewStack(Stick{}, n) }
	for _, c := range []struct {
		name           string
		a, b           Stack
		wantA, wantB   int
		wantAItemEmpty bool
	}{
		{name: "fits entirely", a: stick(10), b: stick(20), wantA: 30, wantB: 0},
		{name: "exactly full", a: stick(60), b: stick(4), wantA: 64, wantB: 0},
		{name: "leftover", a: stick(60), b: stick(10), wantA: 64, wantB: 6},
		{name: "already full", a: stick(64), b: stick(10), wantA: 64, wantB: 10},
		{name: "over full", a: stick(70), b: stick(10), wantA: 70, wantB: 10},
		{name: "incomparable", a: stick(1), b: NewStack(Diamond{}, 1), wantA: 1, wantB: 1},
		{name: "second empty", a: stick(1), b: Stack{}, wantA: 1, wantB: 0},
		{
			// A Stack with no item type has nothing to hold b's items, so both
			// come back untouched.
			name: "first has no item type", a: Stack{}, b: stick(5),
			wantA: 0, wantB: 5, wantAItemEmpty: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, b := c.a.AddStack(c.b)
			if got := a.Count(); got != c.wantA {
				t.Fatalf("a.Count() = %v, want %v", got, c.wantA)
			}
			if got := b.Count(); got != c.wantB {
				t.Fatalf("b.Count() = %v, want %v", got, c.wantB)
			}
			if got := a.Empty(); got != c.wantAItemEmpty {
				t.Fatalf("a.Empty() = %v, want %v", got, c.wantAItemEmpty)
			}
			if got := c.a.Count(); got != c.a.Count() || c.a.Count() == 0 && a.Count() != 0 && !c.wantAItemEmpty {
				t.Fatalf("AddStack mutated its receiver")
			}
		})
	}
}

// A count on a Stack with no item type is a count with nothing behind it.
// Comparable waves such a stack through against anything, so AddStack has to
// reject it in both directions: moving the count in destroys the items, moving
// it out creates items from nothing.
func TestStackAddStackRejectsStacksWithoutAnItemType(t *testing.T) {
	t.Run("a stack with no item type absorbs nothing", func(t *testing.T) {
		a, b := (Stack{}).AddStack(NewStack(Stick{}, 5))
		if got := a.Count(); got != 0 {
			t.Fatalf("a.Count() = %v, want 0: items moved into a stack that cannot hold them", got)
		}
		if got := b.Count(); got != 5 {
			t.Fatalf("b.Count() = %v, want 5: the items were destroyed", got)
		}
	})
	t.Run("a stack with no item type yields nothing", func(t *testing.T) {
		a, b := NewStack(Stick{}, 1).AddStack((Stack{}).Grow(5))
		if got := a.Count(); got != 1 {
			t.Fatalf("a.Count() = %v, want 1: %v items were created from nothing", got, got-1)
		}
		if got := b.Count(); got != 5 {
			t.Fatalf("b.Count() = %v, want 5", got)
		}
	})
}

// inventory.AddItem fills an empty slot with it.Grow(-math.MaxInt32).AddStack(it):
// a zero-count Stack that still carries its item type. Empty reports true for
// that stack, so the rejection above has to key on the item type, not on Empty.
func TestStackAddStackFillsAZeroCountStackOfTheSameType(t *testing.T) {
	a, b := NewStack(Stick{}, 5).Grow(-5).AddStack(NewStack(Stick{}, 5))
	if got := a.Count(); got != 5 {
		t.Fatalf("a.Count() = %v, want 5", got)
	}
	if got := b.Count(); got != 0 {
		t.Fatalf("b.Count() = %v, want 0", got)
	}
}

func TestStackAddStackRespectsMaxCounter(t *testing.T) {
	a, b := NewStack(Sword{Tier: ToolTierIron}, 1).AddStack(NewStack(Sword{Tier: ToolTierIron}, 1))
	if got, want := a.Count(), 1; got != want {
		t.Fatalf("a.Count() = %v, want %v", got, want)
	}
	if got, want := b.Count(), 1; got != want {
		t.Fatalf("b.Count() = %v, want %v", got, want)
	}
}

func TestStackComparable(t *testing.T) {
	sword := func() Stack { return NewStack(Sword{Tier: ToolTierIron}, 1) }
	for _, c := range []struct {
		name string
		a, b Stack
		want bool
	}{
		{"identical", NewStack(Stick{}, 1), NewStack(Stick{}, 1), true},
		{"count ignored", NewStack(Stick{}, 1), NewStack(Stick{}, 64), true},
		{"different item", NewStack(Stick{}, 1), NewStack(Diamond{}, 1), false},
		{
			"different meta",
			NewStack(Helmet{Tier: ArmourTierIron{}}, 1),
			NewStack(Helmet{Tier: ArmourTierGold{}}, 1),
			false,
		},
		{"damage ignored", sword().Damage(5), sword(), true},

		{"same custom name", NewStack(Stick{}, 1).WithCustomName("a"), NewStack(Stick{}, 1).WithCustomName("a"), true},
		{"different custom name", NewStack(Stick{}, 1).WithCustomName("a"), NewStack(Stick{}, 1).WithCustomName("b"), false},
		{"custom name against none", NewStack(Stick{}, 1).WithCustomName("a"), NewStack(Stick{}, 1), false},

		{"same lore", NewStack(Stick{}, 1).WithLore("a", "b"), NewStack(Stick{}, 1).WithLore("a", "b"), true},
		{"different lore", NewStack(Stick{}, 1).WithLore("a", "b"), NewStack(Stick{}, 1).WithLore("a", "c"), false},
		{"shorter lore", NewStack(Stick{}, 1).WithLore("a", "b"), NewStack(Stick{}, 1).WithLore("a"), false},
		{"lore against none", NewStack(Stick{}, 1).WithLore("a"), NewStack(Stick{}, 1), false},

		{"same anvil cost", sword().WithAnvilCost(3), sword().WithAnvilCost(3), true},
		{"different anvil cost", sword().WithAnvilCost(3), sword().WithAnvilCost(4), false},
		{"anvil cost against none", sword().WithAnvilCost(3), sword(), false},

		{
			"same enchantments",
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1)),
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1)),
			true,
		},
		{
			"different enchantment level",
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1)),
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 2)),
			false,
		},
		{
			"different enchantment type",
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1)),
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchB, 1)),
			false,
		},
		{
			"different enchantment count",
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1), NewEnchantment(enchB, 1)),
			NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1)),
			false,
		},

		{"same value", NewStack(Stick{}, 1).WithValue("k", 1), NewStack(Stick{}, 1).WithValue("k", 1), true},
		{"different value", NewStack(Stick{}, 1).WithValue("k", 1), NewStack(Stick{}, 1).WithValue("k", 2), false},
		{"different value key", NewStack(Stick{}, 1).WithValue("k", 1), NewStack(Stick{}, 1).WithValue("j", 1), false},
		{"value against none", NewStack(Stick{}, 1).WithValue("k", 1), NewStack(Stick{}, 1), false},
		{"cleared value equals none", NewStack(Stick{}, 1).WithValue("k", 1).WithValue("k", nil), NewStack(Stick{}, 1), true},

		{"same NBTer", NewStack(WrittenBook{Title: "t"}, 1), NewStack(WrittenBook{Title: "t"}, 1), true},
		{"different NBTer", NewStack(WrittenBook{Title: "t"}, 1), NewStack(WrittenBook{Title: "u"}, 1), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.Comparable(c.b); got != c.want {
				t.Fatalf("a.Comparable(b) = %v, want %v", got, c.want)
			}
			if got := c.b.Comparable(c.a); got != c.want {
				t.Fatalf("Comparable is not symmetric: b.Comparable(a) = %v, want %v", got, c.want)
			}
		})
	}
}

// PINNED BUG: Comparable short-circuits to true whenever either stack is
// empty, so an empty Stack compares equal to every stack in existence. This is
// the root of item/inventory's First, ContainsItem and RemoveItem accepting a
// zero Stack as a wildcard. Pinned as-is; decide in stage 1.
func TestStackComparableEmptyIsWildcard(t *testing.T) {
	for _, c := range []struct {
		name  string
		other Stack
	}{
		{"stick", NewStack(Stick{}, 64)},
		{"diamond", NewStack(Diamond{}, 1)},
		{"named enchanted sword", NewStack(Sword{Tier: ToolTierNetherite}, 1).
			WithCustomName("x").WithLore("y").WithAnvilCost(9).
			WithEnchantments(NewEnchantment(enchA, 3))},
		{"another empty", Stack{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !(Stack{}).Comparable(c.other) {
				t.Fatal("an empty stack must currently compare equal to everything")
			}
			if !c.other.Comparable(Stack{}) {
				t.Fatal("every stack must currently compare equal to an empty stack")
			}
		})
	}
}

// PINNED BUG: stack.go:424 spells the lore check as `for !slices.Equal(...)`
// where an `if` was meant. The body returns unconditionally so the loop can
// never run twice, but the construct is a trap for anyone editing it. Pinned
// as-is; decide in stage 1.
func TestStackComparableLoreLoopTerminates(t *testing.T) {
	a := NewStack(Stick{}, 1).WithLore("a")
	b := NewStack(Stick{}, 1).WithLore("b")
	done := make(chan bool, 1)
	go func() { done <- a.Comparable(b) }()
	select {
	case got := <-done:
		if got {
			t.Fatal("Comparable with differing lore = true, want false")
		}
	case <-t.Context().Done():
		t.Fatal("Comparable did not return: the lore loop is spinning")
	}
}

func TestStackComparableIgnoresUnbreakable(t *testing.T) {
	sword := NewStack(Sword{Tier: ToolTierIron}, 1)
	if !sword.AsUnbreakable().Comparable(sword) {
		t.Fatal("Comparable currently ignores the unbreakable tag; that behaviour changed")
	}
}

func TestStackEqual(t *testing.T) {
	sword := func() Stack { return NewStack(Sword{Tier: ToolTierIron}, 1) }
	for _, c := range []struct {
		name string
		a, b Stack
		want bool
	}{
		{"identical", NewStack(Stick{}, 3), NewStack(Stick{}, 3), true},
		{"different count", NewStack(Stick{}, 3), NewStack(Stick{}, 4), false},
		{"different damage", sword().Damage(5), sword(), false},
		{"same damage", sword().Damage(5), sword().Damage(5), true},
		{"different item", NewStack(Stick{}, 1), NewStack(Diamond{}, 1), false},
		// Equal inherits the empty wildcard from Comparable, but the count check
		// still has to hold, so only equal counts pass.
		{"empty against non-empty", Stack{}, NewStack(Stick{}, 1), false},
		{"empty against zero count", Stack{}, NewStack(Stick{}, 0), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.Equal(c.b); got != c.want {
				t.Fatalf("a.Equal(b) = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStackWithCustomName(t *testing.T) {
	for _, c := range []struct {
		name string
		args []any
		want string
	}{
		{"single", []any{"hello"}, "hello"},
		{"joined with spaces", []any{"a", "b", "c"}, "a b c"},
		{"non strings", []any{1, true}, "1 true"},
		{"no arguments clears", nil, ""},
		{"empty string", []any{""}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := NewStack(Stick{}, 1)
			named := s.WithCustomName(c.args...)
			if got := named.CustomName(); got != c.want {
				t.Fatalf("CustomName() = %q, want %q", got, c.want)
			}
			if got := s.CustomName(); got != "" {
				t.Fatalf("WithCustomName mutated the receiver: CustomName() = %q", got)
			}
		})
	}
}

func TestStackWithCustomNameOnEmpty(t *testing.T) {
	// An empty stack still records a custom name even though Item is nil.
	s := (Stack{}).WithCustomName("x")
	if got, want := s.CustomName(), "x"; got != want {
		t.Fatalf("CustomName() = %q, want %q", got, want)
	}
}

func TestStackWithLore(t *testing.T) {
	s := NewStack(Stick{}, 1)
	lored := s.WithLore("a", "b")
	if got, want := lored.Lore(), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("Lore() = %v, want %v", got, want)
	}
	if got := s.Lore(); len(got) != 0 {
		t.Fatalf("WithLore mutated the receiver: Lore() = %v", got)
	}
	if got := lored.WithLore().Lore(); len(got) != 0 {
		t.Fatalf("WithLore() with no lines did not clear the lore: %v", got)
	}
	if got := (Stack{}).WithLore("a").Lore(); got != nil {
		t.Fatalf("Lore() on an empty stack = %v, want nil", got)
	}
}

// Stack is a value type and clones the maps it holds on every write. The lore
// slice has to be copied on the way in and on the way out for the same reason:
// otherwise the caller keeps a handle on the stack's lore through the slice it
// passed to WithLore or read back from Lore, and WithItem hands that same
// handle to a second stack.
func TestStackLoreIsCopiedInAndOut(t *testing.T) {
	t.Run("the argument is copied", func(t *testing.T) {
		lines := []string{"original"}
		s := NewStack(Stick{}, 1).WithLore(lines...)
		lines[0] = "rewritten"
		if got, want := s.Lore()[0], "original"; got != want {
			t.Fatalf("Lore()[0] = %q, want %q: WithLore aliases its argument", got, want)
		}
	})
	t.Run("the returned slice is a copy", func(t *testing.T) {
		s := NewStack(Stick{}, 1).WithLore("original")
		s.Lore()[0] = "rewritten"
		if got, want := s.Lore()[0], "original"; got != want {
			t.Fatalf("Lore()[0] = %q, want %q: Lore aliases the stack", got, want)
		}
	})
	t.Run("copies do not share lore", func(t *testing.T) {
		a := NewStack(Stick{}, 1).WithLore("original")
		b := a.WithItem(Diamond{})
		a.Lore()[0] = "rewritten"
		if got, want := b.Lore()[0], "original"; got != want {
			t.Fatalf("Lore()[0] = %q, want %q: WithItem shares the lore slice", got, want)
		}
	})
}

func TestStackWithValue(t *testing.T) {
	s := NewStack(Stick{}, 1).WithValue("a", 1)

	if v, ok := s.Value("a"); !ok || v != 1 {
		t.Fatalf(`Value("a") = %v, %v, want 1, true`, v, ok)
	}
	if v, ok := s.Value("missing"); ok || v != nil {
		t.Fatalf(`Value("missing") = %v, %v, want nil, false`, v, ok)
	}

	two := s.WithValue("b", 2)
	if _, ok := s.Value("b"); ok {
		t.Fatal("WithValue mutated the receiver's data map")
	}
	if v, ok := two.Value("a"); !ok || v != 1 {
		t.Fatalf(`Value("a") after a second WithValue = %v, %v, want 1, true`, v, ok)
	}

	cleared := s.WithValue("a", nil)
	if _, ok := cleared.Value("a"); ok {
		t.Fatal("WithValue with a nil value did not clear the key")
	}
	if cleared.data != nil {
		t.Fatalf("clearing the last value left a non-nil data map: %v", cleared.data)
	}
	if v, ok := s.Value("a"); !ok || v != 1 {
		t.Fatalf("clearing on a copy mutated the receiver: Value(\"a\") = %v, %v", v, ok)
	}

	if v, ok := (Stack{}).WithValue("a", 1).Value("a"); ok || v != nil {
		t.Fatalf("Value on an empty stack = %v, %v, want nil, false", v, ok)
	}
}

func TestStackValues(t *testing.T) {
	s := NewStack(Stick{}, 1).WithValue("a", 1)
	v := s.Values()
	if len(v) != 1 || v["a"] != 1 {
		t.Fatalf("Values() = %v, want map[a:1]", v)
	}
	v["a"] = 2
	v["b"] = 3
	if got, _ := s.Value("a"); got != 1 {
		t.Fatal("Values() returned the stack's own map, not a copy")
	}
	if _, ok := s.Value("b"); ok {
		t.Fatal("Values() returned the stack's own map, not a copy")
	}
	if got := (Stack{}).Values(); got != nil {
		t.Fatalf("Values() on an empty stack = %v, want nil", got)
	}
	if got := NewStack(Stick{}, 1).Values(); got != nil {
		t.Fatalf("Values() with no data = %v, want nil", got)
	}
}

func TestNewEnchantmentPanicsBelowLevelOne(t *testing.T) {
	for _, lvl := range []int{0, -1, -1 << 31} {
		defer func() {
			if recover() == nil {
				t.Fatalf("NewEnchantment with level %v did not panic", lvl)
			}
		}()
		NewEnchantment(enchA, lvl)
	}
}

// PINNED BUG: NewEnchantment documents that it panics when the level exceeds
// EnchantmentType.MaxLevel, but it only rejects levels below 1. Any level above
// MaxLevel is accepted and stored verbatim. Pinned as-is; decide in stage 1.
func TestNewEnchantmentIgnoresMaxLevel(t *testing.T) {
	e := NewEnchantment(enchA, enchA.MaxLevel()+100)
	if got, want := e.Level(), enchA.MaxLevel()+100; got != want {
		t.Fatalf("Level() = %v, want %v", got, want)
	}
	if got := e.Type(); got != EnchantmentType(enchA) {
		t.Fatalf("Type() = %v, want %v", got, enchA)
	}
}

func TestStackWithEnchantments(t *testing.T) {
	t.Run("applied and retrievable", func(t *testing.T) {
		s := NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 2))
		e, ok := s.Enchantment(enchA)
		if !ok {
			t.Fatal("Enchantment(enchA) reported not found")
		}
		if got, want := e.Level(), 2; got != want {
			t.Fatalf("Level() = %v, want %v", got, want)
		}
	})
	t.Run("receiver untouched", func(t *testing.T) {
		s := NewStack(Stick{}, 1)
		s.WithEnchantments(NewEnchantment(enchA, 1))
		if _, ok := s.Enchantment(enchA); ok {
			t.Fatal("WithEnchantments mutated the receiver's enchantment map")
		}
	})
	t.Run("map is not shared with the copy", func(t *testing.T) {
		a := NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1))
		b := a.WithEnchantments(NewEnchantment(enchB, 1))
		if _, ok := a.Enchantment(enchB); ok {
			t.Fatal("the copy wrote into the original's enchantment map")
		}
		if _, ok := b.Enchantment(enchA); !ok {
			t.Fatal("the copy lost the original's enchantments")
		}
	})
	t.Run("re-applying overwrites the level", func(t *testing.T) {
		s := NewStack(Stick{}, 1).
			WithEnchantments(NewEnchantment(enchA, 1)).
			WithEnchantments(NewEnchantment(enchA, 3))
		if got := len(s.Enchantments()); got != 1 {
			t.Fatalf("len(Enchantments()) = %v, want 1", got)
		}
		e, _ := s.Enchantment(enchA)
		if got, want := e.Level(), 3; got != want {
			t.Fatalf("Level() = %v, want %v", got, want)
		}
	})
	t.Run("incompatible with the item is dropped", func(t *testing.T) {
		s := NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchSword, 1))
		if _, ok := s.Enchantment(enchSword); ok {
			t.Fatal("an item-incompatible enchantment was applied")
		}
		sword := NewStack(Sword{Tier: ToolTierIron}, 1).WithEnchantments(NewEnchantment(enchSword, 1))
		if _, ok := sword.Enchantment(enchSword); !ok {
			t.Fatal("an item-compatible enchantment was dropped")
		}
	})
	t.Run("incompatible with another enchantment is dropped", func(t *testing.T) {
		s := NewStack(Stick{}, 1).
			WithEnchantments(NewEnchantment(enchA, 1)).
			WithEnchantments(NewEnchantment(enchNoA, 1))
		if _, ok := s.Enchantment(enchNoA); ok {
			t.Fatal("an enchantment-incompatible enchantment was applied")
		}
		// The rejection is symmetric: it holds whichever side is applied first.
		r := NewStack(Stick{}, 1).
			WithEnchantments(NewEnchantment(enchNoA, 1)).
			WithEnchantments(NewEnchantment(enchA, 1))
		if _, ok := r.Enchantment(enchA); ok {
			t.Fatal("the rejection is not symmetric")
		}
	})
	t.Run("books become enchanted books", func(t *testing.T) {
		s := NewStack(Book{}, 1).WithEnchantments(NewEnchantment(enchSword, 1))
		if _, ok := s.Item().(EnchantedBook); !ok {
			t.Fatalf("Item() = %T, want EnchantedBook", s.Item())
		}
		// An enchanted book accepts enchantments its item type would reject.
		if _, ok := s.Enchantment(enchSword); !ok {
			t.Fatal("an enchanted book rejected a sword-only enchantment")
		}
	})
	t.Run("no enchantments is a no-op", func(t *testing.T) {
		s := NewStack(Book{}, 1).WithEnchantments()
		if _, ok := s.Item().(EnchantedBook); !ok {
			t.Fatalf("Item() = %T, want EnchantedBook: the book conversion is unconditional", s.Item())
		}
	})
}

func TestStackWithForcedEnchantments(t *testing.T) {
	s := NewStack(Stick{}, 1).WithForcedEnchantments(
		NewEnchantment(enchSword, 1),
		NewEnchantment(enchA, 1),
		NewEnchantment(enchNoA, 1),
	)
	for _, e := range []EnchantmentType{enchSword, enchA, enchNoA} {
		if _, ok := s.Enchantment(e); !ok {
			t.Fatalf("WithForcedEnchantments dropped %v", e.Name())
		}
	}
	// Unlike WithEnchantments, the forced variant leaves a Book a Book.
	b := NewStack(Book{}, 1).WithForcedEnchantments(NewEnchantment(enchA, 1))
	if _, ok := b.Item().(Book); !ok {
		t.Fatalf("Item() = %T, want Book", b.Item())
	}
}

func TestStackWithoutEnchantments(t *testing.T) {
	s := NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1), NewEnchantment(enchB, 1))

	one := s.WithoutEnchantments(enchA)
	if _, ok := one.Enchantment(enchA); ok {
		t.Fatal("WithoutEnchantments did not remove the enchantment")
	}
	if _, ok := one.Enchantment(enchB); !ok {
		t.Fatal("WithoutEnchantments removed an enchantment it was not given")
	}
	if _, ok := s.Enchantment(enchA); !ok {
		t.Fatal("WithoutEnchantments mutated the receiver")
	}
	if got := len(s.WithoutEnchantments(enchUnreg).Enchantments()); got != 2 {
		t.Fatalf("removing an absent enchantment changed the count to %v, want 2", got)
	}

	book := NewStack(Book{}, 1).WithEnchantments(NewEnchantment(enchA, 1), NewEnchantment(enchB, 1))
	if _, ok := book.WithoutEnchantments(enchA).Item().(EnchantedBook); !ok {
		t.Fatal("a partially disenchanted book stopped being an EnchantedBook")
	}
	plain := book.WithoutEnchantments(enchA, enchB)
	if _, ok := plain.Item().(Book); !ok {
		t.Fatalf("Item() = %T, want Book once the last enchantment is removed", plain.Item())
	}
}

func TestStackEnchantmentsOrder(t *testing.T) {
	// enchB is registered under a lower ID than enchA, so a sort by ID has to
	// reverse the order they were applied in.
	s := NewStack(Stick{}, 1).WithEnchantments(NewEnchantment(enchA, 1), NewEnchantment(enchB, 1))
	got := s.Enchantments()
	if len(got) != 2 {
		t.Fatalf("len(Enchantments()) = %v, want 2", len(got))
	}
	if got[0].Type() != EnchantmentType(enchB) || got[1].Type() != EnchantmentType(enchA) {
		t.Fatalf("Enchantments() = %v, %v, want %v, %v first", got[0].Type().Name(), got[1].Type().Name(), enchB.Name(), enchA.Name())
	}
	if got := (Stack{}).Enchantments(); got != nil {
		t.Fatalf("Enchantments() on an empty stack = %v, want nil", got)
	}
	if _, ok := (Stack{}).Enchantment(enchA); ok {
		t.Fatal("Enchantment on an empty stack reported found")
	}
	if got := NewStack(Stick{}, 1).Enchantments(); len(got) != 0 {
		t.Fatalf("Enchantments() with none applied = %v, want empty", got)
	}
}

// An unregistered EnchantmentType resolves to ID 0 in the sort, so it always
// leads. Pinned to catch a change in how unregistered types are ordered.
func TestStackEnchantmentsUnregisteredSortsFirst(t *testing.T) {
	s := NewStack(Stick{}, 1).WithForcedEnchantments(
		NewEnchantment(enchA, 1),
		NewEnchantment(enchUnreg, 1),
	)
	if got := s.Enchantments()[0].Type(); got != EnchantmentType(enchUnreg) {
		t.Fatalf("Enchantments()[0] = %v, want the unregistered type", got.Name())
	}
}

func TestStackDurability(t *testing.T) {
	for _, c := range []struct {
		name          string
		s             Stack
		durability    int
		maxDurability int
	}{
		{"not durable", NewStack(Stick{}, 1), -1, -1},
		{"empty", Stack{}, -1, -1},
		{"fresh iron sword", NewStack(Sword{Tier: ToolTierIron}, 1), 250, 250},
		{"damaged iron sword", NewStack(Sword{Tier: ToolTierIron}, 1).Damage(10), 240, 250},
		{"fresh elytra", NewStack(Elytra{}, 1), 433, 433},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.Durability(); got != c.durability {
				t.Fatalf("Durability() = %v, want %v", got, c.durability)
			}
			if got := c.s.MaxDurability(); got != c.maxDurability {
				t.Fatalf("MaxDurability() = %v, want %v", got, c.maxDurability)
			}
		})
	}
}

func TestStackDamage(t *testing.T) {
	sword := func() Stack { return NewStack(Sword{Tier: ToolTierWood}, 1) } // 59 durability

	t.Run("not durable", func(t *testing.T) {
		s := NewStack(Stick{}, 1)
		if got := s.Damage(10); !got.Equal(s) {
			t.Fatal("Damage on a non-durable item did not return the stack unchanged")
		}
	})
	t.Run("unbreakable", func(t *testing.T) {
		s := sword().AsUnbreakable()
		if got := s.Damage(10).Durability(); got != 59 {
			t.Fatalf("Durability() = %v, want 59: an unbreakable item took damage", got)
		}
	})
	for _, c := range []struct {
		name           string
		d              int
		wantDurability int
		wantEmpty      bool
	}{
		{"zero", 0, 59, false},
		{"partial", 10, 49, false},
		{"one from breaking", 58, 1, false},
		{"exactly breaking", 59, 0, true},
		{"past breaking", 1000, 0, true},
		{"negative on a fresh item clamps", -10, 59, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := sword().Damage(c.d)
			if got.Empty() != c.wantEmpty {
				t.Fatalf("Empty() = %v, want %v", got.Empty(), c.wantEmpty)
			}
			if c.wantEmpty {
				return
			}
			if got.Durability() != c.wantDurability {
				t.Fatalf("Durability() = %v, want %v", got.Durability(), c.wantDurability)
			}
		})
	}
	t.Run("repair is clamped to the max", func(t *testing.T) {
		s := sword().Damage(30)
		if got, want := s.Durability(), 29; got != want {
			t.Fatalf("Durability() = %v, want %v", got, want)
		}
		if got, want := s.Damage(-10).Durability(), 39; got != want {
			t.Fatalf("Durability() after a partial repair = %v, want %v", got, want)
		}
		if got, want := s.Damage(-1000).Durability(), 59; got != want {
			t.Fatalf("Durability() after an over-repair = %v, want %v", got, want)
		}
	})
	t.Run("persistent items survive breaking", func(t *testing.T) {
		s := NewStack(Elytra{}, 1).Damage(433)
		if s.Empty() {
			t.Fatal("a persistent item broke")
		}
		// The damage is dropped entirely rather than clamped to one short of
		// breaking: the elytra comes back at full durability.
		if got, want := s.Durability(), 433; got != want {
			t.Fatalf("Durability() = %v, want %v", got, want)
		}
	})
	t.Run("receiver untouched", func(t *testing.T) {
		s := sword()
		s.Damage(10)
		if got, want := s.Durability(), 59; got != want {
			t.Fatalf("Damage mutated the receiver: Durability() = %v, want %v", got, want)
		}
	})
}

func TestStackWithDurability(t *testing.T) {
	sword := func() Stack { return NewStack(Sword{Tier: ToolTierWood}, 1) } // 59 durability
	for _, c := range []struct {
		name      string
		d         int
		want      int
		wantEmpty bool
	}{
		{"full", 59, 59, false},
		{"partial", 10, 10, false},
		{"one", 1, 1, false},
		{"zero breaks the item", 0, 0, true},
		{"above the max is clamped", 1000, 59, false},
		// PINNED BUG: a negative durability is neither rejected nor clamped;
		// the stack ends up reporting a durability below zero while still
		// being a usable item. Pinned as-is; decide in stage 1.
		{"negative is stored verbatim", -5, -5, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := sword().WithDurability(c.d)
			if got.Empty() != c.wantEmpty {
				t.Fatalf("Empty() = %v, want %v", got.Empty(), c.wantEmpty)
			}
			if c.wantEmpty {
				return
			}
			if got.Durability() != c.want {
				t.Fatalf("Durability() = %v, want %v", got.Durability(), c.want)
			}
		})
	}
	t.Run("not durable", func(t *testing.T) {
		s := NewStack(Stick{}, 1)
		if got := s.WithDurability(5); !got.Equal(s) {
			t.Fatal("WithDurability on a non-durable item did not return the stack unchanged")
		}
	})
	t.Run("persistent items still break", func(t *testing.T) {
		// Unlike Damage, WithDurability(0) does not consult Persistent.
		if got := NewStack(Elytra{}, 1).WithDurability(0); !got.Empty() {
			t.Fatalf("WithDurability(0) on a persistent item = %v, want an empty stack", got)
		}
	})
}

func TestStackUnbreakable(t *testing.T) {
	sword := NewStack(Sword{Tier: ToolTierIron}, 1)
	if sword.Unbreakable() {
		t.Fatal("a fresh sword reported unbreakable")
	}
	u := sword.AsUnbreakable()
	if !u.Unbreakable() {
		t.Fatal("AsUnbreakable did not set the tag")
	}
	if sword.Unbreakable() {
		t.Fatal("AsUnbreakable mutated the receiver")
	}
	if u.AsBreakable().Unbreakable() {
		t.Fatal("AsBreakable did not clear the tag")
	}

	stick := NewStack(Stick{}, 1)
	if stick.AsUnbreakable().Unbreakable() {
		t.Fatal("AsUnbreakable applied to a non-durable item")
	}
	if (Stack{}).AsUnbreakable().Unbreakable() {
		t.Fatal("AsUnbreakable applied to an empty stack")
	}
}

func TestStackAnvilCost(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
		set  int
		want int
	}{
		{"repairable sword", NewStack(Sword{Tier: ToolTierIron}, 1), 5, 5},
		{"enchanted book", NewStack(EnchantedBook{}, 1), 5, 5},
		{"plain item is ignored", NewStack(Stick{}, 1), 5, 0},
		{"plain book is ignored", NewStack(Book{}, 1), 5, 0},
		{"empty stack is ignored", Stack{}, 5, 0},
		{"zero", NewStack(Sword{Tier: ToolTierIron}, 1), 0, 0},
		{"negative is stored verbatim", NewStack(Sword{Tier: ToolTierIron}, 1), -3, -3},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := c.s.WithAnvilCost(c.set)
			if got.AnvilCost() != c.want {
				t.Fatalf("AnvilCost() = %v, want %v", got.AnvilCost(), c.want)
			}
			if c.s.AnvilCost() != 0 {
				t.Fatal("WithAnvilCost mutated the receiver")
			}
		})
	}
}

func TestStackWithItem(t *testing.T) {
	t.Run("carries everything applicable", func(t *testing.T) {
		src := NewStack(Sword{Tier: ToolTierIron}, 3).
			Damage(10).
			WithCustomName("name").
			WithLore("a", "b").
			WithEnchantments(NewEnchantment(enchA, 2)).
			WithAnvilCost(4).
			WithValue("k", 7).
			AsUnbreakable()

		got := src.WithItem(Sword{Tier: ToolTierDiamond})
		if _, ok := got.Item().(Sword); !ok {
			t.Fatalf("Item() = %T, want Sword", got.Item())
		}
		if got.Count() != 3 {
			t.Fatalf("Count() = %v, want 3", got.Count())
		}
		if got.CustomName() != "name" {
			t.Fatalf("CustomName() = %q, want %q", got.CustomName(), "name")
		}
		if want := []string{"a", "b"}; !slices.Equal(got.Lore(), want) {
			t.Fatalf("Lore() = %v, want %v", got.Lore(), want)
		}
		if e, ok := got.Enchantment(enchA); !ok || e.Level() != 2 {
			t.Fatalf("Enchantment(enchA) = %v, %v, want level 2, true", e, ok)
		}
		if got.AnvilCost() != 4 {
			t.Fatalf("AnvilCost() = %v, want 4", got.AnvilCost())
		}
		if v, ok := got.Value("k"); !ok || v != 7 {
			t.Fatalf(`Value("k") = %v, %v, want 7, true`, v, ok)
		}
		if !got.Unbreakable() {
			t.Fatal("Unbreakable() = false, want true")
		}
		// Damage carries over as a raw damage value, so the durability shifts
		// with the new tier's maximum.
		if want := 1561 - 10; got.Durability() != want {
			t.Fatalf("Durability() = %v, want %v", got.Durability(), want)
		}
	})
	t.Run("drops what the new item cannot hold", func(t *testing.T) {
		src := NewStack(Sword{Tier: ToolTierIron}, 1).Damage(10).WithAnvilCost(4).AsUnbreakable()
		got := src.WithItem(Stick{})
		if got.AnvilCost() != 0 {
			t.Fatalf("AnvilCost() = %v, want 0 on a non-repairable item", got.AnvilCost())
		}
		if got.Durability() != -1 {
			t.Fatalf("Durability() = %v, want -1", got.Durability())
		}
		// PINNED BUG: the unbreakable guard reads s.MaxDurability(), the
		// durability of the item being replaced, not of the item replacing it.
		// A stick therefore keeps an unbreakable tag no item type can honour.
		// Pinned as-is; decide in stage 1.
		if !got.Unbreakable() {
			t.Fatal("Unbreakable() = false: WithItem no longer carries the tag onto a non-durable item")
		}
		// Going the other way the guard reads correctly, because the source is
		// the non-durable one.
		if NewStack(Stick{}, 1).AsUnbreakable().WithItem(Sword{Tier: ToolTierIron}).Unbreakable() {
			t.Fatal("Unbreakable() = true, want false: the source was never unbreakable")
		}
	})
	t.Run("receiver untouched", func(t *testing.T) {
		src := NewStack(Sword{Tier: ToolTierIron}, 1).WithCustomName("name")
		src.WithItem(Stick{})
		if _, ok := src.Item().(Sword); !ok {
			t.Fatal("WithItem mutated the receiver's item")
		}
	})
	t.Run("data map is shared with the copy", func(t *testing.T) {
		// WithItem assigns the data map by reference. Every exported writer
		// clones before writing, so this is currently unobservable through the
		// API; pinned so a future writer that skips the clone is caught.
		src := NewStack(Stick{}, 1).WithValue("k", 1)
		got := src.WithItem(Diamond{})
		if len(got.data) == 0 {
			t.Fatal("WithItem dropped the data map")
		}
		got.data["k"] = 2
		if v, _ := src.Value("k"); v != 2 {
			t.Fatal("WithItem no longer shares the data map with the copy")
		}
	})
}

func TestStackAttackDamage(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
		want float64
	}{
		{"empty", Stack{}, 1},
		{"not a weapon", NewStack(Stick{}, 1), 1},
		// Weapon damage is reported one higher than the item's own value.
		{"wooden sword", NewStack(Sword{Tier: ToolTierWood}, 1), 5},
		{"netherite sword", NewStack(Sword{Tier: ToolTierNetherite}, 1), 9},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.AttackDamage(); got != c.want {
				t.Fatalf("AttackDamage() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStackString(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Stack
		want string
	}{
		{"nil item", Stack{}, "Stack<nil> x0"},
		{
			"plain stick",
			NewStack(Stick{}, 2),
			"Stack<item.Stick{}>(custom name='', lore='[]', damage=0, anvilCost=0) x2",
		},
		{
			"decorated sword",
			NewStack(Sword{Tier: ToolTierWood}, 1).WithCustomName("n").WithLore("l").WithAnvilCost(3).Damage(4),
			"Stack<item.Sword{Tier:{HarvestLevel:1 BaseMiningEfficiency:2 BaseAttackDamage:1 EnchantmentValue:15 Durability:59 Name:wooden}}>(custom name='n', lore='[l]', damage=4, anvilCost=3) x1",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.String(); got != c.want {
				t.Fatalf("String() =\n%v\nwant\n%v", got, c.want)
			}
		})
	}
}

func TestStackIDs(t *testing.T) {
	a := NewStack(Stick{}, 1)
	b := NewStack(Stick{}, 1)
	if a.id == b.id {
		t.Fatal("two stacks were issued the same ID")
	}
	if got := id(Stack{}); got != 0 {
		t.Fatalf("id of an empty stack = %v, want 0", got)
	}
	if got := id(a); got != a.id {
		t.Fatalf("id() = %v, want %v", got, a.id)
	}
	// Only Grow and AddStack issue new IDs; the other With methods keep theirs.
	if got := a.WithCustomName("x"); got.id != a.id {
		t.Fatal("WithCustomName issued a new ID")
	}
	x, y := a.AddStack(b)
	if x.id == a.id || y.id == b.id {
		t.Fatal("AddStack did not issue new IDs")
	}
}
