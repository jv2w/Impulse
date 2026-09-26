package session

import (
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/player/skin"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type skinControllable struct {
	Controllable
	set int
}

func (c *skinControllable) SetSkin(skin.Skin) { c.set++ }

// TestPlayerSkinRejectsTooManyAnimations pins the animation bound: dozens of
// animations, each trivially small, must be rejected before any model is
// built. Before the fix the slice was ranged unchecked.
func TestPlayerSkinRejectsTooManyAnimations(t *testing.T) {
	c := &skinControllable{}
	h := PlayerSkinHandler{}
	anims := make([]protocol.SkinAnimation, 64)
	for i := range anims {
		anims[i] = protocol.SkinAnimation{AnimationType: protocol.SkinAnimationHead}
	}
	pk := &packet.PlayerSkin{Skin: protocol.Skin{
		SkinID:            "test",
		SkinImageWidth:    64,
		SkinImageHeight:   64,
		SkinData:          make([]byte, 64*64*4),
		SkinGeometry:      []byte(`{}`),
		SkinResourcePatch: []byte(`{"geometry":{"default":"x"}}`),
		Animations:        anims,
	}}
	if err := h.Handle(pk, nil, nil, c); err == nil {
		t.Fatalf("PlayerSkin with %d animations succeeded, want rejection", len(anims))
	}
	if c.set != 0 {
		t.Fatalf("oversize skin applied %d times, want 0", c.set)
	}
}

// TestPlayerSkinRejectsOversizeResourcePatch pins the resource-patch bound:
// a patch far beyond legitimate models must be rejected before JSON decoding.
func TestPlayerSkinRejectsOversizeResourcePatch(t *testing.T) {
	c := &skinControllable{}
	h := PlayerSkinHandler{}
	pk := &packet.PlayerSkin{Skin: protocol.Skin{
		SkinID:            "test",
		SkinImageWidth:    64,
		SkinImageHeight:   64,
		SkinData:          make([]byte, 64*64*4),
		SkinGeometry:      []byte(`{}`),
		SkinResourcePatch: []byte("{" + strings.Repeat(`"a":1,`, 32768) + `"z":1}`),
	}}
	if err := h.Handle(pk, nil, nil, c); err == nil {
		t.Fatal("PlayerSkin with 256KB resource patch succeeded, want rejection")
	}
	if c.set != 0 {
		t.Fatalf("oversize skin applied %d times, want 0", c.set)
	}
}

// TestPlayerSkinRejectsOversizeGeometry pins the geometry bound: a geometry
// blob far beyond legitimate models must be rejected before JSON decoding.
func TestPlayerSkinRejectsOversizeGeometry(t *testing.T) {
	c := &skinControllable{}
	h := PlayerSkinHandler{}
	pk := &packet.PlayerSkin{Skin: protocol.Skin{
		SkinID:            "test",
		SkinImageWidth:    64,
		SkinImageHeight:   64,
		SkinData:          make([]byte, 64*64*4),
		SkinGeometry:      []byte("{" + strings.Repeat(`"a":1,`, 32768) + `"z":1}`),
		SkinResourcePatch: []byte(`{"geometry":{"default":"x"}}`),
	}}
	if err := h.Handle(pk, nil, nil, c); err == nil {
		t.Fatal("PlayerSkin with 256KB geometry succeeded, want rejection")
	}
	if c.set != 0 {
		t.Fatalf("oversize skin applied %d times, want 0", c.set)
	}
}

// TestPlayerSkinRejectsOversizeDimensions pins the dimension bound: pixel
// dimensions far beyond legitimate skins must be rejected before any pixel
// buffer is built, even when the pixel data matches.
func TestPlayerSkinRejectsOversizeDimensions(t *testing.T) {
	c := &skinControllable{}
	h := PlayerSkinHandler{}
	pk := &packet.PlayerSkin{Skin: protocol.Skin{
		SkinID:            "test",
		SkinImageWidth:    512,
		SkinImageHeight:   512,
		SkinData:          make([]byte, 512*512*4),
		SkinGeometry:      []byte(`{}`),
		SkinResourcePatch: []byte(`{"geometry":{"default":"x"}}`),
	}}
	if err := h.Handle(pk, nil, nil, c); err == nil {
		t.Fatal("PlayerSkin with 512x512 dimensions succeeded, want rejection")
	}
	if c.set != 0 {
		t.Fatalf("oversize skin applied %d times, want 0", c.set)
	}
}
