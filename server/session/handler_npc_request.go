package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/player/dialogue"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// NPCRequestHandler handles the NPCRequest packet.
type NPCRequestHandler struct {
	dialogue        dialogue.Dialogue
	entityRuntimeID uint64
}

// Handle ...
func (h *NPCRequestHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.NPCRequest)
	if h.entityRuntimeID == 0 {
		// No dialogue is currently open for this session, so there is nothing to submit or close.
		return nil
	}
	// Which input was the problem: NPCRequest packets sent after the dialogue
	// was closed, or naming another entity. The dialogue was kept after a close
	// and the entity was never checked, so a client could run the buttons of a
	// closed dialogue, such as a plugin's shop or reward, as often as it liked.
	if pk.EntityRuntimeID != h.entityRuntimeID {
		return nil
	}
	switch pk.RequestType {
	case packet.NPCRequestActionExecuteAction:
		if err := h.dialogue.Submit(uint(pk.ActionType), c, tx); err != nil {
			return fmt.Errorf("error submitting dialogue: %w", err)
		}
	case packet.NPCRequestActionExecuteClosingCommands:
		d := h.dialogue
		h.dialogue, h.entityRuntimeID = dialogue.Dialogue{}, 0
		d.Close(c, tx)
	}
	return nil
}
