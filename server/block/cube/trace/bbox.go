package trace

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"math"
)

// BBoxResult is the result of a basic ray trace collision with a bounding box.
type BBoxResult struct {
	bb   cube.BBox
	pos  mgl64.Vec3
	face cube.Face
}

// BBox ...
func (r BBoxResult) BBox() cube.BBox {
	return r.bb
}

// Position ...
func (r BBoxResult) Position() mgl64.Vec3 {
	return r.pos
}

// Face ...
func (r BBoxResult) Face() cube.Face {
	return r.face
}

// BBoxIntercept performs a ray trace and calculates the point on the BBox's edge nearest to the start position that the ray trace
// collided with.
// BBoxIntercept returns a BBoxResult with the colliding vector closest to the start position, if no colliding point was found,
// a zero BBoxResult is returned and ok is false.
func BBoxIntercept(bb cube.BBox, start, end mgl64.Vec3) (result BBoxResult, ok bool) {
	min, max := bb.Min(), bb.Max()
	// The six candidates were each returned as a pointer, which put every one
	// of them on the heap. They are held as values now and the nil check is a
	// found flag, so the same six candidates are built in the same order from
	// the same arithmetic without allocating.
	var (
		v     [6]mgl64.Vec3
		found [6]bool
	)
	v[0], found[0] = lineVec3AtX(start, end, min[0])
	v[1], found[1] = lineVec3AtX(start, end, max[0])
	v[2], found[2] = lineVec3AtY(start, end, min[1])
	v[3], found[3] = lineVec3AtY(start, end, max[1])
	v[4], found[4] = lineVec3AtZ(start, end, min[2])
	v[5], found[5] = lineVec3AtZ(start, end, max[2])

	if found[0] && !bb.Vec3WithinYZ(v[0]) {
		found[0] = false
	}
	if found[1] && !bb.Vec3WithinYZ(v[1]) {
		found[1] = false
	}
	if found[2] && !bb.Vec3WithinXZ(v[2]) {
		found[2] = false
	}
	if found[3] && !bb.Vec3WithinXZ(v[3]) {
		found[3] = false
	}
	if found[4] && !bb.Vec3WithinXY(v[4]) {
		found[4] = false
	}
	if found[5] && !bb.Vec3WithinXY(v[5]) {
		found[5] = false
	}

	var (
		// Every candidate was a separate allocation, so comparing the winning
		// pointer against v1..v6 picked out exactly one index. The index is
		// tracked directly instead.
		best = -1
		dist = math.MaxFloat64
	)

	for i, ok := range found {
		if !ok {
			continue
		}

		if d := start.Sub(v[i]).LenSqr(); d < dist {
			best = i
			dist = d
		}
	}

	if best < 0 {
		return
	}

	var f cube.Face
	switch best {
	case 0:
		f = cube.FaceWest
	case 1:
		f = cube.FaceEast
	case 2:
		f = cube.FaceDown
	case 3:
		f = cube.FaceUp
	case 4:
		f = cube.FaceNorth
	case 5:
		f = cube.FaceSouth
	}

	return BBoxResult{bb: bb, pos: v[best], face: f}, true
}

// BBoxIntersects checks if the line segment from start to end intersects the BBox.
// Unlike BBoxIntercept, it only reports whether an intersection exists and does not
// calculate the closest hit position or face.
func BBoxIntersects(bb cube.BBox, start, end mgl64.Vec3) bool {
	min, max := bb.Min(), bb.Max()
	dir := end.Sub(start)
	tMin, tMax := 0.0, 1.0

	for axis := range 3 {
		if mgl64.FloatEqual(dir[axis], 0) {
			if start[axis] < min[axis] || start[axis] > max[axis] {
				return false
			}
			continue
		}

		inv := 1 / dir[axis]
		t1 := (min[axis] - start[axis]) * inv
		t2 := (max[axis] - start[axis]) * inv
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tMin {
			tMin = t1
		}
		if t2 < tMax {
			tMax = t2
		}
		if tMin > tMax {
			return false
		}
	}
	return true
}

// vec3OnLineWithX returns an mgl64.Vec3 on the line between mgl64.Vec3 a and b with an X value passed. If no such vec3
// could be found, the bool returned is false.
func vec3OnLineWithX(a, b mgl64.Vec3, x float64) *mgl64.Vec3 {
	// The pointer form is kept for callers that need one; the value form below
	// runs the same arithmetic and lets the ray trace skip the allocation.
	v, ok := lineVec3AtX(a, b, x)
	if !ok {
		return nil
	}
	return &v
}

// lineVec3AtX is vec3OnLineWithX returning the point by value, so that a
// point that stays inside the caller is not put on the heap.
func lineVec3AtX(a, b mgl64.Vec3, x float64) (mgl64.Vec3, bool) {
	if mgl64.FloatEqual(b[0], a[0]) {
		return mgl64.Vec3{}, false
	}

	f := (x - a[0]) / (b[0] - a[0])
	if f < 0 || f > 1 {
		return mgl64.Vec3{}, false
	}

	return mgl64.Vec3{x, a[1] + (b[1]-a[1])*f, a[2] + (b[2]-a[2])*f}, true
}

// vec3OnLineWithY returns an mgl64.Vec3 on the line between mgl64.Vec3 a and b with a Y value passed. If no such vec3
// could be found, the bool returned is false.
func vec3OnLineWithY(a, b mgl64.Vec3, y float64) *mgl64.Vec3 {
	// The pointer form is kept for callers that need one; the value form below
	// runs the same arithmetic and lets the ray trace skip the allocation.
	v, ok := lineVec3AtY(a, b, y)
	if !ok {
		return nil
	}
	return &v
}

// lineVec3AtY is vec3OnLineWithY returning the point by value, so that a
// point that stays inside the caller is not put on the heap.
func lineVec3AtY(a, b mgl64.Vec3, y float64) (mgl64.Vec3, bool) {
	if mgl64.FloatEqual(a[1], b[1]) {
		return mgl64.Vec3{}, false
	}

	f := (y - a[1]) / (b[1] - a[1])
	if f < 0 || f > 1 {
		return mgl64.Vec3{}, false
	}

	return mgl64.Vec3{a[0] + (b[0]-a[0])*f, y, a[2] + (b[2]-a[2])*f}, true
}

// vec3OnLineWithZ returns an mgl64.Vec3 on the line between mgl64.Vec3 a and b with a Z value passed. If no such vec3
// could be found, the bool returned is false.
func vec3OnLineWithZ(a, b mgl64.Vec3, z float64) *mgl64.Vec3 {
	// The pointer form is kept for callers that need one; the value form below
	// runs the same arithmetic and lets the ray trace skip the allocation.
	v, ok := lineVec3AtZ(a, b, z)
	if !ok {
		return nil
	}
	return &v
}

// lineVec3AtZ is vec3OnLineWithZ returning the point by value, so that a
// point that stays inside the caller is not put on the heap.
func lineVec3AtZ(a, b mgl64.Vec3, z float64) (mgl64.Vec3, bool) {
	if mgl64.FloatEqual(a[2], b[2]) {
		return mgl64.Vec3{}, false
	}

	f := (z - a[2]) / (b[2] - a[2])
	if f < 0 || f > 1 {
		return mgl64.Vec3{}, false
	}

	return mgl64.Vec3{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f, z}, true
}
