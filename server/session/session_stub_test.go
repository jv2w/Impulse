package session

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The stubs in this file are the whole reason server/session had no tests
// beyond item conversion: Controllable has 70 methods and cannot be faked, but
// Conn has ten and world.Entity has four. Everything reachable without a
// Controllable is pinned through these.

// stubConn is a Conn that records nothing and answers every query with a
// configured constant. ReadPacket blocks until the test closes read.
type stubConn struct {
	identity     login.IdentityData
	client       login.ClientData
	cacheEnabled bool
	radius       int
	latency      time.Duration

	// written collects packets handed to WritePacket. A Session built by
	// newStubSession never reaches it: writePacket queues on s.packets and the
	// forwarding goroutine only exists in Config.New.
	written []packet.Packet
	read    chan packet.Packet
	closed  bool
}

func (c *stubConn) IdentityData() login.IdentityData { return c.identity }
func (c *stubConn) ClientData() login.ClientData     { return c.client }
func (c *stubConn) ClientCacheEnabled() bool         { return c.cacheEnabled }
func (c *stubConn) ChunkRadius() int                 { return c.radius }
func (c *stubConn) Latency() time.Duration           { return c.latency }
func (c *stubConn) Flush() error                     { return nil }
func (c *stubConn) RemoteAddr() net.Addr             { return stubAddr{} }
func (c *stubConn) Close() error                     { c.closed = true; return nil }

func (c *stubConn) ReadPacket() (packet.Packet, error) { return <-c.read, nil }

func (c *stubConn) WritePacket(pk packet.Packet) error {
	c.written = append(c.written, pk)
	return nil
}

func (c *stubConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }

type stubAddr struct{}

func (stubAddr) Network() string { return "stub" }
func (stubAddr) String() string  { return "stub:0" }

// newStubSession builds the smallest Session on which writePacket works. It
// deliberately does not run Config.New: that opens a goroutine draining
// s.packets into the Conn, which would make packet assertions racy. Packets
// stay in the buffered channel and are read back with sentPackets.
func newStubSession(t *testing.T, conn *stubConn) *Session {
	t.Helper()
	if conn == nil {
		conn = &stubConn{}
	}
	s := &Session{
		conf:            Config{Log: slog.New(slog.DiscardHandler)},
		conn:            conn,
		packets:         make(chan packet.Packet, packetBufferSize),
		closeBackground: make(chan struct{}),
		handlers:        map[uint32]packetHandler{},
		blobs:           map[uint64][]byte{},

		entityRuntimeIDs: map[*world.EntityHandle]uint64{},
		entities:         map[uint64]*world.EntityHandle{},
		hiddenEntities:   map[uuid.UUID]struct{}{},

		currentEntityRuntimeID: selfEntityRuntimeID,
	}
	var name string
	var lines []string
	s.currentScoreboard.Store(&name)
	s.currentLines.Store(&lines)
	return s
}

// sentPackets drains every packet the Session has queued so far. It never
// blocks: writePacket is synchronous into a buffered channel, so anything a
// call under test wrote is already there when it returns.
func sentPackets(t *testing.T, s *Session) []packet.Packet {
	t.Helper()
	var pks []packet.Packet
	for {
		select {
		case pk := <-s.packets:
			pks = append(pks, pk)
		default:
			return pks
		}
	}
}

// onlyPacket asserts the Session wrote exactly one packet and returns it.
func onlyPacket(t *testing.T, s *Session) packet.Packet {
	t.Helper()
	pks := sentPackets(t, s)
	if len(pks) != 1 {
		t.Fatalf("session wrote %d packets, want exactly 1: %#v", len(pks), pks)
	}
	return pks[0]
}

// stubEntityType is a world.EntityType with a configurable bounding box and
// encoded name. Open returns nil: nothing in the metadata path opens an entity.
type stubEntityType struct {
	name string
	bbox cube.BBox
}

func (stubEntityType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	return nil
}
func (t stubEntityType) EncodeEntity() string                      { return t.name }
func (t stubEntityType) BBox(world.Entity) cube.BBox               { return t.bbox }
func (stubEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (stubEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// offsetEntityType is a stubEntityType that also satisfies OffsetEntity.
type offsetEntityType struct {
	stubEntityType
	offset float64
}

func (t offsetEntityType) NetworkOffset() float64 { return t.offset }

// stubEntityConf leaves every field of world.EntityData at its default.
type stubEntityConf struct{}

func (stubEntityConf) Apply(*world.EntityData) {}

// stubEntity is a world.Entity whose only real content is its handle. The
// metadata path reads the handle's type; position and rotation are constants.
type stubEntity struct {
	handle *world.EntityHandle
	pos    mgl64.Vec3
	rot    cube.Rotation
}

func (e *stubEntity) H() *world.EntityHandle { return e.handle }
func (e *stubEntity) Position() mgl64.Vec3   { return e.pos }
func (e *stubEntity) Rotation() cube.Rotation {
	return e.rot
}
func (e *stubEntity) Close() error { return nil }

// newStubEntity creates a world.Entity backed by a handle of the type passed.
// world.NewEntity builds a handle without a world, which is all the metadata
// and offset paths need.
func newStubEntity(t *testing.T, typ world.EntityType) *stubEntity {
	t.Helper()
	return &stubEntity{handle: world.NewEntity(typ, stubEntityConf{})}
}
