package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

func TestLoadProjectName(t *testing.T) {
	name := LoadProjectName()
	if name != "virtlab" {
		t.Errorf("expected 'virtlab', got '%s'", name)
	}
}

func TestNew_InitializesProjectName(t *testing.T) {
	cfg := &config.Config{ClusterName: "test-cluster"}
	infra := New(cfg, "virbr-test")
	if infra.ProjectName != "virtlab" {
		t.Errorf("expected infra.ProjectName to be 'virtlab', got '%s'", infra.ProjectName)
	}
}

func TestWorkspaceDetectProject(t *testing.T) {
	projPath, err := workspace.DetectProjectPath()
	if err != nil {
		t.Fatalf("DetectProjectPath failed: %v", err)
	}
	t.Logf("Detected project path: %s", projPath)

	proj, err := workspace.LoadProject(projPath)
	if err != nil {
		t.Fatalf("LoadProject failed: %v", err)
	}
	if string(proj.Name) != "virtlab" {
		t.Errorf("expected project name 'virtlab', got '%s'", proj.Name)
	}
}

func TestComputeNodeLeases_Presets(t *testing.T) {
	tests := []struct {
		preset        string
		wantCount     int
		wantFirstIP   string
		wantFirstName string
	}{
		{config.PresetSNO, 1, "192.168.100.10", "master-0"},
		{config.PresetCompact, 3, "192.168.100.10", "master-0"},
		{config.PresetHA, 5, "192.168.100.10", "master-0"},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			cfg := &config.Config{
				ClusterName: "test-leases",
				Preset:      tt.preset,
			}
			leases, err := ComputeNodeLeases(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(leases) != tt.wantCount {
				t.Errorf("want %d leases, got %d", tt.wantCount, len(leases))
			}
			if leases[0].IP != tt.wantFirstIP {
				t.Errorf("want first IP %s, got %s", tt.wantFirstIP, leases[0].IP)
			}
			if leases[0].Name != tt.wantFirstName {
				t.Errorf("want first name %s, got %s", tt.wantFirstName, leases[0].Name)
			}
		})
	}
}

func TestEnsurePulumiPassphrase(t *testing.T) {
	origPass, hadPass := os.LookupEnv("PULUMI_CONFIG_PASSPHRASE")
	origFile, hadFile := os.LookupEnv("PULUMI_CONFIG_PASSPHRASE_FILE")
	defer func() {
		if hadPass {
			_ = os.Setenv("PULUMI_CONFIG_PASSPHRASE", origPass)
		} else {
			_ = os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
		}
		if hadFile {
			_ = os.Setenv("PULUMI_CONFIG_PASSPHRASE_FILE", origFile)
		} else {
			_ = os.Unsetenv("PULUMI_CONFIG_PASSPHRASE_FILE")
		}
	}()

	// 1. Unset both -> should default to ""
	_ = os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
	_ = os.Unsetenv("PULUMI_CONFIG_PASSPHRASE_FILE")
	EnsurePulumiPassphrase()
	val, ok := os.LookupEnv("PULUMI_CONFIG_PASSPHRASE")
	if !ok || val != "" {
		t.Errorf("expected PULUMI_CONFIG_PASSPHRASE to be set to '', got ok=%v, val='%s'", ok, val)
	}

	// 2. Already set -> should preserve existing value
	_ = os.Setenv("PULUMI_CONFIG_PASSPHRASE", "custom-secret")
	EnsurePulumiPassphrase()
	if val := os.Getenv("PULUMI_CONFIG_PASSPHRASE"); val != "custom-secret" {
		t.Errorf("expected custom-secret to be preserved, got '%s'", val)
	}
}

func TestEnsurePulumiBackend_DefaultsToLocalBackend(t *testing.T) {
	t.Setenv("PULUMI_BACKEND_URL", "")
	t.Setenv("PULUMI_ACCESS_TOKEN", "")
	stubStoredCredentials(t, workspace.Credentials{}, nil)

	EnsurePulumiBackend()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	want := "file://" + filepath.ToSlash(home)
	if got := os.Getenv("PULUMI_BACKEND_URL"); got != want {
		t.Errorf("expected PULUMI_BACKEND_URL %q, got %q", want, got)
	}
}

func TestEnsurePulumiBackend_PreservesExplicitBackendURL(t *testing.T) {
	const explicit = "https://api.pulumi.example.com"
	t.Setenv("PULUMI_BACKEND_URL", explicit)

	EnsurePulumiBackend()

	if got := os.Getenv("PULUMI_BACKEND_URL"); got != explicit {
		t.Errorf("expected explicit backend %q to be preserved, got %q", explicit, got)
	}
}

func TestEnsurePulumiBackend_PreservesAccessTokenLogin(t *testing.T) {
	t.Setenv("PULUMI_BACKEND_URL", "")
	t.Setenv("PULUMI_ACCESS_TOKEN", "pul-test-token")

	EnsurePulumiBackend()

	if got := os.Getenv("PULUMI_BACKEND_URL"); got != "" {
		t.Errorf("expected no default when PULUMI_ACCESS_TOKEN is set, got %q", got)
	}
}

func TestEnsurePulumiBackend_PreservesStoredLogin(t *testing.T) {
	t.Setenv("PULUMI_BACKEND_URL", "")
	t.Setenv("PULUMI_ACCESS_TOKEN", "")
	stubStoredCredentials(t, workspace.Credentials{Current: "file:///home/other"}, nil)

	EnsurePulumiBackend()

	if got := os.Getenv("PULUMI_BACKEND_URL"); got != "" {
		t.Errorf("expected stored login to be preserved, got PULUMI_BACKEND_URL %q", got)
	}
}

func TestEnsurePulumiBackend_CredentialsReadErrorLeavesBackendUnset(t *testing.T) {
	t.Setenv("PULUMI_BACKEND_URL", "")
	t.Setenv("PULUMI_ACCESS_TOKEN", "")
	stubStoredCredentials(t, workspace.Credentials{}, errors.New("credentials unreadable"))

	EnsurePulumiBackend()

	if got := os.Getenv("PULUMI_BACKEND_URL"); got != "" {
		t.Errorf("expected no default when credentials cannot be read, got %q", got)
	}
}

// stubStoredCredentials replaces the credentials seam for the duration of a test.
func stubStoredCredentials(t *testing.T, creds workspace.Credentials, err error) {
	t.Helper()
	orig := getStoredCredentialsFn
	t.Cleanup(func() { getStoredCredentialsFn = orig })
	getStoredCredentialsFn = func() (workspace.Credentials, error) {
		return creds, err
	}
}

func TestInfrastructure_Teardown_NonExistentStack(t *testing.T) {
	origSelect, origIs404 := selectStackInlineSourceFn, isSelectStack404ErrorFn
	defer func() {
		selectStackInlineSourceFn = origSelect
		isSelectStack404ErrorFn = origIs404
	}()

	// Stub the Pulumi Automation API so the test stays hermetic: CI runners do
	// not have the pulumi CLI installed, and invoking it would make this test
	// depend on ambient tooling rather than Teardown's error handling.
	sentinel := errors.New("no stack named 'non-existent-cluster-unit-test-404' found")
	selectStackInlineSourceFn = func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
		return auto.Stack{}, sentinel
	}
	isSelectStack404ErrorFn = func(err error) bool { return errors.Is(err, sentinel) }

	cfg := &config.Config{
		ClusterName: "non-existent-cluster-unit-test-404",
	}
	infra := New(cfg, "virbr-test")

	// Destroying a non-existent stack should return nil because 404 is benign.
	if err := infra.Teardown(context.Background()); err != nil {
		t.Fatalf("expected nil when tearing down non-existent stack, got: %v", err)
	}
}

func TestInfrastructure_Teardown_ContextCanceledError(t *testing.T) {
	backendDir := t.TempDir()
	t.Setenv("PULUMI_BACKEND_URL", "file://"+backendDir)
	t.Setenv("PULUMI_CONFIG_PASSPHRASE", "")

	cfg := &config.Config{
		ClusterName: "non-existent-cluster-unit-test-cancel",
	}
	infra := New(cfg, "virbr-test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel context immediately

	err := infra.Teardown(ctx)
	if err == nil {
		t.Fatal("expected non-nil error when context is canceled, got nil")
	}
	if !strings.Contains(err.Error(), "select stack") {
		t.Errorf("expected error to wrap select stack failure, got: %v", err)
	}
}
