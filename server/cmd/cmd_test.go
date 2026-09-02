package cmd

import (
	"context"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// This file holds the fixtures shared by the behaviour pinning tests of the
// cmd package. The package is almost entirely reflection: every parameter type,
// every struct tag and every unsupported field type is resolved at run time, so
// the compiler checks none of it. Stage 1 removes the process-global command
// table (register.go) and reworks the reflection walk; these tests exist so
// that the rework has to change a documented decision rather than an
// undocumented one.

// result records what a test Runnable saw when it was run. Test Runnables hold
// a pointer to one in an unexported field, which the reflection walk skips, so
// the recorder never becomes a command parameter itself.
type result struct {
	runs int
	args []any
}

// testSource is a Source that records every Output it is sent. It is a
// NamedTarget as well, so it may be selected with @s.
type testSource struct {
	pos     mgl64.Vec3
	name    string
	outputs []*Output
}

func (s *testSource) Position() mgl64.Vec3        { return s.pos }
func (s *testSource) Name() string                { return s.name }
func (s *testSource) SendCommandOutput(o *Output) { s.outputs = append(s.outputs, o) }

// exec runs args through a Command built from the Runnables passed with a nil
// world.Tx and returns the single Output the source was sent. It fails the test
// if Execute did not send exactly one Output, since Execute promises to send
// one on every path.
func exec(t *testing.T, args string, r ...Runnable) *Output {
	t.Helper()
	return execTx(t, args, nil, &testSource{}, r...)
}

// execTx is exec with an explicit transaction and source, for the target
// selector tests that need a world.
func execTx(t *testing.T, args string, tx *world.Tx, src *testSource, r ...Runnable) *Output {
	t.Helper()
	New("pinned", "pinned command", nil, r...).Execute(args, src, tx)
	if len(src.outputs) != 1 {
		t.Fatalf("SendCommandOutput calls = %d, want exactly 1", len(src.outputs))
	}
	return src.outputs[0]
}

// errStrings returns the fallback text of every error on the Output. The cmd
// package builds its errors out of chat translations, which hold a slice and
// are therefore not comparable: errors.Is can never match one, so the only
// identity an error has is its rendered text.
func errStrings(o *Output) []string {
	s := make([]string, 0, len(o.Errors()))
	for _, err := range o.Errors() {
		s = append(s, err.Error())
	}
	return s
}

// messageStrings returns the rendered text of every success message.
func messageStrings(o *Output) []string {
	s := make([]string, 0, len(o.Messages()))
	for _, m := range o.Messages() {
		s = append(s, m.String())
	}
	return s
}

// mustPanic runs f and returns the value it panicked with. It fails the test if
// f returned normally.
func mustPanic(t *testing.T, f func()) (v any) {
	t.Helper()
	defer func() { v = recover() }()
	f()
	t.Fatal("expected a panic, but the call returned normally")
	return nil
}

// syncWorld returns a world that runs its tasks on the calling goroutine.
func syncWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() {
		_ = w.Close()
	})
	return w
}

// mustDo runs f in a transaction on w and fails the test if the task did not
// complete.
func mustDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatalf("world task failed: %v", err)
	}
}

// testPlayerType encodes as minecraft:player, which is the only thing
// World.allPlayers checks, so entities of this type are yielded by tx.Players
// and are therefore selectable by @a, @p and @r.
type testPlayerType struct{}

func (testPlayerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &testEntity{tx: tx, handle: handle, data: data}
}
func (testPlayerType) EncodeEntity() string { return "minecraft:player" }
func (testPlayerType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (testPlayerType) DecodeNBT(map[string]any, *world.EntityData) {}
func (testPlayerType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// testMobType is a non-player entity type: it is yielded by tx.Entities but not
// by tx.Players.
type testMobType struct{}

func (testMobType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &testEntity{tx: tx, handle: handle, data: data}
}
func (testMobType) EncodeEntity() string { return "minecraft:pinned_mob" }
func (testMobType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1, 0.3)
}
func (testMobType) DecodeNBT(map[string]any, *world.EntityData) {}
func (testMobType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// nopConfig satisfies world.EntityConfig, which EntitySpawnOpts.New calls
// unconditionally.
type nopConfig struct{}

func (nopConfig) Apply(*world.EntityData) {}

// testEntity is a minimal world.Entity that is also a NamedTarget.
type testEntity struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *testEntity) H() *world.EntityHandle  { return e.handle }
func (e *testEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *testEntity) Rotation() cube.Rotation { return e.data.Rot }
func (e *testEntity) Name() string            { return e.data.Name }
func (e *testEntity) Close() error {
	e.tx.RemoveEntity(e)
	return e.handle.Close()
}

// namelessType is a player-encoded entity type whose Entity has no Name method.
// targets() converts every player to a NamedTarget unconditionally, so an
// entity of this type in the world makes every []Target parameter panic.
type namelessType struct{}

func (namelessType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &namelessEntity{tx: tx, handle: handle, data: data}
}
func (namelessType) EncodeEntity() string { return "minecraft:player" }
func (namelessType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (namelessType) DecodeNBT(map[string]any, *world.EntityData) {}
func (namelessType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type namelessEntity struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *namelessEntity) H() *world.EntityHandle  { return e.handle }
func (e *namelessEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *namelessEntity) Rotation() cube.Rotation { return e.data.Rot }
func (e *namelessEntity) Close() error {
	e.tx.RemoveEntity(e)
	return e.handle.Close()
}
