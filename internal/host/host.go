package host

import (
	"context"
	"fmt"
	"io"

	"github.com/y0n1/virtlab/internal/config"
)

// Host represents the remote KVM/libvirt hypervisor machine.
type Host struct {
	Client *SSHClient
}

var _ CommandRunner = (*Host)(nil)

// New creates a new Host domain service.
func New(user, host string) *Host {
	return &Host{
		Client: NewSSHClient(user, host),
	}
}

// CheckConnectivityContext verifies SSH reachability to the Host with context.
func (h *Host) CheckConnectivityContext(ctx context.Context) error {
	out, err := h.Client.RunContext(ctx, "echo connected")
	if err != nil || out != "connected" {
		return fmt.Errorf("cannot connect to Host %s via SSH: %w", h.Client.Target(), err)
	}
	return nil
}

// CheckConnectivity verifies SSH reachability to the Host.
// Preserved for backward compatibility.
func (h *Host) CheckConnectivity() error {
	return h.CheckConnectivityContext(context.Background())
}

// InspectCapacity queries the Host hardware resource envelope.
func (h *Host) InspectCapacity() (config.HostCapacity, error) {
	return h.Client.InspectCapacity()
}

// Prepare runs Phase 1 Host preparation: base packages, services, tools, and pull-secret delivery.
func (h *Host) Prepare(cfg *config.Config, pullSecretBytes []byte) error {
	if err := h.CheckConnectivity(); err != nil {
		return err
	}

	if err := h.Client.PrepareHost(cfg); err != nil {
		return err
	}

	if err := h.Client.EnsureOpenShiftTools(cfg); err != nil {
		return err
	}

	if err := h.Client.EnsurePullSecret(pullSecretBytes); err != nil {
		return err
	}

	return nil
}

// EnsureOpenShiftTools ensures openshift-install and oc are installed and match cfg.OCPVersion.
func (h *Host) EnsureOpenShiftTools(cfg *config.Config) error {
	return h.Client.EnsureOpenShiftTools(cfg)
}

// TeardownContext cleans up cluster-specific resources and artifacts on the Host hypervisor with context.
func (h *Host) TeardownContext(ctx context.Context, cfg *config.Config) error {
	return h.Client.TeardownContext(ctx, cfg)
}

// Teardown cleans up cluster-specific resources and artifacts on the Host hypervisor.
// Preserved for backward compatibility.
func (h *Host) Teardown(cfg *config.Config) error {
	return h.TeardownContext(context.Background(), cfg)
}


// Run executes a remote command string on the Host hypervisor.
func (h *Host) Run(command string) (string, error) {
	if h == nil || h.Client == nil {
		return "", fmt.Errorf("host client is nil")
	}
	return h.Client.Run(command)
}

// RunContext executes a remote command string with context cancellation support.
func (h *Host) RunContext(ctx context.Context, command string) (string, error) {
	if h == nil || h.Client == nil {
		return "", fmt.Errorf("host client is nil")
	}
	return h.Client.RunContext(ctx, command)
}

// RunStream executes a remote command string streaming stdout and stderr.
func (h *Host) RunStream(command string, stdout, stderr io.Writer) error {
	if h == nil || h.Client == nil {
		return fmt.Errorf("host client is nil")
	}
	return h.Client.RunStream(command, stdout, stderr)
}

// RunStreamContext executes a remote command string with context streaming stdout and stderr.
func (h *Host) RunStreamContext(ctx context.Context, command string, stdout, stderr io.Writer) error {
	if h == nil || h.Client == nil {
		return fmt.Errorf("host client is nil")
	}
	return h.Client.RunStreamContext(ctx, command, stdout, stderr)
}

// RunScript pipes a multi-line script via stdin into bash -s on the remote Host.
func (h *Host) RunScript(script string, args ...string) (string, error) {
	if h == nil || h.Client == nil {
		return "", fmt.Errorf("host client is nil")
	}
	return h.Client.RunScript(script, args...)
}

// RunScriptContext pipes a multi-line script via stdin into bash -s with context.
func (h *Host) RunScriptContext(ctx context.Context, script string, args ...string) (string, error) {
	if h == nil || h.Client == nil {
		return "", fmt.Errorf("host client is nil")
	}
	return h.Client.RunScriptContext(ctx, script, args...)
}

// RunScriptStream pipes a multi-line script streaming stdout and stderr.
func (h *Host) RunScriptStream(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
	if h == nil || h.Client == nil {
		return fmt.Errorf("host client is nil")
	}
	return h.Client.RunScriptStream(ctx, script, stdout, stderr, args...)
}

// RunScriptStreamContext is an alias for RunScriptStream for full API symmetry.
func (h *Host) RunScriptStreamContext(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
	if h == nil || h.Client == nil {
		return fmt.Errorf("host client is nil")
	}
	return h.Client.RunScriptStreamContext(ctx, script, stdout, stderr, args...)
}
