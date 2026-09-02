package world

import "testing"

// oddPropertyValues are the property types an NBT decoder produces that no block state can be registered with. Every
// one of them reaches hashProperties from client-supplied item NBT and from block entities loaded off disk.
var oddPropertyValues = []any{int16(1), int64(1), float32(1), float64(1), []any{}, map[string]any{}}

// TestHashPropertiesRejectsUnencodableTypes verifies that a property of a type outside the four the block format uses
// is reported as a miss rather than crashing the goroutine that hashed it.
func TestHashPropertiesRejectsUnencodableTypes(t *testing.T) {
	for _, v := range oddPropertyValues {
		if h, ok := hashProperties(map[string]any{"x": v}); ok {
			t.Errorf("hashProperties with a %T property = (%q, true), want ok false", v, h)
		}
	}
	for _, v := range []any{true, uint8(1), int32(1), "s"} {
		if _, ok := hashProperties(map[string]any{"x": v}); !ok {
			t.Errorf("hashProperties with a %T property = ok false, want true", v)
		}
	}
}

// TestBlockByNameRejectsUnencodableProperty verifies that a state carrying a property this format cannot encode is a
// lookup miss. Reaching this through a chest's item NBT is the shortest path a client has to the block registry.
func TestBlockByNameRejectsUnencodableProperty(t *testing.T) {
	DefaultBlockRegistry.Finalize()
	for _, v := range oddPropertyValues {
		b, ok := DefaultBlockRegistry.BlockByName("minecraft:stone", map[string]any{"x": v})
		if b != nil || ok {
			t.Errorf("BlockByName with a %T property = (%#v, %v), want (nil, false)", v, b, ok)
		}
	}
}

// TestStateToRuntimeIDUnencodablePropertyFallsBack verifies that an unencodable property leaves the exact-state lookup
// with nothing to match, after which the existing fallback to the block's default properties still resolves.
func TestStateToRuntimeIDUnencodablePropertyFallsBack(t *testing.T) {
	DefaultBlockRegistry.Finalize()
	want, ok := DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", nil)
	if !ok {
		t.Fatal("minecraft:stone has no default state")
	}
	for _, v := range oddPropertyValues {
		rid, ok := DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", map[string]any{"x": v})
		if !ok || rid != want {
			t.Errorf("StateToRuntimeID with a %T property = (%d, %v), want (%d, true)", v, rid, ok, want)
		}
	}
}

// TestBlockByNameOnNonFinalizedRegistry verifies that a lookup before Finalize reports a miss instead of panicking. The
// lookup is reachable from block entity decoding, so the state of a registry's lifetime must not decide whether
// decoding a chest takes the world goroutine down.
func TestBlockByNameOnNonFinalizedRegistry(t *testing.T) {
	br := NewBlockRegistry()
	b, ok := br.BlockByName("minecraft:stone", nil)
	if b != nil || ok {
		t.Errorf("BlockByName on a non-finalized registry = (%#v, %v), want (nil, false)", b, ok)
	}
}
