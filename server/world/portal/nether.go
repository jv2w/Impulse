package portal

import (
	"math"
	"math/rand/v2"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
	"github.com/jv2w/Impulse/server/world"
)

// Nether contains information about a nether portal structure. Values returned from this package are tied to the
// transaction that created them and must not be retained after that transaction finishes.
type Nether struct {
	w, h      int
	framed    bool
	axis      cube.Axis
	tx        *world.Tx
	spawnPos  cube.Pos
	positions []cube.Pos
}

const (
	minimumNetherPortalWidth, maximumNetherPortalWidth   = 2, 21
	minimumNetherPortalHeight, maximumNetherPortalHeight = 3, 21

	minimumNetherPortalArea = minimumNetherPortalWidth * minimumNetherPortalHeight
)

// NetherPortalFromPos returns Nether portal information from a given position in the frame.
func NetherPortalFromPos(tx *world.Tx, pos cube.Pos) (Nether, bool) {
	if tx.World().Dimension() == world.End {
		return Nether{}, false
	}

	axis, positions, width, height, completed, ok := multiAxisScan(pos, tx, matchesNetherPortalInterior)
	if !ok {
		axis, positions, width, height, completed, ok = multiAxisScan(pos, tx, matchesNetherPortal)
	}
	if !ok {
		return Nether{}, false
	}
	return Nether{
		w: width, h: height,
		spawnPos:  pos,
		positions: positions,
		framed:    completed,
		axis:      axis,
		tx:        tx,
	}, ok
}

// ActivateNetherPortal activates an inactive framed Nether portal at the position passed.
func ActivateNetherPortal(tx *world.Tx, pos cube.Pos) bool {
	p, ok := NetherPortalFromPos(tx, pos)
	if !ok || !p.Framed() || p.Activated() {
		return false
	}
	ctx := tx.Event()
	positions := append([]cube.Pos(nil), p.Positions()...)
	if tx.World().Handler().HandlePortalActivate(ctx, world.Nether, positions); ctx.Cancelled() {
		return false
	}
	p.Activate()
	return true
}

// DeactivateNetherPortal deactivates the connected Nether portal at the position passed.
func DeactivateNetherPortal(tx *world.Tx, pos cube.Pos) bool {
	_, positions, ok := connectedNetherPortal(tx, pos)
	if !ok {
		return false
	}
	deactivate(tx, positions)
	return true
}

// FindOrCreateNetherPortal finds or creates a Nether portal at the given position.
func FindOrCreateNetherPortal(tx *world.Tx, pos cube.Pos, radius int) (Nether, bool) {
	n, ok := FindNetherPortal(tx, pos, radius)
	if ok {
		return n, true
	}
	return CreateNetherPortal(tx, pos)
}

// portalBlock represents a block that can be used as a portal to travel between dimensions.
type portalBlock interface {
	// Portal returns the dimension that the portal leads to.
	Portal() world.Dimension
}

// frameBlock represents a block that can be used as a frame for a Nether portal.
type frameBlock interface {
	// Frame returns true if the block is used as a frame for the given dimension.
	Frame(dimension world.Dimension) bool
}

// FindNetherPortal searches a provided radius for a Nether portal.
func FindNetherPortal(tx *world.Tx, pos cube.Pos, radius int) (Nether, bool) {
	if tx.World().Dimension() == world.End {
		return Nether{}, false
	}

	closest, closestDist, found := Nether{}, math.MaxFloat64, false
	seen := make(map[cube.Pos]struct{})
	// pos does not change, so the same Vec3 is subtracted as before; it is converted once instead of once
	// per portal block visited.
	posVec := pos.Vec3()
	for selectedPos := range tx.BlocksWithin(pos, radius, portal(cube.X), portal(cube.Z)) {
		if _, ok := seen[selectedPos]; ok {
			// Part of a portal that was already validated through an earlier block.
			continue
		}
		if n, ok := NetherPortalFromPos(tx, selectedPos); ok && n.Framed() && n.Activated() {
			for _, p := range n.Positions() {
				seen[p] = struct{}{}
				if dist := p.Vec3().Sub(posVec).Len(); dist < closestDist {
					closestDist, closest, found = dist, n, true
				}
			}
		}
	}
	return closest, found
}

// CreateNetherPortal creates a Nether portal at the given position.
func CreateNetherPortal(tx *world.Tx, pos cube.Pos) (Nether, bool) {
	if tx.World().Dimension() == world.End {
		return Nether{}, false
	}

	resultPos, random, distance, a, r := pos, rand.IntN(4), -1.0, 0, tx.Range()
	// The registry lookup behind air returns the same block value on every call, and the scan below reaches
	// it once per block tested, so it is resolved once here. pos and r do not change either.
	airBlock := air()
	posX, posY, posZ := pos.X(), pos.Y(), pos.Z()
	minY, maxY := r.Min(), r.Max()
	searchValidArea := func(directions int, valid func(pos cube.Pos, riv int, coEff1, coEff2 int) bool) {
		for tempX := posX - 16; tempX <= posX+16; tempX++ {
			offsetX := float64(tempX-posX) + 0.5
			for tempZ := posZ - 16; tempZ <= posZ+16; tempZ++ {
				offsetZ := float64(tempZ-posZ) + 0.5
				for tempY := maxY - 1; tempY >= minY; tempY-- {
					entryPos := cube.Pos{tempX, tempY, tempZ}
					if tx.Block(entryPos) != airBlock {
						continue
					}

					for tempY > minY && tx.Block(entryPos.Side(cube.FaceDown)) == airBlock {
						tempY--
						entryPos[1]--
					}

					for riv := random; riv < random+directions; riv++ {
						coEff1 := riv % 2
						coEff2 := 1 - coEff1

						if !valid(entryPos, riv, coEff1, coEff2) {
							break
						}

						offsetY := float64(tempY-posY) + 0.5
						newDist := offsetX*offsetX + offsetY*offsetY + offsetZ*offsetZ
						if distance < 0.0 || newDist < distance {
							distance = newDist
							a = riv % directions
							resultPos = cube.Pos{tempX, tempY, tempZ}
						}
					}
				}
			}
		}
	}

	// Search for a valid area in all four directions, adding some extra space for comfort.
	searchValidArea(4, func(pos cube.Pos, riv int, coEff1, coEff2 int) bool {
		if riv%4 >= 2 {
			coEff1 = -coEff1
			coEff2 = -coEff2
		}

		// Integer addition regroups exactly, so the coordinates are the same ones the loop computed; the
		// terms that do not depend on the inner counters are now added once per level. The model is only
		// consumed below the floor, so it is no longer read for the positions that ignore it.
		cx, cy, cz := pos.X(), pos.Y(), pos.Z()
		for safeSpace1 := range 3 {
			x1, z1 := cx+safeSpace1*coEff2, cz-safeSpace1*coEff1
			for safeSpace2 := -1; safeSpace2 < 3; safeSpace2++ {
				x2, z2 := x1+safeSpace2*coEff1, z1+safeSpace2*coEff2
				for height := -1; height < 4; height++ {
					b := tx.Block(cube.Pos{x2, cy + height, z2})
					if height < 0 {
						if _, solid := b.Model().(model.Solid); !solid {
							return false
						}
					} else if b != airBlock {
						return false
					}
				}
			}
		}
		return true
	})

	if distance < 0.0 {
		// If we couldn't find a valid area under those specifications, we can search the two main directions instead,
		// reducing comfort but at least allowing us to have a portal in the area.
		searchValidArea(2, func(pos cube.Pos, riv int, coEff1, coEff2 int) bool {
			// Same as above: the x and z terms do not depend on height, and the model is only consumed
			// below the floor, so it is no longer read for the positions that ignore it.
			cx, cy, cz := pos.X(), pos.Y(), pos.Z()
			for safeSpace := range 3 {
				x, z := cx+safeSpace*coEff1, cz+safeSpace*coEff2
				for height := -1; height < 4; height++ {
					b := tx.Block(cube.Pos{x, cy + height, z})
					if height < 0 {
						if _, solid := b.Model().(model.Solid); !solid {
							return false
						}
					} else if b != airBlock {
						return false
					}
				}
			}
			return true
		})
	}

	coEff1 := a % 2
	coEff2 := 1 - coEff1
	if a%4 >= 2 {
		coEff1 = -coEff1
		coEff2 = -coEff2
	}

	axis := cube.X
	if coEff1 == 0 {
		axis = cube.Z
	}

	// Buffer the blocks the portal is made up of so a world.Handler may cancel the creation before any of them are
	// written to the world. affected holds each position exactly once, in the order it was first set.
	ob, pb := obsidian(), portal(axis)
	blocks := make(map[cube.Pos]world.Block)
	var affected []cube.Pos
	setBlock := func(pos cube.Pos, b world.Block) {
		if _, ok := blocks[pos]; !ok {
			affected = append(affected, pos)
		}
		blocks[pos] = b
	}

	if distance < 0.0 {
		// If all else fails, we can simply create a floating platform in the void with the portal on it.
		resultPos[1] = min(max(resultPos[1], 70), r.Max()-10)
		for safeBeforeAfter := -1; safeBeforeAfter <= 1; safeBeforeAfter++ {
			for safeWidth := range 2 {
				for height := -1; height < 3; height++ {
					entryPos := cube.Pos{
						resultPos.X() + safeWidth*coEff1 + safeBeforeAfter*coEff2,
						resultPos.Y() + height,
						resultPos.Z() + safeWidth*coEff2 - safeBeforeAfter*coEff1,
					}

					if height < 0 {
						setBlock(entryPos, ob)
					} else {
						setBlock(entryPos, nil)
					}
				}
			}
		}
	}

	// Build the portal frame and activate it.
	var positions []cube.Pos
	for width := -1; width < 3; width++ {
		for height := -1; height < 4; height++ {
			entryPos := cube.Pos{
				resultPos.X() + width*coEff1,
				resultPos.Y() + height,
				resultPos.Z() + width*coEff2,
			}

			if width == -1 || width == 2 || height == -1 || height == 3 {
				setBlock(entryPos, ob)
				continue
			}
			positions = append(positions, entryPos)
			setBlock(entryPos, pb)
		}
	}

	ctx := tx.Event()
	handlerPositions := append([]cube.Pos(nil), affected...)
	if tx.World().Handler().HandlePortalCreate(ctx, world.Nether, handlerPositions); ctx.Cancelled() {
		return Nether{}, false
	}
	for _, pos := range affected {
		tx.SetBlock(pos, blocks[pos], nil)
	}

	return Nether{
		w:         minimumNetherPortalWidth,
		h:         minimumNetherPortalHeight,
		framed:    true,
		spawnPos:  resultPos,
		positions: positions,
		axis:      axis,
		tx:        tx,
	}, true
}

// Activate ...
func (n Nether) Activate() {
	// With no positions there was nothing to set, and otherwise the registry lookup behind portal returns
	// the same block for the same axis on every call, so resolving it once sets the very same block.
	positions := n.Positions()
	if len(positions) == 0 {
		return
	}
	pb := portal(n.axis)
	for _, pos := range positions {
		n.tx.SetBlock(pos, pb, nil)
	}
}

func deactivate(tx *world.Tx, positions []cube.Pos) {
	for _, pos := range positions {
		tx.SetBlock(pos, nil, nil)
	}
}

// Framed ...
func (n Nether) Framed() bool {
	return n.framed
}

// Activated ...
func (n Nether) Activated() bool {
	// With no positions the loop never ran and true was returned, and otherwise the registry lookup behind
	// portal returns the same block for the same axis on every call, so the comparison is unchanged.
	positions := n.Positions()
	if len(positions) == 0 {
		return true
	}
	pb := portal(n.axis)
	for _, pos := range positions {
		if n.tx.Block(pos) != pb {
			return false
		}
	}
	return true
}

// Spawn ...
func (n Nether) Spawn() cube.Pos {
	return n.spawnPos
}

// Positions ...
func (n Nether) Positions() []cube.Pos {
	return n.positions
}
