package world

import (
	"encoding/binary"
	"slices"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

var (
	// maxVanillaBiomeID is the highest ID used by vanilla biomes.
	maxVanillaBiomeID int
)

// finaliseBiomeRegistry is called after all vanilla biomes have been registered.
// It sets maxVanillaBiomeID to the highest ID found among them.
// noinspection GoUnusedFunction
//
//lint:ignore U1000 Function is used through compiler directives.
func finaliseBiomeRegistry() {
	for _, b := range biomes {
		id := b.EncodeBiome()
		if id > maxVanillaBiomeID {
			maxVanillaBiomeID = id
		}
	}
}

// BiomeDefinitions returns the list of biome definitions along with the associated StringList.
func BiomeDefinitions() ([]protocol.BiomeDefinition, []string) {
	var (
		internedStrings     []string
		internedStringIndex = make(map[string]int)
	)

	intern := func(s string) int {
		if index, exists := internedStringIndex[s]; exists {
			return index
		}
		index := len(internedStrings)
		internedStrings = append(internedStrings, s)
		internedStringIndex[s] = index
		return index
	}

	// The definitions are encoded in ID order so that the packet is identical
	// on every server built from the same code: iterating the registry map
	// directly would send the same entries in a different order each run.
	ids := make([]int, 0, len(biomes))
	for id := range biomes {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		intern(biomes[id].String())
	}

	encodedBiomes := make([]protocol.BiomeDefinition, 0, len(biomes))
	for _, id := range ids {
		b := biomes[id]
		nameIndex := intern(b.String())

		tags := b.Tags()
		tagIndices := make([]uint16, len(tags))
		for i, tag := range tags {
			tagIndices[i] = uint16(intern(tag))
		}

		var biomeID int16 = -1
		id := b.EncodeBiome()
		if id > maxVanillaBiomeID {
			biomeID = int16(id)
		}

		// The colour and the rainfall are each read once instead of four and two times. Nothing between the
		// reads can change what the biome returns, so the encoded values are the same.
		colour, rainfall := b.WaterColour(), b.Rainfall()
		def := protocol.BiomeDefinition{
			NameIndex:   int16(nameIndex),
			BiomeID:     biomeID,
			Temperature: float32(b.Temperature()),
			Downfall:    float32(rainfall),
			Depth:       float32(b.Depth()),
			Scale:       float32(b.Scale()),
			MapWaterColour: int32(binary.BigEndian.Uint32([]byte{
				colour.A,
				colour.R,
				colour.G,
				colour.B,
			})),
			Rain: rainfall > 0,
			Tags: protocol.Option[[]uint16](tagIndices),
		}

		encodedBiomes = append(encodedBiomes, def)
	}

	return encodedBiomes, internedStrings
}
