package item

// SmithingTemplate is an item used in smithing tables to alter tools and
// armour. They are consumed when used, but can be duplicated using an existing
// template, its material and diamonds.
type SmithingTemplate struct {
	// Template the upgrade item used in smithing tables.
	Template SmithingTemplateType
}

// smithingTemplateNames holds the encoded name of every vanilla template,
// indexed by the value of the template itself. Stack.Empty reaches EncodeItem
// on nearly every method of Stack, and the concatenation below allocated a
// string on each of those calls. A template outside the table falls back to
// that concatenation, so an unknown one still panics in String as before.
var smithingTemplateNames = templateNames()

// templateNames builds the smithingTemplateNames table.
func templateNames() []string {
	templates := SmithingTemplates()
	names := make([]string, len(templates))
	for _, t := range templates {
		if t == TemplateNetheriteUpgrade() {
			names[t.smithingTemplateType] = "minecraft:netherite_upgrade_smithing_template"
			continue
		}
		names[t.smithingTemplateType] = "minecraft:" + t.String() + "_armor_trim_smithing_template"
	}
	return names
}

// EncodeItem ...
func (t SmithingTemplate) EncodeItem() (name string, meta int16) {
	// Only a template outside the table can reach the tail, and the netherite
	// upgrade is inside it, so the tail is the trim branch alone.
	if i := int(t.Template.smithingTemplateType); i < len(smithingTemplateNames) {
		return smithingTemplateNames[i], 0
	}
	return "minecraft:" + t.Template.String() + "_armor_trim_smithing_template", 0
}
