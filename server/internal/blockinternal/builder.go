package blockinternal

import (
	"github.com/jv2w/Impulse/server/item/category"
	"maps"
	"slices"
)

// ComponentBuilder represents a builder that can be used to construct a block components map to be sent to a client.
type ComponentBuilder struct {
	permutations map[string]map[string]any
	properties   []map[string]any
	components   map[string]any
	blockID      int32

	identifier   string
	menuCategory category.Category
}

// NewComponentBuilder returns a new component builder with the provided block data, using the provided components map
// as a base.
func NewComponentBuilder(identifier string, components map[string]any, blockID int32) *ComponentBuilder {
	if components == nil {
		components = map[string]any{}
	}
	return &ComponentBuilder{
		permutations: make(map[string]map[string]any),
		components:   components,
		blockID:      blockID,

		identifier:   identifier,
		menuCategory: category.Construction(),
	}
}

// AddProperty adds the provided block property to the builder.
func (builder *ComponentBuilder) AddProperty(name string, values []any) {
	builder.properties = append(builder.properties, map[string]any{
		"name": name,
		"enum": values,
	})
}

// AddComponent adds the provided component to the builder. If the component already exists, it will be overwritten.
func (builder *ComponentBuilder) AddComponent(name string, value any) {
	builder.components[name] = value
}

// AddPermutation adds a permutation to the builder. If there is already an existing permutation for the provided
// condition, the new components will be added to the existing permutation.
func (builder *ComponentBuilder) AddPermutation(condition string, components map[string]any) {
	if len(builder.permutations) == 0 {
		// This trigger really does not matter at all, the component just needs to be set for custom block placements to
		// function as expected client-side, when permutations are applied.
		builder.AddComponent("minecraft:on_player_placing", map[string]any{
			"triggerType": "placement_trigger",
		})
	}
	// The condition was looked up again on every component; the same map is
	// now held in a local, so the writes land in the same map in the same
	// order.
	perm := builder.permutations[condition]
	if perm == nil {
		perm = map[string]any{}
		builder.permutations[condition] = perm
	}
	for key, value := range components {
		perm[key] = value
	}
}

// SetMenuCategory sets the creative category for the current block.
func (builder *ComponentBuilder) SetMenuCategory(category category.Category) {
	builder.menuCategory = category
}

// Construct constructs the final block components map that is ready to be sent to the client.
func (builder *ComponentBuilder) Construct() map[string]any {
	components := maps.Clone(builder.components)

	result := map[string]any{
		"components":    components,
		"molangVersion": int32(10),
		"menu_category": map[string]any{
			"category": builder.menuCategory.String(),
			"group":    builder.menuCategory.Group(),
		},
		"vanilla_block_data": map[string]any{
			"block_id": builder.blockID,
		},
	}
	// The clone has the same length as the source, so it is only made when it
	// is going to be stored.
	if len(builder.properties) > 0 {
		result["properties"] = slices.Clone(builder.properties)
	}

	// The permutation map was cloned only to be ranged over, and the list was
	// read back out of result on every append. Ranging the source yields the
	// same pairs with the same shared value maps, and the list is built once
	// at its final length.
	if len(builder.permutations) > 0 {
		permutations := make([]map[string]any, 0, len(builder.permutations))
		for condition, values := range builder.permutations {
			permutations = append(permutations, map[string]any{
				"condition":  condition,
				"components": values,
			})
		}
		result["permutations"] = permutations
	}
	return result
}
