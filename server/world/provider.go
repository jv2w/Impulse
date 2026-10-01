package world

import (
	"sync"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world/chunk"
	"io"
)

// Provider represents a value that may provide world data to a World value. It usually does the reading and
// writing of the world data so that the World may use it.
type Provider interface {
	io.Closer
	// Settings loads the settings for a World and returns them.
	Settings() *Settings
	// SaveSettings saves the settings of a World.
	SaveSettings(*Settings)

	// LoadPlayerSpawnPosition loads the player spawn point if found, otherwise an error will be returned.
	LoadPlayerSpawnPosition(uuid uuid.UUID) (pos cube.Pos, exists bool, err error)
	// SavePlayerSpawnPosition saves the player spawn point. In vanilla, this can be done with beds.
	SavePlayerSpawnPosition(uuid uuid.UUID, pos cube.Pos) error
	// LoadColumn reads a world.Column from the DB at a position in the DB. If
	// no column at that position exists, errors.Is(err, leveldb.ErrNotFound)
	// equals true.
	LoadColumn(pos ChunkPos) (*chunk.Column, error)
	// StoreColumn stores a world.Column at a position in the DB. An error is
	// returned if storing was unsuccessful.
	StoreColumn(pos ChunkPos, col *chunk.Column) error
}

// Compile time check to make sure NopProvider implements Provider.
var _ Provider = (*NopProvider)(nil)

// NopProvider implements a Provider that does not perform any disk I/O. It generates values on the run and
// dynamically, instead of reading and writing data, and otherwise returns empty values. A Settings struct can be passed
// to initialise a world with specific settings. Since Settings is a pointer, using the same NopProvider for multiple
// worlds means those worlds will share the same settings.
type NopProvider struct {
	Set *Settings
}

func (n NopProvider) Settings() *Settings {
	if n.Set == nil {
		return defaultSettings()
	}
	return n.Set
}
func (NopProvider) SaveSettings(*Settings) {}
func (NopProvider) LoadColumn(ChunkPos) (*chunk.Column, error) {
	return nil, leveldb.ErrNotFound
}
func (NopProvider) StoreColumn(ChunkPos, *chunk.Column) error { return nil }
func (NopProvider) LoadPlayerSpawnPosition(uuid.UUID) (cube.Pos, bool, error) {
	return cube.Pos{}, false, nil
}
func (NopProvider) SavePlayerSpawnPosition(uuid.UUID, cube.Pos) error { return nil }
func (NopProvider) Close() error                                      { return nil }

// lockedProvider wraps a Provider, serialising all calls for providers that
// are not safe for concurrent use.
type lockedProvider struct {
	mu sync.Mutex
	p  Provider
}

func (l *lockedProvider) Settings() *Settings {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.p.Settings()
}

func (l *lockedProvider) SaveSettings(s *Settings) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.p.SaveSettings(s)
}

func (l *lockedProvider) LoadPlayerSpawnPosition(id uuid.UUID) (cube.Pos, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.p.LoadPlayerSpawnPosition(id)
}

func (l *lockedProvider) SavePlayerSpawnPosition(id uuid.UUID, pos cube.Pos) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.p.SavePlayerSpawnPosition(id, pos)
}

func (l *lockedProvider) LoadColumn(pos ChunkPos) (*chunk.Column, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.p.LoadColumn(pos)
}

func (l *lockedProvider) StoreColumn(pos ChunkPos, col *chunk.Column) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.p.StoreColumn(pos, col)
}

func (l *lockedProvider) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.p.Close()
}
