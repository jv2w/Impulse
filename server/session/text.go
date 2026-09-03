package session

import (
	"time"

	"github.com/jv2w/Impulse/server/player/chat"
	"github.com/jv2w/Impulse/server/player/scoreboard"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/text/language"
)

// SendMessage ...
func (s *Session) SendMessage(message string) {
	s.writePacket(&packet.Text{
		TextType: packet.TextTypeRaw,
		Message:  message,
	})
}

// SendTranslation sends a translation localised for a specific language.Tag.
func (s *Session) SendTranslation(t chat.Translation, l language.Tag, a []any) {
	tr := t.F(a...)
	s.writePacket(&packet.Text{
		TextType:         packet.TextTypeTranslation,
		NeedsTranslation: true,
		Message:          tr.Resolve(l),
		Parameters:       tr.Params(l),
	})
}

// SendTip ...
func (s *Session) SendTip(message string) {
	s.writePacket(&packet.Text{
		TextType: packet.TextTypeTip,
		Message:  message,
	})
}

// SendAnnouncement ...
func (s *Session) SendAnnouncement(message string) {
	s.writePacket(&packet.Text{
		TextType: packet.TextTypeAnnouncement,
		Message:  message,
	})
}

// SendPopup ...
func (s *Session) SendPopup(message string) {
	s.writePacket(&packet.Text{
		TextType: packet.TextTypePopup,
		Message:  message,
	})
}

// SendJukeboxPopup ...
func (s *Session) SendJukeboxPopup(message string) {
	s.writePacket(&packet.Text{
		TextType: packet.TextTypeJukeboxPopup,
		Message:  message,
	})
}

// SendToast ...
func (s *Session) SendToast(title, message string) {
	s.writePacket(&packet.ToastRequest{
		Title:   title,
		Message: message,
	})
}

// SendScoreboard ...
func (s *Session) SendScoreboard(sb *scoreboard.Scoreboard) {
	if s == Nop {
		return
	}
	currentName := *s.currentScoreboard.Load()

	// The board is read once here instead of five times. Name only returns a field and Lines clones the
	// board's lines and rebuilds every padded string on each call; neither writes to the board, and nothing
	// below changes it, so both return the same data they did before.
	boardName, boardLines := sb.Name(), sb.Lines()
	if currentName != boardName {
		s.RemoveScoreboard()
		pk := &packet.SetDisplayObjective{
			DisplaySlot:   "sidebar",
			ObjectiveName: boardName,
			DisplayName:   boardName,
			CriteriaName:  "dummy",
		}
		if sb.Descending() {
			pk.SortOrder = packet.ScoreboardSortOrderDescending
		} else {
			pk.SortOrder = packet.ScoreboardSortOrderAscending
		}
		s.writePacket(pk)
		name, lines := boardName, append([]string(nil), boardLines...)
		s.currentScoreboard.Store(&name)
		s.currentLines.Store(&lines)
	} else {
		// Remove all current lines from the scoreboard. We can't replace them without removing them.
		// Only this branch needs the recorded lines, so they are loaded here instead of on every send.
		// The number of entries is known up front, so the slice is allocated once instead of grown. An
		// empty board still writes no packet, so the field is never observed.
		currentLines := *s.currentLines.Load()
		pk := &packet.SetScore{Entries: make([]protocol.ScoreboardEntry, 0, len(currentLines))}
		for i := range currentLines {
			pk.Entries = append(pk.Entries, protocol.ScoreboardEntry{
				IdentityType:  protocol.ScoreboardIdentityRemove,
				EntryID:       int64(i),
				ObjectiveName: currentName,
				Score:         int32(i),
			})
		}
		if len(pk.Entries) > 0 {
			s.writePacket(pk)
		}
	}
	// One entry per line, so the slice is allocated once instead of grown.
	pk := &packet.SetScore{Entries: make([]protocol.ScoreboardEntry, 0, len(boardLines))}
	for k, line := range boardLines {
		if len(line) == 0 {
			line = prefixedColours[k]
		}
		pk.Entries = append(pk.Entries, protocol.ScoreboardEntry{
			EntryID:       int64(k),
			ObjectiveName: boardName,
			Score:         int32(k),
			IdentityType:  protocol.ScoreboardIdentityFakePlayer,
			DisplayName:   line,
		})
	}
	if len(pk.Entries) > 0 {
		s.writePacket(pk)
	}
}

// colours holds a list of colour codes to be filled out for empty lines in a scoreboard.
var colours = [15]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"}

// prefixedColours holds the same codes with the section sign already attached, so a blank scoreboard line
// does not build that string again on every send. It is built from colours and has the same length, so a
// line index past the end still panics with the same index and length.
var prefixedColours = func() [15]string {
	var v [15]string
	for i, c := range colours {
		v[i] = "§" + c
	}
	return v
}()

// RemoveScoreboard ...
func (s *Session) RemoveScoreboard() {
	s.writePacket(&packet.RemoveObjective{ObjectiveName: *s.currentScoreboard.Load()})
	var name string
	var lines []string
	s.currentScoreboard.Store(&name)
	s.currentLines.Store(&lines)
}

// SendBossBar sends a boss bar to the player with the text passed and the health percentage of the bar.
// SendBossBar removes any boss bar that might be active before sending the new one.
func (s *Session) SendBossBar(text string, colour uint8, healthPercentage float64) {
	s.RemoveBossBar()
	s.writePacket(&packet.BossEvent{
		BossEntityUniqueID: selfEntityRuntimeID,
		EventType:          packet.BossEventShow,
		BossBarTitle:       text,
		HealthPercentage:   float32(healthPercentage),
		Colour:             colour,
	})
}

// RemoveBossBar removes any boss bar currently active on the player's screen.
func (s *Session) RemoveBossBar() {
	s.writePacket(&packet.BossEvent{
		BossEntityUniqueID: selfEntityRuntimeID,
		EventType:          packet.BossEventHide,
	})
}

const tickLength = time.Second / 20

// SetTitleDurations ...
func (s *Session) SetTitleDurations(fadeInDuration, remainDuration, fadeOutDuration time.Duration) {
	s.writePacket(&packet.SetTitle{
		ActionType:      packet.TitleActionSetDurations,
		FadeInDuration:  int32(fadeInDuration / tickLength),
		RemainDuration:  int32(remainDuration / tickLength),
		FadeOutDuration: int32(fadeOutDuration / tickLength),
	})
}

// SendTitle ...
func (s *Session) SendTitle(text string) {
	s.writePacket(&packet.SetTitle{ActionType: packet.TitleActionSetTitle, Text: text})
}

// SendSubtitle ...
func (s *Session) SendSubtitle(text string) {
	s.writePacket(&packet.SetTitle{ActionType: packet.TitleActionSetSubtitle, Text: text})
}

// SendActionBarMessage ...
func (s *Session) SendActionBarMessage(text string) {
	s.writePacket(&packet.SetTitle{ActionType: packet.TitleActionSetActionBar, Text: text})
}
