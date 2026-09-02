package block_test

import (
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/entity"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/particle"
	"github.com/jv2w/Impulse/server/world/sound"
)

// explOrigin is the centre used by every explosion test. It is the centre of
// the block at (0, 65, 0), which sits in chunk (0, 0) so a single Loader with
// radius 1 covers the whole blast.
var explOrigin = mgl64.Vec3{0.5, 65.5, 0.5}

// explOriginBlock is cube.PosFromVec3(explOrigin).
var explOriginBlock = cube.Pos{0, 65, 0}

// explSource is the world.ExplosionSource fake. world.BlockExplosionSource and
// world.EntityExplosionSource cannot express a fractional position and a size
// independently, which the ray-march goldens below need.
type explSource struct {
	pos  mgl64.Vec3
	size float64
}

func (s explSource) Position() mgl64.Vec3 { return s.pos }
func (s explSource) Size() float64        { return s.size }

// explHandler snapshots the arguments HandleExplosion receives. Because the
// handler runs after the ray march and before any mutation, it is the only
// deterministic window onto the affected block set and onto the defaulted
// ItemDropChance.
type explHandler struct {
	world.NopHandler
	cancel bool

	calls          int
	blocks         []cube.Pos
	entities       int
	itemDropChance float64
	spawnFire      bool
	sounds         []world.Sound
}

func (h *explHandler) HandleExplosion(ctx *world.Context, _ world.ExplosionSource, entities *[]world.Entity, blocks *[]cube.Pos, itemDropChance *float64, spawnFire *bool) {
	h.calls++
	h.blocks = slices.Clone(*blocks)
	h.entities = len(*entities)
	h.itemDropChance = *itemDropChance
	h.spawnFire = *spawnFire
	if h.cancel {
		ctx.Cancel()
	}
}

func (h *explHandler) HandleSound(_ *world.Context, s world.Sound, _ mgl64.Vec3) {
	h.sounds = append(h.sounds, s)
}

// explViewer records the particles and sounds pushed to viewers of the blast
// position, which is the only way to observe ExplosionConfig.Particle.
type explViewer struct {
	world.NopViewer
	particles []world.Particle
	sounds    []world.Sound
}

func (v *explViewer) ViewParticle(_ mgl64.Vec3, p world.Particle) {
	v.particles = append(v.particles, p)
}
func (v *explViewer) ViewSound(_ mgl64.Vec3, s world.Sound) { v.sounds = append(v.sounds, s) }

// explRecord collects ExplodableEntity.Explode calls. It is shared through the
// EntityType because world.EntityType.Open builds a fresh Entity per
// transaction, so the Entity itself cannot hold the result.
type explRecord struct {
	calls  int
	impact float64
}

type explEntityType struct {
	box cube.BBox
	rec *explRecord
}

func (t explEntityType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &explEntity{h: h, d: d, rec: t.rec}
}
func (explEntityType) EncodeEntity() string                        { return "impulse:explosion_test_entity" }
func (t explEntityType) BBox(world.Entity) cube.BBox               { return t.box }
func (explEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (explEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type explEntityConfig struct{}

func (explEntityConfig) Apply(*world.EntityData) {}

type explEntity struct {
	h   *world.EntityHandle
	d   *world.EntityData
	rec *explRecord
}

func (e *explEntity) H() *world.EntityHandle  { return e.h }
func (e *explEntity) Position() mgl64.Vec3    { return e.d.Pos }
func (e *explEntity) Rotation() cube.Rotation { return e.d.Rot }
func (e *explEntity) Close() error            { return nil }

func (e *explEntity) Explode(_ world.ExplosionSource, impact float64) {
	e.rec.calls++
	e.rec.impact = impact
}

// newExplWorld returns a synchronous World with the handler passed installed.
// Its log is discarded because the world recovers panics from scheduled tasks
// and logs them at error level; TestExplosionPanicsOnBoundingBoxSampleAtOrigin
// deliberately triggers one.
func newExplWorld(t *testing.T, h world.Handler) *world.World {
	t.Helper()
	w := world.Config{
		Synchronous: true,
		Entities:    entity.DefaultRegistry,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}.New()
	t.Cleanup(func() { w.Close() })
	if h != nil {
		w.Handle(h)
	}
	return w
}

// explode runs a single explosion of the size passed at explOrigin, returning
// the block positions the handler saw. Panics recovered by the world are
// returned through the Task error rather than unwinding the test goroutine.
func explode(t *testing.T, w *world.World, h *explHandler, c block.ExplosionConfig, size float64, setup func(tx *world.Tx)) []cube.Pos {
	t.Helper()
	if err := w.Do(func(tx *world.Tx) {
		if setup != nil {
			setup(tx)
		}
		c.Explode(tx, explSource{pos: explOrigin, size: size})
	}).Err(); err != nil {
		t.Fatalf("explosion transaction: %v", err)
	}
	if h == nil {
		return nil
	}
	if h.calls != 1 {
		t.Fatalf("HandleExplosion called %d times, want 1", h.calls)
	}
	return h.blocks
}

// explSeed is the fixed rand source every golden in this file was produced
// with. rand.NewPCG must be called per explosion: a rand.Source is stateful and
// sharing one between two explosions would make the second differ.
func explSeed() rand.Source { return rand.NewPCG(1, 2) }

// explExtent returns the number of positions, the largest single-axis offset
// from explOriginBlock, and the largest squared distance of a block centre from
// explOrigin.
func explExtent(blocks []cube.Pos) (n, maxAxis int, maxLenSqr float64) {
	n = len(blocks)
	for _, p := range blocks {
		if l := p.Vec3Middle().Sub(explOrigin).LenSqr(); l > maxLenSqr {
			maxLenSqr = l
		}
		for i := range 3 {
			a := p[i] - explOriginBlock[i]
			if a < 0 {
				a = -a
			}
			if a > maxAxis {
				maxAxis = a
			}
		}
	}
	return n, maxAxis, maxLenSqr
}

// TestExplosionAffectedBlocksAreDeterministicForASeed pins the ray march of
// ExplosionConfig.Explode in empty air, where nothing but the step decay limits
// a ray.
//
// Per ray the blast force starts at size*(0.7+r.Float64()*0.6) and loses 0.225
// per step of length 0.3. Air is neither Breakable nor a Liquid, so it adds no
// resistance term; for size 4 the force starts in [2.8, 5.2] and survives
// between 13 and 24 steps, reaching at most 24*0.3 = 7.2 blocks. The observed
// extent, 7 blocks on the widest axis, agrees.
//
// The goldens (1133 positions, widest axis 7, farthest centre 57.25 squared)
// come from running this test against explosion.go as it stands. They pin two
// things at once: the arithmetic above, and the fact that the package-global
// `rays` slice is iterated in a fixed order and is not mutated by a blast, since
// the identical second run reproduces the slice element for element.
//
// PINNED CONSTRAINT: `rays` is a package-global `var` built by `init()`, which
// CLAUDE.md 2.4 forbids. It has length 1352, exactly its cap, so `append` in
// `init` never reallocates and the slice header is never written again after
// `init` returns. Every World in the process reads that one slice.
func TestExplosionAffectedBlocksAreDeterministicForASeed(t *testing.T) {
	const (
		wantBlocks    = 1133
		wantMaxAxis   = 7
		wantMaxLenSqr = 57.25
	)

	h1 := &explHandler{}
	a := explode(t, newExplWorld(t, h1), h1, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, nil)

	n, maxAxis, maxLenSqr := explExtent(a)
	if n != wantBlocks {
		t.Errorf("affected blocks = %d, want %d", n, wantBlocks)
	}
	if maxAxis != wantMaxAxis {
		t.Errorf("widest axis offset = %d, want %d", maxAxis, wantMaxAxis)
	}
	if maxLenSqr != wantMaxLenSqr {
		t.Errorf("farthest block centre = %v squared, want %v", maxLenSqr, wantMaxLenSqr)
	}

	h2 := &explHandler{}
	b := explode(t, newExplWorld(t, h2), h2, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, nil)
	if !slices.Equal(a, b) {
		t.Errorf("two explosions with the same seed produced different block slices (%d vs %d positions)", len(a), len(b))
	}
}

// TestExplosionRaysAreSafeForConcurrentWorlds runs the same seeded explosion on
// eight independent Worlds at once and compares each result against the serial
// one. Under -race this is the test that answers whether the package-global
// `rays` slice can be read by every world goroutine at the same time: a write
// to it from any explosion would both trip the race detector and change a
// result.
func TestExplosionRaysAreSafeForConcurrentWorlds(t *testing.T) {
	serialH := &explHandler{}
	want := explode(t, newExplWorld(t, serialH), serialH, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, nil)

	const workers = 8
	got := make([][]cube.Pos, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := &explHandler{}
			// A World per goroutine: a Synchronous World must be driven from
			// one goroutine, so only `rays` is genuinely shared here.
			w := world.Config{
				Synchronous: true,
				Entities:    entity.DefaultRegistry,
				Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			}.New()
			defer w.Close()
			w.Handle(h)
			w.Do(func(tx *world.Tx) {
				block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}.
					Explode(tx, explSource{pos: explOrigin, size: 4})
			})
			got[i] = h.blocks
		}()
	}
	wg.Wait()

	for i, blocks := range got {
		if !slices.Equal(blocks, want) {
			t.Errorf("worker %d produced %d positions, want the %d of the serial run", i, len(blocks), len(want))
		}
	}
}

// TestExplosionConfigDefaultsDoNotLeakToTheCaller pins that Explode has a value
// receiver, so the defaults it fills in are written to its own copy only. A
// caller that reuses one ExplosionConfig for several blasts of different sizes
// therefore gets the right 1/size drop chance every time, and a nil RandSource
// stays nil rather than freezing on the first seed.
//
// PINNED CONSTRAINT: a nil RandSource is replaced by rand.NewPCG(t, t) with
// t = time.Now().UnixNano(). An explosion left unseeded is not reproducible, and
// on a platform with a coarse clock two explosions in the same instant share a
// seed. There is no way to read back the source that was used.
func TestExplosionConfigDefaultsDoNotLeakToTheCaller(t *testing.T) {
	c := block.ExplosionConfig{}
	explode(t, newExplWorld(t, nil), nil, c, 4, nil)

	if c.Sound != nil {
		t.Errorf("Sound = %#v after Explode, want nil", c.Sound)
	}
	if c.Particle != nil {
		t.Errorf("Particle = %#v after Explode, want nil", c.Particle)
	}
	if c.RandSource != nil {
		t.Errorf("RandSource = %#v after Explode, want nil", c.RandSource)
	}
	if c.ItemDropChance != 0 {
		t.Errorf("ItemDropChance = %v after Explode, want 0", c.ItemDropChance)
	}
}

// TestExplosionSoundAndParticleDefaults pins sound.Explosion{} and
// particle.HugeExplosion{} as the substitutes for a nil Sound and Particle, and
// pins that a configured value is passed through untouched.
func TestExplosionSoundAndParticleDefaults(t *testing.T) {
	tests := []struct {
		name         string
		conf         block.ExplosionConfig
		wantSound    world.Sound
		wantParticle world.Particle
	}{
		{
			name:         "nil sound and particle take the explosion defaults",
			conf:         block.ExplosionConfig{},
			wantSound:    sound.Explosion{},
			wantParticle: particle.HugeExplosion{},
		},
		{
			name:         "configured sound and particle pass through",
			conf:         block.ExplosionConfig{Sound: sound.Pop{}, Particle: particle.Flame{}},
			wantSound:    sound.Pop{},
			wantParticle: particle.Flame{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &explViewer{}
			w := newExplWorld(t, nil)
			loader := world.NewLoader(1, w, v)
			w.Do(func(tx *world.Tx) { loader.Load(tx, 1) })
			defer w.Do(func(tx *world.Tx) { loader.Close(tx) })

			c := tt.conf
			c.RandSource, c.ItemDropChance = explSeed(), -1
			explode(t, w, nil, c, 4, nil)

			if len(v.sounds) != 1 || v.sounds[0] != tt.wantSound {
				t.Errorf("sounds = %#v, want exactly [%#v]", v.sounds, tt.wantSound)
			}
			if len(v.particles) != 1 || v.particles[0] != tt.wantParticle {
				t.Errorf("particles = %#v, want exactly [%#v]", v.particles, tt.wantParticle)
			}
		})
	}
}

// TestExplosionItemDropChanceDefaulting pins how ItemDropChance is resolved. The
// value the handler receives is the resolved one, so it can be asserted exactly
// without depending on the drop roll.
//
// PINNED BUG: an explicit ItemDropChance of 0 is indistinguishable from an unset
// field, so `if c.ItemDropChance == 0 { c.ItemDropChance = 1.0 / size }` turns a
// caller's request for "drop nothing" into 1/4 = 0.25 at the default size 4.
// Suppressing drops requires a negative value, which is not what 0 means
// anywhere else. Not fixed here; a *float64 or a separate flag would be needed.
func TestExplosionItemDropChanceDefaulting(t *testing.T) {
	// wantItems of -1 marks a fractional chance, where whether the single stone
	// drops is decided by a roll against the shared rand.Rand and is not a
	// property of the configuration.
	tests := []struct {
		name      string
		set       float64
		size      float64
		want      float64
		wantItems int
	}{
		{name: "unset becomes 1/size at the default size", set: 0, size: 4, want: 0.25, wantItems: -1},
		{name: "unset becomes 1/size at size 2", set: 0, size: 2, want: 0.5, wantItems: -1},
		{name: "explicit zero is treated as unset", set: 0, size: 8, want: 0.125, wantItems: -1},
		{name: "negative never drops", set: -1, size: 4, want: -1, wantItems: 0},
		{name: "one always drops", set: 1, size: 4, want: 1, wantItems: 1},
		{name: "above one always drops", set: 2, size: 4, want: 2, wantItems: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &explHandler{}
			w := newExplWorld(t, h)
			// A single stone block two blocks east of the origin: it is inside
			// every blast tested here and drops exactly one item stack.
			explode(t, w, h, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: tt.set}, tt.size, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{2, 65, 0}, block.Stone{}, nil)
			})
			if h.itemDropChance != tt.want {
				t.Errorf("resolved ItemDropChance = %v, want %v", h.itemDropChance, tt.want)
			}

			if tt.wantItems < 0 {
				return
			}
			var items int
			w.Do(func(tx *world.Tx) {
				for range tx.Entities() {
					items++
				}
			})
			if items != tt.wantItems {
				t.Errorf("dropped item entities = %d, want %d", items, tt.wantItems)
			}
		})
	}
}

// TestExplosionSourceDefaultSize pins world's defaultExplosionSize of 4, which
// both built-in sources fall back to when ExplosionSize is left at 0, and pins
// that a set size is returned unchanged.
func TestExplosionSourceDefaultSize(t *testing.T) {
	tests := []struct {
		name string
		src  world.ExplosionSource
		want float64
	}{
		{name: "block source defaults to 4", src: world.BlockExplosionSource{}, want: 4},
		{name: "block source keeps its size", src: world.BlockExplosionSource{ExplosionSize: 1.5}, want: 1.5},
		{name: "entity source defaults to 4", src: world.EntityExplosionSource{}, want: 4},
		{name: "entity source keeps its size", src: world.EntityExplosionSource{ExplosionSize: 6}, want: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.src.Size(); got != tt.want {
				t.Errorf("Size() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestExplosionBlastIsStoppedByResistantBlocks pins the two ways a ray dies
// early.
//
// A Breakable block subtracts (BlastResistance+0.3)*0.3 on top of the 0.225 step
// decay. Obsidian's resistance of 1200 costs (1200+0.3)*0.3 = 360.09, far above
// the largest possible starting force of size*1.3 = 5.2, so a ray entering
// obsidian is spent in one step and the obsidian is never added to the set. With
// the 26 neighbours of the origin walled in, every one of the 1352 rays dies on
// its first step and only the origin block itself survives.
//
// Bedrock is neither a Liquid, nor Breakable, nor Air, so it sets
// explosionBlockStopsRay and the ray breaks before advancing at all. A bedrock
// floor is therefore not merely unbroken, it is absent from the affected set,
// and nothing below it is reached.
func TestExplosionBlastIsStoppedByResistantBlocks(t *testing.T) {
	openH := &explHandler{}
	open := explode(t, newExplWorld(t, openH), openH, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, nil)

	t.Run("obsidian shell leaves only the origin", func(t *testing.T) {
		h := &explHandler{}
		blocks := explode(t, newExplWorld(t, h), h, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, func(tx *world.Tx) {
			for x := -1; x <= 1; x++ {
				for y := -1; y <= 1; y++ {
					for z := -1; z <= 1; z++ {
						if x == 0 && y == 0 && z == 0 {
							continue
						}
						tx.SetBlock(explOriginBlock.Add(cube.Pos{x, y, z}), block.Obsidian{}, nil)
					}
				}
			}
		})
		if !slices.Equal(blocks, []cube.Pos{explOriginBlock}) {
			t.Errorf("affected blocks = %v, want exactly [%v]", blocks, explOriginBlock)
		}
		if len(blocks) >= len(open) {
			t.Errorf("obsidian shell affected %d blocks, want strictly fewer than the %d of an open blast", len(blocks), len(open))
		}
	})

	t.Run("bedrock floor terminates the ray without joining the set", func(t *testing.T) {
		const wantBlocks = 632
		h := &explHandler{}
		blocks := explode(t, newExplWorld(t, h), h, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, func(tx *world.Tx) {
			for x := -8; x <= 8; x++ {
				for z := -8; z <= 8; z++ {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Bedrock{}, nil)
				}
			}
		})
		if len(blocks) != wantBlocks {
			t.Errorf("affected blocks = %d, want %d", len(blocks), wantBlocks)
		}
		if len(blocks) >= len(open) {
			t.Errorf("bedrock floor affected %d blocks, want strictly fewer than the %d of an open blast", len(blocks), len(open))
		}
		for _, p := range blocks {
			if p[1] <= 64 {
				t.Fatalf("block %v at or below the bedrock floor was affected", p)
			}
		}
	})
}

// TestExplosionEntityImpactFalloff pins the entity half of Explode. The blast
// reaches d = size*2 = 8 blocks; an entity is skipped when its distance exceeds
// d or is exactly 0, and otherwise takes impact (1 - dist/d) * exposure. In open
// air every sample ray from the origin reaches the entity's box, so exposure is
// exactly 1 and impact is the linear falloff alone.
//
// PINNED SCOPE: the dist == 0 skip means an entity standing precisely on the
// origin — the usual case for the entity that caused the blast — takes no
// impact at all, while one a millimetre away takes very nearly the full 1.0.
func TestExplosionEntityImpactFalloff(t *testing.T) {
	tests := []struct {
		name       string
		offset     mgl64.Vec3
		wantCalls  int
		wantImpact float64
	}{
		{name: "quarter of the reach", offset: mgl64.Vec3{2, 0, 0}, wantCalls: 1, wantImpact: 0.75},
		{name: "half of the reach", offset: mgl64.Vec3{4, 0, 0}, wantCalls: 1, wantImpact: 0.5},
		{name: "exactly at the reach still fires with zero impact", offset: mgl64.Vec3{8, 0, 0}, wantCalls: 1, wantImpact: 0},
		{name: "beyond the reach is skipped", offset: mgl64.Vec3{8.5, 0, 0}, wantCalls: 0},
		{name: "exactly on the origin is skipped", offset: mgl64.Vec3{}, wantCalls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &explRecord{}
			w := newExplWorld(t, nil)
			// A small box keeps every exposure sample point inside open air and
			// away from the origin, so exposure is 1.
			typ := explEntityType{box: cube.Box(-0.15, 0, -0.15, 0.15, 0.3, 0.15), rec: rec}
			explode(t, w, nil, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}, 4, func(tx *world.Tx) {
				tx.AddEntity(world.EntitySpawnOpts{Position: explOrigin.Add(tt.offset)}.New(typ, explEntityConfig{}))
			})
			if rec.calls != tt.wantCalls {
				t.Fatalf("Explode called %d times, want %d", rec.calls, tt.wantCalls)
			}
			if rec.calls > 0 && rec.impact != tt.wantImpact {
				t.Errorf("impact = %v, want %v", rec.impact, tt.wantImpact)
			}
		})
	}
}

// TestExplosionSpawnFire pins the fire pass. It runs only when SpawnFire
// survives the handler, and then only for the affected positions that pass
// r.IntN(3) == 0 and are air standing on a face-solid block.
//
// The world is a bedrock floor at y=64 with the blast one block above it, so the
// only positions that can catch fire are the y=65 layer resting on the floor.
// 45 fires out of 632 affected positions is the value explosion.go produces for
// the seed rand.NewPCG(1, 2); the roll consumes the same rand.Rand the ray march
// used, so the count is a function of both the seed and the block count.
func TestExplosionSpawnFire(t *testing.T) {
	tests := []struct {
		name      string
		spawnFire bool
		want      int
	}{
		{name: "no fire without SpawnFire", spawnFire: false, want: 0},
		{name: "one in three affected positions catches fire", spawnFire: true, want: 45},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &explHandler{}
			w := newExplWorld(t, h)
			blocks := explode(t, w, h, block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1, SpawnFire: tt.spawnFire}, 4, func(tx *world.Tx) {
				for x := -8; x <= 8; x++ {
					for z := -8; z <= 8; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Bedrock{}, nil)
					}
				}
			})
			if h.spawnFire != tt.spawnFire {
				t.Errorf("handler saw spawnFire = %v, want %v", h.spawnFire, tt.spawnFire)
			}

			var fires int
			w.Do(func(tx *world.Tx) {
				for _, p := range blocks {
					if _, ok := tx.Block(p).(block.Fire); ok {
						fires++
					}
				}
			})
			if fires != tt.want {
				t.Errorf("fire blocks = %d, want %d", fires, tt.want)
			}
		})
	}
}

// TestExplosionCancelledLeavesTheWorldUntouched pins that a handler cancelling
// the Context returns from Explode before anything is applied: no entity is
// exploded, no block is broken, no fire is lit and neither the particle nor the
// sound is emitted. The block set is still computed and handed to the handler
// first, so cancelling costs the full ray march.
func TestExplosionCancelledLeavesTheWorldUntouched(t *testing.T) {
	stone := cube.Pos{2, 65, 0}
	rec := &explRecord{}
	typ := explEntityType{box: cube.Box(-0.15, 0, -0.15, 0.15, 0.3, 0.15), rec: rec}

	h := &explHandler{cancel: true}
	w := newExplWorld(t, h)
	v := &explViewer{}
	loader := world.NewLoader(1, w, v)
	w.Do(func(tx *world.Tx) { loader.Load(tx, 1) })
	defer w.Do(func(tx *world.Tx) { loader.Close(tx) })

	blocks := explode(t, w, h, block.ExplosionConfig{RandSource: explSeed(), SpawnFire: true, ItemDropChance: 1}, 4, func(tx *world.Tx) {
		tx.SetBlock(stone, block.Stone{}, nil)
		tx.AddEntity(world.EntitySpawnOpts{Position: explOrigin.Add(mgl64.Vec3{4, 0, 0})}.New(typ, explEntityConfig{}))
	})
	if len(blocks) == 0 {
		t.Fatal("handler saw no affected blocks, so the cancellation proves nothing")
	}
	if !slices.Contains(blocks, stone) {
		t.Fatalf("the stone at %v was not in the affected set, so the cancellation proves nothing", stone)
	}

	if rec.calls != 0 {
		t.Errorf("ExplodableEntity.Explode called %d times on a cancelled explosion, want 0", rec.calls)
	}
	if len(v.particles) != 0 {
		t.Errorf("particles = %#v on a cancelled explosion, want none", v.particles)
	}
	if len(v.sounds) != 0 || len(h.sounds) != 0 {
		t.Errorf("sounds = %#v / %#v on a cancelled explosion, want none", v.sounds, h.sounds)
	}

	var items, changed, fires int
	w.Do(func(tx *world.Tx) {
		for range tx.Entities() {
			items++
		}
		for _, p := range blocks {
			b := tx.Block(p)
			if _, ok := b.(block.Fire); ok {
				fires++
			}
			want := world.Block(block.Air{})
			if p == stone {
				want = block.Stone{}
			}
			if b != want {
				changed++
			}
		}
	})
	// The one entity is the ExplodableEntity added above; any further entity
	// would be a dropped item.
	if items != 1 {
		t.Errorf("entities after a cancelled explosion = %d, want 1", items)
	}
	if changed != 0 {
		t.Errorf("%d of the %d affected positions changed on a cancelled explosion, want 0", changed, len(blocks))
	}
	if fires != 0 {
		t.Errorf("fire blocks = %d on a cancelled explosion, want 0", fires)
	}
}

// TestExplosionPanicsOnBoundingBoxSampleAtOrigin pins a crash in
// ExplosionConfig.exposure.
//
// PINNED BUG: Explode guards only against an entity whose Position equals the
// blast origin (`dist == 0`). exposure then ray-traces from the origin to sample
// points taken from the entity's bounding box, and trace.TraverseBlocks panics
// when start and end coincide. An entity whose box is offset from its position
// can put a sample point exactly on the origin while its position is far from
// it, and nothing rejects that.
//
// The entity below has a 1x1x1 box offset to (1,1,1)..(2,2,2) and stands at
// origin-(1,1,1), so its position is sqrt(3) away — well inside the blast — but
// its box minimum lands on the origin. diff is (1*2+1) = 3 on every axis, so
// xOffset and zOffset are (1 - floor(3)/3)/2 = 0 and the very first sample,
// x=y=z=0, is the box minimum, which is the origin.
//
// The blast is aborted at that point: the world recovers the panic into a
// *world.PanicError on the Task, so the world goroutine survives, but no block
// is broken and no sound or particle is emitted. CLAUDE.md 2.2 forbids reaching
// a panic through a value a caller controls. Not fixed here.
func TestExplosionPanicsOnBoundingBoxSampleAtOrigin(t *testing.T) {
	rec := &explRecord{}
	typ := explEntityType{box: cube.Box(1, 1, 1, 2, 2, 2), rec: rec}

	h := &explHandler{}
	w := newExplWorld(t, h)
	stone := cube.Pos{2, 65, 0}
	err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(stone, block.Stone{}, nil)
		tx.AddEntity(world.EntitySpawnOpts{Position: explOrigin.Sub(mgl64.Vec3{1, 1, 1})}.New(typ, explEntityConfig{}))
		block.ExplosionConfig{RandSource: explSeed(), ItemDropChance: -1}.
			Explode(tx, explSource{pos: explOrigin, size: 4})
	}).Err()

	var panicErr *world.PanicError
	if !asPanicError(err, &panicErr) {
		t.Fatalf("explosion task error = %v, want a *world.PanicError", err)
	}
	if got, want := panicErr.Value, "start and end points are the same, giving a zero direction vector"; got != want {
		t.Fatalf("panic value = %v, want %q", got, want)
	}
	if h.calls != 1 {
		t.Fatalf("HandleExplosion called %d times, want 1: the panic must happen after the handler", h.calls)
	}
	if !slices.Contains(h.blocks, stone) {
		t.Fatalf("the stone at %v was not in the affected set, so the abort proves nothing", stone)
	}
	if len(h.sounds) != 0 {
		t.Errorf("sounds = %#v, want none: the explosion aborts before playing one", h.sounds)
	}

	var got world.Block
	w.Do(func(tx *world.Tx) { got = tx.Block(stone) })
	if got != (world.Block)(block.Stone{}) {
		t.Errorf("block at %v = %#v, want it left as stone by the aborted explosion", stone, got)
	}
}

// asPanicError reports whether err is a *world.PanicError, storing it in target.
// errors.As is avoided so a wrapped error of a different kind cannot satisfy it.
func asPanicError(err error, target **world.PanicError) bool {
	p, ok := err.(*world.PanicError)
	if ok {
		*target = p
	}
	return ok
}
