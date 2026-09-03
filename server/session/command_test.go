package session

import (
	"reflect"
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/cmd"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/text/language"
)

// Command conversion is one-way: the client is told what parameter types a
// command takes and never sends the description back. The numbers below are the
// contract -- the client resolves a parameter's editor and autocompletion from
// the type word alone -- so they are asserted as literals.

// Golden command argument type words.
const (
	goldenArgTypeInt      = 1
	goldenArgTypeFloat    = 3
	goldenArgTypeRValue   = 4
	goldenArgTypeTarget   = 8
	goldenArgTypeString   = 56
	goldenArgTypePosition = 65
	goldenArgTypeRawText  = 70

	goldenArgValid    = 0x100000
	goldenArgEnum     = 0x200000
	goldenArgSuffixed = 0x1000000
	goldenArgSoftEnum = 0x4000000
)

// TestCommandArgGoldenValues pins the protocol constants the conversion emits.
func TestCommandArgGoldenValues(t *testing.T) {
	types := map[string][2]uint32{
		"Int":      {protocol.CommandArgTypeInt, goldenArgTypeInt},
		"Float":    {protocol.CommandArgTypeFloat, goldenArgTypeFloat},
		"RValue":   {protocol.CommandArgTypeRValue, goldenArgTypeRValue},
		"Target":   {protocol.CommandArgTypeTarget, goldenArgTypeTarget},
		"String":   {protocol.CommandArgTypeString, goldenArgTypeString},
		"Position": {protocol.CommandArgTypePosition, goldenArgTypePosition},
		"RawText":  {protocol.CommandArgTypeRawText, goldenArgTypeRawText},

		"Valid":    {protocol.CommandArgValid, goldenArgValid},
		"Enum":     {protocol.CommandArgEnum, goldenArgEnum},
		"Suffixed": {protocol.CommandArgSuffixed, goldenArgSuffixed},
		"SoftEnum": {protocol.CommandArgSoftEnum, goldenArgSoftEnum},
	}
	for name, pair := range types {
		if pair[0] != pair[1] {
			t.Errorf("protocol.CommandArg%s = %#x, want the golden %#x", name, pair[0], pair[1])
		}
	}
}

// stubSource is a cmd.Source: a position and a place to put command output. It
// is not a Controllable, which is why the command conversion is testable at all.
type stubSource struct {
	pos     mgl64.Vec3
	outputs []*cmd.Output
}

func (s *stubSource) Position() mgl64.Vec3            { return s.pos }
func (s *stubSource) SendCommandOutput(o *cmd.Output) { s.outputs = append(s.outputs, o) }

// stubCmdEnum is a cmd.Enum whose options are fixed.
type stubCmdEnum string

func (stubCmdEnum) Type() string                { return "StubEnum" }
func (stubCmdEnum) Options(cmd.Source) []string { return []string{"alpha", "beta"} }

// sourceEnum is a cmd.Enum whose options depend on the Source, which is how
// soft enums are meant to be used.
type sourceEnum string

func (sourceEnum) Type() string { return "SourceEnum" }
func (sourceEnum) Options(src cmd.Source) []string {
	if src.Position()[0] > 0 {
		return []string{"east"}
	}
	return []string{"west"}
}

// TestValueToParamType pins the type word each Go parameter type maps to, and
// which types produce an enum instead of a type word.
func TestValueToParamType(t *testing.T) {
	tests := []struct {
		name     string
		info     cmd.ParamInfo
		wantType uint32
		wantEnum commandEnum
	}{
		{name: "int", info: cmd.ParamInfo{Value: int(0)}, wantType: goldenArgTypeInt},
		{name: "int8", info: cmd.ParamInfo{Value: int8(0)}, wantType: goldenArgTypeInt},
		{name: "int16", info: cmd.ParamInfo{Value: int16(0)}, wantType: goldenArgTypeInt},
		{name: "int32", info: cmd.ParamInfo{Value: int32(0)}, wantType: goldenArgTypeInt},
		{name: "int64", info: cmd.ParamInfo{Value: int64(0)}, wantType: goldenArgTypeInt},
		{name: "uint", info: cmd.ParamInfo{Value: uint(0)}, wantType: goldenArgTypeInt},
		{name: "uint8", info: cmd.ParamInfo{Value: uint8(0)}, wantType: goldenArgTypeInt},
		{name: "uint16", info: cmd.ParamInfo{Value: uint16(0)}, wantType: goldenArgTypeInt},
		{name: "uint32", info: cmd.ParamInfo{Value: uint32(0)}, wantType: goldenArgTypeInt},
		{name: "uint64", info: cmd.ParamInfo{Value: uint64(0)}, wantType: goldenArgTypeInt},

		{name: "float32", info: cmd.ParamInfo{Value: float32(0)}, wantType: goldenArgTypeFloat},
		{name: "float64", info: cmd.ParamInfo{Value: float64(0)}, wantType: goldenArgTypeFloat},

		{name: "string", info: cmd.ParamInfo{Value: ""}, wantType: goldenArgTypeString},
		{name: "varargs", info: cmd.ParamInfo{Value: cmd.Varargs("")}, wantType: goldenArgTypeRawText},
		{
			name:     "a concrete target",
			info:     cmd.ParamInfo{Value: cmd.Target(&stubSource{})},
			wantType: goldenArgTypeTarget,
		},
		{name: "target slice", info: cmd.ParamInfo{Value: []cmd.Target(nil)}, wantType: goldenArgTypeTarget},
		{
			// PINNED BUG: cmd.Command.Params fills Value with the zero value of
			// the field type, and the zero value of the Target interface is a
			// nil interface with no dynamic type. The type switch therefore
			// misses every case and a bare Target parameter is advertised as a
			// raw value instead of a target. Only []Target survives, because a
			// nil slice still carries its type.
			name:     "a nil target falls through to a raw value",
			info:     cmd.ParamInfo{Value: cmd.Target(nil)},
			wantType: goldenArgTypeRValue,
		},
		{name: "vec3", info: cmd.ParamInfo{Value: mgl64.Vec3{}}, wantType: goldenArgTypePosition},

		{
			name:     "bool becomes a two option enum",
			info:     cmd.ParamInfo{Value: false},
			wantEnum: commandEnum{Type: "Boolean", Options: []string{"true", "false"}},
		},
		{
			// The sub-command enum is named after the parameter, so two
			// sub-commands with the same field name share one enum.
			name:     "sub command becomes an enum named after the parameter",
			info:     cmd.ParamInfo{Name: "reload", Value: cmd.SubCommand{}},
			wantEnum: commandEnum{Type: "SubCommandreload", Options: []string{"reload"}},
		},
		{
			name:     "cmd.Enum reports its own type and options",
			info:     cmd.ParamInfo{Value: stubCmdEnum("")},
			wantEnum: commandEnum{Type: "StubEnum", Options: []string{"alpha", "beta"}},
		},
		{
			// PINNED SCOPE: anything the switch does not name falls through to
			// the raw value type rather than being rejected. Out of scope.
			name:     "an unknown type falls through to a raw value",
			info:     cmd.ParamInfo{Value: struct{}{}},
			wantType: goldenArgTypeRValue,
		},
		{
			name:     "a nil value falls through to a raw value",
			info:     cmd.ParamInfo{},
			wantType: goldenArgTypeRValue,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotType, gotEnum := valueToParamType(test.info, &stubSource{})
			if gotType != test.wantType {
				t.Fatalf("type = %#x, want %#x", gotType, test.wantType)
			}
			if !reflect.DeepEqual(gotEnum, test.wantEnum) {
				t.Fatalf("enum = %#v, want %#v", gotEnum, test.wantEnum)
			}
		})
	}
}

// TestValueToParamTypeEnumOptionsUseTheSource pins that the enum's options are
// resolved against the Source passed, not cached, so two players can be shown
// different options for the same enum.
func TestValueToParamTypeEnumOptionsUseTheSource(t *testing.T) {
	info := cmd.ParamInfo{Value: sourceEnum("")}

	_, east := valueToParamType(info, &stubSource{pos: mgl64.Vec3{1, 0, 0}})
	if got, want := east.Options, []string{"east"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}
	_, west := valueToParamType(info, &stubSource{pos: mgl64.Vec3{-1, 0, 0}})
	if got, want := west.Options, []string{"west"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}
}

// The runnables below are the parameter shapes BuildAvailableCommands is pinned
// against. Each is a struct whose exported fields become command parameters.

type intRunnable struct {
	Amount int
}

func (intRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type mixedRunnable struct {
	Amount int
	Name   string
	Flag   bool
}

func (mixedRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type optionalRunnable struct {
	Amount cmd.Optional[int]
}

func (optionalRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type suffixedRunnable struct {
	Amount int `cmd:"amount,L"`
}

func (suffixedRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type enumRunnable struct {
	Choice stubCmdEnum
}

func (enumRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type sourceEnumRunnable struct {
	Choice sourceEnum
}

func (sourceEnumRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// deniedRunnable refuses every source, so a command made only of it has no
// runnables and is left out of the packet entirely.
type deniedRunnable struct{}

func (deniedRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}
func (deniedRunnable) Allow(cmd.Source) bool                  { return false }

// TestBuildAvailableCommandsSingleCommand pins the whole packet for one command
// with one integer parameter: the parameter type word is the base type ORed
// with the valid bit, and a command with no aliases gets the sentinel offset.
func TestBuildAvailableCommandsSingleCommand(t *testing.T) {
	commands := map[string]cmd.Command{
		"give": cmd.New("give", "Gives an item", nil, intRunnable{}),
	}

	pk, runnables := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	want := &packet.AvailableCommands{
		Commands: []protocol.Command{{
			Name:            "give",
			Description:     "Gives an item",
			AliasesOffset:   0xffffffff,
			PermissionLevel: protocol.CommandPermissionLevelAny,
			Overloads: []protocol.CommandOverload{{
				Parameters: []protocol.CommandParameter{{
					Name: "Amount",
					Type: goldenArgTypeInt | goldenArgValid,
				}},
			}},
		}},
		DynamicEnums: []protocol.DynamicEnum{},
		EnumValues:   []string{},
		Enums:        []protocol.CommandEnum{},
	}
	if !reflect.DeepEqual(pk, want) {
		t.Fatalf("packet = %#v, want %#v", pk, want)
	}
	if len(runnables) != 1 {
		t.Fatalf("runnables = %#v, want one entry", runnables)
	}
	if _, ok := runnables["give"]; !ok {
		t.Fatalf("runnables has no entry for give: %#v", runnables)
	}
}

// TestBuildAvailableCommandsParameterTypes pins the type word of each parameter
// shape as it appears in the packet, including that the parameter name comes
// from the struct field.
func TestBuildAvailableCommandsParameterTypes(t *testing.T) {
	commands := map[string]cmd.Command{
		"c": cmd.New("c", "", nil, mixedRunnable{}),
	}

	pk, _ := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	params := pk.Commands[0].Overloads[0].Parameters
	want := []protocol.CommandParameter{
		{Name: "Amount", Type: goldenArgTypeInt | goldenArgValid},
		{Name: "Name", Type: goldenArgTypeString | goldenArgValid},
		// A bool is an enum, so its type word carries the enum bit and the
		// enum's index rather than a base type.
		{Name: "Flag", Type: goldenArgValid | goldenArgEnum | 0},
	}
	if !reflect.DeepEqual(params, want) {
		t.Fatalf("parameters = %#v, want %#v", params, want)
	}
	if got, want := pk.Enums, []protocol.CommandEnum{{
		Type:         "Boolean",
		ValueIndices: []uint32{0, 1},
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enums = %#v, want %#v", got, want)
	}
	if got, want := pk.EnumValues, []string{"true", "false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enum values = %#v, want %#v", got, want)
	}
}

// TestBuildAvailableCommandsOptional pins that an optional parameter keeps its
// unwrapped type and is marked optional.
func TestBuildAvailableCommandsOptional(t *testing.T) {
	commands := map[string]cmd.Command{
		"c": cmd.New("c", "", nil, optionalRunnable{}),
	}

	pk, _ := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	want := []protocol.CommandParameter{{
		Name:     "Amount",
		Type:     goldenArgTypeInt | goldenArgValid,
		Optional: true,
	}}
	if got := pk.Commands[0].Overloads[0].Parameters; !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters = %#v, want %#v", got, want)
	}
}

// TestBuildAvailableCommandsSuffix pins the suffix branch, which is the only
// place the base type word is discarded.
//
// PINNED BUG: a suffixed parameter's type is overwritten with
// CommandArgSuffixed|index, dropping both the base type (int here) and the
// CommandArgValid bit that every other branch sets.
func TestBuildAvailableCommandsSuffix(t *testing.T) {
	commands := map[string]cmd.Command{
		"c": cmd.New("c", "", nil, suffixedRunnable{}),
	}

	pk, _ := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	want := []protocol.CommandParameter{{
		Name: "amount",
		Type: goldenArgSuffixed | 0,
	}}
	if got := pk.Commands[0].Overloads[0].Parameters; !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters = %#v, want %#v", got, want)
	}
	if got, want := pk.Suffixes, []string{"L"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("suffixes = %#v, want %#v", got, want)
	}
}

// TestBuildAvailableCommandsAliases pins that a command's aliases become an
// enum named after the command, that the command's own name is appended to its
// alias list, and that the alias entries themselves are skipped so the command
// appears once.
func TestBuildAvailableCommandsAliases(t *testing.T) {
	command := cmd.New("teleport", "", []string{"tp"}, intRunnable{})
	commands := map[string]cmd.Command{"teleport": command, "tp": command}

	pk, runnables := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	if len(pk.Commands) != 1 {
		t.Fatalf("packet holds %d commands, want 1: %#v", len(pk.Commands), pk.Commands)
	}
	if got := pk.Commands[0].AliasesOffset; got != 0 {
		t.Fatalf("aliases offset = %d, want 0", got)
	}
	if got, want := pk.Enums, []protocol.CommandEnum{{
		Type:         "teleportAliases",
		ValueIndices: []uint32{0, 1},
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enums = %#v, want %#v", got, want)
	}
	if got, want := pk.EnumValues, []string{"tp", "teleport"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enum values = %#v, want %#v", got, want)
	}
	if len(runnables) != 1 {
		t.Fatalf("runnables = %#v, want a single entry for the command name", runnables)
	}
}

// TestBuildAvailableCommandsSoftEnum pins the soft enum branch: an enum listed
// in softEnums is sent as a dynamic enum the server can update later, and the
// parameter carries the soft enum bit rather than the enum bit.
func TestBuildAvailableCommandsSoftEnum(t *testing.T) {
	commands := map[string]cmd.Command{
		"c": cmd.New("c", "", nil, sourceEnumRunnable{}),
	}
	soft := map[string]struct{}{"SourceEnum": {}}

	pk, _ := BuildAvailableCommands(commands, &stubSource{pos: mgl64.Vec3{1, 0, 0}}, soft)

	want := []protocol.CommandParameter{{
		Name: "Choice",
		Type: goldenArgValid | goldenArgSoftEnum | 0,
	}}
	if got := pk.Commands[0].Overloads[0].Parameters; !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters = %#v, want %#v", got, want)
	}
	if got, want := pk.DynamicEnums, []protocol.DynamicEnum{{
		Type:   "SourceEnum",
		Values: []string{"east"},
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dynamic enums = %#v, want %#v", got, want)
	}
	if len(pk.Enums) != 0 {
		t.Fatalf("a soft enum also landed in the static enum list: %#v", pk.Enums)
	}
}

// TestBuildAvailableCommandsEnumValuesAreDeduplicated pins that two enums
// sharing an option point at the same entry in the value table.
func TestBuildAvailableCommandsEnumValuesAreDeduplicated(t *testing.T) {
	commands := map[string]cmd.Command{
		"c": cmd.New("c", "", nil, mixedRunnable{}, boolPairRunnable{}),
	}

	pk, _ := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	if got, want := pk.EnumValues, []string{"true", "false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enum values = %#v, want %#v: both bools share the Boolean enum", got, want)
	}
	if len(pk.Enums) != 1 {
		t.Fatalf("enums = %#v, want a single shared Boolean enum", pk.Enums)
	}
}

type boolPairRunnable struct {
	First  bool
	Second bool
}

func (boolPairRunnable) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// TestBuildAvailableCommandsOverloads pins that each Runnable of a command
// becomes one overload, in the order the runnables were registered.
func TestBuildAvailableCommandsOverloads(t *testing.T) {
	commands := map[string]cmd.Command{
		"c": cmd.New("c", "", nil, intRunnable{}, optionalRunnable{}),
	}

	pk, _ := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	overloads := pk.Commands[0].Overloads
	if len(overloads) != 2 {
		t.Fatalf("overloads = %#v, want 2", overloads)
	}
	if overloads[0].Parameters[0].Optional {
		t.Fatal("the first overload's parameter is optional, want required")
	}
	if !overloads[1].Parameters[0].Optional {
		t.Fatal("the second overload's parameter is required, want optional")
	}
}

// TestBuildAvailableCommandsSkipsUnrunnableCommands pins that a command the
// source may not run is left out of the packet entirely rather than sent and
// rejected later.
func TestBuildAvailableCommandsSkipsUnrunnableCommands(t *testing.T) {
	commands := map[string]cmd.Command{
		"denied":  cmd.New("denied", "", nil, deniedRunnable{}),
		"allowed": cmd.New("allowed", "", nil, intRunnable{}),
	}

	pk, runnables := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})

	if len(pk.Commands) != 1 || pk.Commands[0].Name != "allowed" {
		t.Fatalf("packet commands = %#v, want only allowed", pk.Commands)
	}
	if _, ok := runnables["denied"]; ok {
		t.Fatalf("runnables include the denied command: %#v", runnables)
	}
}

// TestBuildAvailableCommandsEmpty pins the packet built from no commands. Every
// list is allocated but empty, never nil, except the command list itself.
func TestBuildAvailableCommandsEmpty(t *testing.T) {
	pk, runnables := BuildAvailableCommands(map[string]cmd.Command{}, &stubSource{}, map[string]struct{}{})

	if pk.Commands != nil {
		t.Fatalf("commands = %#v, want nil", pk.Commands)
	}
	if pk.DynamicEnums == nil || len(pk.DynamicEnums) != 0 {
		t.Fatalf("dynamic enums = %#v, want an empty non-nil slice", pk.DynamicEnums)
	}
	if pk.Enums == nil || len(pk.Enums) != 0 {
		t.Fatalf("enums = %#v, want an empty non-nil slice", pk.Enums)
	}
	if pk.EnumValues == nil || len(pk.EnumValues) != 0 {
		t.Fatalf("enum values = %#v, want an empty non-nil slice", pk.EnumValues)
	}
	if len(runnables) != 0 {
		t.Fatalf("runnables = %#v, want empty", runnables)
	}
}

// TestBuildAvailableCommandsOrderIsNotDeterministic pins that the packet's
// command order follows a map range and therefore varies between calls. Nothing
// may depend on the order, and the enum indices baked into parameter type words
// vary with it.
//
// PINNED CONSTRAINT: two calls with identical input can produce packets that
// differ field by field. A test comparing whole packets must use a single
// command or sort first.
func TestBuildAvailableCommandsOrderIsNotDeterministic(t *testing.T) {
	commands := map[string]cmd.Command{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		commands[name] = cmd.New(name, "", nil, intRunnable{})
	}

	seen := map[string]struct{}{}
	for range 60 {
		pk, _ := BuildAvailableCommands(commands, &stubSource{}, map[string]struct{}{})
		names := make([]string, 0, len(pk.Commands))
		for _, c := range pk.Commands {
			names = append(names, c.Name)
		}
		seen[joinCommandNames(names)] = struct{}{}

		sorted := slices.Clone(names)
		slices.Sort(sorted)
		if want := []string{"a", "b", "c", "d", "e", "f", "g", "h"}; !reflect.DeepEqual(sorted, want) {
			t.Fatalf("commands = %#v, want all eight regardless of order", sorted)
		}
	}
	if len(seen) < 2 {
		t.Fatalf("command order was identical across 60 builds (%d distinct orders); "+
			"a map range over eight commands was expected to vary", len(seen))
	}
}

func joinCommandNames(names []string) string {
	out := ""
	for _, n := range names {
		out += n + ","
	}
	return out
}

// TestSendCommandOutputOnNopIsANoOp pins the Nop guard. Nop has no handler
// table, so without the guard the type assertion on the command request handler
// would panic.
func TestSendCommandOutputOnNopIsANoOp(t *testing.T) {
	Nop.SendCommandOutput(&cmd.Output{}, language.English)
}

// TestSendCommandOutput pins the command output packet: successful messages
// first, then errors, with the success count covering only the messages.
func TestSendCommandOutput(t *testing.T) {
	s := newStubSession(t, nil)
	s.registerHandlers()

	out := &cmd.Output{}
	out.Print("first")
	out.Print("second")
	out.Error("bad")

	s.SendCommandOutput(out, language.English)

	got := onlyPacket(t, s).(*packet.CommandOutput)
	if got.OutputType != packet.CommandOutputTypeAllOutput {
		t.Fatalf("output type = %d, want %d", got.OutputType, packet.CommandOutputTypeAllOutput)
	}
	if got.SuccessCount != 2 {
		t.Fatalf("success count = %d, want 2: errors do not count", got.SuccessCount)
	}
	want := []protocol.CommandOutputMessage{
		{Success: true, Message: "first"},
		{Success: true, Message: "second"},
		{Success: false, Message: "bad"},
	}
	if !reflect.DeepEqual(got.OutputMessages, want) {
		t.Fatalf("messages = %#v, want %#v", got.OutputMessages, want)
	}
}
