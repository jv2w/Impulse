package block

import (
	"math/rand/v2"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Oxidisable is a block that can naturally oxidise over time, such as copper.
type Oxidisable interface {
	world.Block
	// CanOxidate returns whether the block can oxidate, i.e. if it's not waxed.
	CanOxidate() bool
	// OxidationLevel returns the currently level of oxidation of the block.
	OxidationLevel() OxidationType
	// WithOxidationLevel returns the oxidizable block with the oxidation level passed.
	WithOxidationLevel(OxidationType) Oxidisable
}

// attemptOxidation attempts to oxidise the block at the position passed. The details for this logic is
// described on the Minecraft Wiki: https://minecraft.wiki/w/Oxidation.
func attemptOxidation(pos cube.Pos, tx *world.Tx, r *rand.Rand, o Oxidisable) {
	level := o.OxidationLevel()
	if level == OxidisedOxidation() || !o.CanOxidate() {
		return
	} else if r.Float64() > 64.0/1125.0 {
		return
	}

	var all, higher int
	// nPos is pos offset by x, y and z, so nPos.X()-pos.X() is exactly x (the same two's complement
	// result even on overflow) and the distance depends only on the loop counters. That lets the two
	// outer terms be hoisted and the offset be computed only for positions actually looked up, so
	// the same tx.Block calls happen on the same positions in the same order. level.Uint8() and
	// b.OxidationLevel() are read once each instead of twice; both are pure reads of a block value.
	levelID := level.Uint8()
	for x := -4; x <= 4; x++ {
		dx := abs(x)
		for y := -4; y <= 4; y++ {
			dxy := dx + abs(y)
			if dxy > 4 {
				// Every z here would exceed the distance of 4 and be skipped anyway.
				continue
			}
			for z := -4; z <= 4; z++ {
				if x == 0 && y == 0 && z == 0 {
					continue
				}
				if dxy+abs(z) > 4 {
					continue
				}

				b, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(Oxidisable)
				if !ok || !b.CanOxidate() {
					continue
				}
				bLevel := b.OxidationLevel().Uint8()
				if bLevel < levelID {
					return
				}
				all++
				if bLevel > levelID {
					higher++
				}
			}
		}
	}

	chance := float64(higher+1) / float64(all+1)
	if level == UnoxidisedOxidation() {
		chance *= chance * 0.75
	} else {
		chance *= chance
	}
	if r.Float64() < chance {
		level, _ = level.Increase()
		tx.SetBlock(pos, o.WithOxidationLevel(level), nil)
	}
}

// copperBlockName returns the name of a copper block with the given oxidation and waxed status.
func copperBlockName(blockName string, oxidation OxidationType, waxed bool) string {
	name := blockName
	if oxidation != UnoxidisedOxidation() {
		name = oxidation.String() + "_" + name
	}
	if waxed {
		name = "waxed_" + name
	}
	return "minecraft:" + name
}
