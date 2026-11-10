package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUp_RequiresFullValidation_MissingPullSecret(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	pubKeyPath := filepath.Join(tmpDir, "id_ed25519.pub")
	if err := os.WriteFile(pubKeyPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleValidKeyFormatForTestsOnlyvirtlab test@host\n"), 0600); err != nil {
		t.Fatalf("failed to write pub key: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-up-pullsecret
baseDomain: example.com
ocpVersion: "4.22.0"
pullSecretFile: /nonexistent/pull-secret.json
hostSsh:
  user: root
  host: 127.0.0.1
  publicKeyFile: ` + pubKeyPath + `
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := runUp(context.Background(), []string{"--yes", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected runUp to reject nonexistent pull secret, but got nil")
	}
	if !strings.Contains(err.Error(), "pullSecretFile") {
		t.Errorf("expected error to mention pullSecretFile, got: %v", err)
	}
}

func TestRunUp_RequiresFullValidation_MissingPublicKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-up-pubkey
baseDomain: example.com
ocpVersion: "4.22.0"
hostSsh:
  user: root
  host: 127.0.0.1
  publicKeyFile: /nonexistent/missing_key.pub
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := runUp(context.Background(), []string{"--yes", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected runUp to reject nonexistent public key, but got nil")
	}
	if !strings.Contains(err.Error(), "hostSsh.publicKeyFile") {
		t.Errorf("expected error to mention hostSsh.publicKeyFile, got: %v", err)
	}
}

func TestRunUp_RequiresFullValidation_UnsupportedOCPVersion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	pubKeyPath := filepath.Join(tmpDir, "id_ed25519.pub")
	if err := os.WriteFile(pubKeyPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleValidKeyFormatForTestsOnlyvirtlab test@host\n"), 0600); err != nil {
		t.Fatalf("failed to write pub key: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-up-ocpver
baseDomain: example.com
ocpVersion: "4.19.0"
hostSsh:
  user: root
  host: 127.0.0.1
  publicKeyFile: ` + pubKeyPath + `
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := runUp(context.Background(), []string{"--yes", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected runUp to reject unsupported OCP version, but got nil")
	}
	if !strings.Contains(err.Error(), "ocpVersion") {
		t.Errorf("expected error to mention ocpVersion, got: %v", err)
	}
}

func TestRunUp_RequiresFullValidation_ReservedClusterName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	pubKeyPath := filepath.Join(tmpDir, "id_ed25519.pub")
	if err := os.WriteFile(pubKeyPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleValidKeyFormatForTestsOnlyvirtlab test@host\n"), 0600); err != nil {
		t.Fatalf("failed to write pub key: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: default
baseDomain: example.com
ocpVersion: "4.22.0"
hostSsh:
  user: root
  host: 127.0.0.1
  publicKeyFile: ` + pubKeyPath + `
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := runUp(context.Background(), []string{"--yes", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected runUp to reject reserved clusterName 'default', but got nil")
	}
	if !strings.Contains(err.Error(), "clusterName 'default' is reserved") {
		t.Errorf("expected error to mention reserved default clusterName, got: %v", err)
	}
}
