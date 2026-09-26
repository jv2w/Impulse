package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/player/form"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"sync"
	"sync/atomic"
)

// ModalFormResponseHandler handles the ModalFormResponse packet.
type ModalFormResponseHandler struct {
	mu        sync.Mutex
	forms     map[uint32]form.Form
	currentID atomic.Uint32
}

// maxFormResponseBytes bounds the response JSON of a single ModalFormResponse.
// A legitimate form submits a small JSON array: 65536 bytes is orders of
// magnitude above that while keeping one packet from forcing a megabyte JSON
// decode per send.
const maxFormResponseBytes = 65536

// Handle ...
func (h *ModalFormResponseHandler) Handle(p packet.Packet, _ *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.ModalFormResponse)

	resp, exists := pk.ResponseData.Value()
	// Which input was the problem: ResponseData reached json.Decode with no
	// length check, so one packet forced a decode proportional to its size.
	// The check runs before the form lookup so an unknown ID with huge data is
	// still rejected for its size.
	if exists && len(resp) > maxFormResponseBytes {
		return fmt.Errorf("form response can not be longer than 65536 bytes")
	}
	h.mu.Lock()
	f, ok := h.forms[pk.FormID]
	delete(h.forms, pk.FormID)
	h.mu.Unlock()

	if !ok && !exists {
		// Sometimes the client seems to send a second response with no data, which would cause the player to be kicked
		// by the server. This should patch that.
		return nil
	}
	if !exists || len(resp) == 0 {
		// The form was cancelled: The cross in the top right corner was clicked.
		resp = nil
	}
	if !ok {
		return fmt.Errorf("no form with ID %v currently opened", pk.FormID)
	}
	if err := f.SubmitJSON(resp, c, tx); err != nil {
		return fmt.Errorf("error submitting form data: %w", err)
	}
	return nil
}
