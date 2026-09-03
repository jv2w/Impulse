package dialogue

import "encoding/json"

// Button represents a button added to a dialogue menu and consists of just
// text.
type Button struct {
	// Text holds the text displayed on the button. It may use Minecraft
	// formatting codes.
	Text string
}

// MarshalJSON ...
func (b Button) MarshalJSON() ([]byte, error) {
	// encoding/json writes map keys in ascending order, so a struct with the
	// fields declared in that order writes the same bytes without the
	// temporary map and its four boxed values.
	return json.Marshal(buttonJSON{
		ButtonName: b.Text,
		Mode:       0, // "Click" activation
		Text:       "",
		Type:       1, // "Command" type
	})
}

// buttonJSON is the JSON shape of a Button. The field order matches the
// ascending key order encoding/json used for the map it replaces.
type buttonJSON struct {
	ButtonName string `json:"button_name"`
	Mode       int    `json:"mode"`
	Text       string `json:"text"`
	Type       int    `json:"type"`
}
