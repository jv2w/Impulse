package sliceutil

import (
	"fmt"
	"slices"
	"testing"
)

// Four generic helpers, no state, no concurrency. What matters about them is
// what the callers cannot see from the signature: whether the returned slice
// shares a backing array with the input, whether the input is written to, and
// what happens at the edges. Three of the four allocate; DeleteVal does not,
// and that difference is the whole risk surface of this package.

// named exercises the ~[]B and ~[]A constraints on Convert and SearchValue.
// Filter and DeleteVal take a plain []E instead, so their element type is
// inferred but their parameter is not constrained to a named slice.
type named []int

type marker interface{ mark() }

type impl struct{ n int }

func (impl) mark() {}

type other struct{}

// --- Convert ---

func TestConvertToInterface(t *testing.T) {
	// The shape cmd/target.go:29 uses: a slice of concrete values widened to a
	// slice of an interface they all satisfy.
	in := []impl{{1}, {2}, {3}}
	got := Convert[marker](in)
	if len(got) != len(in) {
		t.Fatalf("Convert returned %d elements, want %d", len(got), len(in))
	}
	for i, g := range got {
		if g != marker(in[i]) {
			t.Errorf("Convert[%d] = %v, want %v", i, g, in[i])
		}
	}
}

func TestConvertToSameType(t *testing.T) {
	in := []int{1, 2, 3}
	if got := Convert[int](in); !slices.Equal(got, in) {
		t.Fatalf("Convert[int] = %v, want %v", got, in)
	}
}

func TestConvertAcceptsANamedSliceType(t *testing.T) {
	// S ~[]B, so a defined slice type is accepted without a conversion at the
	// call site. The result is always a plain []A, never the named type.
	if got := Convert[any](named{1, 2}); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("Convert[any](named{1, 2}) = %v, want [1 2]", got)
	}
}

// TestConvertReturnsAFreshSlice answers the aliasing question for Convert: it
// allocates with make and never writes to the input, so writing into either
// side leaves the other alone. This holds even when A and B are the same type.
func TestConvertReturnsAFreshSlice(t *testing.T) {
	in := []int{1, 2, 3}
	got := Convert[any](in)
	got[0] = 99
	if in[0] != 1 {
		t.Errorf("writing to the result changed the input: in[0] = %v, want 1", in[0])
	}
	in[1] = 88
	if got[1] != 2 {
		t.Errorf("writing to the input changed the result: got[1] = %v, want 2", got[1])
	}

	same := Convert[int](in)
	if len(same) > 0 && &same[0] == &in[0] {
		t.Error("Convert[int] shares a backing array with its input")
	}
}

// TestConvertNilAndEmptyBecomeNonNilEmpty pins that a nil input does not come
// back as nil. make([]A, 0) is a non-nil empty slice, so a caller that tests
// the result against nil sees a different answer than it put in.
func TestConvertNilAndEmptyBecomeNonNilEmpty(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []int
	}{
		{"nil", nil},
		{"empty", []int{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Convert[any](c.in)
			if got == nil {
				t.Fatal("Convert returned nil, want a non-nil empty slice")
			}
			if len(got) != 0 {
				t.Fatalf("Convert returned %v, want an empty slice", got)
			}
		})
	}
}

// PINNED BUG: Convert takes the process down on an element that does not
// satisfy A. The doc comment says so, which does not make it a value: cmd's
// target.go:29 hands it whatever entities a transaction holds, so a third-party
// entity type that does not implement cmd.Target panics inside the world
// goroutine. A lookup that can fail should return (T, bool) or (T, error).
// Pinned as-is; decide in stage 1.
func TestConvertPanicsOnAFailedAssertion(t *testing.T) {
	for _, c := range []struct {
		name string
		call func()
		want string
	}{
		{
			"wrong type",
			func() { _ = Convert[marker]([]any{impl{1}, other{}}) },
			"interface conversion: sliceutil.other is not sliceutil.marker: missing method mark",
		},
		{
			"nil element",
			func() { _ = Convert[marker]([]any{nil}) },
			"interface conversion: interface is nil, not sliceutil.marker",
		},
		{
			"concrete mismatch",
			func() { _ = Convert[string]([]any{"a", 1}) },
			"interface conversion: interface {} is int, not string",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("Convert did not panic")
				}
				if got := fmt.Sprint(r); got != c.want {
					t.Fatalf("panic value = %q, want %q", got, c.want)
				}
			}()
			c.call()
		})
	}
}

// TestConvertPanicsAfterPartialWork pins that the failure is not atomic: the
// elements before the bad one have already been written into the new slice,
// which is then discarded. Nothing observable leaks, because the slice is
// Convert's own allocation, but a caller cannot recover a partial result.
func TestConvertPanicsAfterPartialWork(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Convert did not panic")
		}
	}()
	_ = Convert[marker]([]any{impl{1}, impl{2}, other{}})
}

// --- SearchValue ---

func TestSearchValue(t *testing.T) {
	for _, c := range []struct {
		name  string
		in    []int
		f     func(int) bool
		want  int
		found bool
	}{
		{"first match wins", []int{3, 5, 7, 5}, func(a int) bool { return a == 5 }, 5, true},
		{"single element", []int{9}, func(a int) bool { return a == 9 }, 9, true},
		{"no match", []int{1, 2}, func(a int) bool { return a == 3 }, 0, false},
		{"empty", []int{}, func(int) bool { return true }, 0, false},
		{"nil", nil, func(int) bool { return true }, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := SearchValue(c.in, c.f)
			if ok != c.found {
				t.Fatalf("SearchValue ok = %v, want %v", ok, c.found)
			}
			if got != c.want {
				t.Fatalf("SearchValue = %v, want %v (the zero value when not found)", got, c.want)
			}
		})
	}
}

func TestSearchValueStopsAtTheFirstMatch(t *testing.T) {
	calls := 0
	got, ok := SearchValue([]int{1, 2, 3, 4}, func(a int) bool {
		calls++
		return a == 2
	})
	if !ok || got != 2 {
		t.Fatalf("SearchValue = %v, %v, want 2, true", got, ok)
	}
	if calls != 2 {
		t.Fatalf("the predicate ran %d times, want 2", calls)
	}
}

func TestSearchValueAcceptsANamedSliceType(t *testing.T) {
	if got, ok := SearchValue(named{4, 5}, func(a int) bool { return a == 5 }); !ok || got != 5 {
		t.Fatalf("SearchValue(named{4, 5}) = %v, %v, want 5, true", got, ok)
	}
}

// PINNED CONSTRAINT: a nil predicate is only safe on an empty slice, because
// the loop body is what dereferences it. There is no guard.
func TestSearchValueWithANilPredicate(t *testing.T) {
	if _, ok := SearchValue[int, []int](nil, nil); ok {
		t.Fatal("SearchValue over a nil slice reported a match")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a nil predicate over a non-empty slice did not panic")
		}
	}()
	_, _ = SearchValue([]int{1}, nil)
}

// TestSearchValueReturnsACopy pins that the element comes back by value. For a
// struct element the caller gets a copy and cannot write through it into the
// slice.
func TestSearchValueReturnsACopy(t *testing.T) {
	in := []impl{{1}, {2}}
	got, ok := SearchValue(in, func(a impl) bool { return a.n == 2 })
	if !ok {
		t.Fatal("SearchValue found nothing")
	}
	got.n = 99
	if in[1].n != 2 {
		t.Errorf("writing to the returned value changed the slice: in[1].n = %d, want 2", in[1].n)
	}
}

// --- Filter ---

func TestFilter(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []int
		f    func(int) bool
		want []int
	}{
		{"keeps order", []int{1, 2, 3, 4, 5}, func(a int) bool { return a%2 == 1 }, []int{1, 3, 5}},
		{"all match", []int{1, 2}, func(int) bool { return true }, []int{1, 2}},
		{"none match", []int{1, 2}, func(int) bool { return false }, []int{}},
		{"single element", []int{7}, func(a int) bool { return a == 7 }, []int{7}},
		{"empty", []int{}, func(int) bool { return true }, []int{}},
		{"nil", nil, func(int) bool { return true }, []int{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Filter(c.in, c.f)
			if !slices.Equal(got, c.want) {
				t.Fatalf("Filter = %v, want %v", got, c.want)
			}
			// make([]E, 0, len(s)) is never nil, so a filtered-away everything
			// still comes back non-nil.
			if got == nil {
				t.Fatal("Filter returned nil, want a non-nil empty slice")
			}
			if cap(got) != len(c.in) {
				t.Fatalf("Filter result has capacity %d, want %d (len of the input)", cap(got), len(c.in))
			}
		})
	}
}

// TestFilterNeverAliasesItsInput pins the counterpart to DeleteVal: Filter
// allocates, so the input is neither written to nor shared, even when every
// element is kept.
func TestFilterNeverAliasesItsInput(t *testing.T) {
	in := []int{1, 2, 3}
	got := Filter(in, func(int) bool { return true })
	if &got[0] == &in[0] {
		t.Fatal("Filter shares a backing array with its input")
	}
	got[0] = 99
	if in[0] != 1 {
		t.Errorf("writing to the result changed the input: in[0] = %d, want 1", in[0])
	}
	if !slices.Equal(in, []int{1, 2, 3}) {
		t.Errorf("Filter modified its input: %v, want [1 2 3]", in)
	}
}

func TestFilterCallsThePredicateOncePerElementInOrder(t *testing.T) {
	var seen []int
	Filter([]int{3, 1, 2}, func(a int) bool {
		seen = append(seen, a)
		return false
	})
	if !slices.Equal(seen, []int{3, 1, 2}) {
		t.Fatalf("the predicate saw %v, want [3 1 2]", seen)
	}
}

// --- DeleteVal ---

// PINNED BUG: DeleteVal is the one function here that does not allocate. It
// calls slices.Delete, which shifts the tail down inside the caller's array and
// returns a slice over that same array. So the input is mutated, the result
// aliases it, and any other slice header over the same array -- a copy of the
// header, a subslice, a value captured before the call -- silently observes
// shifted elements. The three callers (world/tick.go:234, world/world.go:879,
// session/session_list.go:43) assign the result straight back over the input
// and hold no second header, so none is broken today; nothing in the signature
// or the doc comment says the next caller may not.
// Pinned as-is; decide in stage 1.
func TestDeleteValAliasesAndMutatesItsInput(t *testing.T) {
	in := []int{1, 2, 3, 4}
	got := DeleteVal(in, 2)
	if !slices.Equal(got, []int{1, 3, 4}) {
		t.Fatalf("DeleteVal = %v, want [1 3 4]", got)
	}
	if &got[0] != &in[0] {
		t.Fatal("DeleteVal returned a fresh slice; it used to alias its input")
	}
	if cap(got) != cap(in) {
		t.Errorf("result capacity %d, want the input's %d", cap(got), cap(in))
	}
	// The caller's own slice header still has length 4 and now reads the
	// shifted tail, with the vacated slot zeroed by slices.Delete.
	if !slices.Equal(in, []int{1, 3, 4, 0}) {
		t.Fatalf("the input slice is now %v, want [1 3 4 0]", in)
	}
}

func TestDeleteValDeletesOnlyTheFirstOccurrence(t *testing.T) {
	got := DeleteVal([]int{5, 1, 5, 2, 5}, 5)
	if !slices.Equal(got, []int{1, 5, 2, 5}) {
		t.Fatalf("DeleteVal = %v, want [1 5 2 5]", got)
	}
}

// TestDeleteValOnAMissReturnsTheInputItself pins that a value that is not
// present costs nothing and changes nothing: the very same slice header comes
// back, so a caller cannot tell from the result whether anything was removed.
func TestDeleteValOnAMissReturnsTheInputItself(t *testing.T) {
	in := []int{1, 2, 3}
	got := DeleteVal(in, 9)
	if len(got) != len(in) || &got[0] != &in[0] {
		t.Fatalf("DeleteVal on a miss returned %v, want the input slice itself", got)
	}
	if !slices.Equal(in, []int{1, 2, 3}) {
		t.Fatalf("DeleteVal on a miss modified the input: %v", in)
	}
}

func TestDeleteValNilAndEmpty(t *testing.T) {
	if got := DeleteVal([]int(nil), 1); got != nil {
		t.Errorf("DeleteVal(nil, 1) = %v, want nil", got)
	}
	if got := DeleteVal([]int{}, 1); len(got) != 0 {
		t.Errorf("DeleteVal([], 1) = %v, want an empty slice", got)
	}
	if got := DeleteVal([]int{7}, 7); len(got) != 0 {
		t.Errorf("DeleteVal([7], 7) = %v, want an empty slice", got)
	}
}

// TestDeleteValComparesThroughAny pins the comparison DeleteVal performs. It
// boxes both sides into any rather than constraining E to comparable, so the
// match is on dynamic type as well as value: an interface element only matches
// a value of the same concrete type.
func TestDeleteValComparesThroughAny(t *testing.T) {
	in := []marker{impl{1}, impl{2}}
	if got := DeleteVal(in, marker(impl{2})); len(got) != 1 || got[0] != marker(impl{1}) {
		t.Fatalf("DeleteVal = %v, want [{1}]", got)
	}
	nums := []any{int32(1), int64(1)}
	if got := DeleteVal(nums, any(int64(1))); len(got) != 1 || got[0] != any(int32(1)) {
		t.Fatalf("DeleteVal matched across integer widths: %v, want [1] as an int32", got)
	}
}

// PINNED BUG: E is constrained to any rather than comparable, so an element
// type that cannot be compared turns what should be a compile error into a
// runtime panic inside whatever goroutine called it. The `(any)(v) == (any)(vs)`
// boxing is what defers the check to run time; a comparable constraint would
// let the compiler reject the call. Pinned as-is; decide in stage 1.
func TestDeleteValPanicsOnUncomparableElements(t *testing.T) {
	for _, c := range []struct {
		name string
		call func()
		want string
	}{
		{"slice elements", func() { _ = DeleteVal([][]int{{1}}, []int{1}) }, "runtime error: comparing uncomparable type []int"},
		{"map elements", func() { _ = DeleteVal([]map[string]int{{}}, map[string]int{}) }, "runtime error: comparing uncomparable type map[string]int"},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("DeleteVal did not panic")
				}
				if got := fmt.Sprint(r); got != c.want {
					t.Fatalf("panic value = %q, want %q", got, c.want)
				}
			}()
			c.call()
		})
	}
}
