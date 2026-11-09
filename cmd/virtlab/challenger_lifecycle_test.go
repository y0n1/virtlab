package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

// TestChallenger_TeardownErrorStagesPreservesCredentialCleanup verifies that if earlier teardown stages
// (infrastructure, host, or workstation) fail, stage 4 (credential cleanup) still executes.
func TestChallenger_TeardownErrorStagesPreservesCredentialCleanup(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	mockErr := errors.New("simulated earlier teardown failure")
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error { return mockErr }
	hostTeardownFn = func(cfg *config.Config) error { return mockErr }
	wsTeardownFn = func(cfg *config.Config) error { return mockErr }

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	kubeDir := filepath.Join(fakeHome, ".kube")
	if err := os.MkdirAll(kubeDir, 0700); err != nil {
		t.Fatalf("failed to create temp kubeDir: %v", err)
	}

	clusterName := "fail-earlier-stages"
	configPath := filepath.Join(kubeDir, fmt.Sprintf("config.%s", clusterName))
	passwordPath := filepath.Join(kubeDir, fmt.Sprintf("kubeadmin-password.%s", clusterName))

	// Pre-create credentials
	if err := os.WriteFile(configPath, []byte("kc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordPath, []byte("pw"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{ClusterName: clusterName}
	err := runTeardownStages(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected teardown to fail due to earlier errors, got nil")
	}

	// CRITICAL SECURITY ASSERTION: Credential files MUST STILL BE REMOVED even on earlier failures
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("CRITICAL: kubeconfig %s was not cleaned up when earlier teardown stages failed!", configPath)
	}
	if _, err := os.Stat(passwordPath); !os.IsNotExist(err) {
		t.Errorf("CRITICAL: password %s was not cleaned up when earlier teardown stages failed!", passwordPath)
	}
}

// TestChallenger_TeardownReadOnlyFiles verifies that read-only (0400) credential files are successfully removed.
func TestChallenger_TeardownReadOnlyFiles(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error { return nil }
	hostTeardownFn = func(cfg *config.Config) error { return nil }
	wsTeardownFn = func(cfg *config.Config) error { return nil }

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	kubeDir := filepath.Join(fakeHome, ".kube")
	_ = os.MkdirAll(kubeDir, 0700)

	clusterName := "readonly-creds"
	configPath := filepath.Join(kubeDir, fmt.Sprintf("config.%s", clusterName))
	passwordPath := filepath.Join(kubeDir, fmt.Sprintf("kubeadmin-password.%s", clusterName))

	_ = os.WriteFile(configPath, []byte("kc"), 0400)
	_ = os.WriteFile(passwordPath, []byte("pw"), 0400)

	cfg := &config.Config{ClusterName: clusterName}
	if err := runTeardownStages(context.Background(), cfg); err != nil {
		t.Fatalf("runTeardownStages failed: %v", err)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("read-only config file was not removed: %v", err)
	}
	if _, err := os.Stat(passwordPath); !os.IsNotExist(err) {
		t.Errorf("read-only password file was not removed: %v", err)
	}
}

// TestChallenger_StatusCredentialReporting tests runStatus output across various credential presence states.
func TestChallenger_StatusCredentialReporting(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	kubeDir := filepath.Join(fakeHome, ".kube")
	_ = os.MkdirAll(kubeDir, 0700)

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")
	pullSecret := filepath.Join(tempDir, "pull-secret.json")
	_ = os.WriteFile(pullSecret, []byte(`{"auths":{}}`), 0600)
	sshKey := filepath.Join(tempDir, "id_rsa.pub")
	_ = os.WriteFile(sshKey, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA test@virtlab"), 0600)

	cfgContent := fmt.Sprintf(`clusterName: status-lab
baseDomain: example.com
preset: compact
ocpVersion: 4.20.0
pullSecretFile: %s
hostSsh:
  host: 192.168.1.100
  user: root
  publicKeyFile: %s
network:
  name: test-net
  cidr: 192.168.100.0/24
`, pullSecret, sshKey)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	captureStatus := func() string {
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := runStatus(context.Background(), []string{"--config", cfgPath})

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if err != nil {
			t.Logf("runStatus error: %v", err)
		}
		return buf.String()
	}

	// State 1: No credentials present
	out1 := captureStatus()
	if !strings.Contains(out1, "• Kubeconfig:             Not synced yet") {
		t.Errorf("State 1: expected kubeconfig Not synced yet, got:\n%s", out1)
	}
	if !strings.Contains(out1, "• Kubeadmin Password:     Not synced yet") {
		t.Errorf("State 1: expected password Not synced yet, got:\n%s", out1)
	}
	// Verify zero plaintext secrets
	if strings.Contains(out1, "••••••••") || strings.Contains(out1, "admin") && !strings.Contains(out1, "Kubeadmin") {
		t.Errorf("State 1: unexpected secret pattern in status:\n%s", out1)
	}

	// State 2: Both credentials present
	pwPath := filepath.Join(kubeDir, "kubeadmin-password.status-lab")
	kcPath := filepath.Join(kubeDir, "config.status-lab")
	_ = os.WriteFile(pwPath, []byte("secret-pw-never-leak\n"), 0600)
	_ = os.WriteFile(kcPath, []byte("fake-kubeconfig\n"), 0600)

	out2 := captureStatus()
	if !strings.Contains(out2, "• Kubeconfig:             Found at "+kcPath) {
		t.Errorf("State 2: expected kubeconfig found, got:\n%s", out2)
	}
	if !strings.Contains(out2, "• Kubeadmin Password:     Saved at "+pwPath+" (0600)") {
		t.Errorf("State 2: expected password saved, got:\n%s", out2)
	}
	// CRITICAL ASSERTION: The plaintext password MUST NOT be printed in status output!
	if strings.Contains(out2, "secret-pw-never-leak") {
		t.Fatalf("CRITICAL SECURITY DEFECT: status printed plaintext password in output:\n%s", out2)
	}

	// State 3: Password file has non-standard permission (e.g. 0644) - must report actual octal mode
	_ = os.Chmod(pwPath, 0644)
	out3 := captureStatus()
	if !strings.Contains(out3, "• Kubeadmin Password:     Saved at "+pwPath+" (0644)") {
		t.Errorf("State 3: expected dynamic permission (0644), got:\n%s", out3)
	}
}
