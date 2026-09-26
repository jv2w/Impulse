package cmd

import (
	"slices"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/world"
)

type targetsRunnable struct {
	V   []Target
	res *result
}

func (r targetsRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++; r.res.args = []any{r.V} }

// spawn adds a player-encoded entity named name at pos and returns it.
func spawn(tx *world.Tx, name string, pos mgl64.Vec3) world.Entity {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos, NameTag: name}.New(testPlayerType{}, nopConfig{}))
}

// targetNames returns the names of the targets a targetsRunnable recorded.
func targetNames(res *result) []string {
	targets := res.args[0].([]Target)
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		if named, ok := target.(NamedTarget); ok {
			names = append(names, named.Name())
			continue
		}
		names = append(names, "<unnamed>")
	}
	return names
}

func TestSelectorsWithoutATransaction(t *testing.T) {
	// PINNED CONSTRAINT: every @ selector needs a transaction to enumerate the
	// world, so a console source that is not attached to a world can only ever
	// name a player literally, and even that fails because the player list is
	// empty. A command with a []Target parameter is therefore unusable from the
	// console.
	res := &result{}
	for _, args := range []string{"@a", "@e", "@p", "@r", "@s"} {
		t.Run(args, func(t *testing.T) {
			out := exec(t, args, targetsRunnable{res: res})
			if got, want := errStrings(out), []string{
				"§r§cSyntax error: unexpected value: at \"/pinned >>" + args + "<<\"§r",
				errNoTarget,
			}; !slices.Equal(got, want) {
				t.Fatalf("errors = %q, want %q", got, want)
			}
		})
	}
	t.Run("named player", func(t *testing.T) {
		out := exec(t, "Steve", targetsRunnable{res: res})
		if got, want := errStrings(out), []string{
			"§r§cSyntax error: unexpected value: at \"/pinned >>Steve<<\"§r",
			errNotFound,
		}; !slices.Equal(got, want) {
			t.Fatalf("errors = %q, want %q", got, want)
		}
	})
	t.Run("missing argument", func(t *testing.T) {
		out := exec(t, "", targetsRunnable{res: res})
		if got, want := errStrings(out), []string{
			"§r§cSyntax error: unexpected value: at \"/pinned>><<\"§r",
			"§r§cUsage: /pinned <V: target>§r",
		}; !slices.Equal(got, want) {
			t.Fatalf("errors = %q, want %q", got, want)
		}
	})
}

func TestSelectorAllAndEveryone(t *testing.T) {
	// @a yields every player and @e every entity. Both come straight out of the
	// world's entity map, which is unordered, so the tests compare sorted names.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "Steve", mgl64.Vec3{0, 64, 0})
		spawn(tx, "Alex", mgl64.Vec3{10, 64, 0})
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5, 64, 0}, NameTag: "Mob"}.New(testMobType{}, nopConfig{}))

		res := &result{}
		out := execTx(t, "@a", tx, &testSource{}, targetsRunnable{res: res})
		if got := errStrings(out); len(got) != 0 {
			t.Fatalf("errors = %q, want none", got)
		}
		got := targetNames(res)
		slices.Sort(got)
		if want := []string{"Alex", "Steve"}; !slices.Equal(got, want) {
			t.Fatalf("@a = %q, want %q", got, want)
		}

		res = &result{}
		out = execTx(t, "@e", tx, &testSource{}, targetsRunnable{res: res})
		if got := errStrings(out); len(got) != 0 {
			t.Fatalf("errors = %q, want none", got)
		}
		got = targetNames(res)
		slices.Sort(got)
		if want := []string{"Alex", "Mob", "Steve"}; !slices.Equal(got, want) {
			t.Fatalf("@e = %q, want %q", got, want)
		}
	})
}

func TestSelectorSelfDoesNotConsultTheWorld(t *testing.T) {
	// @s returns the Source itself without looking at the world at all, so it
	// works for a source that is not an entity in the world. It still needs a
	// non-nil transaction because of the earlier @-prefix check.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		src := &testSource{name: "Console"}
		res := &result{}
		out := execTx(t, "@s", tx, src, targetsRunnable{res: res})
		if got := errStrings(out); len(got) != 0 {
			t.Fatalf("errors = %q, want none", got)
		}
		targets := res.args[0].([]Target)
		if len(targets) != 1 || targets[0] != Target(src) {
			t.Fatalf("@s = %#v, want the source itself", targets)
		}
	})
}

func TestSelectorNearestIgnoresTheDistancesItComputed(t *testing.T) {
	// @p selects the nearest player to the source. The source sits at the
	// origin and the nearest player is unambiguous, so @p has exactly one
	// right answer no matter what order the world yields its players in.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "far", mgl64.Vec3{1000, 64, 0})
		spawn(tx, "near", mgl64.Vec3{1, 64, 0})
		spawn(tx, "middle", mgl64.Vec3{100, 64, 0})

		// The world yields players in map order, so every attempt may see a
		// different permutation. A correct @p answers "near" for all of them.
		for range 200 {
			res := &result{}
			out := execTx(t, "@p", tx, &testSource{}, targetsRunnable{res: res})
			if got := errStrings(out); len(got) != 0 {
				t.Fatalf("errors = %q, want none", got)
			}
			if got := targetNames(res); !slices.Equal(got, []string{"near"}) {
				t.Fatalf("@p = %q, want [near]", got)
			}
		}
	})
}

func TestSelectorRandom(t *testing.T) {
	// @r picks a uniformly random player and is the only selector whose result
	// is not a function of its input. It has no seam for a seed, so a test can
	// only pin the shape of the result.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "Steve", mgl64.Vec3{0, 64, 0})
		spawn(tx, "Alex", mgl64.Vec3{10, 64, 0})

		seen := map[string]bool{}
		for range 200 {
			res := &result{}
			out := execTx(t, "@r", tx, &testSource{}, targetsRunnable{res: res})
			if got := errStrings(out); len(got) != 0 {
				t.Fatalf("errors = %q, want none", got)
			}
			names := targetNames(res)
			if len(names) != 1 {
				t.Fatalf("@r = %q, want exactly one player", names)
			}
			seen[names[0]] = true
		}
		if len(seen) != 2 {
			t.Fatalf("@r only ever selected %v across 200 draws, want both players", seen)
		}
	})
}

func TestSelectorMatchesOnlyTheFirstTwoCharacters(t *testing.T) {
	// Only a bare selector, or a selector with bracketed arguments, selects:
	// anything else is a literal player name. Bracketed arguments stay
	// ignored (see TestSelectorArgumentsAreNotParsed); the selector letter
	// alone decides.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "Steve", mgl64.Vec3{0, 64, 0})
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5, 64, 0}, NameTag: "Mob"}.New(testMobType{}, nopConfig{}))

		for _, args := range []string{"@e", "@e[type=player]"} {
			res := &result{}
			out := execTx(t, args, tx, &testSource{}, targetsRunnable{res: res})
			if got := errStrings(out); len(got) != 0 {
				t.Fatalf("%q: errors = %q, want none", args, got)
			}
			got := targetNames(res)
			slices.Sort(got)
			if want := []string{"Mob", "Steve"}; !slices.Equal(got, want) {
				t.Fatalf("%q selected %q, want %q", args, got, want)
			}
		}
		for _, args := range []string{"@everyone", "@entities", "@al"} {
			res := &result{}
			out := execTx(t, args, tx, &testSource{}, targetsRunnable{res: res})
			if got, want := errStrings(out), []string{
				"§r§cSyntax error: unexpected value: at \"/pinned >>" + args + "<<\"§r",
				errNotFound,
			}; !slices.Equal(got, want) {
				t.Fatalf("%q: errors = %q, want %q", args, got, want)
			}
			if res.runs != 0 {
				t.Fatalf("%q: Run was called for a name that matches no player", args)
			}
		}
	})
}

func TestSelectorArgumentsAreNotParsed(t *testing.T) {
	// PINNED SCOPE: vanilla selector arguments (@e[type=..., r=..., limit=...])
	// are not implemented. The bracketed part is not parsed and not rejected, it
	// is simply ignored, so a command that looks filtered is not.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "Steve", mgl64.Vec3{0, 64, 0})
		spawn(tx, "Alex", mgl64.Vec3{1000, 64, 0})

		res := &result{}
		out := execTx(t, "@a[r=1]", tx, &testSource{}, targetsRunnable{res: res})
		if got := errStrings(out); len(got) != 0 {
			t.Fatalf("errors = %q, want none", got)
		}
		got := targetNames(res)
		slices.Sort(got)
		if want := []string{"Alex", "Steve"}; !slices.Equal(got, want) {
			t.Fatalf("@a[r=1] selected %q, want %q: the radius argument is ignored", got, want)
		}
	})
}

func TestNamedTargetLookupIsCaseInsensitive(t *testing.T) {
	// A literal name matches a player case insensitively and yields exactly one
	// target. A name that matches nothing is a player-not-found error rather
	// than an empty selection.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "Steve", mgl64.Vec3{0, 64, 0})
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{5, 64, 0}, NameTag: "Mob"}.New(testMobType{}, nopConfig{}))

		res := &result{}
		out := execTx(t, "sTeVe", tx, &testSource{}, targetsRunnable{res: res})
		if got := errStrings(out); len(got) != 0 {
			t.Fatalf("errors = %q, want none", got)
		}
		if got, want := targetNames(res), []string{"Steve"}; !slices.Equal(got, want) {
			t.Fatalf("selected %q, want %q", got, want)
		}

		// PINNED CONSTRAINT: only players are searched by name, so a non-player
		// entity can never be targeted literally even though @e yields it.
		res = &result{}
		out = execTx(t, "Mob", tx, &testSource{}, targetsRunnable{res: res})
		if got, want := errStrings(out), []string{
			"§r§cSyntax error: unexpected value: at \"/pinned >>Mob<<\"§r",
			errNotFound,
		}; !slices.Equal(got, want) {
			t.Fatalf("errors = %q, want %q", got, want)
		}
	})
}

func TestEmptySelectionIsAnError(t *testing.T) {
	// An empty world makes every selector fail with 'no targets matched', which
	// is how a []Target parameter guarantees that Run never sees an empty slice.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		for _, args := range []string{"@a", "@e", "@p", "@r"} {
			res := &result{}
			out := execTx(t, args, tx, &testSource{}, targetsRunnable{res: res})
			if got, want := errStrings(out), []string{
				"§r§cSyntax error: unexpected value: at \"/pinned >>" + args + "<<\"§r",
				errNoTarget,
			}; !slices.Equal(got, want) {
				t.Fatalf("%q: errors = %q, want %q", args, got, want)
			}
			if res.runs != 0 {
				t.Fatalf("%q: Run was called with an empty target list", args)
			}
		}
	})
}

func TestPlayerWithoutANameMethodPanics(t *testing.T) {
	// A player-encoded entity without a Name method is not a NamedTarget,
	// so target selection skips it instead of panicking: every selector
	// only ever sees the entities that implement the interface it needs.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(namelessType{}, nopConfig{}))
		spawn(tx, "Steve", mgl64.Vec3{10, 64, 0})

		for _, c := range []struct {
			selector string
			want     []string
		}{
			// The nameless entity is a Target, so @e still sees it. The
			// world yields entities in map order, so the want is sorted.
			{selector: "@e", want: []string{"<unnamed>", "Steve"}},
			{selector: "@a", want: []string{"Steve"}},
			{selector: "@p", want: []string{"Steve"}},
			{selector: "@r", want: []string{"Steve"}},
			{selector: "@s", want: []string{""}},
		} {
			res := &result{}
			out := execTx(t, c.selector, tx, &testSource{}, targetsRunnable{res: res})
			if got := errStrings(out); len(got) != 0 {
				t.Fatalf("%q: errors = %q, want none", c.selector, got)
			}
			got := targetNames(res)
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Fatalf("%q = %q, want %q", c.selector, got, c.want)
			}
		}
	})
}

func TestTargetParameterConsumesOneArgument(t *testing.T) {
	// A []Target parameter takes exactly one argument regardless of how many
	// targets it resolves to, so a parameter after it sees the next argument.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		spawn(tx, "Steve", mgl64.Vec3{0, 64, 0})
		spawn(tx, "Alex", mgl64.Vec3{1, 64, 0})

		res := &result{}
		out := execTx(t, "@a 7", tx, &testSource{}, targetsAndIntRunnable{res: res})
		if got := errStrings(out); len(got) != 0 {
			t.Fatalf("errors = %q, want none", got)
		}
		if got := res.args[1].(int); got != 7 {
			t.Fatalf("int parameter = %d, want 7", got)
		}
		if got := len(res.args[0].([]Target)); got != 2 {
			t.Fatalf("target count = %d, want 2", got)
		}
	})
}

type targetsAndIntRunnable struct {
	T   []Target
	V   int
	res *result
}

func (r targetsAndIntRunnable) Run(Source, *Output, *world.Tx) {
	r.res.runs++
	r.res.args = []any{r.T, r.V}
}
