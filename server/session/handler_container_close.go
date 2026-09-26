package session

import (
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ContainerCloseHandler handles the ContainerClose packet.
type ContainerCloseHandler struct{}

// Handle ...
func (h *ContainerCloseHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.ContainerClose)

	// Which input was the problem: any WindowID reached MoveItemsToInventory
	// and the default branch echoed pk.ContainerType verbatim, so a bogus
	// close shuffled crafting-grid/cursor items and confirmed a container
	// type the server never opened. Valid closes take the same move-then-echo
	// path as before; unknown IDs are ignored with no move and no echo.
	var containerType byte
	switch pk.WindowID {
	case 0:
		// Closing of the normal inventory.
		c.MoveItemsToInventory()
		s.invOpened = false
	case byte(s.openedWindowID.Load()):
		c.MoveItemsToInventory()
		containerType = byte(s.openedContainerID.Load())
		s.closeCurrentContainer(tx, true)
	case 0xff:
		// Sent when an inventory/container is opened at the same time as chat.
		c.MoveItemsToInventory()
		s.invOpened = false
		if s.containerOpened.Load() {
			s.closeCurrentContainer(tx, false)
		}
		return nil
	default:
		return nil
	}
	s.writePacket(&packet.ContainerClose{
		WindowID:      pk.WindowID,
		ContainerType: containerType,
	})
	return nil
}
