package particle

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
)

// Like server/world/sound, this package holds no behaviour: every type is an
// inert value that server/session/world.go type switches on to pick a protocol
// particle event and its parameters. The contract worth pinning is therefore
// the value set itself -- which types exist and which fields they carry --
// since session reads exactly that.
//
// PINNED CONSTRAINT: the protocol particle event numbers live in
// server/session and cannot be reached from this package.

// exported is every exported struct type in the package with its exported
// fields, in "Name Type" form and declaration order. It is compared against a
// fresh parse of the package sources, so an added, removed or renamed type or
// field fails here.
var exported = []struct{ name, fields string }{
	{"BlockBreak", "Block world.Block"},
	{"BlockForceField", ""},
	{"BoneMeal", "Area bool"},
	{"DragonEggTeleport", "Diff cube.Pos"},
	{"Dust", "Colour color.RGBA"},
	{"DustPlume", ""},
	{"Effect", "Colour color.RGBA"},
	{"EggSmash", ""},
	{"EndermanTeleport", ""},
	{"EntityFlame", ""},
	{"Evaporate", ""},
	{"Flame", "Colour color.RGBA"},
	{"HugeExplosion", ""},
	{"Lava", ""},
	{"LavaDrip", ""},
	{"Note", "Instrument sound.Instrument, Pitch int"},
	{"PunchBlock", "Block world.Block, Face cube.Face"},
	{"SnowballPoof", ""},
	{"Splash", "Colour color.RGBA"},
	{"WaterDrip", ""},
}

// exportedCount is stated separately from len(exported) so that deleting a row
// cannot shrink the expectation along with the package.
const exportedCount = 20

// particles holds one zero value of every exported type. The element type makes
// the world.Particle implementation a compile-time fact; the tests below check
// the list is neither short nor long.
var particles = []world.Particle{
	BlockBreak{}, BlockForceField{}, BoneMeal{}, DragonEggTeleport{}, Dust{}, DustPlume{}, Effect{},
	EggSmash{}, EndermanTeleport{}, EntityFlame{}, Evaporate{}, Flame{}, HugeExplosion{}, Lava{},
	LavaDrip{}, Note{}, PunchBlock{}, SnowballPoof{}, Splash{}, WaterDrip{},
}

func TestExportedTypeInventory(t *testing.T) {
	if len(exported) != exportedCount {
		t.Fatalf("exported holds %d entries, want %d", len(exported), exportedCount)
	}
	got := parseExportedStructs(t)
	if len(got) != exportedCount {
		t.Errorf("package declares %d exported struct types, want %d", len(got), exportedCount)
	}
	for _, e := range exported {
		fields, ok := got[e.name]
		if !ok {
			t.Errorf("type %s is listed but no longer declared", e.name)
			continue
		}
		if fields != e.fields {
			t.Errorf("%s has exported fields %q, want %q", e.name, fields, e.fields)
		}
		delete(got, e.name)
	}
	for name, fields := range got {
		t.Errorf("type %s (fields %q) is declared but not listed", name, fields)
	}
}

func TestParticleListCoversEveryExportedType(t *testing.T) {
	want := make([]string, 0, exportedCount)
	for _, e := range exported {
		want = append(want, e.name)
	}
	got := make([]string, 0, len(particles))
	for _, p := range particles {
		got = append(got, strings.TrimPrefix(fmt.Sprintf("%T", p), "particle."))
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("particles covers %v, want %v", got, want)
	}
}

// TestSpawnIgnoresItsArguments pins that this package carries no behaviour: the
// embedded particle.Spawn never touches the world or the position it is handed,
// despite its doc comment saying a particle "may execute any additional actions
// here, such as spawning different particles". None do.
func TestSpawnIgnoresItsArguments(t *testing.T) {
	for _, p := range particles {
		p.Spawn(nil, mgl64.Vec3{math.NaN(), math.Inf(-1), math.MaxFloat64})
	}
}

// PINNED BUG: Flame and Dust document a fallback for an unspecified Colour --
// "the normal flame particle" and "black" respectively -- but the zero value of
// color.RGBA is an opaque-less black that a caller may also have meant
// literally. The two are indistinguishable here, and the choice between them is
// made in session. Pinned as-is; decide in stage 1.
func TestColourFieldsHaveNoUnsetMarker(t *testing.T) {
	var zero color.RGBA
	for _, c := range []struct {
		name   string
		colour color.RGBA
	}{
		{"Flame", Flame{}.Colour},
		{"Dust", Dust{}.Colour},
		{"Splash", Splash{}.Colour},
		{"Effect", Effect{}.Colour},
	} {
		if c.colour != zero {
			t.Errorf("%s{}.Colour = %#v, want the zero RGBA", c.name, c.colour)
		}
		// Explicitly transparent black is the same value as unset.
		explicit := color.RGBA{R: 0, G: 0, B: 0, A: 0}
		if explicit != c.colour {
			t.Errorf("%s: explicit transparent black differs from the zero value", c.name)
		}
	}
}

// TestZeroValues pins what session sees when a caller builds a particle without
// filling its fields in. Nothing here is validated or rejected.
func TestZeroValues(t *testing.T) {
	if p := (BlockBreak{}); p.Block != nil {
		t.Errorf("BlockBreak{}.Block = %v, want nil", p.Block)
	}
	if p := (PunchBlock{}); p.Block != nil || p.Face != cube.FaceDown {
		t.Errorf("PunchBlock{} = %+v, want a nil Block and FaceDown", p)
	}
	if p := (BoneMeal{}); p.Area {
		t.Error("BoneMeal{}.Area = true, want false")
	}
	if p := (DragonEggTeleport{}); p.Diff != (cube.Pos{}) {
		t.Errorf("DragonEggTeleport{}.Diff = %v, want the zero Pos", p.Diff)
	}
	// Piano is the zero Instrument, so an unfilled Note is a piano rather than
	// an invalid instrument. Pitch is an unvalidated int.
	if p := (Note{}); p.Instrument != sound.Piano() || p.Pitch != 0 {
		t.Errorf("Note{} = %+v, want Piano and pitch 0", p)
	}
	if got := (Note{Pitch: -5}).Pitch; got != -5 {
		t.Errorf("Note{Pitch: -5}.Pitch = %d, want -5", got)
	}
}

// parseExportedStructs parses the non-test sources of this package and returns
// each exported struct type name mapped to its exported fields in "Name Type"
// form, comma separated and in declaration order.
func parseExportedStructs(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				var fields []string
				for _, fl := range st.Fields.List {
					typ := exprString(fl.Type)
					for _, n := range fl.Names {
						if n.IsExported() {
							fields = append(fields, n.Name+" "+typ)
						}
					}
				}
				out[ts.Name.Name] = strings.Join(fields, ", ")
			}
		}
	}
	return out
}

// exprString renders the type expressions this package actually uses. Anything
// it does not know renders as "?" so an unhandled shape shows up as a mismatch
// rather than as an empty string that happens to match.
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(v.X)
	case *ast.ArrayType:
		return "[]" + exprString(v.Elt)
	}
	return "?"
}
