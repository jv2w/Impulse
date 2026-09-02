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
