package item

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// EndCrystal is an item that can be placed on obsidian or bedrock to spawn an End crystal entity.
type EndCrystal struct{}

// endCrystalSupport represents a block that supports an End crystal.
type endCrystalSupport interface {
	SupportsEndCrystal() bool
}

// UseOnBlock places an End crystal on top of the clicked block if it is
// obsidian or bedrock, the two blocks above it are air and no other entities
// intersect the space the crystal is placed in. The face clicked is ignored.
func (e EndCrystal) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	support, ok := tx.Block(pos).(endCrystalSupport)
	if !ok || !support.SupportsEndCrystal() {
		return false
	}

	// The block above is stepped up from once instead of being recomputed for
	// the second step and again for the spawn position: pos.Side is pure.
	above := pos.Side(cube.FaceUp)
	twoAbove := above.Side(cube.FaceUp)
	// Range only reads the world's stored range, so both bounds checks share
	// one call.
	r := tx.Range()
	if above.OutOfBounds(r) || twoAbove.OutOfBounds(r) {
		return false
	}
	// air() looks the block up by name every call and always answers the same
	// block, so the two comparisons share one lookup.
	a := air()
	if tx.Block(above) != a || tx.Block(twoAbove) != a {
		return false
	}

	// Vanilla anchors the entity check at the clicked block, spanning it and the
	// air block above. Any intersecting entity blocks placement, including the
	// placing player.
	box := cube.Box(0, 0, 0, 1, 2, 1).Translate(pos.Vec3())
	for entity := range tx.EntitiesWithin(box.Grow(2)) {
		if entity.H().Type().BBox(entity).Translate(entity.Position()).IntersectsWith(box) {
			return false
		}
	}

	opts := world.EntitySpawnOpts{Position: above.Vec3Middle()}
	tx.AddEntity(tx.World().EntityRegistry().Config().EndCrystal(opts))
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem ...
func (EndCrystal) EncodeItem() (name string, meta int16) {
	return "minecraft:end_crystal", 0
}
