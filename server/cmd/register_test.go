package cmd

import (
	"sync"
	"testing"

	"github.com/jv2w/Impulse/server/world"
)

// register.go keeps the command table in a package-level sync.Map. That is the
// clearest violation of CLAUDE.md 2.4 left in the package: two Impulse servers
// in one process share one command table, a command registered by one is
// runnable on the other, and nothing can ever be removed. The tests below pin
// what that costs so the stage 1 replacement has a target to hit.

type registeredRunnable struct {
	V   int
	res *result
}

func (r registeredRunnable) Run(Source, *Output, *world.Tx) { r.res.runs++ }

func TestRegisterStoresNameAndEveryAlias(t *testing.T) {
	// Register stores the command once per alias, so ByAlias finds the same
	// Command under every one of them, and Commands() reports one entry per
	// alias rather than one per command.
	res := &result{}
	command := New("pinned-register", "", []string{"pr", "PinnedRegister"}, registeredRunnable{res: res})
	Register(command)

	for _, alias := range []string{"pinned-register", "pr", "pinnedregister"} {
		got, ok := ByAlias(alias)
		if !ok {
			t.Fatalf("ByAlias(%q) = _, false, want the registered command", alias)
		}
		if got.Name() != "pinned-register" {
			t.Fatalf("ByAlias(%q).Name() = %q, want %q", alias, got.Name(), "pinned-register")
		}
	}
	// PINNED CONSTRAINT: aliases are lowercased in New, not in ByAlias, so a
	// lookup with the original casing misses. Every caller has to lowercase the
	// alias before looking it up.
	if _, ok := ByAlias("PinnedRegister"); ok {
		t.Fatal("ByAlias(\"PinnedRegister\") found the command: lookups are case sensitive")
	}

	all := Commands()
	for _, alias := range []string{"pinned-register", "pr", "pinnedregister"} {
		if _, ok := all[alias]; !ok {
			t.Fatalf("Commands() is missing the alias %q", alias)
		}
	}
}

func TestByAliasReportsAZeroCommandWhenMissing(t *testing.T) {
	// A miss returns the zero Command rather than a usable one. The zero value
	// is executable (see TestZeroCommandExecutesToANilError), so a caller that
	// ignores the bool ends up running nothing and reporting "<nil>".
	got, ok := ByAlias("pinned-command-that-was-never-registered")
	if ok {
		t.Fatal("ByAlias found a command that was never registered")
	}
	if got.Name() != "" || got.Usage() != "" || len(got.Aliases()) != 0 {
		t.Fatalf("ByAlias miss returned %#v, want the zero Command", got)
	}
}

func TestRegisterOverwritesSilently(t *testing.T) {
	// PINNED BUG: Register is a bare Store, so a second command with the same
	// name replaces the first without any report. A plugin that picks a name
	// another plugin already uses silently disables it, and the same happens
	// when one plugin's alias collides with another plugin's name.
	res := &result{}
	Register(New("pinned-collision", "first", nil, registeredRunnable{res: res}))
	if got, _ := ByAlias("pinned-collision"); got.Description() != "first" {
		t.Fatalf("Description() = %q, want %q", got.Description(), "first")
	}

	Register(New("pinned-collision", "second", nil, registeredRunnable{res: res}))
	if got, _ := ByAlias("pinned-collision"); got.Description() != "second" {
		t.Fatalf("Description() = %q, want %q: the later registration must win", got.Description(), "second")
	}

	// An alias of one command overwriting the *name* of another is the same
	// silent store, so a command can be shadowed without its own name ever
	// being registered by anyone else.
	Register(New("pinned-shadowing", "shadow", []string{"pinned-collision"}, registeredRunnable{res: res}))
	if got, _ := ByAlias("pinned-collision"); got.Description() != "shadow" {
		t.Fatalf("Description() = %q, want %q: an alias must be able to shadow a name", got.Description(), "shadow")
	}
}

func TestThereIsNoWayToUnregisterACommand(t *testing.T) {
	// PINNED CONSTRAINT: the package exports Register, ByAlias and Commands and
	// nothing else. Once a command is in the table it stays there for the life
	// of the process; a server shutting down cannot take its commands with it.
	// The only removal available is overwriting the entry with another command.
	res := &result{}
	Register(New("pinned-permanent", "", nil, registeredRunnable{res: res}))
	before := len(Commands())

	Register(New("pinned-permanent", "", nil, registeredRunnable{res: res}))
	if got := len(Commands()); got != before {
		t.Fatalf("Commands() = %d entries after re-registering, want %d", got, before)
	}
	if _, ok := ByAlias("pinned-permanent"); !ok {
		t.Fatal("the command disappeared, which would mean an unregistration path exists")
	}
}

func TestCommandsReturnsAFreshMap(t *testing.T) {
	// Commands copies the table into a new map on every call, so a caller may
	// mutate the result. That copy is also the only way to enumerate the table,
	// which makes listing commands O(n) in allocations.
	res := &result{}
	Register(New("pinned-copy", "", nil, registeredRunnable{res: res}))

	first := Commands()
	delete(first, "pinned-copy")
	if _, ok := Commands()["pinned-copy"]; !ok {
		t.Fatal("mutating the map from Commands() changed the registry")
	}
}

// pollutionRuns counts how many times TestRegistryLeaksBetweenTestRuns has run
// in this process. Under -count=1 it only ever reaches 1.
var pollutionRuns int

func TestRegistryLeaksBetweenTestRuns(t *testing.T) {
	// PINNED BUG: the registry is process-global and has no reset, so it leaks
	// across test runs inside one binary. Under -count=5 the probe registered by
	// the first run is still present on the second, which is exactly what a
	// second server instance in one process would see. Any future test that
	// asserts a command is absent is only correct on the first run.
	//
	// This test asserts both halves: absent on the first run, present on every
	// later one. It therefore fails if the registry is ever made per-instance
	// without this pin being revisited, and it fails today if the leak were
	// somehow gone.
	pollutionRuns++
	_, found := ByAlias("pinned-pollution-probe")
	if want := pollutionRuns > 1; found != want {
		t.Fatalf("run %d: ByAlias(\"pinned-pollution-probe\") found = %v, want %v", pollutionRuns, found, want)
	}
	Register(New("pinned-pollution-probe", "", nil, registeredRunnable{res: &result{}}))
}

func TestRegisterIsSafeForConcurrentUse(t *testing.T) {
	// The sync.Map makes concurrent registration and lookup race-free, which is
	// the one thing the global table buys. A per-instance replacement has to
	// keep that: commands are registered from plugin goroutines while the world
	// goroutine looks them up.
	res := &result{}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			Register(New("pinned-concurrent", "", []string{"pc"}, registeredRunnable{res: res}))
		}()
		go func() {
			defer wg.Done()
			_, _ = ByAlias("pinned-concurrent")
			_ = Commands()
			_ = i
		}()
	}
	wg.Wait()

	if _, ok := ByAlias("pinned-concurrent"); !ok {
		t.Fatal("ByAlias could not find the concurrently registered command")
	}
}

func TestRegisteredCommandsAreVisibleToEverySource(t *testing.T) {
	// PINNED CONSTRAINT: the table has no notion of which server, world or
	// source a command belongs to. Filtering is left entirely to Allower, which
	// runs per execution, so Commands() and ByAlias always report every command
	// registered anywhere in the process.
	res := &result{}
	Register(New("pinned-visible", "", nil, allowedOnlyRunnable{res: res}))

	command, ok := ByAlias("pinned-visible")
	if !ok {
		t.Fatal("ByAlias could not find the registered command")
	}
	// The command is visible even to a source that may not run any of its
	// overloads; only Params and Runnables filter.
	denied := &testSource{name: "denied"}
	if got := command.Params(denied); len(got) != 0 {
		t.Fatalf("Params() = %#v for a denied source, want none", got)
	}
	if got := command.Runnables(denied); len(got) != 0 {
		t.Fatalf("Runnables() = %v for a denied source, want none", got)
	}
	if _, listed := Commands()["pinned-visible"]; !listed {
		t.Fatal("Commands() does not list the command that a denied source cannot run")
	}
}
