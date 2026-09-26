package session

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/player/hud"
	"github.com/jv2w/Impulse/server/player/input"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The state machines below live on the Session itself rather than on the wire,
// but the packet path reads them every tick, so their edges are pinned here.

// TestSessionOwnerStopped pins which errors mean the session's player is gone
// and its goroutines should stop quietly. Anything else is a real failure and
// must be reported.
func TestSessionOwnerStopped(t *testing.T) {
	stopped := []error{
		world.ErrEntityClosed,
		world.ErrWorldClosed,
		world.ErrTaskCancelled,
	}
	for _, err := range stopped {
		if !sessionOwnerStopped(err) {
			t.Fatalf("sessionOwnerStopped(%v) = false, want true", err)
		}
		// The check is errors.Is, so a wrapped sentinel counts too.
		if !sessionOwnerStopped(fmt.Errorf("handle packet: %w", err)) {
			t.Fatalf("sessionOwnerStopped of a wrapped %v = false, want true", err)
		}
	}

	other := []error{
		nil,
		errors.New("some other failure"),
		errSelfRuntimeID,
	}
	for _, err := range other {
		if sessionOwnerStopped(err) {
			t.Fatalf("sessionOwnerStopped(%v) = true, want false", err)
		}
	}
}

// TestNextWindowID pins the window ID cycle. Container window IDs are a byte
// the client echoes back, and the server hands them out in the range 1..99.
func TestNextWindowID(t *testing.T) {
	s := newStubSession(t, nil)

	for want := byte(1); want <= 99; want++ {
		if got := s.nextWindowID(); got != want {
			t.Fatalf("window ID %d in the cycle = %d, want %d", want, got, want)
		}
	}
	// After 99 the counter wraps back onto 1 rather than reaching 100.
	if got := s.nextWindowID(); got != 1 {
		t.Fatalf("the window ID after 99 = %d, want the wrapped 1", got)
	}
	if got := s.nextWindowID(); got != 2 {
		t.Fatalf("the window ID after the wrap = %d, want 2", got)
	}
}

// TestNextWindowIDIsConcurrencySafe pins that the counter is atomic. It exists
// so that -race has something to observe on this path.
func TestNextWindowIDIsConcurrencySafe(t *testing.T) {
	s := newStubSession(t, nil)

	var wg sync.WaitGroup
	ids := make([]byte, 50)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i] = s.nextWindowID()
		}()
	}
	wg.Wait()

	slices.Sort(ids)
	for i, id := range ids {
		if int(id) != i+1 {
			t.Fatalf("window IDs = %v, want 1..50 with no repeats", ids)
		}
	}
}

// TestInputLocks pins the input lock bitmask: locks OR together, unlocking
// clears only the named bit, and clearing drops everything.
func TestInputLocks(t *testing.T) {
	s := newStubSession(t, nil)

	if s.InputLocked(input.Jump()) {
		t.Fatal("a fresh session reports the jump lock as applied")
	}

	s.LockInput(input.Jump())
	s.LockInput(input.Sneak())
	if !s.InputLocked(input.Jump()) || !s.InputLocked(input.Sneak()) {
		t.Fatal("locking jump and sneak did not apply both")
	}
	if s.InputLocked(input.Camera()) {
		t.Fatal("locking jump and sneak also applied the camera lock")
	}

	// Locking twice is a no-op rather than a counter.
	s.LockInput(input.Jump())
	s.UnlockInput(input.Jump())
	if s.InputLocked(input.Jump()) {
		t.Fatal("a single unlock did not clear a doubly applied lock")
	}
	if !s.InputLocked(input.Sneak()) {
		t.Fatal("unlocking jump also cleared sneak")
	}

	// Unlocking something that was never locked is a no-op.
	s.UnlockInput(input.Camera())
	if !s.InputLocked(input.Sneak()) {
		t.Fatal("unlocking an unapplied lock cleared an applied one")
	}

	s.ClearInputLocks()
	for _, l := range input.All() {
		if s.InputLocked(l) {
			t.Fatalf("lock %v survived ClearInputLocks", l)
		}
	}
}

// TestSendInputLocks pins that the lock word is sent verbatim, so the packet
// carries the OR of every applied lock.
func TestSendInputLocks(t *testing.T) {
	s := newStubSession(t, nil)
	s.LockInput(input.Jump())
	s.LockInput(input.Sneak())

	s.SendInputLocks()

	got := onlyPacket(t, s).(*packet.UpdateClientInputLocks)
	want := input.Jump().Uint32() | input.Sneak().Uint32()
	if got.Locks != want {
		t.Fatalf("locks = %#b, want %#b", got.Locks, want)
	}
}

// TestHudElementShowHideIsAnUpdateQueue pins the HUD state machine. What is
// tracked is not the element's visibility but the pending change to it, so
// showing an element that is already visible queues nothing.
func TestHudElementShowHideIsAnUpdateQueue(t *testing.T) {
	s := newStubSession(t, nil)
	s.hudUpdates = map[hud.Element]bool{}
	s.hiddenHud = map[hud.Element]struct{}{}
	e := hud.Health()

	if s.HudElementHidden(e) {
		t.Fatal("a fresh session reports health as hidden")
	}

	// Showing an already visible element queues nothing at all.
	s.ShowHudElement(e)
	if len(s.hudUpdates) != 0 {
		t.Fatalf("showing a visible element queued %v", s.hudUpdates)
	}

	s.HideHudElement(e)
	if !s.HudElementHidden(e) {
		t.Fatal("an element queued for hiding is not reported as hidden")
	}

	// Showing it again cancels the pending hide rather than queueing a show.
	s.ShowHudElement(e)
	if len(s.hudUpdates) != 0 {
		t.Fatalf("show did not cancel the pending hide: %v", s.hudUpdates)
	}
	if s.HudElementHidden(e) {
		t.Fatal("the element is still reported as hidden after the hide was cancelled")
	}
}

// TestSendHudUpdatesAppliesTheQueue pins that flushing the queue moves elements
// into the hidden set and empties the queue, and that a second flush with
// nothing pending writes no packet.
func TestSendHudUpdatesAppliesTheQueue(t *testing.T) {
	s := newStubSession(t, nil)
	s.hudUpdates = map[hud.Element]bool{}
	s.hiddenHud = map[hud.Element]struct{}{}

	s.HideHudElement(hud.Health())
	s.HideHudElement(hud.Hunger())
	s.SendHudUpdates()

	if len(s.hudUpdates) != 0 {
		t.Fatalf("the update queue survived the flush: %v", s.hudUpdates)
	}
	if len(s.hiddenHud) != 2 {
		t.Fatalf("hidden set = %v, want two elements", s.hiddenHud)
	}
	if !s.HudElementHidden(hud.Health()) {
		t.Fatal("health is not hidden after the flush")
	}
	if pks := sentPackets(t, s); len(pks) == 0 {
		t.Fatal("the flush wrote no packet")
	}

	s.SendHudUpdates()
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("an empty flush wrote %d packets: %#v", len(pks), pks)
	}

	// Showing a hidden element queues a show, which the next flush applies.
	s.ShowHudElement(hud.Health())
	s.SendHudUpdates()
	if s.HudElementHidden(hud.Health()) {
		t.Fatal("health is still hidden after being shown and flushed")
	}
	if _, ok := s.hiddenHud[hud.Hunger()]; !ok {
		t.Fatal("hunger stopped being hidden when health was shown")
	}
}

// TestEntityRuntimeIDLookups pins the two directions of the runtime ID map and
// what each reports for an entity the session has never seen.
func TestEntityRuntimeIDLookups(t *testing.T) {
	s := newStubSession(t, nil)
	known := newStubEntity(t, stubEntityType{name: "impulse:known"})
	unknown := newStubEntity(t, stubEntityType{name: "impulse:unknown"})
	s.entityRuntimeIDs[known.H()] = 5
	s.entities[5] = known.H()

	if got := s.entityRuntimeID(known); got != 5 {
		t.Fatalf("entityRuntimeID = %d, want 5", got)
	}
	if got := s.entityRuntimeID(unknown); got != 0 {
		t.Fatalf("entityRuntimeID of an unknown entity = %d, want the sentinel 0", got)
	}

	if h, ok := s.entityFromRuntimeID(5); !ok || h != known.H() {
		t.Fatalf("entityFromRuntimeID(5) = %v, %v; want the known handle, true", h, ok)
	}
	if h, ok := s.entityFromRuntimeID(6); ok || h != nil {
		t.Fatalf("entityFromRuntimeID(6) = %v, %v; want nil, false", h, ok)
	}

	if !s.viewingEntity(known.H()) {
		t.Fatal("viewingEntity reported false for a known handle")
	}
	if s.viewingEntity(unknown.H()) {
		t.Fatal("viewingEntity reported true for an unknown handle")
	}
}

// TestEntityHidden pins the explicit hide set, which is keyed by UUID rather
// than by handle.
func TestEntityHidden(t *testing.T) {
	s := newStubSession(t, nil)
	e := newStubEntity(t, stubEntityType{name: "impulse:hidden"})

	if s.entityHidden(e) {
		t.Fatal("a fresh session reports an entity as hidden")
	}
	s.hiddenEntities[e.H().UUID()] = struct{}{}
	if !s.entityHidden(e) {
		t.Fatal("an entity in the hidden set is not reported as hidden")
	}

	other := newStubEntity(t, stubEntityType{name: "impulse:other"})
	if s.entityHidden(other) {
		t.Fatal("hiding one entity hid another")
	}
}

// TestViewLayerMethodsAreNoOpsWithoutALayer pins that every view layer method
// tolerates a Session that has no layer. Config.New always installs one, but a
// Session built any other way -- Nop included -- has a nil layer.
func TestViewLayerMethodsAreNoOpsWithoutALayer(t *testing.T) {
	s := newStubSession(t, nil)
	e := newStubEntity(t, stubEntityType{name: "impulse:plain"})

	if s.ViewLayer() != nil {
		t.Fatal("a session built without a layer reported one")
	}
	s.ViewNameTag(e, "Steve")
	s.ViewPublicNameTag(e)
	s.ViewAlwaysShowNameTag(e, true)
	s.ViewPublicAlwaysShowNameTag(e)
	s.ViewScoreTag(e, "1")
	s.ViewPublicScoreTag(e)
	s.ViewVisibility(e, world.EnforceInvisible())
	s.RemoveViewLayer(e)

	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("the view layer no-ops wrote %d packets: %#v", len(pks), pks)
	}
}

// TestViewLayerEntityChangedSkipsEntitiesNotBeingViewed pins that a metadata
// refresh is dropped for an entity the session cannot see, either because it is
// hidden or because it has no runtime ID.
func TestViewLayerEntityChangedSkipsEntitiesNotBeingViewed(t *testing.T) {
	s := newStubSession(t, nil)
	e := newStubEntity(t, stubEntityType{name: "impulse:plain"})

	s.ViewLayerEntityChanged(e)
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("an entity with no runtime ID still refreshed: %#v", pks)
	}

	s.entityRuntimeIDs[e.H()] = 4
	s.hiddenEntities[e.H().UUID()] = struct{}{}
	s.ViewLayerEntityChanged(e)
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("a hidden entity still refreshed: %#v", pks)
	}

	delete(s.hiddenEntities, e.H().UUID())
	s.ViewLayerEntityChanged(e)
	got := onlyPacket(t, s).(*packet.SetActorData)
	if got.EntityRuntimeID != 4 {
		t.Fatalf("refresh named runtime ID %d, want 4", got.EntityRuntimeID)
	}
}

// TestCloseWindow pins the container close path: closing an open window sends a
// server-side close and reports true, while closing nothing reports false and
// sends nothing.
func TestCloseWindow(t *testing.T) {
	s := newStubSession(t, nil)

	if s.closeWindow(false) {
		t.Fatal("closing with no container open reported a close")
	}
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("closing nothing wrote %d packets: %#v", len(pks), pks)
	}

	s.containerOpened.Store(true)
	s.openedWindowID.Store(7)
	s.openedContainerID.Store(3)

	if !s.closeWindow(false) {
		t.Fatal("closing an open container reported no close")
	}
	got := onlyPacket(t, s).(*packet.ContainerClose)
	if got.WindowID != 7 || got.ContainerType != 3 || !got.ServerSide {
		t.Fatalf("close packet = %#v, want window 7, type 3, server side", got)
	}
	if s.openedContainerID.Load() != 0 {
		t.Fatalf("container ID = %d after close, want 0", s.openedContainerID.Load())
	}
	if s.containerOpened.Load() {
		t.Fatal("the container is still marked open after closing")
	}
}

// TestCloseWindowClientRequestedSendsNothing pins that a close the client asked
// for is not echoed back to it.
func TestCloseWindowClientRequestedSendsNothing(t *testing.T) {
	s := newStubSession(t, nil)
	s.containerOpened.Store(true)
	s.openedWindowID.Store(7)

	if !s.closeWindow(true) {
		t.Fatal("closing an open container reported no close")
	}
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("a client requested close wrote %d packets: %#v", len(pks), pks)
	}
}

// TestRegisterHandlersShape pins the packet dispatch table: which packet IDs
// the session accepts, and which of them are explicitly registered as
// unhandled. handlePacket treats a missing key and a nil value differently, so
// the distinction is part of the contract.
func TestRegisterHandlersShape(t *testing.T) {
	s := newStubSession(t, nil)
	s.registerHandlers()

	explicitlyUnhandled := []uint32{
		packet.IDActorEvent,
		packet.IDAdventureSettings,
		packet.IDAnimate,
		packet.IDAnvilDamage,
		packet.IDBossEvent,
		packet.IDEmoteList,
		packet.IDFilterText,
		packet.IDMovePlayer,
		packet.IDSetPlayerFurnaceOptions,
		packet.IDSetPlayerInventoryOptions,
	}
	for _, id := range explicitlyUnhandled {
		h, ok := s.handlers[id]
		if !ok {
			t.Fatalf("packet ID %d is not in the handler table at all", id)
		}
		if h != nil {
			t.Fatalf("packet ID %d has handler %T, want an explicit nil", id, h)
		}
	}

	handled := []uint32{
		packet.IDBlockActorData,
		packet.IDBlockPickRequest,
		packet.IDBookEdit,
		packet.IDClientCacheBlobStatus,
		packet.IDCommandRequest,
		packet.IDContainerClose,
		packet.IDEmote,
		packet.IDInteract,
		packet.IDInventoryTransaction,
		packet.IDItemStackRequest,
		packet.IDLecternUpdate,
		packet.IDMobEquipment,
		packet.IDModalFormResponse,
		packet.IDNPCRequest,
		packet.IDPlayerAction,
		packet.IDPlayerAuthInput,
		packet.IDPlayerSkin,
		packet.IDRequestAbility,
		packet.IDRequestChunkRadius,
		packet.IDRespawn,
		packet.IDSubChunkRequest,
		packet.IDText,
		packet.IDServerBoundLoadingScreen,
		packet.IDServerBoundDiagnostics,
	}
	for _, id := range handled {
		if h, ok := s.handlers[id]; !ok || h == nil {
			t.Fatalf("packet ID %d has no handler (present: %v)", id, ok)
		}
	}

	if got, want := len(s.handlers), len(explicitlyUnhandled)+len(handled); got != want {
		t.Fatalf("handler table holds %d entries, want %d: a packet was added or removed", got, want)
	}
}

// TestChunkRadiusAndChangingDimensionAreFieldReads pins the two accessors the
// world package polls, including that they answer on a session that has never
// been through Config.New.
func TestChunkRadiusAndChangingDimension(t *testing.T) {
	s := newStubSession(t, nil)
	if got := s.ChunkRadius(); got != 0 {
		t.Fatalf("chunk radius = %d, want 0", got)
	}
	if s.ChangingDimension() {
		t.Fatal("a fresh session reports it is changing dimension")
	}

	s.chunkRadius = 12
	s.changingDimension.Store(true)
	if got := s.ChunkRadius(); got != 12 {
		t.Fatalf("chunk radius = %d, want 12", got)
	}
	if !s.ChangingDimension() {
		t.Fatal("ChangingDimension = false after it was set")
	}
}

// TestNopSwallowsPackets pins the Nop sentinel: it has no packet channel, so
// writePacket must return before touching it. Anything that reaches the select
// would block forever on two nil channels.
func TestNopSwallowsPackets(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		Nop.SendMessage("hello")
		Nop.SendTitle("title")
		Nop.RemoveBossBar()
	}()
	<-done
}

// TestSelfEntityRuntimeIDIsGolden pins the reserved runtime ID for a session's
// own player. Handlers reject any other value for packet fields that refer to
// the player itself.
func TestSelfEntityRuntimeIDIsGolden(t *testing.T) {
	if selfEntityRuntimeID != 1 {
		t.Fatalf("selfEntityRuntimeID = %d, want 1", selfEntityRuntimeID)
	}
	if errSelfRuntimeID == nil {
		t.Fatal("errSelfRuntimeID is nil")
	}
	if got := errSelfRuntimeID.Error(); got != "invalid entity runtime ID: runtime ID for self must always be 1" {
		t.Fatalf("errSelfRuntimeID = %q, want the pinned message", got)
	}
}

// TestUUIDHelpersAreNotUsedForHiddenEntities keeps the uuid import honest and
// pins that an entity's UUID is what the hidden set is keyed by, not its
// handle pointer.
func TestHiddenEntitiesAreKeyedByUUID(t *testing.T) {
	s := newStubSession(t, nil)
	id := uuid.New()
	s.hiddenEntities[id] = struct{}{}

	if _, ok := s.hiddenEntities[uuid.New()]; ok {
		t.Fatal("an unrelated UUID matched the hidden set")
	}
	if _, ok := s.hiddenEntities[id]; !ok {
		t.Fatal("the stored UUID did not match the hidden set")
	}
}
