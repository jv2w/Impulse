package player

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// benchPlayer builds a Player standing on a stone floor in a Synchronous world
// and runs f against it. The stub fixtures in player_stub_test.go hold a nil
// *World, so they cannot reach any of the block reads the tick path makes;
// this is the smallest fixture that can.
func benchPlayer(b *testing.B, f func(p *Player)) {
	b.Helper()

	w := world.Config{Synchronous: true, RandSource: rand.NewPCG(1, 1)}.New()
	defer w.Close()

	// Config.Apply overwrites data.Pos with Config.Position, so the standing
	// position has to come from the Config. A player left at the origin falls
	// out of the world and takes void damage every tick instead.
	cfg := Config{EnchantmentSeed: 1, Position: mgl64.Vec3{8.5, 65, 8.5}}
	data := &world.EntityData{}
	cfg.Apply(data)

	w.Do(func(tx *world.Tx) {
		for x := 6; x <= 11; x++ {
			for z := 6; z <= 11; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		p := &Player{
			tx:         tx,
			handle:     world.EntitySpawnOpts{ID: stubUUID}.New(Type, cfg),
			data:       data,
			playerData: data.Data.(*playerData),
		}
		// One tick settles the movement computer onto the floor, so the
		// measured ticks are the standing-still case.
		p.Tick(tx, 0)
		b.ReportAllocs()
		b.ResetTimer()
		f(p)
		b.StopTimer()
	}).Wait(context.Background())
}

// BenchmarkCheckBlockCollisions measures the block box sweep Tick runs for
// every player every tick, with the small downward velocity a standing player
// carries.
func BenchmarkCheckBlockCollisions(b *testing.B) {
	benchPlayer(b, func(p *Player) {
		vel := mgl64.Vec3{0, -0.0784, 0}
		for range b.N {
			p.checkBlockCollisions(vel)
		}
	})
}

// BenchmarkCheckOnGround measures the second block sweep of the tick path.
func BenchmarkCheckOnGround(b *testing.B) {
	benchPlayer(b, func(p *Player) {
		for range b.N {
			p.checkOnGround(mgl64.Vec3{})
		}
	})
}

// BenchmarkTick measures a whole tick of a survival player standing still with
// nothing held, which is what the tick loop spends its time on for most of the
// players on a server.
func BenchmarkTick(b *testing.B) {
	benchPlayer(b, func(p *Player) {
		for i := range b.N {
			p.Tick(p.tx, int64(i))
		}
	})
}
