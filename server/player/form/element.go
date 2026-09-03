package form

import (
	"encoding/json"
	"strings"
)

// Element represents an element that may be added to a Form. Any of the types in this package that implement
// the element interface may be used as struct fields when passing the form structure to form.New().
type Element interface {
	json.Marshaler
	elem()
}

// MenuElement represents an element that may be added to a Menu form. This includes buttons, dividers,
// headers, and labels.
type MenuElement interface {
	json.Marshaler
	menuElem()
}

// Divider represents a visual separator element on a form. It displays a horizontal line.
type Divider struct{}

// MarshalJSON ...
func (d Divider) MarshalJSON() ([]byte, error) {
	// encoding/json writes map keys in ascending order, so a struct whose
	// fields are declared in that same order produces byte-identical output
	// without the temporary map and its boxed values.
	return json.Marshal(textElement{Text: "", Type: "divider"})
}

// Header represents a header element on a form. It displays larger, emphasised text for section titles.
type Header struct {
	// Text is the text held by the header. The text may contain Minecraft formatting codes.
	Text string
}

// NewHeader creates and returns a new Header with the text passed.
func NewHeader(text string) Header {
	return Header{Text: text}
}

// MarshalJSON ...
func (h Header) MarshalJSON() ([]byte, error) {
	// Same sorted key order as the map it replaces, so the bytes are identical.
	return json.Marshal(textElement{Text: h.Text, Type: "header"})
}

// Label represents a static label on a form. It serves only to display a box of text, and users cannot
// submit values to it.
type Label struct {
	// Text is the text held by the label. The text may contain Minecraft formatting codes.
	Text string
}

// NewLabel creates and returns a new Label with the values passed.
func NewLabel(text string) Label {
	return Label{Text: text}
}

// MarshalJSON ...
func (l Label) MarshalJSON() ([]byte, error) {
	// Same sorted key order as the map it replaces, so the bytes are identical.
	return json.Marshal(textElement{Text: l.Text, Type: "label"})
}

// Input represents a text input box element. Submitters may write any text in these boxes with no specific
// length.
type Input struct {
	// Text is the text displayed over the input element. The text may contain Minecraft formatting codes.
	Text string
	// Default is the default value filled out in the input. The user may remove this value and fill out its
	// own text. The text may contain Minecraft formatting codes.
	Default string
	// Placeholder is the text displayed in the input box if it does not contain any text filled out by the
	// user. The text may contain Minecraft formatting codes.
	Placeholder string
	// Tooltip is an optional text displayed when hovering over the element's info icon. The icon only
	// appears when a tooltip is set.
	Tooltip string

	value string
}

// NewInput creates and returns a new Input with the values passed.
func NewInput(text, defaultValue, placeholder string) Input {
	return Input{Text: text, Default: defaultValue, Placeholder: placeholder}
}

// WithTooltip returns a copy of the Input with the tooltip set.
func (i Input) WithTooltip(tooltip string) Input {
	i.Tooltip = tooltip
	return i
}

// MarshalJSON ...
func (i Input) MarshalJSON() ([]byte, error) {
	// Fields are declared in the sorted key order the map produced and
	// omitempty drops the tooltip on exactly the empty string the old
	// condition tested, so the bytes are identical.
	return json.Marshal(inputElement{
		Default:     i.Default,
		Placeholder: i.Placeholder,
		Text:        i.Text,
		Tooltip:     i.Tooltip,
		Type:        "input",
	})
}

// Value returns the value filled out by the user.
func (i Input) Value() string {
	return i.value
}

// Toggle represents an on-off button element. Submitters may either toggle this on or off, which will then
// hold a value of true or false respectively.
type Toggle struct {
	// Text is the text displayed over the toggle element. The text may contain Minecraft formatting codes.
	Text string
	// Default determines if the toggle should be on/off by default.
	Default bool
	// Tooltip is an optional text displayed when hovering over the element's info icon. The icon only
	// appears when a tooltip is set.
	Tooltip string

	value bool
}

// NewToggle creates and returns a new Toggle with the values passed.
func NewToggle(text string, defaultValue bool) Toggle {
	return Toggle{Text: text, Default: defaultValue}
}

// WithTooltip returns a copy of the Toggle with the tooltip set.
func (t Toggle) WithTooltip(tooltip string) Toggle {
	t.Tooltip = tooltip
	return t
}

// MarshalJSON ...
func (t Toggle) MarshalJSON() ([]byte, error) {
	// Sorted key order kept and omitempty matches the old tooltip condition.
	return json.Marshal(toggleElement{
		Default: t.Default,
		Text:    t.Text,
		Tooltip: t.Tooltip,
		Type:    "toggle",
	})
}

// Value returns the value filled out by the user.
func (t Toggle) Value() bool {
	return t.value
}

// Slider represents a slider element. Submitters may move the slider to values within the range of the slider
// to select a value.
type Slider struct {
	// Text is the text displayed over the slider element. The text may contain Minecraft formatting codes.
	Text string
	// Min and Max are used to specify the minimum and maximum range of the slider. A value lower or higher
	// than these values cannot be selected.
	Min, Max float64
	// StepSize is the size that one step of the slider takes up. When set to 1.0 for example, a submitter
	// will be able to select only whole values.
	StepSize float64
	// Default is the default value filled out for the slider.
	Default float64
	// Tooltip is an optional text displayed when hovering over the element's info icon. The icon only
	// appears when a tooltip is set.
	Tooltip string

	value float64
}

// NewSlider creates and returns a new Slider using the values passed.
func NewSlider(text string, min, max, stepSize, defaultValue float64) Slider {
	return Slider{Text: text, Min: min, Max: max, StepSize: stepSize, Default: defaultValue}
}

// WithTooltip returns a copy of the Slider with the tooltip set.
func (s Slider) WithTooltip(tooltip string) Slider {
	s.Tooltip = tooltip
	return s
}

// MarshalJSON ...
func (s Slider) MarshalJSON() ([]byte, error) {
	// Sorted key order kept and omitempty matches the old tooltip condition.
	return json.Marshal(sliderElement{
		Default: s.Default,
		Max:     s.Max,
		Min:     s.Min,
		Step:    s.StepSize,
		Text:    s.Text,
		Tooltip: s.Tooltip,
		Type:    "slider",
	})
}

// Value returns the value filled out by the user.
func (s Slider) Value() float64 {
	return s.value
}

// Dropdown represents a dropdown which, when clicked, opens a window with the options set in the Options
// field. Submitters may select one of the options.
type Dropdown struct {
	// Text is the text displayed over the dropdown element. The text may contain Minecraft formatting codes.
	Text string
	// Options holds a list of options that a Submitter may select. The order of these options is retained
	// when shown to the submitter of the form.
	Options []string
	// DefaultIndex is the index in the Options slice that is used as default. When sent to a Submitter, the
	// value at this index in the Options slice will be selected.
	DefaultIndex int
	// Tooltip is an optional text displayed when hovering over the element's info icon. The icon only
	// appears when a tooltip is set.
	Tooltip string

	value int
}

// NewDropdown creates and returns new Dropdown using the values passed.
func NewDropdown(text string, options []string, defaultIndex int) Dropdown {
	return Dropdown{Text: text, Options: options, DefaultIndex: defaultIndex}
}

// WithTooltip returns a copy of the Dropdown with the tooltip set.
func (d Dropdown) WithTooltip(tooltip string) Dropdown {
	d.Tooltip = tooltip
	return d
}

// MarshalJSON ...
func (d Dropdown) MarshalJSON() ([]byte, error) {
	// Sorted key order kept and omitempty matches the old tooltip condition.
	return json.Marshal(dropdownElement{
		Default: d.DefaultIndex,
		Options: d.Options,
		Text:    d.Text,
		Tooltip: d.Tooltip,
		Type:    "dropdown",
	})
}

// Value returns the value that the Submitter submitted. The value is an index pointing to the selected option
// in the Options slice.
func (d Dropdown) Value() int {
	return d.value
}

// StepSlider represents a slider that has a number of options that may be selected. It is essentially a
// combination of a Dropdown and a Slider, looking like a slider but having properties like a dropdown.
type StepSlider Dropdown

// NewStepSlider creates and returns new StepSlider using the values passed.
func NewStepSlider(text string, options []string, defaultIndex int) StepSlider {
	return StepSlider{Text: text, Options: options, DefaultIndex: defaultIndex}
}

// WithTooltip returns a copy of the StepSlider with the tooltip set.
func (s StepSlider) WithTooltip(tooltip string) StepSlider {
	s.Tooltip = tooltip
	return s
}

// MarshalJSON ...
func (s StepSlider) MarshalJSON() ([]byte, error) {
	// Sorted key order kept and omitempty matches the old tooltip condition.
	return json.Marshal(stepSliderElement{
		Default: s.DefaultIndex,
		Steps:   s.Options,
		Text:    s.Text,
		Tooltip: s.Tooltip,
		Type:    "step_slider",
	})
}

// Value returns the value that the Submitter submitted. The value is an index pointing to the selected option
// in the Options slice.
func (s StepSlider) Value() int {
	return s.value
}

// Button represents a button added to a Menu or Modal form. The button has text on it and an optional image,
// which may be either retrieved from a website or the local assets of the game.
type Button struct {
	// Text holds the text displayed on the button. It may use Minecraft formatting codes and may have
	// newlines.
	Text string
	// Image holds a path to an image for the button. The Image may either be a URL pointing to an image,
	// such as 'https://someimagewebsite.com/someimage.png', or a path pointing to a local asset, such as
	// 'textures/blocks/grass_carried'.
	Image string
}

// NewButton creates and returns a new Button using the text and image passed.
func NewButton(text, image string) Button {
	return Button{Text: text, Image: image}
}

// MarshalJSON ...
func (b Button) MarshalJSON() ([]byte, error) {
	// The outer and inner objects keep the sorted key order of the two maps
	// they replace, and a nil image pointer is omitted exactly where the old
	// code left the key out, so the bytes are identical.
	e := buttonElement{Text: b.Text, Type: "button"}
	if b.Image != "" {
		buttonType := "path"
		if strings.HasPrefix(b.Image, "http:") || strings.HasPrefix(b.Image, "https:") {
			buttonType = "url"
		}
		e.Image = &buttonImage{Data: b.Image, Type: buttonType}
	}
	return json.Marshal(e)
}

// textElement is the JSON shape of the elements that carry nothing but a text
// and a type. Field order matches the ascending key order encoding/json used
// for the maps these replace.
type textElement struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// inputElement is the JSON shape of an Input.
type inputElement struct {
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Text        string `json:"text"`
	Tooltip     string `json:"tooltip,omitempty"`
	Type        string `json:"type"`
}

// toggleElement is the JSON shape of a Toggle.
type toggleElement struct {
	Default bool   `json:"default"`
	Text    string `json:"text"`
	Tooltip string `json:"tooltip,omitempty"`
	Type    string `json:"type"`
}

// sliderElement is the JSON shape of a Slider.
type sliderElement struct {
	Default float64 `json:"default"`
	Max     float64 `json:"max"`
	Min     float64 `json:"min"`
	Step    float64 `json:"step"`
	Text    string  `json:"text"`
	Tooltip string  `json:"tooltip,omitempty"`
	Type    string  `json:"type"`
}

// dropdownElement is the JSON shape of a Dropdown.
type dropdownElement struct {
	Default int      `json:"default"`
	Options []string `json:"options"`
	Text    string   `json:"text"`
	Tooltip string   `json:"tooltip,omitempty"`
	Type    string   `json:"type"`
}

// stepSliderElement is the JSON shape of a StepSlider.
type stepSliderElement struct {
	Default int      `json:"default"`
	Steps   []string `json:"steps"`
	Text    string   `json:"text"`
	Tooltip string   `json:"tooltip,omitempty"`
	Type    string   `json:"type"`
}

// buttonElement is the JSON shape of a Button.
type buttonElement struct {
	Image *buttonImage `json:"image,omitempty"`
	Text  string       `json:"text"`
	Type  string       `json:"type"`
}

// buttonImage is the JSON shape of the optional image of a Button.
type buttonImage struct {
	Data string `json:"data"`
	Type string `json:"type"`
}

func (Divider) elem()    {}
func (Header) elem()     {}
func (Label) elem()      {}
func (Input) elem()      {}
func (Toggle) elem()     {}
func (Slider) elem()     {}
func (Dropdown) elem()   {}
func (StepSlider) elem() {}

func (Divider) menuElem() {}
func (Header) menuElem()  {}
func (Label) menuElem()   {}
func (Button) menuElem()  {}
