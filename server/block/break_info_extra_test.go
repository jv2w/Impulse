package block_test

import (
	"math"
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/enchantment"
	"github.com/jv2w/Impulse/server/world"
)

// breakTicks converts a BreakDuration into whole ticks, mirroring the conversion BreakDuration itself
// performs (one tick is time.Second/20, i.e. 50ms).
func breakTicks(b world.Block, s item.Stack, ctx block.BreakContext) int64 {
	return block.BreakDuration(b, s, ctx).Milliseconds() / 50
}

var (
	woodPick      = item.NewStack(item.Pickaxe{Tier: item.ToolTierWood}, 1)
	goldPick      = item.NewStack(item.Pickaxe{Tier: item.ToolTierGold}, 1)
	stonePick     = item.NewStack(item.Pickaxe{Tier: item.ToolTierStone}, 1)
	ironPick      = item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)
	diaPick       = item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1)
	diaSword      = item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
	shears        = item.NewStack(item.Shears{}, 1)
	stoneDiamOre  = block.DiamondOre{Type: block.StoneOre()}
	bareHandStack = item.Stack{}
)

// TestBreakDurationEdgeCases pins the three early exits of BreakDuration that short-circuit the whole
// mining calculation, plus the fallback applied to a held item that is not a tool.
//
// Bedrock implements no BreakInfo method, so it fails the Breakable assertion and yields math.MaxInt64
// nanoseconds (~292 years), i.e. 5534023222112 ticks after the /50ms conversion. ShortGrass has hardness 0
// and returns exactly 0 before any tool is inspected, so even a non-tool item mines it instantly.
//
// A held item that does not implement item.Tool is replaced by item.ToolNone{} (HarvestLevel 0,
// BaseMiningEfficiency 1, ToolType TypeNone), which is indistinguishable from an empty hand. Stone is
// pickaxeEffective/pickaxeHarvestable, so neither applies: speed stays 1 and the damage divisor is 100,
// not 30. damage = 1 / 1.5 / 100 = 1/150, so ceil(1/damage) = 150 ticks.
//
// PINNED SCOPE: a non-Breakable block is reported as "takes essentially forever" rather than "cannot be
// broken". Callers cannot distinguish that from a legitimately enormous duration except by comparing
// against math.MaxInt64 themselves.
func TestBreakDurationEdgeCases(t *testing.T) {
	maxTicks := int64(math.MaxInt64) / int64(1000*1000) / 50

	tests := []struct {
		name      string
		block     world.Block
		stack     item.Stack
		wantTicks int64
	}{
		{name: "non-breakable block", block: block.Bedrock{}, stack: diaPick, wantTicks: maxTicks},
		{name: "non-breakable block with bare hand", block: block.Bedrock{}, stack: bareHandStack, wantTicks: maxTicks},
		{name: "zero hardness block", block: block.ShortGrass{}, stack: bareHandStack, wantTicks: 0},
		{name: "zero hardness block with non-tool item", block: block.ShortGrass{}, stack: item.NewStack(item.Diamond{}, 1), wantTicks: 0},
		{name: "bare hand on stone", block: block.Stone{}, stack: bareHandStack, wantTicks: 150},
		{name: "non-tool item on stone equals bare hand", block: block.Stone{}, stack: item.NewStack(item.Diamond{}, 1), wantTicks: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breakTicks(tt.block, tt.stack, block.BreakContext{}); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
}

// TestToolTierMiningConstants pins the HarvestLevel and BaseMiningEfficiency of every tool tier. These
// fourteen numbers are the entire constant basis of BreakDuration: HarvestLevel decides whether the /30
// or the /100 damage divisor applies, and BaseMiningEfficiency is the raw speed before enchantments and
// effects. Any change here silently shifts every break time in the game.
//
// PINNED SCOPE: gold has HarvestLevel 1 (tied with wood, the lowest) yet BaseMiningEfficiency 12 (higher
// than netherite's 9). Netherite and diamond share HarvestLevel 4, so netherite unlocks no new block.
func TestToolTierMiningConstants(t *testing.T) {
	tests := []struct {
		name          string
		tier          item.ToolTier
		harvestLevel  int
		baseEfficient float64
	}{
		{name: "wood", tier: item.ToolTierWood, harvestLevel: 1, baseEfficient: 2},
		{name: "gold", tier: item.ToolTierGold, harvestLevel: 1, baseEfficient: 12},
		{name: "stone", tier: item.ToolTierStone, harvestLevel: 2, baseEfficient: 4},
		{name: "copper", tier: item.ToolTierCopper, harvestLevel: 2, baseEfficient: 5},
		{name: "iron", tier: item.ToolTierIron, harvestLevel: 3, baseEfficient: 6},
		{name: "diamond", tier: item.ToolTierDiamond, harvestLevel: 4, baseEfficient: 8},
		{name: "netherite", tier: item.ToolTierNetherite, harvestLevel: 4, baseEfficient: 9},
	}
	if got := len(item.ToolTiers()); got != len(tests) {
		t.Fatalf("ToolTiers returned %d tiers, want %d", got, len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tier.HarvestLevel; got != tt.harvestLevel {
				t.Errorf("HarvestLevel = %d, want %d", got, tt.harvestLevel)
			}
			if got := tt.tier.BaseMiningEfficiency; got != tt.baseEfficient {
				t.Errorf("BaseMiningEfficiency = %v, want %v", got, tt.baseEfficient)
			}
		})
	}
	// item.ToolNone stands in for any non-tool item; its two constants close the table.
	if got := (item.ToolNone{}).HarvestLevel(); got != 0 {
		t.Errorf("ToolNone HarvestLevel = %d, want 0", got)
	}
	if got := (item.ToolNone{}).BaseMiningEfficiency(block.Stone{}); got != 1 {
		t.Errorf("ToolNone BaseMiningEfficiency = %v, want 1", got)
	}
	if got := (item.ToolNone{}).ToolType(); got != item.TypeNone {
		t.Errorf("ToolNone ToolType = %v, want TypeNone", got)
	}
}

// TestHasteAndConduitPowerDoNotStack pins that the two mining-speed effects are folded with max, not with
// addition, and that the surviving level is applied twice along the pipeline: once as speed *= 0.2*n+1 and
// again as damage *= 1.2^n.
//
// Diamond pick on Stone (hardness 1.5, harvestable). n = 1: speed = 8*1.2 = 9.6,
// damage = 9.6/1.5/30 * 1.2 = 0.256, ceil(1/0.256) = ceil(3.906) = 4 ticks.
// n = 2: speed = 8*1.4 = 11.2, damage = 11.2/45 * 1.44 = 0.35840, ceil(2.790) = 3 ticks.
// Haste 1 alone, Conduit Power 1 alone, and both at 1 all reduce to n = 1 and therefore to the same 4
// ticks; only Haste 2 (n = 2) differs. Haste 1 + Conduit Power 2 collapses to n = 2, proving max and not
// sum (a sum would give n = 3: speed = 12.8, damage = 12.8/45*1.728 = 0.49152, ceil(2.034) = 3 -- the same
// tick count here, so the discriminating case is Haste 1 + Conduit Power 1, where a sum would give n = 2
// and 3 ticks instead of the pinned 4).
func TestHasteAndConduitPowerDoNotStack(t *testing.T) {
	tests := []struct {
		name      string
		ctx       block.BreakContext
		wantTicks int64
	}{
		{name: "no effect", ctx: block.BreakContext{}, wantTicks: 6},
		{name: "haste 1", ctx: block.BreakContext{HasteLevel: 1}, wantTicks: 4},
		{name: "conduit power 1 alone", ctx: block.BreakContext{ConduitPowerLevel: 1}, wantTicks: 4},
		{name: "haste 1 and conduit power 1 do not sum", ctx: block.BreakContext{HasteLevel: 1, ConduitPowerLevel: 1}, wantTicks: 4},
		{name: "haste 2", ctx: block.BreakContext{HasteLevel: 2}, wantTicks: 3},
		{name: "conduit power 2 alone", ctx: block.BreakContext{ConduitPowerLevel: 2}, wantTicks: 3},
		{name: "max of the two wins", ctx: block.BreakContext{HasteLevel: 1, ConduitPowerLevel: 2}, wantTicks: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breakTicks(block.Stone{}, diaPick, tt.ctx); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
}

// TestMiningFatigueCompoundsTwice pins that Mining Fatigue, like Haste, is applied at two separate points:
// speed *= 0.3^F and later damage *= 0.7^F, so its true per-level factor is 0.21.
//
// Diamond pick on Stone. F = 1: speed = 8*0.3 = 2.4, damage = 2.4/45*0.7 = 0.037333,
// ceil(26.79) = 27 ticks. F = 2: speed = 8*0.09 = 0.72, damage = 0.72/45*0.49 = 0.00784,
// ceil(127.55) = 128 ticks. F = 3: speed = 8*0.027 = 0.216, damage = 0.216/45*0.343 = 0.0016464,
// ceil(607.4) = 608 ticks.
func TestMiningFatigueCompoundsTwice(t *testing.T) {
	tests := []struct {
		name      string
		level     int
		wantTicks int64
	}{
		{name: "fatigue 1", level: 1, wantTicks: 27},
		{name: "fatigue 2", level: 2, wantTicks: 128},
		{name: "fatigue 3", level: 3, wantTicks: 608},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := block.BreakContext{MiningFatigueLevel: tt.level}
			if got := breakTicks(block.Stone{}, diaPick, ctx); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
}

// TestBreakDurationEnvironmentPenalties pins the two independent /5 penalties and their escapes.
//
// Diamond pick on Stone, base 6 ticks (damage = 8/45 = 0.17778, ceil(5.625) = 6).
// Underwater alone: speed = 8/5 = 1.6, damage = 1.6/45 = 0.035556, ceil(28.125) = 29.
// Airborne alone: identical arithmetic, 29 ticks.
// Both: speed = 8/25 = 0.32, damage = 0.32/45 = 0.0071111, ceil(140.625) = 141 ticks.
//
// Flying cancels only the airborne penalty and AquaAffinity only the underwater one; each leaves the other
// intact, so Underwater+Flying and Airborne+AquaAffinity both stay at 29 ticks.
//
// PINNED CONSTRAINT: there is no flying-equivalent escape for the underwater penalty in BreakContext --
// AquaAffinity is the only field that cancels it, so a flying player with their head in water is still
// slowed 5x. Symmetrically, Flying suppresses the airborne penalty even while the player is Airborne.
func TestBreakDurationEnvironmentPenalties(t *testing.T) {
	tests := []struct {
		name      string
		ctx       block.BreakContext
		wantTicks int64
	}{
		{name: "grounded and dry", ctx: block.BreakContext{}, wantTicks: 6},
		{name: "underwater", ctx: block.BreakContext{Underwater: true}, wantTicks: 29},
		{name: "airborne", ctx: block.BreakContext{Airborne: true}, wantTicks: 29},
		{name: "flying without airborne changes nothing", ctx: block.BreakContext{Flying: true}, wantTicks: 6},
		{name: "flying suppresses airborne", ctx: block.BreakContext{Airborne: true, Flying: true}, wantTicks: 6},
		{name: "aqua affinity without water changes nothing", ctx: block.BreakContext{AquaAffinity: true}, wantTicks: 6},
		{name: "flying does not suppress underwater", ctx: block.BreakContext{Underwater: true, Flying: true}, wantTicks: 29},
		{name: "aqua affinity does not suppress airborne", ctx: block.BreakContext{Airborne: true, AquaAffinity: true}, wantTicks: 29},
		{name: "underwater and airborne divide by 25", ctx: block.BreakContext{Underwater: true, Airborne: true}, wantTicks: 141},
		{name: "aqua affinity leaves the airborne half of the 25", ctx: block.BreakContext{Underwater: true, Airborne: true, AquaAffinity: true}, wantTicks: 29},
		{name: "flying leaves the underwater half of the 25", ctx: block.BreakContext{Underwater: true, Airborne: true, Flying: true}, wantTicks: 29},
		{name: "both escapes cancel both penalties", ctx: block.BreakContext{Underwater: true, Airborne: true, Flying: true, AquaAffinity: true}, wantTicks: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breakTicks(block.Stone{}, diaPick, tt.ctx); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
}

// TestEfficiencyAddendGolden pins enchantment.Efficiency.Addend as L*L+1 (L1 +2, L2 +5, L3 +10, L4 +17,
// L5 +26) and the fact that it is an addend to the tool's base efficiency, not a multiplier.
//
// Obsidian (hardness 35, diamond-pickaxe harvestable) is used because its large hardness keeps every level
// on a distinct tick count: ticks = ceil(35*30/speed) = ceil(1050/speed) with speed = 8 + Addend(L).
// L0 speed 8 -> ceil(131.25) = 132; L2 speed 13 -> ceil(80.77) = 81; L3 speed 18 -> ceil(58.33) = 59;
// L4 speed 25 -> 42; L5 speed 34 -> ceil(30.88) = 31.
//
// PINNED BUG: L1 (speed 10) is exactly 1050/10 = 105 in real arithmetic, but the code computes
// 1/(10/35/30) as 105.00000000000001 in float64 and math.Ceil rounds it up, so the pinned value is 106 --
// one tick more than the mathematically correct answer. Boundary cases that land on a whole tick are
// resolved by float error, not by the rule; L4 (1050/25 = 42) happens to land on the correct side. No
// rounding tolerance is applied before the Ceil.
//
// PINNED SCOPE: Efficiency is added only inside the `Effective && canHarvest` branch. A wooden pickaxe on
// stone diamond ore is effective but cannot harvest it, so speed is forced back to 1 and Efficiency V is
// worth nothing at all: damage = 1/3/100, ceil(300) = 300 ticks, identical to the unenchanted pick. This is
// the non-obvious half of the rule -- the enchantment is not merely reduced, it is discarded entirely.
func TestEfficiencyAddendGolden(t *testing.T) {
	for level, want := range map[int]float64{1: 2, 2: 5, 3: 10, 4: 17, 5: 26} {
		if got := enchantment.Efficiency.Addend(level); got != want {
			t.Errorf("Addend(%d) = %v, want %v", level, got, want)
		}
	}

	withEfficiency := func(s item.Stack, level int) item.Stack {
		if level == 0 {
			return s
		}
		return s.WithEnchantments(item.NewEnchantment(enchantment.Efficiency, level))
	}

	tests := []struct {
		name      string
		block     world.Block
		stack     item.Stack
		level     int
		wantTicks int64
	}{
		{name: "obsidian level 0", block: block.Obsidian{}, stack: diaPick, level: 0, wantTicks: 132},
		{name: "obsidian level 1", block: block.Obsidian{}, stack: diaPick, level: 1, wantTicks: 106},
		{name: "obsidian level 2", block: block.Obsidian{}, stack: diaPick, level: 2, wantTicks: 81},
		{name: "obsidian level 3", block: block.Obsidian{}, stack: diaPick, level: 3, wantTicks: 59},
		{name: "obsidian level 4", block: block.Obsidian{}, stack: diaPick, level: 4, wantTicks: 42},
		{name: "obsidian level 5", block: block.Obsidian{}, stack: diaPick, level: 5, wantTicks: 31},
		{name: "unharvestable ore level 0", block: stoneDiamOre, stack: woodPick, level: 0, wantTicks: 300},
		{name: "unharvestable ore level 1 gains nothing", block: stoneDiamOre, stack: woodPick, level: 1, wantTicks: 300},
		{name: "unharvestable ore level 5 gains nothing", block: stoneDiamOre, stack: woodPick, level: 5, wantTicks: 300},
		{name: "ineffective tool level 5 gains nothing", block: block.Stone{}, stack: withEfficiency(shears, 5), level: 0, wantTicks: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := withEfficiency(tt.stack, tt.level)
			if got := breakTicks(tt.block, s, block.BreakContext{}); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
}

// TestBreakDurationGoldTierAnomaly pins the consequence of gold's HarvestLevel 1 / BaseMiningEfficiency 12
// pairing: it is the fastest tier in the game on blocks it can harvest, yet it is stuck at the wood tier
// for what it is allowed to harvest.
//
// On Stone (hardness 1.5, any pickaxe harvests): gold speed 12 -> damage 12/45 = 0.26667, ceil(3.75) = 4
// ticks, strictly fewer than diamond's 6 and netherite's ceil(45/9) = 5. On stone diamond ore (hardness 3,
// requires HarvestLevel >= iron's 3): gold is effective but not harvestable, so speed collapses to 1 and
// damage = 1/3/100, giving the same 300 ticks as a wooden pickaxe -- 20x slower than an iron pickaxe's
// damage = 6/3/30 = 0.06667, ceil(15) = 15 ticks -- and it drops nothing.
func TestBreakDurationGoldTierAnomaly(t *testing.T) {
	tests := []struct {
		name      string
		block     world.Block
		stack     item.Stack
		wantTicks int64
	}{
		{name: "gold beats diamond on stone", block: block.Stone{}, stack: goldPick, wantTicks: 4},
		{name: "diamond on stone", block: block.Stone{}, stack: diaPick, wantTicks: 6},
		{name: "netherite on stone", block: block.Stone{}, stack: item.NewStack(item.Pickaxe{Tier: item.ToolTierNetherite}, 1), wantTicks: 5},
		{name: "wood on stone", block: block.Stone{}, stack: woodPick, wantTicks: 23},
		{name: "gold cannot harvest diamond ore", block: stoneDiamOre, stack: goldPick, wantTicks: 300},
		{name: "wood cannot harvest diamond ore", block: stoneDiamOre, stack: woodPick, wantTicks: 300},
		{name: "stone tier cannot harvest diamond ore", block: stoneDiamOre, stack: stonePick, wantTicks: 300},
		{name: "iron can harvest diamond ore", block: stoneDiamOre, stack: ironPick, wantTicks: 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breakTicks(tt.block, tt.stack, block.BreakContext{}); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
	if (block.Stone{}).BreakInfo().Harvestable(item.Pickaxe{Tier: item.ToolTierGold}) != true {
		t.Error("golden pickaxe should harvest stone")
	}
	if stoneDiamOre.BreakInfo().Harvestable(item.Pickaxe{Tier: item.ToolTierGold}) != false {
		t.Error("golden pickaxe should not harvest diamond ore")
	}
}

// TestBreakDurationSwordAndShears pins the two tools whose BaseMiningEfficiency is not a tier constant.
//
// Sword returns 15 for any block implementing the unexported marker interface{ Cobweb() } and 1.5 for
// everything else; Shears returns a flat 1.5 with a hard-coded HarvestLevel of 1. Cobweb has hardness 4,
// alwaysHarvestable, and is effective for both swords and shears.
// Diamond sword on cobweb: damage = 15/4/30 = 0.125, ceil(8) = 8 ticks.
// Shears on cobweb: damage = 1.5/4/30 = 0.0125, ceil(80) = 80 ticks.
// Diamond pickaxe on cobweb: not effective, but alwaysHarvestable keeps the /30 divisor, so
// damage = 1/4/30 = 0.0083333, ceil(120) = 120 ticks.
// Diamond sword on stone: not effective (pickaxeEffective) so speed stays 1, and pickaxeHarvestable is
// false for a sword so the divisor is 100: damage = 1/1.5/100, ceil(150) = 150 ticks -- a sword's 1.5
// bonus never reaches a block a sword is not effective on.
//
// PINNED SCOPE: the sword's 15 is keyed on a Cobweb() marker method, so the bonus applies to any block
// that grows one, not to a fixed block identity.
func TestBreakDurationSwordAndShears(t *testing.T) {
	tests := []struct {
		name      string
		block     world.Block
		stack     item.Stack
		wantTicks int64
	}{
		{name: "diamond sword on cobweb", block: block.Cobweb{}, stack: diaSword, wantTicks: 8},
		{name: "wooden sword on cobweb matches diamond", block: block.Cobweb{}, stack: item.NewStack(item.Sword{Tier: item.ToolTierWood}, 1), wantTicks: 8},
		{name: "shears on cobweb", block: block.Cobweb{}, stack: shears, wantTicks: 80},
		{name: "pickaxe on cobweb is ineffective but harvests", block: block.Cobweb{}, stack: diaPick, wantTicks: 120},
		{name: "bare hand on cobweb", block: block.Cobweb{}, stack: bareHandStack, wantTicks: 120},
		{name: "diamond sword on stone gets no bonus", block: block.Stone{}, stack: diaSword, wantTicks: 150},
		{name: "shears on stone get no bonus", block: block.Stone{}, stack: shears, wantTicks: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breakTicks(tt.block, tt.stack, block.BreakContext{}); got != tt.wantTicks {
				t.Errorf("got %d ticks, want %d", got, tt.wantTicks)
			}
		})
	}
	if got := (item.Sword{Tier: item.ToolTierDiamond}).BaseMiningEfficiency(block.Cobweb{}); got != 15 {
		t.Errorf("sword efficiency on cobweb = %v, want 15", got)
	}
	if got := (item.Sword{Tier: item.ToolTierDiamond}).BaseMiningEfficiency(block.Stone{}); got != 1.5 {
		t.Errorf("sword efficiency on stone = %v, want 1.5", got)
	}
	if got := (item.Shears{}).BaseMiningEfficiency(block.Cobweb{}); got != 1.5 {
		t.Errorf("shears efficiency on cobweb = %v, want 1.5", got)
	}
	if got := (item.Shears{}).HarvestLevel(); got != 1 {
		t.Errorf("shears HarvestLevel = %d, want 1", got)
	}
}

// TestHarvestablePickaxeAlias pins the observable consequence of `pickaxeHarvestable = pickaxeEffective`:
// for every block wired to that pair, tier is irrelevant to harvesting and only the tool type matters, so
// a wooden pickaxe harvests stone exactly as a netherite one does while a shovel, sword, shears and an
// empty hand all fail.
//
// The contrast case is a block that supplies its own Harvestable closure instead of the alias: stone
// diamond ore requires HarvestLevel >= item.ToolTierIron.HarvestLevel (3), so the wooden pickaxe that
// harvests stone fails there.
//
// PINNED CONSTRAINT: pickaxeEffective, pickaxeHarvestable and the other eight helpers are package-level
// mutable `var`s of type func(item.Tool) bool. Any code inside package block can reassign them at run
// time and change the harvest rules of every block at once -- this is global mutable state.
// An external block_test package cannot observe or perform that mutation, so
// what is pinned here is the aliasing's visible effect, not the variables themselves.
func TestHarvestablePickaxeAlias(t *testing.T) {
	tests := []struct {
		name  string
		block world.Block
		tool  item.Tool
		want  bool
	}{
		{name: "wooden pickaxe harvests stone", block: block.Stone{}, tool: item.Pickaxe{Tier: item.ToolTierWood}, want: true},
		{name: "golden pickaxe harvests stone", block: block.Stone{}, tool: item.Pickaxe{Tier: item.ToolTierGold}, want: true},
		{name: "netherite pickaxe harvests stone", block: block.Stone{}, tool: item.Pickaxe{Tier: item.ToolTierNetherite}, want: true},
		{name: "shovel does not harvest stone", block: block.Stone{}, tool: item.Shovel{Tier: item.ToolTierNetherite}, want: false},
		{name: "sword does not harvest stone", block: block.Stone{}, tool: item.Sword{Tier: item.ToolTierNetherite}, want: false},
		{name: "shears do not harvest stone", block: block.Stone{}, tool: item.Shears{}, want: false},
		{name: "bare hand does not harvest stone", block: block.Stone{}, tool: item.ToolNone{}, want: false},
		{name: "wooden pickaxe does not harvest diamond ore", block: stoneDiamOre, tool: item.Pickaxe{Tier: item.ToolTierWood}, want: false},
		{name: "iron pickaxe harvests diamond ore", block: stoneDiamOre, tool: item.Pickaxe{Tier: item.ToolTierIron}, want: true},
		{name: "cobweb is harvestable by a bare hand", block: block.Cobweb{}, tool: item.ToolNone{}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, ok := tt.block.(block.Breakable)
			if !ok {
				t.Fatalf("%T is not Breakable", tt.block)
			}
			info := b.BreakInfo()
			if got := info.Harvestable(tt.tool); got != tt.want {
				t.Errorf("Harvestable = %v, want %v", got, tt.want)
			}
			// The alias means Effective and Harvestable agree for every pickaxe-wired block.
			if _, isStone := tt.block.(block.Stone); isStone {
				if got := info.Effective(tt.tool); got != tt.want {
					t.Errorf("Effective = %v, want %v (alias of Harvestable)", got, tt.want)
				}
			}
		})
	}
}

// TestSilkTouchDrops pins that Silk Touch is detected by enchantment type alone (level is never read) and
// replaces the drop table wholesale.
//
// Stone uses silkTouchOneOf(Cobblestone{}, Stone{}): the *normal* drop is cobblestone and the *silk touch*
// drop is the block itself, so the enchantment flips stone from dropping cobblestone to dropping stone.
// Diamond ore uses oreDrops(item.Diamond{}, block): silk touch returns exactly one of the ore block,
// bypassing fortuneOreCount entirely, so no Fortune level can raise it above 1.
//
// PINNED SCOPE: the drop functions never inspect the tool's tier or whether it could actually harvest the
// block -- BreakInfo().Drops with an item.ToolNone{} still yields cobblestone from stone. Filtering an
// unharvestable break is the caller's job, not the drop function's.
func TestSilkTouchDrops(t *testing.T) {
	silk := []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 1)}
	silkAndFortune := []item.Enchantment{
		item.NewEnchantment(enchantment.SilkTouch, 1),
		item.NewEnchantment(enchantment.Fortune, 3),
	}

	tests := []struct {
		name         string
		block        block.Breakable
		tool         item.Tool
		enchantments []item.Enchantment
		wantItem     world.Item
		wantCount    int
	}{
		{name: "stone without silk touch drops cobblestone", block: block.Stone{}, tool: item.Pickaxe{Tier: item.ToolTierDiamond}, wantItem: block.Cobblestone{}, wantCount: 1},
		{name: "stone with silk touch drops stone", block: block.Stone{}, tool: item.Pickaxe{Tier: item.ToolTierDiamond}, enchantments: silk, wantItem: block.Stone{}, wantCount: 1},
		{name: "stone drop ignores the tool entirely", block: block.Stone{}, tool: item.ToolNone{}, wantItem: block.Cobblestone{}, wantCount: 1},
		{name: "diamond ore without silk touch drops a diamond", block: stoneDiamOre, tool: item.Pickaxe{Tier: item.ToolTierDiamond}, wantItem: item.Diamond{}, wantCount: 1},
		{name: "diamond ore with silk touch drops the ore block", block: stoneDiamOre, tool: item.Pickaxe{Tier: item.ToolTierDiamond}, enchantments: silk, wantItem: stoneDiamOre, wantCount: 1},
		{name: "silk touch beats fortune on ore", block: stoneDiamOre, tool: item.Pickaxe{Tier: item.ToolTierDiamond}, enchantments: silkAndFortune, wantItem: stoneDiamOre, wantCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drops := tt.block.BreakInfo().Drops(tt.tool, tt.enchantments)
			if len(drops) != 1 {
				t.Fatalf("got %d drops, want 1", len(drops))
			}
			if got := drops[0].Item(); got != tt.wantItem {
				t.Errorf("dropped %#v, want %#v", got, tt.wantItem)
			}
			if got := drops[0].Count(); got != tt.wantCount {
				t.Errorf("dropped count %d, want %d", got, tt.wantCount)
			}
		})
	}
	// Silk touch is matched on type only, so a level far above the enchantment's max behaves identically.
	drops := (block.Stone{}).BreakInfo().Drops(item.ToolNone{}, []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 99)})
	if len(drops) != 1 || drops[0].Item() != (world.Item)(block.Stone{}) {
		t.Errorf("silk touch level 99 drops %v, want one stone", drops)
	}
}

// TestFortuneOreDropSupport pins the support of the fortuneOreCount distribution rather than any single
// draw, because the drop helpers use the unseedable math/rand/v2 global generator.
//
// fortuneOreCount(base, L) returns base with probability 2/(L+2); otherwise it returns base*k with k drawn
// uniformly from [2, L+1]. So the reachable set is exactly {base} for L = 0, and {base, 2*base, ...,
// (L+1)*base} for L >= 1. Diamond ore has base 1 (oreDrops passes 1), so L = 1 -> {1, 2}, L = 2 ->
// {1, 2, 3}, L = 3 -> {1, 2, 3, 4}. With 20000 samples per level the chance of failing to observe any
// value whose probability is at least L/((L+2)*L) = 1/(L+2) >= 1/5 is far below 1e-1000, so the test is
// deterministic in practice.
//
// PINNED SCOPE: the bonus multiplier is never applied more than once and never exceeds L+1, so Fortune III
// on a single-drop ore can never yield 5 or more.
func TestFortuneOreDropSupport(t *testing.T) {
	const samples = 20000

	tests := []struct {
		name    string
		fortune int
		want    map[int]bool
	}{
		{name: "fortune 0", fortune: 0, want: map[int]bool{1: true}},
		{name: "fortune 1", fortune: 1, want: map[int]bool{1: true, 2: true}},
		{name: "fortune 2", fortune: 2, want: map[int]bool{1: true, 2: true, 3: true}},
		{name: "fortune 3", fortune: 3, want: map[int]bool{1: true, 2: true, 3: true, 4: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var enchantments []item.Enchantment
			if tt.fortune > 0 {
				enchantments = []item.Enchantment{item.NewEnchantment(enchantment.Fortune, tt.fortune)}
			}
			seen := make(map[int]bool, len(tt.want))
			for range samples {
				drops := stoneDiamOre.BreakInfo().Drops(item.Pickaxe{Tier: item.ToolTierDiamond}, enchantments)
				if len(drops) != 1 {
					t.Fatalf("got %d drops, want 1", len(drops))
				}
				if got := drops[0].Item(); got != (world.Item)(item.Diamond{}) {
					t.Fatalf("dropped %#v, want a diamond", got)
				}
				count := drops[0].Count()
				if !tt.want[count] {
					t.Fatalf("observed count %d outside the support %v", count, tt.want)
				}
				seen[count] = true
			}
			for count := range tt.want {
				if !seen[count] {
					t.Errorf("count %d never observed in %d samples", count, samples)
				}
			}
		})
	}
}

// TestDropXPRangeSupport pins XPDropRange as an inclusive [min, max] interval on both ends: RandomValue
// computes rand.IntN(max-min+1)+min, so a range of {3, 7} has five reachable values and a degenerate range
// where min == max always returns that value (diff 0 -> rand.IntN(1) == 0).
//
// Stone diamond ore declares withXPDropRange(3, 7); stone declares no XP range at all, leaving the zero
// value XPDropRange{0, 0}, which therefore always yields 0.
//
// PINNED SCOPE: RandomValue draws from the unseedable math/rand/v2 global generator, so it cannot be made
// reproducible by a caller; only its support is testable. A reversed range (max < min) would make
// rand.IntN panic on a non-positive argument -- nothing validates the range at construction, but no block
// in the tree constructs one, so that path is unreachable today and is not exercised here.
func TestDropXPRangeSupport(t *testing.T) {
	const samples = 20000

	tests := []struct {
		name  string
		block block.Breakable
		want  map[int]bool
	}{
		{name: "diamond ore drops 3 to 7", block: stoneDiamOre, want: map[int]bool{3: true, 4: true, 5: true, 6: true, 7: true}},
		{name: "stone drops no xp", block: block.Stone{}, want: map[int]bool{0: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.block.BreakInfo().XPDrops
			seen := make(map[int]bool, len(tt.want))
			for range samples {
				v := r.RandomValue()
				if !tt.want[v] {
					t.Fatalf("observed xp %d outside the support %v", v, tt.want)
				}
				seen[v] = true
			}
			for v := range tt.want {
				if !seen[v] {
					t.Errorf("xp %d never observed in %d samples", v, samples)
				}
			}
		})
	}
}
