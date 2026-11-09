package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/infrastructure"
)

func TestEnsureStackCoversHostResources(t *testing.T) {
	t.Setenv("PULUMI_BACKEND_URL", "file:///var/lib/virtlab-backend")

	t.Run("fresh lab", func(t *testing.T) {
		if err := ensureStackCoversHostResources(false, false, "fresh", "host-a"); err != nil {
			t.Fatalf("expected nil for a fresh lab, got %v", err)
		}
	})

	t.Run("resumed lab", func(t *testing.T) {
		if err := ensureStackCoversHostResources(true, true, "resumed", "host-a"); err != nil {
			t.Fatalf("expected nil when the stack exists, got %v", err)
		}
	})

	t.Run("missing stack with live host resources", func(t *testing.T) {
		err := ensureStackCoversHostResources(false, true, "orphaned", "host-a")
		if err == nil {
			t.Fatal("expected an error when resources exist without a stack")
		}
		msg := err.Error()
		for _, want := range []string{"host-a", "'orphaned'", "file:///var/lib/virtlab-backend", "restore the Pulumi state backup"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error message %q does not contain %q", msg, want)
			}
		}
	})
}

func TestRunTeardownStages_IdentityMismatchStopsBeforeHostCleanup(t *testing.T) {
	origInfra := infraTeardownFn
	origHost := hostTeardownFn
	origWs := wsTeardownFn
	defer func() {
		infraTeardownFn = origInfra
		hostTeardownFn = origHost
		wsTeardownFn = origWs
	}()

	infraTeardownFn = func(context.Context, *config.Config) error {
		return fmt.Errorf("infrastructure: %w: wrong host", infrastructure.ErrStackIdentityMismatch)
	}
	var hostCalled, wsCalled bool
	hostTeardownFn = func(context.Context, *config.Config) error {
		hostCalled = true
		return nil
	}
	wsTeardownFn = func(*config.Config) error {
		wsCalled = true
		return nil
	}

	cfg := &config.Config{ClusterName: "identity-mismatch"}
	err := runTeardownStages(context.Background(), cfg)
	if !errors.Is(err, infrastructure.ErrStackIdentityMismatch) {
		t.Fatalf("expected ErrStackIdentityMismatch, got %v", err)
	}

	// The configured Host is not the one that owns the lab. Cleanup there
	// would delete unrelated resources.
	if hostCalled || wsCalled {
		t.Errorf("expected host and workstation cleanup to be skipped; got host=%v, ws=%v", hostCalled, wsCalled)
	}
}
