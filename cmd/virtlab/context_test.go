package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/y0n1/virtlab/internal/workstation"
)

func TestCLI_RunUp_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-cancel
baseDomain: example.com
network:
  name: test-net
  cidr: 192.168.100.0/24
hostSsh:
  host: 127.0.0.1
  user: root
  publicKeyFile: /dev/null
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	err := runUp(ctx, []string{"--config", cfgPath, "-y"})
	if err == nil {
		t.Fatal("expected runUp to fail on canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected error to indicate context cancellation, got: %v", err)
	}

	// Verify lock is NOT held (defer lock.Release executed)
	lock, lockErr := workstation.AcquireLock()
	if lockErr != nil {
		t.Fatalf("expected workstation lock to be released after runUp cancellation, but failed to acquire: %v", lockErr)
	}
	lock.Release()
}

func TestCLI_RunDown_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-down-cancel
baseDomain: example.com
network:
  name: test-net
  cidr: 192.168.100.0/24
hostSsh:
  host: 127.0.0.1
  user: root
  publicKeyFile: /dev/null
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	err := runDown(ctx, []string{"--config", cfgPath, "-y"})
	if err == nil {
		t.Fatal("expected runDown to fail on canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected error to indicate context cancellation, got: %v", err)
	}

	// Verify lock is NOT held (defer lock.Release executed)
	lock, lockErr := workstation.AcquireLock()
	if lockErr != nil {
		t.Fatalf("expected workstation lock to be released after runDown cancellation: %v", lockErr)
	}
	lock.Release()
}

func TestCLI_RunStatus_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `clusterName: test-status-cancel
baseDomain: example.com
network:
  name: test-net
  cidr: 192.168.100.0/24
hostSsh:
  host: 127.0.0.1
  user: root
  publicKeyFile: /dev/null
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	err := runStatus(ctx, []string{"--config", cfgPath})
	if err == nil {
		t.Fatal("expected runStatus to fail on canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected error to indicate context cancellation, got: %v", err)
	}
}

func TestCLI_SignalContext_Termination(t *testing.T) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		t.Fatal("context should not be done initially")
	default:
	}

	// Simulate interrupt signal to self
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Signal(os.Interrupt); err != nil {
		t.Skipf("cannot send signal in test environment: %v", err)
	}

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", ctx.Err())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("context was not canceled upon receiving os.Interrupt")
	}
}
