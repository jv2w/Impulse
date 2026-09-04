package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The project is called Impulse. Its code is derived from dragonfly and two
// names from that project are baked into the storage format, so those two, and
// the identifiers that name them, are the only place the old name may appear in
// Go source. Everything else - doc comments, log messages, user visible
// defaults, test fixtures - says Impulse. Attribution lives in LICENSE and
// README.md, which are not Go source and are not checked here.
//
// The rule is enforced rather than written down alone because the two names are
// indistinguishable by eye from the dozens of prose mentions that used to sit
// beside them.

// upstreamRe matches the upstream project's name in either case.
var upstreamRe = regexp.MustCompile(`(?i)dragonfly`)

// storageNames are the values CLAUDE.md section 1 pins into the storage format.
// A line is allowed to name the upstream project when it is one of these, or an
// identifier or comment naming one of them.
var storageNames = []string{"dragonflydata", "dragonfly:text"}

// namingTestFile is this file, which spells the rule out and is the one Go file
// the rule cannot apply to.
const namingTestFile = "naming_test.go"

// TestGoSourceNamesImpulse fails on any Go file that still calls the project by
// the upstream name.
func TestGoSourceNamesImpulse(t *testing.T) {
	for _, f := range goFiles(t) {
		if f == namingTestFile {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %v: %v", f, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if !upstreamRe.MatchString(line) || namesStorageValue(line) {
				continue
			}
			t.Errorf("%v:%d names the upstream project outside the storage format:\n\t%v", f, i+1, strings.TrimSpace(line))
		}
	}
}

// namesStorageValue reports whether line is about one of the two pinned storage
// names rather than about the project.
func namesStorageValue(line string) bool {
	lower := strings.ToLower(line)
	for _, n := range storageNames {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// goFiles returns every Go file in the tree, slash separated.
func goFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != "." && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	sort.Strings(files)
	return files
}
