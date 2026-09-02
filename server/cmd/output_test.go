package cmd

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jv2w/Impulse/server/player/chat"
	"golang.org/x/text/language"
)

// Output is what a command source actually sees, and the Message* globals are
// the only vocabulary the package has for reporting failure. Both are pinned
// literally: a caller cannot match on these errors, only render them.

func TestOutputCollectsErrorsAndMessagesSeparately(t *testing.T) {
	// Errors and messages are two independent, ordered lists. Nothing
	// deduplicates or reorders them, and the counts are the lengths.
	o := &Output{}
	o.Print("first")
	o.Printf("second %d", 2)
	o.Error("bad")
	o.Errorf("worse %d", 3)

	if got, want := messageStrings(o), []string{"first", "second 2"}; !slices.Equal(got, want) {
		t.Fatalf("Messages() = %q, want %q", got, want)
	}
	if got, want := errStrings(o), []string{"bad", "worse 3"}; !slices.Equal(got, want) {
		t.Fatalf("Errors() = %q, want %q", got, want)
	}
	if got, want := o.MessageCount(), 2; got != want {
		t.Fatalf("MessageCount() = %d, want %d", got, want)
	}
	if got, want := o.ErrorCount(), 2; got != want {
		t.Fatalf("ErrorCount() = %d, want %d", got, want)
	}
}

func TestOutputPrintUsesSprintSpacingRules(t *testing.T) {
	// Print is fmt.Sprint, so spaces appear only between two operands that are
	// both non-strings. Print("a", "b") is "ab", not "a b".
	o := &Output{}
	o.Print("a", "b")
	o.Print(1, 2)
	o.Print("a", 1, "b")
	if got, want := messageStrings(o), []string{"ab", "1 2", "a1b"}; !slices.Equal(got, want) {
		t.Fatalf("Messages() = %q, want %q", got, want)
	}
}

func TestOutputErrorUnwrapsASingleError(t *testing.T) {
	// Error keeps a lone error value as-is instead of stringifying it, which is
	// the only path in the package that preserves error identity: errors.Is
	// works on that one and on nothing else.
	sentinel := errors.New("pinned sentinel")
	o := &Output{}
	o.Error(sentinel)
	if got := o.Errors()[0]; !errors.Is(got, sentinel) {
		t.Fatalf("Errors()[0] = %v, want the sentinel itself", got)
	}

	// Two arguments, or one non-error argument, go through fmt.Sprint and lose
	// the identity.
	o = &Output{}
	o.Error(sentinel, " and more")
	if errors.Is(o.Errors()[0], sentinel) {
		t.Fatal("Error with two arguments preserved the sentinel, which it must not")
	}
	if got, want := o.Errors()[0].Error(), "pinned sentinel and more"; got != want {
		t.Fatalf("Errors()[0] = %q, want %q", got, want)
	}
}

func TestOutputErrorfDoesNotWrap(t *testing.T) {
	// PINNED BUG: Errorf is fmt.Errorf, so a caller writing %w does get a
	// wrapping error, but every error the package produces itself is a chat
	// translation built with %v-style formatting. CLAUDE.md 3.3 asks for
	// sentinels; this pins that the package offers none.
	sentinel := errors.New("pinned cause")
	o := &Output{}
	o.Errorf("context: %w", sentinel)
	if !errors.Is(o.Errors()[0], sentinel) {
		t.Fatal("Errorf with a wrapping verb did not wrap, so fmt.Errorf semantics have changed")
	}

	o = &Output{}
	o.Errorf("context: %v", sentinel)
	if errors.Is(o.Errors()[0], sentinel) {
		t.Fatal("Errorf with a non-wrapping verb wrapped the cause, which it must not")
	}
}

func TestOutputTranslationsPanicOnAWrongArgumentCount(t *testing.T) {
	// PINNED BUG: Errort and Printt forward to Translation.F, which panics when
	// the argument count does not match. A command reporting an error can
	// therefore crash the world goroutine while reporting it. CLAUDE.md 2.2.
	o := &Output{}
	if v := mustPanic(t, func() { o.Errort(MessageNumberInvalid) }); v == nil {
		t.Fatal("Errort with too few arguments did not panic")
	}
	if v := mustPanic(t, func() { o.Printt(MessageNoTargets, "unexpected") }); v == nil {
		t.Fatal("Printt with too many arguments did not panic")
	}
	if got := o.ErrorCount() + o.MessageCount(); got != 0 {
		t.Fatalf("the Output collected %d entries from panicking calls, want 0", got)
	}
}

func TestOutputTranslationsAreStoredUnresolved(t *testing.T) {
	// A translation added with Errort or Printt keeps its parameters, so a
	// session can resolve it per player language. String() renders the English
	// fallback, which is what every test in this package compares against.
	o := &Output{}
	o.Errort(MessageNumberInvalid, "zz")
	o.Printt(MessageNoTargets)

	if got, want := o.Errors()[0].Error(), errNumber; got != want {
		t.Fatalf("Errors()[0] = %q, want %q", got, want)
	}
	if got, want := o.Messages()[0].String(), errNoTarget; got != want {
		t.Fatalf("Messages()[0] = %q, want %q", got, want)
	}
	resolver, ok := o.Errors()[0].(interface {
		Resolve(language.Tag) string
		Params(language.Tag) []string
	})
	if !ok {
		t.Fatalf("Errors()[0] is %T, want a resolvable translation", o.Errors()[0])
	}
	if got, want := resolver.Resolve(language.English), "§r§c> %commands.generic.num.invalid§r"; got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
	if got, want := resolver.Params(language.English), []string{"zz"}; !slices.Equal(got, want) {
		t.Fatalf("Params() = %q, want %q", got, want)
	}
}

func TestMessageGlobals(t *testing.T) {
	// The nine Message globals are the package's entire error vocabulary. Each
	// row pins the translation identifier the client is sent, the parameter
	// count F requires, and the rendered fallback. Changing any of them changes
	// what every existing command reports.
	cases := []struct {
		name       string
		message    chat.Translation
		args       []any
		identifier string
		want       string
	}{
		{name: "syntax", message: MessageSyntax, args: []any{"/pinned ", "zz", " rest"},
			identifier: "%commands.generic.syntax",
			want:       "§r§cSyntax error: unexpected value: at \"/pinned >>zz<< rest\"§r"},
		{name: "usage", message: MessageUsage, args: []any{"/pinned <V: int>"},
			identifier: "%commands.generic.usage",
			want:       "§r§cUsage: /pinned <V: int>§r"},
		{name: "unknown", message: MessageUnknown, args: []any{"pinned"},
			identifier: "%commands.generic.unknown",
			want:       errUnknown},
		{name: "no targets", message: MessageNoTargets, args: nil,
			identifier: "%commands.generic.noTargetMatch",
			want:       errNoTarget},
		{name: "number invalid", message: MessageNumberInvalid, args: []any{"zz"},
			identifier: "%commands.generic.num.invalid",
			want:       errNumber},
		{name: "boolean invalid", message: MessageBooleanInvalid, args: []any{"zz"},
			identifier: "%commands.generic.boolean.invalid",
			want:       errBool},
		{name: "player not found", message: MessagePlayerNotFound, args: nil,
			identifier: "%commands.generic.player.notFound",
			want:       errNotFound},
		{name: "parameter invalid", message: MessageParameterInvalid, args: []any{"zz"},
			identifier: "%commands.generic.parameter.invalid",
			want:       errParameter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filled := c.message.F(c.args...)
			if got := filled.Error(); got != c.want {
				t.Fatalf("F(%v).Error() = %q, want %q", c.args, got, c.want)
			}
			if got := filled.Resolve(language.English); !strings.Contains(got, c.identifier) {
				t.Fatalf("Resolve() = %q, want it to carry %q", got, c.identifier)
			}
			// One argument too many is a panic, which pins the parameter count.
			if v := mustPanic(t, func() { c.message.F(append(append([]any{}, c.args...), "extra")...) }); v == nil {
				t.Fatalf("F took %d arguments without panicking", len(c.args)+1)
			}
		})
	}
}

func TestMessagePrefixesSplitIntoTwoFamilies(t *testing.T) {
	// PINNED CONSTRAINT: four messages render with a "> " prefix and four do
	// not, for no reason visible in the code. A source showing an Output cannot
	// normalise them because the prefix is baked into the Enc format rather than
	// applied at render time.
	withArrow := []chat.Translation{MessageNumberInvalid, MessageBooleanInvalid, MessagePlayerNotFound, MessageParameterInvalid}
	withoutArrow := []chat.Translation{MessageSyntax, MessageUsage, MessageUnknown, MessageNoTargets}

	for i, m := range withArrow {
		if got := renderWithFiller(m); !strings.Contains(got, "§c> ") {
			t.Fatalf("withArrow[%d] = %q, want a '> ' prefix", i, got)
		}
	}
	for i, m := range withoutArrow {
		if got := renderWithFiller(m); strings.Contains(got, "§c> ") {
			t.Fatalf("withoutArrow[%d] = %q, want no '> ' prefix", i, got)
		}
	}
}

// renderWithFiller fills a translation with as many placeholder arguments as it
// needs and renders the fallback.
func renderWithFiller(m chat.Translation) string {
	for n := range 4 {
		args := make([]any, n)
		for i := range args {
			args[i] = "x"
		}
		if s, ok := tryFill(m, args); ok {
			return s
		}
	}
	return ""
}

func tryFill(m chat.Translation, args []any) (s string, ok bool) {
	defer func() {
		if recover() != nil {
			s, ok = "", false
		}
	}()
	return m.F(args...).Error(), true
}
