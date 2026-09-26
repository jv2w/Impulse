package mcdb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
)

// TestCloseFailingSaveStillCloses fixes that a failed save on Close does not
// leak the database: the level.dat write below is blocked, so Close reports
// an error, but the leveldb handle must be closed anyway. Before the fix
// Close returned early, keeping the handle and its file lock: the next open
// of the same directory failed and the process leaked the handle on every
// shutdown with a full disk.
func TestCloseFailingSaveStillCloses(t *testing.T) {
	dir := t.TempDir()
	db := newDBAt(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "level.dat"), 0755); err != nil {
		t.Fatalf("block level.dat: %v", err)
	}

	if err := db.Close(); err == nil {
		t.Fatal("Close with a blocked level.dat succeeded, want an error")
	}
	_, _, err := db.LoadPlayerSpawnPosition(playerA)
	if !errors.Is(err, leveldb.ErrClosed) {
		t.Fatalf("load after failed Close: err = %v, want it to wrap leveldb.ErrClosed", err)
	}
}
