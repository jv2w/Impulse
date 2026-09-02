package cmd

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/world"
)

// Golden error texts. Every parse failure in this package renders to one of
// these; the strings are pinned literally because a caller has no other way to
// tell two failures apart (see errStrings).
const (
	errNumber    = "§r§c> 'zz' is not a valid number§r"
	errBool      = "§r§c> 'zz' is not true or false§r"
	errNoTarget  = "§r§cNo targets matched selector§r"
	errNotFound  = "§r§c> That player cannot be found§r"
	errParameter = "§r§c> 'zz' is not a valid parameter§r"
	errUnknown   = "§r§cUnknown command: \"pinned\": Please check that the command exists and that you have permission to use it.§r"
)

type intRunnable struct {
	V   int
	res *result
}

func (r intRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type int8Runnable struct {
	V   int8
	res *result
}

func (r int8Runnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type uint8Runnable struct {
	V   uint8
	res *result
}

func (r uint8Runnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type uintRunnable struct {
	V   uint
	res *result
}

func (r uintRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type float32Runnable struct {
	V   float32
	res *result
}

func (r float32Runnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type float64Runnable struct {
	V   float64
	res *result
}

func (r float64Runnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type stringRunnable struct {
	V   string
	res *result
}

func (r stringRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type boolRunnable struct {
	V   bool
	res *result
}

func (r boolRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type vec3Runnable struct {
	V   mgl64.Vec3
	res *result
}

func (r vec3Runnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type varargsRunnable struct {
	V   Varargs
	res *result
}

func (r varargsRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

// parseCase is one row of a per-type parsing table. Exactly one of want and
// wantErrs is meaningful: a row with wantErrs set must not run the Runnable.
type parseCase struct {
	name     string
	args     string
	want     []any
	wantErrs []string
}

// runParseCases builds a Runnable with make, runs args through it and checks
// the parsed values and the errors against the row.
func runParseCases(t *testing.T, cases []parseCase, make func(*result) Runnable) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := &result{}
			out := exec(t, c.args, make(res))
			if got := errStrings(out); !slices.Equal(got, c.wantErrs) {
				t.Fatalf("errors = %q, want %q", got, c.wantErrs)
			}
			if len(c.wantErrs) > 0 {
				if res.runs != 0 {
					t.Fatalf("Run was called %d times despite a parse error", res.runs)
				}
				return
			}
			if res.runs != 1 {
				t.Fatalf("Run calls = %d, want 1", res.runs)
			}
			if !reflect.DeepEqual(res.args, c.want) {
				t.Fatalf("parsed = %#v, want %#v", res.args, c.want)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	// strconv.ParseInt with the field's own bit width, so the accepted range is
	// the field type's range and nothing wider. Note that a leading '+' and an
	// underscore-free decimal string are all that is accepted: base prefixes are
	// rejected because the base is pinned to 10.
	usage := "§r§cUsage: /pinned <V: int>§r"
	runParseCases(t, []parseCase{
		{name: "zero", args: "0", want: []any{0}},
		{name: "positive", args: "12345", want: []any{12345}},
		{name: "negative", args: "-12345", want: []any{-12345}},
		{name: "explicit plus", args: "+7", want: []any{7}},
		{name: "hex is not a number", args: "0x10", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>0x10<<\"§r", "§r§c> '0x10' is not a valid number§r"}},
		{name: "underscores rejected", args: "1_000", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>1_000<<\"§r", "§r§c> '1_000' is not a valid number§r"}},
		{name: "float rejected", args: "1.0", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>1.0<<\"§r", "§r§c> '1.0' is not a valid number§r"}},
		{name: "text rejected", args: "zz", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>zz<<\"§r", errNumber}},
		{name: "missing argument", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", usage}},
	}, func(r *result) Runnable { return intRunnable{res: r} })
}

func TestParseIntUsesTheFieldBitWidth(t *testing.T) {
	// The parser reads v.Type().Bits(), so an int8 field rejects 128 while the
	// same text in an int field is fine. This is the only place where the width
	// of the declared field type reaches the user.
	usage := "§r§cUsage: /pinned <V: int>§r"
	runParseCases(t, []parseCase{
		{name: "int8 minimum", args: "-128", want: []any{int8(-128)}},
		{name: "int8 maximum", args: "127", want: []any{int8(127)}},
		{name: "one over the maximum", args: "128", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>128<<\"§r", "§r§c> '128' is not a valid number§r"}},
		{name: "one under the minimum", args: "-129", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>-129<<\"§r", "§r§c> '-129' is not a valid number§r"}},
		{name: "missing argument", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", usage}},
	}, func(r *result) Runnable { return int8Runnable{res: r} })
}

func TestParseUint(t *testing.T) {
	// PINNED CONSTRAINT: an unsigned parameter reports a negative number with
	// the same 'not a valid number' text as a non-numeric one, so a user cannot
	// tell 'out of range' from 'not a number'.
	runParseCases(t, []parseCase{
		{name: "zero", args: "0", want: []any{uint(0)}},
		{name: "large", args: "18446744073709551615", want: []any{uint(math.MaxUint64)}},
		{name: "negative rejected", args: "-1", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>-1<<\"§r", "§r§c> '-1' is not a valid number§r"}},
		{name: "text rejected", args: "zz", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>zz<<\"§r", errNumber}},
	}, func(r *result) Runnable { return uintRunnable{res: r} })

	runParseCases(t, []parseCase{
		{name: "uint8 maximum", args: "255", want: []any{uint8(255)}},
		{name: "one over the maximum", args: "256", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>256<<\"§r", "§r§c> '256' is not a valid number§r"}},
	}, func(r *result) Runnable { return uint8Runnable{res: r} })
}

func TestParseFloat(t *testing.T) {
	// PINNED BUG: strconv.ParseFloat accepts "NaN", "Inf", "+Inf" and "-Inf",
	// and the parser passes them straight into the command parameter. Every
	// downstream consumer of a float parameter (positions, velocities, health)
	// therefore has to defend itself: server/entity already pins that a NaN
	// reaching Hurt or a velocity travels the whole way through unchecked.
	runParseCases(t, []parseCase{
		{name: "integer text", args: "5", want: []any{5.0}},
		{name: "decimal", args: "1.5", want: []any{1.5}},
		{name: "negative zero", args: "-0", want: []any{math.Copysign(0, -1)}},
		{name: "exponent", args: "1e3", want: []any{1000.0}},
		{name: "hex float accepted", args: "0x1p4", want: []any{16.0}},
		{name: "text rejected", args: "zz", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>zz<<\"§r", errNumber}},
	}, func(r *result) Runnable { return float64Runnable{res: r} })

	for _, arg := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "infinity"} {
		t.Run("non-finite "+arg, func(t *testing.T) {
			res := &result{}
			out := exec(t, arg, float64Runnable{res: res})
			if got := errStrings(out); len(got) != 0 {
				t.Fatalf("errors = %q, want none: %q is accepted as a float parameter", got, arg)
			}
			v := res.args[0].(float64)
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				t.Fatalf("parsed %q as %v, want a non-finite value", arg, v)
			}
		})
	}
}

func TestParseFloat32NarrowsSilently(t *testing.T) {
	// A float32 field parses with 32 bits, so a value that does not fit is
	// rounded rather than rejected. Only a value beyond the float32 range is an
	// error.
	runParseCases(t, []parseCase{
		{name: "rounded", args: "0.1", want: []any{float32(0.1)}},
		{name: "out of range", args: "1e40", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>1e40<<\"§r", "§r§c> '1e40' is not a valid number§r"}},
	}, func(r *result) Runnable { return float32Runnable{res: r} })
}

func TestParseString(t *testing.T) {
	// A string parameter takes exactly one whitespace separated argument. Quotes
	// are consumed by the csv reader, not by the string parser, so the value the
	// command sees never contains them.
	runParseCases(t, []parseCase{
		{name: "single word", args: "hello", want: []any{"hello"}},
		{name: "quoted phrase is one argument", args: `"hello world"`, want: []any{"hello world"}},
		{name: "second word is left over", args: "hello world", wantErrs: []string{
			"§r§cSyntax error: unexpected value: at \"/pinned hello >>world<<\"§r",
			"§r§cUsage: /pinned <V: string>§r",
		}},
		{name: "missing argument", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", "§r§cUsage: /pinned <V: string>§r"}},
	}, func(r *result) Runnable { return stringRunnable{res: r} })
}

func TestParseBool(t *testing.T) {
	// PINNED CONSTRAINT: the parser uses strconv.ParseBool, so it accepts eight
	// spellings while the error message claims the value "is not true or false".
	// A client-side boolean parameter only ever sends "true" or "false", so the
	// extra spellings are reachable from the console and from any caller that
	// builds a command line itself.
	cases := make([]parseCase, 0, 10)
	for _, arg := range []string{"1", "t", "T", "TRUE", "true", "True"} {
		cases = append(cases, parseCase{name: "true as " + arg, args: arg, want: []any{true}})
	}
	for _, arg := range []string{"0", "f", "F", "FALSE", "false", "False"} {
		cases = append(cases, parseCase{name: "false as " + arg, args: arg, want: []any{false}})
	}
	cases = append(cases,
		parseCase{name: "yes rejected", args: "yes", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>yes<<\"§r", "§r§c> 'yes' is not true or false§r"}},
		parseCase{name: "text rejected", args: "zz", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>zz<<\"§r", errBool}},
		parseCase{name: "missing argument", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", "§r§cUsage: /pinned <V: bool>§r"}},
	)
	runParseCases(t, cases, func(r *result) Runnable { return boolRunnable{res: r} })
}

func TestParseVec3(t *testing.T) {
	// A Vec3 consumes three float arguments. The parser removes the first two
	// itself and lets parseArgument remove the third, so a Vec3 is the only
	// parameter type whose consumption is split across two functions.
	runParseCases(t, []parseCase{
		{name: "three floats", args: "1 2.5 -3", want: []any{mgl64.Vec3{1, 2.5, -3}}},
		{name: "second component invalid", args: "1 zz 3", wantErrs: []string{
			"§r§cSyntax error: unexpected value: at \"/pinned 1 >>zz<< 3\"§r",
			errNumber,
		}},
		{name: "only two components", args: "1 2", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned 1 2>><<\"§r", "§r§cUsage: /pinned <V: x y z>§r"}},
		{name: "no components", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", "§r§cUsage: /pinned <V: x y z>§r"}},
	}, func(r *result) Runnable { return vec3Runnable{res: r} })
}

func TestParseVec3RejectsRelativeCoordinates(t *testing.T) {
	// PINNED SCOPE: vanilla accepts '~' and '^' as relative and local
	// coordinates. This parser is three ParseFloat calls, so '~' is simply an
	// invalid number. Adding relative coordinates would change what a Vec3
	// parameter means for every existing command.
	res := &result{}
	out := exec(t, "~ ~1 ~", vec3Runnable{res: res})
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned >>~<< ~1 ~\"§r",
		"§r§c> '~' is not a valid number§r",
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

func TestParseVarargs(t *testing.T) {
	// Varargs takes everything that is left and joins it with a single space.
	// The join is over the fields the csv reader produced, so the original
	// spacing survives only because a double space becomes an empty field.
	runParseCases(t, []parseCase{
		{name: "single word", args: "hello", want: []any{Varargs("hello")}},
		{name: "several words", args: "hello there world", want: []any{Varargs("hello there world")}},
		{name: "double space survives as an empty field", args: "hello  world", want: []any{Varargs("hello  world")}},
		{name: "quotes are lost", args: `"hello world" again`, want: []any{Varargs("hello world again")}},
	}, func(r *result) Runnable { return varargsRunnable{res: r} })
}

func TestParseVarargsAcceptsNothing(t *testing.T) {
	// PINNED BUG: a Varargs parameter is declared non-optional but can never
	// fail, so a command with a required Varargs parameter runs with an empty
	// string when the user passed no argument at all. The usage string still
	// renders it as <V: text>, which promises the opposite.
	res := &result{}
	out := exec(t, "", varargsRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if res.runs != 1 {
		t.Fatalf("Run calls = %d, want 1", res.runs)
	}
	if got := res.args[0].(Varargs); got != "" {
		t.Fatalf("Varargs = %q, want the empty string", got)
	}
	if want := "/pinned <V: text>"; New("pinned", "", nil, varargsRunnable{res: res}).Usage() != want {
		t.Fatalf("Usage() = %q, want %q", New("pinned", "", nil, varargsRunnable{res: res}).Usage(), want)
	}
}

type twoVarargsRunnable struct {
	A, B Varargs
	res  *result
}

func (r twoVarargsRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.A, r.B}
}

func TestParseVarargsSwallowsEveryLaterParameter(t *testing.T) {
	// PINNED BUG: nothing rejects a parameter declared after a Varargs. The
	// first Varargs takes the whole line and the second silently receives the
	// empty string, so the command runs with a parameter the user never had a
	// chance to fill in.
	res := &result{}
	out := exec(t, "one two three", twoVarargsRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{Varargs("one two three"), Varargs("")}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("parsed = %#v, want %#v", res.args, want)
	}
}

// pinnedEnum is an Enum whose options differ in case from what a user types, so
// that the canonicalisation the parser performs is visible.
type pinnedEnum string

func (pinnedEnum) Type() string { return "PinnedEnum" }
func (pinnedEnum) Options(Source) []string {
	return []string{"Survival", "Creative", "spectator"}
}

type enumRunnable struct {
	V   pinnedEnum
	res *result
}

func (r enumRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

func TestParseEnum(t *testing.T) {
	// Matching is case insensitive but the value stored is the option's own
	// spelling, not the user's. A command comparing the parameter against a
	// literal therefore has to use the spelling from Options.
	runParseCases(t, []parseCase{
		{name: "exact", args: "Survival", want: []any{pinnedEnum("Survival")}},
		{name: "lowercase input keeps option casing", args: "survival", want: []any{pinnedEnum("Survival")}},
		{name: "uppercase input keeps option casing", args: "CREATIVE", want: []any{pinnedEnum("Creative")}},
		{name: "already lowercase option", args: "SPECTATOR", want: []any{pinnedEnum("spectator")}},
		{name: "unknown option", args: "adventure", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned >>adventure<<\"§r", "§r§c> 'adventure' is not a valid parameter§r"}},
		{name: "missing argument", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", "§r§cUsage: /pinned <V: PinnedEnum>§r"}},
	}, func(r *result) Runnable { return enumRunnable{res: r} })
}

// sourceEnum returns options that depend on the Source, which is how the
// package supports per-player option lists.
type sourceEnum string

func (sourceEnum) Type() string { return "SourceEnum" }
func (sourceEnum) Options(src Source) []string {
	if named, ok := src.(NamedTarget); ok && named.Name() != "" {
		return []string{named.Name()}
	}
	return nil
}

type sourceEnumRunnable struct {
	V   sourceEnum
	res *result
}

func (r sourceEnumRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

func TestParseEnumReceivesTheSource(t *testing.T) {
	// Options is called with the executing Source on every parse, so an enum may
	// present a different option list per source. An empty option list makes
	// every value invalid rather than every value valid.
	src := &testSource{name: "Steve"}
	res := &result{}
	out := execTx(t, "steve", nil, src, sourceEnumRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if got := res.args[0].(sourceEnum); got != "Steve" {
		t.Fatalf("parsed = %q, want %q", got, "Steve")
	}

	res = &result{}
	out = execTx(t, "steve", nil, &testSource{}, sourceEnumRunnable{res: res})
	if got, want := errStrings(out), []string{"§r§cSyntax error: unexpected value: at \"/pinned >>steve<<\"§r", "§r§c> 'steve' is not a valid parameter§r"}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

type subCommandRunnable struct {
	Reload SubCommand
	V      int
	res    *result
}

func (r subCommandRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

type taggedSubCommandRunnable struct {
	Sub SubCommand `cmd:"reset"`
	res *result
}

func (r taggedSubCommandRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

func TestParseSubCommand(t *testing.T) {
	// A SubCommand matches its parameter name case insensitively and consumes
	// one argument without storing anything.
	runParseCases(t, []parseCase{
		{name: "exact name", args: "Reload 3", want: []any{3}},
		{name: "case insensitive", args: "reload 3", want: []any{3}},
		{name: "wrong literal", args: "restart 3", wantErrs: []string{
			"§r§cSyntax error: unexpected value: at \"/pinned >>restart<< 3\"§r",
			"§r§c> 'restart' is not a valid parameter§r",
		}},
		{name: "missing literal", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", "§r§cUsage: /pinned Reload <V: int>§r"}},
	}, func(r *result) Runnable { return subCommandRunnable{res: r} })
}

func TestSubCommandLiteralComesFromTheTag(t *testing.T) {
	// The literal a SubCommand matches is the parameter name, which the cmd tag
	// overrides. The field name becomes unreachable once a tag is set.
	res := &result{}
	if got := errStrings(exec(t, "reset", taggedSubCommandRunnable{res: res})); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if res.runs != 1 {
		t.Fatalf("Run calls = %d, want 1", res.runs)
	}
	res = &result{}
	out := exec(t, "sub", taggedSubCommandRunnable{res: res})
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned >>sub<<\"§r",
		"§r§c> 'sub' is not a valid parameter§r",
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

type optionalRunnable struct {
	Required int
	Opt      Optional[string]
	res      *result
}

func (r optionalRunnable) Run(Source, *Output, *world.Tx) {
	v, ok := r.Opt.Load()
	r.res.runs++
	r.res.args = []any{r.Required, v, ok, r.Opt.LoadOr("fallback")}
}

func TestParseOptional(t *testing.T) {
	// An absent optional parameter leaves the zero value and reports false from
	// Load, which is the only way a command can tell 'not given' from 'given as
	// the zero value'.
	runParseCases(t, []parseCase{
		{name: "given", args: "1 hello", want: []any{1, "hello", true, "hello"}},
		{name: "absent", args: "1", want: []any{1, "", false, "fallback"}},
		{name: "given as empty string", args: `1 ""`, want: []any{1, "", true, ""}},
		{name: "required still missing", args: "", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r", "§r§cUsage: /pinned <Required: int> [Opt: string]§r"}},
	}, func(r *result) Runnable { return optionalRunnable{res: r} })
}

type optionalVec3Runnable struct {
	Opt Optional[mgl64.Vec3]
	res *result
}

func (r optionalVec3Runnable) Run(Source, *Output, *world.Tx) {
	v, ok := r.Opt.Load()
	r.res.runs++
	r.res.args = []any{v, ok}
}

func TestParseOptionalMultiArgument(t *testing.T) {
	// PINNED BUG: the 'is this optional parameter present' test is
	// line.Len() == 0, which only checks that at least one argument is left. An
	// Optional[mgl64.Vec3] with one argument left is therefore attempted and
	// fails, rather than being treated as absent.
	runParseCases(t, []parseCase{
		{name: "absent", args: "", want: []any{mgl64.Vec3{}, false}},
		{name: "fully given", args: "1 2 3", want: []any{mgl64.Vec3{1, 2, 3}, true}},
		{name: "partially given", args: "1", wantErrs: []string{"§r§cSyntax error: unexpected value: at \"/pinned 1>><<\"§r", "§r§cUsage: /pinned [Opt: x y z]§r"}},
	}, func(r *result) Runnable { return optionalVec3Runnable{res: r} })
}

type emptyRunnable struct {
	res *result
}

func (r emptyRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

func TestExecuteWithNoParameters(t *testing.T) {
	// A Runnable with no parameters runs on an empty line and reports leftover
	// arguments otherwise. The leftover check happens after every field, so the
	// syntax error names the first argument that had nowhere to go.
	res := &result{}
	if got := errStrings(exec(t, "", emptyRunnable{res: res})); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if res.runs != 1 {
		t.Fatalf("Run calls = %d, want 1", res.runs)
	}

	res = &result{}
	out := exec(t, "extra args", emptyRunnable{res: res})
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned >>extra<< args\"§r",
		"§r§cUsage: /pinned§r",
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
	if res.runs != 0 {
		t.Fatalf("Run was called %d times with leftover arguments", res.runs)
	}
}

func TestSplittingIsCSVWithASpaceSeparator(t *testing.T) {
	// PINNED BUG: arguments are split by encoding/csv with a space separator and
	// LazyQuotes enabled. Two consequences reach the user: consecutive spaces
	// produce empty arguments that then fail to parse, and an unterminated quote
	// is silently accepted instead of being reported.
	res := &result{}
	out := exec(t, "1  2 3", vec3Runnable{res: res})
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned 1 >><< 2 3\"§r",
		"§r§c> '' is not a valid number§r",
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q: a double space must produce an empty argument", got, want)
	}

	res = &result{}
	out = exec(t, `"unterminated`, stringRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none: LazyQuotes accepts an unterminated quote", got)
	}
	if got := res.args[0].(string); got != "unterminated" {
		t.Fatalf("parsed = %q, want %q", got, "unterminated")
	}

	// A leading space is an empty first argument, so a command line that merely
	// looks indented fails.
	res = &result{}
	out = exec(t, " hello", stringRunnable{res: res})
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned  >>hello<<\"§r",
		"§r§cUsage: /pinned <V: string>§r",
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

func TestCommasAndTabsAreNotSeparators(t *testing.T) {
	// The csv Comma is a space, so a comma is an ordinary character and a tab is
	// not a separator either. Both end up inside a single argument.
	res := &result{}
	if got := errStrings(exec(t, "a,b", stringRunnable{res: res})); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if got := res.args[0].(string); got != "a,b" {
		t.Fatalf("parsed = %q, want %q", got, "a,b")
	}

	res = &result{}
	if got := errStrings(exec(t, "a\tb", stringRunnable{res: res})); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if got := res.args[0].(string); got != "a\tb" {
		t.Fatalf("parsed = %q, want %q", got, "a\tb")
	}
}

func TestLineConsumption(t *testing.T) {
	// Line is exported and handed to every Parameter implementation, so its
	// consumption rules are part of the public contract. Next does not consume;
	// only RemoveNext and RemoveN do, and Leftover empties the line without
	// recording what it took in the 'seen' list used by SyntaxError.
	line := &Line{args: []string{"a", "b", "c"}, seen: []string{"/pinned"}}
	if v, ok := line.Next(); !ok || v != "a" {
		t.Fatalf("Next() = %q, %v, want \"a\", true", v, ok)
	}
	if line.Len() != 3 {
		t.Fatalf("Len() = %d after Next(), want 3: Next must not consume", line.Len())
	}
	if v, ok := line.NextN(2); !ok || !slices.Equal(v, []string{"a", "b"}) {
		t.Fatalf("NextN(2) = %q, %v, want [a b], true", v, ok)
	}
	if _, ok := line.NextN(4); ok {
		t.Fatal("NextN(4) succeeded on a line of 3 arguments")
	}
	line.RemoveN(2)
	if line.Len() != 1 {
		t.Fatalf("Len() = %d after RemoveN(2), want 1", line.Len())
	}
	if got, want := line.SyntaxError().Error(), "§r§cSyntax error: unexpected value: at \"/pinned a b >>c<<\"§r"; got != want {
		t.Fatalf("SyntaxError() = %q, want %q", got, want)
	}
	if got := line.Leftover(); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("Leftover() = %q, want [c]", got)
	}
	if line.Len() != 0 {
		t.Fatalf("Len() = %d after Leftover(), want 0", line.Len())
	}
	// PINNED BUG: Leftover does not add what it took to 'seen', so a SyntaxError
	// raised after a Varargs parameter reports a command line that is missing
	// every argument the Varargs swallowed.
	if got, want := line.SyntaxError().Error(), "§r§cSyntax error: unexpected value: at \"/pinned a b>><<\"§r"; got != want {
		t.Fatalf("SyntaxError() after Leftover() = %q, want %q", got, want)
	}
}

func TestRemoveNBeyondTheEndEmptiesTheLine(t *testing.T) {
	// RemoveN with more arguments than are left drops everything and, unlike the
	// normal path, records nothing in 'seen'.
	line := &Line{args: []string{"a"}, seen: []string{"/pinned"}}
	line.RemoveN(5)
	if line.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", line.Len())
	}
	if got, want := line.SyntaxError().Error(), "§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r"; got != want {
		t.Fatalf("SyntaxError() = %q, want %q", got, want)
	}
}
