package session

import (
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// bookSession returns a Session whose inventory slot 0 holds a book and quill
// with a single page. BookEditHandler.Handle ignores the Tx and the
// Controllable, so both are passed as nil below.
func bookSession(t *testing.T) *Session {
	t.Helper()
	s := newStubSession(t, nil)
	s.inv = inventory.New(36, nil)
	if err := s.inv.SetItem(0, item.NewStack(item.BookAndQuill{Pages: []string{"page one"}}, 1)); err != nil {
		t.Fatalf("seed book and quill: %v", err)
	}
	return s
}

// signBook signs the book in slot 0 with the title and author passed.
func signBook(s *Session, title, author string) error {
	return BookEditHandler{}.Handle(&packet.BookEdit{
		ActionType:    packet.BookActionSign,
		InventorySlot: 0,
		Title:         title,
		Author:        author,
	}, s, nil, nil)
}

// slotItem returns the item in inventory slot 0.
func slotItem(t *testing.T, s *Session) item.Stack {
	t.Helper()
	it, err := s.inv.Item(0)
	if err != nil {
		t.Fatalf("read slot 0: %v", err)
	}
	return it
}

// TestBookEditSignStoresTitleAndAuthor pins what signing a book writes into the
// slot. The stored value goes straight to item NBT, so every field is asserted.
func TestBookEditSignStoresTitleAndAuthor(t *testing.T) {
	s := bookSession(t)
	if err := signBook(s, "My Diary", "Steve"); err != nil {
		t.Fatalf("sign book: %v", err)
	}
	book, ok := slotItem(t, s).Item().(item.WrittenBook)
	if !ok {
		t.Fatalf("slot 0 holds %T, want item.WrittenBook", slotItem(t, s).Item())
	}
	if book.Title != "My Diary" {
		t.Fatalf("title = %q, want %q", book.Title, "My Diary")
	}
	if book.Author != "Steve" {
		t.Fatalf("author = %q, want %q", book.Author, "Steve")
	}
	if len(book.Pages) != 1 || book.Pages[0] != "page one" {
		t.Fatalf("pages = %q, want [\"page one\"]", book.Pages)
	}
	if book.Generation != item.OriginalGeneration() {
		t.Fatalf("generation = %v, want the original generation", book.Generation)
	}
}

// TestBookEditSignNBTKeys pins the NBT keys the signed book is stored under.
// Reading and writing use the same map, so a round trip alone would not catch a
// renamed key; the keys and the values behind them are asserted directly.
func TestBookEditSignNBTKeys(t *testing.T) {
	s := bookSession(t)
	if err := signBook(s, "My Diary", "Steve"); err != nil {
		t.Fatalf("sign book: %v", err)
	}
	book := slotItem(t, s).Item().(item.WrittenBook)
	data := book.EncodeNBT()
	if got, ok := data["title"].(string); !ok || got != "My Diary" {
		t.Fatalf("nbt[\"title\"] = %#v, want %q", data["title"], "My Diary")
	}
	if got, ok := data["author"].(string); !ok || got != "Steve" {
		t.Fatalf("nbt[\"author\"] = %#v, want %q", data["author"], "Steve")
	}
	if got, ok := data["generation"].(uint8); !ok || got != 0 {
		t.Fatalf("nbt[\"generation\"] = %#v, want uint8(0)", data["generation"])
	}
	pages, ok := data["pages"].([]any)
	if !ok || len(pages) != 1 {
		t.Fatalf("nbt[\"pages\"] = %#v, want one page", data["pages"])
	}
	if got := pages[0].(map[string]any)["text"]; got != "page one" {
		t.Fatalf("nbt page text = %#v, want %q", got, "page one")
	}
}

// TestBookEditSignRejectsOversizedFields pins the cap on the title and the
// author. Both come straight from a client packet and end up in item NBT that
// is written to the world, and nothing bounded them before.
func TestBookEditSignRejectsOversizedFields(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		author  string
		wantErr string
	}{
		{
			name:    "title one byte past the cap",
			title:   strings.Repeat("a", 257),
			author:  "Steve",
			wantErr: "title can not be longer than 256 bytes",
		},
		{
			name:    "author one byte past the cap",
			title:   "My Diary",
			author:  strings.Repeat("a", 257),
			wantErr: "author can not be longer than 256 bytes",
		},
		{
			// The cap is on bytes, not runes: 86 three-byte runes are 258
			// bytes and are rejected even though they are 86 characters.
			name:    "multi byte title past the byte cap",
			title:   strings.Repeat("한", 86),
			author:  "Steve",
			wantErr: "title can not be longer than 256 bytes",
		},
		{
			name:    "title is not valid utf8",
			title:   "\xff\xfe",
			author:  "Steve",
			wantErr: "title must be valid utf8",
		},
		{
			name:    "author is not valid utf8",
			title:   "My Diary",
			author:  "\xff\xfe",
			wantErr: "author must be valid utf8",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := bookSession(t)
			err := signBook(s, test.title, test.author)
			if err == nil {
				t.Fatal("signing was accepted, want an error")
			}
			if err.Error() != test.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), test.wantErr)
			}
			// The slot must be untouched: a rejected sign may not consume or
			// replace the book and quill.
			if _, ok := slotItem(t, s).Item().(item.BookAndQuill); !ok {
				t.Fatalf("slot 0 holds %T after a rejected sign, want the untouched item.BookAndQuill", slotItem(t, s).Item())
			}
		})
	}
}

// TestBookEditSignAcceptsFieldsAtTheCap pins that the cap is inclusive, so a
// title and an author of exactly 256 bytes still sign as they did before.
func TestBookEditSignAcceptsFieldsAtTheCap(t *testing.T) {
	title, author := strings.Repeat("a", 256), strings.Repeat("b", 256)
	s := bookSession(t)
	if err := signBook(s, title, author); err != nil {
		t.Fatalf("sign book at the cap: %v", err)
	}
	book, ok := slotItem(t, s).Item().(item.WrittenBook)
	if !ok {
		t.Fatalf("slot 0 holds %T, want item.WrittenBook", slotItem(t, s).Item())
	}
	if book.Title != title || book.Author != author {
		t.Fatalf("title/author of %d/%d bytes were not stored unchanged", len(book.Title), len(book.Author))
	}
}

// TestBookEditNonSignActionsIgnoreTitleAndAuthor pins that the cap is scoped to
// the sign action. The client fills Title and Author in on every BookEdit
// packet, so checking them before the switch would start rejecting page edits
// that are accepted today.
func TestBookEditNonSignActionsIgnoreTitleAndAuthor(t *testing.T) {
	s := bookSession(t)
	err := BookEditHandler{}.Handle(&packet.BookEdit{
		ActionType:    packet.BookActionReplacePage,
		InventorySlot: 0,
		PageNumber:    0,
		Text:          "rewritten",
		Title:         strings.Repeat("a", 257),
		Author:        "\xff\xfe",
	}, s, nil, nil)
	if err != nil {
		t.Fatalf("replacing a page with an oversized title = %v, want no error", err)
	}
	book, ok := slotItem(t, s).Item().(item.BookAndQuill)
	if !ok {
		t.Fatalf("slot 0 holds %T, want item.BookAndQuill", slotItem(t, s).Item())
	}
	if len(book.Pages) != 1 || book.Pages[0] != "rewritten" {
		t.Fatalf("pages = %q, want [\"rewritten\"]", book.Pages)
	}
}
