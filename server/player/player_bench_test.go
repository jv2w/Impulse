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

	data := &world.EntityData{Pos: mgl64.Vec3{8.5, 65, 8.5}}
	Config{EnchantmentSeed: 1}.Apply(data)

	w.Do(func(tx *world.Tx) {
		for x := 7; x <= 10; x++ {
			for z := 7; z <= 10; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		p := &Player{
			tx:         tx,
			handle:     world.EntitySpawnOpts{ID: stubUUID}.New(Type, Config{EnchantmentSeed: 1}),
			data:       data,
			playerData: data.Data.(*playerData),
		}
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
