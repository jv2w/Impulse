package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/player/skin"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// PINNED CONSTRAINT: `sessions` is a package-level *sessionList mutated by
// SetHandle and Session.close. Every test here builds its own &sessionList{}
// instead, so the tests describe the type's behaviour, not the global's state.
// Nothing here touches `sessions`.

// newListSession builds a Session that a sessionList can hold: it needs a
// handle, a connection to read the display name from, and a packet channel.
func newListSession(t *testing.T, name string) *Session {
	t.Helper()
	s := newStubSession(t, &stubConn{
		identity: login.IdentityData{DisplayName: name, XUID: "xuid-" + name},
	})
	s.ent = newStubEntity(t, stubEntityType{name: "impulse:player"}).H()
	return s
}

// TestSessionListAddFirstSessionSeesItself pins that the first session added is
// shown to itself with runtime ID 1, the reserved ID for the session's own
// player.
func TestSessionListAddFirstSessionSeesItself(t *testing.T) {
	l := &sessionList{}
	s := newListSession(t, "Steve")

	l.Add(s)

	got := onlyPacket(t, s).(*packet.PlayerList)
	if got.Entries[0].ActionType != protocol.PlayerListActionAdd {
		t.Fatalf("action = %d, want add %d", got.Entries[0].ActionType, protocol.PlayerListActionAdd)
	}
	if got.Entries[0].EntityUniqueID != selfEntityRuntimeID {
		t.Fatalf("own entity unique ID = %d, want %d", got.Entries[0].EntityUniqueID, selfEntityRuntimeID)
	}
	if got.Entries[0].Username != "Steve" {
		t.Fatalf("username = %q, want Steve", got.Entries[0].Username)
	}
	if got.Entries[0].UUID != s.ent.UUID() {
		t.Fatalf("uuid = %v, want %v", got.Entries[0].UUID, s.ent.UUID())
	}
	if got, want := s.entityRuntimeIDs[s.ent], uint64(selfEntityRuntimeID); got != want {
		t.Fatalf("own runtime ID = %d, want %d", got, want)
	}
}

// TestSessionListAddSecondSessionIsMutual pins the cross-registration: adding a
// second session shows each existing session to it and it to each of them,
// before it is shown to itself.
func TestSessionListAddSecondSessionIsMutual(t *testing.T) {
	l := &sessionList{}
	first := newListSession(t, "Steve")
	l.Add(first)
	sentPackets(t, first)

	second := newListSession(t, "Alex")
	l.Add(second)

	// The first session is told about the second only.
	firstPks := sentPackets(t, first)
	if len(firstPks) != 1 {
		t.Fatalf("the existing session got %d packets, want 1: %#v", len(firstPks), firstPks)
	}
	entry := firstPks[0].(*packet.PlayerList).Entries[0]
	if entry.Username != "Alex" {
		t.Fatalf("existing session was told about %q, want Alex", entry.Username)
	}
	if entry.EntityUniqueID != selfEntityRuntimeID+1 {
		t.Fatalf("Alex's runtime ID on Steve's session = %d, want %d", entry.EntityUniqueID, selfEntityRuntimeID+1)
	}

	// The new session is told about the first, then about itself.
	secondPks := sentPackets(t, second)
	if len(secondPks) != 2 {
		t.Fatalf("the new session got %d packets, want 2: %#v", len(secondPks), secondPks)
	}
	if got := secondPks[0].(*packet.PlayerList).Entries[0]; got.Username != "Steve" {
		t.Fatalf("new session was first told about %q, want Steve", got.Username)
	}
	if got := secondPks[1].(*packet.PlayerList).Entries[0]; got.Username != "Alex" {
		t.Fatalf("new session was then told about %q, want itself", got.Username)
	}
	if got := secondPks[1].(*packet.PlayerList).Entries[0].EntityUniqueID; got != selfEntityRuntimeID {
		t.Fatalf("the new session's own runtime ID = %d, want %d", got, selfEntityRuntimeID)
	}
}

// TestSessionListRuntimeIDsIncrementPerSession pins that every session keeps its
// own runtime ID counter starting at 1, so the same player has a different
// runtime ID on each viewer.
func TestSessionListRuntimeIDsIncrementPerSession(t *testing.T) {
	l := &sessionList{}
	a, b, c := newListSession(t, "A"), newListSession(t, "B"), newListSession(t, "C")
	l.Add(a)
	l.Add(b)
	l.Add(c)

	if got, want := a.entityRuntimeIDs[a.ent], uint64(1); got != want {
		t.Fatalf("A sees itself as %d, want %d", got, want)
	}
	if got, want := a.entityRuntimeIDs[b.ent], uint64(2); got != want {
		t.Fatalf("A sees B as %d, want %d", got, want)
	}
	if got, want := a.entityRuntimeIDs[c.ent], uint64(3); got != want {
		t.Fatalf("A sees C as %d, want %d", got, want)
	}
	if got, want := c.entityRuntimeIDs[a.ent], uint64(2); got != want {
		t.Fatalf("C sees A as %d, want %d", got, want)
	}
	if got, want := c.entityRuntimeIDs[c.ent], uint64(1); got != want {
		t.Fatalf("C sees itself as %d, want %d", got, want)
	}
}

// TestSessionListAddTwiceDuplicates pins that the list does not deduplicate.
//
// PINNED BUG: Add has no membership check, so adding the same session twice
// leaves it in the slice twice and resends its own player list entry three more
// times. The runtime ID is unaffected only because sendSessionTo short-circuits
// when the sender and receiver are the same session. Pinned as-is; decide in
// stage 1.
func TestSessionListAddTwiceDuplicates(t *testing.T) {
	l := &sessionList{}
	s := newListSession(t, "Steve")

	l.Add(s)
	if got := len(sentPackets(t, s)); got != 1 {
		t.Fatalf("the first Add wrote %d packets, want 1", got)
	}

	l.Add(s)

	if len(l.s) != 2 {
		t.Fatalf("list holds %d entries, want the duplicated 2", len(l.s))
	}
	if got := len(sentPackets(t, s)); got != 3 {
		t.Fatalf("the second Add wrote %d packets, want the redundant 3", got)
	}
	if got, want := s.entityRuntimeIDs[s.ent], uint64(selfEntityRuntimeID); got != want {
		t.Fatalf("own runtime ID = %d, want %d", got, want)
	}
	if got, want := s.currentEntityRuntimeID, uint64(selfEntityRuntimeID); got != want {
		t.Fatalf("runtime ID counter = %d, want %d", got, want)
	}
}

// TestSessionListRemove pins that removing a session tells every session in the
// list -- including the removed one -- to drop it, and clears the runtime ID
// mappings on the remaining ones.
func TestSessionListRemove(t *testing.T) {
	l := &sessionList{}
	a, b := newListSession(t, "A"), newListSession(t, "B")
	l.Add(a)
	l.Add(b)
	sentPackets(t, a)
	sentPackets(t, b)

	l.Remove(b, nil)

	if len(l.s) != 1 || l.s[0] != a {
		t.Fatalf("list = %#v, want only A", l.s)
	}
	pks := sentPackets(t, a)
	if len(pks) != 1 {
		t.Fatalf("A got %d packets, want 1: %#v", len(pks), pks)
	}
	entry := pks[0].(*packet.PlayerList).Entries[0]
	if entry.ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("action = %d, want remove %d", entry.ActionType, protocol.PlayerListActionRemove)
	}
	if entry.UUID != b.ent.UUID() {
		t.Fatalf("removed uuid = %v, want %v", entry.UUID, b.ent.UUID())
	}
	if _, ok := a.entityRuntimeIDs[b.ent]; ok {
		t.Fatal("A still maps B's handle to a runtime ID")
	}
	if _, ok := a.entities[2]; ok {
		t.Fatal("A still maps runtime ID 2 to a handle")
	}

	// The removed session is told about its own removal too.
	if len(sentPackets(t, b)) != 1 {
		t.Fatal("the removed session was not told about its own removal")
	}
}

// TestSessionListRemoveUnknownSession pins that removing a session that was
// never added still notifies everyone and leaves the list alone.
//
// PINNED BUG: Remove loops over every session in the list unconditionally, so
// removing a stranger broadcasts a player list removal for a UUID no viewer has
// ever seen. Pinned as-is; decide in stage 1.
func TestSessionListRemoveUnknownSession(t *testing.T) {
	l := &sessionList{}
	a := newListSession(t, "A")
	l.Add(a)
	sentPackets(t, a)

	stranger := newListSession(t, "Stranger")
	l.Remove(stranger, nil)

	if len(l.s) != 1 {
		t.Fatalf("list holds %d entries, want 1", len(l.s))
	}
	pks := sentPackets(t, a)
	if len(pks) != 1 {
		t.Fatalf("A got %d packets, want the spurious 1: %#v", len(pks), pks)
	}
	if got := pks[0].(*packet.PlayerList).Entries[0].UUID; got != stranger.ent.UUID() {
		t.Fatalf("removal named %v, want %v", got, stranger.ent.UUID())
	}
}

// TestSessionListRemoveFromEmptyList pins that removing from an empty list is a
// no-op rather than a panic.
func TestSessionListRemoveFromEmptyList(t *testing.T) {
	l := &sessionList{}
	s := newListSession(t, "Steve")

	l.Remove(s, nil)

	if len(l.s) != 0 {
		t.Fatalf("list holds %d entries, want 0", len(l.s))
	}
	if pks := sentPackets(t, s); len(pks) != 0 {
		t.Fatalf("an empty list wrote %d packets: %#v", len(pks), pks)
	}
}

// TestSessionListLookup pins the UUID lookup, which searches by the handle's
// UUID rather than by the session pointer.
func TestSessionListLookup(t *testing.T) {
	l := &sessionList{}
	a, b := newListSession(t, "A"), newListSession(t, "B")
	l.Add(a)
	l.Add(b)

	got, ok := l.Lookup(b.ent.UUID())
	if !ok || got != b {
		t.Fatalf("Lookup(B) = %v, %v; want B, true", got, ok)
	}
	if _, ok := l.Lookup(uuid.New()); ok {
		t.Fatal("Lookup of an unknown UUID reported a hit")
	}
}

// TestSessionListLookupOnEmptyList pins that a lookup against an empty list
// returns a nil session and false rather than panicking on the nil handle
// dereference the search would otherwise do.
func TestSessionListLookupOnEmptyList(t *testing.T) {
	l := &sessionList{}
	if s, ok := l.Lookup(uuid.New()); s != nil || ok {
		t.Fatalf("Lookup on an empty list = %v, %v; want nil, false", s, ok)
	}
}

// TestSessionListLookupPanicsOnASessionWithoutAHandle pins the one input that
// takes the lookup down.
//
// PINNED BUG: Lookup dereferences session.ent without a nil check, so a single
// session in the list that has not been through SetHandle panics the caller.
// Pinned as-is; decide in stage 1.
func TestSessionListLookupPanicsOnASessionWithoutAHandle(t *testing.T) {
	l := &sessionList{s: []*Session{newStubSession(t, nil)}}

	defer func() {
		if recover() == nil {
			t.Fatal("Lookup over a session with a nil handle did not panic")
		}
	}()
	_, _ = l.Lookup(uuid.New())
}

// TestSessionListConcurrentAddAndLookup pins that the list's mutex actually
// covers concurrent use. It exists to give -race something to observe.
func TestSessionListConcurrentAddAndLookup(t *testing.T) {
	l := &sessionList{}
	members := make([]*Session, 8)
	for i := range members {
		members[i] = newListSession(t, string(rune('A'+i)))
	}

	var wg sync.WaitGroup
	for _, s := range members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Add(s)
		}()
	}
	for _, s := range members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = l.Lookup(s.ent.UUID())
		}()
	}
	wg.Wait()

	if len(l.s) != len(members) {
		t.Fatalf("list holds %d entries, want %d", len(l.s), len(members))
	}
}

// TestSkinToProtocol pins the outgoing skin conversion: which protocol field
// each skin field lands in, and the two fallbacks for an unset full ID and an
// empty model.
func TestSkinToProtocol(t *testing.T) {
	s := skin.New(64, 32)
	s.Persona = true
	s.PlayFabID = "playfab"
	s.FullID = "full"
	s.Model = []byte(`{"a":1}`)
	s.ModelConfig = skin.ModelConfig{Default: "geometry.humanoid"}
	s.Cape = skin.NewCape(64, 32)
	for i := range s.Pix {
		s.Pix[i] = byte(i)
	}

	got := skinToProtocol(s)

	if got.PlayFabID != "playfab" {
		t.Fatalf("PlayFabID = %q, want playfab", got.PlayFabID)
	}
	if got.FullID != "full" {
		t.Fatalf("FullID = %q, want full", got.FullID)
	}
	if !got.PersonaSkin {
		t.Fatal("PersonaSkin = false, want true")
	}
	if got.SkinImageWidth != 64 || got.SkinImageHeight != 32 {
		t.Fatalf("skin image = %dx%d, want 64x32", got.SkinImageWidth, got.SkinImageHeight)
	}
	if got.CapeImageWidth != 64 || got.CapeImageHeight != 32 {
		t.Fatalf("cape image = %dx%d, want 64x32", got.CapeImageWidth, got.CapeImageHeight)
	}
	if !reflect.DeepEqual(got.SkinData, s.Pix) {
		t.Fatal("SkinData is not the skin's pixel slice")
	}
	if !reflect.DeepEqual(got.SkinGeometry, []byte(`{"a":1}`)) {
		t.Fatalf("SkinGeometry = %q, want the model bytes", got.SkinGeometry)
	}
	if want := `{"geometry":{"default":"geometry.humanoid"}}`; string(got.SkinResourcePatch) != want {
		t.Fatalf("SkinResourcePatch = %q, want %q", got.SkinResourcePatch, want)
	}
	if !got.Trusted || !got.OverrideAppearance {
		t.Fatalf("Trusted/OverrideAppearance = %v/%v, want both true", got.Trusted, got.OverrideAppearance)
	}
	if string(got.GeometryDataEngineVersion) != protocol.CurrentVersion {
		t.Fatalf("GeometryDataEngineVersion = %q, want %q", got.GeometryDataEngineVersion, protocol.CurrentVersion)
	}
}

// TestSkinToProtocolFallbacks pins the two substitutions made for missing data:
// an empty full ID becomes a fresh UUID and an empty model becomes "{}".
func TestSkinToProtocolFallbacks(t *testing.T) {
	got := skinToProtocol(skin.New(64, 32))

	if _, err := uuid.Parse(got.FullID); err != nil {
		t.Fatalf("FullID = %q, want a generated UUID: %v", got.FullID, err)
	}
	if string(got.SkinGeometry) != "{}" {
		t.Fatalf("SkinGeometry = %q, want the empty object fallback", got.SkinGeometry)
	}
}

// TestSkinToProtocolIDsAreFreshEachCall pins that the skin ID and cape ID are
// generated per call rather than derived from the skin.
//
// PINNED SCOPE: two conversions of the same skin produce different SkinIDs, so
// the client cannot use the ID to cache a skin. Out of scope.
func TestSkinToProtocolIDsAreFreshEachCall(t *testing.T) {
	s := skin.New(64, 32)
	s.FullID = "stable"

	first, second := skinToProtocol(s), skinToProtocol(s)
	if first.SkinID == second.SkinID {
		t.Fatalf("SkinID was stable across calls: %q", first.SkinID)
	}
	if first.CapeID == second.CapeID {
		t.Fatalf("CapeID was stable across calls: %q", first.CapeID)
	}
	if first.FullID != second.FullID {
		t.Fatalf("FullID changed across calls: %q then %q", first.FullID, second.FullID)
	}
}

// TestSkinToProtocolAnimations pins the animation type mapping and that the
// animation's expression is carried through.
func TestSkinToProtocolAnimations(t *testing.T) {
	s := skin.New(64, 32)
	head := skin.NewAnimation(8, 8, 3, skin.AnimationHead)
	body := skin.NewAnimation(16, 16, 0, skin.AnimationBody32x32)
	high := skin.NewAnimation(32, 32, 0, skin.AnimationBody128x128)
	s.Animations = []skin.Animation{head, body, high}

	got := skinToProtocol(s).Animations

	want := []uint32{
		protocol.SkinAnimationHead,
		protocol.SkinAnimationBody32x32,
		protocol.SkinAnimationBody128x128,
	}
	for i, anim := range got {
		if anim.AnimationType != want[i] {
			t.Fatalf("animation %d type = %d, want %d", i, anim.AnimationType, want[i])
		}
		if anim.FrameCount != 1 {
			t.Fatalf("animation %d frame count = %v, want 1", i, anim.FrameCount)
		}
	}
	if got[0].ExpressionType != 3 {
		t.Fatalf("head animation expression = %d, want 3", got[0].ExpressionType)
	}
	if got[0].ImageWidth != 8 || got[0].ImageHeight != 8 {
		t.Fatalf("head animation image = %dx%d, want 8x8", got[0].ImageWidth, got[0].ImageHeight)
	}
}

// validProtocolSkin builds the smallest protocol.Skin that protocolToSkin
// accepts, so that each error test can break exactly one thing.
func validProtocolSkin() protocol.Skin {
	return protocol.Skin{
		SkinID:            "skin-id",
		SkinImageWidth:    64,
		SkinImageHeight:   32,
		SkinData:          make([]byte, 64*32*4),
		SkinGeometry:      []byte(`{}`),
		SkinResourcePatch: []byte(`{"geometry":{"default":"geometry.humanoid"}}`),
	}
}

// TestProtocolToSkin pins the incoming conversion of a well-formed skin.
func TestProtocolToSkin(t *testing.T) {
	sk := validProtocolSkin()
	sk.PersonaSkin = true
	sk.PlayFabID = "playfab"
	sk.FullID = "full"
	sk.CapeImageWidth, sk.CapeImageHeight = 64, 32
	sk.CapeData = make([]byte, 64*32*4)

	got, err := protocolToSkin(sk)
	if err != nil {
		t.Fatalf("protocolToSkin returned %v, want no error", err)
	}
	if got.Bounds().Max.X != 64 || got.Bounds().Max.Y != 32 {
		t.Fatalf("bounds = %v, want 64x32", got.Bounds())
	}
	if !got.Persona || got.PlayFabID != "playfab" || got.FullID != "full" {
		t.Fatalf("skin identity = %v/%q/%q, want true/playfab/full", got.Persona, got.PlayFabID, got.FullID)
	}
	if got.ModelConfig.Default != "geometry.humanoid" {
		t.Fatalf("model config default = %q, want geometry.humanoid", got.ModelConfig.Default)
	}
	if got.Cape.Bounds().Max.X != 64 {
		t.Fatalf("cape bounds = %v, want a 64 wide cape", got.Cape.Bounds())
	}
}

// TestProtocolToSkinErrors pins each rejection, which is what a hostile client
// hits first.
func TestProtocolToSkinErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(sk *protocol.Skin)
		wantSub string
	}{
		{
			name:    "empty skin ID",
			mutate:  func(sk *protocol.Skin) { sk.SkinID = "" },
			wantSub: "SkinID must not be an empty string",
		},
		{
			name:    "geometry is not JSON",
			mutate:  func(sk *protocol.Skin) { sk.SkinGeometry = []byte("not json") },
			wantSub: "SkinGeometry was not a valid JSON string",
		},
		{
			name:    "geometry is JSON but not an object",
			mutate:  func(sk *protocol.Skin) { sk.SkinGeometry = []byte("[1,2]") },
			wantSub: "SkinGeometry was not a valid JSON string",
		},
		{
			name:    "resource patch is not JSON",
			mutate:  func(sk *protocol.Skin) { sk.SkinResourcePatch = []byte("nope") },
			wantSub: "SkinResourcePatch was not a valid JSON string",
		},
		{
			name: "unknown animation type",
			mutate: func(sk *protocol.Skin) {
				sk.Animations = []protocol.SkinAnimation{{AnimationType: 99}}
			},
			wantSub: "invalid animation type: 99",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sk := validProtocolSkin()
			test.mutate(&sk)

			got, err := protocolToSkin(sk)
			if err == nil {
				t.Fatal("protocolToSkin accepted the skin, want an error")
			}
			if !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("error = %q, want it to contain %q", err, test.wantSub)
			}
			if !reflect.DeepEqual(got, skin.Skin{}) {
				t.Fatalf("a rejected skin still returned data: %#v", got)
			}
		})
	}
}

// TestProtocolToSkinErrorsAreNotWrapped pins that the two JSON failures report
// their cause with %v rather than %w, so errors.Is and errors.As cannot see
// through to the json error.
//
// PINNED BUG: a wrapped error must use %w. Pinned as-is; decide in stage 1.
func TestProtocolToSkinErrorsAreNotWrapped(t *testing.T) {
	sk := validProtocolSkin()
	sk.SkinGeometry = []byte("not json")

	_, err := protocolToSkin(sk)
	if err == nil {
		t.Fatal("protocolToSkin accepted invalid geometry")
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("the json error is reachable through errors.Unwrap; the verb-v formatting this pins was fixed")
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		t.Fatal("the json error is reachable through errors.As; the verb-v formatting this pins was fixed")
	}
}

// TestProtocolToSkinPixelDataIsNotValidated pins that the declared dimensions
// and the pixel slice are never checked against each other, so the skin the
// server ends up holding can claim a size its data does not have.
//
// PINNED BUG: skin.New allocates width*height*4 bytes and the very next line
// throws that slice away for the client's SkinData, whatever length it is. A
// later Skin.At within the declared bounds then reads out of range. The
// unbounded make itself is the more serious half: nothing caps the dimensions.
// Pinned as-is; decide in stage 1.
func TestProtocolToSkinPixelDataIsNotValidated(t *testing.T) {
	sk := validProtocolSkin()
	sk.SkinData = []byte{1, 2, 3, 4}

	got, err := protocolToSkin(sk)
	if err != nil {
		t.Fatalf("protocolToSkin rejected mismatched pixel data: %v", err)
	}
	if got.Bounds().Max.X != 64 || got.Bounds().Max.Y != 32 {
		t.Fatalf("bounds = %v, want the declared 64x32", got.Bounds())
	}
	if len(got.Pix) != 4 {
		t.Fatalf("pixel data length = %d, want the client's 4", len(got.Pix))
	}
}
