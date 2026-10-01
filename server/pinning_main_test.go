package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/cmd"
	"github.com/jv2w/Impulse/server/item/creative"
	"github.com/jv2w/Impulse/server/item/recipe"
	"github.com/jv2w/Impulse/server/world"
)

// PINNED CONSTRAINT: every fact this package pins about a *first* Config.New()
// is a fact about process-global state that no API can undo. A test body cannot
// observe it, because by the time the first test runs another test may already
// have called Config.New(). Everything irreversible is therefore measured once
// in TestMain, before m.Run(), and the tests below only assert against those
// recordings. This is what makes go test -count=5 meaningful here: m.Run()
// repeats, TestMain does not, so the recordings stay the ones taken in a
// pristine process.
//
// The same reason rules out t.Parallel() anywhere in this package. Config.New()
// mutates package-level registries in world, item/creative and item/recipe with
// no locking, several tests create Servers, and two of them hold Server.pmu
// while probing. Running any two of those at once is a data race, not a flake.

// quiet is installed as slog.Default() by TestMain, so a Config with no Log
// still writes nowhere. Anything below Error is dropped; tests that need to see
// what was logged use recordLog instead.
var quiet = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

// globals is the part of the process-wide state that Config.New() adds to.
type globals struct {
	recipes, dynamicRecipes int
	creativeItems           int
	creativeGroups          int
	customItems             int
	commands                int
	blocksFinalized         bool
}

func snapshot() globals {
	return globals{
		recipes:         len(recipe.Recipes()),
		dynamicRecipes:  len(recipe.DynamicRecipes()),
		creativeItems:   len(creative.Items()),
		creativeGroups:  len(creative.Groups()),
		customItems:     len(world.CustomItems()),
		commands:        len(cmd.Commands()),
		blocksFinalized: caught(func() { world.DefaultBlockRegistry.AirRuntimeID() }) == nil,
	}
}

// Recordings taken by TestMain in a pristine process, in the order documented
// on captureGlobals.
var (
	gPristine, gAfterProbes, gAfterFirst, gAfterSecond, gAfterRecipesDisabled globals

	pristineAirPanic   any
	cloneFirst         world.BlockRegistry
	cloneFinalizePanic any

	firstServer, secondServer *Server
	firstNewLog               *logRecording

	airRIDAfterFirst        uint32
	registerBlockPanic      any
	registerBlockStatePanic any
	finalizeAgainPanic      any
	cloneAfterFinalizePanic any
)

func TestMain(m *testing.M) {
	if s := os.Getenv(scenarioEnv); s != "" {
		runScenario(s)
		return
	}
	slog.SetDefault(quiet)
	captureGlobals()
	os.Exit(m.Run())
}

// captureGlobals performs, in a process where nothing has called Config.New()
// yet, the sequence of calls whose effects cannot be undone. The order matters:
// steps 2 and 3 are only observable while world.DefaultBlockRegistry is still
// unfinalized, steps 5 onwards only after it is, and every step adds to
// registries no later step can subtract from again.
func captureGlobals() {
	// 1. Nothing has run yet.
	gPristine = snapshot()
	pristineAirPanic = caught(func() { world.DefaultBlockRegistry.AirRuntimeID() })

	// 2. A registry from world.NewBlockRegistry() can be finalized by hand even
	// while the default one is still unfinalized: composite block hashes no
	// longer shift by the default registry, so there is no order dependence
	// left for a caller to trip on. The clone is kept so the test below can
	// assert it actually works.
	cloneFirst = world.NewBlockRegistry()
	cloneFinalizePanic = caught(cloneFirst.Finalize)

	// 3. The probe left nothing behind.
	gAfterProbes = snapshot()

	// 4. The first successful Config.New(). Its log is recorded so that the
	// order in which it creates the three worlds can be asserted.
	firstLog, firstRec := recordLog()
	firstNewLog = firstRec
	firstServer = Config{Log: firstLog}.New()
	gAfterFirst = snapshot()

	airRIDAfterFirst = world.DefaultBlockRegistry.AirRuntimeID()
	registerBlockPanic = caught(func() { world.RegisterBlock(block.Grass{}) })
	registerBlockStatePanic = caught(func() {
		world.DefaultBlockRegistry.RegisterBlockState(world.BlockState{Name: "impulse:pin"})
	})
	finalizeAgainPanic = caught(world.DefaultBlockRegistry.Finalize)
	cloneAfterFinalizePanic = caught(world.NewBlockRegistry().Finalize)

	// 5. A second Config.New() on the same process. Config.Log is left nil here
	// so that the defaulting to slog.Default() is covered as well.
	secondServer = Config{}.New()
	gAfterSecond = snapshot()

	// 6. A third Config.New(), this one with Config.VanillaRecipesDisabled set.
	// It runs last because it must not shift what steps 4 and 5 recorded.
	Config{VanillaRecipesDisabled: true}.New()
	gAfterRecipesDisabled = snapshot()
}

func caught(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

// logRecording collects the lines written to a recording logger. Server.wait
// logs from its own goroutine, so it is guarded.
type logRecording struct {
	mu    sync.Mutex
	lines []string
}

// Lines returns a copy of everything logged so far.
func (r *logRecording) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// logRecorder is a slog.Handler that appends every record, with its attributes
// flattened, to a logRecording. slog.Logger.With must keep returning a
// recorder, which is why WithAttrs folds the attributes into a prefix instead
// of delegating to another handler.
type logRecorder struct {
	attrs string
	rec   *logRecording
}

func recordLog() (*slog.Logger, *logRecording) {
	rec := &logRecording{}
	return slog.New(logRecorder{rec: rec}), rec
}

func (logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r logRecorder) Handle(_ context.Context, rec slog.Record) error {
	line := rec.Message + r.attrs
	rec.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf(" %v=%v", a.Key, a.Value)
		return true
	})
	r.rec.mu.Lock()
	r.rec.lines = append(r.rec.lines, line)
	r.rec.mu.Unlock()
	return nil
}

func (r logRecorder) WithAttrs(as []slog.Attr) slog.Handler {
	for _, a := range as {
		r.attrs += fmt.Sprintf(" %v=%v", a.Key, a.Value)
	}
	return r
}

func (r logRecorder) WithGroup(string) slog.Handler { return r }

// newServer creates a Server that is shut down when the test ends. Close panics
// unless the Server was started, so the cleanup starts it first when the test
// did not. Close itself is guarded by a sync.Once and may be called twice.
func newServer(t *testing.T, conf Config) *Server {
	if conf.Log == nil {
		conf.Log = quiet
	}
	srv := conf.New()
	t.Cleanup(func() {
		if srv.started.Load() == nil {
			srv.Listen()
		}
		_ = srv.Close()
	})
	return srv
}

// Scenario support. A fact about the very first Config.New() of a process can
// only be measured once, and captureGlobals already spends that one chance. A
// scenario re-runs this test binary with scenarioEnv set; TestMain then runs
// the named scenario in that fresh process instead of any test, printing
// key=value lines that the parent asserts on.
const scenarioEnv = "IMPULSE_PIN_SCENARIO"

const (
	scenarioCreativeGroupShift          = "creative-group-shift"
	scenarioCustomBlockRegistryFirst    = "custom-block-registry-first"
	scenarioVanillaRecipesDisabledFirst = "vanilla-recipes-disabled-first"
)

func runScenario(name string) {
	slog.SetDefault(quiet)
	switch name {
	case scenarioCustomBlockRegistryFirst:
		// Config.Blocks documents a registry of the caller's own. Handing one
		// to the very first Config.New() of a process is the case that has to
		// work, because that is when world.DefaultBlockRegistry is still
		// unfinalized and the composite block hashes have nothing to shift by.
		br := world.NewBlockRegistry()
		fmt.Printf("panic=%v\n", caught(func() { Config{Log: quiet, Blocks: br}.New() }))
		fmt.Printf("air=%d\n", br.AirRuntimeID())
		fmt.Printf("defaultair=%d\n", world.DefaultBlockRegistry.AirRuntimeID())
	case scenarioVanillaRecipesDisabledFirst:
		// VanillaRecipesDisabled is only ever meaningful on the Config.New()
		// that would otherwise fill the recipe tables, which after the fill is
		// kept to one is the first one that asks for them.
		Config{Log: quiet, VanillaRecipesDisabled: true}.New()
		fmt.Printf("recipes=%d\n", len(recipe.Recipes()))
		fmt.Printf("dynamic=%d\n", len(recipe.DynamicRecipes()))
		fmt.Printf("creativeitems=%d\n", len(creative.Items()))
	case scenarioCreativeGroupShift:
		// A single creative group registered before the first Config.New()
		// takes index 0, so every vanilla group lands one index higher than the
		// index the vanilla item data refers to.
		creative.RegisterGroup(creative.Group{Name: "impulse:pin"})
		Config{Log: quiet}.New()

		groups, items := creative.Groups(), creative.Items()
		counts := map[string]int{}
		for _, it := range items {
			counts[it.Group]++
		}
		fmt.Printf("groups=%d\n", len(groups))
		fmt.Printf("group0=%s\n", groups[0].Name)
		fmt.Printf("group1=%s\n", groups[1].Name)
		fmt.Printf("items=%d\n", len(items))
		fmt.Printf("item0group=%s\n", items[0].Group)
		fmt.Printf("misplaced=%d\n", counts["impulse:pin"])
		fmt.Printf("planks=%d\n", counts["itemGroup.name.planks"])
	default:
		fmt.Fprintf(os.Stderr, "unknown scenario %q\n", name)
		os.Exit(2)
	}
}

// scenario runs name in a fresh copy of this test binary and returns the
// key=value lines it printed.
func scenario(t *testing.T, name string) map[string]string {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), scenarioEnv+"="+name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scenario %v: %v\n%s", name, err, out)
	}
	res := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			res[k] = v
		}
	}
	return res
}
