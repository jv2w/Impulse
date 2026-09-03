package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// TextHandler handles the Text packet.
type TextHandler struct{}

// Handle ...
func (TextHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*packet.Text)

	if pk.TextType != packet.TextTypeChat {
		return fmt.Errorf("TextType should always be Chat (%v), but got %v", packet.TextTypeChat, pk.TextType)
	}
	// IdentityData returns its data by value, so reading it once instead of twice saves a copy of the whole
	// struct. Nothing between the two checks can change what the connection reports.
	identity := s.conn.IdentityData()
	if pk.SourceName != identity.DisplayName {
		return fmt.Errorf("SourceName must be equal to DisplayName")
	}
	if pk.XUID != identity.XUID {
		return fmt.Errorf("XUID must be equal to player's XUID")
	}
	c.Chat(pk.Message)
	return nil
}
