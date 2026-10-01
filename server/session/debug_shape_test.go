package session

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/player/debug"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Debug shapes are another one-way conversion, and almost every field is filled
// in from a default when the caller leaves it zero. Both the shape type numbers
// and the defaults are pinned literally: a client that draws a cone where a
// pyramid was asked for has no way to tell anyone.

// Golden primitive shape type numbers.
const (
	goldenShapeLine      = 0
	goldenShapeBox       = 1
	goldenShapeSphere    = 2
	goldenShapeCircle    = 3
	goldenShapeText      = 4
	goldenShapeArrow     = 5
	goldenShapeCylinder  = 6
	goldenShapePyramid   = 7
	goldenShapeEllipsoid = 8
	goldenShapeCone      = 9
)

// TestPrimitiveShapeGoldenValues pins the protocol shape numbers.
func TestPrimitiveShapeGoldenValues(t *testing.T) {
	golden := map[string][2]uint8{
		"Line":      {protocol.PrimitiveShapeLine, goldenShapeLine},
		"Box":       {protocol.PrimitiveShapeBox, goldenShapeBox},
		"Sphere":    {protocol.PrimitiveShapeSphere, goldenShapeSphere},
		"Circle":    {protocol.PrimitiveShapeCircle, goldenShapeCircle},
		"Text":      {protocol.PrimitiveShapeText, goldenShapeText},
		"Arrow":     {protocol.PrimitiveShapeArrow, goldenShapeArrow},
		"Cylinder":  {protocol.PrimitiveShapeCylinder, goldenShapeCylinder},
		"Pyramid":   {protocol.PrimitiveShapePyramid, goldenShapePyramid},
		"Ellipsoid": {protocol.PrimitiveShapeEllipsoid, goldenShapeEllipsoid},
		"Cone":      {protocol.PrimitiveShapeCone, goldenShapeCone},
	}
	for name, pair := range golden {
		if pair[0] != pair[1] {
			t.Errorf("protocol.PrimitiveShape%s = %d, want the golden %d", name, pair[0], pair[1])
		}
	}
}

// TestDebugShapeToProtocolTypes pins which shape number each debug shape
// converts to. The shapes are otherwise left at their zero values so that only
// the type distinguishes the cases.
func TestDebugShapeToProtocolTypes(t *testing.T) {
	tests := []struct {
		name  string
		shape debug.Shape
		want  uint8
	}{
		{name: "arrow", shape: &debug.Arrow{}, want: goldenShapeArrow},
		{name: "box", shape: &debug.Box{}, want: goldenShapeBox},
		{name: "circle", shape: &debug.Circle{}, want: goldenShapeCircle},
		{name: "line", shape: &debug.Line{}, want: goldenShapeLine},
		{name: "sphere", shape: &debug.Sphere{}, want: goldenShapeSphere},
		{name: "text", shape: &debug.Text{}, want: goldenShapeText},
		{name: "cylinder", shape: &debug.Cylinder{}, want: goldenShapeCylinder},
		{name: "pyramid", shape: &debug.Pyramid{}, want: goldenShapePyramid},
		{name: "ellipsoid", shape: &debug.Ellipsoid{}, want: goldenShapeEllipsoid},
		{name: "cone", shape: &debug.Cone{}, want: goldenShapeCone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ps := debugShapeToProtocol(test.shape, 0)

			got, ok := ps.Type.Value()
			if !ok {
				t.Fatal("the shape type was not set")
			}
			if got != test.want {
				t.Fatalf("type = %d, want %d", got, test.want)
			}
			if ps.NetworkID != uint64(test.shape.ShapeID()) {
				t.Fatalf("network ID = %d, want the shape ID %d", ps.NetworkID, test.shape.ShapeID())
			}
			if dim, ok := ps.DimensionID.Value(); !ok || dim != 0 {
				t.Fatalf("dimension = %v (set: %v), want the overworld 0", dim, ok)
			}
			if _, ok := ps.AttachedToEntityID.Value(); ok {
				t.Fatal("an unattached shape reported an entity ID")
			}
			// Every shape defaults its colour to opaque white.
			if c, ok := ps.Colour.Value(); !ok || c != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
				t.Fatalf("colour = %v (set: %v), want opaque white", c, ok)
			}
		})
	}
}

// TestDebugShapeToProtocolDimensionAndAttachment pins the two fields that come
// from outside the shape.
func TestDebugShapeToProtocolDimensionAndAttachment(t *testing.T) {
	ps := debugShapeToProtocol(&debug.Box{}, 42)

	if dim, ok := ps.DimensionID.Value(); !ok || dim != 0 {
		t.Fatalf("dimension = %v (set: %v), want the overworld 0", dim, ok)
	}
	if id, ok := ps.AttachedToEntityID.Value(); !ok || id != 42 {
		t.Fatalf("attached entity = %v (set: %v), want 42", id, ok)
	}

	// A runtime ID of zero means unattached, so it is left out entirely.
	ps = debugShapeToProtocol(&debug.Box{}, 0)
	if _, ok := ps.AttachedToEntityID.Value(); ok {
		t.Fatal("runtime ID 0 was sent as an attachment")
	}
}

// TestDebugShapeToProtocolArrowDefaults pins the arrow's four defaults, which
// are the values the client draws when the caller left the field zero.
func TestDebugShapeToProtocolArrowDefaults(t *testing.T) {
	ps := debugShapeToProtocol(&debug.Arrow{}, 0)

	arrow, ok := ps.ExtraShapeData.(*protocol.ArrowShape)
	if !ok {
		t.Fatalf("extra shape data = %T, want *protocol.ArrowShape", ps.ExtraShapeData)
	}
	if length, _ := arrow.ArrowHeadLength.Value(); length != 1 {
		t.Fatalf("head length = %v, want the default 1", length)
	}
	if radius, _ := arrow.ArrowHeadRadius.Value(); radius != 0.5 {
		t.Fatalf("head radius = %v, want the default 0.5", radius)
	}
	if segments, _ := arrow.Segments.Value(); segments != 4 {
		t.Fatalf("segments = %v, want the default 4", segments)
	}

	explicit := debugShapeToProtocol(&debug.Arrow{
		Position:     mgl64.Vec3{1, 2, 3},
		EndPosition:  mgl64.Vec3{4, 5, 6},
		HeadLength:   2,
		HeadRadius:   3,
		HeadSegments: 8,
		Colour:       color.RGBA{R: 1, G: 2, B: 3, A: 4},
	}, 0)

	if loc, _ := explicit.Location.Value(); loc != (mgl32.Vec3{1, 2, 3}) {
		t.Fatalf("location = %v, want {1 2 3}", loc)
	}
	if c, _ := explicit.Colour.Value(); c != (color.RGBA{R: 1, G: 2, B: 3, A: 4}) {
		t.Fatalf("colour = %v, want the explicit one", c)
	}
	arrow = explicit.ExtraShapeData.(*protocol.ArrowShape)
	if end, _ := arrow.ArrowEndLocation.Value(); end != (mgl32.Vec3{4, 5, 6}) {
		t.Fatalf("end location = %v, want {4 5 6}", end)
	}
	if segments, _ := arrow.Segments.Value(); segments != 8 {
		t.Fatalf("segments = %v, want the explicit 8", segments)
	}
}

// TestDebugShapeToProtocolShapeDefaults pins the per-shape defaults in one
// table. Each entry converts a zero-valued shape and asserts the extra data the
// client is handed.
func TestDebugShapeToProtocolShapeDefaults(t *testing.T) {
	tests := []struct {
		name  string
		shape debug.Shape
		want  any
	}{
		{
			name:  "box bounds default to a unit cube",
			shape: &debug.Box{},
			want:  &protocol.BoxShape{BoxBound: mgl32.Vec3{1, 1, 1}},
		},
		{
			name:  "circle defaults to twenty segments",
			shape: &debug.Circle{},
			want:  &protocol.SphereShape{Segments: 20},
		},
		{
			name:  "sphere defaults to twenty segments",
			shape: &debug.Sphere{},
			want:  &protocol.SphereShape{Segments: 20},
		},
		{
			name:  "line ends at the origin",
			shape: &debug.Line{},
			want:  &protocol.LineShape{LineEndLocation: mgl32.Vec3{}},
		},
		{
			name:  "cylinder defaults to unit radii and height",
			shape: &debug.Cylinder{},
			want: &protocol.CylinderShape{
				RadiusX:     mgl32.Vec2{1, 1},
				RadiusZ:     mgl32.Vec2{1, 1},
				Height:      1,
				NumSegments: 20,
			},
		},
		{
			name:  "pyramid defaults to a unit base and height",
			shape: &debug.Pyramid{},
			want:  &protocol.PyramidShape{Width: 1, Height: 1},
		},
		{
			name:  "ellipsoid defaults to unit radii",
			shape: &debug.Ellipsoid{},
			want:  &protocol.EllipsoidShape{Radii: mgl32.Vec3{1, 1, 1}, SegmentsPerAxis: 20},
		},
		{
			name:  "cone defaults to unit radii and height",
			shape: &debug.Cone{},
			want:  &protocol.ConeShape{Radii: mgl32.Vec2{1, 1}, Height: 1, NumSegments: 20},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ps := debugShapeToProtocol(test.shape, 0)
			if !reflect.DeepEqual(ps.ExtraShapeData, test.want) {
				t.Fatalf("extra shape data = %#v, want %#v", ps.ExtraShapeData, test.want)
			}
			// Everything but the arrow and line also carries a scale of 1.
			switch test.shape.(type) {
			case *debug.Line:
			default:
				if scale, ok := ps.Scale.Value(); !ok || scale != 1 {
					t.Fatalf("scale = %v (set: %v), want the default 1", scale, ok)
				}
			}
		})
	}
}

// TestDebugShapeToProtocolCylinderTopRadiusDefaultsToTheBase pins that a
// cylinder with no top radius is straight rather than collapsing to a cone, and
// that the two radii are packed one axis per Vec2.
func TestDebugShapeToProtocolCylinderTopRadiusDefaultsToTheBase(t *testing.T) {
	ps := debugShapeToProtocol(&debug.Cylinder{BaseRadius: mgl64.Vec2{2, 3}}, 0)

	want := &protocol.CylinderShape{
		// RadiusX holds the base and top radius on X; RadiusZ the same on Z.
		RadiusX:     mgl32.Vec2{2, 2},
		RadiusZ:     mgl32.Vec2{3, 3},
		Height:      1,
		NumSegments: 20,
	}
	if !reflect.DeepEqual(ps.ExtraShapeData, want) {
		t.Fatalf("cylinder = %#v, want %#v", ps.ExtraShapeData, want)
	}

	tapered := debugShapeToProtocol(&debug.Cylinder{
		BaseRadius: mgl64.Vec2{2, 3},
		TopRadius:  mgl64.Vec2{1, 1},
	}, 0)
	wantTapered := &protocol.CylinderShape{
		RadiusX:     mgl32.Vec2{2, 1},
		RadiusZ:     mgl32.Vec2{3, 1},
		Height:      1,
		NumSegments: 20,
	}
	if !reflect.DeepEqual(tapered.ExtraShapeData, wantTapered) {
		t.Fatalf("tapered cylinder = %#v, want %#v", tapered.ExtraShapeData, wantTapered)
	}
}

// TestDebugShapeToProtocolPyramidDepth pins the pyramid's depth, which is the
// one field that is left unset rather than defaulted so the client can fall
// back to the width.
func TestDebugShapeToProtocolPyramidDepth(t *testing.T) {
	ps := debugShapeToProtocol(&debug.Pyramid{Width: 4}, 0)
	pyramid := ps.ExtraShapeData.(*protocol.PyramidShape)
	if _, ok := pyramid.Depth.Value(); ok {
		t.Fatal("an unset depth was sent, want it left out so the client uses the width")
	}

	ps = debugShapeToProtocol(&debug.Pyramid{Width: 4, Depth: 5}, 0)
	pyramid = ps.ExtraShapeData.(*protocol.PyramidShape)
	if depth, ok := pyramid.Depth.Value(); !ok || depth != 5 {
		t.Fatalf("depth = %v (set: %v), want 5", depth, ok)
	}
}

// TestDebugShapeToProtocolText pins the text shape, whose flags are all
// inverted on the way out and whose background has three states.
func TestDebugShapeToProtocolText(t *testing.T) {
	ps := debugShapeToProtocol(&debug.Text{Text: "hello"}, 0)
	text := ps.ExtraShapeData.(*protocol.TextShape)

	if text.Text != "hello" {
		t.Fatalf("text = %q, want hello", text.Text)
	}
	// The debug shape says what to hide; the protocol says what to show.
	if !text.DepthTest || !text.ShowBackface || !text.ShowBackfaceText {
		t.Fatalf("default flags = %#v, want everything shown", text)
	}
	if text.UseRotation {
		t.Fatal("rotation was used without LockRotation")
	}
	if _, ok := ps.Rotation.Value(); ok {
		t.Fatal("a rotation was sent without LockRotation")
	}
	if _, ok := text.BackgroundColour.Value(); ok {
		t.Fatal("an unset background colour was sent, want the client's default")
	}

	hidden := debugShapeToProtocol(&debug.Text{
		HideBackground:   true,
		BackgroundColour: color.RGBA{R: 1, A: 1},
		DisableDepthTest: true,
		HideBackface:     true,
		HideBackfaceText: true,
		LockRotation:     true,
		Rotation:         mgl64.Vec3{0, 90, 0},
	}, 0)
	text = hidden.ExtraShapeData.(*protocol.TextShape)

	// HideBackground takes precedence over an explicit background colour.
	if c, ok := text.BackgroundColour.Value(); !ok || c != (color.RGBA{}) {
		t.Fatalf("background = %v (set: %v), want a fully transparent colour", c, ok)
	}
	if text.DepthTest || text.ShowBackface || text.ShowBackfaceText {
		t.Fatalf("flags = %#v, want everything hidden", text)
	}
	if !text.UseRotation {
		t.Fatal("UseRotation was not set with LockRotation")
	}
	if rot, ok := hidden.Rotation.Value(); !ok || rot != (mgl32.Vec3{0, 90, 0}) {
		t.Fatalf("rotation = %v (set: %v), want {0 90 0}", rot, ok)
	}

	coloured := debugShapeToProtocol(&debug.Text{
		BackgroundColour: color.RGBA{R: 1, G: 2, B: 3, A: 4},
	}, 0)
	text = coloured.ExtraShapeData.(*protocol.TextShape)
	if c, ok := text.BackgroundColour.Value(); !ok || c != (color.RGBA{R: 1, G: 2, B: 3, A: 4}) {
		t.Fatalf("background = %v (set: %v), want the explicit colour", c, ok)
	}
}

// TestDebugShapeToProtocolUnknownShapePanics pins the enumeration default.
//
// PINNED BUG: an unknown debug.Shape implementation panics rather than being
// skipped or reported. debug.Shape is an exported single-method interface, so a
// third-party shape reaching SendDebugShapes takes the session down.
func TestDebugShapeToProtocolUnknownShapePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unknown shape did not panic")
		}
	}()
	debugShapeToProtocol(foreignShape{}, 0)
}

// foreignShape is a debug.Shape the conversion's type switch does not name.
type foreignShape struct{}

func (foreignShape) ShapeID() int { return 1 }

// TestShapeAttachedEntityRuntimeID pins that a shape's attached entity is
// resolved through the session's runtime ID map, and that a shape with no
// entity reports 0.
func TestShapeAttachedEntityRuntimeID(t *testing.T) {
	s := newStubSession(t, nil)
	e := newStubEntity(t, stubEntityType{name: "impulse:attached"})
	s.entityRuntimeIDs[e.H()] = 11

	shapes := []debug.Shape{
		&debug.Arrow{Entity: e.H()},
		&debug.Box{Entity: e.H()},
		&debug.Circle{Entity: e.H()},
		&debug.Line{Entity: e.H()},
		&debug.Sphere{Entity: e.H()},
		&debug.Text{Entity: e.H()},
		&debug.Cylinder{Entity: e.H()},
		&debug.Pyramid{Entity: e.H()},
		&debug.Ellipsoid{Entity: e.H()},
		&debug.Cone{Entity: e.H()},
	}
	for _, shape := range shapes {
		if got := s.shapeAttachedEntityRuntimeID(shape); got != 11 {
			t.Fatalf("%T attached runtime ID = %d, want 11", shape, got)
		}
	}

	if got := s.shapeAttachedEntityRuntimeID(&debug.Box{}); got != 0 {
		t.Fatalf("an unattached box reported runtime ID %d, want 0", got)
	}
	// PINNED SCOPE: an unknown shape reports 0 rather than panicking here, so
	// the panic in debugShapeToProtocol is the only one on this path. Out of
	// scope.
	if got := s.shapeAttachedEntityRuntimeID(foreignShape{}); got != 0 {
		t.Fatalf("an unknown shape reported runtime ID %d, want 0", got)
	}
}
