package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func TestRunTeardownStages_AllSuccess(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	var infraCalled, hostCalled, wsCalled bool
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		infraCalled = true
		return nil
	}
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		hostCalled = true
		return nil
	}
	wsTeardownFn = func(cfg *config.Config) error {
		wsCalled = true
		return nil
	}

	cfg := &config.Config{ClusterName: "test-all-success"}
	err := runTeardownStages(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected nil error on all success, got: %v", err)
	}

	if !infraCalled || !hostCalled || !wsCalled {
		t.Errorf("expected all stages called; got infra=%v, host=%v, ws=%v", infraCalled, hostCalled, wsCalled)
	}
}

func TestRunTeardownStages_InfraFailure(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	errInfra := errors.New("simulated pulumi destroy failure")
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		return errInfra
	}
	var hostCalled, wsCalled bool
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		hostCalled = true
		return nil
	}
	wsTeardownFn = func(cfg *config.Config) error {
		wsCalled = true
		return nil
	}

	cfg := &config.Config{ClusterName: "demo-fail"}
	err := runTeardownStages(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected non-nil error when infra fails, got nil")
	}

	if !errors.Is(err, errInfra) {
		t.Errorf("expected error to wrap infra failure %v, got: %v", errInfra, err)
	}
	if !strings.Contains(err.Error(), "infrastructure teardown") {
		t.Errorf("expected error message to mention 'infrastructure teardown', got: %v", err)
	}

	// Downstream stages must still be called for best-effort cleanup
	if !hostCalled || !wsCalled {
		t.Errorf("expected best-effort continuation; got hostCalled=%v, wsCalled=%v", hostCalled, wsCalled)
	}
}

func TestRunTeardownStages_HostFailure(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	errHost := errors.New("simulated ssh failure during host teardown")
	var infraCalled, wsCalled bool
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		infraCalled = true
		return nil
	}
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		return errHost
	}
	wsTeardownFn = func(cfg *config.Config) error {
		wsCalled = true
		return nil
	}

	cfg := &config.Config{ClusterName: "test-host-fail"}
	err := runTeardownStages(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected non-nil error when host fails, got nil")
	}

	if !errors.Is(err, errHost) {
		t.Errorf("expected error to wrap host failure %v, got: %v", errHost, err)
	}
	if !strings.Contains(err.Error(), "host teardown") {
		t.Errorf("expected error message to mention 'host teardown', got: %v", err)
	}

	if !infraCalled || !wsCalled {
		t.Errorf("expected infra and ws to be called; got infra=%v, ws=%v", infraCalled, wsCalled)
	}
}

func TestRunTeardownStages_WorkstationFailure(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	errWs := errors.New("simulated tunnel stop failure")
	var infraCalled, hostCalled bool
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		infraCalled = true
		return nil
	}
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		hostCalled = true
		return nil
	}
	wsTeardownFn = func(cfg *config.Config) error {
		return errWs
	}

	cfg := &config.Config{ClusterName: "test-ws-fail"}
	err := runTeardownStages(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected non-nil error when workstation fails, got nil")
	}

	if !errors.Is(err, errWs) {
		t.Errorf("expected error to wrap workstation failure %v, got: %v", errWs, err)
	}
	if !strings.Contains(err.Error(), "workstation teardown") {
		t.Errorf("expected error message to mention 'workstation teardown', got: %v", err)
	}

	if !infraCalled || !hostCalled {
		t.Errorf("expected infra and host to be called; got infra=%v, host=%v", infraCalled, hostCalled)
	}
}

func TestRunTeardownStages_MultipleFailuresAggregated(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	errInfra := errors.New("simulated infra error")
	errHost := errors.New("simulated host error")
	errWs := errors.New("simulated ws error")

	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		return errInfra
	}
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		return errHost
	}
	wsTeardownFn = func(cfg *config.Config) error {
		return errWs
	}

	cfg := &config.Config{ClusterName: "test-multi-fail"}
	err := runTeardownStages(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	// Verify all three errors are wrapped and detectable by errors.Is
	if !errors.Is(err, errInfra) {
		t.Errorf("expected joined error to wrap errInfra; got %v", err)
	}
	if !errors.Is(err, errHost) {
		t.Errorf("expected joined error to wrap errHost; got %v", err)
	}
	if !errors.Is(err, errWs) {
		t.Errorf("expected joined error to wrap errWs; got %v", err)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "infrastructure teardown") ||
		!strings.Contains(errMsg, "host teardown") ||
		!strings.Contains(errMsg, "workstation teardown") {
		t.Errorf("expected combined error message to mention all failed stages, got: %v", errMsg)
	}
}

func TestRunDown_IntegrationSeams(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	// Create a minimal valid config file for testing
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)
	pubKeyPath := filepath.Join(tmpDir, "id_ed25519.pub")
	if err := os.WriteFile(pubKeyPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleValidKeyFormatForTestsOnlyvirtlab test@host\n"), 0600); err != nil {
		t.Fatalf("failed to write pub key: %v", err)
	}

	pullSecretPath := filepath.Join(tmpDir, "pull-secret.json")
	if err := os.WriteFile(pullSecretPath, []byte(`{"auths":{"cloud.openshift.com":{"auth":"dGVzdDp0ZXN0","email":"test@example.com"}}}`), 0600); err != nil {
		t.Fatalf("failed to write pull secret: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := fmt.Sprintf(`clusterName: test-rundown
baseDomain: example.com
preset: sno
ocpVersion: "4.22.0"
pullSecretFile: %s
hostSsh:
  user: root
  host: 127.0.0.1
  publicKeyFile: %s
network:
  name: virbr-test
  cidr: 192.168.100.0/24
`, pullSecretPath, pubKeyPath)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// 1. Success case
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error { return nil }
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error { return nil }
	wsTeardownFn = func(cfg *config.Config) error { return nil }

	err := runDown(context.Background(), []string{"--yes", "--config", cfgPath})
	if err != nil {
		t.Fatalf("expected runDown to succeed with mocked seams, got: %v", err)
	}

	// 2. Failure case
	mockErr := errors.New("infra failed in runDown")
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error { return mockErr }

	err = runDown(context.Background(), []string{"--yes", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected runDown to return non-nil error when infra fails, got nil")
	}
	if !errors.Is(err, mockErr) {
		t.Errorf("expected runDown error to wrap %v, got: %v", mockErr, err)
	}
}

func TestRunTeardownStages_CleansUpCredentials(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error { return nil }
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error { return nil }
	wsTeardownFn = func(cfg *config.Config) error { return nil }

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	kubeDir := filepath.Join(fakeHome, ".kube")
	if err := os.MkdirAll(kubeDir, 0700); err != nil {
		t.Fatalf("failed to create temp kubeDir: %v", err)
	}

	clusterName := "test-cleanup-creds"
	configPath := filepath.Join(kubeDir, fmt.Sprintf("config.%s", clusterName))
	passwordPath := filepath.Join(kubeDir, fmt.Sprintf("kubeadmin-password.%s", clusterName))

	if err := os.WriteFile(configPath, []byte("fake-kubeconfig"), 0600); err != nil {
		t.Fatalf("failed to write fake kubeconfig: %v", err)
	}
	if err := os.WriteFile(passwordPath, []byte("fake-password"), 0600); err != nil {
		t.Fatalf("failed to write fake password: %v", err)
	}

	cfg := &config.Config{ClusterName: clusterName}
	if err := runTeardownStages(context.Background(), cfg); err != nil {
		t.Fatalf("runTeardownStages failed: %v", err)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to be deleted after teardown, but it still exists", configPath)
	}
	if _, err := os.Stat(passwordPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to be deleted after teardown, but it still exists", passwordPath)
	}
}

func TestRunTeardownStages_HostTeardownReceivesContext(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	var receivedCtx context.Context
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error { return nil }
	hostTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		receivedCtx = ctx
		return nil
	}
	wsTeardownFn = func(cfg *config.Config) error { return nil }

	type testContextKey string
	expectedKey := testContextKey("test-key")
	expectedVal := "test-val"
	testCtx := context.WithValue(context.Background(), expectedKey, expectedVal)

	cfg := &config.Config{ClusterName: "test-ctx-prop"}
	if err := runTeardownStages(testCtx, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedCtx == nil {
		t.Fatal("expected hostTeardownFn to receive non-nil context")
	}
	if receivedCtx.Value(expectedKey) != expectedVal {
		t.Errorf("expected context value %q, got: %v", expectedVal, receivedCtx.Value(expectedKey))
	}
}
