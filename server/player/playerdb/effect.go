package playerdb

import "github.com/jv2w/Impulse/server/entity/effect"

func effectsToData(effects []effect.Effect) []jsonEffect {
	// Which input was the problem: an effect whose type is not registered, such
	// as a plugin's own. Its slot was left as a zero entry, stored, and loaded
	// back as an effect of level 0, which panics the world when it is added.
	// Such effects are left out.
	data := make([]jsonEffect, 0, len(effects))
	for _, eff := range effects {
		id, ok := effect.ID(eff.Type())
		if !ok {
			continue
		}
		data = append(data, jsonEffect{
			ID:              id,
			Duration:        eff.Duration(),
			Level:           eff.Level(),
			Ambient:         eff.Ambient(),
			ParticlesHidden: eff.ParticlesHidden(),
			Infinite:        eff.Infinite(),
		})
	}
	return data
}

func dataToEffects(data []jsonEffect) []effect.Effect {
	// Which input was the problem: an entry with an unknown ID, a level of 0 or
	// below or a negative duration, from a corrupt record or a zero entry saved
	// by an older build. It became an effect the effect manager panics on when
	// the player first ticks, which ended the process on every join of that
	// player. Such entries are left out.
	effects := make([]effect.Effect, 0, len(data))
	for _, d := range data {
		e, ok := effect.ByID(d.ID)
		if !ok || d.Level <= 0 || (d.Duration < 0 && !d.Infinite) {
			continue
		}
		var eff effect.Effect
		switch t := e.(type) {
		case effect.LastingType:
			switch {
			case d.Ambient:
				eff = effect.NewAmbient(t, d.Level, d.Duration)
			case d.Infinite:
				eff = effect.NewInfinite(t, d.Level)
			default:
				eff = effect.New(t, d.Level, d.Duration)
			}

			if d.ParticlesHidden {
				eff = eff.WithoutParticles()
			}
		default:
			eff = effect.NewInstant(t, d.Level)
		}
		effects = append(effects, eff)
	}
	return effects
}
