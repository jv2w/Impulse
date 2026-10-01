package chunk

import (
	"fmt"
	"maps"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// testBlock is one entry of testRegistry. Its index in testRegistry.blocks is its runtime ID.
type testBlock struct {
	name       string
	properties map[string]any
	filtering  uint8
	light      uint8
	randomTick bool
	nbt        bool
	displacing bool
	liquid     bool
}

// testRegistry is a deterministic BlockRegistry for the characterization tests. The real registry
// (world.DefaultBlockRegistry) is unusable here: server/world imports this package, and its registry needs a
// global init plus Finalize before any accessor works.
//
// Block names are deliberately not minecraft:* so that blockupgrader.Upgrade, which disk decoding runs on every
// palette entry, leaves them untouched. Tests that need the upgrader exercise it explicitly.
type testRegistry struct {
	blocks []testBlock
}

// testBlocks is the fixed block table shared by every test. Runtime IDs are the indices, so air is 0 and the
// values below are stable for golden comparisons.
var testBlocks = []testBlock{
	{name: "impulse:air"},
	{name: "impulse:solid", filtering: 15},
	{name: "impulse:glass", filtering: 0},
	{name: "impulse:leaves", filtering: 1},
	{name: "impulse:lamp", filtering: 15, light: 15},
	{name: "impulse:torch", properties: map[string]any{"facing": int32(1)}, light: 14},
	{name: "impulse:water", properties: map[string]any{"depth": int32(0)}, filtering: 2, liquid: true, displacing: true},
	{name: "impulse:sapling", properties: map[string]any{"stage": int32(0)}, randomTick: true},
	{name: "impulse:chest", properties: map[string]any{"facing": int32(2)}, filtering: 15, nbt: true},
}

// newTestRegistry returns a registry over testBlocks. The returned value is read-only and may be shared.
func newTestRegistry() *testRegistry {
	return &testRegistry{blocks: testBlocks}
}

func (r *testRegistry) BlockCount() int      { return len(r.blocks) }
func (r *testRegistry) AirRuntimeID() uint32 { return 0 }

func (r *testRegistry) RuntimeIDToState(rid uint32) (string, map[string]any, bool) {
	if int(rid) >= len(r.blocks) {
		return "", nil, false
	}
	b := r.blocks[rid]
	props := map[string]any{}
	maps.Copy(props, b.properties)
	return b.name, props, true
}

func (r *testRegistry) StateToRuntimeID(name string, properties map[string]any) (uint32, bool) {
	for rid, b := range r.blocks {
		if b.name != name || len(b.properties) != len(properties) {
			continue
		}
		if maps.Equal(propsOf(b.properties), propsOf(properties)) {
			return uint32(rid), true
		}
	}
	return 0, false
}

// propsOf renders property values as strings so that map comparison does not depend on the concrete numeric
// type NBT decoding produced.
func propsOf(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func (r *testRegistry) at(rid uint32) testBlock {
	if int(rid) >= len(r.blocks) {
		return testBlock{}
	}
	return r.blocks[rid]
}

func (r *testRegistry) FilteringBlock(rid uint32) uint8       { return r.at(rid).filtering }
func (r *testRegistry) LightBlock(rid uint32) uint8           { return r.at(rid).light }
func (r *testRegistry) RandomTickBlock(rid uint32) bool       { return r.at(rid).randomTick }
func (r *testRegistry) NBTBlock(rid uint32) bool              { return r.at(rid).nbt }
func (r *testRegistry) LiquidDisplacingBlock(rid uint32) bool { return r.at(rid).displacing }
func (r *testRegistry) LiquidBlock(rid uint32) bool           { return r.at(rid).liquid }

// HashToRuntimeID and RuntimeIDToHash use a fixed offset so that the mapping is invertible and stable.
const testHashOffset = 0x1000

func (r *testRegistry) HashToRuntimeID(hash uint32) (uint32, bool) {
	if hash < testHashOffset || int(hash-testHashOffset) >= len(r.blocks) {
		return 0, false
	}
	return hash - testHashOffset, true
}

func (r *testRegistry) RuntimeIDToHash(rid uint32) (uint32, bool) {
	if int(rid) >= len(r.blocks) {
		return 0, false
	}
	return rid + testHashOffset, true
}

// Runtime IDs of testBlocks, named for readability in test tables.
const (
	ridAir uint32 = iota
	ridSolid
	ridGlass
	ridLeaves
	ridLamp
	ridTorch
	ridWater
	ridSapling
	ridChest
)

// overworldRange is the vertical range used by every test unless a case needs a different one. It matches the
// range chunks are created with in production.
var overworldRange = cube.Range{-64, 319}

// newTestChunk returns an empty chunk over overworldRange backed by the test registry.
func newTestChunk() *Chunk {
	return New(newTestRegistry(), overworldRange)
}

// assertNoError fails the test if err is non-nil.
func assertNoError(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if err != nil {
		t.Fatalf(format+": %v", append(args, err)...)
	}
}

func TestRegistryFakeIsConsistent(t *testing.T) {
	r := newTestRegistry()
	for rid := range uint32(r.BlockCount()) {
		name, props, ok := r.RuntimeIDToState(rid)
		if !ok {
			t.Fatalf("RuntimeIDToState(%d): not found", rid)
		}
		got, ok := r.StateToRuntimeID(name, props)
		if !ok || got != rid {
			t.Fatalf("StateToRuntimeID(%q, %v) = %d, %v, want %d, true", name, props, got, ok, rid)
		}
		hash, ok := r.RuntimeIDToHash(rid)
		if !ok {
			t.Fatalf("RuntimeIDToHash(%d): not found", rid)
		}
		if back, ok := r.HashToRuntimeID(hash); !ok || back != rid {
			t.Fatalf("HashToRuntimeID(%d) = %d, %v, want %d, true", hash, back, ok, rid)
		}
	}
}
