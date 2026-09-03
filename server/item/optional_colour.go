package item

// OptionalColour represents a Colour that may be absent. It is used by items
// and blocks that have one variant per Colour plus an additional uncoloured
// variant (e.g. shulker boxes, where the uncoloured form is encoded as
// minecraft:undyed_shulker_box). A zero OptionalColour denotes the absent
// case; values 1..16 map to the 16 dye Colours.
type OptionalColour uint8

// colours is a slice of all Colours.
var colours = Colours()

// NewOptionalColour returns a new OptionalColour from a Colour.
func NewOptionalColour(c Colour) OptionalColour {
	return OptionalColour(c.Uint8() + 1)
}

// Colour returns the Colour of the OptionalColour, and a bool indicating
// whether the Colour is present.
func (oc OptionalColour) Colour() (Colour, bool) {
	if oc == 0 {
		return Colour{}, false
	}
	return colours[(oc - 1)], true
}

// Uint8 returns the OptionalColour as a uint8.
func (oc OptionalColour) Uint8() uint8 {
	return uint8(oc)
}

// colourPrefixes holds the "<colour>_" prefix that Prepend puts in front of a
// string, one per Colour in colours. Colour.String returns a constant per
// colour, so the prefix built once here is the one the concatenation built on
// every call.
var colourPrefixes = colourPrefixTable()

// colourPrefixTable builds the table above.
func colourPrefixTable() []string {
	prefixes := make([]string, len(colours))
	for i, c := range colours {
		prefixes[i] = c.String() + "_"
	}
	return prefixes
}

// Prepend prepends the Colour to the string if the Colour is present.
func (oc OptionalColour) Prepend(str string) string {
	if oc != 0 {
		// One join of two strings instead of three, and the colour's name is no
		// longer looked up through String on each call. The index is the same
		// one, so an out of range OptionalColour still panics here.
		return colourPrefixes[oc-1] + str
	}
	return str
}

// OptionalColours returns a slice of all OptionalColours, including the absent case.
func OptionalColours() []OptionalColour {
	optionalColours := make([]OptionalColour, 17)
	for i, c := range colours {
		optionalColours[i+1] = NewOptionalColour(c)
	}
	return optionalColours
}
