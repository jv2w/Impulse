package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// InteractHandler handles the packet.Interact.
type InteractHandler struct{}

// Handle ...
func (h *InteractHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*packet.Interact)

	switch pk.ActionType {
	case packet.InteractActionMouseOverEntity:
		// We don't need this action.
	case packet.InteractActionOpenInventory:
		if s.invOpened {
			// When there is latency, this might end up being sent multiple times. If we send a ContainerOpen
			// multiple times, the client crashes.
			return nil
		}
		s.invOpened = true
		// The position is only used by the packet below, and reading it has no
		// effect on anything, so it is read on the one path that needs it
		// instead of on every interact packet.
		pos := c.Position()
		s.writePacket(&packet.ContainerOpen{
			WindowID:                0,
			ContainerType:           0xff,
			ContainerEntityUniqueID: -1,
			ContainerPosition: protocol.BlockPos{
				int32(pos[0]),
				int32(pos[1]),
				int32(pos[2]),
			},
		})
	default:
		return fmt.Errorf("unexpected interact packet action %v", pk.ActionType)
	}
	return nil
}
