package block

import (
	"github.com/jv2w/Impulse/server/world"
)

// encodeSlabBlock encodes the provided block in to an identifier and meta value that can be used to encode the slab.
// halfFlattened is a temporary hack for a stone_block_slab which has been flattened but double_stone_block_slab
// has not. This can be removed in 1.21.10 where they have flattened all slab types.
func encodeSlabBlock(block world.Block, double bool) (id string, suffix string) {
	suffix = "_slab"
	if double {
		suffix = "_double_slab"
	}

	switch block := block.(type) {
	case Andesite:
		if block.Polished {
			return "polished_andesite", suffix
		}
		return "andesite", suffix
	case Blackstone:
		if block.Type == NormalBlackstone() {
			return "blackstone", suffix
		} else if block.Type == PolishedBlackstone() {
			return "polished_blackstone", suffix
		}
	case BambooMosaic:
		return "bamboo_mosaic", suffix
	case Bricks:
		return "brick", suffix
	case Cinnabar:
		if !block.Chiseled {
			return "cinnabar", suffix
		}
	case CinnabarBricks:
		return "cinnabar_brick", suffix
	case Cobblestone:
		if block.Mossy {
			return "mossy_cobblestone", suffix
		}
		return "cobblestone", suffix
	case Copper:
		if block.Type == CutCopper() {
			suffix = "cut_copper_slab"
			if double {
				// The join could only ever produce this one string, so it is spelled out.
				suffix = "double_cut_copper_slab"
			}
			// The table holds the prefix the joins produced for every oxidation and waxed state,
			// so the string is the same one without allocating. An oxidation outside the table
			// falls back to the joins and panics in String exactly as it did before.
			if name, ok := cutCopperSlabPrefixes[cutCopperKey{block.Oxidation, block.Waxed}]; ok {
				return name, suffix
			}
			return cutCopperSlabPrefix(block.Oxidation, block.Waxed), suffix
		}
	case Deepslate:
		if block.Type == CobbledDeepslate() {
			return "cobbled_deepslate", suffix
		} else if block.Type == PolishedDeepslate() {
			return "polished_deepslate", suffix
		}
	case DeepslateBricks:
		if !block.Cracked {
			return "deepslate_brick", suffix
		}
	case DeepslateTiles:
		if !block.Cracked {
			return "deepslate_tile", suffix
		}
	case Diorite:
		if block.Polished {
			return "polished_diorite", suffix
		}
		return "diorite", suffix
	case EndBricks:
		return "end_stone_brick", suffix
	case Granite:
		if block.Polished {
			return "polished_granite", suffix
		}
		return "granite", suffix
	case MudBricks:
		return "mud_brick", suffix
	case NetherBricks:
		if block.Type == RedNetherBricks() {
			return "nether_brick", suffix
		}
		return "red_nether_brick", suffix
	case Planks:
		return block.Wood.String(), suffix
	case PolishedBlackstoneBrick:
		if !block.Cracked {
			return "polished_blackstone_brick", suffix
		}
	case PolishedCinnabar:
		return "polished_cinnabar", suffix
	case PolishedSulfur:
		return "polished_sulfur", suffix
	case PolishedTuff:
		return "polished_tuff", suffix
	case Prismarine:
		switch block.Type {
		case NormalPrismarine():
			return "prismarine", suffix
		case DarkPrismarine():
			return "dark_prismarine", suffix
		case BrickPrismarine():
			return "prismarine_brick", suffix
		}
		panic("invalid prismarine type")
	case Purpur:
		return "purpur", suffix
	case Quartz:
		if block.Smooth {
			return "smooth_quartz", suffix
		}
		return "quartz", suffix
	case ResinBricks:
		return "resin_brick", suffix
	case Sandstone:
		switch block.Type {
		case NormalSandstone():
			if block.Red {
				return "red_sandstone", suffix
			}
			return "sandstone", suffix
		case CutSandstone():
			if block.Red {
				return "cut_red_sandstone", suffix
			}
			return "cut_sandstone", suffix
		case SmoothSandstone():
			if block.Red {
				return "smooth_red_sandstone", suffix
			}
			return "smooth_sandstone", suffix
		}
		panic("invalid sandstone type")
	case Stone:
		if block.Smooth {
			return "smooth_stone", suffix
		}
		return "normal_stone", suffix
	case StoneBricks:
		if block.Type == MossyStoneBricks() {
			return "mossy_stone_brick", suffix
		}
		return "stone_brick", suffix
	case Sulfur:
		if !block.Chiseled {
			return "sulfur", suffix
		}
	case SulfurBricks:
		return "sulfur_brick", suffix
	case Tuff:
		if !block.Chiseled {
			return "tuff", suffix
		}
	case TuffBricks:
		if !block.Chiseled {
			return "tuff_brick", suffix
		}
	}
	panic("invalid block used for slab")
}

// cutCopperKey identifies one cut copper variant by oxidation and waxed state.
type cutCopperKey struct {
	oxidation OxidationType
	waxed     bool
}

// cutCopperSlabPrefix gives the oxidation and waxed prefix of a cut copper slab
// name, joining exactly the strings encodeSlabBlock used to join inline.
func cutCopperSlabPrefix(oxidation OxidationType, waxed bool) string {
	var name string
	if oxidation != UnoxidisedOxidation() {
		name = oxidation.String() + "_"
	}
	if waxed {
		name = "waxed_" + name
	}
	return name
}

// cutCopperSlabPrefixes holds cutCopperSlabPrefix for every oxidation and waxed
// state, so those joins run once at start instead of on every encode.
var cutCopperSlabPrefixes = func() map[cutCopperKey]string {
	oxidations := OxidationTypes()
	m := make(map[cutCopperKey]string, len(oxidations)*2)
	for _, o := range oxidations {
		m[cutCopperKey{o, false}] = cutCopperSlabPrefix(o, false)
		m[cutCopperKey{o, true}] = cutCopperSlabPrefix(o, true)
	}
	return m
}()

// SlabBlocks returns a list of all possible blocks for a slab.
func SlabBlocks() []world.Block {
	// The fixed list below holds 38 blocks and the loops add one per prismarine type, two per
	// sandstone type, one per wood type and two per oxidation, so the whole list is built in one
	// presized slice instead of growing. The same blocks are appended in the same order, and the
	// count only sizes the buffer, so a stale number would cost one regrow and nothing else.
	prismarines, sandstones, woods, oxidations := PrismarineTypes(), SandstoneTypes(), WoodTypes(), OxidationTypes()
	b := append(make([]world.Block, 0, 38+len(prismarines)+2*len(sandstones)+len(woods)+2*len(oxidations)),
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
		Stone{Smooth: true},
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
		if s != ChiseledSandstone() {
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
	return b
}
