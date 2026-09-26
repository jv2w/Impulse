package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"

	"github.com/jv2w/Impulse/server/session"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// stubConn is a session.Conn that serves a fixed identity string. It embeds the
// interface so only the methods finaliseConn reaches on the rejection path need
// bodies; anything else would panic on the nil embedded value.
type stubConn struct {
	session.Conn
	identity string
}

func (s *stubConn) IdentityData() login.IdentityData {
	return login.IdentityData{Identity: s.identity, DisplayName: "Steve"}
}

func (s *stubConn) RemoteAddr() net.Addr { return &net.UDPAddr{} }

func (s *stubConn) Close() error { return nil }

// stubListener records Disconnect calls.
type stubListener struct {
	disconnected bool
	reason       string
}

func (s *stubListener) Accept() (session.Conn, error) { return nil, errors.New("closed") }

func (s *stubListener) Disconnect(_ session.Conn, reason string) error {
	s.disconnected = true
	s.reason = reason
	return nil
}

func (s *stubListener) Close() error { return nil }

// TestFinaliseConnRejectsMalformedIdentity pins that a login carrying an
// identity string that is not a UUID is disconnected, not crashed on.
// finaliseConn runs in a bare goroutine with no recover, so before the fix the
// uuid.MustParse below panicked and took the whole server process down. The
// string is client controlled whenever authentication is disabled.
func TestFinaliseConnRejectsMalformedIdentity(t *testing.T) {
	for _, identity := range []string{"", "not-a-uuid", "9a9c8cb2-a527-4fc4-8127-f373c11c9ee9-trailing"} {
		srv := &Server{conf: Config{Log: slog.New(slog.DiscardHandler)}}
		l := &stubListener{}
		srv.finaliseConn(context.Background(), &stubConn{identity: identity}, l)
		if !l.disconnected {
			t.Fatalf("identity %q: not disconnected, want rejection", identity)
		}
		if l.reason == "" {
			t.Fatalf("identity %q: empty disconnect reason", identity)
		}
	}
}
