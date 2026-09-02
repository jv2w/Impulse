package trace

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/block/model"
)

// benchResult keeps the benchmarked results alive so the calls are not
// optimised away.
var (
	benchResult BlockResult
	benchOK     bool
)

func BenchmarkBlockInterceptSolid(b *testing.B) {
	pos := cube.Pos{0, 0, 0}
	src := testSource{}
	blk := testBlock{model: model.Solid{}}
	start, end := mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{2, 0.5, 0.5}

	b.ReportAllocs()
	for range b.N {
		benchResult, benchOK = BlockIntercept(pos, src, blk, start, end)
	}
}

func BenchmarkBlockInterceptMultipleBoxes(b *testing.B) {
	pos := cube.Pos{0, 0, 0}
	src := testSource{}
	blk := testBlock{model: model.Hopper{}}
	start, end := mgl64.Vec3{-1, 0.3, 0.5}, mgl64.Vec3{2, 0.3, 0.5}

	b.ReportAllocs()
	for range b.N {
		benchResult, benchOK = BlockIntercept(pos, src, blk, start, end)
	}
}

func BenchmarkBBoxIntercept(b *testing.B) {
	bb := cube.Box(0, 0, 0, 1, 1, 1)
	start, end := mgl64.Vec3{-1, 0.5, 0.5}, mgl64.Vec3{2, 0.5, 0.5}

	b.ReportAllocs()
	for range b.N {
		_, benchOK = BBoxIntercept(bb, start, end)
	}
}
