package session

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jv2w/Impulse/server/player/chat"
	"github.com/jv2w/Impulse/server/player/scoreboard"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/text/language"
)

// Everything in text.go is a one-way write onto the wire, so the golden value
// is the packet itself: which packet type, which text type, and which field the
// caller's string ends up in. The numeric text types are pinned separately
// because the client switches on them.

// TestTextTypeGoldenValues pins the packet.TextType* iota block. A renumbering
// there silently redirects every message this file sends to a different part of
// the client's UI.
func TestTextTypeGoldenValues(t *testing.T) {
	golden := map[string][2]byte{
		"Raw":          {packet.TextTypeRaw, 0},
		"Chat":         {packet.TextTypeChat, 1},
		"Translation":  {packet.TextTypeTranslation, 2},
		"Popup":        {packet.TextTypePopup, 3},
		"JukeboxPopup": {packet.TextTypeJukeboxPopup, 4},
		"Tip":          {packet.TextTypeTip, 5},
		"System":       {packet.TextTypeSystem, 6},
		"Whisper":      {packet.TextTypeWhisper, 7},
		"Announcement": {packet.TextTypeAnnouncement, 8},
	}
	for name, pair := range golden {
		if pair[0] != pair[1] {
			t.Errorf("packet.TextType%s = %d, want the golden %d", name, pair[0], pair[1])
		}
	}

	titles := map[string][2]int32{
		"Clear":        {packet.TitleActionClear, 0},
		"SetTitle":     {packet.TitleActionSetTitle, 2},
		"SetSubtitle":  {packet.TitleActionSetSubtitle, 3},
		"SetActionBar": {packet.TitleActionSetActionBar, 4},
		"SetDurations": {packet.TitleActionSetDurations, 5},
	}
	for name, pair := range titles {
		if pair[0] != pair[1] {
			t.Errorf("packet.TitleAction%s = %d, want the golden %d", name, pair[0], pair[1])
		}
	}

	if packet.BossEventShow != 0 || packet.BossEventHide != 2 {
		t.Errorf("boss event show/hide = %d/%d, want the golden 0/2",
			packet.BossEventShow, packet.BossEventHide)
	}
}

// TestSendTextMessages pins which packet.Text each of the plain message senders
// produces. The message string is the same in all of them so that only the text
// type distinguishes the cases.
func TestSendTextMessages(t *testing.T) {
	const msg = "hello §cworld"

	tests := []struct {
		name string
		send func(s *Session)
		want *packet.Text
	}{
		{
			name: "SendMessage",
			send: func(s *Session) { s.SendMessage(msg) },
			want: &packet.Text{TextType: packet.TextTypeRaw, Message: msg},
		},
		{
			name: "SendTip",
			send: func(s *Session) { s.SendTip(msg) },
			want: &packet.Text{TextType: packet.TextTypeTip, Message: msg},
		},
		{
			name: "SendAnnouncement",
			send: func(s *Session) { s.SendAnnouncement(msg) },
			want: &packet.Text{TextType: packet.TextTypeAnnouncement, Message: msg},
		},
		{
			name: "SendPopup",
			send: func(s *Session) { s.SendPopup(msg) },
			want: &packet.Text{TextType: packet.TextTypePopup, Message: msg},
		},
		{
			name: "SendJukeboxPopup",
			send: func(s *Session) { s.SendJukeboxPopup(msg) },
			want: &packet.Text{TextType: packet.TextTypeJukeboxPopup, Message: msg},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			test.send(s)

			got, ok := onlyPacket(t, s).(*packet.Text)
			if !ok {
				t.Fatalf("%s did not write a packet.Text", test.name)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("packet = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestSendMessageIsOpaque pins that nothing in the message path escapes,
// truncates or validates the caller's string: colour codes, newlines, invalid
// UTF-8 and the empty string all reach the wire byte for byte.
func TestSendMessageIsOpaque(t *testing.T) {
	messages := []string{
		"",
		"§c§lred and bold",
		"line one\nline two",
		"tab\there",
		"日本語のメッセージ",
		"\xff\xfe invalid utf-8",
		strings.Repeat("a", 100000),
		"%s %v %%",
	}
	for _, msg := range messages {
		s := newStubSession(t, nil)
		s.SendMessage(msg)

		got := onlyPacket(t, s).(*packet.Text)
		if got.Message != msg {
			t.Fatalf("message round trip changed %q into %q", msg, got.Message)
		}
		if got.NeedsTranslation {
			t.Fatalf("SendMessage set NeedsTranslation for %q", msg)
		}
		if got.Parameters != nil {
			t.Fatalf("SendMessage set parameters %v for %q", got.Parameters, msg)
		}
	}
}

// stubTranslationString is a chat.TranslationString whose resolution depends on
// the language, so that a test can tell which language.Tag reached it.
type stubTranslationString struct{}

func (stubTranslationString) Resolve(l language.Tag) string {
	return "resolved:" + l.String()
}

// TestSendTranslation pins that a translation is sent as a translated text
// packet with its arguments in Parameters rather than substituted into Message,
// and that the language reaches both the string and its arguments.
func TestSendTranslation(t *testing.T) {
	tr := chat.Translate(stubTranslationString{}, 2, "%v and %v")

	s := newStubSession(t, nil)
	s.SendTranslation(tr, language.German, []any{"Steve", 7})

	got := onlyPacket(t, s).(*packet.Text)
	want := &packet.Text{
		TextType:         packet.TextTypeTranslation,
		NeedsTranslation: true,
		// text.Colourf runs the translation through the Translation's format,
		// which defaults to "%v" and comes back with a leading reset code.
		Message:    "§rresolved:de",
		Parameters: []string{"Steve", "7"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packet = %#v, want %#v", got, want)
	}
}

// TestSendTranslationParameterConversion pins how argument values become
// strings. Everything goes through fmt.Sprint except a TranslationString, which
// is resolved in the target language first.
func TestSendTranslationParameterConversion(t *testing.T) {
	tr := chat.Translate(stubTranslationString{}, 5, "%v %v %v %v %v")

	s := newStubSession(t, nil)
	s.SendTranslation(tr, language.English, []any{
		nil,
		true,
		3.5,
		[]int{1, 2},
		stubTranslationString{},
	})

	got := onlyPacket(t, s).(*packet.Text)
	want := []string{"<nil>", "true", "3.5", "[1 2]", "resolved:en"}
	if !reflect.DeepEqual(got.Parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", got.Parameters, want)
	}
}

// TestSendTranslationZeroParameters pins that a translation taking no arguments
// still sends an empty, non-nil parameter slice.
func TestSendTranslationZeroParameters(t *testing.T) {
	tr := chat.Translate(stubTranslationString{}, 0, "no args")

	s := newStubSession(t, nil)
	s.SendTranslation(tr, language.English, nil)

	got := onlyPacket(t, s).(*packet.Text)
	if got.Parameters == nil || len(got.Parameters) != 0 {
		t.Fatalf("parameters = %#v, want an empty non-nil slice", got.Parameters)
	}
}

// TestSendTranslationArgumentCountMismatchPanics pins the one way a caller can
// take the session down through this path.
//
// PINNED BUG: chat.Translation.F panics when the argument count does not match
// the count the Translation was declared with, and SendTranslation calls it
// without checking. A server-side caller with the wrong argument count kills
// the goroutine rather than getting an error back. Reachable only from server
// code, not from a packet.
func TestSendTranslationArgumentCountMismatchPanics(t *testing.T) {
	tr := chat.Translate(stubTranslationString{}, 2, "%v and %v")
	s := newStubSession(t, nil)

	defer func() {
		if recover() == nil {
			t.Fatal("SendTranslation with too few arguments did not panic")
		}
	}()
	s.SendTranslation(tr, language.English, []any{"only one"})
}

func TestSendToast(t *testing.T) {
	s := newStubSession(t, nil)
	s.SendToast("Title", "Body")

	got, ok := onlyPacket(t, s).(*packet.ToastRequest)
	if !ok {
		t.Fatal("SendToast did not write a packet.ToastRequest")
	}
	want := &packet.ToastRequest{Title: "Title", Message: "Body"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packet = %#v, want %#v", got, want)
	}
}

// TestSendTitles pins the three title slots and the duration conversion. Titles
// are all carried by packet.SetTitle and told apart by ActionType alone.
func TestSendTitles(t *testing.T) {
	tests := []struct {
		name string
		send func(s *Session)
		want *packet.SetTitle
	}{
		{
			name: "SendTitle",
			send: func(s *Session) { s.SendTitle("t") },
			want: &packet.SetTitle{ActionType: packet.TitleActionSetTitle, Text: "t"},
		},
		{
			name: "SendSubtitle",
			send: func(s *Session) { s.SendSubtitle("t") },
			want: &packet.SetTitle{ActionType: packet.TitleActionSetSubtitle, Text: "t"},
		},
		{
			name: "SendActionBarMessage",
			send: func(s *Session) { s.SendActionBarMessage("t") },
			want: &packet.SetTitle{ActionType: packet.TitleActionSetActionBar, Text: "t"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			test.send(s)

			got := onlyPacket(t, s).(*packet.SetTitle)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("packet = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestSetTitleDurations pins the duration to tick conversion. A tick is 50ms;
// the division truncates towards zero and is not clamped, so a sub-tick
// duration becomes zero and a negative duration stays negative.
func TestSetTitleDurations(t *testing.T) {
	tests := []struct {
		name                        string
		in, remain, out             time.Duration
		wantIn, wantRemain, wantOut int32
	}{
		{
			name: "whole seconds",
			in:   time.Second, remain: 2 * time.Second, out: 3 * time.Second,
			wantIn: 20, wantRemain: 40, wantOut: 60,
		},
		{
			name: "exactly one tick",
			in:   50 * time.Millisecond, remain: 50 * time.Millisecond, out: 50 * time.Millisecond,
			wantIn: 1, wantRemain: 1, wantOut: 1,
		},
		{
			name: "sub-tick durations truncate to zero",
			in:   49 * time.Millisecond, remain: time.Nanosecond, out: 0,
			wantIn: 0, wantRemain: 0, wantOut: 0,
		},
		{
			// PINNED SCOPE: negative durations are neither rejected nor
			// clamped. Out of scope.
			name: "negative durations pass through",
			in:   -time.Second, remain: -50 * time.Millisecond, out: -time.Nanosecond,
			wantIn: -20, wantRemain: -1, wantOut: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStubSession(t, nil)
			s.SetTitleDurations(test.in, test.remain, test.out)

			got := onlyPacket(t, s).(*packet.SetTitle)
			want := &packet.SetTitle{
				ActionType:      packet.TitleActionSetDurations,
				FadeInDuration:  test.wantIn,
				RemainDuration:  test.wantRemain,
				FadeOutDuration: test.wantOut,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("packet = %#v, want %#v", got, want)
			}
		})
	}
}

// TestSendBossBar pins that showing a boss bar first hides any existing one,
// so two packets go out in a fixed order.
func TestSendBossBar(t *testing.T) {
	s := newStubSession(t, nil)
	s.SendBossBar("Boss", 3, 0.5)

	pks := sentPackets(t, s)
	if len(pks) != 2 {
		t.Fatalf("SendBossBar wrote %d packets, want 2 (a hide then a show)", len(pks))
	}
	hide := &packet.BossEvent{
		BossEntityUniqueID: selfEntityRuntimeID,
		EventType:          packet.BossEventHide,
	}
	if !reflect.DeepEqual(pks[0], hide) {
		t.Fatalf("first packet = %#v, want %#v", pks[0], hide)
	}
	show := &packet.BossEvent{
		BossEntityUniqueID: selfEntityRuntimeID,
		EventType:          packet.BossEventShow,
		BossBarTitle:       "Boss",
		HealthPercentage:   0.5,
		Colour:             3,
	}
	if !reflect.DeepEqual(pks[1], show) {
		t.Fatalf("second packet = %#v, want %#v", pks[1], show)
	}
}

// TestRemoveBossBar pins that removing a boss bar sends a single hide event
// against the session's own runtime ID, which is always 1.
func TestRemoveBossBar(t *testing.T) {
	s := newStubSession(t, nil)
	s.RemoveBossBar()

	got := onlyPacket(t, s).(*packet.BossEvent)
	want := &packet.BossEvent{
		BossEntityUniqueID: 1,
		EventType:          packet.BossEventHide,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packet = %#v, want %#v", got, want)
	}
}

// TestSendScoreboardFirstSend pins the packets a brand new scoreboard produces:
// a remove of whatever was there, a display objective, then one score entry per
// line.
func TestSendScoreboardFirstSend(t *testing.T) {
	s := newStubSession(t, nil)
	sb := scoreboard.New("Board")
	sb.RemovePadding()
	_, _ = sb.WriteString("first\nsecond")

	s.SendScoreboard(sb)

	pks := sentPackets(t, s)
	if len(pks) != 3 {
		t.Fatalf("wrote %d packets, want 3: %#v", len(pks), pks)
	}
	if got, want := pks[0], (&packet.RemoveObjective{ObjectiveName: ""}); !reflect.DeepEqual(got, want) {
		t.Fatalf("first packet = %#v, want %#v", got, want)
	}
	objective := &packet.SetDisplayObjective{
		DisplaySlot:   "sidebar",
		ObjectiveName: "Board",
		DisplayName:   "Board",
		CriteriaName:  "dummy",
		SortOrder:     packet.ScoreboardSortOrderAscending,
	}
	if got := pks[1]; !reflect.DeepEqual(got, objective) {
		t.Fatalf("second packet = %#v, want %#v", got, objective)
	}
	scores := &packet.SetScore{Entries: []protocol.ScoreboardEntry{
		{EntryID: 0, ObjectiveName: "Board", Score: 0, IdentityType: protocol.ScoreboardIdentityFakePlayer, DisplayName: "first"},
		{EntryID: 1, ObjectiveName: "Board", Score: 1, IdentityType: protocol.ScoreboardIdentityFakePlayer, DisplayName: "second"},
	}}
	if got := pks[2]; !reflect.DeepEqual(got, scores) {
		t.Fatalf("third packet = %#v, want %#v", got, scores)
	}

	if got := *s.currentScoreboard.Load(); got != "Board" {
		t.Fatalf("recorded scoreboard name = %q, want Board", got)
	}
	if got, want := *s.currentLines.Load(), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded lines = %#v, want %#v", got, want)
	}
}

// TestSendScoreboardDescending pins that a descending board reverses its lines
// and flips the sort order, while the scores stay 0..n in packet order.
func TestSendScoreboardDescending(t *testing.T) {
	s := newStubSession(t, nil)
	sb := scoreboard.New("Board")
	sb.RemovePadding()
	sb.SetDescending()
	_, _ = sb.WriteString("first\nsecond")

	s.SendScoreboard(sb)

	pks := sentPackets(t, s)
	objective := pks[1].(*packet.SetDisplayObjective)
	if objective.SortOrder != packet.ScoreboardSortOrderDescending {
		t.Fatalf("sort order = %d, want descending %d", objective.SortOrder, packet.ScoreboardSortOrderDescending)
	}
	scores := pks[2].(*packet.SetScore)
	if got, want := scores.Entries[0].DisplayName, "second"; got != want {
		t.Fatalf("first entry = %q, want %q", got, want)
	}
	if got, want := scores.Entries[1].DisplayName, "first"; got != want {
		t.Fatalf("second entry = %q, want %q", got, want)
	}
}

// TestSendScoreboardResendSameName pins the update path: sending a board with
// the same name as the one already displayed removes the old lines instead of
// removing the whole objective, so the objective packet is not repeated.
func TestSendScoreboardResendSameName(t *testing.T) {
	s := newStubSession(t, nil)
	sb := scoreboard.New("Board")
	sb.RemovePadding()
	_, _ = sb.WriteString("a\nb")
	s.SendScoreboard(sb)
	sentPackets(t, s)

	next := scoreboard.New("Board")
	next.RemovePadding()
	_, _ = next.WriteString("c")
	s.SendScoreboard(next)

	pks := sentPackets(t, s)
	if len(pks) != 2 {
		t.Fatalf("wrote %d packets, want 2 (a removal then the new scores): %#v", len(pks), pks)
	}
	removal := &packet.SetScore{Entries: []protocol.ScoreboardEntry{
		{IdentityType: protocol.ScoreboardIdentityRemove, EntryID: 0, ObjectiveName: "Board", Score: 0},
		{IdentityType: protocol.ScoreboardIdentityRemove, EntryID: 1, ObjectiveName: "Board", Score: 1},
	}}
	if got := pks[0]; !reflect.DeepEqual(got, removal) {
		t.Fatalf("first packet = %#v, want %#v", got, removal)
	}

	// PINNED BUG: the same-name path never updates currentLines, so the next
	// resend still removes two entries even though only one line is displayed.
	if got, want := *s.currentLines.Load(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded lines = %#v, want the stale %#v this pins", got, want)
	}
}

// TestSendScoreboardEmptyLineGetsAColourCode pins the placeholder written for a
// blank line: an empty line would collapse client-side, so each index gets a
// distinct colour code instead.
func TestSendScoreboardEmptyLineGetsAColourCode(t *testing.T) {
	s := newStubSession(t, nil)
	sb := scoreboard.New("Board")
	sb.RemovePadding()
	_, _ = sb.WriteString("\n\nthird")

	s.SendScoreboard(sb)

	pks := sentPackets(t, s)
	scores := pks[2].(*packet.SetScore)
	want := []string{"§1", "§2", "third"}
	for i, entry := range scores.Entries {
		if entry.DisplayName != want[i] {
			t.Fatalf("entry %d = %q, want %q", i, entry.DisplayName, want[i])
		}
	}
}

// TestSendScoreboardColoursTableIsGolden pins the placeholder table itself. It
// is indexed by line number, so its length caps how many blank lines a
// scoreboard may have.
func TestSendScoreboardColoursTableIsGolden(t *testing.T) {
	want := [15]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"}
	if colours != want {
		t.Fatalf("colours = %#v, want %#v", colours, want)
	}
}

// TestSendScoreboardPaddingHidesTheBlankLineCase pins that the default padded
// scoreboard never produces a blank line, because padding surrounds every line
// with spaces. The colour placeholder is therefore only reachable through
// RemovePadding.
func TestSendScoreboardPaddingHidesTheBlankLineCase(t *testing.T) {
	s := newStubSession(t, nil)
	sb := scoreboard.New("Board")
	_, _ = sb.WriteString("")

	s.SendScoreboard(sb)

	scores := sentPackets(t, s)[2].(*packet.SetScore)
	if got := scores.Entries[0].DisplayName; strings.TrimSpace(got) != "" || got == "" {
		t.Fatalf("padded blank line = %q, want spaces only", got)
	}
}

// TestSendScoreboardEmptyBoardSendsNoScores pins that a board with no lines
// still installs the objective but sends no score packet.
func TestSendScoreboardEmptyBoardSendsNoScores(t *testing.T) {
	s := newStubSession(t, nil)
	s.SendScoreboard(scoreboard.New("Board"))

	pks := sentPackets(t, s)
	if len(pks) != 2 {
		t.Fatalf("wrote %d packets, want 2: %#v", len(pks), pks)
	}
	if _, ok := pks[1].(*packet.SetDisplayObjective); !ok {
		t.Fatalf("second packet = %#v, want a SetDisplayObjective", pks[1])
	}
}

// TestRemoveScoreboard pins that removing the scoreboard names the one
// currently recorded and then clears the record.
func TestRemoveScoreboard(t *testing.T) {
	s := newStubSession(t, nil)
	sb := scoreboard.New("Board")
	_, _ = sb.WriteString("a")
	s.SendScoreboard(sb)
	sentPackets(t, s)

	s.RemoveScoreboard()

	got := onlyPacket(t, s).(*packet.RemoveObjective)
	if got.ObjectiveName != "Board" {
		t.Fatalf("removed objective = %q, want Board", got.ObjectiveName)
	}
	if name := *s.currentScoreboard.Load(); name != "" {
		t.Fatalf("recorded scoreboard name after removal = %q, want empty", name)
	}
	if lines := *s.currentLines.Load(); lines != nil {
		t.Fatalf("recorded lines after removal = %#v, want nil", lines)
	}
}

// TestSendScoreboardOnNopIsANoOp pins the Nop guard. Nop is the shared
// no-operation session; it must swallow a scoreboard without touching its own
// nil state.
func TestSendScoreboardOnNopIsANoOp(t *testing.T) {
	sb := scoreboard.New("Board")
	_, _ = sb.WriteString("a")
	Nop.SendScoreboard(sb)
}

// TestTickLengthIsGolden pins the tick used to convert durations for the
// client. Everything in this file that reports a duration divides by it.
func TestTickLengthIsGolden(t *testing.T) {
	if tickLength != 50*time.Millisecond {
		t.Fatalf("tickLength = %v, want 50ms", tickLength)
	}
}
