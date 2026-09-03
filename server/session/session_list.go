package session

import (
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/internal/sliceutil"
	"github.com/jv2w/Impulse/server/player/skin"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var sessions = new(sessionList)

type sessionList struct {
	mu sync.Mutex
	s  []*Session
}

func (l *sessionList) Add(s *Session) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, other := range l.s {
		// Show all sessions to the new session and the new session to all
		// existing sessions.
		l.sendSessionTo(s, other)
		l.sendSessionTo(other, s)
	}
	// Show the new session to itself.
	l.sendSessionTo(s, s)
	l.s = append(l.s, s)
}

func (l *sessionList) Remove(s *Session, entity world.Entity) {
	l.mu.Lock()
	removedFrom := slices.Clone(l.s)
	for _, other := range l.s {
		l.unsendSessionFrom(s, other)
	}
	l.s = sliceutil.DeleteVal(l.s, s)
	l.mu.Unlock()

	if entity == nil {
		return
	}
	for _, other := range removedFrom {
		if other.viewLayer != nil {
			other.viewLayer.Remove(entity)
		}
	}
}

func (l *sessionList) Lookup(id uuid.UUID) (*Session, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index := slices.IndexFunc(l.s, func(session *Session) bool {
		return session.ent.UUID() == id
	}); index != -1 {
		return l.s[index], true
	}
	return nil, false
}

func (l *sessionList) sendSessionTo(s, to *Session) {
	runtimeID := uint64(selfEntityRuntimeID)

	to.entityMutex.Lock()
	if s != to {
		to.currentEntityRuntimeID += 1
		runtimeID = to.currentEntityRuntimeID
	}
	to.entityRuntimeIDs[s.ent] = runtimeID
	to.entities[runtimeID] = s.ent
	to.entityMutex.Unlock()

	// IdentityData returns its data by value, so it is read once here instead of twice.
	identity := s.conn.IdentityData()
	to.writePacket(&packet.PlayerList{
		Entries: []protocol.PlayerListEntry{{
			ActionType:     protocol.PlayerListActionAdd,
			UUID:           s.ent.UUID(),
			EntityUniqueID: int64(runtimeID),
			Username:       identity.DisplayName,
			XUID:           identity.XUID,
			BuildPlatform:  int32(protocol.DeviceUnknown),
			Skin:           skinToProtocol(s.joinSkin),
		}},
	})
}

func (l *sessionList) unsendSessionFrom(s, from *Session) {
	from.entityMutex.Lock()
	delete(from.entities, from.entityRuntimeIDs[s.ent])
	delete(from.entityRuntimeIDs, s.ent)
	from.entityMutex.Unlock()

	from.writePacket(&packet.PlayerList{
		Entries: []protocol.PlayerListEntry{{
			ActionType: protocol.PlayerListActionRemove,
			UUID:       s.ent.UUID(),
		}},
	})
}

// skinToProtocol converts a skin to its protocol representation.
func skinToProtocol(s skin.Skin) protocol.Skin {
	var animations []protocol.SkinAnimation
	if len(s.Animations) > 0 {
		// The final length is known, so the slice is allocated once instead of grown. A skin without
		// animations still leaves the field nil, exactly as the plain append did.
		animations = make([]protocol.SkinAnimation, 0, len(s.Animations))
	}
	for _, animation := range s.Animations {
		// Bounds has a value receiver, so it copied the whole animation twice per iteration. It only reads
		// the stored width and height, so one call yields both.
		animBounds := animation.Bounds()
		protocolAnim := protocol.SkinAnimation{
			ImageWidth:  uint32(animBounds.Max.X),
			ImageHeight: uint32(animBounds.Max.Y),
			ImageData:   animation.Pix,
			FrameCount:  float32(animation.FrameCount),
		}
		switch animation.Type() {
		case skin.AnimationHead:
			protocolAnim.AnimationType = protocol.SkinAnimationHead
		case skin.AnimationBody32x32:
			protocolAnim.AnimationType = protocol.SkinAnimationBody32x32
		case skin.AnimationBody128x128:
			protocolAnim.AnimationType = protocol.SkinAnimationBody128x128
		}
		protocolAnim.ExpressionType = uint32(animation.AnimationExpression)
		animations = append(animations, protocolAnim)
	}

	fullID := s.FullID
	if fullID == "" {
		fullID = uuid.New().String()
	}
	model := s.Model
	if len(model) == 0 {
		model = []byte("{}")
	}
	// Bounds has a value receiver on both the skin and the cape, so each call copied the whole struct. Both
	// only read the stored width and height and neither has a side effect, so one call each yields the same
	// numbers and the uuid.New calls below still run in the same order.
	skinBounds, capeBounds := s.Bounds(), s.Cape.Bounds()
	return protocol.Skin{
		PlayFabID:                 s.PlayFabID,
		SkinID:                    uuid.New().String(),
		SkinResourcePatch:         s.ModelConfig.Encode(),
		SkinImageWidth:            uint32(skinBounds.Max.X),
		SkinImageHeight:           uint32(skinBounds.Max.Y),
		SkinData:                  s.Pix,
		CapeImageWidth:            uint32(capeBounds.Max.X),
		CapeImageHeight:           uint32(capeBounds.Max.Y),
		CapeData:                  s.Cape.Pix,
		SkinGeometry:              model,
		PersonaSkin:               s.Persona,
		CapeID:                    uuid.New().String(),
		FullID:                    fullID,
		Animations:                animations,
		Trusted:                   true,
		OverrideAppearance:        true,
		GeometryDataEngineVersion: []byte(protocol.CurrentVersion),
	}
}
