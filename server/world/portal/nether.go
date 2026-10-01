package portal

import (
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// Nether contains information about a nether portal structure. Values returned from this package are tied to the
// transaction that created them and must not be retained after that transaction finishes.
type Nether struct {
	w, h      int
	framed    bool
	axis      cube.Axis
	tx        *world.Tx
	positions []cube.Pos
}

const (
	minimumNetherPortalWidth, maximumNetherPortalWidth   = 2, 21
	minimumNetherPortalHeight, maximumNetherPortalHeight = 3, 21

	minimumNetherPortalArea = minimumNetherPortalWidth * minimumNetherPortalHeight
)

// NetherPortalFromPos returns Nether portal information from a given position in the frame.
func NetherPortalFromPos(tx *world.Tx, pos cube.Pos) (Nether, bool) {
	axis, positions, width, height, completed, ok := multiAxisScan(pos, tx, matchesNetherPortalInterior)
	if !ok {
		axis, positions, width, height, completed, ok = multiAxisScan(pos, tx, matchesNetherPortal)
	}
	if !ok {
		return Nether{}, false
	}
	return Nether{
		w: width, h: height,
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
	if tx.World().Handler().HandlePortalActivate(ctx, portal(p.axis), positions); ctx.Cancelled() {
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

// frameBlock represents a block that can be used as a frame for a Nether portal.
type frameBlock interface {
	// Frame returns true if the block is used as a frame for a Nether portal.
	Frame() bool
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

// Positions ...
func (n Nether) Positions() []cube.Pos {
	return n.positions
}
