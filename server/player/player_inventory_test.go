package player

import (
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
)

func apples(n int) item.Stack { return item.NewStack(item.Apple{}, n) }

func woodPickaxe() item.Stack {
	return item.NewStack(item.Pickaxe{Tier: item.ToolTierWood}, 1)
}

// heldOf reports the counts of the main hand and the off-hand in one value, so
// a failure message shows both halves of an operation that moves items between
// them.
func heldOf(p *Player) [2]int {
	main, off := p.HeldItems()
	return [2]int{main.Count(), off.Count()}
}

// totalOf counts every item comparable to want across the main inventory, the
// off-hand and the armour, which is what an item loss or duplication shows up
// in.
func totalOf(p *Player, want item.Stack) int {
	n := 0
	for _, s := range append(append(p.inv.Slots(), p.offHand.Slots()...), p.armour.Slots()...) {
		if !s.Empty() && s.Comparable(want) {
			n += s.Count()
		}
	}
	return n
}

// subtractItem is the whole of the count deduction after an item use: a pure
// Grow keyed on the game mode, with no world behind it.
func TestSubtractItemOnlyDeductsOutsideCreative(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode world.GameMode
		sub  int
		want int
	}{
		{name: "survival deducts", mode: world.GameModeSurvival, sub: 3, want: 7},
		{name: "adventure deducts", mode: world.GameModeAdventure, sub: 3, want: 7},
		{name: "spectator deducts", mode: world.GameModeSpectator, sub: 3, want: 7},
		{name: "creative keeps the stack whole", mode: world.GameModeCreative, sub: 3, want: 10},
		{name: "a zero subtraction is a no-op", mode: world.GameModeSurvival, sub: 0, want: 10},
		{name: "a negative subtraction grows the stack", mode: world.GameModeSurvival, sub: -5, want: 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{GameMode: tc.mode})
			if got := p.subtractItem(apples(10), tc.sub).Count(); got != tc.want {
				t.Fatalf("subtractItem(10, %v) = %v, want %v", tc.sub, got, tc.want)
			}
		})
	}
}

// PINNED BUG: a negative CountSub grows the stack instead of shrinking it, and
// handleUseContext feeds item.UseContext.CountSub straight into subtractItem
// with no check. Any item whose Use writes a negative CountSub duplicates
// itself. Inventory.RemoveItemFunc guards the same mistake explicitly
// (inventory.go:295); this path does not. Pinned as-is; decide in stage 1.
func TestNegativeCountSubDuplicatesTheHeldStack(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, apples(10)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	p.handleUseContext(&item.UseContext{CountSub: -22})

	if got := heldOf(p); got[0] != 32 {
		t.Fatalf("held count = %v, want 32: a negative CountSub minted %v apples", got[0], got[0]-10)
	}
}

// damageItem leaves the stack alone on all three of its early returns, and
// those are the only arms reachable without a world: past them it plays a
// breaking sound through the transaction.
func TestDamageItemEarlyReturns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   world.GameMode
		stack  item.Stack
		damage int
	}{
		{name: "creative never damages", mode: world.GameModeCreative, stack: woodPickaxe(), damage: 5},
		{name: "zero damage", mode: world.GameModeSurvival, stack: woodPickaxe(), damage: 0},
		{name: "an item with no durability", mode: world.GameModeSurvival, stack: apples(1), damage: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{GameMode: tc.mode})
			got := p.damageItem(tc.stack, tc.damage)
			if !got.Equal(tc.stack) {
				t.Fatalf("damageItem returned %v, want the stack unchanged (%v)", got, tc.stack)
			}
		})
	}
}

// Past the early returns the handler runs and the durability actually drops.
// The stack must survive the whole way without reaching the breaking sound.
func TestDamageItemAppliesDurabilityLoss(t *testing.T) {
	p := newStubPlayer(Config{})
	pick := woodPickaxe()

	got := p.damageItem(pick, 9)

	if want := pick.MaxDurability() - 9; got.Durability() != want {
		t.Fatalf("durability = %v, want %v", got.Durability(), want)
	}
	if got.Count() != 1 {
		t.Fatalf("count = %v, want 1", got.Count())
	}
}

// Unbreaking is applied inside damageItem, between the handler and the write,
// so its reduction is part of the player's contract rather than the item's.
func TestDamageItemAppliesUnbreaking(t *testing.T) {
	p := newStubPlayer(Config{})
	pick := woodPickaxe().WithEnchantments(item.NewEnchantment(enchantment.Unbreaking, 3))

	got := p.damageItem(pick, 20)

	if got.Durability() >= pick.MaxDurability() {
		t.Fatalf("durability = %v, want less than %v: Unbreaking swallowed the whole hit", got.Durability(), pick.MaxDurability())
	}
	if got.Durability() <= pick.MaxDurability()-20 {
		t.Fatalf("durability = %v, want more than %v: Unbreaking reduced nothing", got.Durability(), pick.MaxDurability()-20)
	}
}

// addNewItem prefers an empty main hand over the inventory, which is what makes
// a crafted or transformed item land where the player is looking.
func TestAddNewItemFillsTheEmptyMainHandFirst(t *testing.T) {
	p := newStubPlayer(Config{})

	p.addNewItem(&item.UseContext{NewItem: apples(3)})

	if got := heldOf(p); got != [2]int{3, 0} {
		t.Fatalf("held = %v, want [3 0]", got)
	}
	if got := totalOf(p, apples(1)); got != 3 {
		t.Fatalf("apples = %v, want 3", got)
	}
}

func TestAddNewItemFallsBackToTheInventory(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, woodPickaxe()); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	p.addNewItem(&item.UseContext{NewItem: apples(3)})

	if got := heldOf(p); got[0] != 1 {
		t.Fatalf("main hand count = %v, want 1: the pickaxe was replaced", got[0])
	}
	if got := totalOf(p, apples(1)); got != 3 {
		t.Fatalf("apples = %v, want 3", got)
	}
}

func TestAddNewItemSkipsTheEmptyAndCreativeOnlyCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode world.GameMode
		ctx  item.UseContext
	}{
		{name: "an empty new item", mode: world.GameModeSurvival, ctx: item.UseContext{}},
		{
			name: "a survival-only new item in creative",
			mode: world.GameModeCreative,
			ctx:  item.UseContext{NewItem: apples(3), NewItemSurvivalOnly: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{GameMode: tc.mode})
			p.addNewItem(&tc.ctx)
			if got := totalOf(p, apples(1)); got != 0 {
				t.Fatalf("apples = %v, want 0", got)
			}
		})
	}
}

// The consumption loop takes the whole stack from one inventory or the other,
// never a share of each, because Inventory.RemoveItem is all or nothing.
func TestHandleUseContextConsumptionPaths(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		offHand, invSlotOne      item.Stack
		consumed                 []item.Stack
		wantOffHand, wantSlotOne int
	}{
		{
			name:    "the off-hand pays when it can",
			offHand: apples(3), invSlotOne: apples(5), consumed: []item.Stack{apples(2)},
			wantOffHand: 1, wantSlotOne: 5,
		},
		{
			name:    "the off-hand pays its exact count",
			offHand: apples(2), invSlotOne: apples(5), consumed: []item.Stack{apples(2)},
			wantOffHand: 0, wantSlotOne: 5,
		},
		{
			name:    "a too-small off-hand pays nothing at all",
			offHand: apples(1), invSlotOne: apples(5), consumed: []item.Stack{apples(2)},
			wantOffHand: 1, wantSlotOne: 3,
		},
		{
			name:    "an off-hand holding something else is skipped",
			offHand: woodPickaxe(), invSlotOne: apples(5), consumed: []item.Stack{apples(2)},
			wantOffHand: 1, wantSlotOne: 3,
		},
		{
			name:    "an empty off-hand is comparable to anything and is tried first",
			offHand: item.Stack{}, invSlotOne: apples(5), consumed: []item.Stack{apples(2)},
			wantOffHand: 0, wantSlotOne: 3,
		},
		{
			name:    "two consumed stacks are charged one after the other",
			offHand: apples(3), invSlotOne: apples(5), consumed: []item.Stack{apples(2), apples(2)},
			wantOffHand: 1, wantSlotOne: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{})
			if !tc.offHand.Empty() {
				if err := p.offHand.SetItem(0, tc.offHand); err != nil {
					t.Fatalf("offHand.SetItem: %v", err)
				}
			}
			if err := p.inv.SetItem(1, tc.invSlotOne); err != nil {
				t.Fatalf("inv.SetItem: %v", err)
			}

			p.handleUseContext(&item.UseContext{ConsumedItems: tc.consumed})

			if got := p.offHand.Slots()[0].Count(); got != tc.wantOffHand {
				t.Fatalf("off-hand count = %v, want %v", got, tc.wantOffHand)
			}
			if got := p.inv.Slots()[1].Count(); got != tc.wantSlotOne {
				t.Fatalf("inventory slot 1 count = %v, want %v", got, tc.wantSlotOne)
			}
		})
	}
}

// PINNED BUG: when neither the off-hand nor the inventory can satisfy a
// consumed stack, both RemoveItem calls fail, the second error is discarded at
// player.go:1741, and the player keeps every item the use was supposed to cost.
// Consuming is silently free. Pinned as-is; decide in stage 1.
func TestConsumingMoreThanThePlayerHasCostsNothing(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.offHand.SetItem(0, apples(1)); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}
	if err := p.inv.SetItem(1, apples(2)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	p.handleUseContext(&item.UseContext{ConsumedItems: []item.Stack{apples(10)}})

	if got := totalOf(p, apples(1)); got != 3 {
		t.Fatalf("apples = %v, want 3", got)
	}
}

// The three item.UseContext fields are applied in one fixed order: damage, then
// count subtraction, then the write back to the hands, then the new item, then
// the consumption. Pinning the combination catches a reorder that each field on
// its own would not.
func TestHandleUseContextAppliesEveryFieldInOrder(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.inv.SetItem(0, woodPickaxe()); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}
	if err := p.inv.SetItem(1, apples(5)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}

	p.handleUseContext(&item.UseContext{
		Damage:        4,
		NewItem:       item.NewStack(item.Diamond{}, 2),
		ConsumedItems: []item.Stack{apples(2)},
	})

	main, _ := p.HeldItems()
	if want := woodPickaxe().MaxDurability() - 4; main.Durability() != want {
		t.Fatalf("held durability = %v, want %v", main.Durability(), want)
	}
	if got := totalOf(p, apples(1)); got != 3 {
		t.Fatalf("apples = %v, want 3", got)
	}
	if got := totalOf(p, item.NewStack(item.Diamond{}, 1)); got != 2 {
		t.Fatalf("diamonds = %v, want 2: the new item did not reach the inventory", got)
	}
}

// PINNED BUG: Config.HeldSlot is never range-checked. fillDefaults leaves it
// alone (conf.go:120) and Apply stores it verbatim (conf.go:58), while
// SetHeldSlot rejects anything outside 0-8 (player.go:1487). A Config that
// names a slot the inventory does not have gives a player whose main hand is
// permanently empty and who destroys every item written to it: SetHeldItems
// discards the ErrSlotOutOfRange at player.go:1477, so handleUseContext loses
// the item it was holding. Pinned as-is; decide in stage 1.
func TestAnOutOfRangeHeldSlotSilentlyDestroysTheHeldItem(t *testing.T) {
	p := newStubPlayer(Config{HeldSlot: 40})

	main, _ := p.HeldItems()
	if !main.Empty() {
		t.Fatalf("main hand = %v, want empty", main)
	}

	p.SetHeldItems(apples(5), item.Stack{})
	if got := totalOf(p, apples(1)); got != 0 {
		t.Fatalf("apples = %v, want 0: the write unexpectedly landed somewhere", got)
	}

	// The new item takes the same route, so an item use on such a player mints
	// nothing and loses whatever it produced.
	p.addNewItem(&item.UseContext{NewItem: apples(3)})
	if got := totalOf(p, apples(1)); got != 0 {
		t.Fatalf("apples = %v, want 0", got)
	}
}

// Collect is the item pickup path. The off-hand takes a partial share, unlike
// the all-or-nothing removal in handleUseContext, and the remainder goes to the
// main inventory.
func TestCollectSplitsBetweenTheOffHandAndTheInventory(t *testing.T) {
	p := newStubPlayer(Config{})
	full := apples(1).MaxCount()
	if err := p.offHand.SetItem(0, apples(full-2)); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}

	added, ok := p.Collect(apples(10))

	if !ok || added != 10 {
		t.Fatalf("Collect = (%v, %v), want (10, true)", added, ok)
	}
	if got := p.offHand.Slots()[0].Count(); got != full {
		t.Fatalf("off-hand count = %v, want %v", got, full)
	}
	if got := totalOf(p, apples(1)); got != full+8 {
		t.Fatalf("apples = %v, want %v", got, full+8)
	}
}

func TestCollectSkipsTheOffHandWhenItHoldsSomethingElse(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.offHand.SetItem(0, woodPickaxe()); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}

	added, ok := p.Collect(apples(10))

	if !ok || added != 10 {
		t.Fatalf("Collect = (%v, %v), want (10, true)", added, ok)
	}
	if got := p.offHand.Slots()[0].Count(); got != 1 {
		t.Fatalf("off-hand count = %v, want 1: the pickaxe slot was touched", got)
	}
}

// An empty off-hand is Comparable to anything, but Collect gates on
// !offHand.Empty() first, so a free off-hand is never filled by a pickup.
func TestCollectNeverFillsAnEmptyOffHand(t *testing.T) {
	p := newStubPlayer(Config{})

	added, ok := p.Collect(apples(4))

	if !ok || added != 4 {
		t.Fatalf("Collect = (%v, %v), want (4, true)", added, ok)
	}
	if got := p.offHand.Slots()[0].Count(); got != 0 {
		t.Fatalf("off-hand count = %v, want 0", got)
	}
}

func TestCollectIsRefusedWhenDeadOrSpectating(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    Config
		before func(*Player)
	}{
		{name: "spectator", cfg: Config{GameMode: world.GameModeSpectator}},
		{
			name:   "dead",
			cfg:    Config{},
			before: func(p *Player) { p.health.AddHealth(-20) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(tc.cfg)
			if tc.before != nil {
				tc.before(p)
			}
			if added, ok := p.Collect(apples(4)); added != 0 || ok {
				t.Fatalf("Collect = (%v, %v), want (0, false)", added, ok)
			}
			if got := totalOf(p, apples(1)); got != 0 {
				t.Fatalf("apples = %v, want 0", got)
			}
		})
	}
}

// A full inventory takes what it can and reports it. The caller is told how
// many were taken, not that the pickup failed.
func TestCollectReportsAPartialPickupAsASuccess(t *testing.T) {
	full := inventory.New(1, nil)
	if err := full.SetItem(0, item.NewStack(item.Diamond{}, item.NewStack(item.Diamond{}, 1).MaxCount())); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	p := newStubPlayer(Config{Inventory: full})

	added, ok := p.Collect(apples(4))

	if added != 0 || !ok {
		t.Fatalf("Collect = (%v, %v), want (0, true)", added, ok)
	}
}

// MoveItemsToInventory empties the temporary crafting slots into the main
// inventory. Only the overflow needs the world, so the move itself is pinnable.
func TestMoveItemsToInventoryEmptiesTheUISlots(t *testing.T) {
	p := newStubPlayer(Config{})
	if err := p.ui.SetItem(0, apples(3)); err != nil {
		t.Fatalf("ui.SetItem: %v", err)
	}
	if err := p.ui.SetItem(7, woodPickaxe()); err != nil {
		t.Fatalf("ui.SetItem: %v", err)
	}

	p.MoveItemsToInventory()

	for _, s := range p.ui.Slots() {
		if !s.Empty() {
			t.Fatalf("ui still holds %v", s)
		}
	}
	if got := totalOf(p, apples(1)); got != 3 {
		t.Fatalf("apples = %v, want 3", got)
	}
	if got := totalOf(p, woodPickaxe()); got != 1 {
		t.Fatalf("pickaxes = %v, want 1", got)
	}
}

// dropItems is the death drop. It clears the main inventory, the armour and the
// off-hand and drops what came out, except for stacks carrying the Curse of
// Vanishing, which are destroyed instead. Every stack here is cursed, so no
// entity is spawned and the whole path runs without a world.
//
// Curse of Vanishing only applies to durable items, so every fixture below is
// one; an apple would silently drop the enchantment in WithEnchantments and
// take the test into Tx.AddEntity.
func TestDropItemsClearsAllThreeInventories(t *testing.T) {
	p := newStubPlayer(Config{})
	curse := item.NewEnchantment(enchantment.CurseOfVanishing, 1)
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1).WithEnchantments(curse)

	if err := p.inv.SetItem(0, woodPickaxe().WithEnchantments(curse)); err != nil {
		t.Fatalf("inv.SetItem: %v", err)
	}
	shovel := item.NewStack(item.Shovel{Tier: item.ToolTierWood}, 1).WithEnchantments(curse)
	if err := p.offHand.SetItem(0, shovel); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}
	p.armour.SetHelmet(helmet)

	p.dropItems()

	for name, slots := range map[string][]item.Stack{
		"inventory": p.inv.Slots(),
		"off-hand":  p.offHand.Slots(),
		"armour":    p.armour.Slots(),
	} {
		for i, s := range slots {
			if !s.Empty() {
				t.Fatalf("%v slot %v still holds %v after the death drop", name, i, s)
			}
		}
	}
	if p.experience.Experience() != 0 {
		t.Fatalf("experience = %v, want 0", p.experience.Experience())
	}
}
