package session

import (
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// writePacket is called from the world goroutine while broadcasting, so what it
// does when a client stops reading decides whether one connection stalls or the
// whole server does.

// TestWritePacketDoesNotBlockOnAFullBuffer pins that a session whose writer
// never drains the buffer stops taking packets instead of parking the caller,
// and that it is closed rather than left half fed.
func TestWritePacketDoesNotBlockOnAFullBuffer(t *testing.T) {
	s := newStubSession(t, &stubConn{})
	// No goroutine drains s.packets, so the buffer fills and stays full.

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range cap(s.packets) + 64 {
			s.writePacket(&packet.Text{Message: strconv.Itoa(i)})
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second * 10):
		t.Fatal("writePacket blocked on a full packet buffer")
	}

	select {
	case <-s.closeBackground:
	case <-time.After(time.Second * 10):
		t.Fatal("the session was not closed after its packet buffer overflowed")
	}
	if got := len(s.packets); got != cap(s.packets) {
		t.Errorf("buffered packets = %d, want the buffer full at %d", got, cap(s.packets))
	}
}

// TestWritePacketLogsTheOverflowThatClosedTheSession pins the record of the
// disconnection. The player is dropped with no reason attached to it, so this
// line is the only place the cause is written down.
func TestWritePacketLogsTheOverflowThatClosedTheSession(t *testing.T) {
	rec := &recordingHandler{}
	s := newStubSession(t, &stubConn{})
	s.conf.Log = slog.New(rec)

	for range cap(s.packets) + 8 {
		s.writePacket(&packet.Text{Message: "x"})
	}

	_, attrs, ok := rec.find("closing session: packet buffer full")
	if !ok {
		t.Fatalf("the overflow was not logged; records: %v", rec.records)
	}
	want := map[string]string{
		"buffered": strconv.Itoa(cap(s.packets)),
		"capacity": strconv.Itoa(cap(s.packets)),
		"dropped":  "*packet.Text",
	}
	for k, v := range want {
		if attrs[k] != v {
			t.Errorf("%v = %q, want %q (all attrs %v)", k, attrs[k], v, attrs)
		}
	}

	// Only the first overflow is logged, so a session that keeps being written
	// to does not fill the log with one line per dropped packet.
	n := 0
	for _, r := range rec.records {
		if r.Message == "closing session: packet buffer full" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the overflow was logged %d times, want 1", n)
	}
}

// TestWritePacketKeepsOrderAndContent pins that nothing changes for a session
// whose buffer has room: every packet is queued, in the order it was written,
// and the session is left open.
func TestWritePacketKeepsOrderAndContent(t *testing.T) {
	s := newStubSession(t, &stubConn{})

	want := make([]*packet.Text, cap(s.packets))
	for i := range want {
		want[i] = &packet.Text{Message: strconv.Itoa(i)}
		s.writePacket(want[i])
	}

	got := sentPackets(t, s)
	if len(got) != len(want) {
		t.Fatalf("queued %d packets, want %d", len(got), len(want))
	}
	for i, pk := range got {
		text, ok := pk.(*packet.Text)
		if !ok {
			t.Fatalf("packet %d = %T, want *packet.Text", i, pk)
		}
		if text != want[i] {
			t.Fatalf("packet %d = %q, want %q", i, text.Message, want[i].Message)
		}
	}
	if s.overflowed.Load() {
		t.Error("the session was marked overflowed while its buffer had room")
	}
	select {
	case <-s.closeBackground:
		t.Error("the session was closed while its buffer had room")
	default:
	}
}
