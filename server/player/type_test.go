package player

import (
	"math"
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
)

// boxEpsilon is the absolute tolerance for a bounding box coordinate. Every
// golden below is a product of two exactly representable binary fractions
// except the 1.49 and 1.8 sneaking and standing heights, whose scaled products
// carry one rounding step; 1e-9 covers that and nothing larger.
const boxEpsilon = 1e-9

func boxCloseTo(got cube.BBox, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	want := [6]float64{minX, minY, minZ, maxX, maxY, maxZ}
	have := [6]float64{got.Min()[0], got.Min()[1], got.Min()[2], got.Max()[0], got.Max()[1], got.Max()[2]}
	for i := range want {
		if math.Abs(have[i]-want[i]) > boxEpsilon {
			return false
		}
	}
	return true
}

// The player's bounding box is chosen by a fixed precedence over five state
// flags and then scaled. Sleeping wins over everything, the three prone states
// share a box and beat sneaking, and sneaking beats standing.
func TestTypeBBoxGolden(t *testing.T) {
	for _, tc := range []struct {
		name              string
		set               func(*playerData)
		scale             float64
		halfWidth, height float64
	}{
		{name: "standing", set: func(*playerData) {}, scale: 1, halfWidth: 0.3, height: 1.8},
		{name: "sneaking", set: func(d *playerData) { d.sneaking = true }, scale: 1, halfWidth: 0.3, height: 1.49},
		{name: "swimming", set: func(d *playerData) { d.swimming = true }, scale: 1, halfWidth: 0.3, height: 0.6},
		{name: "gliding", set: func(d *playerData) { d.gliding = true }, scale: 1, halfWidth: 0.3, height: 0.6},
		{name: "crawling", set: func(d *playerData) { d.crawling = true }, scale: 1, halfWidth: 0.3, height: 0.6},
		{name: "sleeping", set: func(d *playerData) { d.sleeping = true }, scale: 1, halfWidth: 0.1, height: 0.2},
		{
			name: "sleeping beats every other flag",
			set: func(d *playerData) {
				d.sleeping, d.sneaking, d.swimming, d.gliding, d.crawling = true, true, true, true, true
			},
			scale: 1, halfWidth: 0.1, height: 0.2,
		},
		{
			name:  "swimming beats sneaking",
			set:   func(d *playerData) { d.swimming, d.sneaking = true, true },
			scale: 1, halfWidth: 0.3, height: 0.6,
		},
		{name: "standing at half scale", set: func(*playerData) {}, scale: 0.5, halfWidth: 0.15, height: 0.9},
		{name: "sneaking at double scale", set: func(d *playerData) { d.sneaking = true }, scale: 2, halfWidth: 0.6, height: 2.98},
		{name: "sleeping at double scale", set: func(d *playerData) { d.sleeping = true }, scale: 2, halfWidth: 0.2, height: 0.4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStubPlayer(Config{})
			p.scale = tc.scale
			tc.set(p.playerData)

			got := Type.BBox(p)

			if !boxCloseTo(got, -tc.halfWidth, 0, -tc.halfWidth, tc.halfWidth, tc.height, tc.halfWidth) {
				t.Fatalf("BBox = %v..%v, want %v..%v height %v", got.Min(), got.Max(), -tc.halfWidth, tc.halfWidth, tc.height)
			}
		})
	}
}

// PINNED BUG: every branch of BBox multiplies by the scale with no floor, so a
// player scaled to 0 collapses to a point and one scaled negative gets a box
// mirrored below its feet, which cube.Box then normalises into a legal box the
// player's head hangs out of. Player.SetScale accepts both.
func TestBBoxDegeneratesOnANonPositiveScale(t *testing.T) {
	p := newStubPlayer(Config{})

	p.scale = 0
	if got := Type.BBox(p); !boxCloseTo(got, 0, 0, 0, 0, 0, 0) {
		t.Fatalf("BBox at scale 0 = %v..%v, want a point at the origin", got.Min(), got.Max())
	}

	p.scale = -1
	if got := Type.BBox(p); !boxCloseTo(got, -0.3, -1.8, -0.3, 0.3, 0, 0.3) {
		t.Fatalf("BBox at scale -1 = %v..%v, want the box normalised below the feet", got.Min(), got.Max())
	}
}

// PINNED BUG: BBox asserts its argument to *Player with no comma-ok, so an
// EntityType handed the wrong entity takes the world goroutine down.
// world.EntityType.BBox has no error return to use instead, so this cannot be
// fixed inside the method.
func TestBBoxPanicsOnAForeignEntity(t *testing.T) {
	mustPanic(t, "Type.BBox(nil)", func() { Type.BBox(nil) })
}

func TestTypeConstants(t *testing.T) {
	if got := Type.EncodeEntity(); got != "minecraft:player" {
		t.Fatalf("EncodeEntity() = %v, want minecraft:player", got)
	}
	if got := Type.NetworkOffset(); got != 1.621 {
		t.Fatalf("NetworkOffset() = %v, want 1.621", got)
	}
}

// PINNED SCOPE: a player carries nothing through the entity NBT path. Both
// methods are silent no-ops because player persistence is playerdb's job, not
// the entity type's.
func TestTypeNBTIsANoOp(t *testing.T) {
	data := &world.EntityData{Name: "steve"}
	Type.DecodeNBT(map[string]any{"Name": "not steve"}, data)
	if data.Name != "steve" {
		t.Fatalf("DecodeNBT changed the data: Name = %v", data.Name)
	}
	if got := Type.EncodeNBT(data); got != nil {
		t.Fatalf("EncodeNBT = %v, want nil", got)
	}
}

// PINNED BUG: Open asserts EntityData.Data to *playerData with no comma-ok, so
// opening a handle whose data belongs to another entity type panics rather than
// reporting the mismatch.
func TestOpenPanicsOnForeignEntityData(t *testing.T) {
	mustPanic(t, "Type.Open with foreign data", func() {
		Type.Open(&world.Tx{}, nil, &world.EntityData{Data: "not a player"})
	})
}

// Open is where the off-hand slot validator is installed. Only the four items
// that implement item.OffHand may be placed there; everything else is refused,
// and Inventory.SetItem reports that refusal as a nil error with nothing
// written.
func TestOpenInstallsTheOffHandValidator(t *testing.T) {
	p := newOpenedPlayer(Config{})

	if err := p.offHand.SetItem(0, apples(1)); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}
	if got := p.offHand.Slots()[0]; !got.Empty() {
		t.Fatalf("off-hand = %v, want empty: an apple passed the validator", got)
	}
}

// PINNED CONSTRAINT: any write the validator accepts runs the slot func Open
// installed, which broadcasts to the player's viewers and needs a world. That
// covers an item.OffHand item and, because the validator always accepts an
// empty stack and Inventory.setItem runs the callback whether or not the slot
// changed, an empty write over an already-empty slot as well. Only refused
// writes are observable on this fixture, so the accepting half of the validator
// is pinned by the panic rather than by the stored stack.
func TestAnAcceptedOffHandWriteBroadcastsThroughTheWorld(t *testing.T) {
	p := newOpenedPlayer(Config{})
	mustPanic(t, "offHand.SetItem with an off-hand item", func() {
		_ = p.offHand.SetItem(0, item.NewStack(item.Arrow{}, 1))
	})

	q := newOpenedPlayer(Config{})
	mustPanic(t, "offHand.SetItem clearing an empty slot", func() {
		_ = q.offHand.SetItem(0, item.Stack{})
	})
}

// PINNED CONSTRAINT: newStubPlayer does not run Open, so its off-hand keeps
// inventory.New's permissive validator. Every off-hand test in this package
// that stores an apple depends on that difference, including the two
// consumption tests in player_test.go.
func TestTheStubOffHandHasNoValidator(t *testing.T) {
	p := newStubPlayer(Config{})

	if err := p.offHand.SetItem(0, apples(1)); err != nil {
		t.Fatalf("offHand.SetItem: %v", err)
	}
	if got := p.offHand.Slots()[0].Count(); got != 1 {
		t.Fatalf("off-hand count = %v, want 1", got)
	}
}
