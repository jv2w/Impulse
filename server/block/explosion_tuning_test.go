package block_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/entity"
	"github.com/jv2w/Impulse/server/world"
)

// TestExplosionsEnabledIsTheBaseline records what one explosion of the fixture
// below does with the rule left unset, so that the test after it can attribute
// the absence of all of it to the rule alone.
func TestExplosionsEnabledIsTheBaseline(t *testing.T) {
	got := tuningExplode(t, world.Tuning{})

	if got.handlerCalls != 1 {
		t.Errorf("HandleExplosion calls = %d, want 1", got.handlerCalls)
	}
	if got.entityCalls != 1 {
		t.Errorf("ExplodableEntity.Explode calls = %d, want 1", got.entityCalls)
	}
	if got.blockSurvived {
		t.Error("the stone beside the blast survived, want it broken")
	}
	if got.sounds == 0 || got.particles == 0 {
		t.Errorf("viewer saw %d sounds and %d particles, want at least one of each", got.sounds, got.particles)
	}
}

// TestExplosionsDisabledLeavesEverythingUntouched pins that the rule stops the
// explosion before any of its work: the handler is never consulted, so nothing
// ray marched and nothing scanned for entities, and no block, entity, sound or
// particle is touched.
func TestExplosionsDisabledLeavesEverythingUntouched(t *testing.T) {
	got := tuningExplode(t, world.Tuning{ExplosionsDisabled: true})

	if got.handlerCalls != 0 {
		t.Errorf("HandleExplosion calls = %d, want 0", got.handlerCalls)
	}
	if got.entityCalls != 0 {
		t.Errorf("ExplodableEntity.Explode calls = %d, want 0", got.entityCalls)
	}
	if !got.blockSurvived {
		t.Error("the stone beside the blast was broken, want it untouched")
	}
	if got.sounds != 0 || got.particles != 0 {
		t.Errorf("viewer saw %d sounds and %d particles, want none of either", got.sounds, got.particles)
	}
}

// tuningExplosionResult is everything one explosion of the fixture is
// observable through.
type tuningExplosionResult struct {
	handlerCalls  int
	entityCalls   int
	blockSurvived bool
	sounds        int
	particles     int
}

// tuningExplode runs a single size 4 explosion at explOrigin in a world with
// the rules passed, against a stone block and an explodable entity that are both
// well inside the blast.
func tuningExplode(t *testing.T, rules world.Tuning) tuningExplosionResult {
	t.Helper()

	w := world.Config{
		Synchronous: true,
		Entities:    entity.DefaultRegistry,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Tuning:      rules,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	h := &explHandler{}
	w.Handle(h)

	v := &explViewer{}
	loader := world.NewLoader(1, w, v)
	w.Do(func(tx *world.Tx) { loader.Load(tx, 1) })
	t.Cleanup(func() { w.Do(func(tx *world.Tx) { loader.Close(tx) }) })

	rec := &explRecord{}
	typ := explEntityType{box: cube.Box(-0.15, 0, -0.15, 0.15, 0.3, 0.15), rec: rec}
	stone := explOriginBlock.Side(cube.FaceEast)

	res := tuningExplosionResult{}
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(stone, block.Stone{}, nil)
		tx.AddEntity(world.EntitySpawnOpts{Position: explOrigin.Add(mgl64.Vec3{1, 0, 0})}.New(typ, explEntityConfig{}))

		block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: 1}.
			Explode(tx, explSource{pos: explOrigin, size: 4})

		_, res.blockSurvived = tx.Block(stone).(block.Stone)
	}).Err(); err != nil {
		t.Fatalf("explosion transaction: %v", err)
	}

	res.handlerCalls, res.entityCalls = h.calls, rec.calls
	res.sounds, res.particles = len(v.sounds), len(v.particles)
	return res
}
