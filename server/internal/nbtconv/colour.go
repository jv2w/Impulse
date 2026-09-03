package nbtconv

import (
	"image/color"
)

// Int32FromRGBA converts a color.RGBA into an int32. These int32s are present in, for example, signs.
func Int32FromRGBA(x color.RGBA) int32 {
	if x.R == 0 && x.G == 0 && x.B == 0 {
		// Default to black colour. The default (0x000000) is a transparent colour. Text with this colour will not show
		// up on the sign.
		return int32(-0x1000000)
	}
	// binary.BigEndian.Uint32 of {A, R, G, B} is exactly this shift, so the
	// four byte slice is no longer built.
	return int32(uint32(x.A)<<24 | uint32(x.R)<<16 | uint32(x.G)<<8 | uint32(x.B))
}

// RGBAFromInt32 converts an int32 into a color.RGBA. These int32s are present in, for example, signs.
func RGBAFromInt32(x int32) color.RGBA {
	// PutUint32 writes byte(v>>24), byte(v>>16), byte(v>>8), byte(v) in that
	// order, so reading the same shifts drops the four byte slice.
	u := uint32(x)
	return color.RGBA{A: byte(u >> 24), R: byte(u >> 16), G: byte(u >> 8), B: byte(u)}
}
