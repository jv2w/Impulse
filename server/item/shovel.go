package item

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
	"time"
)

// Shovel is a tool generally used for mining ground-like blocks, such as sand, gravel and dirt. Additionally,
// shovels may be used to turn grass into dirt paths.
type Shovel struct {
	// Tier is the tier of the shovel.
	Tier ToolTier
}

// UseOnBlock handles the creation of dirt path blocks from dirt or grass blocks.
func (s Shovel) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	// A bottom face returned false no matter what the block was: the guard
	// below did it for a shovellable block and the final return did it for
	// every other one. Checking the face first skips the block lookup and the
	// Shovel call on that face.
	if face == cube.FaceDown {
		// Dirt paths are not created when the bottom face is clicked.
		return false
	}
	if b, ok := tx.Block(pos).(shovellable); ok {
		if res, ok := b.Shovel(); ok {
			if tx.Block(pos.Side(cube.FaceUp)) != air() {
				// Dirt paths can only be created if air is above the grass block.
				return false
			}
			tx.SetBlock(pos, res, nil)
			tx.PlaySound(pos.Vec3(), sound.ItemUseOn{Block: res})

			ctx.DamageItem(1)
			return true
		}
	}
	return false
}

// shovellable represents a block that can be changed by using a shovel on it.
type shovellable interface {
	// Shovel returns a block that results from using a shovel on it, or false if it could not be changed using
	// a shovel.
	Shovel() (world.Block, bool)
}

// MaxCount always returns 1.
func (s Shovel) MaxCount() int {
	return 1
}

// AttackDamage returns the attack damage to the shovel.
func (s Shovel) AttackDamage() float64 {
	return s.Tier.BaseAttackDamage
}

// ToolType returns the tool type for shovels.
func (s Shovel) ToolType() ToolType {
	return TypeShovel
}

// HarvestLevel ...
func (s Shovel) HarvestLevel() int {
	return s.Tier.HarvestLevel
}

// BaseMiningEfficiency ...
func (s Shovel) BaseMiningEfficiency(world.Block) float64 {
	return s.Tier.BaseMiningEfficiency
}

// EnchantmentValue ...
func (s Shovel) EnchantmentValue() int {
	return s.Tier.EnchantmentValue
}

// DurabilityInfo ...
func (s Shovel) DurabilityInfo() DurabilityInfo {
	// brokenIntoNothing is the same simpleItem(Stack{}) this built on every
	// call: it returns the same zero Stack by value, so sharing one closure
	// instead of allocating a new one is indistinguishable.
	return DurabilityInfo{
		MaxDurability:    s.Tier.Durability,
		BrokenItem:       brokenIntoNothing,
		AttackDurability: 2,
		BreakDurability:  1,
	}
}

// SmeltInfo ...
func (s Shovel) SmeltInfo() SmeltInfo {
	switch s.Tier {
	case ToolTierIron:
		return newOreSmeltInfo(NewStack(IronNugget{}, 1), 0.1)
	case ToolTierGold:
		return newOreSmeltInfo(NewStack(GoldNugget{}, 1), 0.1)
	case ToolTierCopper:
		return newOreSmeltInfo(NewStack(CopperNugget{}, 1), 0.1)
	}
	return SmeltInfo{}
}

// FuelInfo ...
func (s Shovel) FuelInfo() FuelInfo {
	if s.Tier == ToolTierWood {
		return newFuelInfo(time.Second * 10)
	}
	return FuelInfo{}
}

// RepairableBy ...
func (s Shovel) RepairableBy(i Stack) bool {
	return toolTierRepairable(s.Tier)(i)
}

// shovelNames holds the encoded name of every vanilla shovel tier.
var shovelNames = toolNames("_shovel")

// EncodeItem ...
func (s Shovel) EncodeItem() (name string, meta int16) {
	if n, ok := shovelNames[s.Tier.Name]; ok {
		return n, 0
	}
	return "minecraft:" + s.Tier.Name + "_shovel", 0
}
