package server

import (
	"net"
	"strings"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// rejectAllower is the smallest thing that can refuse a connection.
type rejectAllower struct{ reason string }

func (a rejectAllower) Allow(net.Addr, login.IdentityData, login.ClientData) (string, bool) {
	return a.reason, false
}

// The default Allower admits everyone with an empty message, and it ignores all
// three of its arguments, so no address, identity or client data can change the
// answer.
func TestDefaultAllowerAdmitsEverything(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:19132")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		addr net.Addr
		id   login.IdentityData
		cl   login.ClientData
	}{
		{"zero", nil, login.IdentityData{}, login.ClientData{}},
		{"populated", addr, login.IdentityData{DisplayName: "Steve", XUID: "2535000000000000"}, login.ClientData{DeviceOS: 7}},
	} {
		t.Run(c.name, func(t *testing.T) {
			msg, ok := allower{}.Allow(c.addr, c.id, c.cl)
			if !ok || msg != "" {
				t.Fatalf("Allow() = %q, %v; want \"\", true", msg, ok)
			}
		})
	}
}

// PINNED SCOPE: Allower is only consulted by the default listener, which hands
// conf.Allower.Allow to gophertunnel. Nothing in the Server calls it, so a
// custom Allower has no effect on any Listener a caller supplies themselves.
// What Config.New guarantees is only that the field is non-nil by the time a
// listener factory runs.
func TestCustomAllowerIsKept(t *testing.T) {
	a := rejectAllower{reason: "banned"}
	srv := newServer(t, Config{Allower: a})
	if srv.conf.Allower != Allower(a) {
		t.Fatalf("Allower = %#v, want the one passed in", srv.conf.Allower)
	}
	if msg, ok := srv.conf.Allower.Allow(nil, login.IdentityData{}, login.ClientData{}); ok || msg != "banned" {
		t.Fatalf("Allow() = %q, %v; want \"banned\", false", msg, ok)
	}
}

// PINNED CONSTRAINT: rakNetListenerFunc dereferences conf.Allower and conf.Log
// without a nil check. It is only ever called from Config.New, after both have
// been defaulted, so a Config assembled by hand and passed straight to
// rakNetListenerFunc panics instead of reporting an error.
func TestRakNetListenerFuncRequiresADefaultedConfig(t *testing.T) {
	uc := UserConfig{}
	uc.Network.Address = "127.0.0.1:0"

	if got := caught(func() { uc.rakNetListenerFunc(Config{}) }); got == nil {
		t.Fatal("rakNetListenerFunc on a zero Config did not panic")
	}
	if got := caught(func() { uc.rakNetListenerFunc(Config{Log: quiet}) }); got == nil {
		t.Fatal("rakNetListenerFunc without an Allower did not panic")
	}
}

// The standard listener binds a real UDP socket, so a port of 0 is the only
// address a test may use. A malformed address is reported as an error rather
// than panicking.
func TestRakNetListenerFunc(t *testing.T) {
	uc := UserConfig{}
	uc.Network.Address = "127.0.0.1:0"
	conf := Config{Log: quiet, Allower: allower{}, MaxPlayers: 5}

	l, err := uc.rakNetListenerFunc(conf)
	if err != nil {
		t.Fatalf("rakNetListenerFunc() = %v", err)
	}
	if _, ok := l.(listener); !ok {
		t.Fatalf("listener type = %T, want server.listener", l)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	uc.Network.Address = "not-an-address"
	if _, err = uc.rakNetListenerFunc(conf); err == nil {
		t.Fatal("a malformed address produced no error")
	}
	if !strings.HasPrefix(err.Error(), "create RakNet listener: ") {
		t.Fatalf("error = %q, want it wrapped by rakNetListenerFunc", err)
	}
}

// The default status provider reports whatever counts it is handed, without
// clamping them or filling in a sub name.
func TestStatusProviderReportsCountsVerbatim(t *testing.T) {
	p := statusProvider{name: "Impulse Server"}
	for _, c := range []struct{ players, max int }{{0, 0}, {3, 7}, {9, 4}, {-1, -2}} {
		got := p.ServerStatus(c.players, c.max)
		want := minecraft.ServerStatus{ServerName: "Impulse Server", PlayerCount: c.players, MaxPlayers: c.max}
		if got != want {
			t.Fatalf("ServerStatus(%v, %v) = %+v, want %+v", c.players, c.max, got, want)
		}
	}
	// The name is captured when Config.New builds the provider, so it is the
	// server name and never the empty string a zero Config would have.
	if got := (statusProvider{}).ServerStatus(0, 0); got.ServerName != "" {
		t.Fatalf("a zero statusProvider reported %q", got.ServerName)
	}
}

// A StatusProvider set by the caller is left alone, which also means the server
// name is then never shown unless the caller repeats it.
func TestCustomStatusProviderIsKept(t *testing.T) {
	p := statusProvider{name: "something else"}
	srv := newServer(t, Config{Name: "Impulse Server", StatusProvider: p})
	if srv.conf.StatusProvider != minecraft.ServerStatusProvider(p) {
		t.Fatalf("StatusProvider = %+v, want the one passed in", srv.conf.StatusProvider)
	}
}

// New() is Config{}.New() and nothing else, so it produces the same defaults.
func TestPackageLevelNew(t *testing.T) {
	srv := New()
	t.Cleanup(func() {
		srv.Listen()
		_ = srv.Close()
	})
	if srv.conf.Name != "Impulse Server" || srv.conf.MaxChunkRadius != 12 {
		t.Fatalf("New() = %+v, want the Config{}.New() defaults", srv.conf)
	}
	if srv.World() == nil || srv.Nether() == nil || srv.End() == nil {
		t.Fatal("New() did not create all three worlds")
	}
}

// The custom entry lists are empty until startListening builds them, and stay
// empty when nothing custom is registered. Rebuilding them is idempotent.
func TestCustomEntryListsAreBuiltAtStartup(t *testing.T) {
	srv := newServer(t, Config{})
	if srv.customBlocks != nil || srv.customItems != nil || srv.customDimensions != nil {
		t.Fatal("Config.New built the custom entry lists; startListening used to")
	}
	srv.Listen()
	for _, c := range []struct {
		name string
		n    int
	}{
		{"blocks", len(srv.customBlocks)},
		{"items", len(srv.customItems)},
		{"dimensions", len(srv.customDimensions)},
	} {
		if c.n != 0 {
			t.Fatalf("custom %v = %v, want 0 with nothing registered", c.name, c.n)
		}
	}
	if srv.customBlocks == nil || srv.customItems == nil {
		t.Fatal("the block and item lists are nil rather than empty after startup")
	}
}

// itemEntries is the vanilla item table plus whatever custom items the Server
// built, and the vanilla half comes from the embedded NBT rather than from any
// registry a Config.New call touched.
func TestItemEntries(t *testing.T) {
	srv := firstServer
	if got, want := len(vanillaItems), 1976; got != want {
		t.Fatalf("len(vanillaItems) = %v, want %v", got, want)
	}
	if got := len(srv.itemEntries()); got != len(vanillaItems) {
		t.Fatalf("len(itemEntries()) = %v, want %v", got, len(vanillaItems))
	}
	// Every entry carries the name it is keyed by, and the runtime IDs are the
	// ones from the data file rather than positions in the slice.
	entries := srv.itemEntries()
	byName := make(map[string]protocol.ItemEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	if len(byName) != len(entries) {
		t.Fatal("itemEntries() produced duplicate names")
	}
	for name, want := range vanillaItems {
		got, ok := byName[name]
		if !ok {
			t.Fatalf("%v is missing from itemEntries()", name)
		}
		if got.RuntimeID != int16(want.RuntimeID) || got.ComponentBased != want.ComponentBased || got.Version != want.Version {
			t.Fatalf("%v = %+v, want the data file's %+v", name, got, want)
		}
	}
}

// The StartGame data a joining player receives. Everything here is fixed except
// the world time and the spawn-derived position, which move with the world.
func TestDefaultGameData(t *testing.T) {
	d := firstServer.defaultGameData()
	if d.EntityUniqueID != 1 || d.EntityRuntimeID != 1 {
		t.Fatalf("entity IDs = %v/%v, want 1/1", d.EntityUniqueID, d.EntityRuntimeID)
	}
	if d.WorldName != "Impulse Server" {
		t.Fatalf("WorldName = %q, want the server name", d.WorldName)
	}
	if d.BaseGameVersion != protocol.CurrentVersion {
		t.Fatalf("BaseGameVersion = %q, want %q", d.BaseGameVersion, protocol.CurrentVersion)
	}
	if d.Difficulty != 2 {
		t.Fatalf("Difficulty = %v, want 2 (normal)", d.Difficulty)
	}
	if d.PlayerGameMode != 0 {
		t.Fatalf("PlayerGameMode = %v, want 0 (survival)", d.PlayerGameMode)
	}
	if d.PlayerPermissions != packet.PermissionLevelMember {
		t.Fatalf("PlayerPermissions = %v, want member", d.PlayerPermissions)
	}
	if !d.ServerAuthoritativeInventory || !d.PlayerMovementSettings.ServerAuthoritativeBlockBreaking {
		t.Fatal("the server-authoritative flags are no longer both set")
	}
	want := []protocol.GameRule{
		{Name: "naturalregeneration", Value: false},
		{Name: "locatorBar", Value: false},
	}
	if len(d.GameRules) != len(want) {
		t.Fatalf("GameRules = %+v, want %+v", d.GameRules, want)
	}
	for i, r := range d.GameRules {
		if r.Name != want[i].Name || r.Value != want[i].Value || r.CanBeModifiedByPlayer {
			t.Fatalf("GameRules[%v] = %+v, want %+v", i, r, want[i])
		}
	}
	// The spawn is the overworld's, raised by the player's eye height.
	spawn := firstServer.world.Spawn().Vec3Centre().Add(mgl64.Vec3{0, 1.62})
	if d.PlayerPosition != vec64To32(spawn) {
		t.Fatalf("PlayerPosition = %v, want %v", d.PlayerPosition, vec64To32(spawn))
	}
	if len(d.Items) != len(vanillaItems) {
		t.Fatalf("len(Items) = %v, want %v", len(d.Items), len(vanillaItems))
	}
}

// vec64To32 truncates rather than rounds, and carries infinities through.
func TestVec64To32(t *testing.T) {
	got := vec64To32(mgl64.Vec3{0.1, -2.5, 1 << 40})
	if got[0] != float32(0.1) || got[1] != -2.5 || got[2] != float32(1<<40) {
		t.Fatalf("vec64To32 = %v", got)
	}
	// A value no float32 can hold becomes an infinity instead of an error.
	inf := vec64To32(mgl64.Vec3{1e300, -1e300, 0})
	if !isInf(float64(inf[0]), 1) || !isInf(float64(inf[1]), -1) {
		t.Fatalf("vec64To32 of a huge vector = %v, want infinities", inf)
	}
}

func isInf(f float64, sign int) bool {
	return sign >= 0 && f > 1e308 || sign <= 0 && f < -1e308
}
