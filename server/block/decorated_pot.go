package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/internal/nbtconv"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/particle"
	"github.com/jv2w/Impulse/server/world/sound"
)

// PotDecoration represents an item that can be used as a decoration on a pot.
type PotDecoration interface {
	world.Item
	PotDecoration() bool
}

// DecoratedPot is a decoration block that can be crafted from up to four pottery sherds, and bricks on the sides where
// no pattern should be displayed.
type DecoratedPot struct {
	transparent
	sourceWaterDisplacer

	// Item is the item being stored in the decorated pot.
	Item item.Stack
	// Facing is the direction the pot is facing. The first decoration will be facing opposite of this direction.
	Facing cube.Direction
	// Decorations are the four decorations displayed on the sides of the pot. If a decoration is a brick or nil,
	// the side will appear to be empty.
	Decorations [4]PotDecoration
}

func (DecoratedPot) ContainerSize() int { return 1 }

// SideClosed ...
func (p DecoratedPot) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// ProjectileHit ...
func (p DecoratedPot) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	// pos does not change in the loop, so its centre is the same vector on every iteration.
	centre := pos.Vec3Centre()
	for _, d := range p.Decorations {
		if d == nil {
			dropItem(tx, item.NewStack(item.Brick{}, 1), centre)
			continue
		}
		dropItem(tx, item.NewStack(d, 1), centre)
	}
	breakBlockNoDrops(p, pos, tx)
}

// Pick ...
func (p DecoratedPot) Pick() item.Stack {
	return item.NewStack(DecoratedPot{Decorations: p.Decorations}, 1)
}

// ExtractItem ...
func (p DecoratedPot) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	if p.Item.Empty() {
		return false
	}
	if _, err := h.inventory.AddItem(p.Item.Grow(-p.Item.Count() + 1)); err != nil {
		return false
	}
	p.Item = p.Item.Grow(-1)
	tx.SetBlock(pos, p, nil)
	return true
}

// InsertItem ...
func (p DecoratedPot) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if !sourceStack.Empty() && sourceStack.Comparable(p.Item) && p.Item.Count() < p.Item.MaxCount() {
			if p.Item.Empty() {
				p.Item = sourceStack.Grow(-sourceStack.Count() + 1)
			} else {
				p.Item = p.Item.Grow(1)
			}
			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			tx.SetBlock(pos, p, nil)
			return true
		}
	}
	return false
}

// wobble ...
func (p DecoratedPot) wobble(pos cube.Pos, tx *world.Tx, success bool) {
	// Both the viewer lookup and the sound below want the same centre of pos, which never changes
	// in this call.
	centre := pos.Vec3Centre()
	for _, v := range tx.Viewers(centre) {
		v.ViewBlockAction(pos, DecoratedPotWobbleAction{DecoratedPot: p, Success: success})
	}

	if success {
		tx.AddParticle(pos.Vec3Middle().Add(mgl64.Vec3{0, 1.2}), particle.DustPlume{})
		tx.PlaySound(centre, sound.DecoratedPotInserted{Progress: float64(p.Item.Count()) / float64(p.Item.MaxCount())})
	} else {
		tx.PlaySound(centre, sound.DecoratedPotInsertFailed{})
	}
}

// Activate ...
func (p DecoratedPot) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if held.Empty() || !p.Item.Comparable(held) || p.Item.Count() == p.Item.MaxCount() {
		p.wobble(pos, tx, false)
		return false
	}

	if p.Item.Empty() {
		p.Item = held.Grow(-held.Count() + 1)
	} else {
		p.Item = p.Item.Grow(1)
	}
	tx.SetBlock(pos, p, nil)
	p.wobble(pos, tx, true)
	ctx.SubtractFromCount(1)
	return true
}

// BreakInfo ...
func (p DecoratedPot) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(DecoratedPot{Decorations: p.Decorations})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if !p.Item.Empty() {
			dropItem(tx, p.Item, pos.Vec3Centre())
		}
	})
}

// EncodeItem ...
func (p DecoratedPot) EncodeItem() (name string, meta int16) {
	return "minecraft:decorated_pot", 0
}

// EncodeBlock ...
func (p DecoratedPot) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:decorated_pot", map[string]any{"direction": int32(horizontalDirection(p.Facing))}
}

// Model ...
func (p DecoratedPot) Model() world.BlockModel {
	return model.DecoratedPot{}
}

// UseOnBlock ...
func (p DecoratedPot) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, p)
	if !used {
		return
	}
	p.Facing = user.Rotation().Direction().Opposite()

	place(tx, pos, p, user, ctx)
	return placed(ctx)
}

// EncodeNBT ...
func (p DecoratedPot) EncodeNBT() map[string]any {
	sherds := make([]any, 0, len(p.Decorations))
	for _, decoration := range p.Decorations {
		if decoration == nil {
			sherds = append(sherds, "minecraft:brick")
		} else {
			name, _ := decoration.EncodeItem()
			sherds = append(sherds, name)
		}
	}

	m := map[string]any{
		"id":     "DecoratedPot",
		"sherds": sherds,
	}
	if !p.Item.Empty() {
		m["item"] = item.WriteNBT(p.Item, true)
	}
	return m
}

// DecodeNBT ...
func (p DecoratedPot) DecodeNBT(data map[string]any) any {
	// DecodeNBT has no channel to report an item this build cannot resolve on;
	// the pot loads empty.
	p.Item, _ = item.MapNBT(data, "item")
	p.Decorations = [4]PotDecoration{}
	if sherds := nbtconv.Slice(data, "sherds"); sherds != nil {
		for i, name := range sherds {
			if i >= len(p.Decorations) {
				break
			}
			// A sherd this build cannot resolve to a decoration leaves the slot undecorated, which is what an
			// absent sherd means and what EncodeNBT writes back as a plain brick.
			id, ok := name.(string)
			if !ok {
				continue
			}
			it, ok := world.ItemByName(id, 0)
			if !ok {
				continue
			}
			if decoration, ok := it.(PotDecoration); ok {
				p.Decorations[i] = decoration
			}
		}
	}
	return p
}

// allDecoratedPots ...
func allDecoratedPots() (pots []world.Block) {
	// One pot per direction, so the final length is known and the list is allocated once.
	directions := cube.Directions()
	pots = make([]world.Block, 0, len(directions))
	for _, f := range directions {
		pots = append(pots, DecoratedPot{Facing: f})
	}
	return
}
