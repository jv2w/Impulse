package player

import (
	"sync"
)

// hungerManager handles the changes in hunger, exhaustion and saturation of a player.
type hungerManager struct {
	mu              sync.RWMutex
	foodLevel       int
	saturationLevel float64
	exhaustionLevel float64
	foodTick        int
}

// newHungerManager returns a new hunger manager with the default values for food level, saturation level and
// exhaustion level.
func newHungerManager() *hungerManager {
	return &hungerManager{foodLevel: 20, saturationLevel: 5, foodTick: 1}
}

// Food returns the current food level of a player. The level returned is guaranteed to always be between 0
// and 20.
func (m *hungerManager) Food() int {
	// A single field read cannot panic, so the deferred unlock is replaced by a
	// direct one. The lock is held over exactly the same read.
	m.mu.RLock()
	level := m.foodLevel
	m.mu.RUnlock()
	return level
}

// SetFood sets the food level of a player. The level passed must be in a range of 0-20. If the level passed
// is negative, the food level will be set to 0. If the level exceeds 20, the food level will be set to 20.
func (m *hungerManager) SetFood(level int) {
	// max/min on ints are builtins that cannot panic, so the deferred unlock is
	// replaced by a direct one over the same critical section.
	m.mu.Lock()
	m.foodLevel = max(min(level, 20), 0)
	m.mu.Unlock()
}

// AddFood adds a number of food points to the current food level of a player.
func (m *hungerManager) AddFood(points int) {
	// Same as SetFood: the critical section is one arithmetic field update that
	// cannot panic, so no defer frame is needed.
	m.mu.Lock()
	m.foodLevel = max(min(m.foodLevel+points, 20), 0)
	m.mu.Unlock()
}

// Reset resets the hunger manager to its default values, identical to those set when creating a new manager
// using newHungerManager.
func (m *hungerManager) Reset() {
	// Four constant field writes cannot panic, so the defer frame is dropped.
	m.mu.Lock()
	m.foodLevel = 20
	m.saturationLevel = 5
	m.exhaustionLevel = 0
	m.foodTick = 1
	m.mu.Unlock()
}

// ResetExhaustion resets the player's exhaustion level to 0. It prevents the
// player's food level from decreasing immediately after cancelling food loss.
func (m *hungerManager) resetExhaustion() {
	// Three constant field writes cannot panic, so the defer frame is dropped.
	m.mu.Lock()
	m.exhaustionLevel = 0
	m.saturationLevel = 0
	m.foodTick = 1
	m.mu.Unlock()
}

// exhaust exhausts the player by the amount of points passed. If the total exhaustion level exceeds 4, a
// saturation point, or food point, if saturation is 0, will be subtracted.
func (m *hungerManager) exhaust(points float64) {
	// desaturate only reads and writes fields of m, so nothing in the loop can
	// panic and the deferred unlock is replaced by a direct one.
	// Which input was the problem: a movement packet placing the player about
	// 1e38 blocks away, the float32 limit, which exhausts by a tenth of the
	// distance. The loop below ran once per 4 points, about 1e37 times, holding
	// the world goroutine. Past 40 passes, which empty any food and saturation,
	// the rest is dropped.
	m.mu.Lock()
	m.exhaustionLevel += points
	for i := 0; m.exhaustionLevel >= 4; i++ {
		if i == 40 {
			m.exhaustionLevel = 0
			break
		}
		// Maximum exhaustion value is 4, so keep removing one saturation point until the exhaustion level
		// is below 4.
		m.exhaustionLevel -= 4
		m.desaturate()
	}
	m.mu.Unlock()
}

// saturate saturates the player's food and saturation by the amount of points passed. Note that the total
// saturation will never exceed the total food value.
func (m *hungerManager) saturate(food int, saturation float64) {
	// Arithmetic on two fields with builtin max/min cannot panic, so the defer
	// frame is dropped.
	m.mu.Lock()
	m.foodLevel = max(min(m.foodLevel+food, 20), 0)
	m.saturationLevel = max(min(m.saturationLevel+saturation, float64(m.foodLevel)), 0)
	m.mu.Unlock()
}

// desaturate removes one saturation point from the player. If the saturation level of the player is already
// 0, a point will be subtracted from the food level instead. If that level, too, is already 0, nothing will
// happen.
func (m *hungerManager) desaturate() {
	if m.saturationLevel <= 0 && m.foodLevel != 0 {
		m.foodLevel--
	} else if m.saturationLevel > 0 {
		m.saturationLevel = max(m.saturationLevel-1, 0)
	}
}

// canQuicklyRegenerate checks if the player can quickly regenerate. The function returns true if Food() returns 20
// and the player still has saturation left.
// The rate of regeneration is 1/0.5 seconds.
func (m *hungerManager) canQuicklyRegenerate() bool {
	// Two field reads cannot panic, so the defer frame is dropped.
	m.mu.RLock()
	quick := m.foodLevel == 20 && m.saturationLevel > 0
	m.mu.RUnlock()
	return quick
}

// canRegenerate checks if the player with the amount of food levels in the hunger manager can regenerate.
// The function returns true if Food() returns either 18-20.
// The rate of regeneration is 1/4 seconds.
func (m *hungerManager) canRegenerate() bool {
	return m.Food() >= 18
}

// canSprint returns true if the food level of the player is 7 or higher.
func (m *hungerManager) canSprint() bool {
	return m.Food() > 6
}

// starving checks if the player is currently considered to be starving. True is returned if Food() returns 0.
func (m *hungerManager) starving() bool {
	return m.Food() == 0
}

// StarvationDamageSource is the world.DamageSource passed when a player is
// dealt damage from an empty food bar.
type StarvationDamageSource struct{}

func (StarvationDamageSource) ReducedByArmour() bool     { return false }
func (StarvationDamageSource) ReducedByResistance() bool { return false }
func (StarvationDamageSource) Fire() bool                { return false }
func (StarvationDamageSource) IgnoreTotem() bool         { return false }
