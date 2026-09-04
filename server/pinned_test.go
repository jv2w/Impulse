// This file and naming_test.go check the repository itself rather than any part
// of the server. They live beside the other tests in this package that already
// pin things no single package owns.
package server

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRoot is where the walks below start, relative to this package. Every path
// reported is made relative to it again, so a result reads the same whichever
// directory the test was run from.
const repoRoot = ".."

// indexPath is the checked-in index of every PINNED marker in the tree. It
// exists because a marker records a known defect in a comment inside a test
// file: production code carries no sign of it and the suite stays green, so
// without an index the only way to find one is to know to grep for it.
const indexPath = "PINNED.md"

// updateEnv rewrites indexPath instead of failing when it is set to "1", which
// is how the index is regenerated after a marker is added, removed or reworded.
const updateEnv = "IMPULSE_UPDATE_PINNED"

// markerRe matches the first line of a marker. The three kinds are the ones
// CLAUDE.md section 4.3 defines; a fourth would be a typo, so it is not matched
// and the marker would go missing from the index rather than be filed wrongly.
var markerRe = regexp.MustCompile(`^\s*// PINNED (BUG|SCOPE|CONSTRAINT): (.*)$`)

// continuationRe matches a comment line that continues the marker above it.
var continuationRe = regexp.MustCompile(`^\s*//(.*)$`)

// tokenRe matches the marker words anywhere on a line. A line that holds one
// without being a marker is what the index cannot see, so it is rejected rather
// than skipped: markers written any other way went missing from this index
// silently until the rule below was enforced.
var tokenRe = regexp.MustCompile(`PINNED (BUG|SCOPE|CONSTRAINT)`)

// summaryLimit is where a marker's text is cut in the index. The index says
// which defect is pinned and where; the test file says the rest.
const summaryLimit = 170

type marker struct {
	file, kind, summary string
}

// TestEveryMarkerIsWrittenTheSameWay fails on a marker the index cannot see.
// It is what makes the index complete rather than merely long.
func TestEveryMarkerIsWrittenTheSameWay(t *testing.T) {
	for _, f := range testFiles(t) {
		data, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatalf("read %v: %v", f, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if f == indexTestFile || !tokenRe.MatchString(line) || markerRe.MatchString(line) {
				continue
			}
			t.Errorf("%v:%d is not a marker but names one:\n\t%v\nwrite it as \"// PINNED %v: ...\" on a line of its own, or reword it",
				f, i+1, strings.TrimSpace(line), tokenRe.FindStringSubmatch(line)[1])
		}
	}
}

// indexTestFile is this file, which names the marker words in its own code and
// is the one file the rule above cannot apply to.
const indexTestFile = "server/pinned_test.go"

// TestPinnedIndexIsCurrent fails when PINNED.md does not list exactly the
// markers the tree holds.
func TestPinnedIndexIsCurrent(t *testing.T) {
	want := renderIndex(collectMarkers(t))

	if os.Getenv(updateEnv) == "1" {
		if err := os.WriteFile(indexPath, []byte(want), 0644); err != nil {
			t.Fatalf("write %v: %v", indexPath, err)
		}
		t.Logf("%v rewritten with %v markers", indexPath, strings.Count(want, "\n- "))
		return
	}

	got, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read %v: %v", indexPath, err)
	}
	// The index is not a .go file, so git checks it out with the platform's
	// line endings. The comparison is on the text, not on how it was stored.
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Fatalf("%v is out of date. Regenerate it with:\n\t%v=1 go test ./server -run TestPinnedIndexIsCurrent", indexPath, updateEnv)
	}
}

// testFiles returns every test file in the tree, slash separated so that the
// index reads the same on either platform. Only test files are read: a marker
// pins what a test asserts, so one outside a test file asserts nothing.
func testFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != repoRoot && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	sort.Strings(files)
	return files
}

// collectMarkers reads every marker in the tree.
func collectMarkers(t *testing.T) []marker {
	t.Helper()

	var markers []marker
	for _, f := range testFiles(t) {
		data, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatalf("read %v: %v", f, err)
		}
		markers = append(markers, markersIn(f, string(data))...)
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].file != markers[j].file {
			return markers[i].file < markers[j].file
		}
		if markers[i].kind != markers[j].kind {
			return markers[i].kind < markers[j].kind
		}
		return markers[i].summary < markers[j].summary
	})
	return markers
}

// markersIn extracts the markers of one file, joining each marker's wrapped
// continuation lines back into one sentence before cutting it.
func markersIn(path, src string) []marker {
	lines := strings.Split(src, "\n")
	var out []marker
	for i := 0; i < len(lines); i++ {
		m := markerRe.FindStringSubmatch(strings.TrimRight(lines[i], "\r"))
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[2])
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimRight(lines[j], "\r")
			if markerRe.MatchString(next) {
				break
			}
			c := continuationRe.FindStringSubmatch(next)
			if c == nil {
				break
			}
			part := strings.TrimSpace(c[1])
			if part == "" {
				break
			}
			text += " " + part
			i = j
		}
		out = append(out, marker{file: path, kind: m[1], summary: cut(text)})
	}
	return out
}

// cut trims a marker's text to summaryLimit on a word boundary.
func cut(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= summaryLimit {
		return s
	}
	s = s[:summaryLimit]
	if i := strings.LastIndex(s, " "); i > 0 {
		s = s[:i]
	}
	return s + "..."
}

// renderIndex builds the whole of PINNED.md, counts included, so that the file
// is a function of the tree and nothing else.
func renderIndex(markers []marker) string {
	counts := map[string]int{}
	for _, m := range markers {
		counts[m.kind]++
	}

	var b strings.Builder
	b.WriteString("# Pinned behaviour\n\n")
	b.WriteString("A `PINNED` marker in a test pins **current** behaviour, not correct behaviour.\n")
	b.WriteString("A green test run therefore means behaviour has not drifted, not that it is right.\n\n")
	b.WriteString("- `BUG` — a defect, deliberately left in place and recorded here.\n")
	b.WriteString("- `SCOPE` — a feature that is intentionally absent.\n")
	b.WriteString("- `CONSTRAINT` — a limitation of the test environment.\n\n")
	b.WriteString("Before fixing one, read CLAUDE.md section 4.3: a defect fix updates the test and deletes\n")
	b.WriteString("the marker, while a performance change that breaks one is itself the thing that is wrong.\n\n")
	fmt.Fprintf(&b, "Totals: %d BUG, %d SCOPE, %d CONSTRAINT.\n\n", counts["BUG"], counts["SCOPE"], counts["CONSTRAINT"])
	b.WriteString("This file is generated. Regenerate it with:\n\n")
	fmt.Fprintf(&b, "```\n%s=1 go test ./server -run TestPinnedIndexIsCurrent\n```\n", updateEnv)

	file := ""
	for _, m := range markers {
		if m.file != file {
			file = m.file
			fmt.Fprintf(&b, "\n## %s\n\n", file)
		}
		fmt.Fprintf(&b, "- **%s** — %s\n", m.kind, m.summary)
	}
	return b.String()
}
