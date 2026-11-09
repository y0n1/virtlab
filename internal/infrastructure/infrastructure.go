package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

// ErrStackIdentityMismatch reports that the Pulumi stack state belongs to a
// different Host or Pulumi backend than the one the current command targets.
// Callers must stop before mutating state: applying or destroying such a stack
// can orphan running labs.
var ErrStackIdentityMismatch = errors.New("pulumi stack identity mismatch")

// Seams for unit tests. Production code delegates to the Pulumi Automation API.
var (
	selectStackInlineSourceFn = auto.SelectStackInlineSource
	isSelectStack404ErrorFn   = auto.IsSelectStack404Error
	getStoredCredentialsFn    = workspace.GetStoredCredentials
	getStackAllConfigFn       = func(ctx context.Context, stack auto.Stack) (auto.ConfigMap, error) {
		return stack.GetAllConfig(ctx)
	}
	destroyStackFn = func(ctx context.Context, stack auto.Stack, opts ...optdestroy.Option) error {
		_, err := stack.Destroy(ctx, opts...)
		return err
	}
)

// EnsurePulumiPassphrase ensures PULUMI_CONFIG_PASSPHRASE is defined in the environment.
// On local backends (file://~), Pulumi requires a passphrase to initialize stack encryption.
// If the operator hasn't explicitly set PULUMI_CONFIG_PASSPHRASE or PULUMI_CONFIG_PASSPHRASE_FILE,
// this defaults it to an empty passphrase ("") so unattended Automation API operations succeed.
func EnsurePulumiPassphrase() {
	if _, ok := os.LookupEnv("PULUMI_CONFIG_PASSPHRASE"); !ok {
		if _, okFile := os.LookupEnv("PULUMI_CONFIG_PASSPHRASE_FILE"); !okFile {
			_ = os.Setenv("PULUMI_CONFIG_PASSPHRASE", "")
		}
	}
}

// EnsurePulumiBackend defaults PULUMI_BACKEND_URL to the local file backend so a
// fresh workstation does not need `pulumi login --local` before `virtlab up` or
// `virtlab down`.
//
// The lab is local-only, but an operator may have deliberately selected another
// backend (via PULUMI_BACKEND_URL, PULUMI_ACCESS_TOKEN, or `pulumi login`), so the
// default must only apply when no backend is configured.
func EnsurePulumiBackend() {
	if os.Getenv("PULUMI_BACKEND_URL") != "" || os.Getenv("PULUMI_ACCESS_TOKEN") != "" {
		return
	}

	creds, err := getStoredCredentialsFn()
	if err != nil || creds.Current != "" {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_ = os.Setenv("PULUMI_BACKEND_URL", "file://"+filepath.ToSlash(home))
}

// pulumiProjectName is the fixed Pulumi project name for the inline program.
// The lab is driven exclusively through the Automation API, so project identity
// is a constant rather than a discovered Pulumi.yaml manifest.
const pulumiProjectName = "virtlab"

const (
	// libvirtURIConfigKey is the Pulumi config key that the vendored libvirt
	// provider reads. virtlab also uses it as the stack's Host identity: a
	// stack that was applied to one Host must never be applied to another.
	libvirtURIConfigKey = "libvirt:uri"

	// backendConfigKey records the Pulumi backend that owns the stack, so an
	// operation cannot silently run against a different backend.
	backendConfigKey = pulumiProjectName + ":backend"
)

// ActiveBackendURL returns the Pulumi backend URL that the current process will
// use: PULUMI_BACKEND_URL when set, otherwise the stored `pulumi login` target.
// It returns "" when neither is known, for example before EnsurePulumiBackend
// or `pulumi login` has selected one.
func ActiveBackendURL() string {
	if v := strings.TrimSpace(os.Getenv("PULUMI_BACKEND_URL")); v != "" {
		return normalizeBackendURL(v)
	}
	creds, err := getStoredCredentialsFn()
	if err != nil {
		return ""
	}
	return normalizeBackendURL(creds.Current)
}

// normalizeBackendURL makes equivalent spellings compare equal. In particular,
// the local backend may be written as file://~ or file:///home/<user>.
func normalizeBackendURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "file://") {
		path := strings.TrimPrefix(raw, "file://")
		if path == "~" || strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, strings.TrimPrefix(path, "~"))
			}
		}
		return "file://" + filepath.ToSlash(filepath.Clean(path))
	}
	return strings.TrimRight(raw, "/")
}

// describeLibvirtURI turns a libvirt connection URI into a short user@host form
// for error messages.
func describeLibvirtURI(uri string) string {
	trimmed := strings.TrimPrefix(uri, "qemu+ssh://")
	return strings.TrimSuffix(trimmed, "/system")
}

// validateStackIdentity refuses to use a stack whose recorded Host or backend
// differs from the current invocation. An empty recorded value means the stack
// predates identity tracking; it is accepted so existing labs keep working.
func validateStackIdentity(stackName string, stored auto.ConfigMap, wantURI, wantBackend string) error {
	storedURI := strings.TrimSpace(stored[libvirtURIConfigKey].Value)
	if storedURI != "" && wantURI != "" && storedURI != wantURI {
		return fmt.Errorf(
			"%w: Pulumi stack '%s' was created against Host '%s', but config.yaml targets '%s'; refusing to touch state across Hosts.\n"+
				"  - Use the original hostSsh.host (%s) in config.yaml, or\n"+
				"  - restore the stack state backup for the target Host (see docs/state.md#backup-and-restore)",
			ErrStackIdentityMismatch, stackName, describeLibvirtURI(storedURI), describeLibvirtURI(wantURI), describeLibvirtURI(storedURI))
	}

	storedBackend := normalizeBackendURL(stored[backendConfigKey].Value)
	activeBackend := normalizeBackendURL(wantBackend)
	if storedBackend != "" && activeBackend != "" && storedBackend != activeBackend {
		return fmt.Errorf(
			"%w: Pulumi stack '%s' is recorded on backend '%s', but the active backend is '%s'; refusing to use a different backend.\n"+
				"  - Set PULUMI_BACKEND_URL='%s' (or run `pulumi login` to that backend) and retry, or\n"+
				"  - restore the stack state into the active backend (see docs/state.md#backup-and-restore)",
			ErrStackIdentityMismatch, stackName, storedBackend, activeBackend, storedBackend)
	}

	return nil
}

// Infrastructure manages Phase 2 Lab Setup: storage pool, network, and guest VMs.
type Infrastructure struct {
	Config      *config.Config
	BridgeName  string
	ProjectName string
}

// New creates a new Infrastructure domain manager.
func New(cfg *config.Config, bridgeName string) *Infrastructure {
	if bridgeName == "" {
		bridgeName = "virbr-lab0"
	}
	return &Infrastructure{
		Config:      cfg,
		BridgeName:  bridgeName,
		ProjectName: pulumiProjectName,
	}
}

// InlineProgram defines the Pulumi program for all infrastructure resources.
func (infra *Infrastructure) InlineProgram() pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		// 1. Storage Pool (strictly at /home/libvirt/<clusterName>)
		pool, err := BuildStoragePool(ctx, infra.Config)
		if err != nil {
			return err
		}

		// 2. Network & Bridge
		net, err := BuildNetwork(ctx, infra.Config, infra.BridgeName)
		if err != nil {
			return err
		}

		// 3. Guest VMs & Volumes
		guests, err := BuildGuests(ctx, infra.Config, pool, net)
		if err != nil {
			return err
		}

		ctx.Export("clusterName", pulumi.String(infra.Config.ClusterName))
		ctx.Export("networkName", net.Name)
		ctx.Export("networkBridge", net.Bridge.Elem().Name())
		ctx.Export("poolName", pool.Name)
		ctx.Export("nodeCount", pulumi.Int(len(guests)))

		return nil
	}
}

// stackIdentity returns the stack and project names for the configured lab.
func (infra *Infrastructure) stackIdentity() (string, string) {
	projectName := infra.ProjectName
	if projectName == "" {
		projectName = pulumiProjectName
	}
	return infra.Config.ClusterName, projectName
}

// libvirtURI returns the libvirt connection URI for the configured Host. The
// user defaults to root to match config.applyDefaults.
func (infra *Infrastructure) libvirtURI() string {
	user := infra.Config.HostSSH.User
	if user == "" {
		user = "root"
	}
	return fmt.Sprintf("qemu+ssh://%s@%s/system", user, infra.Config.HostSSH.Host)
}

// workspaceOptions pins the passphrase that the Automation API needs for local
// stack encryption.
func workspaceOptions() []auto.LocalWorkspaceOption {
	return []auto.LocalWorkspaceOption{
		auto.EnvVars(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": os.Getenv("PULUMI_CONFIG_PASSPHRASE"),
		}),
	}
}

// StackHasState reports whether the Pulumi stack for this lab exists in the
// active backend and records the Host it was applied to. A missing or empty
// stack means virtlab has no record of the resources it created, so callers
// must not provision over live Host resources.
func (infra *Infrastructure) StackHasState(ctx context.Context) (bool, error) {
	stackName, projectName := infra.stackIdentity()

	EnsurePulumiPassphrase()

	stack, err := selectStackInlineSourceFn(ctx, stackName, projectName, infra.InlineProgram(), workspaceOptions()...)
	if err != nil {
		if isSelectStack404ErrorFn(err) {
			return false, nil
		}
		return false, fmt.Errorf("infrastructure: select stack '%s': %w", stackName, err)
	}

	stored, err := getStackAllConfigFn(ctx, stack)
	if err != nil {
		return false, fmt.Errorf("infrastructure: read stack config: %w", err)
	}
	return strings.TrimSpace(stored[libvirtURIConfigKey].Value) != "", nil
}

// Provision brings up the storage pool, network, and guest VMs using Pulumi Automation API.
//
// The stack records the Host it was applied to (libvirt:uri) and the backend
// that owns it. Provision refuses to continue when config.yaml or the active
// backend no longer match that record, because rewriting the URI would leave
// the old resources unreachable by Teardown.
func (infra *Infrastructure) Provision(ctx context.Context) error {
	stackName, projectName := infra.stackIdentity()

	EnsurePulumiPassphrase()

	fmt.Printf("\n[Infrastructure] Initializing Pulumi Automation stack '%s'...\n", stackName)
	stack, err := auto.UpsertStackInlineSource(ctx, stackName, projectName, infra.InlineProgram(), workspaceOptions()...)
	if err != nil {
		return fmt.Errorf("infrastructure: upsert stack: %w", err)
	}

	// Validate before writing anything: a stack bound to another Host or
	// backend must not be rewritten.
	stored, err := getStackAllConfigFn(ctx, stack)
	if err != nil {
		return fmt.Errorf("infrastructure: read stack config: %w", err)
	}
	libvirtURI := infra.libvirtURI()
	activeBackend := ActiveBackendURL()
	if err := validateStackIdentity(stackName, stored, libvirtURI, activeBackend); err != nil {
		return err
	}

	if err := stack.SetConfig(ctx, libvirtURIConfigKey, auto.ConfigValue{Value: libvirtURI}); err != nil {
		return fmt.Errorf("infrastructure: set libvirt URI config: %w", err)
	}
	if activeBackend != "" {
		if err := stack.SetConfig(ctx, backendConfigKey, auto.ConfigValue{Value: activeBackend}); err != nil {
			return fmt.Errorf("infrastructure: record Pulumi backend: %w", err)
		}
	}

	fmt.Printf("[Infrastructure] Applying infrastructure definitions (pool, network, %s guests)...\n", infra.Config.Preset)
	res, err := stack.Up(ctx, optup.ProgressStreams(os.Stdout))
	if err != nil {
		return fmt.Errorf("infrastructure: pulumi up failed: %w", err)
	}

	fmt.Printf("✔ Infrastructure setup complete: %s\n", res.Summary.Message)
	return nil
}

// Teardown destroys all infrastructure resources using Pulumi Automation API.
//
// Teardown never rewrites libvirt:uri. The stored value is the Host that owns
// the resources, so Destroy always targets that Host. A stack recorded for a
// different Host or backend than config.yaml targets is refused.
func (infra *Infrastructure) Teardown(ctx context.Context) error {
	stackName, projectName := infra.stackIdentity()

	EnsurePulumiPassphrase()

	fmt.Printf("\n[Infrastructure] Destroying infrastructure resources for stack '%s'...\n", stackName)
	stack, err := selectStackInlineSourceFn(ctx, stackName, projectName, infra.InlineProgram(), workspaceOptions()...)
	if err != nil {
		if isSelectStack404ErrorFn(err) {
			return nil // Benign: Nothing to destroy if stack doesn't exist
		}
		return fmt.Errorf("select stack '%s': %w", stackName, err)
	}

	stored, err := getStackAllConfigFn(ctx, stack)
	if err != nil {
		return fmt.Errorf("infrastructure: read stack config: %w", err)
	}
	if err := validateStackIdentity(stackName, stored, infra.libvirtURI(), ActiveBackendURL()); err != nil {
		return err
	}

	if err := destroyStackFn(ctx, stack, optdestroy.ProgressStreams(os.Stdout)); err != nil {
		return fmt.Errorf("infrastructure: pulumi destroy failed: %w", err)
	}

	fmt.Printf("✔ Infrastructure resources destroyed.\n")
	return nil
}
