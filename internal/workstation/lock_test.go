package workstation

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLock_AcquireAndRelease(t *testing.T) {
	tmpDir := t.TempDir()

	lock1, err := AcquireLock(tmpDir)
	if err != nil {
		t.Fatalf("expected to acquire lock1, got error: %v", err)
	}
	defer lock1.Release()

	// Attempting to acquire a second lock on the same directory must fail
	lock2, err := AcquireLock(tmpDir)
	if err == nil {
		lock2.Release()
		t.Fatalf("expected lock2 to fail while lock1 is held, but succeeded")
	}

	if !strings.Contains(err.Error(), "another virtlab operation is currently running") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Release lock1
	lock1.Release()

	// Acquiring lock should now succeed
	lock3, err := AcquireLock(tmpDir)
	if err != nil {
		t.Fatalf("expected to acquire lock3 after lock1 release, got: %v", err)
	}
	lock3.Release()
}

func TestTunnelManager_XDGPath(t *testing.T) {
	tm := NewTunnelManager()
	expectedDir := StateDir()
	if filepath.Dir(tm.PIDFilePath) != expectedDir {
		t.Errorf("expected PID file in %s, got %s", expectedDir, tm.PIDFilePath)
	}
}
