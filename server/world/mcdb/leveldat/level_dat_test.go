package leveldat

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadRejectsNegativeFileLength pins the header bound: a corrupt FileLength
// must be an error, not a panic in make. Before the fix the int32 from the
// file reached make([]byte, ...) unchecked, so opening a world with 8 corrupt
// header bytes crashed the server.
func TestReadRejectsNegativeFileLength(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, int32(9))
	_ = binary.Write(&buf, binary.LittleEndian, int32(-1))
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Read panicked on negative FileLength: %v", r)
		}
	}()
	if _, err := Read(&buf); err == nil {
		t.Fatal("Read with FileLength -1 succeeded, want rejection")
	}
}

// TestReadRejectsHugeFileLength pins the allocation bound: a FileLength far
// beyond any real level.dat must be rejected before allocating.
func TestReadRejectsHugeFileLength(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, int32(9))
	_ = binary.Write(&buf, binary.LittleEndian, int32(1<<31-1))
	if _, err := Read(&buf); err == nil {
		t.Fatal("Read with 2GB FileLength succeeded, want rejection")
	}
}

// TestWriteFileRoundTrip pins the write path: Marshal, WriteFile and ReadFile
// must preserve the data, with the NBT keys vanilla expects.
func TestWriteFileRoundTrip(t *testing.T) {
	var ld LevelDat
	if err := ld.Marshal(Data{LevelName: "ImpulseTest", RandomSeed: 12345}); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "level.dat")
	if err := ld.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var data Data
	if err := got.Unmarshal(&data); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if data.LevelName != "ImpulseTest" || data.RandomSeed != 12345 {
		t.Fatalf("round trip = %+v, want LevelName ImpulseTest seed 12345", data)
	}
	var keys map[string]any
	if err := got.Unmarshal(&keys); err != nil {
		t.Fatalf("Unmarshal keys: %v", err)
	}
	if _, ok := keys["LevelName"]; !ok {
		t.Fatalf("NBT keys %v missing golden key LevelName", keys)
	}
}

// TestWriteFileLeavesNoTemp pins atomicity: after WriteFile the directory must
// hold exactly level.dat, with no temp file left behind.
func TestWriteFileLeavesNoTemp(t *testing.T) {
	var ld LevelDat
	if err := ld.Marshal(Data{LevelName: "x"}); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	dir := t.TempDir()
	if err := ld.WriteFile(filepath.Join(dir, "level.dat")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "level.dat" || strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("unexpected file %q after WriteFile", e.Name())
		}
	}
}
