package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// maxCommandLineLen bounds the bytes of a single command line. A legitimate
// command fits in a line: 4096 bytes is several times that while keeping one
// packet from forcing a megabyte split plus command lookup per send.
const maxCommandLineLen = 4096

// CommandRequestHandler handles the CommandRequest packet.
type CommandRequestHandler struct {
	origin protocol.CommandOrigin
}

// Handle ...
func (h *CommandRequestHandler) Handle(p packet.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*packet.CommandRequest)
	if pk.Internal {
		return fmt.Errorf("command request packet must never have the internal field set to true")
	}
	// Which input was the problem: CommandLine was split and executed with no
	// length check, so one packet allocated a string slice proportional to its
	// size on every send.
	if len(pk.CommandLine) > maxCommandLineLen {
		return fmt.Errorf("command line can not be longer than 4096 bytes")
	}

	h.origin = pk.CommandOrigin
	c.ExecuteCommand(pk.CommandLine)
	return nil
}
