package block

import (
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// encodeStairsBlock encodes the provided block in to an identifier and meta value that can be used to encode the stairs.
func encodeStairsBlock(block world.Block) string {
	switch block := block.(type) {
	case Andesite:
		if block.Polished {
			return "polished_andesite"
		}
		return "andesite"
	case Blackstone:
		if block.Type == NormalBlackstone() {
			return "blackstone"
		} else if block.Type == PolishedBlackstone() {
			return "polished_blackstone"
		}
	case BambooMosaic:
		return "bamboo_mosaic"
	case Bricks:
		return "brick"
	case Cinnabar:
		if !block.Chiseled {
			return "cinnabar"
		}
	case CinnabarBricks:
		return "cinnabar_brick"
	case Cobblestone:
		if block.Mossy {
			return "mossy_cobblestone"
		}
		return "stone"
	case Concrete:
		return block.Colour.String() + "_concrete"
	case Copper:
		if block.Type == CutCopper() {
			name := "cut_copper"
			if block.Oxidation != UnoxidisedOxidation() {
				name = block.Oxidation.String() + "_" + name
			}
			if block.Waxed {
				name = "waxed_" + name
			}
			return name
		}
	case Deepslate:
		if block.Type == CobbledDeepslate() {
			return "cobbled_deepslate"
		} else if block.Type == PolishedDeepslate() {
			return "polished_deepslate"
		}
	case DeepslateBricks:
		if !block.Cracked {
			return "deepslate_brick"
		}
	case DeepslateTiles:
		if !block.Cracked {
			return "deepslate_tile"
		}
	case Diorite:
		if block.Polished {
			return "polished_diorite"
		}
		return "diorite"
	case EndBricks:
		return "end_brick"
	case Granite:
		if block.Polished {
			return "polished_granite"
		}
		return "granite"
	case MudBricks:
		return "mud_brick"
	case NetherBricks:
		if block.Type == RedNetherBricks() {
			return "nether_brick"
		}
		return "red_nether_brick"
	case Planks:
		return block.Wood.String()
	case PolishedBlackstoneBrick:
		if !block.Cracked {
			return "polished_blackstone_brick"
		}
	case PolishedCinnabar:
		return "polished_cinnabar"
	case PolishedSulfur:
		return "polished_sulfur"
	case PolishedTuff:
		return "polished_tuff"
	case Prismarine:
		switch block.Type {
		case NormalPrismarine():
			return "prismarine"
		case DarkPrismarine():
			return "dark_prismarine"
		case BrickPrismarine():
			return "prismarine_bricks"
		}
		panic("invalid prismarine type")
	case Purpur:
		return "purpur"
	case Quartz:
		if block.Smooth {
			return "smooth_quartz"
		}
		return "quartz"
	case ResinBricks:
		return "resin_brick"
	case Sandstone:
		switch block.Type {
		case NormalSandstone():
			if block.Red {
				return "red_sandstone"
			}
			return "sandstone"
		case SmoothSandstone():
			if block.Red {
				return "smooth_red_sandstone"
			}
			return "smooth_sandstone"
		}
		panic("invalid sandstone type")
	case Stone:
		if !block.Smooth {
			return "normal_stone"
		}
	case StoneBricks:
		if block.Type == MossyStoneBricks() {
			return "mossy_stone_brick"
		}
		return "stone_brick"
	case Sulfur:
		if !block.Chiseled {
			return "sulfur"
		}
	case SulfurBricks:
		return "sulfur_brick"
	case Tuff:
		if !block.Chiseled {
			return "tuff"
		}
	case TuffBricks:
		if !block.Chiseled {
			return "tuff_brick"
		}
	case Wool:
		return block.Colour.String() + "_wool"
	}
	panic("invalid block used for stairs")
}

// StairsBlocks returns a list of all possible blocks for stairs.
func StairsBlocks() []world.Block {
	// The fixed list below holds 37 blocks and the loops add one per prismarine type, two per
	// sandstone type, one per wood type, two per oxidation and two per colour, so the whole list
	// is built in one presized slice instead of growing. The same blocks are appended in the same
	// order, and the count only sizes the buffer, so a stale number would cost one regrow and
	// nothing else.
	prismarines, sandstones, woods, oxidations, colours := PrismarineTypes(), SandstoneTypes(), WoodTypes(), OxidationTypes(), item.Colours()
	b := append(make([]world.Block, 0, 37+len(prismarines)+2*len(sandstones)+len(woods)+2*len(oxidations)+2*len(colours)),
		Andesite{Polished: true},
		Andesite{},
		BambooMosaic{},
		Blackstone{Type: PolishedBlackstone()},
		Blackstone{},
		Bricks{},
		Cinnabar{},
		CinnabarBricks{},
		Cobblestone{Mossy: true},
		Cobblestone{},
		DeepslateBricks{},
		DeepslateTiles{},
		Deepslate{Type: CobbledDeepslate()},
		Deepslate{Type: PolishedDeepslate()},
		Diorite{Polished: true},
		Diorite{},
		EndBricks{},
		Granite{Polished: true},
		Granite{},
		MudBricks{},
		NetherBricks{Type: RedNetherBricks()},
		NetherBricks{},
		PolishedBlackstoneBrick{},
		PolishedCinnabar{},
		PolishedSulfur{},
		PolishedTuff{},
		Purpur{},
		Quartz{Smooth: true},
		Quartz{},
		ResinBricks{},
		StoneBricks{Type: MossyStoneBricks()},
		StoneBricks{},
		Stone{},
		Sulfur{},
		SulfurBricks{},
		Tuff{},
		TuffBricks{},
	)
	for _, p := range prismarines {
		b = append(b, Prismarine{Type: p})
	}
	for _, s := range sandstones {
		if s != CutSandstone() && s != ChiseledSandstone() {
			b = append(b, Sandstone{Type: s})
			b = append(b, Sandstone{Type: s, Red: true})
		}
	}
	for _, w := range woods {
		b = append(b, Planks{Wood: w})
	}
	for _, o := range oxidations {
		b = append(b, Copper{Type: CutCopper(), Oxidation: o})
		b = append(b, Copper{Type: CutCopper(), Oxidation: o, Waxed: true})
	}
	for _, c := range colours {
		b = append(b, Concrete{Colour: c})
		b = append(b, Wool{Colour: c})
	}
	return b
}
