package session

import (
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/player/form"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type chatControllable struct {
	Controllable
	chatted  int
	commands int
}

func (c *chatControllable) Chat(...any)           { c.chatted++ }
func (c *chatControllable) ExecuteCommand(string) { c.commands++ }

// TestTextRejectsOversizeMessage pins the chat bound: a message far beyond
// anything a legitimate client sends must be rejected before reaching chat.
func TestTextRejectsOversizeMessage(t *testing.T) {
	s := newStubSession(t, nil)
	c := &chatControllable{}
	h := TextHandler{}
	pk := &packet.Text{
		TextType:   packet.TextTypeChat,
		SourceName: s.conn.IdentityData().DisplayName,
		XUID:       s.conn.IdentityData().XUID,
		Message:    strings.Repeat("a", 8192),
	}
	if err := h.Handle(pk, s, nil, c); err == nil {
		t.Fatalf("Text with %d-byte message succeeded, want rejection", len(pk.Message))
	}
	if c.chatted != 0 {
		t.Fatalf("oversize chat reached Chat %d times, want 0", c.chatted)
	}
}

// TestCommandRequestRejectsOversizeLine pins the command bound: a command line
// far beyond legitimate use must be rejected before splitting and executing.
func TestCommandRequestRejectsOversizeLine(t *testing.T) {
	c := &chatControllable{}
	h := &CommandRequestHandler{}
	pk := &packet.CommandRequest{CommandLine: "/" + strings.Repeat("a", 32768)}
	if err := h.Handle(pk, nil, nil, c); err == nil {
		t.Fatalf("CommandRequest with %d-byte line succeeded, want rejection", len(pk.CommandLine))
	}
	if c.commands != 0 {
		t.Fatalf("oversize command executed %d times, want 0", c.commands)
	}
}

// TestModalFormResponseRejectsOversizeData pins the form bound: a response far
// beyond legitimate forms must be rejected for its size before JSON decoding,
// even for an unknown form ID.
func TestModalFormResponseRejectsOversizeData(t *testing.T) {
	h := &ModalFormResponseHandler{forms: map[uint32]form.Form{}}
	pk := &packet.ModalFormResponse{
		FormID:       9999,
		ResponseData: protocol.Option([]byte(strings.Repeat("a", 262144))),
	}
	err := h.Handle(pk, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("ModalFormResponse with 256KB data error = %v, want too-long rejection", err)
	}
}
