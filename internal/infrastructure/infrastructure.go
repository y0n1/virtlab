package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

// Seams for unit tests. Production code delegates to the Pulumi Automation API.
var (
	selectStackInlineSourceFn = auto.SelectStackInlineSource
	isSelectStack404ErrorFn   = auto.IsSelectStack404Error
	getStoredCredentialsFn    = workspace.GetStoredCredentials
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

// Provision brings up the storage pool, network, and guest VMs using Pulumi Automation API.
func (infra *Infrastructure) Provision(ctx context.Context) error {
	stackName := infra.Config.ClusterName
	projectName := infra.ProjectName
	if projectName == "" {
		projectName = pulumiProjectName
	}

	EnsurePulumiPassphrase()

	fmt.Printf("\n[Infrastructure] Initializing Pulumi Automation stack '%s'...\n", stackName)
	opts := []auto.LocalWorkspaceOption{
		auto.EnvVars(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": os.Getenv("PULUMI_CONFIG_PASSPHRASE"),
		}),
	}
	stack, err := auto.UpsertStackInlineSource(ctx, stackName, projectName, infra.InlineProgram(), opts...)
	if err != nil {
		return fmt.Errorf("infrastructure: upsert stack: %w", err)
	}

	libvirtURI := fmt.Sprintf("qemu+ssh://%s@%s/system", infra.Config.HostSSH.User, infra.Config.HostSSH.Host)
	if err := stack.SetConfig(ctx, "libvirt:uri", auto.ConfigValue{Value: libvirtURI}); err != nil {
		return fmt.Errorf("infrastructure: set libvirt URI config: %w", err)
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
func (infra *Infrastructure) Teardown(ctx context.Context) error {
	stackName := infra.Config.ClusterName
	projectName := infra.ProjectName
	if projectName == "" {
		projectName = pulumiProjectName
	}

	EnsurePulumiPassphrase()

	fmt.Printf("\n[Infrastructure] Destroying infrastructure resources for stack '%s'...\n", stackName)
	opts := []auto.LocalWorkspaceOption{
		auto.EnvVars(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": os.Getenv("PULUMI_CONFIG_PASSPHRASE"),
		}),
	}
	stack, err := selectStackInlineSourceFn(ctx, stackName, projectName, infra.InlineProgram(), opts...)
	if err != nil {
		if isSelectStack404ErrorFn(err) {
			return nil // Benign: Nothing to destroy if stack doesn't exist
		}
		return fmt.Errorf("select stack '%s': %w", stackName, err)
	}

	if _, err := stack.Destroy(ctx, optdestroy.ProgressStreams(os.Stdout)); err != nil {
		return fmt.Errorf("infrastructure: pulumi destroy failed: %w", err)
	}

	fmt.Printf("✔ Infrastructure resources destroyed.\n")
	return nil
}
