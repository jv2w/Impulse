package player

import (
	"math/rand/v2"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/entity"
	"github.com/jv2w/Impulse/server/world"
)

// TestHungerRuleGovernsEveryExhaustion pins that the rule cuts food loss off at
// Player.Exhaust, the one point every source of exhaustion goes through, so no
// amount of exhaustion from any caller moves the food level.
func TestHungerRuleGovernsEveryExhaustion(t *testing.T) {
	for _, test := range []struct {
		name           string
		rules          world.GameRules
		wantFood       int
		wantExhaustion float64
	}{
		{name: "unset drains as vanilla", rules: world.GameRules{}, wantFood: 13, wantExhaustion: 0},
		{
			name:           "disabled never drains",
			rules:          world.GameRules{HungerDisabled: true},
			wantFood:       20,
			wantExhaustion: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			gameRulePlayer(t, test.rules, func(p *Player) {
				// Twelve exhaustion units: the first five spend the starting
				// saturation, the remaining seven take food points.
				for range 12 {
					p.Exhaust(4)
				}
				if got := p.Food(); got != test.wantFood {
					t.Errorf("food = %d, want %d", got, test.wantFood)
				}
				if got := p.hunger.exhaustionLevel; got != test.wantExhaustion {
					t.Errorf("exhaustion = %v, want %v", got, test.wantExhaustion)
				}
			})
		})
	}
}

// TestFallDamageRuleGovernsTheDamageOnly pins that the rule drops the damage of
// a landing without dropping the landing: the block landed on is still told
// about it, which is what slime blocks, farmland and hay bales react to.
func TestFallDamageRuleGovernsTheDamageOnly(t *testing.T) {
	for _, test := range []struct {
		name       string
		rules      world.GameRules
		wantHealth float64
	}{
		{name: "unset hurts as vanilla", rules: world.GameRules{}, wantHealth: 13},
		{name: "disabled never hurts", rules: world.GameRules{FallDamageDisabled: true}, wantHealth: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			gameRulePlayer(t, test.rules, func(p *Player) {
				// Farmland is the EntityLander whose landing is observable
				// without touching the damage: it trample-reverts to dirt, and
				// it does so for certain once the fall distance is past 1.5.
				under := cube.Pos{8, 63, 8}
				p.tx.SetBlock(under, block.Farmland{}, nil)
				p.fallDistance = 10

				// A ten block fall is 7 damage in vanilla.
				p.fall(10)

				if got := p.Health(); got != test.wantHealth {
					t.Errorf("health = %v, want %v", got, test.wantHealth)
				}
				if _, ok := p.tx.Block(under).(block.Dirt); !ok {
					t.Errorf("block under the player = %T, want the farmland trampled to block.Dirt", p.tx.Block(under))
				}
			})
		})
	}
}

// TestExperienceOrbRuleStopsTheSpawn pins that the rule keeps the orbs from
// being created at all, and that the experience they would have carried is
// destroyed rather than left on the player.
func TestExperienceOrbRuleStopsTheSpawn(t *testing.T) {
	for _, test := range []struct {
		name  string
		rules world.GameRules
		want  bool
	}{
		{name: "unset drops orbs", rules: world.GameRules{}, want: true},
		{name: "disabled drops none", rules: world.GameRules{ExperienceOrbsDisabled: true}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			gameRulePlayer(t, test.rules, func(p *Player) {
				p.SetExperienceLevel(5)
				p.dropItems()

				orbs := 0
				for e := range p.tx.Entities() {
					if e.H().Type() == entity.ExperienceOrbType {
						orbs++
					}
				}
				if got := orbs > 0; got != test.want {
					t.Errorf("experience orbs dropped = %d, want any = %t", orbs, test.want)
				}
				if got := p.ExperienceLevel(); got != 0 {
					t.Errorf("experience level after dropping = %d, want 0", got)
				}
			})
		})
	}
}

// gameRulePlayer builds a Player standing on a stone floor in a synchronous
// World with the rules passed, and runs f against it inside the transaction that
// owns it. The stub fixtures in player_stub_test.go hold a nil *World, which
// reads every rule as vanilla, so they cannot be used here.
func gameRulePlayer(t *testing.T, rules world.GameRules, f func(p *Player)) {
	t.Helper()

	w := world.Config{Synchronous: true, RandSource: rand.NewPCG(1, 1), GameRules: rules}.New()
	t.Cleanup(func() { _ = w.Close() })

	// Config.Apply overwrites data.Pos with Config.Position, so the standing
	// position has to come from the Config.
	cfg := Config{EnchantmentSeed: 1, Position: mgl64.Vec3{8.5, 64, 8.5}}
	data := &world.EntityData{}
	cfg.Apply(data)

	w.Do(func(tx *world.Tx) {
		for x := 6; x <= 11; x++ {
			for z := 6; z <= 11; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		f(&Player{
			tx:         tx,
			handle:     world.EntitySpawnOpts{ID: stubUUID}.New(Type, cfg),
			data:       data,
			playerData: data.Data.(*playerData),
		})
	})
}
