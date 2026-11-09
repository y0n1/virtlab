package host

import (
	"context"
	"fmt"

	"github.com/y0n1/virtlab/internal/config"
)

// Host represents the remote KVM/libvirt hypervisor machine.
type Host struct {
	Client *SSHClient
}

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
