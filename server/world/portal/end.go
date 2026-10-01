package portal

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// endRingFrame is one of the twelve canonical ring positions and the Facing each frame must have.
type endRingFrame struct {
	pos    cube.Pos
	facing cube.Direction
}

// endFrameBlock is implemented by block.EndPortalFrame, which cannot be imported here directly.
type endFrameBlock interface {
	world.Block
	EndPortalFrameState() (eye bool, facing cube.Direction)
}

// ActivateEndPortal fills the 3x3 interior with end_portal blocks if a complete twelve-frame ring exists around the
// frame at the position passed. All twelve frames must hold an eye and face toward the centre, as in vanilla.
func ActivateEndPortal(tx *world.Tx, framePos cube.Pos) bool {
	f, ok := tx.Block(framePos).(endFrameBlock)
	if !ok {
		return false
	}
	_, facing := f.EndPortalFrameState()

	// The frame may be the left, middle or right of its side: walk inward twice, then try the three candidate centres.
	inward, tangent := facing.Face(), facing.RotateRight().Face()
	base := framePos.Side(inward).Side(inward)
	for _, center := range []cube.Pos{base.Side(tangent.Opposite()), base, base.Side(tangent)} {
		interior, ok := matchEndRing(tx, center)
		if !ok {
			continue
		}
		ep := endPortal()
		active := true
		for _, pos := range interior {
			if tx.Block(pos) != ep {
				active = false
				break
			}
		}
		if active {
			return false
		}
		ctx := tx.Event()
		positions := append([]cube.Pos(nil), interior...)
		if tx.World().Handler().HandlePortalActivate(ctx, world.End, positions); ctx.Cancelled() {
			return false
		}
		for _, pos := range interior {
			if tx.Block(pos) != ep {
				tx.SetBlock(pos, ep, nil)
			}
		}
		return true
	}
	return false
}

// EndPortalRingIntact reports whether an intact twelve-frame ring still surrounds the end_portal block at the
// position passed, which may be any of the nine interior positions.
func EndPortalRingIntact(tx *world.Tx, portalPos cube.Pos) bool {
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if _, ok := matchEndRing(tx, portalPos.Add(cube.Pos{dx, 0, dz})); ok {
				return true
			}
		}
	}
	return false
}

// DeactivateEndPortal clears every end_portal block reachable from the position passed through cardinal neighbours
// on the same y plane, removing a single portal's interior without touching unrelated portals.
func DeactivateEndPortal(tx *world.Tx, portalPos cube.Pos) {
	ep := endPortal()
	if tx.Block(portalPos) != ep {
		return
	}
	var positions []cube.Pos
	queue := []cube.Pos{portalPos}
	seen := map[cube.Pos]struct{}{portalPos: {}}
	// HorizontalFaces returns the same package level array on every call, so reading it once yields the very
	// same slice the loop walked before.
	faces := cube.HorizontalFaces()
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if tx.Block(p) != ep {
			continue
		}
		positions = append(positions, p)
		for _, face := range faces {
			n := p.Side(face)
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			queue = append(queue, n)
		}
	}
	deactivate(tx, positions)
}

// matchEndRing returns the 3x3 interior positions if the twelve canonical ring positions around centre all hold
// matching frames.
func matchEndRing(tx *world.Tx, center cube.Pos) ([]cube.Pos, bool) {
	for _, want := range expectedEndRingFrames(center) {
		b, ok := tx.Block(want.pos).(endFrameBlock)
		if !ok {
			return nil, false
		}
		eye, facing := b.EndPortalFrameState()
		if !eye || facing != want.facing {
			return nil, false
		}
	}
	return endRingInterior(center), true
}

// expectedEndRingFrames returns the twelve (position, facing) pairs a complete ring around the centre must have, with
// every frame facing toward the centre.
func expectedEndRingFrames(center cube.Pos) []endRingFrame {
	frames := make([]endRingFrame, 0, 12)
	for _, side := range cube.Directions() {
		base := center.Side(side.Face()).Side(side.Face())
		tangent := side.RotateRight().Face()
		inward := side.Opposite()
		for _, pos := range []cube.Pos{base.Side(tangent.Opposite()), base, base.Side(tangent)} {
			frames = append(frames, endRingFrame{pos: pos, facing: inward})
		}
	}
	return frames
}

// endRingInterior returns the nine 3x3 interior positions on the y plane of centre.
func endRingInterior(center cube.Pos) []cube.Pos {
	out := make([]cube.Pos, 0, 9)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			out = append(out, center.Add(cube.Pos{dx, 0, dz}))
		}
	}
	return out
}

// endPortal returns the end_portal block.
func endPortal() world.Block {
	p, ok := world.BlockByName("minecraft:end_portal", nil)
	if !ok {
		panic("could not find end_portal block")
	}
	return p
}
