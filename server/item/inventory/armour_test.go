package inventory

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
)

// This file pins the current behaviour of Armour. Several of the behaviours
// pinned here are defects; they are marked with PINNED BUG and are pinned
// as-is so that a change to them shows up as a failing test rather than as a
// silent behaviour change.

// armEps is the tolerance used when comparing damage reduction results. The
// reduction formula multiplies by 0.04, which has no exact binary
// representation, so results are never bit-exact.
const armEps = 1e-9

// armNear reports whether a and b are within armEps of each other.
func armNear(a, b float64) bool {
	return math.Abs(a-b) <= armEps
}

// armPieces returns the helmet, chestplate, leggings and boots of the tier
// passed, in armour slot order.
func armPieces(tier item.ArmourTier) (item.Stack, item.Stack, item.Stack, item.Stack) {
	return item.NewStack(item.Helmet{Tier: tier}, 1),
		item.NewStack(item.Chestplate{Tier: tier}, 1),
		item.NewStack(item.Leggings{Tier: tier}, 1),
		item.NewStack(item.Boots{Tier: tier}, 1)
}

// armEquip sets a full set of armour and verifies every piece landed, so that
// a later failure cannot be blamed on the setup silently being rejected.
func armEquip(t *testing.T, a *Armour, h, c, l, b item.Stack) {
	t.Helper()

	a.Set(h, c, l, b)
	assertSlots(t, a.Inventory(), h, c, l, b)
}

// armDamageCall is one recorded DamageFunc invocation.
type armDamageCall struct {
	s item.Stack
	d int
}

// armDamageRecorder records every DamageFunc call. transform, when non-nil,
// decides the stack handed back to the caller; otherwise the stack passed in is
// returned unchanged.
type armDamageRecorder struct {
	calls     []armDamageCall
	transform func(s item.Stack, d int) item.Stack
}

// fn is the DamageFunc to pass to Damage or ThornsDamage.
func (r *armDamageRecorder) fn(s item.Stack, d int) item.Stack {
	r.calls = append(r.calls, armDamageCall{s: s, d: d})
	if r.transform != nil {
		return r.transform(s, d)
	}
	return s
}

// TestArmourCanAddArmour pins the validator that every write into an Armour
// inventory passes through: which item type is accepted in which slot. The
// matrix is exhaustive over the four armour pieces, two non-armour items, a
// zero stack and a typed but empty stack, against slots -1 through 4.
func TestArmourCanAddArmour(t *testing.T) {
	slots := []int{-1, 0, 1, 2, 3, 4}

	for _, c := range []struct {
		name string
		it   item.Stack
		// want holds the expected result per entry of slots.
		want []bool
	}{
		//                              -1     0      1      2      3      4
		{"helmet", helmet(), []bool{false, true, false, false, false, false}},
		{"chestplate", chestplate(), []bool{false, false, true, false, false, false}},
		{"leggings", leggings(), []bool{false, false, false, true, false, false}},
		{"boots", boots(), []bool{false, false, false, false, true, false}},
		{"diamonds", diamonds(1), []bool{false, false, false, false, false, false}},
		{"sticks", sticks(1), []bool{false, false, false, false, false, false}},
		// An empty stack short-circuits before the slot switch is reached, so
		// it is accepted in every slot, out-of-range slots included.
		{"zero stack", item.Stack{}, []bool{true, true, true, true, true, true}},
		{"empty diamonds", diamonds(0), []bool{true, true, true, true, true, true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			for i, slot := range slots {
				if got := canAddArmour(c.it, slot); got != c.want[i] {
					t.Errorf("canAddArmour(%v, %v): got %v, want %v", c.name, slot, got, c.want[i])
				}
			}
		})
	}
}

// TestArmourNewArmour pins the construction contract: a nil SlotFunc is
// accepted, the inventory has exactly four slots, all of them start empty and
// the armour validator is installed.
func TestArmourNewArmour(t *testing.T) {
	a := NewArmour(nil)

	if got := a.Inventory().Size(); got != 4 {
		t.Fatalf("Size(): got %v, want 4", got)
	}
	assertSlots(t, a.Inventory(), item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{})

	// A nil SlotFunc is replaced by New, so setting a piece must not panic.
	a.SetHelmet(helmet())
	assertStack(t, a.Helmet(), helmet())

	// The validator is installed by NewArmour, not by New.
	a.SetHelmet(boots())
	assertStack(t, a.Helmet(), helmet())
}

// TestArmourSettersAndGetters pins that each setter writes to its own slot and
// each getter reads it back, and that a setter fully overwrites the slot
// instead of merging into it.
func TestArmourSettersAndGetters(t *testing.T) {
	r := &recorder{}
	a := NewArmour(r.fn)

	h, c, l, b := helmet(), chestplate(), leggings(), boots()
	a.SetHelmet(h)
	a.SetChestplate(c)
	a.SetLeggings(l)
	a.SetBoots(b)

	assertStack(t, a.Helmet(), h)
	assertStack(t, a.Chestplate(), c)
	assertStack(t, a.Leggings(), l)
	assertStack(t, a.Boots(), b)
	assertSlots(t, a.Inventory(), h, c, l, b)

	if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
		t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
	}

	// Overwriting a slot replaces its contents and fires the SlotFunc again,
	// with the previous piece as the before value.
	r.reset()
	gold := item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1)
	a.SetHelmet(gold)
	assertStack(t, a.Helmet(), gold)

	calls := r.calling()
	if len(calls) != 1 {
		t.Fatalf("SlotFunc calls: got %v, want 1", len(calls))
	}
	assertStack(t, calls[0].before, h)
	assertStack(t, calls[0].after, gold)

	// Clearing a slot through its setter is allowed: an empty stack passes the
	// validator.
	r.reset()
	a.SetChestplate(item.Stack{})
	assertStack(t, a.Chestplate(), item.Stack{})
	if got := r.count(); got != 1 {
		t.Fatalf("SlotFunc calls after clearing: got %v, want 1", got)
	}
}

// armSlotsEqual reports whether two slot index slices hold the same values in
// the same order.
func armSlotsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestArmourRejectionIsSilent pins that a rejected write is indistinguishable
// from a successful one for the caller.
//
// PINNED BUG: A write rejected by the armour validator is silent in three
// ways at once. Inventory.SetItem returns a nil error, the Set* helpers
// discard even that error with `_ =`, and no SlotFunc fires. A caller has no
// way at all to learn that the item it handed over was dropped on the floor.
func TestArmourRejectionIsSilent(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(a *Armour)
		slot int
	}{
		{"boots into helmet slot", func(a *Armour) { a.SetHelmet(boots()) }, 0},
		{"helmet into chestplate slot", func(a *Armour) { a.SetChestplate(helmet()) }, 1},
		{"chestplate into leggings slot", func(a *Armour) { a.SetLeggings(chestplate()) }, 2},
		{"leggings into boots slot", func(a *Armour) { a.SetBoots(leggings()) }, 3},
		{"diamonds into helmet slot", func(a *Armour) { a.SetHelmet(diamonds(1)) }, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{}
			a := NewArmour(r.fn)

			c.set(a)

			assertSlots(t, a.Inventory(), item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{})
			if got := r.count(); got != 0 {
				t.Fatalf("SlotFunc calls: got %v, want 0", got)
			}
		})
	}

	// The same rejection through Inventory.SetItem reports success.
	r := &recorder{}
	a := NewArmour(r.fn)
	if err := a.Inventory().SetItem(0, boots()); err != nil {
		t.Fatalf("SetItem(0, boots): got error %v, want nil", err)
	}
	assertStack(t, a.Helmet(), item.Stack{})
	if got := r.count(); got != 0 {
		t.Fatalf("SlotFunc calls: got %v, want 0", got)
	}

	// An out-of-range slot is the one failure that is reported, so a nil error
	// really does mean "in range" and nothing more.
	if err := a.Inventory().SetItem(4, helmet()); !errors.Is(err, ErrSlotOutOfRange) {
		t.Fatalf("SetItem(4, helmet): got error %v, want ErrSlotOutOfRange", err)
	}
	if err := a.Inventory().SetItem(-1, helmet()); !errors.Is(err, ErrSlotOutOfRange) {
		t.Fatalf("SetItem(-1, helmet): got error %v, want ErrSlotOutOfRange", err)
	}
}

// TestArmourSet pins that Set is four sequential setters rather than one
// atomic assignment, and pins the SlotFunc calls it produces.
func TestArmourSet(t *testing.T) {
	t.Run("fully valid", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)

		h, c, l, b := helmet(), chestplate(), leggings(), boots()
		a.Set(h, c, l, b)

		assertSlots(t, a.Inventory(), h, c, l, b)
		if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})

	t.Run("partially rejected", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)

		// PINNED BUG: Set is not atomic. The rejected chestplate does not stop
		// or roll back the surrounding pieces, so the caller is left with a
		// half-applied set and no error.
		h, l, b := helmet(), leggings(), boots()
		a.Set(h, boots(), l, b)

		assertSlots(t, a.Inventory(), h, item.Stack{}, l, b)
		if got, want := r.slots(), []int{0, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})

	t.Run("all empty still fires four callbacks", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)

		// There is no change detection: writing an empty stack over an empty
		// slot still reports a slot change.
		a.Set(item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{})

		if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
		for _, call := range r.calling() {
			assertStack(t, call.before, item.Stack{})
			assertStack(t, call.after, item.Stack{})
		}
	})

	t.Run("overwrites rather than merges", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)
		armEquip(t, a, helmet(), chestplate(), leggings(), boots())

		r.reset()
		c := chestplate()
		a.Set(item.Stack{}, c, item.Stack{}, item.Stack{})

		assertSlots(t, a.Inventory(), item.Stack{}, c, item.Stack{}, item.Stack{})
		if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})
}

// TestArmourCloneDropsValidator pins that the armour validator is a property of
// the Inventory instance and is not carried over by Clone.
//
// PINNED BUG: Inventory.Clone installs an unconditional always-true validator,
// so the clone of an Armour inventory accepts boots in the helmet slot. Any
// code that clones armour to inspect or transfer it produces an inventory that
// no longer enforces the armour contract.
func TestArmourCloneDropsValidator(t *testing.T) {
	a := NewArmour(nil)
	h, c, l, b := helmet(), chestplate(), leggings(), boots()
	armEquip(t, a, h, c, l, b)

	clone := a.Inventory().Clone(nil)

	if got := clone.Size(); got != 4 {
		t.Fatalf("clone Size(): got %v, want 4", got)
	}
	assertSlots(t, clone, h, c, l, b)

	// The clone accepts what the original rejects.
	bad := boots()
	if err := clone.SetItem(0, bad); err != nil {
		t.Fatalf("clone SetItem(0, boots): got error %v, want nil", err)
	}
	assertSlot(t, clone, 0, bad)

	// The original is unaffected in both contents and validation.
	assertStack(t, a.Helmet(), h)
	a.SetHelmet(boots())
	assertStack(t, a.Helmet(), h)
}

// TestArmourAddItem pins that Inventory.AddItem walks past the slots the armour
// validator refuses instead of consuming the stack in them, and reports what it
// actually stored.
func TestArmourAddItem(t *testing.T) {
	for _, c := range []struct {
		name    string
		it      item.Stack
		wantN   int
		wantErr bool
		// wantSlots is the full slot content expected afterwards.
		wantSlots []item.Stack
	}{
		{
			name: "non-armour fits nowhere", it: diamonds(5), wantN: 0, wantErr: true,
			wantSlots: []item.Stack{{}, {}, {}, {}},
		},
		{
			name: "boots skip the first three slots", it: boots(), wantN: 1,
			wantSlots: []item.Stack{{}, {}, {}, boots()},
		},
		{
			name: "a helmet lands in slot 0", it: helmet(), wantN: 1,
			wantSlots: []item.Stack{helmet(), {}, {}, {}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{}
			a := NewArmour(r.fn)

			n, err := a.Inventory().AddItem(c.it)
			if (err != nil) != c.wantErr {
				t.Fatalf("AddItem: got error %v, want error: %v", err, c.wantErr)
			}
			if n != c.wantN {
				t.Fatalf("AddItem count: got %v, want %v", n, c.wantN)
			}
			assertSlots(t, a.Inventory(), c.wantSlots...)
		})
	}
}

// TestArmourDamageAmount pins the item damage computed from the hearts of
// damage passed: floor(dmg/4), clamped up to a minimum of 1.
func TestArmourDamageAmount(t *testing.T) {
	for _, c := range []struct {
		dmg  float64
		want int
	}{
		{0, 1},
		{1, 1},
		{3.9, 1},
		{4, 1},
		{7.9, 1},
		{8, 2},
		{11.999, 2},
		{12, 3},
		{100, 25},
		// Negative damage floors below 1 and is clamped, not passed through.
		{-1, 1},
		{-8, 1},
	} {
		t.Run(fmt.Sprintf("dmg=%v", c.dmg), func(t *testing.T) {
			a := NewArmour(nil)
			armEquip(t, a, helmet(), chestplate(), leggings(), boots())

			d := &armDamageRecorder{}
			a.Damage(c.dmg, d.fn)

			if len(d.calls) != 4 {
				t.Fatalf("DamageFunc calls: got %v, want 4", len(d.calls))
			}
			for i, call := range d.calls {
				if call.d != c.want {
					t.Fatalf("DamageFunc call %v damage: got %v, want %v", i, call.d, c.want)
				}
			}
		})
	}
}

// TestArmourDamageVisitsEverySlot pins that Damage hands every slot to the
// DamageFunc in ascending slot order, empty slots included.
func TestArmourDamageVisitsEverySlot(t *testing.T) {
	t.Run("full set", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)
		h, c, l, b := helmet(), chestplate(), leggings(), boots()
		armEquip(t, a, h, c, l, b)

		r.reset()
		d := &armDamageRecorder{}
		a.Damage(8, d.fn)

		if len(d.calls) != 4 {
			t.Fatalf("DamageFunc calls: got %v, want 4", len(d.calls))
		}
		for i, want := range []item.Stack{h, c, l, b} {
			assertStack(t, d.calls[i].s, want)
		}

		// The unchanged stacks are written straight back, and each write is
		// reported as a slot change.
		if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
		assertSlots(t, a.Inventory(), h, c, l, b)
	})

	t.Run("empty armour", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)

		d := &armDamageRecorder{}
		a.Damage(8, d.fn)

		// Empty slots are damaged too: the DamageFunc is called four times
		// with an empty stack and each empty write-back is reported.
		if len(d.calls) != 4 {
			t.Fatalf("DamageFunc calls: got %v, want 4", len(d.calls))
		}
		for i, call := range d.calls {
			assertStack(t, call.s, item.Stack{})
			if call.d != 2 {
				t.Fatalf("DamageFunc call %v damage: got %v, want 2", i, call.d)
			}
		}
		if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})
}

// TestArmourDamageWriteBack pins that whatever the DamageFunc returns is
// written back through the validator, so a DamageFunc may silently fail to
// apply, may durability-damage a piece, or may clear a slot outright.
func TestArmourDamageWriteBack(t *testing.T) {
	t.Run("durability damage lands", func(t *testing.T) {
		a := NewArmour(nil)
		h, c, l, b := helmet(), chestplate(), leggings(), boots()
		armEquip(t, a, h, c, l, b)

		d := &armDamageRecorder{transform: func(s item.Stack, d int) item.Stack {
			return s.Damage(d)
		}}
		a.Damage(8, d.fn)

		for _, p := range []struct {
			name string
			got  item.Stack
			base item.Stack
		}{
			{"helmet", a.Helmet(), h},
			{"chestplate", a.Chestplate(), c},
			{"leggings", a.Leggings(), l},
			{"boots", a.Boots(), b},
		} {
			if want := p.base.MaxDurability() - 2; p.got.Durability() != want {
				t.Errorf("%v durability: got %v, want %v", p.name, p.got.Durability(), want)
			}
		}
	})

	t.Run("invalid result is rejected", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)
		h, c, l, b := helmet(), chestplate(), leggings(), boots()
		armEquip(t, a, h, c, l, b)

		r.reset()
		d := &armDamageRecorder{transform: func(item.Stack, int) item.Stack {
			return diamonds(1)
		}}
		// PINNED BUG: Damage discards the error from SetItem, so a DamageFunc
		// returning something the validator refuses leaves the old piece in
		// place with no indication that the damage was not applied.
		a.Damage(8, d.fn)

		if len(d.calls) != 4 {
			t.Fatalf("DamageFunc calls: got %v, want 4", len(d.calls))
		}
		if got := r.count(); got != 0 {
			t.Fatalf("SlotFunc calls: got %v, want 0", got)
		}
		assertSlots(t, a.Inventory(), h, c, l, b)
	})

	t.Run("only the invalid slot is skipped", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)
		h, c, l, b := helmet(), chestplate(), leggings(), boots()
		armEquip(t, a, h, c, l, b)

		r.reset()
		d := &armDamageRecorder{transform: func(s item.Stack, _ int) item.Stack {
			if _, ok := s.Item().(item.Helmet); ok {
				return diamonds(1)
			}
			return s.Damage(1)
		}}
		a.Damage(4, d.fn)

		if got, want := r.slots(), []int{1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
		if got := a.Helmet().Durability(); got != h.MaxDurability() {
			t.Fatalf("helmet durability: got %v, want %v", got, h.MaxDurability())
		}
		if got := a.Boots().Durability(); got != b.MaxDurability()-1 {
			t.Fatalf("boots durability: got %v, want %v", got, b.MaxDurability()-1)
		}
	})

	t.Run("empty result clears the slot", func(t *testing.T) {
		r := &recorder{}
		a := NewArmour(r.fn)
		armEquip(t, a, helmet(), chestplate(), leggings(), boots())

		// An empty stack always passes the validator, so a DamageFunc that
		// breaks a piece really does remove it.
		r.reset()
		d := &armDamageRecorder{transform: func(item.Stack, int) item.Stack {
			return item.Stack{}
		}}
		a.Damage(8, d.fn)

		assertSlots(t, a.Inventory(), item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{})
		if got, want := r.slots(), []int{0, 1, 2, 3}; !armSlotsEqual(got, want) {
			t.Fatalf("SlotFunc slots: got %v, want %v", got, want)
		}
	})
}

// TestArmourDamageReduction pins the damage reduction formula for known armour
// sets. Iron defence points are helmet 2, chestplate 6, leggings 5 and boots 2
// for a total of 15, with a toughness of 0.
func TestArmourDamageReduction(t *testing.T) {
	ironH, ironC, ironL, ironB := armPieces(item.ArmourTierIron{})
	diaH, diaC, diaL, diaB := armPieces(item.ArmourTierDiamond{})
	nethH, nethC, nethL, nethB := armPieces(item.ArmourTierNetherite{})
	prot4 := item.NewEnchantment(enchantment.Protection, 4)
	prot6 := item.NewEnchantment(enchantment.Protection, 6)
	thorns3 := item.NewEnchantment(enchantment.Thorns, 3)

	for _, c := range []struct {
		name    string
		pieces  [4]item.Stack
		dmg     float64
		reduced bool
		want    float64
	}{
		// A source not reduced by armour only sees the enchantment term, which
		// is 0 without enchantments.
		{"unenchanted iron, source not reduced by armour", [4]item.Stack{ironH, ironC, ironL, ironB}, 10, false, 0},
		{"empty armour, source not reduced by armour", [4]item.Stack{}, 10, false, 0},
		{"empty armour, source reduced by armour", [4]item.Stack{}, 10, true, 0},
		{"empty armour, zero damage", [4]item.Stack{}, 0, true, 0},

		// 15 defence points, effective points max(15*0.2, 15-dmg/2).
		{"full iron, 1 damage", [4]item.Stack{ironH, ironC, ironL, ironB}, 1, true, 0.58},
		{"full iron, 10 damage", [4]item.Stack{ironH, ironC, ironL, ironB}, 10, true, 4},
		{"full iron, 30 damage clamps to the 20% floor", [4]item.Stack{ironH, ironC, ironL, ironB}, 30, true, 3.6},
		{"full iron, 100 damage clamps to the 20% floor", [4]item.Stack{ironH, ironC, ironL, ironB}, 100, true, 12},
		{"full iron, zero damage", [4]item.Stack{ironH, ironC, ironL, ironB}, 0, true, 0},

		// Toughness only shows up in the divisor: 20 defence points and a
		// toughness of 8 give effective points of 20-10/(2+8/4) = 17.5.
		{"full diamond, 10 damage", [4]item.Stack{diaH, diaC, diaL, diaB}, 10, true, 7},
		// Netherite has the same 20 defence points but a toughness of 12,
		// raising the divisor to 5 and effective points to 18.
		{"full netherite, 10 damage", [4]item.Stack{nethH, nethC, nethL, nethB}, 10, true, 7.2},
		// Toughness delays the 20% floor but does not remove it.
		{"full diamond, 100 damage clamps to the 20% floor", [4]item.Stack{diaH, diaC, diaL, diaB}, 100, true, 16},

		// Only non-empty slots contribute, because DamageReduction walks
		// Items() rather than Slots(). A lone iron helmet is 2 defence points,
		// whose effective value falls to the 20% floor of 0.4 at 10 damage.
		{"iron helmet only", [4]item.Stack{ironH}, 10, true, 0.16},
		{"iron chestplate only", [4]item.Stack{{}, ironC}, 10, true, 0.48},

		// Protection applies to any source whose damage is reduced by
		// resistance, which testSource always is.
		{
			"protection IV on every piece, source not reduced by armour",
			[4]item.Stack{
				ironH.WithEnchantments(prot4), ironC.WithEnchantments(prot4),
				ironL.WithEnchantments(prot4), ironB.WithEnchantments(prot4),
			}, 10, false, 6.4,
		},
		{
			"protection IV on every piece, source reduced by armour",
			[4]item.Stack{
				ironH.WithEnchantments(prot4), ironC.WithEnchantments(prot4),
				ironL.WithEnchantments(prot4), ironB.WithEnchantments(prot4),
			}, 10, true, 8.3008,
		},
		{
			"protection IV on the helmet only",
			[4]item.Stack{ironH.WithEnchantments(prot4)}, 10, false, 1.6,
		},
		// 24 levels of protection would be a factor of 0.96, capped at 0.8.
		{
			"protection factor is capped at 0.8",
			[4]item.Stack{
				ironH.WithEnchantments(prot6), ironC.WithEnchantments(prot6),
				ironL.WithEnchantments(prot6), ironB.WithEnchantments(prot6),
			}, 10, false, 8,
		},
		// Thorns is not a DamageModifier, so it contributes nothing.
		{
			"thorns does not reduce damage",
			[4]item.Stack{
				ironH.WithEnchantments(thorns3), ironC.WithEnchantments(thorns3),
				ironL.WithEnchantments(thorns3), ironB.WithEnchantments(thorns3),
			}, 10, false, 0,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := NewArmour(nil)
			armEquip(t, a, c.pieces[0], c.pieces[1], c.pieces[2], c.pieces[3])

			if got := a.DamageReduction(c.dmg, testSource{reduced: c.reduced}); !armNear(got, c.want) {
				t.Fatalf("DamageReduction(%v): got %v, want %v", c.dmg, got, c.want)
			}
		})
	}
}

// TestArmourHighestEnchantmentLevel pins that the highest level across the
// non-empty pieces is returned, and 0 when the enchantment is absent.
func TestArmourHighestEnchantmentLevel(t *testing.T) {
	ironH, ironC, ironL, ironB := armPieces(item.ArmourTierIron{})
	thorns := func(lvl int) item.Enchantment {
		return item.NewEnchantment(enchantment.Thorns, lvl)
	}

	for _, c := range []struct {
		name   string
		pieces [4]item.Stack
		query  item.EnchantmentType
		want   int
	}{
		{"nothing enchanted", [4]item.Stack{ironH, ironC, ironL, ironB}, enchantment.Thorns, 0},
		{"empty armour", [4]item.Stack{}, enchantment.Thorns, 0},
		{
			"the maximum across pieces wins",
			[4]item.Stack{
				ironH.WithEnchantments(thorns(1)), ironC.WithEnchantments(thorns(3)),
				ironL.WithEnchantments(thorns(2)), ironB,
			}, enchantment.Thorns, 3,
		},
		{
			"a later piece does not lower the maximum",
			[4]item.Stack{
				ironH.WithEnchantments(thorns(3)), ironC.WithEnchantments(thorns(1)),
				ironL, ironB.WithEnchantments(thorns(2)),
			}, enchantment.Thorns, 3,
		},
		{
			"only the boots are enchanted",
			[4]item.Stack{{}, {}, {}, ironB.WithEnchantments(thorns(2))},
			enchantment.Thorns, 2,
		},
		{
			"an enchantment on no piece is 0",
			[4]item.Stack{
				ironH.WithEnchantments(thorns(3)), ironC, ironL, ironB,
			}, enchantment.Protection, 0,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := NewArmour(nil)
			armEquip(t, a, c.pieces[0], c.pieces[1], c.pieces[2], c.pieces[3])

			if got := a.HighestEnchantmentLevel(c.query); got != c.want {
				t.Fatalf("HighestEnchantmentLevel: got %v, want %v", got, c.want)
			}
		})
	}
}

// TestArmourKnockBackResistance pins that the resistance of every non-empty
// piece is summed rather than maximised. Only netherite has a non-zero value.
func TestArmourKnockBackResistance(t *testing.T) {
	ironH, ironC, ironL, ironB := armPieces(item.ArmourTierIron{})
	nethH, nethC, nethL, nethB := armPieces(item.ArmourTierNetherite{})

	for _, c := range []struct {
		name   string
		pieces [4]item.Stack
		want   float64
	}{
		{"empty armour", [4]item.Stack{}, 0},
		{"full iron", [4]item.Stack{ironH, ironC, ironL, ironB}, 0},
		{"netherite boots only", [4]item.Stack{{}, {}, {}, nethB}, 0.1},
		{"two netherite pieces", [4]item.Stack{nethH, {}, {}, nethB}, 0.2},
		// A sum, not a maximum: four pieces of 0.1 give 0.4.
		{"full netherite", [4]item.Stack{nethH, nethC, nethL, nethB}, 0.4},
		{"mixed iron and netherite", [4]item.Stack{nethH, ironC, ironL, nethB}, 0.2},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := NewArmour(nil)
			armEquip(t, a, c.pieces[0], c.pieces[1], c.pieces[2], c.pieces[3])

			if got := a.KnockBackResistance(); !armNear(got, c.want) {
				t.Fatalf("KnockBackResistance: got %v, want %v", got, c.want)
			}
		})
	}
}

// TestArmourThornsWithoutThorns pins that thorns is inert without the
// enchantment: no damage is returned and the DamageFunc is never reached,
// because the damage-dealing branch is guarded by dmg > 0.
func TestArmourThornsWithoutThorns(t *testing.T) {
	for _, c := range []struct {
		name   string
		pieces [4]item.Stack
	}{
		{"empty armour", [4]item.Stack{}},
		{"unenchanted full set", [4]item.Stack{helmet(), chestplate(), leggings(), boots()}},
		{
			"armour enchanted with something else",
			[4]item.Stack{helmet().WithEnchantments(item.NewEnchantment(enchantment.Protection, 4))},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{}
			a := NewArmour(r.fn)
			armEquip(t, a, c.pieces[0], c.pieces[1], c.pieces[2], c.pieces[3])
			r.reset()

			// Repeated because the activation check is random; with no thorns
			// the level is 0 and the check can never pass.
			for i := 0; i < 100; i++ {
				d := &armDamageRecorder{}
				if got := a.ThornsDamage(d.fn); got != 0 {
					t.Fatalf("ThornsDamage: got %v, want 0", got)
				}
				if len(d.calls) != 0 {
					t.Fatalf("DamageFunc calls: got %v, want 0", len(d.calls))
				}
			}
			if got := r.count(); got != 0 {
				t.Fatalf("SlotFunc calls: got %v, want 0", got)
			}
			assertSlots(t, a.Inventory(), c.pieces[0], c.pieces[1], c.pieces[2], c.pieces[3])
		})
	}
}

// TestArmourThornsAboveTenIsDeterministic pins the high-level thorns override.
// Above thorns X the returned damage is exactly the highest level minus 10,
// with no randomness left in the result.
//
// item.NewEnchantment does not enforce MaxLevel and Stack.WithEnchantments only
// checks item and enchantment compatibility, so a level above MaxLevel can be
// built without WithForcedEnchantments. That is asserted below, since the rest
// of this test depends on it.
func TestArmourThornsAboveTenIsDeterministic(t *testing.T) {
	ironH, _, _, ironB := armPieces(item.ArmourTierIron{})

	for _, c := range []struct {
		name   string
		pieces [4]item.Stack
		want   float64
	}{
		{
			"thorns XI on the helmet",
			[4]item.Stack{ironH.WithEnchantments(item.NewEnchantment(enchantment.Thorns, 11))},
			1,
		},
		{
			"thorns XIV on the boots",
			[4]item.Stack{{}, {}, {}, ironB.WithEnchantments(item.NewEnchantment(enchantment.Thorns, 14))},
			4,
		},
		{
			"the highest level wins",
			[4]item.Stack{
				ironH.WithEnchantments(item.NewEnchantment(enchantment.Thorns, 11)), {}, {},
				ironB.WithEnchantments(item.NewEnchantment(enchantment.Thorns, 13)),
			},
			3,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := NewArmour(nil)
			armEquip(t, a, c.pieces[0], c.pieces[1], c.pieces[2], c.pieces[3])

			for i := 0; i < 50; i++ {
				d := &armDamageRecorder{}
				if got := a.ThornsDamage(d.fn); got != c.want {
					t.Fatalf("ThornsDamage: got %v, want %v", got, c.want)
				}
				// Exactly one piece is damaged, always for 2 points.
				if len(d.calls) != 1 {
					t.Fatalf("DamageFunc calls: got %v, want 1", len(d.calls))
				}
				if d.calls[0].d != 2 {
					t.Fatalf("DamageFunc damage: got %v, want 2", d.calls[0].d)
				}
			}
		})
	}

	// A level above MaxLevel survives WithEnchantments, which the cases above
	// rely on.
	enchanted := ironH.WithEnchantments(item.NewEnchantment(enchantment.Thorns, 11))
	e, ok := enchanted.Enchantment(enchantment.Thorns)
	if !ok {
		t.Fatalf("Enchantment(Thorns): not present on the stack")
	}
	if e.Level() != 11 {
		t.Fatalf("Thorns level: got %v, want 11", e.Level())
	}
}

// TestArmourThornsDamagesARandomSlot pins that the piece thorns damages is
// picked from Slots() rather than Items(), so an empty slot may be picked.
//
// PINNED BUG: ThornsDamage damages a slot chosen uniformly from all four
// slots, so with a single thorns piece the damage lands on the wrong piece, or
// on nothing at all, three times out of four. Its own doc comment says a
// random thorns item is damaged.
func TestArmourThornsDamagesARandomSlot(t *testing.T) {
	ironH, _, _, _ := armPieces(item.ArmourTierIron{})
	thorns := ironH.WithEnchantments(item.NewEnchantment(enchantment.Thorns, 11))

	a := NewArmour(nil)
	armEquip(t, a, thorns, item.Stack{}, item.Stack{}, item.Stack{})

	// A slot outside 0 has probability 3/4 per iteration, so the chance of
	// never observing one over 200 iterations is (1/4)^200.
	const iterations = 200
	sawEmpty, sawHelmet := false, false
	for i := 0; i < iterations; i++ {
		d := &armDamageRecorder{}
		if got := a.ThornsDamage(d.fn); got != 1 {
			t.Fatalf("ThornsDamage: got %v, want 1", got)
		}
		if len(d.calls) != 1 {
			t.Fatalf("DamageFunc calls: got %v, want 1", len(d.calls))
		}
		if s := d.calls[0].s; s.Empty() {
			sawEmpty = true
		} else {
			sawHelmet = true
			assertStack(t, s, thorns)
		}
	}
	if !sawEmpty {
		t.Fatalf("DamageFunc never received an empty slot over %v iterations: thorns appears to iterate Items(), not Slots()", iterations)
	}
	if !sawHelmet {
		t.Fatalf("DamageFunc never received the thorns helmet over %v iterations", iterations)
	}
	// The DamageFunc returned every stack unchanged, so nothing moved.
	assertSlots(t, a.Inventory(), thorns, item.Stack{}, item.Stack{}, item.Stack{})
}

// TestArmourThornsWithinMaxLevel pins the properties of the random branch that
// hold on every run: the damage never exceeds 4, is always a whole number, and
// the DamageFunc is called exactly once if and only if damage was dealt.
func TestArmourThornsWithinMaxLevel(t *testing.T) {
	h, c, l, b := armPieces(item.ArmourTierIron{})
	e := item.NewEnchantment(enchantment.Thorns, 3)

	a := NewArmour(nil)
	armEquip(t, a,
		h.WithEnchantments(e), c.WithEnchantments(e),
		l.WithEnchantments(e), b.WithEnchantments(e),
	)

	for i := 0; i < 300; i++ {
		d := &armDamageRecorder{}
		dmg := a.ThornsDamage(d.fn)

		if dmg < 0 || dmg > 4 {
			t.Fatalf("ThornsDamage: got %v, want a value in [0, 4]", dmg)
		}
		if dmg != math.Trunc(dmg) {
			t.Fatalf("ThornsDamage: got %v, want a whole number", dmg)
		}
		want := 0
		if dmg > 0 {
			want = 1
		}
		if len(d.calls) != want {
			t.Fatalf("DamageFunc calls for damage %v: got %v, want %v", dmg, len(d.calls), want)
		}
		if want == 1 && d.calls[0].d != 2 {
			t.Fatalf("DamageFunc damage: got %v, want 2", d.calls[0].d)
		}
	}
}

// TestArmourSlotsAndItems pins the difference between the two views on the
// armour contents and pins that neither aliases the inventory.
func TestArmourSlotsAndItems(t *testing.T) {
	h, c, l, b := helmet(), chestplate(), leggings(), boots()

	t.Run("empty armour", func(t *testing.T) {
		a := NewArmour(nil)

		slots := a.Slots()
		if len(slots) != 4 {
			t.Fatalf("Slots(): got %v entries, want 4", len(slots))
		}
		for _, s := range slots {
			assertStack(t, s, item.Stack{})
		}
		if got := a.Items(); len(got) != 0 {
			t.Fatalf("Items(): got %v entries, want 0", len(got))
		}
	})

	t.Run("partially filled", func(t *testing.T) {
		a := NewArmour(nil)
		armEquip(t, a, h, item.Stack{}, item.Stack{}, b)

		slots := a.Slots()
		if len(slots) != 4 {
			t.Fatalf("Slots(): got %v entries, want 4", len(slots))
		}
		for i, want := range []item.Stack{h, {}, {}, b} {
			assertStack(t, slots[i], want)
		}

		// Items() skips the empty slots but keeps the slot order.
		items := a.Items()
		if len(items) != 2 {
			t.Fatalf("Items(): got %v entries, want 2", len(items))
		}
		assertStack(t, items[0], h)
		assertStack(t, items[1], b)
	})

	t.Run("returned slices do not alias", func(t *testing.T) {
		a := NewArmour(nil)
		armEquip(t, a, h, c, l, b)

		slots := a.Slots()
		slots[0] = diamonds(1)
		items := a.Items()
		items[0] = diamonds(1)

		assertSlots(t, a.Inventory(), h, c, l, b)
	})
}

// TestArmourClear pins the stacks Clear returns, that every slot ends up empty
// and the order in which the slot changes are reported.
func TestArmourClear(t *testing.T) {
	h, c, l, b := helmet(), chestplate(), leggings(), boots()

	for _, tc := range []struct {
		name   string
		pieces [4]item.Stack
		want   []item.Stack
		// wantSlots is the order the SlotFunc is called in.
		wantSlots []int
	}{
		{"empty armour", [4]item.Stack{}, nil, nil},
		{
			"full set", [4]item.Stack{h, c, l, b}, []item.Stack{h, c, l, b},
			// Clear defers its callbacks inside the loop, so they run LIFO:
			// descending slot order, the reverse of the returned stacks.
			[]int{3, 2, 1, 0},
		},
		{
			"helmet and boots only", [4]item.Stack{h, {}, {}, b}, []item.Stack{h, b},
			[]int{3, 0},
		},
		{
			"leggings only", [4]item.Stack{{}, {}, l, {}}, []item.Stack{l},
			[]int{2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{}
			a := NewArmour(r.fn)
			armEquip(t, a, tc.pieces[0], tc.pieces[1], tc.pieces[2], tc.pieces[3])
			r.reset()

			got := a.Clear()

			if len(got) != len(tc.want) {
				t.Fatalf("Clear(): got %v stacks, want %v", len(got), len(tc.want))
			}
			for i := range tc.want {
				assertStack(t, got[i], tc.want[i])
			}
			assertSlots(t, a.Inventory(), item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{})

			if gotSlots := r.slots(); !armSlotsEqual(gotSlots, tc.wantSlots) {
				t.Fatalf("SlotFunc slots: got %v, want %v", gotSlots, tc.wantSlots)
			}
		})
	}
}

// TestArmourInventoryIsLive pins that Inventory returns the inventory the
// Armour reads and writes, not a copy of it.
func TestArmourInventoryIsLive(t *testing.T) {
	a := NewArmour(nil)

	if a.Inventory() != a.Inventory() {
		t.Fatalf("Inventory(): consecutive calls returned different inventories")
	}

	// Writes through the inventory are visible through the armour getters.
	h, c := helmet(), chestplate()
	mustSet(t, a.Inventory(), 0, h)
	mustSet(t, a.Inventory(), 1, c)
	assertStack(t, a.Helmet(), h)
	assertStack(t, a.Chestplate(), c)

	// And the other way around.
	l := leggings()
	a.SetLeggings(l)
	assertSlot(t, a.Inventory(), 2, l)
}

// TestArmourHandle pins that Handle installs the Handler on the underlying
// inventory and that nil selects the NopHandler.
func TestArmourHandle(t *testing.T) {
	a := NewArmour(nil)

	if got := a.Inventory().Handler(); got != (NopHandler{}) {
		t.Fatalf("default Handler(): got %T, want NopHandler", got)
	}

	h := &recordingHandler{}
	a.Handle(h)
	if got := a.Inventory().Handler(); got != Handler(h) {
		t.Fatalf("Handler() after Handle(h): got %v, want %v", got, h)
	}

	a.Handle(nil)
	if got := a.Inventory().Handler(); got != (NopHandler{}) {
		t.Fatalf("Handler() after Handle(nil): got %T, want NopHandler", got)
	}
}

// TestArmourClose pins what closing an Armour does and does not do.
//
// PINNED BUG: Inventory.Close documents that it "also clears any items that
// may currently be in the inventory", but it only replaces the SlotFunc. The
// four pieces survive the close, and the inventory stays fully writable while
// silently reporting no slot changes at all.
func TestArmourClose(t *testing.T) {
	r := &recorder{}
	a := NewArmour(r.fn)
	h, c, l, b := helmet(), chestplate(), leggings(), boots()
	armEquip(t, a, h, c, l, b)
	r.reset()

	if err := a.Close(); err != nil {
		t.Fatalf("Close(): got error %v, want nil", err)
	}
	// Close does not clear despite its documentation.
	assertSlots(t, a.Inventory(), h, c, l, b)

	// The SlotFunc is gone, but writes still land.
	gold := item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1)
	a.SetHelmet(gold)
	assertStack(t, a.Helmet(), gold)
	if got := r.count(); got != 0 {
		t.Fatalf("SlotFunc calls after Close(): got %v, want 0", got)
	}

	// Closing twice is still nil.
	if err := a.Close(); err != nil {
		t.Fatalf("second Close(): got error %v, want nil", err)
	}
}

// TestArmourString pins the exact layout of the string representation.
func TestArmourString(t *testing.T) {
	t.Run("empty armour", func(t *testing.T) {
		a := NewArmour(nil)

		want := "(helmet: Stack<nil> x0, chestplate: Stack<nil> x0, leggings: Stack<nil> x0, boots: Stack<nil> x0)"
		if got := a.String(); got != want {
			t.Fatalf("String():\ngot  %v\nwant %v", got, want)
		}
	})

	t.Run("full set", func(t *testing.T) {
		a := NewArmour(nil)
		h, c, l, b := helmet(), chestplate(), leggings(), boots()
		armEquip(t, a, h, c, l, b)

		want := fmt.Sprintf("(helmet: %v, chestplate: %v, leggings: %v, boots: %v)", h, c, l, b)
		if got := a.String(); got != want {
			t.Fatalf("String():\ngot  %v\nwant %v", got, want)
		}
	})

	t.Run("a rejected piece leaves its slot empty in the output", func(t *testing.T) {
		a := NewArmour(nil)
		h := helmet()
		a.Set(h, boots(), item.Stack{}, item.Stack{})

		want := fmt.Sprintf("(helmet: %v, chestplate: %v, leggings: %v, boots: %v)", h, item.Stack{}, item.Stack{}, item.Stack{})
		if got := a.String(); got != want {
			t.Fatalf("String():\ngot  %v\nwant %v", got, want)
		}
	})
}
