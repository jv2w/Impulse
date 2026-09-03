package block

import (
	"math/rand/v2"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// Blackstone is a naturally generating block in the nether that can be used to craft stone tools, brewing stands and
// furnaces. Gilded blackstone also has a 10% chance to drop 2-6 golden nuggets.
type Blackstone struct {
	solid
	bassDrum

	// Type is the type of blackstone of the block.
	Type BlackstoneType
}

// gildedNuggetChances holds the chance of gilded blackstone dropping gold nuggets for fortune
// levels 0 to 3. The list was built again on every drop; the constants are the same, so the
// chances are bit for bit what they were. It never leaves this file.
var gildedNuggetChances = [4]float64{0.1, 1.0 / 7.0, 0.25, 1.0}

// BreakInfo ...
func (b Blackstone) BreakInfo() BreakInfo {
	var drops func(item.Tool, []item.Enchantment) []item.Stack
	hardness := 1.5

	switch b.Type {
	case GildedBlackstone():
		drops = func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
			if hasSilkTouch(enchantments) {
				return []item.Stack{item.NewStack(b, 1)}
			}
			if rand.Float64() < gildedNuggetChances[min(fortuneLevel(enchantments), 3)] {
				return []item.Stack{item.NewStack(item.GoldNugget{}, rand.IntN(4)+2)}
			}
			return []item.Stack{item.NewStack(b, 1)}
		}
	case PolishedBlackstone():
		hardness = 2
	}
	if drops == nil {
		// oneOf boxes the block and builds a closure, so it is only built for the types that
		// reach it. Gilded blackstone threw the result away before.
		drops = oneOf(b)
	}

	return newBreakInfo(hardness, pickaxeHarvestable, pickaxeEffective, drops).withBlastResistance(6)
}

// blackstoneName gives the encoded name of blackstone of a type.
var blackstoneName = enumNamer(BlackstoneTypes(), BlackstoneType.String)

// EncodeItem ...
func (b Blackstone) EncodeItem() (name string, meta int16) {
	return blackstoneName(b.Type), 0
}

// EncodeBlock ...
func (b Blackstone) EncodeBlock() (string, map[string]any) {
	return blackstoneName(b.Type), nil
}

// allBlackstone returns a list of all blackstone block variants.
func allBlackstone() (s []world.Block) {
	types := BlackstoneTypes()
	// The count is known before the loop, so the list is sized once instead of grown.
	s = make([]world.Block, 0, len(types))
	for _, t := range types {
		s = append(s, Blackstone{Type: t})
	}
	return
}
