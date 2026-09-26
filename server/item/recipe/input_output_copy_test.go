package recipe

import (
	"reflect"
	"testing"

	"github.com/jv2w/Impulse/server/item"
)

// Regression for the Input/Output aliasing defect: both must hand out copies,
// so a caller that writes to one cannot edit the registered recipe.
func TestInputAndOutputReturnCopies(t *testing.T) {
	stick := item.NewStack(item.Stick{}, 1)
	diamond := item.NewStack(item.Diamond{}, 1)
	r := NewShapeless([]Item{stick}, diamond, "crafting_table")

	r.Input()[0] = diamond
	if !reflect.DeepEqual(r.Input()[0], Item(stick)) {
		t.Fatal("write through Input() leaked into the recipe")
	}
	r.Output()[0] = stick
	if !r.Output()[0].Equal(diamond) {
		t.Fatal("write through Output() leaked into the recipe")
	}
}
