package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// DoubleFlower is a two block high flower consisting of an upper and lower part.
type DoubleFlower struct {
	transparent
	empty

	// UpperPart is set if the plant is the upper part.
	UpperPart bool
	// Type is the type of the double plant.
	Type DoubleFlowerType
}

// FlammabilityInfo ...
func (d DoubleFlower) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, true)
}

// BoneMeal ...
func (d DoubleFlower) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	dropItem(tx, item.NewStack(d, 1), pos.Vec3Centre())
	return item.BoneMealResultSmall
}

// NeighbourUpdateTick ...
func (d DoubleFlower) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if d.UpperPart {
		if bottom, ok := tx.Block(pos.Side(cube.FaceDown)).(DoubleFlower); !ok || bottom.Type != d.Type || bottom.UpperPart {
			breakBlockNoDrops(d, pos, tx)
		}
	} else if upper, ok := tx.Block(pos.Side(cube.FaceUp)).(DoubleFlower); !ok || upper.Type != d.Type || !upper.UpperPart {
		breakBlockNoDrops(d, pos, tx)
	} else if !supportsVegetation(d, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(d, pos, tx)
	}
}

// UseOnBlock ...
func (d DoubleFlower) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used || !replaceableWith(tx, pos.Side(cube.FaceUp), d) || !supportsVegetation(d, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, d, user, ctx)
	place(tx, pos.Side(cube.FaceUp), DoubleFlower{Type: d.Type, UpperPart: true}, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (d DoubleFlower) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(d))
}

// CompostChance ...
func (DoubleFlower) CompostChance() float64 {
	return 0.65
}

// HasLiquidDrops ...
func (d DoubleFlower) HasLiquidDrops() bool {
	return true
}

// doubleFlowerName gives the encoded name of a double flower of a type.
var doubleFlowerName = enumNamer(DoubleFlowerTypes(), DoubleFlowerType.String)

// EncodeItem ...
func (d DoubleFlower) EncodeItem() (name string, meta int16) {
	return doubleFlowerName(d.Type), 0
}

// EncodeBlock ...
func (d DoubleFlower) EncodeBlock() (string, map[string]any) {
	return doubleFlowerName(d.Type), map[string]any{"upper_block_bit": d.UpperPart}
}

// allDoubleFlowers ...
func allDoubleFlowers() (b []world.Block) {
	// Every type yields exactly two blocks, so the final length is known before the loop starts.
	types := DoubleFlowerTypes()
	b = make([]world.Block, 0, len(types)*2)
	for _, d := range types {
		b = append(b, DoubleFlower{Type: d, UpperPart: true})
		b = append(b, DoubleFlower{Type: d, UpperPart: false})
	}
	return
}
