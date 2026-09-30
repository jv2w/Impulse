package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/internal/nbtconv"
	"github.com/jv2w/Impulse/server/world"
)

// NewTNT creates a new primed TNT entity.
func NewTNT(opts world.EntitySpawnOpts, fuse time.Duration) *world.EntityHandle {
	conf := tntConf
	conf.ExistenceDuration = fuse
	// Len() is the square root of LenSqr() and both build the sum of squares with
	// the same expression, so the test against zero picks the same branch for
	// every input, NaN and infinities included, without the square root.
	if opts.Velocity.LenSqr() == 0 {
		angle := rand.Float64() * math.Pi * 2
		opts.Velocity = mgl64.Vec3{-math.Sin(angle) * 0.02, 0.1, -math.Cos(angle) * 0.02}
	}
	return opts.New(TNTType, conf)
}

var tntConf = PassiveBehaviourConfig{
	Gravity: 0.04,
	Drag:    0.02,
	Expire:  explodeTNT,
}

// explodeTNT creates an explosion at the position of e.
func explodeTNT(e *Ent, tx *world.Tx) {
	block.ExplosionConfig{ItemDropChance: 1}.Explode(tx, world.EntityExplosionSource{
		Entity:        e,
		ExplosionSize: 4,
	})
}

// TNTType is a world.EntityType implementation for TNT.
var TNTType tntType

type tntType struct{}

func (t tntType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (tntType) EncodeEntity() string   { return "minecraft:tnt" }
func (tntType) NetworkOffset() float64 { return 0.49 }
func (tntType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.98, 0.49)
}

func (t tntType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := tntConf
	// Which input was the problem: primed TNT saved partway through its fuse.
	// Fuse holds the time left, but the fuse length was set to it while Age was
	// restored as well, and the fuse runs out once Age passes the length: TNT
	// saved past half its fuse exploded on load, and TNT saved with no time left
	// got a length of zero, which never expires. The length is the age plus the
	// time left, and at least one tick.
	left := max(nbtconv.TickDuration[int16](m, "Fuse"), 0)
	conf.ExistenceDuration = max(data.Age+left, time.Second/20)
	b := conf.New()
	// Until its first tick the behaviour reports the whole length as the time
	// left. A chunk without viewers does not tick its entities, so it could be
	// saved like that and the fuse would grow with every load.
	b.fuse = conf.ExistenceDuration - data.Age
	data.Data = b
}

func (tntType) EncodeNBT(data *world.EntityData) map[string]any {
	return map[string]any{"Fuse": int16(min(data.Data.(*PassiveBehaviour).Fuse()/(time.Second/20), math.MaxInt16))}
}
