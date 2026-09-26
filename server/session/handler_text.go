package session

import (
	"fmt"
	"unicode/utf8"

	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// maxChatMessageLen bounds the bytes of a single chat message. A legitimate
// message is a line of text: 512 bytes is several times that while keeping
// one packet from filling the global chat log with megabytes per send.
const maxChatMessageLen = 512

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
	// Which input was the problem: pk.Message reached global chat with no
	// length check, so one packet filled the shared log with kilobytes.
	if len(pk.Message) > maxChatMessageLen {
		return fmt.Errorf("chat message can not be longer than 512 bytes")
	}
	if !utf8.ValidString(pk.Message) {
		return fmt.Errorf("chat message must be valid utf8")
	}
	c.Chat(pk.Message)
	return nil
}
