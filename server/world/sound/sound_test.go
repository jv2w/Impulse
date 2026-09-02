package sound

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/world"
)

// This package holds no behaviour: every type is an inert value that
// server/session/world.go type switches on to pick a protocol sound event and
// its parameters. So the contract worth pinning is the shape of that value set
// -- which types exist, which fields they carry, and what the three enum
// wrappers encode to -- because session reads exactly that and nothing else.
//
// PINNED CONSTRAINT: the protocol sound event numbers themselves live in
// server/session, not here, and cannot be reached from this package. They are
// out of this file's scope.

// exported is every exported struct type in the package with its exported
// fields, in "Name Type" form and declaration order. It is compared against a
// fresh parse of the package sources, so a type or field that is added,
// removed or renamed fails here rather than silently changing what session can
// construct.
var exported = []struct{ name, fields string }{
	{"AnvilBreak", ""},
	{"AnvilLand", ""},
	{"AnvilUse", ""},
	{"ArrowHit", ""},
	{"Attack", "Damage bool"},
	{"BarrelClose", ""},
	{"BarrelOpen", ""},
	{"BlastFurnaceCrackle", ""},
	{"BlockBreaking", "Block world.Block"},
	{"BlockPlace", "Block world.Block"},
	{"BowShoot", ""},
	{"BucketEmpty", "Liquid world.Liquid"},
	{"BucketFill", "Liquid world.Liquid"},
	{"Burning", ""},
	{"Burp", ""},
	{"CampfireCrackle", ""},
	{"ChestClose", ""},
	{"ChestOpen", ""},
	{"Click", ""},
	{"ComposterEmpty", ""},
	{"ComposterFill", ""},
	{"ComposterFillLayer", ""},
	{"ComposterReady", ""},
	{"CopperScraped", ""},
	{"CrossbowLoad", "Stage int, QuickCharge bool"},
	{"CrossbowShoot", ""},
	{"Custom", "Name string, Volume float64, Pitch float64"},
	{"DecoratedPotInsertFailed", ""},
	{"DecoratedPotInserted", "Progress float64"},
	{"Deny", ""},
	{"DiscType", ""},
	{"DoorClose", "Block world.Block"},
	{"DoorCrash", ""},
	{"DoorOpen", "Block world.Block"},
	{"Drowning", ""},
	{"EndPortalCreated", ""},
	{"EnderChestClose", ""},
	{"EnderChestOpen", ""},
	{"EnderEyePlaced", ""},
	{"EquipItem", "Item world.Item"},
	{"Experience", ""},
	{"Explosion", ""},
	{"Fall", "Distance float64"},
	{"FenceGateClose", "Block world.Block"},
	{"FenceGateOpen", "Block world.Block"},
	{"FireCharge", ""},
	{"FireExtinguish", ""},
	{"FireworkBlast", ""},
	{"FireworkHugeBlast", ""},
	{"FireworkLaunch", ""},
	{"FireworkTwinkle", ""},
	{"Fizz", ""},
	{"FurnaceCrackle", ""},
	{"GhastShoot", ""},
	{"GhastWarning", ""},
	{"GlassBreak", ""},
	{"GoatHorn", "Horn Horn"},
	{"Horn", ""},
	{"Ignite", ""},
	{"Instrument", ""},
	{"ItemAdd", ""},
	{"ItemBreak", ""},
	{"ItemFrameRemove", ""},
	{"ItemFrameRotate", ""},
	{"ItemThrow", ""},
	{"ItemUseOn", "Block world.Block"},
	{"LecternBookPlace", ""},
	{"LevelUp", ""},
	{"LightningExplode", ""},
	{"LightningThunder", ""},
	{"MusicDiscEnd", ""},
	{"MusicDiscPlay", "DiscType DiscType"},
	{"Note", "Instrument Instrument, Pitch int"},
	{"Pop", ""},
	{"PotionBrewed", ""},
	{"PowerOff", ""},
	{"PowerOn", ""},
	{"ShulkerBoxClose", ""},
	{"ShulkerBoxOpen", ""},
	{"SignWaxed", ""},
	{"SmokerCrackle", ""},
	{"StopUsingSpyglass", ""},
	{"TNT", ""},
	{"Teleport", ""},
	{"Thunder", ""},
	{"Totem", ""},
	{"TrapdoorClose", "Block world.Block"},
	{"TrapdoorOpen", "Block world.Block"},
	{"UseSpyglass", ""},
	{"WaxRemoved", ""},
	{"WaxedSignFailedInteraction", ""},
}

// exportedCount is stated separately from len(exported) so that deleting a row
// cannot shrink the expectation along with the package.
const exportedCount = 91

// enums are the exported struct types that are not sounds. They wrap an
// unexported integer and exist to be carried as a field of a sound.
var enums = []string{"DiscType", "Horn", "Instrument"}

// sounds holds one zero value of every type in the package that implements
// world.Sound. The element type makes that implementation a compile-time fact;
// the tests below check the list is neither short nor long.
var sounds = []world.Sound{
	AnvilBreak{}, AnvilLand{}, AnvilUse{}, ArrowHit{}, Attack{}, BarrelClose{}, BarrelOpen{},
	BlastFurnaceCrackle{}, BlockBreaking{}, BlockPlace{}, BowShoot{}, BucketEmpty{}, BucketFill{},
	Burning{}, Burp{}, CampfireCrackle{}, ChestClose{}, ChestOpen{}, Click{}, ComposterEmpty{},
	ComposterFill{}, ComposterFillLayer{}, ComposterReady{}, CopperScraped{}, CrossbowLoad{},
	CrossbowShoot{}, Custom{}, DecoratedPotInsertFailed{}, DecoratedPotInserted{}, Deny{},
	DoorClose{}, DoorCrash{}, DoorOpen{}, Drowning{}, EndPortalCreated{}, EnderChestClose{},
	EnderChestOpen{}, EnderEyePlaced{}, EquipItem{}, Experience{}, Explosion{}, Fall{},
	FenceGateClose{}, FenceGateOpen{}, FireCharge{}, FireExtinguish{}, FireworkBlast{},
	FireworkHugeBlast{}, FireworkLaunch{}, FireworkTwinkle{}, Fizz{}, FurnaceCrackle{}, GhastShoot{},
	GhastWarning{}, GlassBreak{}, GoatHorn{}, Ignite{}, ItemAdd{}, ItemBreak{}, ItemFrameRemove{},
	ItemFrameRotate{}, ItemThrow{}, ItemUseOn{}, LecternBookPlace{}, LevelUp{}, LightningExplode{},
	LightningThunder{}, MusicDiscEnd{}, MusicDiscPlay{}, Note{}, Pop{}, PotionBrewed{}, PowerOff{},
	PowerOn{}, ShulkerBoxClose{}, ShulkerBoxOpen{}, SignWaxed{}, SmokerCrackle{}, StopUsingSpyglass{},
	TNT{}, Teleport{}, Thunder{}, Totem{}, TrapdoorClose{}, TrapdoorOpen{}, UseSpyglass{},
	WaxRemoved{}, WaxedSignFailedInteraction{},
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

func TestSoundListCoversEveryNonEnumType(t *testing.T) {
	want := make([]string, 0, exportedCount-len(enums))
	for _, e := range exported {
		if !slices.Contains(enums, e.name) {
			want = append(want, e.name)
		}
	}
	got := make([]string, 0, len(sounds))
	for _, s := range sounds {
		got = append(got, typeName(s))
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("sounds covers %v, want %v", got, want)
	}
}

// TestPlayIgnoresItsArguments pins that this package carries no behaviour: the
// embedded sound.Play never touches the world or the position it is handed, so
// it is safe on a nil world. Everything a sound actually does happens in
// session.
func TestPlayIgnoresItsArguments(t *testing.T) {
	for _, s := range sounds {
		s.Play(nil, mgl64.Vec3{math.NaN(), math.Inf(-1), math.MaxFloat64})
	}
}

// discs is the full disc registry. Uint8 is the value a jukebox stores and
// sends, so it is the on-disk and on-network identity of a disc.
var discs = []struct {
	id      uint8
	name    string
	display string
	author  string
	disc    DiscType
}{
	{0, "13", "13", "C418", Disc13()},
	{1, "cat", "cat", "C418", DiscCat()},
	{2, "blocks", "blocks", "C418", DiscBlocks()},
	{3, "chirp", "chirp", "C418", DiscChirp()},
	{4, "far", "far", "C418", DiscFar()},
	{5, "mall", "mall", "C418", DiscMall()},
	{6, "mellohi", "mellohi", "C418", DiscMellohi()},
	{7, "stal", "stal", "C418", DiscStal()},
	{8, "strad", "strad", "C418", DiscStrad()},
	{9, "ward", "ward", "C418", DiscWard()},
	{10, "11", "11", "C418", Disc11()},
	{11, "wait", "wait", "C418", DiscWait()},
	{12, "otherside", "otherside", "Lena Raine", DiscOtherside()},
	{13, "pigstep", "Pigstep", "Lena Raine", DiscPigstep()},
	{14, "5", "5", "Samuel Åberg", Disc5()},
	{15, "relic", "Relic", "Aaron Cherof", DiscRelic()},
	{16, "creator", "Creator", "Lena Raine", DiscCreator()},
	{17, "creator_music_box", "Creator (Music Box)", "Lena Raine", DiscCreatorMusicBox()},
	{18, "precipice", "Precipice", "Aaron Cherof", DiscPrecipice()},
	{19, "tears", "Tears", "Amos Roddy", DiscTears()},
	{20, "lava_chicken", "Lava Chicken", "Hyper Potions", DiscLavaChicken()},
}

const discCount = 21

// PINNED BUG: DisplayName title-cases on the boundary d > 12, so disc 12
// ("otherside") is the only disc after the C418 block left lowercase while
// every later one is capitalised. jukebox.go puts this straight into the
// "Now playing: %v - %v" popup, so the inconsistency is player-visible.
// Pinned as-is; decide in stage 1.
func TestDiscGolden(t *testing.T) {
	if len(discs) != discCount {
		t.Fatalf("discs holds %d entries, want %d", len(discs), discCount)
	}
	for _, d := range discs {
		if got := d.disc.Uint8(); got != d.id {
			t.Errorf("%s.Uint8() = %d, want %d", d.name, got, d.id)
		}
		if got := d.disc.String(); got != d.name {
			t.Errorf("disc %d String() = %q, want %q", d.id, got, d.name)
		}
		if got := d.disc.DisplayName(); got != d.display {
			t.Errorf("disc %d DisplayName() = %q, want %q", d.id, got, d.display)
		}
		if got := d.disc.Author(); got != d.author {
			t.Errorf("disc %d Author() = %q, want %q", d.id, got, d.author)
		}
		// DiscType is a comparable value with no pointer inside, so two calls
		// to the same constructor are equal and can be used as a map key.
		if d.disc != d.disc {
			t.Errorf("disc %d is not equal to itself", d.id)
		}
	}
}

func TestMusicDiscsIsExhaustiveAndOrdered(t *testing.T) {
	all := MusicDiscs()
	if len(all) != discCount {
		t.Fatalf("MusicDiscs() returned %d discs, want %d", len(all), discCount)
	}
	for i, d := range all {
		if got := d.Uint8(); int(got) != i {
			t.Fatalf("MusicDiscs()[%d].Uint8() = %d, want %d", i, got, i)
		}
		if d != discs[i].disc {
			t.Fatalf("MusicDiscs()[%d] is %q, want %q", i, d.String(), discs[i].name)
		}
	}
}

// PINNED BUG: an out-of-range disc takes the process down instead of reporting
// failure. disc is an unexported uint8, but DiscType is exported with an
// exported zero value, so DiscType{} plus arithmetic on the wrapped value is
// not the only route -- any future decoder reading a stored uint8 back into a
// DiscType reaches this. CLAUDE.md 2.2 names exactly this shape.
// Pinned as-is; decide in stage 1.
func TestDiscOutOfRangePanics(t *testing.T) {
	for _, c := range []struct {
		name string
		call func()
		want string
	}{
		{"String", func() { _ = DiscType{disc(discCount)}.String() }, "unknown record type"},
		{"DisplayName", func() { _ = DiscType{disc(discCount)}.DisplayName() }, "unknown record type"},
		{"Author", func() { _ = DiscType{disc(discCount)}.Author() }, "unknown record type"},
		{"String255", func() { _ = DiscType{disc(255)}.String() }, "unknown record type"},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("disc %d %s did not panic", discCount, c.name)
				}
				if got := fmt.Sprint(r); got != c.want {
					t.Fatalf("panic value = %q, want %q", got, c.want)
				}
			}()
			c.call()
		})
	}
}

// PINNED SCOPE: there is no lookup from a stored uint8 back to a DiscType and
// no error-returning constructor. A caller decoding a jukebox has to scan
// MusicDiscs itself.
func TestNoDiscLookupExists(t *testing.T) {
	var found DiscType
	ok := false
	for _, d := range MusicDiscs() {
		if d.Uint8() == 13 {
			found, ok = d, true
		}
	}
	if !ok || found != DiscPigstep() {
		t.Fatalf("scanning MusicDiscs for 13 gave %v, %v, want DiscPigstep, true", found, ok)
	}
}

// horns is the full goat horn registry. Uint8 is what is written to the item's
// NBT and sent in the sound event.
var horns = []struct {
	id   uint8
	name string
	horn Horn
}{
	{0, "Ponder", Ponder()},
	{1, "Sing", Sing()},
	{2, "Seek", Seek()},
	{3, "Feel", Feel()},
	{4, "Admire", Admire()},
	{5, "Call", Call()},
	{6, "Yearn", Yearn()},
	{7, "Dream", Dream()},
}

const hornCount = 8

func TestHornGolden(t *testing.T) {
	if len(horns) != hornCount {
		t.Fatalf("horns holds %d entries, want %d", len(horns), hornCount)
	}
	all := GoatHorns()
	if len(all) != hornCount {
		t.Fatalf("GoatHorns() returned %d horns, want %d", len(all), hornCount)
	}
	for i, h := range horns {
		if got := h.horn.Uint8(); got != h.id {
			t.Errorf("%s.Uint8() = %d, want %d", h.name, got, h.id)
		}
		if got := h.horn.Name(); got != h.name {
			t.Errorf("horn %d Name() = %q, want %q", h.id, got, h.name)
		}
		if all[i] != h.horn {
			t.Errorf("GoatHorns()[%d] is %q, want %q", i, all[i].Name(), h.name)
		}
	}
}

// PINNED BUG: goatHornType.Name panics with the literal string CLAUDE.md 2.2
// forbids, "should never happen", for any value above 7.
// Pinned as-is; decide in stage 1.
func TestHornOutOfRangePanics(t *testing.T) {
	for _, id := range []uint8{hornCount, 100, 255} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("horn %d Name() did not panic", id)
				}
				if got, want := fmt.Sprint(r), "should never happen"; got != want {
					t.Fatalf("panic value = %q, want %q", got, want)
				}
			}()
			_ = Horn{goatHornType(id)}.Name()
		}()
	}
}

// instruments is the full note block instrument set. Int32 is the value the
// note block sound event carries.
var instruments = []struct {
	id   int32
	name string
	inst Instrument
}{
	{0, "Piano", Piano()},
	{1, "BassDrum", BassDrum()},
	{2, "Snare", Snare()},
	{3, "ClicksAndSticks", ClicksAndSticks()},
	{4, "Bass", Bass()},
	{5, "Flute", Flute()},
	{6, "Bell", Bell()},
	{7, "Guitar", Guitar()},
	{8, "Chimes", Chimes()},
	{9, "Xylophone", Xylophone()},
	{10, "IronXylophone", IronXylophone()},
	{11, "CowBell", CowBell()},
	{12, "Didgeridoo", Didgeridoo()},
	{13, "Bit", Bit()},
	{14, "Banjo", Banjo()},
	{15, "Pling", Pling()},
}

// PINNED SCOPE: Instrument has no String, no Name and no exhaustive list
// function, unlike DiscType and Horn. The list below is this test's own, built
// by hand from the constructors, so a new instrument added to the package is
// not caught here. That gap is the reason the inventory test above parses the
// sources instead of trusting a hand-written list.
func TestInstrumentGolden(t *testing.T) {
	seen := map[int32]string{}
	for _, i := range instruments {
		if got := i.inst.Int32(); got != i.id {
			t.Errorf("%s.Int32() = %d, want %d", i.name, got, i.id)
		}
		if prev, ok := seen[i.id]; ok {
			t.Errorf("%s and %s both encode to %d", prev, i.name, i.id)
		}
		seen[i.id] = i.name
	}
	// Piano is the zero value, so a Note built without an Instrument plays as
	// a piano rather than as an invalid instrument.
	if (Note{}).Instrument != Piano() {
		t.Errorf("Note{}.Instrument = %d, want Piano (%d)", (Note{}).Instrument.Int32(), Piano().Int32())
	}
}

func TestCrossbowLoadingStages(t *testing.T) {
	for _, c := range []struct {
		stage int
		want  int
	}{
		{CrossbowLoadingStart, 0},
		{CrossbowLoadingMiddle, 1},
		{CrossbowLoadingEnd, 2},
	} {
		if c.stage != c.want {
			t.Errorf("crossbow stage = %d, want %d", c.stage, c.want)
		}
	}
	// CrossbowLoad.Stage is a plain int with no validation, so a stage outside
	// the three constants is constructible and is passed on unchanged.
	if got := (CrossbowLoad{Stage: 99}).Stage; got != 99 {
		t.Errorf("CrossbowLoad{Stage: 99}.Stage = %d, want 99", got)
	}
}

// TestEnumListsAreFreshPerCall answers the aliasing question CLAUDE.md 3.4 asks
// of a returned slice. Both lists are built from a literal on every call, so a
// caller that sorts or truncates one cannot affect the next caller.
func TestEnumListsAreFreshPerCall(t *testing.T) {
	first := MusicDiscs()
	first[0] = DiscLavaChicken()
	if got := MusicDiscs()[0]; got != Disc13() {
		t.Errorf("MusicDiscs()[0] = %q after the previous result was overwritten, want %q", got.String(), Disc13().String())
	}
	horns := GoatHorns()
	horns[0] = Dream()
	if got := GoatHorns()[0]; got != Ponder() {
		t.Errorf("GoatHorns()[0] = %q after the previous result was overwritten, want %q", got.Name(), Ponder().Name())
	}
}

// TestParameterisedSoundZeroValues pins what session sees when a caller builds
// a sound without filling its fields in. None of these are rejected.
func TestParameterisedSoundZeroValues(t *testing.T) {
	if s := (Custom{}); s.Name != "" || s.Volume != 0 || s.Pitch != 0 {
		t.Errorf("Custom{} = %+v, want all fields zero", s)
	}
	if s := (Fall{}); s.Distance != 0 {
		t.Errorf("Fall{}.Distance = %v, want 0", s.Distance)
	}
	if s := (Attack{}); s.Damage {
		t.Errorf("Attack{}.Damage = true, want false")
	}
	if s := (BlockPlace{}); s.Block != nil {
		t.Errorf("BlockPlace{}.Block = %v, want nil", s.Block)
	}
	if s := (MusicDiscPlay{}); s.DiscType != Disc13() {
		t.Errorf("MusicDiscPlay{}.DiscType = %q, want %q", s.DiscType.String(), Disc13().String())
	}
	if s := (GoatHorn{}); s.Horn != Ponder() {
		t.Errorf("GoatHorn{}.Horn = %q, want %q", s.Horn.Name(), Ponder().Name())
	}
	if s := (DecoratedPotInserted{}); s.Progress != 0 {
		t.Errorf("DecoratedPotInserted{}.Progress = %v, want 0", s.Progress)
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

// typeName is the Go type of s without the package qualifier.
func typeName(s world.Sound) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", s), "sound.")
}
