package world

import "sync/atomic"

// GameRules holds toggles for World behaviour that a server may want to turn
// off to save the work it costs. The zero value of every field leaves the World
// behaving exactly as vanilla does, so a GameRules that is not filled in
// changes nothing.
type GameRules struct {
	// FixedLight pins the light level reported by Tx.Light and Tx.SkyLight to
	// its value and skips light calculation entirely for chunks loaded while it
	// is set. The value is clamped to 0-15, where 0 keeps vanilla lighting.
	// Light is never written to disk or sent to the client, so a change here
	// only affects the server-side queries that crops, grass and beacons make.
	FixedLight int
	// ExplosionsDisabled stops every explosion before it computes anything: no
	// blocks are broken, no entities are hurt or knocked back, and no sound or
	// particle is shown.
	ExplosionsDisabled bool
	// PickupScanPeriod is the number of ticks between two scans of a dropped
	// item entity for collectors to be picked up by and other stacks to merge
	// with. 0 and 1 both scan every tick, as vanilla does. A higher value
	// divides the scanning cost by the period, at the cost of an item being
	// picked up or merged up to PickupScanPeriod-1 ticks later.
	PickupScanPeriod int
	// ExperienceOrbsDisabled stops experience orbs from being spawned at all.
	// The experience they would have carried is destroyed rather than credited.
	ExperienceOrbsDisabled bool
	// HungerDisabled stops the food level of players from ever dropping. No
	// exhaustion is accumulated, from any source.
	HungerDisabled bool
	// FallDamageDisabled stops players from taking fall damage. A block landed
	// on still sees the landing, so slime blocks still bounce, farmland is
	// still trampled and hay bales still break the fall.
	FallDamageDisabled bool
}

// gameRules is the form GameRules is stored in on a World. The tick loop, the
// chunk load workers and any goroutine calling into the World all read these,
// so each rule is an atomic that its reader loads on its own: a rule that is
// off costs one load and one predicted branch, and nothing else.
type gameRules struct {
	fixedLight             atomic.Int32
	explosionsDisabled     atomic.Bool
	pickupScanPeriod       atomic.Int32
	experienceOrbsDisabled atomic.Bool
	hungerDisabled         atomic.Bool
	fallDamageDisabled     atomic.Bool
}

// store writes rules to r, clamping the two numeric rules so that no reader has
// to check what a caller passed.
func (r *gameRules) store(rules GameRules) {
	r.fixedLight.Store(int32(min(max(rules.FixedLight, 0), 15)))
	r.explosionsDisabled.Store(rules.ExplosionsDisabled)
	r.pickupScanPeriod.Store(int32(max(rules.PickupScanPeriod, 1)))
	r.experienceOrbsDisabled.Store(rules.ExperienceOrbsDisabled)
	r.hungerDisabled.Store(rules.HungerDisabled)
	r.fallDamageDisabled.Store(rules.FallDamageDisabled)
}

// GameRules returns the game rules currently set on the World. Each rule is
// read on its own, so a GameRules returned while another goroutine is calling
// SetGameRules may hold some of the new rules and some of the old ones.
func (w *World) GameRules() GameRules {
	if w == nil {
		return GameRules{}
	}
	return GameRules{
		FixedLight:             w.FixedLight(),
		ExplosionsDisabled:     w.ExplosionsDisabled(),
		PickupScanPeriod:       w.PickupScanPeriod(),
		ExperienceOrbsDisabled: w.ExperienceOrbsDisabled(),
		HungerDisabled:         w.HungerDisabled(),
		FallDamageDisabled:     w.FallDamageDisabled(),
	}
}

// SetGameRules replaces all game rules of the World with those passed. Chunks
// that are already loaded keep the light they were loaded with, so a change to
// GameRules.FixedLight only reaches chunks loaded after this call.
func (w *World) SetGameRules(rules GameRules) {
	if w == nil {
		return
	}
	w.gameRules.store(rules)
}

// FixedLight returns the level that GameRules.FixedLight pins light to, or 0 if
// the World is lit as it is in vanilla.
func (w *World) FixedLight() int {
	if w == nil {
		return 0
	}
	return int(w.gameRules.fixedLight.Load())
}

// ExplosionsDisabled returns whether GameRules.ExplosionsDisabled is set.
func (w *World) ExplosionsDisabled() bool {
	if w == nil {
		return false
	}
	return w.gameRules.explosionsDisabled.Load()
}

// PickupScanPeriod returns the number of ticks between two pickup scans of a
// dropped item. The value returned is never below 1. The floor is applied here
// as well as in store because a World that never went through Config.New, such
// as the placeholder a Server holds until its worlds are created, has never had
// any rule stored and would otherwise report a period of 0.
func (w *World) PickupScanPeriod() int {
	if w == nil {
		return 1
	}
	return int(max(w.gameRules.pickupScanPeriod.Load(), 1))
}

// ExperienceOrbsDisabled returns whether GameRules.ExperienceOrbsDisabled is
// set.
func (w *World) ExperienceOrbsDisabled() bool {
	if w == nil {
		return false
	}
	return w.gameRules.experienceOrbsDisabled.Load()
}

// HungerDisabled returns whether GameRules.HungerDisabled is set.
func (w *World) HungerDisabled() bool {
	if w == nil {
		return false
	}
	return w.gameRules.hungerDisabled.Load()
}

// FallDamageDisabled returns whether GameRules.FallDamageDisabled is set.
func (w *World) FallDamageDisabled() bool {
	if w == nil {
		return false
	}
	return w.gameRules.fallDamageDisabled.Load()
}
