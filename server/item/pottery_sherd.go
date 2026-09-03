package item

// PotterySherd is an item that can be found from brushing suspicious sand or gravel.
type PotterySherd struct {
	Type SherdType
}

// potterySherdNames holds the encoded name of every vanilla sherd type,
// indexed by the value of the type itself. Stack.Empty reaches EncodeItem on
// nearly every method of Stack, and the concatenation below allocated a
// string on each of those calls. A type outside the table falls back to that
// concatenation, so an unknown one still panics in String as before.
var potterySherdNames = sherdNames()

// sherdNames builds the potterySherdNames table.
func sherdNames() []string {
	types := SherdTypes()
	names := make([]string, len(types))
	for _, t := range types {
		names[t.Uint8()] = "minecraft:" + t.String() + "_pottery_sherd"
	}
	return names
}

// EncodeItem ...
func (s PotterySherd) EncodeItem() (name string, meta int16) {
	if i := int(s.Type.Uint8()); i < len(potterySherdNames) {
		return potterySherdNames[i], 0
	}
	return "minecraft:" + s.Type.String() + "_pottery_sherd", 0
}

// PotDecoration ...
func (PotterySherd) PotDecoration() bool {
	return true
}
