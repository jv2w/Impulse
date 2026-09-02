package cmd

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/world"
)

// This file pins what the reflection walk in command.go accepts, what it
// rejects, and where it rejects it. The distinction matters: a signature
// rejected in New fails at start-up where a developer sees it, while one
// rejected in Execute fails on the world goroutine while a player is typing.

type unsupportedRunnable struct {
	V   []string
	res *result
}

func (r unsupportedRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

type unsupportedMapRunnable struct {
	V   map[string]int
	res *result
}

func (r unsupportedMapRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

type unsupportedPointerRunnable struct {
	V   *int
	res *result
}

func (r unsupportedPointerRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

type unsupportedOptionalRunnable struct {
	V   Optional[[]string]
	res *result
}

func (r unsupportedOptionalRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

func TestUnsupportedFieldTypePanicsAtExecuteNotAtNew(t *testing.T) {
	// PINNED BUG: an exported field of an unsupported type passes New without a
	// word and panics inside parseArgument the first time a source runs the
	// command. Execute is called on the world goroutine, so a third party
	// shipping a command with a []string parameter takes the world down at the
	// moment a player types the command rather than at registration.
	// CLAUDE.md 2.2: the default branch of parseArgument's type switch must
	// become a value, and verifySignature must reject the type in New.
	cases := []struct {
		name string
		make func(*result) Runnable
		want string
	}{
		{name: "slice", make: func(r *result) Runnable { return unsupportedRunnable{res: r} },
			want: "non-command parameter type []string in command structure"},
		{name: "map", make: func(r *result) Runnable { return unsupportedMapRunnable{res: r} },
			want: "non-command parameter type map[string]int in command structure"},
		{name: "pointer", make: func(r *result) Runnable { return unsupportedPointerRunnable{res: r} },
			want: "non-command parameter type *int in command structure"},
		{name: "optional of an unsupported type", make: func(r *result) Runnable { return unsupportedOptionalRunnable{res: r} },
			want: "non-command parameter type []string in command structure"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := &result{}
			// New accepts the signature without complaint. If this line ever
			// panics the deferred failure has been turned into a start-up one,
			// which is the fix, and this pin has to be replaced.
			command := New("pinned", "", nil, c.make(res))
			if command.Usage() == "" {
				t.Fatal("New returned a command with no usage")
			}
			v := mustPanic(t, func() { command.Execute("x", &testSource{}, nil) })
			if got, ok := v.(string); !ok || got != c.want {
				t.Fatalf("panic = %v, want %q", v, c.want)
			}
		})
	}
}

func TestUnsupportedRequiredTypePanicsEvenOnAnEmptyLine(t *testing.T) {
	// The early return for a line that has run out only covers optional
	// parameters, so a required parameter of an unsupported type reaches the
	// type switch even when the user typed nothing at all. Such a command is
	// unusable in every form, and the only signal is a panic on the world
	// goroutine.
	res := &result{}
	v := mustPanic(t, func() { exec(t, "", unsupportedRunnable{res: res}) })
	if got, want := fmt.Sprint(v), "non-command parameter type []string in command structure"; got != want {
		t.Fatalf("panic = %q, want %q", got, want)
	}

	// An optional parameter of an unsupported type is the one case that
	// survives, and only while the user supplies no value for it.
	res = &result{}
	out := exec(t, "", unsupportedOptionalRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if res.runs != 1 {
		t.Fatalf("Run calls = %d, want 1", res.runs)
	}
}

type ignoredFieldRunnable struct {
	V       int
	Ignored []string `cmd:"-"`
	res     *result
}

func (r ignoredFieldRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.V, r.Ignored}
}

func TestIgnoredFieldsKeepTheirValue(t *testing.T) {
	// A `cmd:"-"` field is skipped by the reflection walk entirely, so it may
	// hold a type the parser knows nothing about, and the value the Runnable was
	// constructed with survives into Run. This is the only supported way to
	// carry per-command state into Run.
	res := &result{}
	out := exec(t, "5", ignoredFieldRunnable{Ignored: []string{"carried"}, res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{5, []string{"carried"}}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}
	if got, want := New("pinned", "", nil, ignoredFieldRunnable{res: res}).Usage(), "/pinned <V: int>"; got != want {
		t.Fatalf("Usage() = %q, want %q", got, want)
	}
}

type unexportedOnlyRunnable struct {
	hidden int
	res    *result
}

func (r unexportedOnlyRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.hidden}
}

func TestUnexportedFieldsAreCopiedButNotParsed(t *testing.T) {
	// Unexported fields never become parameters, and Execute copies the whole
	// Runnable value, so their contents reach Run unchanged.
	res := &result{}
	out := exec(t, "", unexportedOnlyRunnable{hidden: 42, res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{42}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}
}

type embedded struct {
	Promoted int
}

type embeddingRunnable struct {
	embedded
	Own int
	res *result
}

func (r embeddingRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.Promoted, r.Own}
}

func TestPromotedFieldsBecomeParameters(t *testing.T) {
	// PINNED CONSTRAINT: exportedFields drops the anonymous field itself but
	// keeps the fields promoted through it, and reflect.VisibleFields yields
	// promoted fields before the declaring struct's own later fields. Embedding a
	// struct in a Runnable therefore silently prepends parameters, in an order
	// that is not the order the fields are written in the Runnable.
	res := &result{}
	command := New("pinned", "", nil, embeddingRunnable{res: res})
	if got, want := command.Usage(), "/pinned <Promoted: int> <Own: int>"; got != want {
		t.Fatalf("Usage() = %q, want %q", got, want)
	}
	out := exec(t, "1 2", embeddingRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{1, 2}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}
}

type badOptionalOrderRunnable struct {
	Opt      Optional[int]
	Required int
}

func (badOptionalOrderRunnable) Run(Source, *Output, *world.Tx) {}

type badEnum int

func (badEnum) Type() string            { return "BadEnum" }
func (badEnum) Options(Source) []string { return nil }

type badEnumRunnable struct {
	V badEnum
}

func (badEnumRunnable) Run(Source, *Output, *world.Tx) {}

type notAStruct func()

func (notAStruct) Run(Source, *Output, *world.Tx) {}

func TestNewPanicsOnAnInvalidSignature(t *testing.T) {
	// PINNED BUG: every signature New does reject, it rejects by panicking with
	// a bare string. New has no error return, so a caller building commands from
	// third-party plugins cannot survive a bad one. CLAUDE.md 2.2.
	cases := []struct {
		name string
		make func() Command
		want string
	}{
		{name: "required after optional", make: func() Command { return New("pinned", "", nil, badOptionalOrderRunnable{}) },
			want: "command must only have optional parameters at the end"},
		{name: "enum that is not a string", make: func() Command { return New("pinned", "", nil, badEnumRunnable{}) },
			want: "parameters implementing Enum must be of the type string"},
		{name: "runnable that is not a struct", make: func() Command { return New("pinned", "", nil, notAStruct(func() {})) },
			want: "Runnable r must be struct or pointer to struct, but got func"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := mustPanic(t, func() { c.make() })
			if got, ok := v.(string); !ok || got != c.want {
				t.Fatalf("panic = %v, want %q", v, c.want)
			}
		})
	}
}

type pointerRunnable struct {
	V   int
	res *result
}

func (r *pointerRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.V}
}

func TestPointerRunnableIsDereferencedAndCopied(t *testing.T) {
	// A pointer Runnable is dereferenced in New and the pointed-to value is
	// stored. Execute allocates a fresh copy per execution, so the struct the
	// caller passed to New is never written to and two concurrent executions
	// cannot see each other's parameters.
	res := &result{}
	original := &pointerRunnable{V: 99, res: res}
	command := New("pinned", "", nil, original)
	src := &testSource{}
	command.Execute("5", src, nil)
	if got := errStrings(src.outputs[0]); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{5}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}
	if original.V != 99 {
		t.Fatalf("the Runnable passed to New was written to: V = %d, want 99", original.V)
	}
}

type overloadA struct {
	Set SubCommand
	V   int
	res *result
}

func (r overloadA) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{"A", r.V} }

type overloadB struct {
	Set SubCommand
	V   string
	res *result
}

func (r overloadB) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{"B", r.V} }

func TestOverloadSelection(t *testing.T) {
	// Runnables are tried in declaration order and the first that parses runs.
	// When none parses, the reported error is the one from the Runnable that
	// consumed the most arguments, and ties go to the *last* such Runnable
	// because the comparison is '<=' rather than '<'.
	res := &result{}
	out := exec(t, "set 5", overloadA{res: res}, overloadB{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{"A", 5}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v: the first parsing overload must win", res.args, want)
	}

	res = &result{}
	out = exec(t, "set hello", overloadA{res: res}, overloadB{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{"B", "hello"}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}

	// Both overloads fail after consuming the subcommand literal, so the tie is
	// broken in favour of the later one and its usage line is reported.
	res = &result{}
	out = exec(t, "wrong 5", overloadA{res: res}, overloadB{res: res})
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned >>wrong<< 5\"§r",
		"§r§c> 'wrong' is not a valid parameter§r",
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
	if res.runs != 0 {
		t.Fatalf("Run calls = %d, want 0", res.runs)
	}
}

type overloadC struct {
	Set SubCommand
	V   bool
	res *result
}

func (r overloadC) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{"C", r.V} }

func TestOverloadTieGoesToTheLastRunnable(t *testing.T) {
	// PINNED CONSTRAINT: the tie-break is '<=', so when several overloads fail
	// having consumed the same number of arguments, the error reported is the
	// one from the *last* of them. Reordering the Runnables passed to New
	// therefore changes the error message a user sees for an identical mistake.
	res := &result{}
	intFirst := errStrings(exec(t, "set zz", overloadA{res: res}, overloadC{res: res}))
	if want := []string{
		"§r§cSyntax error: unexpected value: at \"/pinned set >>zz<<\"§r",
		errBool,
	}; !slices.Equal(intFirst, want) {
		t.Fatalf("errors = %q, want %q: the last tying overload must supply the error", intFirst, want)
	}

	boolFirst := errStrings(exec(t, "set zz", overloadC{res: res}, overloadA{res: res}))
	if want := []string{
		"§r§cSyntax error: unexpected value: at \"/pinned set >>zz<<\"§r",
		errNumber,
	}; !slices.Equal(boolFirst, want) {
		t.Fatalf("errors = %q, want %q", boolFirst, want)
	}
}

func TestUsageJoinsEveryOverload(t *testing.T) {
	// Usage is the usage line of every Runnable joined by newlines, and String
	// returns the same value. A command with several overloads therefore has a
	// multi-line usage, which is what the usage error message carries.
	res := &result{}
	command := New("pinned", "", nil, overloadA{res: res}, overloadB{res: res})
	want := "/pinned Set <V: int>\n/pinned Set <V: string>"
	if got := command.Usage(); got != want {
		t.Fatalf("Usage() = %q, want %q", got, want)
	}
	if got := command.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

type suffixRunnable struct {
	Amount   int              `cmd:"amount,blocks"`
	Renamed  string           `cmd:"target"`
	Optional Optional[string] `cmd:"reason,optional"`
	res      *result
}

func (r suffixRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

func TestUsageRendersTagsAndSuffixes(t *testing.T) {
	// The cmd tag is "name,suffix". The name replaces the field name in both the
	// usage and the parameter list, and the suffix is appended outside the
	// angle or square brackets.
	res := &result{}
	command := New("pinned", "", nil, suffixRunnable{res: res})
	want := "/pinned <amount: int>blocks <target: string> [reason: string]optional"
	if got := command.Usage(); got != want {
		t.Fatalf("Usage() = %q, want %q", got, want)
	}
}

func TestUsageTypeNames(t *testing.T) {
	// The usage names a type by its parser family, not by its Go type: every
	// signed width is "int", every unsigned width is "uint", both floats are
	// "float". A SubCommand renders as its own name with no brackets at all.
	res := &result{}
	cases := []struct {
		name string
		r    Runnable
		want string
	}{
		{name: "int8", r: int8Runnable{res: res}, want: "/pinned <V: int>"},
		{name: "uint8", r: uint8Runnable{res: res}, want: "/pinned <V: uint>"},
		{name: "float32", r: float32Runnable{res: res}, want: "/pinned <V: float>"},
		{name: "string", r: stringRunnable{res: res}, want: "/pinned <V: string>"},
		{name: "bool", r: boolRunnable{res: res}, want: "/pinned <V: bool>"},
		{name: "vec3", r: vec3Runnable{res: res}, want: "/pinned <V: x y z>"},
		{name: "varargs", r: varargsRunnable{res: res}, want: "/pinned <V: text>"},
		{name: "targets", r: targetsRunnable{res: res}, want: "/pinned <V: target>"},
		{name: "enum", r: enumRunnable{res: res}, want: "/pinned <V: PinnedEnum>"},
		{name: "subcommand", r: subCommandRunnable{res: res}, want: "/pinned Reload <V: int>"},
		{name: "optional", r: optionalRunnable{res: res}, want: "/pinned <Required: int> [Opt: string]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New("pinned", "", nil, c.r).Usage(); got != c.want {
				t.Fatalf("Usage() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParams(t *testing.T) {
	// Params describes each Runnable's parameters. The Value is a zero value of
	// the parameter type, unwrapped out of Optional, and Optional and Suffix
	// come from the field type and tag rather than from the value.
	res := &result{}
	command := New("pinned", "", nil, suffixRunnable{res: res})
	got := command.Params(&testSource{})
	want := [][]ParamInfo{{
		{Name: "amount", Value: 0, Optional: false, Suffix: "blocks"},
		{Name: "target", Value: "", Optional: false, Suffix: ""},
		{Name: "reason", Value: "", Optional: true, Suffix: "optional"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Params() = %#v, want %#v", got, want)
	}
}

// describingRunnable overrides reflection with an explicit parameter list.
type describingRunnable struct {
	V   int
	res *result
}

func (r describingRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }
func (describingRunnable) DescribeParams(Source) []ParamInfo {
	return []ParamInfo{{Name: "described", Value: "", Optional: true, Suffix: "!"}}
}

func TestParamDescriberWinsOverReflection(t *testing.T) {
	// PINNED CONSTRAINT: DescribeParams replaces the reflected parameter list
	// but not the usage string, which is still built from the struct fields in
	// New. A Runnable that describes itself therefore advertises one thing to
	// the client and another in its usage error.
	res := &result{}
	command := New("pinned", "", nil, describingRunnable{res: res})
	want := [][]ParamInfo{{{Name: "described", Value: "", Optional: true, Suffix: "!"}}}
	if got := command.Params(&testSource{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Params() = %#v, want %#v", got, want)
	}
	if got, want := command.Usage(), "/pinned <V: int>"; got != want {
		t.Fatalf("Usage() = %q, want %q: DescribeParams must not affect the usage", got, want)
	}
}

// allowedOnlyRunnable is runnable only by a source with the name "allowed".
type allowedOnlyRunnable struct {
	V   int
	res *result
}

func (r allowedOnlyRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }
func (allowedOnlyRunnable) Allow(src Source) bool {
	named, ok := src.(NamedTarget)
	return ok && named.Name() == "allowed"
}

func TestAllowerGatesExecution(t *testing.T) {
	// A disallowed Runnable is reported as an unknown command rather than as a
	// permission failure, which is deliberate: it hides the existence of the
	// command from a source that may not use it.
	res := &result{}
	out := execTx(t, "5", nil, &testSource{name: "allowed"}, allowedOnlyRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{5}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}

	res = &result{}
	out = execTx(t, "5", nil, &testSource{name: "denied"}, allowedOnlyRunnable{res: res})
	if got, want := errStrings(out), []string{errUnknown}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
	if res.runs != 0 {
		t.Fatalf("Run calls = %d, want 0 for a disallowed source", res.runs)
	}
}

func TestAllowerHidesOnlyTheDisallowedOverload(t *testing.T) {
	// A disallowed Runnable never contributes a leftover line, so when another
	// overload exists the error reported is the other one's. The unknown-command
	// message only survives when every overload was refused.
	res := &result{}
	out := execTx(t, "hello", nil, &testSource{name: "denied"}, allowedOnlyRunnable{res: res}, stringRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none: the permitted overload must still run", got)
	}
	if want := []any{"hello"}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}
}

func TestParamsAndRunnablesFilterByAllower(t *testing.T) {
	// Params and Runnables both drop the Runnables a source may not execute, and
	// Runnables keeps the original index so a caller can map a client-side
	// overload index back onto the Command.
	res := &result{}
	command := New("pinned", "", nil, allowedOnlyRunnable{res: res}, stringRunnable{res: res})

	denied := &testSource{name: "denied"}
	if got := command.Params(denied); len(got) != 1 {
		t.Fatalf("Params() returned %d overloads for a denied source, want 1", len(got))
	}
	runnables := command.Runnables(denied)
	if len(runnables) != 1 {
		t.Fatalf("Runnables() = %d entries, want 1", len(runnables))
	}
	if _, ok := runnables[1]; !ok {
		t.Fatalf("Runnables() = %v, want the surviving overload to keep index 1", runnables)
	}

	allowed := &testSource{name: "allowed"}
	if got := len(command.Runnables(allowed)); got != 2 {
		t.Fatalf("Runnables() = %d entries for an allowed source, want 2", got)
	}
}

func TestExecutePanicsOnANilSource(t *testing.T) {
	// PINNED BUG: Execute documents and enforces a nil Source with a panic. It is
	// the one input check the package makes, and it makes it the way CLAUDE.md
	// 2.2 forbids.
	res := &result{}
	v := mustPanic(t, func() { New("pinned", "", nil, intRunnable{res: res}).Execute("5", nil, nil) })
	if got, want := fmt.Sprint(v), "execute: invalid command source: source must not be nil"; got != want {
		t.Fatalf("panic = %q, want %q", got, want)
	}
}

func TestZeroCommandExecutesToANilError(t *testing.T) {
	// PINNED BUG: a zero Command has no Runnables, so the loop finds neither a
	// working overload nor an erroneous one and reports the nil error verbatim.
	// The source is sent an output whose only error renders as "<nil>".
	src := &testSource{}
	Command{}.Execute("anything", src, nil)
	if len(src.outputs) != 1 {
		t.Fatalf("SendCommandOutput calls = %d, want 1", len(src.outputs))
	}
	if got, want := errStrings(src.outputs[0]), []string{"<nil>"}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

func TestEveryFailureEmitsBothASyntaxAndACauseError(t *testing.T) {
	// PINNED CONSTRAINT: whenever at least one Runnable got as far as consuming
	// arguments, Execute adds the leftover line's syntax error before the actual
	// cause. Callers rendering an Output therefore always show two lines for one
	// mistake, and the first one is the less informative of the two.
	res := &result{}
	out := exec(t, "zz", intRunnable{res: res})
	if got := out.ErrorCount(); got != 2 {
		t.Fatalf("ErrorCount() = %d, want 2", got)
	}
	if got, want := errStrings(out), []string{
		"§r§cSyntax error: unexpected value: at \"/pinned >>zz<<\"§r",
		errNumber,
	}; !slices.Equal(got, want) {
		t.Fatalf("errors = %q, want %q", got, want)
	}

	// A source that may not run any overload is the only failure reported on its
	// own, because no Runnable ever produced a leftover line.
	out = execTx(t, "5", nil, &testSource{name: "denied"}, allowedOnlyRunnable{res: res})
	if got := out.ErrorCount(); got != 1 {
		t.Fatalf("ErrorCount() = %d, want 1 for a refused source", got)
	}
}

func TestNewLowercasesAndMutatesTheAliasSlice(t *testing.T) {
	// PINNED BUG: New lowercases the caller's alias slice in place and appends
	// the command name to it, so the slice the caller passed is modified. A
	// caller reusing one alias slice for several commands ends up with the names
	// of the earlier commands in the later ones.
	res := &result{}
	aliases := []string{"Alpha", "BETA"}
	command := New("Pinned", "", aliases, intRunnable{res: res})

	if got, want := command.Name(), "pinned"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
	if got, want := aliases, []string{"alpha", "beta"}; !slices.Equal(got, want) {
		t.Fatalf("the caller's slice = %q, want %q: New lowercases in place", got, want)
	}
	if got, want := command.Aliases(), []string{"alpha", "beta", "pinned"}; !slices.Equal(got, want) {
		t.Fatalf("Aliases() = %q, want %q", got, want)
	}
	if got, want := command.Description(), ""; got != want {
		t.Fatalf("Description() = %q, want %q", got, want)
	}
}

func TestAliasesAreEmptyWhenNoneWereGiven(t *testing.T) {
	// PINNED CONSTRAINT: the name is appended to the alias list only when the
	// list is non-empty, so Aliases() is empty for a command with no aliases
	// even though the name is always a usable alias. Any caller iterating
	// Aliases() to build a lookup table has to add the name itself.
	res := &result{}
	command := New("pinned", "", nil, intRunnable{res: res})
	if got := command.Aliases(); len(got) != 0 {
		t.Fatalf("Aliases() = %q, want empty", got)
	}
	command = New("pinned", "", []string{"pinned"}, intRunnable{res: res})
	if got, want := command.Aliases(), []string{"pinned"}; !slices.Equal(got, want) {
		t.Fatalf("Aliases() = %q, want %q: an alias equal to the name is not duplicated", got, want)
	}
}

type vec3AndMoreRunnable struct {
	Pos mgl64.Vec3
	V   int
	res *result
}

func (r vec3AndMoreRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.Pos, r.V}
}

func TestVec3ConsumesExactlyThreeArguments(t *testing.T) {
	// The Vec3 parser removes two arguments itself and leaves the third to
	// parseArgument, so a parameter after a Vec3 sees the fourth argument. A
	// change to that split silently shifts every later parameter.
	res := &result{}
	out := exec(t, "1 2 3 4", vec3AndMoreRunnable{res: res})
	if got := errStrings(out); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
	if want := []any{mgl64.Vec3{1, 2, 3}, 4}; !reflect.DeepEqual(res.args, want) {
		t.Fatalf("Run saw %#v, want %#v", res.args, want)
	}
}
