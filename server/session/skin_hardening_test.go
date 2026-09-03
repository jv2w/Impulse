package session

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// maxSkinAlloc is the amount protocolToSkin may allocate for a skin whose declared dimensions imply a pixel
// buffer of gigabytes. The conversion of a rejected skin does no real work, so anything approaching the
// implied buffer size means the dimensions were believed and the make already happened.
const maxSkinAlloc = 16 << 20

// marshalSkin encodes sk with the protocol writer. gophertunnel reports a skin that fails its own validation
// by panicking through Writer.InvalidValue, so the panic is turned into an error here.
func marshalSkin(sk protocol.Skin) (b []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	buf := &bytes.Buffer{}
	sk.Marshal(protocol.NewWriter(buf, 0))
	return buf.Bytes(), nil
}

// unmarshalSkin decodes a skin from b the way a Session reads packet.PlayerSkin off the wire, with the
// reader's limits enabled.
func unmarshalSkin(b []byte) (sk protocol.Skin, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	sk.Marshal(protocol.NewReader(bytes.NewBuffer(b), 0, true))
	return sk, nil
}

// TestProtocolToSkinRejectsWrappingDimensions covers the gap between the size gophertunnel validates and the
// size protocolToSkin then allocates. protocol.Skin.validate computes width*height*4 as a uint32 and compares
// it against uint32(len(data)), so any dimensions whose product is a multiple of 2^32 validate against empty
// pixel data. protocolToSkin widened those same dimensions to int and handed them to skin.New, skin.NewCape
// and skin.NewAnimation, each of which does make([]uint8, width*height*4) in int arithmetic: the product no
// longer wraps and the allocation is the full, unwrapped size.
//
// The dimensions below are reachable through packet.PlayerSkin, which a client may send at any time after
// login. The packet loop reading them has no recover, so both outcomes (a multi gigabyte allocation and the
// makeslice panic the larger dimensions produce) take the process down.
func TestProtocolToSkinRejectsWrappingDimensions(t *testing.T) {
	// 2^30 * 1 * 4 and 2^30 * 2^30 * 4 are both 0 mod 2^32. In int arithmetic the first asks for 4 GiB and
	// the second is past the largest slice Go will make at all.
	dimensions := []struct {
		name string
		w, h uint32
	}{
		{name: "4GiB", w: 1 << 30, h: 1},
		{name: "past max alloc", w: 1 << 30, h: 1 << 30},
	}
	targets := []struct {
		name  string
		apply func(sk *protocol.Skin, w, h uint32)
	}{
		{
			name: "skin",
			apply: func(sk *protocol.Skin, w, h uint32) {
				sk.SkinImageWidth, sk.SkinImageHeight, sk.SkinData = w, h, nil
			},
		},
		{
			name: "cape",
			apply: func(sk *protocol.Skin, w, h uint32) {
				sk.CapeImageWidth, sk.CapeImageHeight, sk.CapeData = w, h, nil
			},
		},
		{
			name: "animation",
			apply: func(sk *protocol.Skin, w, h uint32) {
				sk.Animations = []protocol.SkinAnimation{{
					ImageWidth:    w,
					ImageHeight:   h,
					AnimationType: protocol.SkinAnimationHead,
					FrameCount:    1,
				}}
			},
		},
	}

	for _, d := range dimensions {
		for _, target := range targets {
			t.Run(target.name+"/"+d.name, func(t *testing.T) {
				sk := validProtocolSkin()
				target.apply(&sk, d.w, d.h)

				// gophertunnel has to accept the skin for the defect to be reachable at all: if the
				// round trip below starts rejecting these dimensions, the check in protocolToSkin is
				// no longer load bearing and this test is proving nothing.
				b, err := marshalSkin(sk)
				if err != nil {
					t.Fatalf("gophertunnel rejected the skin when writing it: %v", err)
				}
				got, err := unmarshalSkin(b)
				if err != nil {
					t.Fatalf("gophertunnel rejected the skin when reading it: %v", err)
				}
				want := skinDimensions(sk, target.name)
				if gotDim := skinDimensions(got, target.name); gotDim != want {
					t.Fatalf("round tripped %s dimensions = %v, want %v", target.name, gotDim, want)
				}

				var convErr error
				allocated := allocatedBySkin(func() {
					_, convErr = protocolToSkin(got)
				})
				if convErr == nil {
					t.Fatalf("protocolToSkin accepted %s dimensions of %dx%d against empty pixel data", target.name, d.w, d.h)
				}
				if allocated > maxSkinAlloc {
					t.Errorf("protocolToSkin allocated %d bytes, want at most %d: the dimensions were believed before they were checked", allocated, maxSkinAlloc)
				}
			})
		}
	}
}

// skinDimensions returns the declared width and height of the named part of sk, so the round trip can be
// checked to have preserved exactly the values the conversion later reads.
func skinDimensions(sk protocol.Skin, target string) [2]uint32 {
	switch target {
	case "skin":
		return [2]uint32{sk.SkinImageWidth, sk.SkinImageHeight}
	case "cape":
		return [2]uint32{sk.CapeImageWidth, sk.CapeImageHeight}
	default:
		if len(sk.Animations) != 1 {
			return [2]uint32{}
		}
		return [2]uint32{sk.Animations[0].ImageWidth, sk.Animations[0].ImageHeight}
	}
}

// allocatedBySkin reports the number of bytes allocated by f. TotalAlloc is cumulative and unaffected by
// collection, so a buffer that is made and then immediately dropped still shows up here.
func allocatedBySkin(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestProtocolToSkinAcceptsConsistentDimensions is the counterpart: a skin, cape and animation whose pixel
// data matches the declared dimensions still convert, and the converted values are unchanged. Pix is the
// client's slice itself, not a copy, on all three.
func TestProtocolToSkinAcceptsConsistentDimensions(t *testing.T) {
	sk := validProtocolSkin()
	sk.CapeImageWidth, sk.CapeImageHeight = 64, 32
	sk.CapeData = make([]byte, 64*32*4)
	sk.Animations = []protocol.SkinAnimation{{
		ImageWidth:    8,
		ImageHeight:   8,
		ImageData:     make([]byte, 8*8*4),
		AnimationType: protocol.SkinAnimationHead,
		FrameCount:    1,
	}}

	got, err := protocolToSkin(sk)
	if err != nil {
		t.Fatalf("protocolToSkin returned %v, want no error", err)
	}
	if got.Bounds().Max.X != 64 || got.Bounds().Max.Y != 32 {
		t.Errorf("skin bounds = %v, want 64x32", got.Bounds())
	}
	if &got.Pix[0] != &sk.SkinData[0] {
		t.Error("skin Pix is a copy, want the client's SkinData slice itself")
	}
	if got.Cape.Bounds().Max.X != 64 || got.Cape.Bounds().Max.Y != 32 {
		t.Errorf("cape bounds = %v, want 64x32", got.Cape.Bounds())
	}
	if &got.Cape.Pix[0] != &sk.CapeData[0] {
		t.Error("cape Pix is a copy, want the client's CapeData slice itself")
	}
	if len(got.Animations) != 1 {
		t.Fatalf("animations = %d, want 1", len(got.Animations))
	}
	anim := got.Animations[0]
	if anim.Bounds().Max.X != 8 || anim.Bounds().Max.Y != 8 {
		t.Errorf("animation bounds = %v, want 8x8", anim.Bounds())
	}
	if anim.Type() != 0 {
		t.Errorf("animation type = %v, want AnimationHead", anim.Type())
	}
	if anim.FrameCount != 1 {
		t.Errorf("animation frame count = %d, want 1", anim.FrameCount)
	}
	if &anim.Pix[0] != &sk.Animations[0].ImageData[0] {
		t.Error("animation Pix is a copy, want the client's ImageData slice itself")
	}
}

// TestProtocolToSkinZeroDimensions pins the empty skin, cape and animation: a zero sized part with no pixel
// data is consistent and must keep converting. An absent cape is exactly this, and every skin has one.
func TestProtocolToSkinZeroDimensions(t *testing.T) {
	sk := validProtocolSkin()
	sk.SkinImageWidth, sk.SkinImageHeight, sk.SkinData = 0, 0, nil
	sk.Animations = []protocol.SkinAnimation{{AnimationType: protocol.SkinAnimationBody32x32}}

	got, err := protocolToSkin(sk)
	if err != nil {
		t.Fatalf("protocolToSkin returned %v, want no error", err)
	}
	if got.Bounds().Max.X != 0 || got.Bounds().Max.Y != 0 {
		t.Errorf("skin bounds = %v, want 0x0", got.Bounds())
	}
	if got.Cape.Bounds().Max.X != 0 || got.Cape.Bounds().Max.Y != 0 {
		t.Errorf("cape bounds = %v, want 0x0", got.Cape.Bounds())
	}
	if len(got.Animations) != 1 {
		t.Fatalf("animations = %d, want 1", len(got.Animations))
	}
	if got.Animations[0].Bounds().Max.X != 0 {
		t.Errorf("animation bounds = %v, want 0x0", got.Animations[0].Bounds())
	}
}
