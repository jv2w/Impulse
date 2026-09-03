package player

import (
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"golang.org/x/text/language"
)

// applied builds the playerData a Config produces, which is what every field of
// Config is ultimately for.
func applied(cfg Config) *playerData {
	data := &world.EntityData{}
	cfg.Apply(data)
	return data.Data.(*playerData)
}

// fillDefaults substitutes for the fields a Provider is not required to store.
// The sizes are the vanilla ones and the locale falls back to British English.
func TestFillDefaultsFillsTheEmptyFields(t *testing.T) {
	got := fillDefaults(Config{})

	if got.Locale != language.BritishEnglish {
		t.Fatalf("Locale = %v, want %v", got.Locale, language.BritishEnglish)
	}
	for _, tc := range []struct {
		name string
		inv  *inventory.Inventory
		size int
	}{
		{name: "Inventory", inv: got.Inventory, size: 36},
		{name: "EnderChestInventory", inv: got.EnderChestInventory, size: 27},
		{name: "OffHand", inv: got.OffHand, size: 1},
	} {
		if tc.inv == nil {
			t.Fatalf("%v = nil", tc.name)
		}
		if n := len(tc.inv.Slots()); n != tc.size {
			t.Fatalf("%v size = %v, want %v", tc.name, n, tc.size)
		}
	}
	if got.Armour == nil {
		t.Fatal("Armour = nil")
	}
	if got.Food != 20 || got.Saturation != 5 {
		t.Fatalf("Food, Saturation = %v, %v, want 20, 5", got.Food, got.Saturation)
	}
	if got.AirSupply != 300 || got.MaxAirSupply != 300 {
		t.Fatalf("AirSupply, MaxAirSupply = %v, %v, want 300, 300", got.AirSupply, got.MaxAirSupply)
	}
	if got.Health != 20 || got.MaxHealth != 20 {
		t.Fatalf("Health, MaxHealth = %v, %v, want 20, 20", got.Health, got.MaxHealth)
	}
	if got.GameMode != world.GameModeSurvival {
		t.Fatalf("GameMode = %#v, want survival", got.GameMode)
	}
	if got.EnchantmentSeed == 0 {
		t.Fatal("EnchantmentSeed = 0, want a drawn seed")
	}
}

// A Config that already carries a value keeps it. This is the half of
// fillDefaults a Provider relies on.
func TestFillDefaultsLeavesSetFieldsAlone(t *testing.T) {
	inv, ender, off := inventory.New(9, nil), inventory.New(9, nil), inventory.New(2, nil)
	armour := inventory.NewArmour(nil)
	in := Config{
		Locale:              language.Korean,
		Inventory:           inv,
		EnderChestInventory: ender,
		OffHand:             off,
		Armour:              armour,
		Food:                3,
		FoodTick:            17,
		Exhaustion:          1.5,
		Saturation:          0.5,
		EnchantmentSeed:     42,
		AirSupply:           50,
		MaxAirSupply:        400,
		Health:              1,
		MaxHealth:           30,
		GameMode:            world.GameModeAdventure,
	}

	got := fillDefaults(in)

	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{name: "Locale", got: got.Locale, want: in.Locale},
		{name: "Inventory", got: got.Inventory, want: in.Inventory},
		{name: "EnderChestInventory", got: got.EnderChestInventory, want: in.EnderChestInventory},
		{name: "OffHand", got: got.OffHand, want: in.OffHand},
		{name: "Armour", got: got.Armour, want: in.Armour},
		{name: "Food", got: got.Food, want: in.Food},
		{name: "FoodTick", got: got.FoodTick, want: in.FoodTick},
		{name: "Exhaustion", got: got.Exhaustion, want: in.Exhaustion},
		{name: "Saturation", got: got.Saturation, want: in.Saturation},
		{name: "EnchantmentSeed", got: got.EnchantmentSeed, want: in.EnchantmentSeed},
		{name: "AirSupply", got: got.AirSupply, want: in.AirSupply},
		{name: "MaxAirSupply", got: got.MaxAirSupply, want: in.MaxAirSupply},
		{name: "Health", got: got.Health, want: in.Health},
		{name: "MaxHealth", got: got.MaxHealth, want: in.MaxHealth},
		{name: "GameMode", got: got.GameMode, want: in.GameMode},
	} {
		if tc.got != tc.want {
			t.Fatalf("fillDefaults overwrote %v: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// PINNED BUG: the hunger guard at conf.go:136 cannot tell an unset Config from
// a starving player. All four hunger fields at zero is the exact shape of a
// player who ran the bar down and whose Provider does not persist the food
// tick, and that player is silently healed to a full bar with five saturation.
// The shipped playerdb escapes it only because a live foodTick is never 0.
func TestAStarvingPlayerWithNoFoodTickIsHealedToFull(t *testing.T) {
	got := fillDefaults(Config{Food: 0, FoodTick: 0, Exhaustion: 0, Saturation: 0})

	if got.Food != 20 || got.Saturation != 5 {
		t.Fatalf("Food, Saturation = %v, %v, want 20, 5", got.Food, got.Saturation)
	}
}

// PINNED BUG: the same guard read the other way. Setting any one companion
// field skips the whole block, so a caller who asks for a default bar plus some
// exhaustion gets a player at food 0 who starves immediately. There is no way
// to express "default food, non-zero exhaustion".
func TestSettingOneHungerFieldLeavesTheBarEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{name: "only FoodTick", cfg: Config{FoodTick: 1}},
		{name: "only Exhaustion", cfg: Config{Exhaustion: 0.5}},
		{name: "only Saturation", cfg: Config{Saturation: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := fillDefaults(tc.cfg)
			if got.Food != 0 {
				t.Fatalf("Food = %v, want 0", got.Food)
			}
		})
	}
}

// PINNED BUG: the guards at conf.go:142 and conf.go:145 key on the maximum and
// overwrite the current value with it. A Provider that stores a drowning or a
// near-dead player without also storing the maximum hands back a player at full
// air and full health.
func TestAirAndHealthAreClobberedWhenOnlyTheMaximumIsMissing(t *testing.T) {
	got := fillDefaults(Config{AirSupply: 50, Health: 1})

	if got.AirSupply != 300 {
		t.Fatalf("AirSupply = %v, want 300 (the stored 50 was discarded)", got.AirSupply)
	}
	if got.Health != 20 {
		t.Fatalf("Health = %v, want 20 (the stored 1 was discarded)", got.Health)
	}
}

// PINNED BUG: MaxHealth is only defaulted when it is exactly 0, so a negative
// maximum survives into entity.NewHealthManager, which clamps the health down
// to it. The result is a player who is dead on arrival and can never be healed.
// HealthManager.SetMaxHealth guards the same value (entity/health.go:37); the
// constructor path does not.
func TestANegativeMaxHealthProducesAPermanentlyDeadPlayer(t *testing.T) {
	pd := applied(Config{MaxHealth: -5})

	if got := pd.health.Health(); got != -5 {
		t.Fatalf("health = %v, want -5", got)
	}
	if got := pd.health.MaxHealth(); got != -5 {
		t.Fatalf("max health = %v, want -5", got)
	}
}

// PINNED BUG: Apply writes Config.FoodTick over the 1 newHungerManager set, and
// fillDefaults never defaults FoodTick, so a Config that took the defaults path
// yields foodTick 0. On the very first tickFood that makes both the % 10 and
// the % 20 gate true at once, producing a quick regeneration on tick zero, and
// makes the foodTick == 1 gate false, skipping that tick's starvation check.
// The counter never returns to 0 afterwards.
func TestApplyLeavesTheFoodTickAtZero(t *testing.T) {
	if got := newHungerManager().foodTick; got != 1 {
		t.Fatalf("newHungerManager foodTick = %v, want 1", got)
	}
	if got := applied(Config{}).hunger.foodTick; got != 0 {
		t.Fatalf("applied foodTick = %v, want 0", got)
	}
}

// Apply fills the fields Config has no slot for with fixed values, so they are
// part of the constructor's contract rather than the caller's.
func TestApplyFixedDefaults(t *testing.T) {
	pd := applied(Config{})

	if pd.scale != 1 {
		t.Fatalf("scale = %v, want 1", pd.scale)
	}
	if pd.speed != 0.1 || pd.flightSpeed != 0.05 || pd.verticalFlightSpeed != 1 {
		t.Fatalf("speeds = %v, %v, %v, want 0.1, 0.05, 1", pd.speed, pd.flightSpeed, pd.verticalFlightSpeed)
	}
	if _, ok := pd.h.(NopHandler); !ok {
		t.Fatalf("handler = %T, want NopHandler", pd.h)
	}
	if !pd.breathing {
		t.Fatal("breathing = false, want true")
	}
	if !pd.alwaysShowNameTag {
		t.Fatal("alwaysShowNameTag = false, want true")
	}
	if n := len(pd.ui.Slots()); n != 54 {
		t.Fatalf("ui size = %v, want 54", n)
	}
	if pd.cooldowns == nil {
		t.Fatal("cooldowns = nil, want an allocated map")
	}
}

// Apply writes three fields of the EntityData and leaves the rest alone.
func TestApplyWritesOnlyNamePositionAndRotation(t *testing.T) {
	data := &world.EntityData{
		Vel:               mgl64.Vec3{9, 9, 9},
		AlwaysShowNameTag: false,
		Age:               42,
	}
	cfg := Config{Name: "steve", Position: mgl64.Vec3{1, 2, 3}, Rotation: cube.Rotation{45, 10}}

	cfg.Apply(data)

	if data.Name != "steve" || data.Pos != (mgl64.Vec3{1, 2, 3}) || data.Rot != (cube.Rotation{45, 10}) {
		t.Fatalf("data = %+v, want the three fields written", data)
	}
	if data.Vel != (mgl64.Vec3{9, 9, 9}) || data.AlwaysShowNameTag || data.Age != 42 {
		t.Fatalf("data = %+v, want Vel, AlwaysShowNameTag and Age untouched", data)
	}
}

// PINNED BUG: Config.Velocity is written by Data() and persisted by playerdb,
// but Apply never reads it. A player's velocity is discarded on every load.
func TestApplyIgnoresTheConfiguredVelocity(t *testing.T) {
	data := &world.EntityData{}
	Config{Velocity: mgl64.Vec3{1, 2, 3}}.Apply(data)

	if data.Vel != (mgl64.Vec3{}) {
		t.Fatalf("data.Vel = %v, want the zero vector", data.Vel)
	}
}

// PINNED BUG: Apply stores the caller's inventory pointers without cloning
// them, so applying one Config twice produces two players sharing one bag.
// inventory.Clone exists and is not used.
func TestApplyingOneConfigTwiceSharesTheInventories(t *testing.T) {
	cfg := Config{Inventory: inventory.New(9, nil)}

	a, b := applied(cfg), applied(cfg)
	if err := a.inv.SetItem(0, item.NewStack(item.Apple{}, 1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}

	if b.inv.Slots()[0].Empty() {
		t.Fatal("the second player's inventory is independent; the aliasing was fixed")
	}
}

// Two Configs with no inventories of their own do get independent ones, because
// fillDefaults allocates per call.
func TestApplyingAnEmptyConfigTwiceGivesIndependentInventories(t *testing.T) {
	a, b := applied(Config{}), applied(Config{})
	if err := a.inv.SetItem(0, item.NewStack(item.Apple{}, 1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}

	if !b.inv.Slots()[0].Empty() {
		t.Fatal("two default Configs shared an inventory")
	}
}

// Data is what a Provider stores. These fields survive the trip back through
// Apply unchanged, and they are the whole of what a player carries between
// sessions.
func TestDataRoundTripsThePersistedFields(t *testing.T) {
	in := Config{
		XUID:            "1234",
		Name:            "steve",
		Locale:          language.Korean,
		GameMode:        world.GameModeAdventure,
		Position:        mgl64.Vec3{1, 2, 3},
		Rotation:        cube.Rotation{45, 10},
		Health:          7,
		MaxHealth:       30,
		FoodTick:        17,
		Food:            11,
		Exhaustion:      1.5,
		Saturation:      2.5,
		AirSupply:       50,
		MaxAirSupply:    400,
		EnchantmentSeed: 42,
		Experience:      16,
		HeldSlot:        4,
		FireTicks:       9,
		FallDistance:    3.5,
	}
	p := newStubPlayer(in)

	out := p.Data()

	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{name: "XUID", got: out.XUID, want: in.XUID},
		{name: "Name", got: out.Name, want: in.Name},
		{name: "Locale", got: out.Locale, want: in.Locale},
		{name: "GameMode", got: out.GameMode, want: in.GameMode},
		{name: "Position", got: out.Position, want: in.Position},
		{name: "Rotation", got: out.Rotation, want: in.Rotation},
		{name: "Health", got: out.Health, want: in.Health},
		{name: "MaxHealth", got: out.MaxHealth, want: in.MaxHealth},
		{name: "FoodTick", got: out.FoodTick, want: in.FoodTick},
		{name: "Food", got: out.Food, want: in.Food},
		{name: "Exhaustion", got: out.Exhaustion, want: in.Exhaustion},
		{name: "Saturation", got: out.Saturation, want: in.Saturation},
		{name: "AirSupply", got: out.AirSupply, want: in.AirSupply},
		{name: "MaxAirSupply", got: out.MaxAirSupply, want: in.MaxAirSupply},
		{name: "EnchantmentSeed", got: out.EnchantmentSeed, want: in.EnchantmentSeed},
		{name: "Experience", got: out.Experience, want: in.Experience},
		{name: "HeldSlot", got: out.HeldSlot, want: in.HeldSlot},
		{name: "FireTicks", got: out.FireTicks, want: in.FireTicks},
		{name: "FallDistance", got: out.FallDistance, want: in.FallDistance},
		{name: "UUID", got: out.UUID, want: stubUUID},
	} {
		if tc.got != tc.want {
			t.Fatalf("%v = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// PINNED BUG: Data reports the name tag in the Name field, and Apply writes
// that field to both the entity name and the name tag. A player whose name tag
// was changed has their account name overwritten by it on the next load, and
// the change is permanent. Player.Name carries a TODO acknowledging it.
func TestDataReportsTheNameTagAsTheName(t *testing.T) {
	p := newStubPlayer(Config{Name: "steve"})
	p.nameTag = "not steve"

	if got := p.Data().Name; got != "not steve" {
		t.Fatalf("Data().Name = %v, want the name tag", got)
	}
	if got := p.data.Name; got != "steve" {
		t.Fatalf("EntityData.Name = %v, want steve: the two are still separate before the save", got)
	}
}

// PINNED SCOPE: Data carries no slot for these, so each one is reset to the
// Apply default on every reload. They are session state by design rather than
// defects, but the list is what a Provider silently cannot preserve.
func TestDataDropsTheSessionOnlyState(t *testing.T) {
	p := newStubPlayer(Config{})
	p.scoreTag, p.absorptionHealth, p.scale = "tag", 4, 2
	p.speed, p.flightSpeed = 0.5, 0.5
	p.sneaking, p.sprinting, p.flying, p.invisible, p.immobile = true, true, true, true, true
	p.breathing = false
	p.SetCooldown(item.Apple{}, time.Hour)
	if err := p.ui.SetItem(0, item.NewStack(item.Apple{}, 1)); err != nil {
		t.Fatalf("ui.SetItem: %v", err)
	}

	pd := applied(p.Data())

	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{name: "scoreTag", got: pd.scoreTag, want: ""},
		{name: "absorptionHealth", got: pd.absorptionHealth, want: float64(0)},
		{name: "scale", got: pd.scale, want: float64(1)},
		{name: "speed", got: pd.speed, want: 0.1},
		{name: "flightSpeed", got: pd.flightSpeed, want: 0.05},
		{name: "sneaking", got: pd.sneaking, want: false},
		{name: "sprinting", got: pd.sprinting, want: false},
		{name: "flying", got: pd.flying, want: false},
		{name: "invisible", got: pd.invisible, want: false},
		{name: "immobile", got: pd.immobile, want: false},
		{name: "breathing", got: pd.breathing, want: true},
		{name: "cooldowns", got: len(pd.cooldowns), want: 0},
		{name: "ui slot 0 empty", got: pd.ui.Slots()[0].Empty(), want: true},
	} {
		if tc.got != tc.want {
			t.Fatalf("%v = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// The four inventories are handed over by pointer, so a Provider that stores a
// Config and keeps it holds live references to the player's bags.
func TestDataHandsOutTheLiveInventories(t *testing.T) {
	p := newStubPlayer(Config{})

	out := p.Data()

	if out.Inventory != p.inv || out.OffHand != p.offHand ||
		out.Armour != p.armour || out.EnderChestInventory != p.enderChest {
		t.Fatal("Data returned copies of the inventories rather than the live ones")
	}
}
