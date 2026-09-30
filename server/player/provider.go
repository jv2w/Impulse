package player

import (
	"errors"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/world"
	"io"
)

// Provider represents a value that may provide data to a Player value. It usually does the reading and
// writing of the player data so that the Player may use it.
type Provider interface {
	// Save is called when the player leaves the server. The Data of the player is passed.
	Save(uuid uuid.UUID, data Config, w *world.World) error
	// Load is called when the player joins and passes the UUID of the player.
	// It expects to the player data, and an error that is nil if the player data could be found. If no data is
	// stored for the UUID, the error is ErrNoData or wraps it, and the player will use default values. Any other
	// error means data may exist but could not be read: the join is refused, so that the default values are not
	// saved over it when the player leaves.
	Load(uuid uuid.UUID, world func(world.Dimension) *world.World) (Config, *world.World, error)
	// Closer is used on server close when the server calls Provider.Close() and is used to safely close the Provider.
	io.Closer
}

// ErrNoData is returned by Provider.Load when no data is stored for a player.
var ErrNoData = errors.New("no player data stored")

// Compile time check to make sure NopProvider implements Provider.
var _ Provider = (*NopProvider)(nil)

// NopProvider is a player data provider that won't store any data and instead always return default values
type NopProvider struct{}

func (NopProvider) Save(uuid.UUID, Config, *world.World) error { return nil }
func (NopProvider) Load(uuid.UUID, func(world.Dimension) *world.World) (Config, *world.World, error) {
	return Config{}, nil, ErrNoData
}
func (NopProvider) Close() error { return nil }
