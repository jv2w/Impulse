package enchantment

import (
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// registered lists every enchantment the package's init registers, with the ID
// it is registered under. The IDs are the vanilla network IDs and are written
// into every saved item, so they are pinned as literals.
var registered = []struct {
	id       int
	t        item.EnchantmentType
	name     string
	maxLevel int
	rarity   item.EnchantmentRarity
}{
	{0, Protection, "Protection", 4, item.EnchantmentRarityCommon},
	{1, FireProtection, "Fire Protection", 4, item.EnchantmentRarityUncommon},
	{2, FeatherFalling, "Feather Falling", 4, item.EnchantmentRarityUncommon},
	{3, BlastProtection, "Blast Protection", 4, item.EnchantmentRarityRare},
	{4, ProjectileProtection, "Projectile Protection", 4, item.EnchantmentRarityUncommon},
	{5, Thorns, "Thorns", 3, item.EnchantmentRarityVeryRare},
	{6, Respiration, "Respiration", 3, item.EnchantmentRarityRare},
	{7, DepthStrider, "Depth Strider", 3, item.EnchantmentRarityRare},
	{8, AquaAffinity, "Aqua Affinity", 1, item.EnchantmentRarityRare},
	{9, Sharpness, "Sharpness", 5, item.EnchantmentRarityCommon},
	{12, Knockback, "Knockback", 2, item.EnchantmentRarityUncommon},
	{13, FireAspect, "Fire Aspect", 2, item.EnchantmentRarityRare},
	{15, Efficiency, "Efficiency", 5, item.EnchantmentRarityCommon},
	{16, SilkTouch, "Silk Touch", 1, item.EnchantmentRarityVeryRare},
	{17, Unbreaking, "Unbreaking", 3, item.EnchantmentRarityUncommon},
	{18, Fortune, "Fortune", 3, item.EnchantmentRarityRare},
	{19, Power, "Power", 5, item.EnchantmentRarityCommon},
	{20, Punch, "Punch", 2, item.EnchantmentRarityRare},
	{21, Flame, "Flame", 1, item.EnchantmentRarityRare},
	{22, Infinity, "Infinity", 1, item.EnchantmentRarityVeryRare},
	{26, Mending, "Mending", 1, item.EnchantmentRarityRare},
	{28, CurseOfVanishing, "Curse of Vanishing", 1, item.EnchantmentRarityVeryRare},
	{33, Multishot, "Multishot", 1, item.EnchantmentRarityRare},
	{34, Piercing, "Piercing", 4, item.EnchantmentRarityCommon},
	{35, QuickCharge, "Quick Charge", 3, item.EnchantmentRarityUncommon},
	{36, SoulSpeed, "Soul Speed", 3, item.EnchantmentRarityVeryRare},
	{37, SwiftSneak, "Swift Sneak", 3, item.EnchantmentRarityVeryRare},
}

func TestRegistryCensus(t *testing.T) {
	if got, want := len(item.Enchantments()), len(registered); got != want {
		t.Fatalf("len(item.Enchantments()) = %v, want %v", got, want)
	}
	for _, c := range registered {
		t.Run(c.name, func(t *testing.T) {
			got, ok := item.EnchantmentByID(c.id)
			if !ok {
				t.Fatalf("EnchantmentByID(%v) reported not found", c.id)
			}
			if got != c.t {
				t.Fatalf("EnchantmentByID(%v) = %v, want %v", c.id, got.Name(), c.name)
			}
			id, ok := item.EnchantmentID(c.t)
			if !ok {
				t.Fatalf("EnchantmentID(%v) reported not found", c.name)
			}
			if id != c.id {
				t.Fatalf("EnchantmentID(%v) = %v, want %v", c.name, id, c.id)
			}
		})
	}
}

func TestRegistryOrder(t *testing.T) {
	// item.Enchantments sorts by ID, which is what decides the order they are
	// offered in and written in.
	got := item.Enchantments()
	for i, c := range registered {
		if got[i] != c.t {
			t.Fatalf("Enchantments()[%v] = %v, want %v", i, got[i].Name(), c.name)
		}
	}
}

// PINNED SCOPE: twelve vanilla enchantments have no implementation, so their
// IDs resolve to nothing. Adding them is out of scope; this
// pins which IDs are deliberately absent so an accidental partial
// implementation is visible.
func TestUnimplementedEnchantmentIDsAreAbsent(t *testing.T) {
	for id, name := range map[int]string{
		10: "Smite",
		11: "Bane of Arthropods",
		14: "Looting",
		23: "Luck of the Sea",
		24: "Lure",
		25: "Frost Walker",
		27: "Curse of Binding",
		29: "Impaling",
		30: "Riptide",
		31: "Loyalty",
		32: "Channeling",
		38: "past the last registered ID",
	} {
		if e, ok := item.EnchantmentByID(id); ok {
			t.Fatalf("EnchantmentByID(%v) returned %v; %v is now implemented", id, e.Name(), name)
		}
	}
}

func TestNamesAndMaxLevels(t *testing.T) {
	for _, c := range registered {
		t.Run(c.name, func(t *testing.T) {
			if got := c.t.Name(); got != c.name {
				t.Fatalf("Name() = %q, want %q", got, c.name)
			}
			if got := c.t.MaxLevel(); got != c.maxLevel {
				t.Fatalf("MaxLevel() = %v, want %v", got, c.maxLevel)
			}
			if got := c.t.Rarity(); got != c.rarity {
				t.Fatalf("Rarity() = %v, want %v", got.Name(), c.rarity.Name())
			}
		})
	}
}

// The cost range decides which enchantments an enchanting table may offer at a
// given level, so every value is pinned for every level the enchantment can
// reach.
func TestCosts(t *testing.T) {
	type lvlCost struct {
		lvl      int
		min, max int
	}
	for _, c := range []struct {
		t     item.EnchantmentType
		costs []lvlCost
	}{
		{AquaAffinity, []lvlCost{{1, 1, 41}}},
		{BlastProtection, []lvlCost{{1, 5, 13}, {2, 13, 21}, {3, 21, 29}, {4, 29, 37}}},
		{CurseOfVanishing, []lvlCost{{1, 25, 50}}},
		{DepthStrider, []lvlCost{{1, 10, 25}, {2, 20, 35}, {3, 30, 45}}},
		{Efficiency, []lvlCost{{1, 1, 51}, {2, 11, 61}, {3, 21, 71}, {4, 31, 81}, {5, 41, 91}}},
		{FeatherFalling, []lvlCost{{1, 5, 11}, {2, 11, 17}, {3, 17, 23}, {4, 23, 29}}},
		{FireAspect, []lvlCost{{1, 10, 60}, {2, 30, 80}}},
		{FireProtection, []lvlCost{{1, 10, 18}, {2, 18, 26}, {3, 26, 34}, {4, 34, 42}}},
		{Flame, []lvlCost{{1, 20, 50}}},
		// Fortune is the only enchantment whose maximum cost depends on the
		// level: the offset is minCost + 50 + level, not minCost + 50.
		{Fortune, []lvlCost{{1, 15, 66}, {2, 24, 76}, {3, 33, 86}}},
		{Infinity, []lvlCost{{1, 20, 50}}},
		{Knockback, []lvlCost{{1, 5, 55}, {2, 25, 75}}},
		{Mending, []lvlCost{{1, 25, 75}}},
		{Multishot, []lvlCost{{1, 20, 50}}},
		{Piercing, []lvlCost{{1, 1, 50}, {2, 11, 50}, {3, 21, 50}, {4, 31, 50}}},
		{Power, []lvlCost{{1, 1, 16}, {2, 11, 26}, {3, 21, 36}, {4, 31, 46}, {5, 41, 56}}},
		{ProjectileProtection, []lvlCost{{1, 3, 9}, {2, 9, 15}, {3, 15, 21}, {4, 21, 27}}},
		{Protection, []lvlCost{{1, 1, 12}, {2, 12, 23}, {3, 23, 34}, {4, 34, 45}}},
		{Punch, []lvlCost{{1, 12, 37}, {2, 32, 57}}},
		{QuickCharge, []lvlCost{{1, 12, 50}, {2, 32, 50}, {3, 52, 50}}},
		{Respiration, []lvlCost{{1, 10, 40}, {2, 20, 50}, {3, 30, 60}}},
		{Sharpness, []lvlCost{{1, 1, 21}, {2, 12, 32}, {3, 23, 43}, {4, 34, 54}, {5, 45, 65}}},
		{SilkTouch, []lvlCost{{1, 15, 65}}},
		{SoulSpeed, []lvlCost{{1, 10, 25}, {2, 20, 35}, {3, 30, 45}}},
		{SwiftSneak, []lvlCost{{1, 25, 75}, {2, 50, 100}, {3, 75, 125}}},
		{Thorns, []lvlCost{{1, 10, 60}, {2, 30, 80}, {3, 50, 100}}},
		{Unbreaking, []lvlCost{{1, 5, 55}, {2, 13, 63}, {3, 21, 71}}},
	} {
		t.Run(c.t.Name(), func(t *testing.T) {
			if len(c.costs) != c.t.MaxLevel() {
				t.Fatalf("the table covers %v levels but MaxLevel() is %v", len(c.costs), c.t.MaxLevel())
			}
			for _, lc := range c.costs {
				min, max := c.t.Cost(lc.lvl)
				if min != lc.min || max != lc.max {
					t.Fatalf("Cost(%v) = %v, %v, want %v, %v", lc.lvl, min, max, lc.min, lc.max)
				}
			}
		})
	}
}

// PINNED BUG: Quick Charge caps its maximum cost at a constant 50 while its
// minimum keeps climbing, so at level 3 the range is inverted (52, 50). The
// enchanting table selects on minCost <= cost <= maxCost, so Quick Charge III
// can never be rolled. Vanilla's maximum is minCost + 50.
func TestQuickChargeTopLevelIsUnreachable(t *testing.T) {
	min, max := QuickCharge.Cost(QuickCharge.MaxLevel())
	if min <= max {
		t.Fatalf("Cost(%v) = %v, %v: the range is no longer inverted", QuickCharge.MaxLevel(), min, max)
	}
	for lvl := 1; lvl < QuickCharge.MaxLevel(); lvl++ {
		if min, max := QuickCharge.Cost(lvl); min > max {
			t.Fatalf("Cost(%v) = %v, %v: only the top level should be inverted", lvl, min, max)
		}
	}
}

// Nothing guards the level argument, so out-of-range levels produce out-of-range
// costs rather than an error.
func TestCostsAreUnguarded(t *testing.T) {
	for _, c := range []struct {
		t        item.EnchantmentType
		lvl      int
		min, max int
	}{
		{Protection, 0, -10, 1},
		{Sharpness, 0, -10, 10},
		{Efficiency, -1, -19, 31},
		{Protection, 100, 1090, 1101},
	} {
		min, max := c.t.Cost(c.lvl)
		if min != c.min || max != c.max {
			t.Fatalf("%v.Cost(%v) = %v, %v, want %v, %v", c.t.Name(), c.lvl, min, max, c.min, c.max)
		}
	}
}

func TestCompatibleWithEnchantment(t *testing.T) {
	// The four armour protection types are mutually exclusive; every other
	// rejection is a single pair.
	rejects := map[item.EnchantmentType][]item.EnchantmentType{
		Protection:           {BlastProtection, FireProtection, ProjectileProtection},
		FireProtection:       {BlastProtection, ProjectileProtection, Protection},
		BlastProtection:      {FireProtection, ProjectileProtection, Protection},
		ProjectileProtection: {BlastProtection, FireProtection, Protection},
		Fortune:              {SilkTouch},
		SilkTouch:            {Fortune},
		Infinity:             {Mending},
		Mending:              {Infinity},
		Multishot:            {Piercing},
		Piercing:             {Multishot},
	}
	for _, c := range registered {
		t.Run(c.name, func(t *testing.T) {
			for _, other := range registered {
				want := true
				for _, r := range rejects[c.t] {
					if r == other.t {
						want = false
					}
				}
				if got := c.t.CompatibleWithEnchantment(other.t); got != want {
					t.Fatalf("%v.CompatibleWithEnchantment(%v) = %v, want %v", c.name, other.name, got, want)
				}
			}
			// No enchantment rejects itself, including the mutually exclusive
			// protection family.
			if !c.t.CompatibleWithEnchantment(c.t) {
				t.Fatalf("%v rejects itself", c.name)
			}
		})
	}
}

var (
	helmet     = item.Helmet{Tier: item.ArmourTierIron{}}
	chestplate = item.Chestplate{Tier: item.ArmourTierIron{}}
	leggings   = item.Leggings{Tier: item.ArmourTierIron{}}
	boots      = item.Boots{Tier: item.ArmourTierIron{}}
	sword      = item.Sword{Tier: item.ToolTierIron}
	pickaxe    = item.Pickaxe{Tier: item.ToolTierIron}
	axe        = item.Axe{Tier: item.ToolTierIron}
	shovel     = item.Shovel{Tier: item.ToolTierIron}
	hoe        = item.Hoe{Tier: item.ToolTierIron}
)

func TestCompatibleWithItem(t *testing.T) {
	// accepts lists every item from the pool below that the enchantment takes.
	// Anything not listed must be rejected.
	pool := map[string]world.Item{
		"helmet":     helmet,
		"chestplate": chestplate,
		"leggings":   leggings,
		"boots":      boots,
		"elytra":     item.Elytra{},
		"turtle":     item.TurtleShell{},
		"sword":      sword,
		"pickaxe":    pickaxe,
		"axe":        axe,
		"shovel":     shovel,
		"hoe":        hoe,
		"shears":     item.Shears{},
		"bow":        item.Bow{},
		"crossbow":   item.Crossbow{},
		"compass":    item.Compass{},
		"recovery":   item.RecoveryCompass{},
		"stick":      item.Stick{},
	}
	armour := []string{"helmet", "chestplate", "leggings", "boots", "elytra", "turtle"}
	tools := []string{"sword", "pickaxe", "axe", "shovel", "hoe", "shears"}
	durable := append(append([]string{}, armour...), append(append([]string{}, tools...), "bow", "crossbow")...)

	for _, c := range []struct {
		t       item.EnchantmentType
		accepts []string
	}{
		// The protection family keys off item.Armour, which an elytra and a
		// turtle shell both satisfy even though vanilla does not enchant them
		// this way.
		{Protection, armour},
		{FireProtection, armour},
		{BlastProtection, armour},
		{ProjectileProtection, armour},
		{Thorns, armour},
		{AquaAffinity, []string{"helmet", "turtle"}},
		{Respiration, []string{"helmet", "turtle"}},
		{FeatherFalling, []string{"boots"}},
		{DepthStrider, []string{"boots"}},
		{SoulSpeed, []string{"boots"}},
		{SwiftSneak, []string{"leggings"}},
		{Sharpness, []string{"sword", "axe"}},
		{FireAspect, []string{"sword"}},
		{Knockback, []string{"sword"}},
		// Efficiency and Silk Touch take any tool that is neither a sword nor
		// ToolNone, so both include shears. Fortune enumerates its tool types
		// and leaves shears out.
		{Efficiency, []string{"pickaxe", "axe", "shovel", "hoe", "shears"}},
		{SilkTouch, []string{"pickaxe", "axe", "shovel", "hoe", "shears"}},
		{Fortune, []string{"pickaxe", "axe", "shovel", "hoe"}},
		{Power, []string{"bow"}},
		{Punch, []string{"bow"}},
		{Flame, []string{"bow"}},
		{Infinity, []string{"bow"}},
		{Multishot, []string{"crossbow"}},
		{Piercing, []string{"crossbow"}},
		{QuickCharge, []string{"crossbow"}},
		{Unbreaking, durable},
		{Mending, durable},
		{CurseOfVanishing, append(append([]string{}, durable...), "compass", "recovery")},
	} {
		t.Run(c.t.Name(), func(t *testing.T) {
			accepted := map[string]bool{}
			for _, name := range c.accepts {
				accepted[name] = true
			}
			for name, it := range pool {
				if got := c.t.CompatibleWithItem(it); got != accepted[name] {
					t.Fatalf("CompatibleWithItem(%v) = %v, want %v", name, got, accepted[name])
				}
			}
			// A stick is not durable, not armour and not a tool, so it is the
			// control case every enchantment must reject.
			if c.t.CompatibleWithItem(item.Stick{}) {
				t.Fatal("CompatibleWithItem(stick) = true")
			}
		})
	}
}

func TestDamageModifiers(t *testing.T) {
	for _, c := range []struct {
		t    item.EnchantmentType
		want float64
	}{
		{Protection, 0.04},
		{FireProtection, 0.08},
		{BlastProtection, 0.08},
		{ProjectileProtection, 0.08},
		{FeatherFalling, 0.12},
	} {
		t.Run(c.t.Name(), func(t *testing.T) {
			m, ok := c.t.(DamageModifier)
			if !ok {
				t.Fatal("the type no longer implements DamageModifier")
			}
			if got := m.Modifier(); got != c.want {
				t.Fatalf("Modifier() = %v, want %v", got, c.want)
			}
		})
	}
	// Exactly those five implement DamageModifier: anything else joining the
	// set silently changes damage reduction.
	var n int
	for _, c := range registered {
		if _, ok := c.t.(DamageModifier); ok {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("%v enchantments implement DamageModifier, want 5", n)
	}
}

func TestLevelledEffects(t *testing.T) {
	t.Run("Efficiency addend", func(t *testing.T) {
		for lvl, want := range map[int]float64{0: 1, 1: 2, 2: 5, 3: 10, 4: 17, 5: 26, 6: 37} {
			if got := Efficiency.Addend(lvl); got != want {
				t.Fatalf("Addend(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
	t.Run("Sharpness addend", func(t *testing.T) {
		for lvl, want := range map[int]float64{0: 0, 1: 1.25, 2: 2.5, 3: 3.75, 4: 5, 5: 6.25, 6: 7.5} {
			if got := Sharpness.Addend(lvl); got != want {
				t.Fatalf("Addend(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
	t.Run("Knockback force", func(t *testing.T) {
		// The division is on float64s, so odd levels keep their half.
		for lvl, want := range map[int]float64{0: 0, 1: 0.5, 2: 1, 3: 1.5} {
			if got := Knockback.Force(lvl); got != want {
				t.Fatalf("Force(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
	t.Run("Power damage", func(t *testing.T) {
		for lvl, want := range map[int]float64{0: 0.5, 1: 1, 2: 1.5, 3: 2, 4: 2.5, 5: 3} {
			if got := Power.PowerDamage(lvl); got != want {
				t.Fatalf("PowerDamage(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
	t.Run("Respiration chance", func(t *testing.T) {
		for lvl, want := range map[int]float64{0: 0, 1: 0.5, 2: 2.0 / 3.0, 3: 0.75} {
			if got := Respiration.Chance(lvl); got != want {
				t.Fatalf("Chance(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
	t.Run("Fire Aspect duration", func(t *testing.T) {
		for lvl, want := range map[int]time.Duration{0: 0, 1: 4 * time.Second, 2: 8 * time.Second} {
			if got := FireAspect.Duration(lvl); got != want {
				t.Fatalf("Duration(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
	t.Run("Quick Charge duration", func(t *testing.T) {
		for lvl, want := range map[int]time.Duration{
			1: time.Second,
			2: 750 * time.Millisecond,
			3: 500 * time.Millisecond,
			// The formula is unguarded, so level 5 charges instantly and any
			// higher level is negative.
			5: 0,
			6: -250 * time.Millisecond,
		} {
			if got := QuickCharge.ChargeDuration(lvl); got != want {
				t.Fatalf("ChargeDuration(%v) = %v, want %v", lvl, got, want)
			}
		}
	})
}

func TestLevelIndependentEffects(t *testing.T) {
	if got, want := Flame.BurnDuration(), 5*time.Second; got != want {
		t.Fatalf("Flame.BurnDuration() = %v, want %v", got, want)
	}
	if got, want := Punch.KnockBackMultiplier(), 0.25; got != want {
		t.Fatalf("Punch.KnockBackMultiplier() = %v, want %v", got, want)
	}
	if Infinity.ConsumesArrows() {
		t.Fatal("Infinity.ConsumesArrows() = true, want false")
	}
	if !Multishot.MultipleProjectiles() {
		t.Fatal("Multishot.MultipleProjectiles() = false, want true")
	}
	if !Piercing.Pierces() {
		t.Fatal("Piercing.Pierces() = false, want true")
	}
	if !CurseOfVanishing.Curse() {
		t.Fatal("CurseOfVanishing.Curse() = false, want true")
	}
}

func TestTreasureSet(t *testing.T) {
	treasure := map[item.EnchantmentType]bool{
		Mending: true, SoulSpeed: true, SwiftSneak: true, CurseOfVanishing: true,
	}
	for _, c := range registered {
		got := false
		if v, ok := c.t.(interface{ Treasure() bool }); ok {
			got = v.Treasure()
		}
		if got != treasure[c.t] {
			t.Fatalf("%v treasure = %v, want %v", c.name, got, treasure[c.t])
		}
	}
}

// damageSource is a world.DamageSource whose two switches are what
// ProtectionFactor branches on.
type damageSource struct {
	resistance bool
	affectedBy item.EnchantmentType
}

func (d damageSource) ReducedByArmour() bool     { return true }
func (d damageSource) ReducedByResistance() bool { return d.resistance }
func (d damageSource) Fire() bool                { return false }
func (d damageSource) IgnoreTotem() bool         { return false }

// affectedSource additionally opts in to a single enchantment.
type affectedSource struct{ damageSource }

func (a affectedSource) AffectedByEnchantment(e item.EnchantmentType) bool {
	return e == a.affectedBy
}

func TestProtectionFactor(t *testing.T) {
	e := func(t item.EnchantmentType, lvl int) item.Enchantment {
		return item.NewEnchantment(t, lvl)
	}
	for _, c := range []struct {
		name string
		src  world.DamageSource
		ench []item.Enchantment
		want float64
	}{
		{"no enchantments", damageSource{resistance: true}, nil, 0},
		{"empty slice", damageSource{resistance: true}, []item.Enchantment{}, 0},
		{
			"protection applies to anything reduced by resistance",
			damageSource{resistance: true},
			[]item.Enchantment{e(Protection, 1)},
			0.04,
		},
		{
			"protection does not apply when resistance does not",
			damageSource{resistance: false},
			[]item.Enchantment{e(Protection, 4)},
			0,
		},
		{
			"protection scales with the level",
			damageSource{resistance: true},
			[]item.Enchantment{e(Protection, 4)},
			0.16,
		},
		{
			// A full set of four pieces is four separate entries in the slice.
			"duplicate entries accumulate",
			damageSource{resistance: true},
			[]item.Enchantment{e(Protection, 4), e(Protection, 4), e(Protection, 4)},
			0.48,
		},
		{
			"the factor is capped at 0.8",
			damageSource{resistance: true},
			[]item.Enchantment{e(Protection, 4), e(Protection, 4), e(Protection, 4), e(Protection, 4), e(Protection, 4), e(Protection, 4)},
			0.8,
		},
		{
			"a specific protection needs the source to opt in",
			damageSource{resistance: true},
			[]item.Enchantment{e(BlastProtection, 4)},
			0,
		},
		{
			"an opted-in source applies the specific protection",
			affectedSource{damageSource{resistance: true, affectedBy: BlastProtection}},
			[]item.Enchantment{e(BlastProtection, 4)},
			0.32,
		},
		{
			"an opted-in source does not apply the others",
			affectedSource{damageSource{resistance: true, affectedBy: BlastProtection}},
			[]item.Enchantment{e(FireProtection, 4)},
			0,
		},
		{
			// The protection branch is checked first and short-circuits, so an
			// opted-in source never gets asked about protection itself.
			"protection ignores the opt-in when resistance is off",
			affectedSource{damageSource{resistance: false, affectedBy: Protection}},
			[]item.Enchantment{e(Protection, 4)},
			0.16,
		},
		{
			"enchantments without a modifier are skipped",
			damageSource{resistance: true},
			[]item.Enchantment{e(Sharpness, 5), e(Unbreaking, 3), e(Protection, 1)},
			0.04,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ProtectionFactor(c.src, c.ench); got != c.want {
				t.Fatalf("ProtectionFactor = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUnbreakingReduce(t *testing.T) {
	t.Run("nothing to reduce", func(t *testing.T) {
		if got := Unbreaking.Reduce(sword, 3, 0); got != 0 {
			t.Fatalf("Reduce(_, 3, 0) = %v, want 0", got)
		}
	})
	t.Run("level zero never reduces", func(t *testing.T) {
		// rand.IntN(1) is always 0, so the guard never passes.
		for range 100 {
			if got := Unbreaking.Reduce(sword, 0, 5); got != 5 {
				t.Fatalf("Reduce(_, 0, 5) = %v, want 5", got)
			}
		}
	})
	t.Run("the result stays within bounds", func(t *testing.T) {
		for range 200 {
			for _, it := range []world.Item{sword, helmet} {
				got := Unbreaking.Reduce(it, 3, 10)
				if got < 0 || got > 10 {
					t.Fatalf("Reduce(_, 3, 10) = %v, want a value in [0, 10]", got)
				}
			}
		}
	})
	// PINNED BUG: Reduce is exported and takes a raw level, and rand.IntN
	// panics once level+1 drops to zero or below. item.NewEnchantment keeps a
	// level below 1 out of a Stack, so the reachable paths are safe, but a
	// direct caller is not protected.
	t.Run("a level below minus one panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("Reduce did not panic at level -1")
			}
		}()
		Unbreaking.Reduce(sword, -1, 1)
	})
}

// PINNED BUG: item.NewEnchantment documents a panic when the level exceeds
// EnchantmentType.MaxLevel, but only rejects levels below 1. Thorns XIV goes
// straight in and reaches the level-above-ten branch in inventory's armour
// code.
func TestNewEnchantmentAcceptsLevelsAboveMaxLevel(t *testing.T) {
	e := item.NewEnchantment(Thorns, 14)
	if got, want := e.Level(), 14; got != want {
		t.Fatalf("Level() = %v, want %v", got, want)
	}
	if got, want := Thorns.MaxLevel(), 3; got != want {
		t.Fatalf("MaxLevel() = %v, want %v", got, want)
	}
	// The stack keeps it too: nothing between the constructor and the item
	// clamps the level.
	s := item.NewStack(chestplate, 1).WithEnchantments(e)
	got, ok := s.Enchantment(Thorns)
	if !ok {
		t.Fatal("the enchantment was not applied")
	}
	if got.Level() != 14 {
		t.Fatalf("Level() on the stack = %v, want 14", got.Level())
	}
}

func TestThornsDamageSource(t *testing.T) {
	src := ThornsDamageSource{}
	if !src.ReducedByResistance() {
		t.Fatal("ReducedByResistance() = false, want true")
	}
	if src.ReducedByArmour() {
		t.Fatal("ReducedByArmour() = true, want false")
	}
	if src.Fire() {
		t.Fatal("Fire() = true, want false")
	}
	if src.IgnoreTotem() {
		t.Fatal("IgnoreTotem() = true, want false")
	}
	// It carries no AffectedByEnchantment, so only protection reduces it.
	if _, ok := world.DamageSource(src).(AffectedDamageSource); ok {
		t.Fatal("ThornsDamageSource now implements AffectedDamageSource")
	}
	if got, want := ProtectionFactor(src, []item.Enchantment{item.NewEnchantment(Protection, 4)}), 0.16; got != want {
		t.Fatalf("ProtectionFactor = %v, want %v", got, want)
	}
}
