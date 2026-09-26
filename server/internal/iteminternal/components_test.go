package iteminternal

import (
	"image"
	"testing"

	"github.com/jv2w/Impulse/server/item/category"
)

// testWeapon is a custom weapon: the damage property must be sent so the
// client applies the right attack damage instead of a default.
type testWeapon struct{}

func (testWeapon) EncodeItem() (string, int16) { return "impulse:test_weapon", 0 }
func (testWeapon) Name() string                { return "Test Weapon" }
func (testWeapon) Texture() image.Image        { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
func (testWeapon) Category() category.Category { return category.Equipment() }
func (testWeapon) AttackDamage() float64       { return 7 }

// TestCustomWeaponDamageComponent pins the attack damage component: a custom
// weapon's components must carry its damage as an int32 item property.
func TestCustomWeaponDamageComponent(t *testing.T) {
	components, ok := Components(testWeapon{})["components"].(map[string]any)
	if !ok {
		t.Fatal("components map has no \"components\" entry")
	}
	props, ok := components["item_properties"].(map[string]any)
	if !ok {
		t.Fatal("components map has no item_properties entry")
	}
	if got, want := props["damage"], int32(7); got != want {
		t.Fatalf("damage = %#v, want %#v", got, want)
	}
}
