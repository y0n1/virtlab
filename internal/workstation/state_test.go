package workstation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateManager_LoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	sm := NewStateManager(statePath)
	state, err := sm.Load()
	if err != nil {
		t.Fatalf("expected no error loading non-existent file, got %v", err)
	}

	if !state.IsEmpty() {
		t.Errorf("expected empty state, got %+v", state)
	}
	if state.Version != 1 {
		t.Errorf("expected version 1, got %d", state.Version)
	}
}

func TestStateManager_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "sub", "state.json")

	sm := NewStateManager(statePath)
	state := NewEmptyState()
	state.ClusterName = "lab"
	state.BaseDomain = "virtlab.internal"
	state.RecordInstalled("ocm", "brew", "/opt/homebrew/bin/ocm")

	if err := sm.Save(state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file to exist at %s: %v", statePath, err)
	}

	// Load back
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if loaded.ClusterName != "lab" {
		t.Errorf("expected clusterName 'lab', got '%s'", loaded.ClusterName)
	}
	if !loaded.IsInstalledByVirtlab("ocm") {
		t.Errorf("expected ocm to be marked as installed by virtlab")
	}
	if loaded.IsInstalledByVirtlab("sshuttle") {
		t.Errorf("expected sshuttle to NOT be marked as installed by virtlab")
	}

	// Unmark ocm
	loaded.RecordUninstalled("ocm")
	if loaded.IsInstalledByVirtlab("ocm") {
		t.Errorf("expected ocm to be unrecorded")
	}

	// Remove state file
	if err := sm.Remove(); err != nil {
		t.Fatalf("failed to remove state: %v", err)
	}

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("expected state file to be removed")
	}
}

func TestDefaultStateFilePath_XDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/xdg/state")
	path := DefaultStateFilePath()
	expected := "/custom/xdg/state/virtlab/state.json"
	if path != expected {
		t.Errorf("expected '%s', got '%s'", expected, path)
	}
}
