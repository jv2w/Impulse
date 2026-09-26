package server

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/player"
	"github.com/jv2w/Impulse/server/player/chat"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/generator"
	"github.com/sandertv/gophertunnel/minecraft/resource"
)

// The defaults Config.New() fills in are read off the Server it returns, which
// keeps the filled-in Config as srv.conf. firstServer and secondServer are the
// two Servers TestMain created; neither has been mutated since.
func TestConfigDefaults(t *testing.T) {
	c := firstServer.conf
	if c.Name != "Impulse Server" {
		t.Fatalf("Name = %q, want %q", c.Name, "Impulse Server")
	}
	if c.MaxChunkRadius != 12 {
		t.Fatalf("MaxChunkRadius = %v, want 12", c.MaxChunkRadius)
	}
	if c.ShutdownMessage != chat.MessageServerDisconnect {
		t.Fatalf("ShutdownMessage = %+v, want chat.MessageServerDisconnect", c.ShutdownMessage)
	}
	if got, want := c.StatusProvider, (statusProvider{name: "Impulse Server"}); got != want {
		t.Fatalf("StatusProvider = %+v, want %+v", got, want)
	}
	if _, ok := c.PlayerProvider.(player.NopProvider); !ok {
		t.Fatalf("PlayerProvider = %T, want player.NopProvider", c.PlayerProvider)
	}
	if _, ok := c.WorldProvider.(world.NopProvider); !ok {
		t.Fatalf("WorldProvider = %T, want world.NopProvider", c.WorldProvider)
	}
	if _, ok := c.Allower.(allower); !ok {
		t.Fatalf("Allower = %T, want server.allower", c.Allower)
	}
	if c.Blocks != world.DefaultBlockRegistry {
		t.Fatal("Blocks was not defaulted to world.DefaultBlockRegistry")
	}
	if got := len(c.Entities.Types()); got != 16 {
		t.Fatalf("len(Entities.Types()) = %v, want 16 (entity.DefaultRegistry)", got)
	}
	// Fields with no default: a zero MaxPlayers means "grow", and everything
	// below is left at the zero value rather than being filled in.
	if c.MaxPlayers != 0 || c.RandomTickSpeed != 0 || c.SaveInterval != 0 ||
		c.ChunkUnloadInterval != 0 || c.ChunkLoadWorkers != 0 {
		t.Fatalf("Config.New filled in a field it used to leave zero: %+v", c)
	}
	if c.JoinMessage != (chat.Translation{}) || c.QuitMessage != (chat.Translation{}) {
		t.Fatal("JoinMessage/QuitMessage were defaulted; only ShutdownMessage is")
	}
	// No custom items or blocks exist, so packbuilder produces no pack and
	// Resources stays empty even with DisableResourceBuilding unset.
	if len(c.Resources) != 0 {
		t.Fatalf("len(Resources) = %v, want 0", len(c.Resources))
	}
	// A nil Log becomes slog.Default(), which TestMain set to quiet.
	if secondServer.conf.Log != quiet {
		t.Fatal("a nil Config.Log was not replaced with slog.Default()")
	}
}

// Config.New copies Resources so that a caller editing their slice afterwards
// cannot change what the Server sends. The packs themselves are shared.
func TestConfigCopiesResources(t *testing.T) {
	in := make([]*resource.Pack, 2)
	srv := newServer(t, Config{Resources: in})
	got := srv.conf.Resources
	if len(got) != 2 {
		t.Fatalf("len(Resources) = %v, want 2", len(got))
	}
	in[0] = new(resource.Pack)
	if got[0] != nil {
		t.Fatal("Config.New kept the caller's slice rather than a copy")
	}
}

// PINNED SCOPE: only a zero MaxChunkRadius is replaced. A negative radius or a
// negative player count is carried through untouched, and MaxPlayerCount then
// reports the negative number as the server's capacity.
func TestConfigDoesNotValidateNumbers(t *testing.T) {
	srv := newServer(t, Config{MaxChunkRadius: -5, MaxPlayers: -1})
	if srv.conf.MaxChunkRadius != -5 {
		t.Fatalf("MaxChunkRadius = %v, want the -5 passed in", srv.conf.MaxChunkRadius)
	}
	if got := srv.MaxPlayerCount(); got != -1 {
		t.Fatalf("MaxPlayerCount() = %v, want -1", got)
	}
	full := newServer(t, Config{MaxPlayers: 0})
	if got := full.MaxPlayerCount(); got != 1 {
		t.Fatalf("MaxPlayerCount() with MaxPlayers 0 = %v, want PlayerCount+1 = 1", got)
	}
}

// PINNED CONSTRAINT: the three worlds are always created in the order
// overworld, nether, end, and each is logged twice. The wiring below depends on
// it only through the pointers taken in conf.go:206-208, but the log is the one
// place the order is observable, so it is asserted here.
func TestWorldsAreCreatedInDimensionOrder(t *testing.T) {
	want := []string{
		"config: no listeners set, no connections will be accepted",
		"Loading dimension... dimension=overworld",
		"Opened dimension. dimension=overworld name=World",
		"Loading dimension... dimension=nether",
		"Opened dimension. dimension=nether name=World",
		"Loading dimension... dimension=end",
		"Opened dimension. dimension=end name=World",
	}
	if got := firstNewLog.Lines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Config.New logged\n%q\nwant\n%q", got, want)
	}
}

// All three worlds carry the same name and only the end starts players away
// from the origin.
func TestWorldDefaults(t *testing.T) {
	srv := firstServer
	for _, c := range []struct {
		w     *world.World
		dim   world.Dimension
		spawn [3]int
	}{
		{srv.World(), world.Overworld, [3]int{0, 0, 0}},
		{srv.Nether(), world.Nether, [3]int{0, 0, 0}},
		{srv.End(), world.End, [3]int{100, 50, 0}},
	} {
		if c.w.Dimension() != c.dim {
			t.Fatalf("dimension = %v, want %v", c.w.Dimension(), c.dim)
		}
		if c.w.Name() != "World" {
			t.Fatalf("%v name = %q, want %q", c.dim, c.w.Name(), "World")
		}
		if got := c.w.Spawn(); [3]int{got.X(), got.Y(), got.Z()} != c.spawn {
			t.Fatalf("%v spawn = %v, want %v", c.dim, got, c.spawn)
		}
		if got, want := c.w.DefaultGameMode(), world.GameModeSurvival; got != want {
			t.Fatalf("%v default game mode = %T, want %T", c.dim, got, want)
		}
		if c.w.BlockRegistry() != world.DefaultBlockRegistry {
			t.Fatalf("%v does not use the default block registry", c.dim)
		}
	}
}

// Portals point at the other two worlds of the same Server. A portal leading
// back to the dimension a world already is returns that same world, because
// world.PortalDestination falls back to the receiver when the configured
// function yields nil.
func TestPortalDestinationsAreCrossWired(t *testing.T) {
	srv := firstServer
	for _, c := range []struct {
		name string
		from *world.World
		dim  world.Dimension
		want *world.World
	}{
		{"overworld to nether", srv.World(), world.Nether, srv.Nether()},
		{"overworld to end", srv.World(), world.End, srv.End()},
		{"overworld to overworld", srv.World(), world.Overworld, srv.World()},
		{"nether to nether", srv.Nether(), world.Nether, srv.World()},
		{"nether to end", srv.Nether(), world.End, srv.End()},
		{"end to end", srv.End(), world.End, srv.World()},
		{"end to nether", srv.End(), world.Nether, srv.Nether()},
	} {
		if got := c.from.PortalDestination(c.dim); got != c.want {
			t.Fatalf("%v: got %v, want %v", c.name, got.Dimension(), c.want.Dimension())
		}
	}
}

// PINNED BUG: loadGenerator panics on any dimension that is not one of the
// three built-in ones, rather than returning an error. It is reachable
// from Config.New for any caller that registers a custom dimension and leaves
// Config.Generator nil.
func TestLoadGeneratorPanicsOnUnknownDimensions(t *testing.T) {
	for _, dim := range []world.Dimension{world.Overworld, world.Nether, world.End} {
		if _, ok := loadGenerator(dim).(generator.Flat); !ok {
			t.Fatalf("%v generator = %T, want generator.Flat", dim, loadGenerator(dim))
		}
	}
	if got := caught(func() { loadGenerator(nil) }); got != "should never happen" {
		t.Fatalf("loadGenerator(nil) panicked with %v, want %q", got, "should never happen")
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	want := UserConfig{}
	want.Network.Address = ":19132"
	want.Server.Name = "Impulse Server"
	want.Server.AuthEnabled = true
	want.World.SaveData = true
	want.World.Folder = "world"
	want.Players.MaximumChunkRadius = 32
	want.Players.SaveData = true
	want.Players.Folder = "players"
	want.Resources.AutoBuildPack = true
	want.Resources.Folder = "resources"
	// Network.Transport is a slice, so the struct is no longer comparable with
	// ==: DeepEqual compares it by value instead.
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("DefaultConfig() = %+v, want %+v", c, want)
	}
	// The fields left false are as much a part of the default as the ones set:
	// join and quit messages are on, emote chat is not muted and resource packs
	// are not required.
	if c.Server.DisableJoinQuitMessages || c.Server.MuteEmoteChat || c.Resources.Required || c.Players.MaxCount != 0 {
		t.Fatalf("DefaultConfig() no longer leaves its optional fields off: %+v", c)
	}
}

// PINNED CONSTRAINT: DefaultConfig names folders relative to the working
// directory, and UserConfig.Config creates all three of them as a side effect
// of being called. A test that converts a default UserConfig without
// redirecting the folders writes world/, players/ and resources/ into whatever
// directory it runs in.
func TestDefaultUserConfigConversion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	uc := DefaultConfig()
	conf, err := uc.Config(quiet)
	if err != nil {
		t.Fatalf("Config() = %v", err)
	}
	t.Cleanup(func() {
		_ = conf.WorldProvider.Close()
		_ = conf.PlayerProvider.Close()
	})
	for _, name := range []string{"world", "players", "resources"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("Config() did not create %v: %v", name, err)
		}
	}
	if conf.WorldProvider == nil || conf.PlayerProvider == nil {
		t.Fatal("SaveData did not produce providers")
	}
	if len(conf.Listeners) != 1 {
		t.Fatalf("len(Listeners) = %v, want 1", len(conf.Listeners))
	}
	if conf.AuthDisabled {
		t.Fatal("AuthEnabled true produced AuthDisabled true")
	}
	if conf.DisableResourceBuilding {
		t.Fatal("AutoBuildPack true produced DisableResourceBuilding true")
	}
	if conf.MaxChunkRadius != 32 || conf.Name != "Impulse Server" {
		t.Fatalf("MaxChunkRadius = %v, Name = %q", conf.MaxChunkRadius, conf.Name)
	}
	if conf.JoinMessage != chat.MessageJoin || conf.QuitMessage != chat.MessageQuit {
		t.Fatal("join/quit messages were not set")
	}
	if conf.Log != quiet {
		t.Fatal("Config() did not carry the logger through")
	}
}

// A zero UserConfig leaves both providers nil for Config.New to default, turns
// authentication off because AuthEnabled is false, and still sets the join and
// quit messages because they are opt-out rather than opt-in.
func TestZeroUserConfigConversion(t *testing.T) {
	t.Chdir(t.TempDir())

	base := UserConfig{}
	base.Resources.Folder = "packs"
	conf, err := base.Config(quiet)
	if err != nil {
		t.Fatalf("Config() = %v", err)
	}
	if conf.WorldProvider != nil || conf.PlayerProvider != nil {
		t.Fatalf("providers = %v/%v, want nil", conf.WorldProvider, conf.PlayerProvider)
	}
	if !conf.AuthDisabled {
		t.Fatal("AuthEnabled false did not disable authentication")
	}
	if !conf.DisableResourceBuilding {
		t.Fatal("AutoBuildPack false did not disable resource building")
	}
	if conf.JoinMessage != chat.MessageJoin || conf.QuitMessage != chat.MessageQuit {
		t.Fatal("join/quit messages are opt-out and should be set here")
	}
	if conf.Name != "" || conf.MaxChunkRadius != 0 || conf.MaxPlayers != 0 {
		t.Fatalf("a zero UserConfig produced %q/%v/%v", conf.Name, conf.MaxChunkRadius, conf.MaxPlayers)
	}
	// PINNED CONSTRAINT: a zero UserConfig cannot be converted at all. Its empty
	// Resources.Folder is created as "" and then read, which always fails, so
	// UserConfig{}.Config() is an error path rather than a usable default.
	_, err = UserConfig{}.Config(quiet)
	if err == nil {
		t.Fatal("UserConfig{}.Config() succeeded; the empty resource folder used to fail")
	}
	if !strings.HasPrefix(err.Error(), "load resources: create dir: ") {
		t.Fatalf("error = %q, want the directory creation to be blamed", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %q does not unwrap to fs.ErrNotExist", err)
	}
}

// PINNED SCOPE: UserConfig.Config reports failures as errors, but the partially
// filled Config is returned alongside them. A caller that ignores the error
// gets a Config with providers already opened.
func TestUserConfigErrorPaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	uc := UserConfig{}
	uc.Resources.Folder = "packs"
	if err := os.MkdirAll("packs", 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("packs", "junk.txt"), []byte("not a pack"), 0666); err != nil {
		t.Fatal(err)
	}
	conf, err := uc.Config(quiet)
	if err == nil {
		t.Fatal("a file that is not a resource pack produced no error")
	}
	if !strings.HasPrefix(err.Error(), "load resources: compile resource (junk.txt): ") {
		t.Fatalf("error = %q, want it to name the offending file", err)
	}
	// The listener func is appended last, so an error before it leaves the
	// returned Config with no listeners at all.
	if len(conf.Listeners) != 0 {
		t.Fatalf("len(Listeners) = %v, want 0 on the error path", len(conf.Listeners))
	}
}

// loadResources creates the directory it is asked to read and reports an empty
// directory as an empty, non-nil slice.
func TestLoadResources(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "made", "up")
	packs, err := loadResources(dir)
	if err != nil {
		t.Fatalf("loadResources() = %v", err)
	}
	if len(packs) != 0 {
		t.Fatalf("len(packs) = %v, want 0", len(packs))
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("loadResources did not create the directory: %v", err)
	}

	// A MkdirAll failure is reported as the creation failing, so the caller
	// learns why the directory is unusable instead of being told it does not
	// exist.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0666); err != nil {
		t.Fatal(err)
	}
	_, err = loadResources(filepath.Join(file, "under-a-file"))
	if err == nil {
		t.Fatal("loadResources under a regular file produced no error")
	}
	if !strings.HasPrefix(err.Error(), "create dir: ") {
		t.Fatalf("error = %q, want the creation to be blamed", err)
	}
}
