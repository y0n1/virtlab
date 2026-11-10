package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/workstation"
)

func TestRunStatus_TieredValidation_SucceedsWithoutPullSecretOrKeys(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	// Config with unsupported OCP version, missing pull secret file, and missing public key file
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-tier-status
baseDomain: example.com
ocpVersion: "4.19.0"
pullSecretFile: /nonexistent/path/pull-secret.json
hostSsh:
  user: root
  host: 127.0.0.1
  publicKeyFile: /nonexistent/path/id_ed25519.pub
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// runStatus performs diagnostic inspection. Even if the host SSH connection fails,
	// runStatus should NOT fail at config loading.
	// Note: runStatus prints status and returns nil error even if hypervisor is unreachable.
	err := runStatus(context.Background(), []string{"--config", cfgPath})
	if err != nil {
		t.Fatalf("expected runStatus to succeed with valid identity/host fields, got: %v", err)
	}
}

func TestRunStatus_TieredValidation_RejectsInvalidIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: default
hostSsh:
  user: root
  host: 127.0.0.1
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := runStatus(context.Background(), []string{"--config", cfgPath})
	if err == nil {
		t.Fatal("expected runStatus to reject clusterName 'default', but got nil")
	}
	if !strings.Contains(err.Error(), "clusterName 'default' is reserved") {
		t.Errorf("expected error to mention reserved default clusterName, got: %v", err)
	}
}

func TestFormatWorkstationState_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "virtlab", "state.json")
	ws := workstation.New(tmpDir, statePath)

	t.Run("inactive tunnel reports Clean with no state file", func(t *testing.T) {
		got := formatWorkstationState(ws, false, 0)
		want := "Clean (no state file at " + statePath + ")"
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want substring %q", got, want)
		}
	})

	t.Run("active tunnel reports Untracked with no state file", func(t *testing.T) {
		got := formatWorkstationState(ws, true, 72997)
		want := "Untracked (no state file at " + statePath + "; active tunnel PID 72997)"
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want substring %q", got, want)
		}
		if strings.Contains(got, "Clean") {
			t.Errorf("should not report Clean when tunnel is active: %q", got)
		}
	})
}

func TestFormatWorkstationState_ExistingFile(t *testing.T) {
	t.Run("empty state file on disk and inactive tunnel reports Clean with path", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		sm := workstation.NewStateManager(statePath)
		if err := sm.Save(workstation.NewEmptyState()); err != nil {
			t.Fatalf("failed to save empty state: %v", err)
		}

		ws := workstation.New(tmpDir, statePath)
		got := formatWorkstationState(ws, false, 0)
		want := "Clean (" + statePath + ")"
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want substring %q", got, want)
		}
	})

	t.Run("empty state file on disk and active tunnel reports Untracked", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		sm := workstation.NewStateManager(statePath)
		if err := sm.Save(workstation.NewEmptyState()); err != nil {
			t.Fatalf("failed to save empty state: %v", err)
		}

		ws := workstation.New(tmpDir, statePath)
		got := formatWorkstationState(ws, true, 12345)
		want := "Untracked (empty state file at " + statePath + "; active tunnel PID 12345)"
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want substring %q", got, want)
		}
	})

	t.Run("tracked state reports Tracked at path and sorted tools", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		sm := workstation.NewStateManager(statePath)
		st := workstation.NewEmptyState()
		st.ClusterName = "lab"
		st.SplitDNSConfigured = true
		st.RecordInstalled("pulumi", "brew", "/opt/homebrew/bin/pulumi")
		st.RecordInstalled("ocm", "brew", "/opt/homebrew/bin/ocm")
		if err := sm.Save(st); err != nil {
			t.Fatalf("failed to save state: %v", err)
		}

		ws := workstation.New(tmpDir, statePath)
		got := formatWorkstationState(ws, true, 12345)
		if !strings.Contains(got, "Tracked at "+statePath) {
			t.Errorf("expected Tracked at %s, got %q", statePath, got)
		}
		if !strings.Contains(got, "Virtlab-managed tools: ocm (brew), pulumi (brew)") {
			t.Errorf("expected sorted tools list, got %q", got)
		}
	})

	t.Run("corrupt state file reports Unreadable", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		if err := os.WriteFile(statePath, []byte("{corrupt"), 0o600); err != nil {
			t.Fatalf("failed to write corrupt state: %v", err)
		}

		ws := workstation.New(tmpDir, statePath)
		got := formatWorkstationState(ws, false, 0)
		if !strings.Contains(got, "Unreadable at "+statePath) {
			t.Errorf("expected Unreadable at %s, got %q", statePath, got)
		}
	})
}

func TestRunStatus_MissingStateFile_ReportsNoStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-status-missing-state
baseDomain: example.com
hostSsh:
  user: root
  host: 127.0.0.1
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	runErr := runStatus(context.Background(), []string{"--config", cfgPath})
	_ = w.Close()
	outBytes, _ := io.ReadAll(r)
	_ = r.Close()

	if runErr != nil {
		t.Fatalf("runStatus returned error: %v", runErr)
	}

	expectedStatePath := filepath.Join(tmpDir, "virtlab", "state.json")
	if _, statErr := os.Stat(expectedStatePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected state file %s not to exist", expectedStatePath)
	}
	want := "Clean (no state file at " + expectedStatePath + ")"
	if !strings.Contains(string(outBytes), want) {
		t.Errorf("expected status output to contain %q, got:\n%s", want, string(outBytes))
	}
}
