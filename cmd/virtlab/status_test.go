package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
