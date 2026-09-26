package cmd

import (
	"fmt"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/internal/sliceutil"
	"github.com/jv2w/Impulse/server/world"
	"math/rand/v2"
	"reflect"
	"strconv"
	"strings"
)

// Line represents a command line holding command arguments that were passed upon the execution of the
// command. It is a convenience wrapper around a string slice.
type Line struct {
	args []string
	seen []string
	src  Source
	cmd  Command
}

// SyntaxError returns a translated syntax error.
func (line *Line) SyntaxError() error {
	if len(line.args) == 0 {
		return MessageSyntax.F(strings.Join(line.seen, " "), "", "")
	}
	next := strings.Join(line.args[1:], " ")
	if next != "" {
		next = " " + next
	}
	return MessageSyntax.F(strings.Join(line.seen, " ")+" ", line.args[0], next)
}

// UsageError returns a translated usage error.
func (line *Line) UsageError() error {
	return MessageUsage.F(line.cmd.Usage())
}

// Next reads the next argument from the command line and returns it. If there were no more arguments to
// consume, false is returned.
func (line *Line) Next() (string, bool) {
	v, ok := line.NextN(1)
	if !ok {
		return "", false
	}
	return v[0], true
}

// NextN reads the next N arguments from the command line and returns them. If there were not enough arguments
// (n arguments), false is returned.
func (line *Line) NextN(n int) ([]string, bool) {
	if len(line.args) < n {
		return nil, false
	}
	v := line.args[:n]
	return v, true
}

// RemoveNext consumes the next argument from the command line.
func (line *Line) RemoveNext() {
	line.RemoveN(1)
}

// RemoveN consumes the next N arguments from the command line.
func (line *Line) RemoveN(n int) {
	if len(line.args) < n {
		line.args = nil
		return
	}
	line.seen = append(line.seen, line.args[:n]...)
	line.args = line.args[n:]
}

// Leftover takes the leftover arguments from the command line.
func (line *Line) Leftover() []string {
	v := line.args
	line.args = nil
	return v
}

// Len returns the leftover length of the arguments in the command line.
func (line *Line) Len() int {
	return len(line.args)
}

// parser manages the parsing of a Line, turning the raw arguments into values which are then stored in the
// struct fields.
type parser struct {
	currentField string
}

// parseArgument parses the next argument from the command line passed and sets it to value v passed. If
// parsing was not successful, an error is returned.
func (p parser) parseArgument(line *Line, v reflect.Value, optional bool, name string, source Source, tx *world.Tx) (error, bool) {
	var err error
	if line.Len() == 0 && optional {
		// The command run didn't have enough arguments for this parameter, but
		// it was optional, so it does not matter. Make sure to clear the value
		// though.
		v.Set(reflect.Zero(v.Type()))
		return nil, false
	}
	// Boxed after the early return: this path never looked at i, so the
	// interface value it allocated was always thrown away.
	i := v.Interface()
	switch i.(type) {
	case int, int8, int16, int32, int64:
		err = p.int(line, v)
	case uint, uint8, uint16, uint32, uint64:
		err = p.uint(line, v)
	case float32, float64:
		err = p.float(line, v)
	case string:
		err = p.string(line, v)
	case bool:
		err = p.bool(line, v)
	case mgl64.Vec3:
		err = p.vec3(line, v)
	case Varargs:
		err = p.varargs(line, v)
	case []Target:
		err = p.targets(line, v, tx)
	case SubCommand:
		err = p.sub(line, name)
	default:
		if param, ok := i.(Parameter); ok {
			err = param.Parse(line, v)
			break
		}
		if enum, ok := i.(Enum); ok {
			err = p.enum(line, v, enum, source)
			break
		}
		panic(fmt.Sprintf("non-command parameter type %T in command structure", i))
	}
	if err == nil {
		// The argument was parsed successfully, so it needs to be removed from the command line.
		line.RemoveNext()
	}
	return err, err == nil
}

// int ...
func (p parser) int(line *Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	value, err := strconv.ParseInt(arg, 10, v.Type().Bits())
	if err != nil {
		return MessageNumberInvalid.F(arg)
	}
	v.SetInt(value)
	return nil
}

// uint ...
func (p parser) uint(line *Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	value, err := strconv.ParseUint(arg, 10, v.Type().Bits())
	if err != nil {
		return MessageNumberInvalid.F(arg)
	}
	v.SetUint(value)
	return nil
}

// float ...
func (p parser) float(line *Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	value, err := strconv.ParseFloat(arg, v.Type().Bits())
	if err != nil {
		return MessageNumberInvalid.F(arg)
	}
	v.SetFloat(value)
	return nil
}

// string ...
func (p parser) string(line *Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	v.SetString(arg)
	return nil
}

// bool ...
func (p parser) bool(line *Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	value, err := strconv.ParseBool(arg)
	if err != nil {
		return MessageBooleanInvalid.F(arg)
	}
	v.SetBool(value)
	return nil
}

// enum ...
func (p parser) enum(line *Line, val reflect.Value, v Enum, source Source) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	// A plain loop stops at the same first match slices.IndexFunc would report,
	// but does not allocate a closure capturing arg on every call.
	for _, opt := range v.Options(source) {
		if strings.EqualFold(opt, arg) {
			val.SetString(opt)
			return nil
		}
	}
	return MessageParameterInvalid.F(arg)
}

// sub reads verifies a SubCommand against the next argument.
func (p parser) sub(line *Line, name string) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	if strings.EqualFold(name, arg) {
		return nil
	}
	return MessageParameterInvalid.F(arg)
}

// vec3 ...
func (p parser) vec3(line *Line, v reflect.Value) error {
	if err := p.float(line, v.Index(0)); err != nil {
		return err
	}
	line.RemoveNext()
	if err := p.float(line, v.Index(1)); err != nil {
		return err
	}
	line.RemoveNext()
	return p.float(line, v.Index(2))
}

// varargs ...
func (p parser) varargs(line *Line, v reflect.Value) error {
	v.SetString(strings.Join(line.Leftover(), " "))
	return nil
}

// targets ...
func (p parser) targets(line *Line, v reflect.Value, tx *world.Tx) error {
	targets, err := p.parseTargets(line, tx)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return MessageNoTargets.F()
	}
	v.Set(reflect.ValueOf(targets))
	return nil
}

// parseTargets parses one or more Targets from the Line passed.
func (p parser) parseTargets(line *Line, tx *world.Tx) ([]Target, error) {
	entities, players := targets(tx)
	first, ok := line.Next()
	if !ok {
		return nil, line.UsageError()
	}
	if strings.HasPrefix(first, "@") && tx == nil {
		return nil, MessageNoTargets.F()
	}
	switch first[:min(len(first), 2)] {
	case "@p":
		if len(players) == 0 {
			return nil, nil
		}
		pos := line.src.Position()
		// Which input was the problem: any player order that needed a swap.
		// The old code sorted players while reading distances from a
		// separate slice by index, so after the first swap the comparison
		// read the distance of the player no longer at that index. A minimum
		// scan keeps no second slice: only one player is ever returned, so
		// the rest of the order is unobservable and sorting it was waste.
		// Ties stay order-dependent, as they were under the unstable sort.
		best := 0
		bestDist := players[0].Position().Sub(pos).Len()
		for i, p := range players[1:] {
			if d := p.Position().Sub(pos).Len(); d < bestDist {
				best, bestDist = i+1, d
			}
		}
		return sliceutil.Convert[Target](players[best : best+1]), nil
	case "@e":
		return entities, nil
	case "@a":
		return sliceutil.Convert[Target](players), nil
	case "@s":
		return []Target{line.src}, nil
	case "@r":
		if len(players) == 0 {
			return nil, nil
		}
		return []Target{players[rand.IntN(len(players))]}, nil
	default:
		target, err := p.parsePlayer(first, players)
		if err != nil {
			return nil, err
		}
		return []Target{target}, nil
	}
}

// parsePlayer attempts to find a target whose name matches the name passed.
func (p parser) parsePlayer(name string, players []NamedTarget) (Target, error) {
	// A plain loop returns the same first match slices.IndexFunc would find,
	// without allocating a closure capturing name on every call.
	for _, target := range players {
		if strings.EqualFold(target.Name(), name) {
			return target, nil
		}
	}
	return nil, MessagePlayerNotFound.F()
}
