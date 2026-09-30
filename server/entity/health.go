package entity

import "math"

// HealthManager handles the health of an entity.
type HealthManager struct {
	health float64
	max    float64
}

// NewHealthManager returns a new health manager with the health and max health provided.
func NewHealthManager(health, max float64) *HealthManager {
	if health > max {
		health = max
	}
	return &HealthManager{health: health, max: max}
}

// Health returns the current health of an entity.
func (m *HealthManager) Health() float64 {
	return m.health
}

// AddHealth adds a given amount of health points to the player. If the health added to the current health
// exceeds the max, health will be set to the max. If the health is instead negative and results in a health
// lower than 0, the final health will be 0.
func (m *HealthManager) AddHealth(health float64) {
	// Which input was the problem: a NaN amount, as from a damage computed by a
	// plugin. min and max return NaN, and a NaN health is never below or above
	// anything again, so the entity could neither die nor heal.
	if math.IsNaN(health) {
		return
	}
	m.health = max(min(m.health+health, m.max), 0)
}

// MaxHealth returns the maximum health of the entity.
func (m *HealthManager) MaxHealth() float64 {
	return m.max
}

// SetMaxHealth changes the max health of an entity to the maximum passed. If the maximum is set to 0 or
// lower, SetMaxHealth will default to a value of 1.
func (m *HealthManager) SetMaxHealth(max float64) {
	// A NaN maximum is treated like one that is too low, for the reason given
	// in AddHealth.
	if max <= 0 || math.IsNaN(max) {
		max = 1
	}
	m.max = max
	m.health = min(m.health, max)
}
