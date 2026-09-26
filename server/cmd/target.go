package cmd

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/world"
)

// Target represents the target of a command. A []Target may be used as command parameter
// types to allow passing targets to the command.
type Target interface {
	// Position returns the position of the Target as an mgl64.Vec3.
	Position() mgl64.Vec3
}

// NamedTarget is a Target that has a name.
type NamedTarget interface {
	Target
	// Name returns a name of the Target. Note that this name needs not to be and is not unique for a Target.
	Name() string
}

// targets returns all Targets selectable by the Source passed. Entities that
// do not implement the interface needed are skipped: no entity type is
// required to be a command target.
func targets(tx *world.Tx) (entities []Target, players []NamedTarget) {
	if tx == nil {
		return nil, nil
	}
	// Which input was the problem: an entity type that does not implement
	// Target, or a player-encoded type without a Name method. The slice-wide
	// assertion in sliceutil.Convert panicked on the first such element.
	// Comma-ok per element skips those instead. The @p and @a conversions
	// then only ever see slices that already satisfy the assertion, so
	// their results for conforming worlds are unchanged.
	for e := range tx.Entities() {
		if t, ok := e.(Target); ok {
			entities = append(entities, t)
		}
	}
	for e := range tx.Players() {
		if p, ok := e.(NamedTarget); ok {
			players = append(players, p)
		}
	}
	return entities, players
}
