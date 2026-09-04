package server

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/session"
	"github.com/jv2w/Impulse/server/world"
)

// blockingListener is a Listener whose Accept parks until Close is called, the
// shape every real Listener has. Server.listen only leaves its loop when Accept
// returns an error, so this is what a shutdown has to unblock.
type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (session.Conn, error) {
	<-l.closed
	return nil, errors.New("listener closed")
}

func (l *blockingListener) Disconnect(session.Conn, string) error { return nil }

func (l *blockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func listenerConf(ls ...Listener) Config {
	conf := Config{Log: quiet}
	for _, l := range ls {
		conf.Listeners = append(conf.Listeners, func(Config) (Listener, error) { return l, nil })
	}
	return conf
}

// A listener factory that fails is logged and skipped. The Server is still
// created, and with every factory failing it accepts nobody while looking
// perfectly healthy.
func TestListenerFactoryErrorsAreSkipped(t *testing.T) {
	log, rec := recordLog()
	good := newBlockingListener()
	conf := Config{Log: log, Listeners: []func(Config) (Listener, error){
		func(Config) (Listener, error) { return nil, errors.New("boom") },
		func(Config) (Listener, error) { return good, nil },
	}}
	srv := newServer(t, conf)
	lines := rec.Lines()
	if len(srv.listeners) != 1 || srv.listeners[0] != Listener(good) {
		t.Fatalf("listeners = %v, want only the one that was created", srv.listeners)
	}
	found := false
	for _, line := range lines {
		if line == "create listener: boom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the failed factory was not logged: %q", lines)
	}
	// With a listener present the "no listeners set" warning is not emitted,
	// even though only one of the two factories worked.
	for _, line := range lines {
		if line == "config: no listeners set, no connections will be accepted" {
			t.Fatal("the no-listeners warning was emitted despite a listener existing")
		}
	}
}

// PINNED CONSTRAINT: Listen may be called once and Close only after Listen. A
// Server that is created and then abandoned cannot be shut down at all: Close
// panics, and the three worlds it started keep ticking for the life of the
// process.
func TestListenAndCloseOrdering(t *testing.T) {
	srv := newServer(t, Config{})
	if got := caught(func() { _ = srv.Close() }); got != "server not yet running" {
		t.Fatalf("Close before Listen panicked with %v, want %q", got, "server not yet running")
	}
	srv.Listen()
	if got := caught(srv.Listen); got != "start server: already started" {
		t.Fatalf("a second Listen panicked with %v, want %q", got, "start server: already started")
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	// Close is guarded by a sync.Once and always reports success, so a second
	// call is a no-op rather than an error.
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	if got := caught(srv.Listen); got != "start server: already started" {
		t.Fatalf("Listen after Close panicked with %v, want %q", got, "start server: already started")
	}
}

// Close is what unblocks a parked Listener: it closes every Listener, each
// listen goroutine sees the error and drops its wg count, and wait then closes
// the incoming channel so Accept's iterator ends.
func TestCloseUnblocksAcceptThroughTheListeners(t *testing.T) {
	l1, l2 := newBlockingListener(), newBlockingListener()
	srv := newServer(t, listenerConf(l1, l2))
	srv.Listen()

	accepted := make(chan int, 1)
	go func() {
		n := 0
		for range srv.Accept() {
			n++
		}
		accepted <- n
	}()

	select {
	case <-accepted:
		t.Fatal("Accept ended before the Server was closed")
	case <-time.After(50 * time.Millisecond):
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	select {
	case n := <-accepted:
		if n != 0 {
			t.Fatalf("Accept yielded %v players, want 0", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Accept did not end after Close")
	}

	// Every later Accept ends immediately: the incoming channel stays closed.
	for range srv.Accept() {
		t.Fatal("Accept yielded after the Server was closed")
	}
}

// PINNED SCOPE: a Server with no Listeners closes its incoming channel as soon
// as Listen returns, because wait() has nothing to wait for. Accept therefore
// ends immediately rather than blocking, and nothing distinguishes that from a
// Server that was closed.
func TestAcceptEndsImmediatelyWithoutListeners(t *testing.T) {
	srv := newServer(t, Config{})
	srv.Listen()
	done := make(chan struct{})
	go func() {
		for range srv.Accept() {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Accept blocked on a Server with no listeners")
	}
}

// The shutdown sequence is fixed and fully logged. The order matters: players
// are disconnected and awaited before the player provider is closed, and the
// worlds close before the listeners.
func TestCloseLogsItsShutdownOrder(t *testing.T) {
	log, rec := recordLog()
	l := newBlockingListener()
	conf := listenerConf(l)
	conf.Log = log
	srv := newServer(t, conf)
	srv.Listen()
	if err := srv.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	want := []string{"Server closing...", "Disconnecting players...", "Closing player provider...", "Closing worlds...", "Closing listeners..."}
	lines := rec.Lines()
	i := 0
	for _, line := range lines {
		if i < len(want) && line == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("Close logged\n%q\nwant it to contain, in order,\n%q", lines, want)
	}
}

// PlayerByName and PlayerByXUID walk the whole map rather than indexing it, so
// they are the two readers that a writer resizing the map underneath would take
// the process down with. They wait for the writer like every other reader.
func TestNameAndXUIDLookupsWaitForWriters(t *testing.T) {
	srv := newServer(t, Config{})
	id := uuid.New()
	srv.p[id] = &onlinePlayer{name: "Steve", xuid: "2535000000000000"}

	srv.pmu.Lock()
	done := make(chan string, 2)
	go func() {
		srv.PlayerByName("Steve")
		done <- "PlayerByName"
	}()
	go func() {
		srv.PlayerByXUID("2535000000000000")
		done <- "PlayerByXUID"
	}()
	// The lock is released before anything is reported: t.Fatalf runs
	// runtime.Goexit, and the cleanup newServer installed closes the Server,
	// which takes pmu itself.
	early := ""
	select {
	case early = <-done:
	case <-time.After(100 * time.Millisecond):
	}
	srv.pmu.Unlock()
	if early != "" {
		t.Fatalf("%v ran while the write lock was held", early)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("a lookup never woke up after the writer released pmu")
		}
	}
}

// The lookups that do take the lock block while a writer holds it, which is
// what the test above contrasts against.
func TestLockedLookupsWaitForWriters(t *testing.T) {
	srv := newServer(t, Config{})
	srv.pmu.Lock()
	blocked := make(chan struct{})
	go func() {
		srv.PlayerCount()
		srv.Player(uuid.New())
		srv.MaxPlayerCount()
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("PlayerCount/Player/MaxPlayerCount ran while the write lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	srv.pmu.Unlock()
	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("the readers never woke up after the writer released pmu")
	}
}

// pmu is exercised for real here: the readers and the writers run at the same
// time on the same map, which is what -race has to see to be worth anything.
func TestPlayerMapUnderConcurrentAccess(t *testing.T) {
	srv := newServer(t, Config{})
	const writers, readers, iterations = 4, 4, 500

	ids := make([][]uuid.UUID, writers)
	for i := range ids {
		ids[i] = make([]uuid.UUID, iterations)
		for j := range ids[i] {
			ids[i][j] = uuid.New()
		}
	}

	var wg sync.WaitGroup
	wg.Add(writers + readers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for _, id := range ids[w] {
				srv.pmu.Lock()
				srv.p[id] = &onlinePlayer{name: id.String(), xuid: id.String()}
				srv.pmu.Unlock()
				srv.pmu.Lock()
				delete(srv.p, id)
				srv.pmu.Unlock()
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		go func(r int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if n := srv.PlayerCount(); n < 0 {
					t.Errorf("PlayerCount() = %v", n)
					return
				}
				srv.Player(ids[r%writers][i])
				srv.PlayerByName(ids[r%writers][i].String())
				srv.PlayerByXUID(ids[r%writers][i].String())
				srv.MaxPlayerCount()
				for range srv.Players(nil) {
				}
			}
		}(r)
	}
	wg.Wait()

	if got := srv.PlayerCount(); got != 0 {
		t.Fatalf("PlayerCount() = %v after every writer removed its entries, want 0", got)
	}
}

// PINNED SCOPE: Server.Players yields nothing for an entry whose handle is not
// in a world. player.Call fails for it and the iterator skips it silently, so
// the count Players walks and the count PlayerCount reports can disagree.
func TestPlayersSkipsEntriesItCannotResolve(t *testing.T) {
	srv := newServer(t, Config{})
	id := uuid.New()
	srv.p[id] = &onlinePlayer{name: "Steve", xuid: "2535000000000000"}

	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("PlayerCount() = %v, want 1", got)
	}
	n := 0
	for range srv.Players(nil) {
		n++
	}
	if n != 0 {
		t.Fatalf("Players() yielded %v, want 0", n)
	}
	// The lookups still find it, handle and all.
	if h, ok := srv.Player(id); !ok || h != nil {
		t.Fatalf("Player() = %v, %v; want the nil handle that was stored", h, ok)
	}
	if _, ok := srv.PlayerByName("Steve"); !ok {
		t.Fatal("PlayerByName did not find the entry")
	}
	if _, ok := srv.PlayerByXUID("2535000000000000"); !ok {
		t.Fatal("PlayerByXUID did not find the entry")
	}
	if _, ok := srv.PlayerByName("Alex"); ok {
		t.Fatal("PlayerByName found a name that was never added")
	}
}

// dimension maps a Dimension onto one of the Server's three worlds, and its
// default branch means an unknown or nil dimension lands in the overworld
// rather than failing.
func TestDimensionLookupFallsBackToTheOverworld(t *testing.T) {
	srv := firstServer
	for _, c := range []struct {
		dim  world.Dimension
		want *world.World
	}{
		{world.Overworld, srv.world},
		{world.Nether, srv.nether},
		{world.End, srv.end},
		{nil, srv.world},
	} {
		if got := srv.dimension(c.dim); got != c.want {
			t.Fatalf("dimension(%v) = %v, want %v", c.dim, got.Dimension(), c.want.Dimension())
		}
	}
}

// PINNED CONSTRAINT: CloseOnProgramEnd installs a SIGINT/SIGTERM handler on a
// goroutine that calls Close. Neither signal can be raised against this process
// portably, so what is pinned here is the part that is reachable: the call is
// safe before Listen, and the Server it watches can still be closed normally
// afterwards. Whether Close is reached from the signal is not covered.
func TestCloseOnProgramEndDoesNotStartTheServer(t *testing.T) {
	srv := newServer(t, Config{})
	srv.CloseOnProgramEnd()
	if srv.started.Load() != nil {
		t.Fatal("CloseOnProgramEnd started the Server")
	}
	if got := caught(func() { _ = srv.Close() }); got != "server not yet running" {
		t.Fatalf("Close still panicked with %v, want %q", got, "server not yet running")
	}
	srv.Listen()
	if err := srv.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}
