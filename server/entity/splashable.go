package entity

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/cube/trace"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item/potion"
	"github.com/jv2w/Impulse/server/world"
	"time"
)

// SplashableBlock is a block that can be splashed with a splash bottle.
type SplashableBlock interface {
	world.Block
	// Splash is called when a water bottle splashes onto a block.
	Splash(tx *world.Tx, pos cube.Pos)
}

// SplashableEntity is an entity that can be splashed with a splash bottle.
type SplashableEntity interface {
	world.Entity
	// Splash is called when a water bottle splashes onto an entity.
	Splash(tx *world.Tx, pos mgl64.Vec3)
}

// potionSplash returns a function that creates a potion splash with a specific
// duration multiplier and potion type.
func potionSplash(durMul float64, pot potion.Potion, linger bool) func(e *Ent, tx *world.Tx, res trace.Result) {
	return func(e *Ent, tx *world.Tx, res trace.Result) {
		pos := e.Position()
		effects := pot.Effects()
		box := e.H().Type().BBox(e).Translate(pos)

		if len(effects) > 0 {
			reach := box.GrowVec3(mgl64.Vec3{4.125, 2.125, 4.125})
			// res never changes while the loop runs and Entity() is a plain field
			// read, so the hit entity's handle is resolved once instead of once
			// per entity in reach. The comparison below is the same pointer
			// comparison it was before.
			hit, hitEntity := res.(trace.EntityResult)
			var hitHandle *world.EntityHandle
			if hitEntity {
				hitHandle = hit.Entity().H()
			}
			// filterLiving allocated an iterator closure and made each entity
			// cross an extra yield hop only to have Living asserted a second time
			// in the body. This walks the same entities in the same order and
			// keeps the Living check first.
			for otherE := range tx.EntitiesWithin(box.GrowVec3(mgl64.Vec3{8.25, 4.25, 8.25})) {
				splashed, ok := otherE.(Living)
				if !ok {
					continue
				}
				otherPos := otherE.Position()
				if !otherE.H().Type().BBox(otherE).Translate(otherPos).IntersectsWith(reach) {
					continue
				}

				dist := otherPos.Sub(pos).Len()
				if dist > 4 {
					continue
				}

				f := 1 - dist/4
				if hitEntity && hitHandle == otherE.H() {
					f = 1
				}

				for _, eff := range effects {
					// eff.Type() and the LastingType assertion were both done
					// twice per effect; the results are identical, so they are
					// taken once and reused.
					typ := eff.Type()
					lasting, ok := typ.(effect.LastingType)
					if !ok {
						splashed.AddEffect(effect.NewInstantWithPotency(typ, eff.Level(), f))
						continue
					}

					dur := time.Duration(float64(eff.Duration()) * durMul * f)
					if dur < time.Second {
						continue
					}
					splashed.AddEffect(effect.New(lasting, eff.Level(), dur))
				}
			}
		} else if pot == potion.Water() {
			switch result := res.(type) {
			case trace.BlockResult:
				// BlockPosition() is a field read of an immutable result, so
				// reading it once up front gives the same position both uses had.
				resultPos := result.BlockPosition()
				blockPos := resultPos.Side(result.Face())
				if _, ok := tx.Block(blockPos).(block.Fire); ok {
					tx.SetBlock(blockPos, nil, nil)
				}

				for _, f := range cube.HorizontalFaces() {
					h := blockPos.Side(f)
					// Only the fire branch can change h, so the second read is
					// now made only there; without it the first read is still
					// the current block.
					side := tx.Block(h)
					if _, ok := side.(block.Fire); ok {
						tx.SetBlock(h, nil, nil)

						// A second read of h: the fire above may just have been cleared.
						side = tx.Block(h)
					}

					if b, ok := side.(SplashableBlock); ok {
						b.Splash(tx, h)
					}
				}

				if b, ok := tx.Block(resultPos).(SplashableBlock); ok {
					b.Splash(tx, resultPos)
				}
			case trace.EntityResult:
				// TODO: Damage endermen, blazes, striders and snow golems when implemented and rehydrate axolotls.
			}

			// Same inlining of filterLiving as above: no iterator closure and no
			// extra yield hop, with the Living check kept ahead of the
			// SplashableEntity check so the same entities are splashed in order.
			for otherE := range tx.EntitiesWithin(box.GrowVec3(mgl64.Vec3{8.25, 4.25, 8.25})) {
				if _, ok := otherE.(Living); !ok {
					continue
				}
				if splashE, ok := otherE.(SplashableEntity); ok {
					splashE.Splash(tx, otherE.Position())
				}
			}
		}
		if linger {
			tx.AddEntity(NewAreaEffectCloud(world.EntitySpawnOpts{Position: pos}, pot))
		}
	}
}
