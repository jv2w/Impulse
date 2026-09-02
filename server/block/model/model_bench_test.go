package model

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
)

// benchBoxes keeps the benchmarked BBox results alive so the calls are not
// optimised away.
var benchBoxes []cube.BBox

func BenchmarkHopperBBox(b *testing.B) {
	src := fakeSource{}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = Hopper{}.BBox(cube.Pos{}, src)
	}
}

func BenchmarkWallBBox(b *testing.B) {
	src := fakeSource{}
	m := Wall{NorthConnection: 1, EastConnection: 1, SouthConnection: 1, WestConnection: 1}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = m.BBox(cube.Pos{}, src)
	}
}

func BenchmarkFenceBBox(b *testing.B) {
	pos := cube.Pos{0, 0, 0}
	src := fakeSource{}
	for _, f := range cube.HorizontalFaces() {
		src[pos.Side(f)] = fakeBlock{model: Fence{Wood: true}}
	}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = Fence{Wood: true}.BBox(pos, src)
	}
}

// BenchmarkFenceBBoxOpen benchmarks a fence with nothing around it, the case
// where a side has to be rejected by every connection check in turn.
func BenchmarkFenceBBoxOpen(b *testing.B) {
	src := fakeSource{}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = Fence{Wood: true}.BBox(cube.Pos{}, src)
	}
}

func BenchmarkThinBBox(b *testing.B) {
	pos := cube.Pos{0, 0, 0}
	src := fakeSource{}
	for _, f := range cube.HorizontalFaces() {
		src[pos.Side(f)] = fakeBlock{model: Thin{}}
	}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = Thin{}.BBox(pos, src)
	}
}

// BenchmarkThinBBoxOpen benchmarks a pane with nothing around it, the case
// where a side has to be rejected by every connection check in turn.
func BenchmarkThinBBoxOpen(b *testing.B) {
	src := fakeSource{}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = Thin{}.BBox(cube.Pos{}, src)
	}
}

func BenchmarkStairBBox(b *testing.B) {
	src := fakeSource{}
	m := Stair{Facing: cube.North}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = m.BBox(cube.Pos{}, src)
	}
}

func BenchmarkStairBBoxInnerCorner(b *testing.B) {
	pos := cube.Pos{0, 0, 0}
	m := Stair{Facing: cube.North}
	src := fakeSource{pos.Side(cube.FaceSouth): fakeBlock{model: Stair{Facing: cube.East}}}

	b.ReportAllocs()
	for range b.N {
		benchBoxes = m.BBox(pos, src)
	}
}
