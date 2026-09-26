package leveldat

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// LevelDat implements the encoding and decoding of level.dat files. An empty
// LevelDat is a valid value and may be used to Marshal and Write to a writer or
// file afterward.
type LevelDat struct {
	hdr  header
	data []byte
}

// header holds the header for a level.dat file.
type header struct {
	StorageVersion int32
	FileLength     int32
}

// ReadFile reads a level.dat at a path and returns it.
func ReadFile(name string) (*LevelDat, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("level.dat: open file: %w", err)
	}
	defer f.Close()
	return Read(bufio.NewReader(f))
}

// maxLevelDatBytes bounds the NBT payload a level.dat may claim. A real
// level.dat holds world settings, kilobytes at most: 64MB is orders of
// magnitude above that while keeping 8 corrupt header bytes from forcing a
// gigabyte allocation or a negative-length panic on open.
const maxLevelDatBytes = 1 << 26

// Read reads a level.dat from r and returns it.
func Read(r io.Reader) (*LevelDat, error) {
	var ldat LevelDat
	if err := binary.Read(r, binary.LittleEndian, &ldat.hdr); err != nil {
		return nil, fmt.Errorf("level.dat: read header: %w", err)
	}
	// Which input was the problem: FileLength straight from the file. A
	// negative value panicked in make, a huge one allocated gigabytes before
	// the short read below could fail.
	if ldat.hdr.FileLength < 0 || ldat.hdr.FileLength > maxLevelDatBytes {
		return nil, fmt.Errorf("level.dat: invalid file length %d", ldat.hdr.FileLength)
	}
	ldat.data = make([]byte, ldat.hdr.FileLength)
	if n, err := io.ReadFull(r, ldat.data); err != nil || int32(n) != ldat.hdr.FileLength {
		return nil, fmt.Errorf("level.dat: read data: %w", err)
	}
	return &ldat, nil
}

// Unmarshal decodes the level.dat properties from ld into dst. Unmarshal
// returns an error if dst was unable to store all properties found in the
// level.dat.
func (ld *LevelDat) Unmarshal(dst any) error {
	if err := nbt.UnmarshalEncoding(ld.data, dst, nbt.LittleEndian); err != nil {
		return fmt.Errorf("level.dat: decode nbt: %w", err)
	}
	return nil
}

// Ver returns the version of the level.dat decoded, or 0 if ld is the empty
// value.
func (ld *LevelDat) Ver() int {
	return int(ld.hdr.StorageVersion)
}

// Marshal encodes src and stores it in the level.dat. src should be either a
// struct or a map of fields. Marshal updates the storage version to the latest.
func (ld *LevelDat) Marshal(src any) error {
	var err error
	ld.data, err = nbt.MarshalEncoding(src, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("level.dat: encode nbt: %w", err)
	}
	ld.hdr = header{
		StorageVersion: Version,
		FileLength:     int32(len(ld.data)),
	}
	return nil
}

// Write writes ld to w.
func (ld *LevelDat) Write(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, ld.hdr); err != nil {
		return fmt.Errorf("level.dat: write header: %w", err)
	}
	if _, err := w.Write(ld.data); err != nil {
		return fmt.Errorf("level.dat: write data: %w", err)
	}
	return nil
}

// WriteFile writes ld to a file at name.
func (ld *LevelDat) WriteFile(name string) error {
	// Which input was the problem: a crash or a failed flush mid-write. The
	// file was opened with O_TRUNC and flush/close errors were discarded, so
	// either left a torn level.dat behind and the next open failed on it. The
	// data is written to a temp file in the same directory, synced to disk
	// and renamed over the target, so the target is always either the old or
	// the new level.dat, never a partial one.
	tmp, err := os.CreateTemp(filepath.Dir(name), ".level.dat.*.tmp")
	if err != nil {
		return fmt.Errorf("level.dat: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// A failed write below returns before the rename: remove the temp file so
	// no stray temp accumulates. After a successful rename there is nothing
	// left to remove, and Remove errors only when the file is already gone.
	defer func() { _ = os.Remove(tmpName) }()
	w := bufio.NewWriter(tmp)
	// The Close errors below are discarded because the write, flush or sync
	// failure beside each of them is the error reported: closing a file that
	// failed to write can only repeat that failure.
	if err := ld.Write(w); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("level.dat: flush file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("level.dat: sync file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("level.dat: close file: %w", err)
	}
	if err := os.Rename(tmpName, name); err != nil {
		return fmt.Errorf("level.dat: rename file: %w", err)
	}
	return nil
}
