package server

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/internal/blockinternal"
	"github.com/jv2w/Impulse/server/internal/iteminternal"
	"github.com/jv2w/Impulse/server/internal/sliceutil"
	_ "github.com/jv2w/Impulse/server/item" // Imported for maintaining correct initialisation order.
	"github.com/jv2w/Impulse/server/player"
	"github.com/jv2w/Impulse/server/player/chat"
	"github.com/jv2w/Impulse/server/player/skin"
	"github.com/jv2w/Impulse/server/session"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/text/language"
)

// Server implements an Impulse server. It runs the main server loop and
// handles the connections of players trying to join the server.
type Server struct {
	conf Config

	once    sync.Once
	started atomic.Pointer[time.Time]

	world, nether, end *world.World

	customBlocks     []protocol.BlockEntry
	customItems      []protocol.ItemEntry
	customDimensions []protocol.DimensionDefinition

	listeners []Listener
	incoming  chan incoming

	pmu sync.RWMutex
	// p holds a map of all players currently connected to the server. When they
	// leave, they are removed from the map.
	p map[uuid.UUID]*onlinePlayer
	// reserved holds the UUID of every login from the moment it passes its
	// identity check until its player data has been saved after it left. A
	// UUID is in p only for part of that time, so p alone cannot tell whether
	// a login would load data another session of the same UUID is about to
	// overwrite. Guarded by pmu.
	reserved map[uuid.UUID]struct{}
	// closing is set under pmu once the server starts closing. No login is
	// reserved or added to p after that.
	closing bool
	// pwg is a sync.WaitGroup used to wait for all players to be disconnected
	// before server shutdown, so that their data is saved properly. It counts
	// the entries of reserved.
	pwg sync.WaitGroup
	// joinCtx is cancelled when the server starts closing, so that logins still
	// being set up stop waiting on the client and on Accept. cancelJoins
	// cancels it.
	joinCtx     context.Context
	cancelJoins context.CancelFunc
	// wg is used to wait for all Listeners to be closed and their respective
	// goroutines to be finished.
	wg sync.WaitGroup
}

// incoming holds data of a player that is connecting to the server.
type incoming struct {
	conf player.Config
	s    *session.Session
	p    *onlinePlayer
	w    *world.World
}

// onlinePlayer holds the entity handle, XUID and name of a player.
type onlinePlayer struct {
	handle *world.EntityHandle
	xuid   string
	name   string
}

// New creates a Server using a default Config. The Server's worlds are created
// and connections from the Server's listeners may be accepted by calling
// Server.Listen() and Server.Accept() afterwards.
func New() *Server {
	var conf Config
	return conf.New()
}

// Listen starts running the server's listeners. Connections will be accepted
// until the listeners are closed using a call to Close. Once Listen is called,
// players may be accepted using Server.Accept().
func (srv *Server) Listen() {
	t := time.Now()
	if !srv.started.CompareAndSwap(nil, &t) {
		panic("start server: already started")
	}

	info, _ := debug.ReadBuildInfo()
	if info == nil {
		info = &debug.BuildInfo{GoVersion: "N/A", Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "N/A"}}}
	}
	revision := ""
	for _, set := range info.Settings {
		if set.Key == "vcs.revision" {
			revision = set.Value
		}
	}

	srv.conf.Log.Info("Impulse server started.", "mc-version", protocol.CurrentVersion, "go-version", info.GoVersion, "commit", revision)
	srv.startListening()
	go srv.wait()
}

// Accept accepts incoming players into the server, returning an iterator that
// yields players that join the server while blocking otherwise. The iterator
// returned ends when the Server is closed using a call to Close. The loop body
// runs on the player's world owner: blocking there stalls that world, and
// calling world.Call, world.CallEntity, world.CallRef, or Task.Wait for the
// same owner deadlocks. Players returned are only valid within the block of the
// for loop used to iterate over them. Use p.H() with player.Do for work that
// outlives the loop body:
//
//	for p := range srv.Accept() {
//	  // p is valid here
//	  go func() {
//	    // p is no longer valid here
//	  }()
//	}
//	// p is no longer valid here
func (srv *Server) Accept() iter.Seq[*player.Player] {
	return func(yield func(*player.Player) bool) {
		for {
			inc, ok := <-srv.incoming
			if !ok {
				return
			}
			// The handle's UUID is fixed at creation, so reading it once outside
			// the lock returns what both calls returned before.
			id := inc.p.handle.UUID()

			// Which input was the problem: a login handed to Accept after close
			// had already disconnected everyone in p. Nothing disconnected it,
			// so close waited for it forever and the worlds were never saved.
			srv.pmu.Lock()
			closing := srv.closing
			if !closing {
				srv.p[id] = inc.p
			}
			srv.pmu.Unlock()
			if closing {
				srv.abortJoin(inc, "server closing")
				continue
			}

			ret, err := world.Call(context.Background(), inc.w, func(tx *world.Tx) (bool, error) {
				p := tx.AddEntity(inc.p.handle).(*player.Player)
				inc.s.Spawn(p, tx)
				return !yield(p), nil
			})
			if err != nil {
				srv.pmu.Lock()
				delete(srv.p, id)
				srv.pmu.Unlock()
				srv.abortJoin(inc, "join failed")
				continue
			}
			if ret {
				return
			}
		}
	}
}

// World returns the overworld of the server. Players will be spawned in this
// world and this world will be read from and written to when the world is
// edited.
func (srv *Server) World() *world.World {
	return srv.world
}

// Nether returns the nether world of the server. Players are transported to it
// when entering a nether portal in the world returned by the World method.
func (srv *Server) Nether() *world.World {
	return srv.nether
}

// End returns the end world of the server. Players are transported to it when
// entering an end portal in the world returned by the World method.
func (srv *Server) End() *world.World {
	return srv.end
}

// MaxPlayerCount returns the maximum amount of players that are allowed to
// play on the server at the same time. Players trying to join when the server
// is full will be refused to enter. If the config has a maximum player count
// set to 0, MaxPlayerCount will return Server.PlayerCount + 1.
func (srv *Server) MaxPlayerCount() int {
	if srv.conf.MaxPlayers == 0 {
		// len cannot panic, so unlocking directly runs the same unlock the defer
		// did, without the defer frame.
		srv.pmu.RLock()
		n := len(srv.p)
		srv.pmu.RUnlock()
		return n + 1
	}
	return srv.conf.MaxPlayers
}

// PlayerCount returns the total number of players connected to the Server.
func (srv *Server) PlayerCount() int {
	// len cannot panic, so the unlock always ran here anyway.
	srv.pmu.RLock()
	n := len(srv.p)
	srv.pmu.RUnlock()
	return n
}

// Players returns an iterator that yields players currently online. If Players
// is called from within a transaction, the respective transaction should be
// passed. Passing nil is otherwise valid. Each loop body runs on the yielded
// player's world owner, so blocking stalls that world and calling world.Call,
// world.CallEntity, world.CallRef, or Task.Wait for the same owner deadlocks.
// Players in other worlds are yielded by blocking on those owners sequentially;
// mirrored handlers in two worlds can therefore deadlock each other. For
// fan-out, collect Player.H values and schedule each with player.Do instead.
// Players returned are only valid within the block of the for loop used to
// iterate over them:
//
//	for p := range srv.Players(nil) {
//	  // p is valid here
//	  go func() {
//	    // p is no longer valid here
//	  }()
//	}
//	// p is no longer valid here
//
// Collecting all values from the iterator using a function such as
// slices.Collect immediately invalidates the players because their transactions
// will be finished. Use Player.H(), player.NewRef, or player.Do when a
// player must be referenced after the iterator callback returns.
func (srv *Server) Players(tx *world.Tx) iter.Seq[*player.Player] {
	srv.pmu.RLock()
	handles := make([]*world.EntityHandle, 0, len(srv.p))
	for _, p := range srv.p {
		handles = append(handles, p.handle)
	}
	srv.pmu.RUnlock()

	return func(yield func(*player.Player) bool) {
		for _, handle := range handles {
			if tx != nil {
				if e, ok := handle.Entity(tx); ok {
					if !yield(e.(*player.Player)) {
						break
					}
					continue
				}
			}
			ret, err := player.Call(context.Background(), handle, func(_ *world.Tx, p *player.Player) (bool, error) {
				return !yield(p), nil
			})
			if err != nil {
				continue
			}
			if ret {
				break
			}
		}
	}
}

// Player looks for a player on the server with the UUID passed. If found, the
// entity handle is returned and the bool returns holds a true value. If not,
// the bool returned is false and the handle is nil.
func (srv *Server) Player(uuid uuid.UUID) (*world.EntityHandle, bool) {
	// A map read cannot panic, so the unlock always ran; the handle field of an
	// onlinePlayer is written before it is put in the map and never changes, so
	// reading it after the unlock returns the same pointer.
	srv.pmu.RLock()
	p, ok := srv.p[uuid]
	srv.pmu.RUnlock()
	if !ok {
		return nil, false
	}
	return p.handle, ok
}

// PlayerByName looks for a player on the server with the name passed. If
// found, the entity handle is returned and the bool returned holds a true
// value. If not, the bool is false and the handle is nil
func (srv *Server) PlayerByName(name string) (*world.EntityHandle, bool) {
	// Server.p is walked rather than indexed, and a walk racing a writer is a
	// concurrent map iteration and map write: a fatal runtime error that no
	// recover reaches. A player joining or leaving is enough to cause it.
	srv.pmu.RLock()
	defer srv.pmu.RUnlock()
	if p, ok := sliceutil.SearchValue(slices.Collect(maps.Values(srv.p)), func(p *onlinePlayer) bool {
		return p.name == name
	}); ok {
		return p.handle, true
	}
	return nil, false
}

// PlayerByXUID looks for a player on the server with the XUID passed. If
// found, the entity handle is returned and the bool returned is true. If no
// player with the XUID was found, nil and false are returned.
func (srv *Server) PlayerByXUID(xuid string) (*world.EntityHandle, bool) {
	// Held for the same reason as in PlayerByName above.
	srv.pmu.RLock()
	defer srv.pmu.RUnlock()
	if p, ok := sliceutil.SearchValue(slices.Collect(maps.Values(srv.p)), func(p *onlinePlayer) bool {
		return p.xuid == xuid
	}); ok {
		return p.handle, true
	}
	return nil, false
}

// CloseOnProgramEnd closes the server right before the program ends, so that
// all data of the server are saved properly.
func (srv *Server) CloseOnProgramEnd() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		if err := srv.Close(); err != nil {
			srv.conf.Log.Error("close server: " + err.Error())
		}
	}()
}

// Close closes the server, making any call to Run/Accept cancel immediately.
func (srv *Server) Close() error {
	if srv.started.Load() == nil {
		panic("server not yet running")
	}
	srv.once.Do(srv.close)
	return nil
}

// close stops the server, storing player and world data to disk.
func (srv *Server) close() {
	srv.conf.Log.Info("Server closing...")

	// No login is reserved or spawned from here on, and the ones still being
	// set up stop waiting and release their reservation, so the wait below
	// covers only players that are disconnected here or were already leaving.
	// The listeners stay open until the end: closing them closes the
	// connections before the disconnect messages below reach the clients.
	srv.pmu.Lock()
	srv.closing = true
	srv.pmu.Unlock()
	srv.cancelJoins()

	srv.conf.Log.Debug("Disconnecting players...")
	srv.pmu.RLock()
	handles := make([]*world.EntityHandle, 0, len(srv.p))
	for _, p := range srv.p {
		handles = append(handles, p.handle)
	}
	srv.pmu.RUnlock()
	for _, handle := range handles {
		srv.closeStep("disconnect player", func() {
			// An error means the player already left, which is what this asks for.
			_, _ = player.Call(context.Background(), handle, func(_ *world.Tx, p *player.Player) (struct{}, error) {
				p.Disconnect(chat.MessageServerDisconnect.Resolve(p.Locale()))
				return struct{}{}, nil
			})
		})
	}
	srv.pwg.Wait()

	srv.conf.Log.Debug("Closing player provider...")
	srv.closeStep("close player provider", func() {
		if err := srv.conf.PlayerProvider.Close(); err != nil {
			srv.conf.Log.Error("Close player provider: " + err.Error())
		}
	})

	srv.conf.Log.Debug("Closing worlds...")
	for _, w := range []*world.World{srv.end, srv.nether, srv.world} {
		srv.closeStep("close world", func() {
			if err := w.Close(); err != nil {
				srv.conf.Log.Error(fmt.Sprintf("Close dimension %v: ", w.Dimension()) + err.Error())
			}
		})
	}

	srv.conf.Log.Debug("Closing listeners...")
	for _, l := range srv.listeners {
		if err := l.Close(); err != nil {
			srv.conf.Log.Error("Close listener: " + err.Error())
		}
	}
}

// closeStep runs one step of close, logging a panic in it instead of letting it
// end the process.
//
// Which input was the problem: a panic in a plugin's quit handler or player
// provider during shutdown. A world raises a panic from its goroutine again on
// the goroutine waiting on it, which here is close itself, so the process ended
// before the worlds were saved. What is swallowed is that panic, raised by a
// handler or provider the step called; the steps after it still run.
func (srv *Server) closeStep(step string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			srv.conf.Log.Error("panic while closing server", "step", step, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	f()
}

// listen makes the Server listen for new connections from the Listener passed.
// This may be used to listen for players on different interfaces. Note that
// the maximum player count of additional Listeners added is not enforced
// automatically. The limit must be enforced by the Listener.
func (srv *Server) listen(l Listener) {
	wg := new(sync.WaitGroup)
	ctx, cancel := context.WithCancel(srv.joinCtx)
	for {
		c, err := l.Accept()
		if err != nil {
			// Cancel the context so that any call to StartGameContext is
			// cancelled rapidly.
			cancel()
			// First wait until all connections that are being handled are done
			// inserting the player into the channel. Afterwards, when we're
			// sure no more values will be inserted in the players channel, we
			// can return so the player channel can be closed.
			wg.Wait()
			srv.wg.Done()
			return
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			srv.finaliseConn(ctx, c, l)
		}()
	}
}

// startListening starts making the EncodeBlock listener listen, accepting new
// connections from players.
func (srv *Server) startListening() {
	srv.makeBlockEntries()
	srv.makeItemComponents()
	srv.makeDimensionData()

	srv.wg.Add(len(srv.listeners))
	for _, l := range srv.listeners {
		go srv.listen(l)
	}
}

// makeBlockEntries initialises the server's block components map using the
// registered custom blocks. It allows block components to be created only once
// at startup.
func (srv *Server) makeBlockEntries() {
	custom, dataDriven := slices.Collect(maps.Values(srv.conf.Blocks.CustomBlocks())), world.DataDrivenBlocks()
	srv.customBlocks = make([]protocol.BlockEntry, 0, len(dataDriven)+len(custom))
	for _, b := range dataDriven {
		srv.customBlocks = append(srv.customBlocks, protocol.BlockEntry{Name: b.Name, Properties: b.Components})
	}

	for i, b := range custom {
		name, _ := b.EncodeBlock()
		srv.customBlocks = append(srv.customBlocks, protocol.BlockEntry{
			Name:       name,
			Properties: blockinternal.Components(name, b, 10000+int32(i)),
		})
	}
}

// makeItemComponents initialises the server's item components map using the
// registered custom items. It allows item components to be created only once
// at startup
func (srv *Server) makeItemComponents() {
	custom := world.CustomItems()
	srv.customItems = make([]protocol.ItemEntry, len(custom))

	for i, it := range custom {
		name, _ := it.EncodeItem()
		rid, _, _ := world.ItemRuntimeID(it)
		_, isCustomBlock := it.(world.CustomBlock)
		var entryVersion int32 = protocol.ItemEntryVersionDataDriven
		if isCustomBlock {
			entryVersion = protocol.ItemEntryVersionNone
		}
		srv.customItems[i] = protocol.ItemEntry{
			Name:           name,
			ComponentBased: !isCustomBlock,
			RuntimeID:      int16(rid),
			Version:        entryVersion,
			Data:           iteminternal.Components(it),
		}
	}
}

// makeDimensionData initialises the server's custom dimensions list.
func (srv *Server) makeDimensionData() {
	dimensions := world.CustomDimensions()
	srv.customDimensions = make([]protocol.DimensionDefinition, 0, len(dimensions))
	for _, registration := range dimensions {
		r := registration.Dimension.Range()
		srv.customDimensions = append(srv.customDimensions, protocol.DimensionDefinition{
			Name:          registration.Name,
			MinimumY:      int32(r.Min()),
			HeightRange:   int32(r.Height()),
			Generator:     protocol.GeneratorVoid,
			DimensionType: int32(registration.ID),
		})
	}
}

// wait awaits the closing of all Listeners added to the Server through a call
// to listen and closed the players channel once that happens.
func (srv *Server) wait() {
	srv.wg.Wait()
	srv.conf.Log.Info("Server closed.", "uptime", time.Since(*srv.started.Load()).String())
	close(srv.incoming)
}

// finaliseConn finalises the session.Conn passed and adds it to the incoming
// channel.
func (srv *Server) finaliseConn(ctx context.Context, conn session.Conn, l Listener) {
	id := uuid.MustParse(conn.IdentityData().Identity)
	if ok, closing := srv.reserve(id); !ok {
		reason := "Already logged in."
		if closing {
			reason = "Server closing."
		}
		// Discarding the error is safe: the connection is being torn down
		// already, and the reason is recorded in the debug log beside it.
		_ = l.Disconnect(conn, reason)
		srv.conf.Log.Debug("spawn failed: "+reason, "raddr", conn.RemoteAddr())
		return
	}
	data := srv.defaultGameData()

	d, w, err := srv.conf.PlayerProvider.Load(id, srv.dimension)
	if err != nil && !errors.Is(err, player.ErrNoData) {
		// Which input was the problem: a record that exists but could not be
		// read, such as a corrupt entry or an I/O error. It was treated as no
		// record, and the empty inventory the player then got was saved over
		// it when they left. The join is refused so the record is kept.
		// Discarding the error is safe: the connection is being torn down
		// already, and the cause is logged beside it.
		_ = l.Disconnect(conn, "Failed to load player data.")
		srv.conf.Log.Error("spawn failed: load player data: "+err.Error(), "uuid", id, "raddr", conn.RemoteAddr())
		srv.release(id)
		return
	}
	if err != nil {
		w = srv.world
		d.Position = w.Spawn().Vec3Centre()
		d.GameMode = w.DefaultGameMode()
	}

	data.PlayerPosition = vec64To32(d.Position).Add(mgl32.Vec3{0, 1.62})
	dim, _ := world.DimensionID(w.Dimension())
	data.Dimension = int32(dim)
	data.Yaw, data.Pitch = float32(d.Rotation.Yaw()), float32(d.Rotation.Pitch())

	data.EmoteChatMuted = srv.conf.MuteEmoteChat

	if err := conn.StartGameContext(ctx, data); err != nil {
		_ = l.Disconnect(conn, "Connection timeout.")

		srv.conf.Log.Debug("spawn failed: "+err.Error(), "raddr", conn.RemoteAddr())
		srv.release(id)
		return
	}
	_ = conn.WritePacket(&packet.ItemRegistry{Items: srv.customItems})
	inc := srv.createPlayer(id, conn, d, w)
	select {
	case srv.incoming <- inc:
	case <-ctx.Done():
		// The server is closing and Accept may never take this login, which
		// would leave this goroutine, the listener waiting for it and close
		// waiting on the reservation blocked forever.
		srv.abortJoin(inc, "server closing")
	}
}

// reserve reserves id for a login and reports whether that succeeded. It fails
// while another login of the same UUID is being set up, is online or is still
// having its data saved, and once the server is closing.
//
// Which input was the problem: two logins of one UUID. The "already logged in"
// check looked only at p, which a login enters after its data is loaded and
// leaves before that data is saved. A second login loaded the data of a session
// that was about to save over it, and spawned with the items the first session
// had since given away; two logins at once both passed and spawned from the
// same data. Reserving before the load and releasing after the save closes both.
func (srv *Server) reserve(id uuid.UUID) (ok, closing bool) {
	srv.pmu.Lock()
	defer srv.pmu.Unlock()
	if srv.closing {
		return false, true
	}
	if _, reserved := srv.reserved[id]; reserved {
		return false, false
	}
	srv.reserved[id] = struct{}{}
	// Added under pmu, which close takes to set closing before it waits, so
	// the counter never goes up after close has started waiting on it.
	srv.pwg.Add(1)
	return true, false
}

// release ends the reservation of id made by reserve.
func (srv *Server) release(id uuid.UUID) {
	srv.pmu.Lock()
	delete(srv.reserved, id)
	srv.pmu.Unlock()
	srv.pwg.Done()
}

// abortJoin tears down a login created by createPlayer that never spawned and
// releases its reservation.
func (srv *Server) abortJoin(inc incoming, reason string) {
	// The entity was never added to a world, so the handle is closed here, and
	// the session is torn down in full: Disconnect only writes a packet, while
	// CloseConnection frees the connection and its goroutines. Closing a handle
	// that is in no world cannot fail in a way that leaves anything to undo.
	_ = inc.p.handle.Close()
	inc.s.Abandon()
	inc.s.Disconnect(reason)
	inc.s.CloseConnection()
	srv.release(inc.p.handle.UUID())
}

// defaultGameData returns a minecraft.GameData as sent for a new player. It
// may later be modified if the player was saved in the player provider of the
// server.
func (srv *Server) defaultGameData() minecraft.GameData {
	gm, _ := world.GameModeID(srv.world.DefaultGameMode())
	return minecraft.GameData{
		// Entity runtime/unique ID for the player itself is always 1 in df.
		EntityUniqueID:  1,
		EntityRuntimeID: 1,

		WorldName:       srv.conf.Name,
		BaseGameVersion: protocol.CurrentVersion,

		Time:       int64(srv.world.Time()),
		Difficulty: 2,

		PlayerGameMode:    int32(gm),
		PlayerPermissions: packet.PermissionLevelMember,
		PlayerPosition:    vec64To32(srv.world.Spawn().Vec3Centre().Add(mgl64.Vec3{0, 1.62})),

		Items:        srv.itemEntries(),
		CustomBlocks: srv.customBlocks,
		GameRules: []protocol.GameRule{
			{Name: "naturalregeneration", Value: false},
			{Name: "locatorBar", Value: false},
		},

		ServerAuthoritativeInventory: true,
		PlayerMovementSettings: protocol.PlayerMovementSettings{
			ServerAuthoritativeBlockBreaking: true,
		},
		Dimensions: srv.customDimensions,
	}
}

// dimension returns a world by a dimension passed.
func (srv *Server) dimension(dimension world.Dimension) *world.World {
	switch dimension {
	default:
		return srv.world
	case world.Nether:
		return srv.nether
	case world.End:
		return srv.end
	}
}

// handleSessionClose handles the closing of a session. It removes the player
// of the session from the server.
func (srv *Server) handleSessionClose(tx *world.Tx, c session.Controllable) {
	// The UUID of a Controllable never changes, so asking for it once gives the
	// same value the four calls returned, and keeps two of them out of the lock.
	id := c.UUID()

	srv.pmu.Lock()
	_, ok := srv.p[id]
	delete(srv.p, id)
	srv.pmu.Unlock()
	if !ok {
		// When a player disconnects immediately after a session is started, it
		// might not be added to the players map yet. This is expected, but we
		// need to be careful not to crash when this happens.
		return
	}

	if tx != nil {
		if err := srv.conf.PlayerProvider.Save(id, c.(*player.Player).Data(), tx.World()); err != nil {
			srv.conf.Log.Error("Save player data: " + err.Error())
		}
	} else {
		srv.conf.Log.Error("Save player data: player's worlds closed before teardown; data not saved", "uuid", id)
	}
	// Released only now, so that a new login of this UUID loads the data just
	// saved rather than what was stored before this session.
	srv.release(id)
}

// createPlayer creates a new player instance using the UUID and connection
// passed. id must be reserved.
func (srv *Server) createPlayer(id uuid.UUID, conn session.Conn, conf player.Config, w *world.World) incoming {
	s := session.Config{
		Log:            srv.conf.Log,
		MaxChunkRadius: srv.conf.MaxChunkRadius,
		EmoteChatMuted: srv.conf.MuteEmoteChat,
		JoinMessage:    srv.conf.JoinMessage,
		QuitMessage:    srv.conf.QuitMessage,
		HandleStop:     srv.handleSessionClose,
		BlockRegistry:  w.BlockRegistry(),
	}.New(conn)

	// Both getters return a copy of a field of conn that nothing changes here,
	// so fetching each once copies the same data the repeated calls did.
	identity, client := conn.IdentityData(), conn.ClientData()

	conf.Name = identity.DisplayName
	conf.XUID = identity.XUID
	conf.UUID = id
	conf.Locale, _ = language.Parse(strings.Replace(client.LanguageCode, "_", "-", 1))
	conf.Skin = srv.parseSkin(client)
	conf.Session = s

	handle := world.EntitySpawnOpts{Position: conf.Position, ID: id}.New(player.Type, conf)
	s.SetHandle(handle, conf.Skin)
	return incoming{s: s, w: w, conf: conf, p: &onlinePlayer{name: conf.Name, xuid: conf.XUID, handle: handle}}
}

// createWorld loads a world with a specific dimension using the provider set
// in the Config. The nether and end dimensions point to the worlds that players
// are moved to when passing through the respective portals.
func (srv *Server) createWorld(dim world.Dimension, nether, end **world.World) *world.World {
	logger := srv.conf.Log.With("dimension", strings.ToLower(fmt.Sprint(dim)))
	logger.Debug("Loading dimension...")

	conf := world.Config{
		Log:                 logger,
		Dim:                 dim,
		Provider:            srv.conf.WorldProvider,
		Generator:           srv.conf.Generator(dim),
		RandomTickSpeed:     srv.conf.RandomTickSpeed,
		ReadOnly:            srv.conf.ReadOnlyWorld,
		SaveInterval:        srv.conf.SaveInterval,
		ChunkUnloadInterval: srv.conf.ChunkUnloadInterval,
		ChunkLoadWorkers:    srv.conf.ChunkLoadWorkers,
		Entities:            srv.conf.Entities,
		Blocks:              srv.conf.Blocks,
		Tuning:              srv.conf.Tuning,
		PortalDestination: func(dim world.Dimension) *world.World {
			switch dim {
			case world.Nether:
				return *nether
			case world.End:
				return *end
			default:
				return nil
			}
		},
	}
	w := conf.New()
	logger.Info("Opened dimension.", "name", w.Name())
	return w
}

// parseSkin parses a skin from the login.ClientData and returns it.
func (srv *Server) parseSkin(data login.ClientData) skin.Skin {
	// Gophertunnel guarantees the following values are valid data and are of
	// the correct size.
	skinResourcePatch, _ := base64.StdEncoding.DecodeString(data.SkinResourcePatch)

	playerSkin := skin.New(data.SkinImageWidth, data.SkinImageHeight)
	playerSkin.Persona = data.PersonaSkin
	playerSkin.Pix, _ = base64.StdEncoding.DecodeString(data.SkinData)
	playerSkin.Model, _ = base64.StdEncoding.DecodeString(data.SkinGeometry)
	playerSkin.ModelConfig, _ = skin.DecodeModelConfig(skinResourcePatch)
	playerSkin.PlayFabID = data.PlayFabID
	playerSkin.FullID = data.SkinID

	playerSkin.Cape = skin.NewCape(data.CapeImageWidth, data.CapeImageHeight)
	playerSkin.Cape.Pix, _ = base64.StdEncoding.DecodeString(data.CapeData)

	if n := len(data.AnimatedImageData); n > 0 {
		// Presized to the number of animations appended below. Left nil when
		// there are none, exactly as the append loop did.
		playerSkin.Animations = make([]skin.Animation, 0, n)
	}
	for _, animation := range data.AnimatedImageData {
		var t skin.AnimationType
		switch animation.Type {
		case protocol.SkinAnimationHead:
			t = skin.AnimationHead
		case protocol.SkinAnimationBody32x32:
			t = skin.AnimationBody32x32
		case protocol.SkinAnimationBody128x128:
			t = skin.AnimationBody128x128
		}

		anim := skin.NewAnimation(animation.ImageWidth, animation.ImageHeight, animation.AnimationExpression, t)
		anim.FrameCount = int(animation.Frames)
		anim.Pix, _ = base64.StdEncoding.DecodeString(animation.Image)

		playerSkin.Animations = append(playerSkin.Animations, anim)
	}

	return playerSkin
}

// vec64To32 converts a mgl64.Vec3 to a mgl32.Vec3.
func vec64To32(vec3 mgl64.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{float32(vec3[0]), float32(vec3[1]), float32(vec3[2])}
}

// itemEntries loads a list of all custom item entries of the server, ready to
// be sent in the StartGame packet.
func (srv *Server) itemEntries() []protocol.ItemEntry {
	// Room for the custom entries too, so the append below never has to grow and
	// copy the slice. Both loops still append in the same order.
	entries := make([]protocol.ItemEntry, 0, len(vanillaItems)+len(srv.customItems))

	for name, e := range vanillaItems {
		entries = append(entries, protocol.ItemEntry{
			Name:           name,
			RuntimeID:      int16(e.RuntimeID),
			ComponentBased: e.ComponentBased,
			Version:        e.Version,
			Data:           e.Data,
		})
	}
	entries = append(entries, srv.customItems...)
	return entries
}

var (
	//go:embed world/vanilla_items.nbt
	vanillaItemsData []byte
	vanillaItems     = map[string]struct {
		RuntimeID      int32          `nbt:"runtime_id"`
		ComponentBased bool           `nbt:"component_based"`
		Version        int32          `nbt:"version"`
		Data           map[string]any `nbt:"data,omitempty"`
	}{}
)

// init reads all item entries from the resource JSON, and sets the according
// values in the runtime ID maps.
func init() {
	_ = nbt.Unmarshal(vanillaItemsData, &vanillaItems)
}
