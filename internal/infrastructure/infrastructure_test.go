package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

func TestNew_InitializesProjectName(t *testing.T) {
	cfg := &config.Config{ClusterName: "test-cluster"}
	infra := New(cfg, "virbr-test")
	if infra.ProjectName != "virtlab" {
		t.Errorf("expected infra.ProjectName to be 'virtlab', got '%s'", infra.ProjectName)
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

func TestNormalizeBackendURL(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	homeURL := "file://" + filepath.ToSlash(home)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"cloud trailing slash", "https://api.pulumi.com/", "https://api.pulumi.com"},
		{"absolute local", homeURL, homeURL},
		{"local tilde", "file://~", homeURL},
		{"local tilde slash", "file://~/", homeURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBackendURL(tt.in); got != tt.want {
				t.Errorf("normalizeBackendURL(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestActiveBackendURL(t *testing.T) {
	t.Run("environment wins", func(t *testing.T) {
		t.Setenv("PULUMI_BACKEND_URL", "https://api.pulumi.example/")
		stubStoredCredentials(t, workspace.Credentials{Current: "file:///stored"}, nil)
		if got := ActiveBackendURL(); got != "https://api.pulumi.example" {
			t.Errorf("ActiveBackendURL() = %q; want %q", got, "https://api.pulumi.example")
		}
	})

	t.Run("stored login is used when environment is unset", func(t *testing.T) {
		t.Setenv("PULUMI_BACKEND_URL", "")
		stubStoredCredentials(t, workspace.Credentials{Current: "file:///stored/"}, nil)
		if got := ActiveBackendURL(); got != "file:///stored" {
			t.Errorf("ActiveBackendURL() = %q; want %q", got, "file:///stored")
		}
	})

	t.Run("credentials error yields empty", func(t *testing.T) {
		t.Setenv("PULUMI_BACKEND_URL", "")
		stubStoredCredentials(t, workspace.Credentials{}, errors.New("unreadable"))
		if got := ActiveBackendURL(); got != "" {
			t.Errorf("ActiveBackendURL() = %q; want empty", got)
		}
	})
}

func TestValidateStackIdentity(t *testing.T) {
	const (
		uriA     = "qemu+ssh://root@host-a/system"
		uriB     = "qemu+ssh://root@host-b/system"
		backendA = "https://api.pulumi.example"
		backendB = "s3://other-bucket/virtlab"
	)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	homeURL := "file://" + filepath.ToSlash(home)

	tests := []struct {
		name        string
		stored      auto.ConfigMap
		wantURI     string
		wantBackend string
		wantErr     bool
	}{
		{
			name:        "matching host and backend",
			stored:      auto.ConfigMap{libvirtURIConfigKey: {Value: uriA}, backendConfigKey: {Value: backendA}},
			wantURI:     uriA,
			wantBackend: backendA,
		},
		{
			name:        "different host",
			stored:      auto.ConfigMap{libvirtURIConfigKey: {Value: uriA}, backendConfigKey: {Value: backendA}},
			wantURI:     uriB,
			wantBackend: backendA,
			wantErr:     true,
		},
		{
			name:        "different backend",
			stored:      auto.ConfigMap{libvirtURIConfigKey: {Value: uriA}, backendConfigKey: {Value: backendA}},
			wantURI:     uriA,
			wantBackend: backendB,
			wantErr:     true,
		},
		{
			name:        "legacy stack without identity",
			stored:      auto.ConfigMap{},
			wantURI:     uriA,
			wantBackend: backendA,
		},
		{
			name:        "unknown active backend does not fail",
			stored:      auto.ConfigMap{libvirtURIConfigKey: {Value: uriA}, backendConfigKey: {Value: backendA}},
			wantURI:     uriA,
			wantBackend: "",
		},
		{
			name:        "equivalent local backend spellings",
			stored:      auto.ConfigMap{libvirtURIConfigKey: {Value: uriA}, backendConfigKey: {Value: homeURL}},
			wantURI:     uriA,
			wantBackend: "file://~/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStackIdentity("lab", tt.stored, tt.wantURI, tt.wantBackend)
			if tt.wantErr {
				if !errors.Is(err, ErrStackIdentityMismatch) {
					t.Fatalf("expected ErrStackIdentityMismatch, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestInfrastructure_StackHasState(t *testing.T) {
	origSelect, origIs404 := selectStackInlineSourceFn, isSelectStack404ErrorFn
	origConfig := getStackAllConfigFn
	defer func() {
		selectStackInlineSourceFn = origSelect
		isSelectStack404ErrorFn = origIs404
		getStackAllConfigFn = origConfig
	}()

	sentinel404 := errors.New("stack not found")
	sentinelOther := errors.New("backend unreachable")

	cfg := &config.Config{ClusterName: "stack-exists-lab", Preset: config.PresetSNO}
	infra := New(cfg, "virbr-test")

	selectPresent := func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
		return auto.Stack{}, nil
	}

	t.Run("present with recorded host", func(t *testing.T) {
		selectStackInlineSourceFn = selectPresent
		getStackAllConfigFn = func(context.Context, auto.Stack) (auto.ConfigMap, error) {
			return auto.ConfigMap{libvirtURIConfigKey: {Value: "qemu+ssh://root@host/system"}}, nil
		}
		hasState, err := infra.StackHasState(context.Background())
		if err != nil || !hasState {
			t.Fatalf("StackHasState() = (%v, %v); want (true, nil)", hasState, err)
		}
	})

	t.Run("present but empty", func(t *testing.T) {
		selectStackInlineSourceFn = selectPresent
		getStackAllConfigFn = func(context.Context, auto.Stack) (auto.ConfigMap, error) {
			return auto.ConfigMap{}, nil
		}
		hasState, err := infra.StackHasState(context.Background())
		if err != nil || hasState {
			t.Fatalf("StackHasState() = (%v, %v); want (false, nil)", hasState, err)
		}
	})

	t.Run("present with exported deployment and empty workspace config", func(t *testing.T) {
		origExport := exportStackFn
		defer func() { exportStackFn = origExport }()

		depBytes, err := json.Marshal(apitype.DeploymentV3{
			Resources: []apitype.ResourceV3{
				{Type: "pulumi:pulumi:Stack"},
				{
					Type:   "pulumi:providers:libvirt",
					Inputs: map[string]any{"uri": "qemu+ssh://root@host/system"},
				},
				{Type: "libvirt:index/pool:Pool"},
			},
		})
		if err != nil {
			t.Fatalf("marshal deployment: %v", err)
		}

		selectStackInlineSourceFn = selectPresent
		exportStackFn = func(context.Context, auto.Stack) (apitype.UntypedDeployment, error) {
			return apitype.UntypedDeployment{Version: 3, Deployment: depBytes}, nil
		}
		getStackAllConfigFn = func(context.Context, auto.Stack) (auto.ConfigMap, error) {
			return auto.ConfigMap{}, nil
		}

		hasState, err := infra.StackHasState(context.Background())
		if err != nil || !hasState {
			t.Fatalf("StackHasState() = (%v, %v); want (true, nil)", hasState, err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		selectStackInlineSourceFn = func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
			return auto.Stack{}, sentinel404
		}
		isSelectStack404ErrorFn = func(err error) bool { return errors.Is(err, sentinel404) }
		hasState, err := infra.StackHasState(context.Background())
		if err != nil || hasState {
			t.Fatalf("StackHasState() = (%v, %v); want (false, nil)", hasState, err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		selectStackInlineSourceFn = func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
			return auto.Stack{}, sentinelOther
		}
		isSelectStack404ErrorFn = func(error) bool { return false }
		hasState, err := infra.StackHasState(context.Background())
		if err == nil || hasState {
			t.Fatalf("StackHasState() = (%v, %v); want (false, non-nil error)", hasState, err)
		}
		if !strings.Contains(err.Error(), "select stack") {
			t.Errorf("expected error to mention select stack, got %v", err)
		}
	})
}

func TestInfrastructure_Teardown_IdentityMismatchSkipsDestroy(t *testing.T) {
	origSelect, origIs404 := selectStackInlineSourceFn, isSelectStack404ErrorFn
	origConfig, origDestroy := getStackAllConfigFn, destroyStackFn
	defer func() {
		selectStackInlineSourceFn = origSelect
		isSelectStack404ErrorFn = origIs404
		getStackAllConfigFn = origConfig
		destroyStackFn = origDestroy
	}()

	selectStackInlineSourceFn = func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
		return auto.Stack{}, nil
	}
	getStackAllConfigFn = func(context.Context, auto.Stack) (auto.ConfigMap, error) {
		return auto.ConfigMap{
			libvirtURIConfigKey: {Value: "qemu+ssh://root@original-host/system"},
		}, nil
	}
	destroyed := false
	destroyStackFn = func(context.Context, auto.Stack, ...optdestroy.Option) error {
		destroyed = true
		return nil
	}

	cfg := &config.Config{
		ClusterName: "identity-lab",
		HostSSH:     config.HostSSHConfig{User: "root", Host: "different-host"},
	}
	infra := New(cfg, "virbr-test")

	err := infra.Teardown(context.Background())
	if !errors.Is(err, ErrStackIdentityMismatch) {
		t.Fatalf("expected ErrStackIdentityMismatch, got %v", err)
	}
	if destroyed {
		t.Fatal("Destroy must not run when the stack belongs to a different Host")
	}
	if !strings.Contains(err.Error(), "original-host") || !strings.Contains(err.Error(), "different-host") {
		t.Errorf("expected error to name both Hosts, got %v", err)
	}
}

func TestInfrastructure_Teardown_IdentityMismatchFromExportedState(t *testing.T) {
	origSelect, origIs404 := selectStackInlineSourceFn, isSelectStack404ErrorFn
	origConfig, origExport, origDestroy := getStackAllConfigFn, exportStackFn, destroyStackFn
	defer func() {
		selectStackInlineSourceFn = origSelect
		isSelectStack404ErrorFn = origIs404
		getStackAllConfigFn = origConfig
		exportStackFn = origExport
		destroyStackFn = origDestroy
	}()

	depBytes, err := json.Marshal(apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				Type: "pulumi:pulumi:Stack",
				Outputs: map[string]any{
					libvirtURIOutputKey: "qemu+ssh://root@original-host/system",
					backendOutputKey:    "file:///Users/alice",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal deployment: %v", err)
	}

	selectStackInlineSourceFn = func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
		return auto.Stack{}, nil
	}
	exportStackFn = func(context.Context, auto.Stack) (apitype.UntypedDeployment, error) {
		return apitype.UntypedDeployment{Version: 3, Deployment: depBytes}, nil
	}
	getStackAllConfigFn = func(context.Context, auto.Stack) (auto.ConfigMap, error) {
		return auto.ConfigMap{}, nil
	}
	destroyed := false
	destroyStackFn = func(context.Context, auto.Stack, ...optdestroy.Option) error {
		destroyed = true
		return nil
	}

	cfg := &config.Config{
		ClusterName: "identity-lab",
		HostSSH:     config.HostSSHConfig{User: "root", Host: "different-host"},
	}
	infra := New(cfg, "virbr-test")

	err = infra.Teardown(context.Background())
	if !errors.Is(err, ErrStackIdentityMismatch) {
		t.Fatalf("expected ErrStackIdentityMismatch, got %v", err)
	}
	if destroyed {
		t.Fatal("Destroy must not run when exported deployment belongs to a different Host")
	}
}

func TestInfrastructure_Teardown_MatchingIdentityDestroys(t *testing.T) {
	origSelect, origIs404 := selectStackInlineSourceFn, isSelectStack404ErrorFn
	origConfig, origDestroy := getStackAllConfigFn, destroyStackFn
	defer func() {
		selectStackInlineSourceFn = origSelect
		isSelectStack404ErrorFn = origIs404
		getStackAllConfigFn = origConfig
		destroyStackFn = origDestroy
	}()

	uri := "qemu+ssh://root@original-host/system"
	selectStackInlineSourceFn = func(context.Context, string, string, pulumi.RunFunc, ...auto.LocalWorkspaceOption) (auto.Stack, error) {
		return auto.Stack{}, nil
	}
	getStackAllConfigFn = func(context.Context, auto.Stack) (auto.ConfigMap, error) {
		return auto.ConfigMap{libvirtURIConfigKey: {Value: uri}}, nil
	}
	destroyed := false
	destroyStackFn = func(context.Context, auto.Stack, ...optdestroy.Option) error {
		destroyed = true
		return nil
	}

	cfg := &config.Config{
		ClusterName: "identity-lab",
		HostSSH:     config.HostSSHConfig{User: "root", Host: "original-host"},
	}
	infra := New(cfg, "virbr-test")

	if err := infra.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown() failed: %v", err)
	}
	if !destroyed {
		t.Fatal("Destroy was not called for a matching identity")
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
