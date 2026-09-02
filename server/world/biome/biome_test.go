package biome

import (
	"encoding/binary"
	"fmt"
	"image/color"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// register.go fills two package-level maps in server/world from an init
// function, then hardens the registry through a linkname'd
// world.finaliseBiomeRegistry. Both halves are CLAUDE.md 2.4 violations: the
// state is global, and it is built by init rather than by a constructor.
//
// The IDs those maps hold are the on-disk and on-network identity of a biome.
// A chunk stores biome IDs, and BiomeDefinitions sends them to the client, so
// a changed ID silently rewrites every saved chunk and desynchronises every
// connected client. That makes the mapping below result integrity in the sense
// of CLAUDE.md 2.1, and it is pinned exhaustively rather than by sampling.

// golden is the complete registry as it stands: every biome that init
// registers appears exactly once, and nothing else is registered. Ordered by
// ID. typ is the Go type name inside this package, which is deliberately not
// the same string as the biome name for many entries (ID 3 is WindsweptHills
// but serialises as "extreme_hills"), so both are pinned.
var golden = []struct {
	id    int
	name  string
	typ   string
	temp  float64
	rain  float64
	depth float64
	scale float64
	water color.RGBA
	tags  []string
}{
	{0, "ocean", "Ocean", 0.5, 0.5, -1, 0.1, color.RGBA{R: 0x17, G: 0x87, B: 0xd4, A: 0xa5}, []string{"monster", "ocean", "overworld", "fast_fishing", "high_seas"}},
	{1, "plains", "Plains", 0.8, 0.4, 0.125, 0.05, color.RGBA{R: 0x44, G: 0xaf, B: 0xf5, A: 0xa5}, []string{"animal", "monster", "overworld", "plains", "bee_habitat"}},
	{2, "desert", "Desert", 2, 0, 0.125, 0.05, color.RGBA{R: 0x32, G: 0xa5, B: 0x98, A: 0xa5}, []string{"desert", "monster", "overworld", "spawns_gold_rabbits", "spawns_warm_variant_farm_animals", "spawns_warm_variant_frogs"}},
	{3, "extreme_hills", "WindsweptHills", 0.2, 0.3, 1, 0.5, color.RGBA{R: 0x00, G: 0x7b, B: 0xf7, A: 0xa5}, []string{"animal", "extreme_hills", "monster", "overworld", "spawns_cold_variant_farm_animals"}},
	{4, "forest", "Forest", 0.7, 0.8, 0.1, 0.2, color.RGBA{R: 0x1e, G: 0x97, B: 0xf2, A: 0xa5}, []string{"animal", "forest", "monster", "overworld", "bee_habitat"}},
	{5, "taiga", "Taiga", 0.25, 0.8, 0.1, 0.2, color.RGBA{R: 0x28, G: 0x70, B: 0x82, A: 0xa5}, []string{"animal", "forest", "monster", "overworld", "taiga", "has_structure_trail_ruins", "spawns_cold_variant_farm_animals"}},
	{6, "swampland", "Swamp", 0.8, 0.9, -0.2, 0.1, color.RGBA{R: 0x61, G: 0x7b, B: 0x64, A: 0xa5}, []string{"animal", "monster", "overworld", "swamp", "spawns_slimes_on_surface", "slime", "swamp_water_huge_mushroom"}},
	{7, "river", "River", 0.5, 0.5, -0.5, 0, color.RGBA{R: 0x00, G: 0x84, B: 0xff, A: 0xa5}, []string{"overworld", "spawns_more_frequent_drowned", "spawns_reduced_water_ambient_mobs", "spawns_river_mobs", "river"}},
	{8, "hell", "NetherWastes", 2, 0, 0.1, 0.2, color.RGBA{R: 0x90, G: 0x59, B: 0x57, A: 0xa5}, []string{"nether", "nether_wastes", "spawn_endermen", "spawn_few_piglins", "spawn_ghast", "spawn_magma_cubes", "spawns_nether_mobs", "spawn_zombified_piglin", "spawns_warm_variant_farm_animals"}},
	{9, "the_end", "End", 0.5, 0.5, 0.1, 0.2, color.RGBA{R: 0x62, G: 0x52, B: 0x9e, A: 0xa5}, []string{"the_end", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs"}},
	{10, "legacy_frozen_ocean", "LegacyFrozenOcean", 0, 0.5, -1, 0.1, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"legacy", "frozen", "ocean", "overworld", "spawns_cold_variant_farm_animals", "spawns_polar_bears_on_alternate_blocks", "fast_fishing", "high_seas"}},
	{11, "frozen_river", "FrozenRiver", 0, 0.5, -0.5, 0, color.RGBA{R: 0x18, G: 0x53, B: 0x90, A: 0xa5}, []string{"frozen", "overworld", "river", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_river_mobs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{12, "ice_plains", "SnowyPlains", 0, 0.5, 0.125, 0.05, color.RGBA{R: 0x14, G: 0x55, B: 0x9b, A: 0xa5}, []string{"frozen", "ice", "ice_plains", "overworld", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{13, "ice_mountains", "SnowyMountains", 0, 0.5, 0.45, 0.3, color.RGBA{R: 0x11, G: 0x56, B: 0xa7, A: 0xa5}, []string{"frozen", "ice", "mountain", "overworld", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{14, "mushroom_island", "MushroomFields", 0.9, 1, 0.2, 0.3, color.RGBA{R: 0x8a, G: 0x89, B: 0x97, A: 0xa5}, []string{"mooshroom_island", "overworld", "spawns_without_patrols"}},
	{15, "mushroom_island_shore", "MushroomFieldShore", 0.9, 1, 0, 0.025, color.RGBA{R: 0x81, G: 0x81, B: 0x93, A: 0xa5}, []string{"mooshroom_island", "overworld", "shore", "spawns_without_patrols"}},
	{16, "beach", "Beach", 0.8, 0.4, 0, 0.025, color.RGBA{R: 0x15, G: 0x7c, B: 0xab, A: 0xa5}, []string{"beach", "monster", "overworld", "warm"}},
	{17, "desert_hills", "DesertHills", 2, 0, 0.45, 0.3, color.RGBA{R: 0x1a, G: 0x7a, B: 0xa1, A: 0xa5}, []string{"desert", "hills", "monster", "overworld", "spawns_gold_rabbits", "spawns_warm_variant_farm_animals"}},
	{18, "forest_hills", "WoodedHills", 0.7, 0.8, 0.45, 0.3, color.RGBA{R: 0x05, G: 0x6b, B: 0xd1, A: 0xa5}, []string{"animal", "hills", "monster", "overworld", "forest", "bee_habitat"}},
	{19, "taiga_hills", "TaigaHills", 0.25, 0.8, 0.45, 0.3, color.RGBA{R: 0x23, G: 0x65, B: 0x83, A: 0xa5}, []string{"animal", "hills", "monster", "overworld", "forest", "taiga", "spawns_cold_variant_farm_animals"}},
	{20, "extreme_hills_edge", "MountainEdge", 0.2, 0.3, 0.8, 0.4, color.RGBA{R: 0x04, G: 0x5c, B: 0xd5, A: 0xa5}, []string{"animal", "edge", "extreme_hills", "monster", "mountain", "overworld", "spawns_cold_variant_farm_animals"}},
	{21, "jungle", "Jungle", 0.95, 0.9, 0.1, 0.2, color.RGBA{R: 0x14, G: 0xa2, B: 0xc5, A: 0xa5}, []string{"animal", "has_structure_trail_ruins", "jungle", "monster", "overworld", "rare", "spawns_jungle_mobs", "spawns_warm_variant_farm_animals"}},
	{22, "jungle_hills", "JungleHills", 0.95, 0.9, 0.45, 0.3, color.RGBA{R: 0x1b, G: 0x9e, B: 0xd8, A: 0xa5}, []string{"animal", "hills", "jungle", "monster", "overworld", "spawns_jungle_mobs", "spawns_warm_variant_farm_animals"}},
	{23, "jungle_edge", "JungleEdge", 0.95, 0.8, 0.1, 0.2, color.RGBA{R: 0x0d, G: 0x8a, B: 0xe3, A: 0xa5}, []string{"animal", "edge", "jungle", "monster", "overworld", "spawns_jungle_mobs", "spawns_warm_variant_farm_animals"}},
	{24, "deep_ocean", "DeepOcean", 0.5, 0.5, -1.8, 0.1, color.RGBA{R: 0x17, G: 0x87, B: 0xd4, A: 0xa5}, []string{"deep", "monster", "ocean", "overworld", "spawns_warm_variant_farm_animals", "fast_fishing", "high_seas"}},
	{25, "stone_beach", "StonyShore", 0.2, 0.3, 0.1, 0.8, color.RGBA{R: 0x0d, G: 0x67, B: 0xbb, A: 0xa5}, []string{"beach", "monster", "overworld", "stone"}},
	{26, "cold_beach", "SnowyBeach", 0.05, 0.3, 0, 0.025, color.RGBA{R: 0x14, G: 0x63, B: 0xa5, A: 0xa5}, []string{"beach", "cold", "monster", "overworld", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{27, "birch_forest", "BirchForest", 0.6, 0.6, 0.1, 0.2, color.RGBA{R: 0x06, G: 0x77, B: 0xce, A: 0xa5}, []string{"animal", "birch", "forest", "monster", "overworld", "bee_habitat"}},
	{28, "birch_forest_hills", "BirchForestHills", 0.6, 0.6, 0.45, 0.3, color.RGBA{R: 0x0a, G: 0x74, B: 0xc4, A: 0xa5}, []string{"animal", "birch", "forest", "hills", "monster", "overworld", "bee_habitat"}},
	{29, "roofed_forest", "DarkForest", 0.7, 0.8, 0.1, 0.2, color.RGBA{R: 0x3b, G: 0x6c, B: 0xd1, A: 0xa5}, []string{"animal", "forest", "monster", "no_legacy_worldgen", "overworld", "roofed"}},
	{30, "cold_taiga", "SnowyTaiga", -0.5, 0.4, 0.2, 0.2, color.RGBA{R: 0x20, G: 0x5e, B: 0x83, A: 0xa5}, []string{"animal", "cold", "forest", "monster", "overworld", "taiga", "has_structure_trail_ruins", "spawns_cold_variant_farm_animals"}},
	{31, "cold_taiga_hills", "SnowyTaigaHills", -0.5, 0.4, 0.45, 0.3, color.RGBA{R: 0x24, G: 0x5b, B: 0x78, A: 0xa5}, []string{"animal", "cold", "forest", "hills", "monster", "overworld", "taiga", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_white_rabbits"}},
	{32, "mega_taiga", "OldGrowthPineTaiga", 0.3, 0.8, 0.2, 0.2, color.RGBA{R: 0x2d, G: 0x6d, B: 0x77, A: 0xa5}, []string{"animal", "forest", "mega", "monster", "overworld", "rare", "taiga", "has_structure_trail_ruins", "spawns_cold_variant_farm_animals"}},
	{33, "mega_taiga_hills", "GiantTreeTaigaHills", 0.3, 0.8, 0.45, 0.3, color.RGBA{R: 0x28, G: 0x63, B: 0x78, A: 0xa5}, []string{"animal", "forest", "hills", "mega", "monster", "overworld", "taiga", "spawns_cold_variant_farm_animals"}},
	{34, "extreme_hills_plus_trees", "WindsweptForest", 0.2, 0.3, 1, 0.5, color.RGBA{R: 0x0e, G: 0x63, B: 0xab, A: 0xa5}, []string{"animal", "extreme_hills", "forest", "monster", "mountain", "overworld", "spawns_cold_variant_farm_animals"}},
	{35, "savanna", "Savanna", 1.2, 0, 0.125, 0.05, color.RGBA{R: 0x2c, G: 0x8b, B: 0x9c, A: 0xa5}, []string{"animal", "monster", "overworld", "savanna", "spawns_savanna_mobs", "spawns_warm_variant_farm_animals"}},
	{36, "savanna_plateau", "SavannaPlateau", 1, 0, 1.5, 0.025, color.RGBA{R: 0x25, G: 0x90, B: 0xa8, A: 0xa5}, []string{"animal", "monster", "overworld", "plateau", "savanna", "spawns_savanna_mobs", "spawns_warm_variant_farm_animals"}},
	{37, "mesa", "Badlands", 2, 0, 0.1, 0.2, color.RGBA{R: 0x4e, G: 0x7f, B: 0x81, A: 0xa5}, []string{"animal", "mesa", "monster", "overworld", "spawns_mesa_mobs", "spawns_warm_variant_farm_animals", "surface_mineshaft"}},
	{38, "mesa_plateau_stone", "WoodedBadlandsPlateau", 2, 0, 1.5, 0.025, color.RGBA{R: 0x55, G: 0x80, B: 0x9e, A: 0xa5}, []string{"animal", "mesa", "monster", "overworld", "plateau", "rare", "stone", "spawns_mesa_mobs", "spawns_warm_variant_farm_animals", "surface_mineshaft"}},
	{39, "mesa_plateau", "BadlandsPlateau", 2, 0, 1.5, 0.025, color.RGBA{R: 0x55, G: 0x80, B: 0x9e, A: 0xa5}, []string{"animal", "mesa", "monster", "overworld", "plateau", "rare", "spawns_mesa_mobs", "spawns_warm_variant_farm_animals", "surface_mineshaft"}},
	{40, "warm_ocean", "WarmOcean", 0.5, 0.5, -1, 0.1, color.RGBA{R: 0x02, G: 0xb0, B: 0xe5, A: 0xa5}, []string{"monster", "ocean", "overworld", "warm", "spawns_warm_variant_farm_animals", "spawns_warm_variant_frogs", "fast_fishing", "high_seas"}},
	{41, "deep_warm_ocean", "DeepWarmOcean", 0.5, 0.5, -1.8, 0.1, color.RGBA{R: 0x02, G: 0xb0, B: 0xe5, A: 0xa5}, []string{"deep", "monster", "ocean", "overworld", "warm", "spawns_warm_variant_farm_animals", "fast_fishing", "high_seas"}},
	{42, "lukewarm_ocean", "LukewarmOcean", 0.5, 0.5, -1, 0.1, color.RGBA{R: 0x0d, G: 0x96, B: 0xdb, A: 0xa5}, []string{"lukewarm", "monster", "ocean", "overworld", "spawns_warm_variant_farm_animals", "fast_fishing", "high_seas"}},
	{43, "deep_lukewarm_ocean", "DeepLukewarmOcean", 0.5, 0.5, -1.8, 0.1, color.RGBA{R: 0x0d, G: 0x96, B: 0xdb, A: 0xa5}, []string{"deep", "lukewarm", "monster", "ocean", "overworld", "spawns_warm_variant_farm_animals", "fast_fishing", "high_seas"}},
	{44, "cold_ocean", "ColdOcean", 0.5, 0.5, -1, 0.1, color.RGBA{R: 0x20, G: 0x80, B: 0xc9, A: 0xa5}, []string{"cold", "monster", "ocean", "overworld", "spawns_cold_variant_farm_animals", "fast_fishing", "high_seas"}},
	{45, "deep_cold_ocean", "DeepColdOcean", 0.5, 0.5, -1.8, 0.1, color.RGBA{R: 0x20, G: 0x80, B: 0xc9, A: 0xa5}, []string{"cold", "deep", "monster", "ocean", "overworld", "spawns_cold_variant_farm_animals", "fast_fishing", "high_seas"}},
	{46, "frozen_ocean", "FrozenOcean", 0, 0.5, -1, 0.1, color.RGBA{R: 0x25, G: 0x70, B: 0xb5, A: 0xa5}, []string{"frozen", "monster", "ocean", "overworld", "spawns_polar_bears_on_alternate_blocks", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits", "fast_fishing", "high_seas"}},
	{47, "deep_frozen_ocean", "DeepFrozenOcean", 0.5, 0.5, -1.8, 0.1, color.RGBA{R: 0x25, G: 0x70, B: 0xb5, A: 0xa5}, []string{"deep", "frozen", "monster", "ocean", "overworld", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_polar_bears_on_alternate_blocks", "fast_fishing", "high_seas"}},
	{48, "bamboo_jungle", "BambooJungle", 0.95, 0.9, 0.1, 0.2, color.RGBA{R: 0x14, G: 0xa2, B: 0xc5, A: 0xa5}, []string{"animal", "bamboo", "jungle", "monster", "overworld", "spawns_jungle_mobs", "spawns_warm_variant_farm_animals"}},
	{49, "bamboo_jungle_hills", "BambooJungleHills", 0.95, 0.9, 0.45, 0.3, color.RGBA{R: 0x1b, G: 0x9e, B: 0xd8, A: 0xa5}, []string{"animal", "bamboo", "hills", "jungle", "monster", "overworld", "spawns_jungle_mobs", "spawns_warm_variant_farm_animals"}},
	{129, "sunflower_plains", "SunflowerPlains", 0.8, 0.4, 0.125, 0.05, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"animal", "monster", "mutated", "overworld", "plains", "bee_habitat"}},
	{130, "desert_mutated", "DesertLakes", 2, 0, 0.225, 0.25, color.RGBA{R: 0x32, G: 0xa5, B: 0x98, A: 0xa5}, []string{"desert", "monster", "mutated", "overworld_generation", "spawns_gold_rabbits", "spawns_warm_variant_farm_animals"}},
	{131, "extreme_hills_mutated", "WindsweptGravellyHills", 0.2, 0.3, 1, 0.5, color.RGBA{R: 0x0e, G: 0x63, B: 0xab, A: 0xa5}, []string{"animal", "extreme_hills", "monster", "mutated", "overworld", "spawns_cold_variant_farm_animals"}},
	{132, "flower_forest", "FlowerForest", 0.7, 0.8, 0.1, 0.4, color.RGBA{R: 0x20, G: 0xa3, B: 0xcc, A: 0xa5}, []string{"animal", "flower_forest", "monster", "mutated", "overworld", "bee_habitat"}},
	{133, "taiga_mutated", "TaigaMountains", 0.25, 0.8, 0.2, 0.4, color.RGBA{R: 0x1e, G: 0x6b, B: 0x82, A: 0xa5}, []string{"animal", "forest", "monster", "mutated", "taiga", "overworld_generation", "spawns_cold_variant_farm_animals"}},
	{134, "swampland_mutated", "SwampHills", 0.8, 0.5, -0.1, 0.3, color.RGBA{R: 0x61, G: 0x7b, B: 0x64, A: 0xa5}, []string{"animal", "monster", "mutated", "swamp", "overworld_generation", "spawns_slimes_on_surface", "slime", "swamp_water_huge_mushroom"}},
	{140, "ice_plains_spikes", "IceSpikes", 0, 1, 0.425, 0.45, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"frozen", "ice_plains", "monster", "mutated", "overworld", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_white_rabbits"}},
	{149, "jungle_mutated", "ModifiedJungle", 0.95, 0.9, 0.2, 0.4, color.RGBA{R: 0x1b, G: 0x9e, B: 0xd8, A: 0xa5}, []string{"animal", "jungle", "monster", "mutated", "overworld_generation", "spawns_warm_variant_farm_animals"}},
	{151, "jungle_edge_mutated", "ModifiedJungleEdge", 0.95, 0.8, 0.2, 0.4, color.RGBA{R: 0x0d, G: 0x8a, B: 0xe3, A: 0xa5}, []string{"animal", "edge", "jungle", "monster", "mutated", "overworld_generation", "spawns_jungle_mobs", "spawns_warm_variant_farm_animals"}},
	{155, "birch_forest_mutated", "OldGrowthBirchForest", 0.6, 0.6, 0.2, 0.4, color.RGBA{R: 0x06, G: 0x77, B: 0xce, A: 0xa5}, []string{"animal", "birch", "forest", "monster", "mutated", "bee_habitat", "overworld_generation", "has_structure_trail_ruins"}},
	{156, "birch_forest_hills_mutated", "TallBirchHills", 0.7, 0.8, 0.55, 0.5, color.RGBA{R: 0x0a, G: 0x74, B: 0xc4, A: 0xa5}, []string{"animal", "birch", "forest", "hills", "monster", "mutated", "overworld_generation"}},
	{157, "roofed_forest_mutated", "DarkForestHills", 0.7, 0.8, 0.2, 0.4, color.RGBA{R: 0x3b, G: 0x6c, B: 0xd1, A: 0xa5}, []string{"animal", "forest", "monster", "mutated", "roofed", "overworld_generation"}},
	{158, "cold_taiga_mutated", "SnowyTaigaMountains", -0.5, 0.4, 0.3, 0.4, color.RGBA{R: 0x20, G: 0x5e, B: 0x83, A: 0xa5}, []string{"animal", "cold", "forest", "monster", "mutated", "taiga", "overworld_generation", "spawns_cold_variant_farm_animals"}},
	{160, "redwood_taiga_mutated", "OldGrowthSpruceTaiga", 0.25, 0.8, 0.2, 0.2, color.RGBA{R: 0x2d, G: 0x6d, B: 0x77, A: 0xa5}, []string{"animal", "forest", "mega", "monster", "mutated", "overworld", "taiga", "has_structure_trail_ruins", "spawns_cold_variant_farm_animals"}},
	{161, "redwood_taiga_hills_mutated", "GiantSpruceTaigaHills", 0.3, 0.8, 0.55, 0.5, color.RGBA{R: 0x28, G: 0x63, B: 0x78, A: 0xa5}, []string{"animal", "forest", "hills", "mega", "monster", "mutated", "taiga", "overworld_generation", "spawns_cold_variant_farm_animals"}},
	{162, "extreme_hills_plus_trees_mutated", "GravellyMountainsPlus", 0.2, 0.3, 1, 0.5, color.RGBA{R: 0x0e, G: 0x63, B: 0xab, A: 0xa5}, []string{"animal", "extreme_hills", "forest", "monster", "mutated", "overworld", "spawns_cold_variant_farm_animals"}},
	{163, "savanna_mutated", "WindsweptSavanna", 2, 0, 0.363, 1.225, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"animal", "monster", "mutated", "overworld", "savanna", "spawns_savanna_mobs", "spawns_warm_variant_farm_animals"}},
	{164, "savanna_plateau_mutated", "ShatteredSavannaPlateau", 1, 0.5, 1.05, 1.212, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"animal", "monster", "mutated", "overworld", "plateau", "savanna", "spawns_savanna_mobs", "spawns_warm_variant_farm_animals"}},
	{165, "mesa_bryce", "ErodedBadlands", 2, 0, 0.1, 0.2, color.RGBA{R: 0x14, G: 0xa2, B: 0xc5, A: 0xa5}, []string{"animal", "mesa", "monster", "mutated", "overworld", "spawns_mesa_mobs", "spawns_warm_variant_farm_animals", "surface_mineshaft"}},
	{166, "mesa_plateau_stone_mutated", "ModifiedWoodedBadlandsPlateau", 2, 0, 0.45, 0.3, color.RGBA{R: 0x55, G: 0x80, B: 0x9e, A: 0xa5}, []string{"animal", "mesa", "monster", "mutated", "overworld", "plateau", "spawns_mesa_mobs", "spawns_warm_variant_farm_animals", "surface_mineshaft"}},
	{167, "mesa_plateau_mutated", "ModifiedBadlandsPlateau", 2, 0, 0.45, 0.3, color.RGBA{R: 0x55, G: 0x80, B: 0x9e, A: 0xa5}, []string{"animal", "mesa", "monster", "mutated", "overworld", "plateau", "stone", "spawns_mesa_mobs", "spawns_warm_variant_farm_animals", "surface_mineshaft"}},
	{178, "soulsand_valley", "SoulSandValley", 2, 0, 0.1, 0.2, color.RGBA{R: 0x90, G: 0x59, B: 0x57, A: 0xa5}, []string{"nether", "soulsand_valley", "spawn_ghast", "spawn_endermen", "spawns_warm_variant_farm_animals"}},
	{179, "crimson_forest", "CrimsonForest", 2, 0, 0.1, 0.2, color.RGBA{R: 0x90, G: 0x59, B: 0x57, A: 0xa5}, []string{"nether", "netherwart_forest", "crimson_forest", "spawn_few_zombified_piglins", "spawn_piglin", "spawns_warm_variant_farm_animals"}},
	{180, "warped_forest", "WarpedForest", 2, 0, 0.1, 0.2, color.RGBA{R: 0x90, G: 0x59, B: 0x57, A: 0xa5}, []string{"nether", "netherwart_forest", "warped_forest", "spawn_endermen", "spawns_warm_variant_farm_animals"}},
	{181, "basalt_deltas", "BasaltDeltas", 2, 0, 0.1, 0.2, color.RGBA{R: 0x3f, G: 0x76, B: 0xe4, A: 0xa5}, []string{"nether", "basalt_deltas", "spawn_many_magma_cubes", "spawn_ghast", "spawns_warm_variant_farm_animals"}},
	{182, "jagged_peaks", "JaggedPeaks", -0.7, 0.9, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "monster", "overworld", "frozen", "jagged_peaks", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{183, "frozen_peaks", "FrozenPeaks", -0.7, 0.9, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "monster", "overworld", "frozen", "frozen_peaks", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{184, "snowy_slopes", "SnowySlopes", -0.3, 0.9, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "monster", "overworld", "frozen", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits", "snowy_slopes", "spawns_cold_variant_farm_animals"}},
	{185, "grove", "Grove", -0.2, 0.8, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "cold", "monster", "overworld", "grove", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs", "spawns_snow_foxes", "spawns_white_rabbits"}},
	{186, "meadow", "Meadow", 0.3, 0.8, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "monster", "overworld", "meadow", "bee_habitat"}},
	{187, "lush_caves", "LushCaves", 0.9, 0, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"caves", "lush_caves", "overworld", "monster", "spawns_tropical_fish_at_any_height"}},
	{188, "dripstone_caves", "DripstoneCaves", 0.2, 0, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"caves", "overworld", "dripstone_caves", "monster"}},
	{189, "stony_peaks", "StonyPeaks", 1, 0.3, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "monster", "overworld", "spawns_cold_variant_farm_animals"}},
	{190, "deep_dark", "DeepDark", 0.8, 0.4, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"caves", "deep_dark", "overworld", "spawns_cold_variant_farm_animals", "spawns_cold_variant_frogs"}},
	{191, "mangrove_swamp", "MangroveSwamp", 0.8, 0.9, -0.2, 0.1, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mangrove_swamp", "overworld", "monster", "spawns_slimes_on_surface", "spawns_warm_variant_farm_animals", "spawns_warm_variant_frogs", "slime"}},
	{192, "cherry_grove", "CherryGrove", 0.3, 0.8, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"mountains", "monster", "overworld", "cherry_grove", "bee_habitat"}},
	{193, "pale_garden", "PaleGarden", 0.7, 0.8, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"monster", "overworld", "pale_garden"}},
	{194, "sulfur_caves", "SulfurCaves", 0.8, 0.4, 0.1, 0.2, color.RGBA{R: 0x60, G: 0xb7, B: 0xff, A: 0xa6}, []string{"caves", "sulfur_caves", "overworld", "monster"}},
}

// goldenCount is stated separately from len(golden) on purpose: a deleted table
// row would otherwise shrink the expectation along with the registry and pass
// silently.
const goldenCount = 88

// highestID is the largest registered ID. finaliseBiomeRegistry stores it in
// world.maxVanillaBiomeID, which decides whether BiomeDefinitions sends a real
// BiomeID or -1 for a biome.
const highestID = 194

func TestGoldenTableIsWellFormed(t *testing.T) {
	if len(golden) != goldenCount {
		t.Fatalf("golden holds %d entries, want %d", len(golden), goldenCount)
	}
	ids, names := map[int]string{}, map[string]int{}
	for _, g := range golden {
		if prev, ok := ids[g.id]; ok {
			t.Fatalf("golden lists ID %d twice: %s and %s", g.id, prev, g.name)
		}
		if prev, ok := names[g.name]; ok {
			t.Fatalf("golden lists name %q twice: IDs %d and %d", g.name, prev, g.id)
		}
		ids[g.id], names[g.name] = g.name, g.id
	}
}

func TestEveryGoldenBiomeResolvesByIDAndByName(t *testing.T) {
	// The full round trip in both directions, one biome at a time. Swapping the
	// EncodeBiome of any two biomes breaks this at the type-name assertion even
	// though every ID would still resolve to some biome.
	for _, g := range golden {
		byID, ok := world.BiomeByID(g.id)
		if !ok {
			t.Fatalf("BiomeByID(%d) = _, false, want %s", g.id, g.name)
		}
		if got := typeName(byID); got != g.typ {
			t.Fatalf("BiomeByID(%d) is biome.%s, want biome.%s", g.id, got, g.typ)
		}
		if got := byID.String(); got != g.name {
			t.Fatalf("BiomeByID(%d).String() = %q, want %q", g.id, got, g.name)
		}
		byName, ok := world.BiomeByName(g.name)
		if !ok {
			t.Fatalf("BiomeByName(%q) = _, false, want ID %d", g.name, g.id)
		}
		if got := typeName(byName); got != g.typ {
			t.Fatalf("BiomeByName(%q) is biome.%s, want biome.%s", g.name, got, g.typ)
		}
		if got := byName.EncodeBiome(); got != g.id {
			t.Fatalf("BiomeByName(%q).EncodeBiome() = %d, want %d", g.name, got, g.id)
		}
	}
}

func TestRegistryHoldsNothingBeyondGolden(t *testing.T) {
	// Direction the previous test cannot cover: a biome registered but absent
	// from the table.
	all := world.Biomes()
	if len(all) != goldenCount {
		t.Fatalf("world.Biomes() returned %d biomes, want %d", len(all), goldenCount)
	}
	known := map[int]string{}
	for _, g := range golden {
		known[g.id] = g.typ
	}
	for _, b := range all {
		typ, ok := known[b.EncodeBiome()]
		if !ok {
			t.Fatalf("registry holds unlisted biome %s with ID %d", typeName(b), b.EncodeBiome())
		}
		if got := typeName(b); got != typ {
			t.Fatalf("ID %d is biome.%s in the registry, biome.%s in golden", b.EncodeBiome(), got, typ)
		}
	}
}

func TestGoldenValuesMatchRegistry(t *testing.T) {
	for _, g := range golden {
		b, ok := world.BiomeByID(g.id)
		if !ok {
			t.Fatalf("BiomeByID(%d) = _, false", g.id)
		}
		if got := b.Temperature(); got != g.temp {
			t.Errorf("%s.Temperature() = %v, want %v", g.name, got, g.temp)
		}
		if got := b.Rainfall(); got != g.rain {
			t.Errorf("%s.Rainfall() = %v, want %v", g.name, got, g.rain)
		}
		if got := b.Depth(); got != g.depth {
			t.Errorf("%s.Depth() = %v, want %v", g.name, got, g.depth)
		}
		if got := b.Scale(); got != g.scale {
			t.Errorf("%s.Scale() = %v, want %v", g.name, got, g.scale)
		}
		if got := b.WaterColour(); got != g.water {
			t.Errorf("%s.WaterColour() = %#v, want %#v", g.name, got, g.water)
		}
		if got := b.Tags(); !slices.Equal(got, g.tags) {
			t.Errorf("%s.Tags() = %q, want %q", g.name, got, g.tags)
		}
	}
}

// TestHighestRegisteredID pins the input to finaliseBiomeRegistry. A biome
// registered above this ID would be sent to the client with a real BiomeID
// instead of -1, which is a different packet shape.
func TestHighestRegisteredID(t *testing.T) {
	high := math.MinInt
	for _, b := range world.Biomes() {
		high = max(high, b.EncodeBiome())
	}
	if high != highestID {
		t.Fatalf("highest registered biome ID is %d, want %d", high, highestID)
	}
}

var nameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// TestRangeInvariants covers all 88 biomes in one sweep. The bounds are the
// observed extremes, not vanilla's documented ranges: they exist to catch a
// value drifting outside the set the registry has ever held, not to validate
// the values against Minecraft.
func TestRangeInvariants(t *testing.T) {
	for _, b := range world.Biomes() {
		name := b.String()
		if !nameRe.MatchString(name) {
			t.Errorf("biome %d has name %q, want lower snake case", b.EncodeBiome(), name)
		}
		// world.unknownBiome, the value substituted for an unregistered biome,
		// reports this name. A registered biome taking it would make the two
		// indistinguishable.
		if name == "unknown" {
			t.Errorf("biome %d is named %q, colliding with the unknown-biome fallback", b.EncodeBiome(), name)
		}
		if temp := b.Temperature(); temp < -0.7 || temp > 2 {
			t.Errorf("%s.Temperature() = %v, outside [-0.7, 2]", name, temp)
		}
		if rain := b.Rainfall(); rain < 0 || rain > 1 {
			t.Errorf("%s.Rainfall() = %v, outside [0, 1]", name, rain)
		}
		if depth := b.Depth(); depth < -1.8 || depth > 1.5 {
			t.Errorf("%s.Depth() = %v, outside [-1.8, 1.5]", name, depth)
		}
		if scale := b.Scale(); scale < 0 || scale > 1.225 {
			t.Errorf("%s.Scale() = %v, outside [0, 1.225]", name, scale)
		}
		if a := b.WaterColour().A; a != 0xa5 && a != 0xa6 {
			t.Errorf("%s.WaterColour() has alpha 0x%02x, want 0xa5 or 0xa6", name, a)
		}
		tags := b.Tags()
		if len(tags) == 0 {
			t.Errorf("%s.Tags() is empty", name)
		}
		seen := map[string]bool{}
		for _, tag := range tags {
			if !nameRe.MatchString(tag) {
				t.Errorf("%s has tag %q, want lower snake case", name, tag)
			}
			if seen[tag] {
				t.Errorf("%s lists tag %q twice", name, tag)
			}
			seen[tag] = true
		}
	}
}

// TestTagsAreNotSharedBetweenCalls checks the aliasing question CLAUDE.md 3.4
// asks of every returned slice. Each Tags implementation builds a fresh literal
// per call, so a caller that sorts or truncates the result cannot corrupt the
// registry for everyone else.
func TestTagsAreNotSharedBetweenCalls(t *testing.T) {
	for _, b := range world.Biomes() {
		first := b.Tags()
		want := slices.Clone(first)
		for i := range first {
			first[i] = "clobbered"
		}
		if second := b.Tags(); !slices.Equal(second, want) {
			t.Fatalf("%s.Tags() returned %q after the previous result was overwritten, want %q", b.String(), second, want)
		}
	}
}

// TestUnregisteredLookupsReturnFalse pins the CLAUDE.md 2.2 behaviour of the
// two lookups: they report failure as a value rather than panicking, and the
// biome they return alongside false is nil, not the unknown-biome fallback.
func TestUnregisteredLookupsReturnFalse(t *testing.T) {
	for _, id := range []int{-1, math.MinInt, 50, 128, 195, highestID + 1, math.MaxInt} {
		if b, ok := world.BiomeByID(id); ok {
			t.Errorf("BiomeByID(%d) = %s, true, want _, false", id, typeName(b))
		} else if b != nil {
			t.Errorf("BiomeByID(%d) returned %v alongside false, want nil", id, b)
		}
	}
	for _, name := range []string{"", "unknown", "Plains", "plains ", "minecraft:plains", "nonexistent"} {
		if b, ok := world.BiomeByName(name); ok {
			t.Errorf("BiomeByName(%q) = %s, true, want _, false", name, typeName(b))
		} else if b != nil {
			t.Errorf("BiomeByName(%q) returned %v alongside false, want nil", name, b)
		}
	}
}

// PINNED BUG: RegisterBiome panics on a duplicate ID. The caller of a registry
// is exactly the third party CLAUDE.md 2.2 says must not be able to take the
// process down, and "this ID is taken" is a condition a caller can act on.
// Pinned as-is; decide in stage 1.
func TestDuplicateRegistrationPanics(t *testing.T) {
	// Ocean is already registered under ID 0, so this panics before it writes
	// anything and leaves the global registry untouched.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("re-registering Ocean did not panic")
		}
		if got, want := fmt.Sprint(r), "cannot register the same biome (ocean) twice"; got != want {
			t.Fatalf("panic value = %q, want %q", got, want)
		}
	}()
	world.RegisterBiome(Ocean{})
}

// TestBiomeDefinitionsMatchRegistry pins the network projection of every biome.
// The definition list and its interned string list are what the client is told
// a biome is, so each field is checked against the registry entry it came from.
func TestBiomeDefinitionsMatchRegistry(t *testing.T) {
	defs, strs := world.BiomeDefinitions()
	if len(defs) != goldenCount {
		t.Fatalf("BiomeDefinitions returned %d definitions, want %d", len(defs), goldenCount)
	}
	seen := map[string]bool{}
	for _, def := range defs {
		if def.NameIndex < 0 || int(def.NameIndex) >= len(strs) {
			t.Fatalf("NameIndex %d is outside the %d-entry string list", def.NameIndex, len(strs))
		}
		name := strs[def.NameIndex]
		if seen[name] {
			t.Fatalf("biome %q has two definitions", name)
		}
		seen[name] = true

		b, ok := world.BiomeByName(name)
		if !ok {
			t.Fatalf("definition names %q, which is not a registered biome", name)
		}
		// Every registered biome sits at or below maxVanillaBiomeID, so the
		// `id > maxVanillaBiomeID` branch never fires and the client is told -1
		// for all of them.
		if def.BiomeID != -1 {
			t.Errorf("%s has BiomeID %d, want -1", name, def.BiomeID)
		}
		if got, want := def.Temperature, float32(b.Temperature()); got != want {
			t.Errorf("%s Temperature = %v, want %v", name, got, want)
		}
		if got, want := def.Downfall, float32(b.Rainfall()); got != want {
			t.Errorf("%s Downfall = %v, want %v", name, got, want)
		}
		if got, want := def.Depth, float32(b.Depth()); got != want {
			t.Errorf("%s Depth = %v, want %v", name, got, want)
		}
		if got, want := def.Scale, float32(b.Scale()); got != want {
			t.Errorf("%s Scale = %v, want %v", name, got, want)
		}
		if got, want := def.Rain, b.Rainfall() > 0; got != want {
			t.Errorf("%s Rain = %v, want %v", name, got, want)
		}
		// The colour is packed ARGB and then reinterpreted as a signed int32,
		// so every biome with alpha 0xa5 or 0xa6 sends a negative value.
		c := b.WaterColour()
		want := int32(binary.BigEndian.Uint32([]byte{c.A, c.R, c.G, c.B}))
		if def.MapWaterColour != want {
			t.Errorf("%s MapWaterColour = %d, want %d", name, def.MapWaterColour, want)
		}
		if def.MapWaterColour >= 0 {
			t.Errorf("%s MapWaterColour = %d, expected the sign bit set by the 0xa5/0xa6 alpha", name, def.MapWaterColour)
		}
		// PINNED SCOPE: FoliageSnow and ChunkGeneration are never populated.
		// The comment on ChunkGeneration says a server should leave it empty;
		// FoliageSnow has no source in the Biome interface at all.
		if def.FoliageSnow != 0 {
			t.Errorf("%s FoliageSnow = %v, want 0", name, def.FoliageSnow)
		}
		if _, ok := def.ChunkGeneration.Value(); ok {
			t.Errorf("%s carries ChunkGeneration, want it absent", name)
		}

		idx, ok := def.Tags.Value()
		if !ok {
			t.Fatalf("%s carries no Tags option", name)
		}
		tags := b.Tags()
		if len(idx) != len(tags) {
			t.Fatalf("%s has %d tag indices, want %d", name, len(idx), len(tags))
		}
		for i, at := range idx {
			if int(at) >= len(strs) {
				t.Fatalf("%s tag index %d is outside the %d-entry string list", name, at, len(strs))
			}
			if strs[at] != tags[i] {
				t.Errorf("%s tag %d resolves to %q, want %q", name, i, strs[at], tags[i])
			}
		}
	}
	if len(seen) != goldenCount {
		t.Fatalf("definitions covered %d distinct biomes, want %d", len(seen), goldenCount)
	}
}

// TestBiomeDefinitionsInternsEveryStringOnce pins the interning contract: the
// string list holds exactly the distinct biome names and tag names, with no
// repeats and no unreferenced entries.
func TestBiomeDefinitionsInternsEveryStringOnce(t *testing.T) {
	_, strs := world.BiomeDefinitions()
	want := map[string]bool{}
	for _, b := range world.Biomes() {
		want[b.String()] = true
		for _, tag := range b.Tags() {
			want[tag] = true
		}
	}
	if len(strs) != len(want) {
		t.Fatalf("string list holds %d entries, want %d distinct names and tags", len(strs), len(want))
	}
	seen := map[string]bool{}
	for _, s := range strs {
		if seen[s] {
			t.Errorf("string list holds %q twice", s)
		}
		seen[s] = true
		if !want[s] {
			t.Errorf("string list holds %q, which is neither a biome name nor a tag", s)
		}
	}
}

// PINNED BUG: BiomeDefinitions iterates a Go map, so both the definition order
// and the interning order of the string list differ from run to run. The packet
// stays self-consistent because indices are resolved against the list it ships
// with, but two servers built from identical code send byte-different packets
// and the output cannot be diffed or cached. Pinned as-is; decide in stage 1.
//
// The assertion is a statement about the current behaviour, so it fails once
// the order is made deterministic. With 88 entries the odds of eight
// consecutive calls agreeing by chance are nil.
func TestBiomeDefinitionsOrderIsNondeterministic(t *testing.T) {
	first, firstStrs := world.BiomeDefinitions()
	defsVary, strsVary := false, false
	for range 8 {
		defs, strs := world.BiomeDefinitions()
		if !slices.Equal(order(defs, strs), order(first, firstStrs)) {
			defsVary = true
		}
		if !slices.Equal(strs, firstStrs) {
			strsVary = true
		}
	}
	if !defsVary {
		t.Error("BiomeDefinitions returned definitions in a stable order; the map-iteration nondeterminism this pins is gone")
	}
	if !strsVary {
		t.Error("BiomeDefinitions returned a stable string list; the map-iteration nondeterminism this pins is gone")
	}
}

// order resolves the biome names of defs through their own string list so that
// two calls can be compared despite carrying different indices.
func order(defs []protocol.BiomeDefinition, strs []string) []string {
	names := make([]string, len(defs))
	for i, def := range defs {
		names[i] = strs[def.NameIndex]
	}
	return names
}

// typeName is the Go type of b without the package qualifier.
func typeName(b world.Biome) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", b), "biome.")
}
