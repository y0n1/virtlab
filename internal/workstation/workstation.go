package workstation

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/y0n1/virtlab/internal/config"
)

// Workstation manages local machine preparation, split-DNS, sshuttle tunneling,
// and state-tracked tool dependencies.
type Workstation struct {
	OS            Info
	SplitDNS      *SplitDNSManager
	Tunnel        *TunnelManager
	WorkspaceRoot string
	StateMgr      *StateManager
	State         *State
	PullSecret    []byte
}

// New creates a new Workstation domain manager.
func New(workspaceRoot string, customStatePath ...string) *Workstation {
	EnsureLocalBinInPATH()
	stateMgr := NewStateManager(customStatePath...)
	state, err := stateMgr.Load()
	if err != nil {
		fmt.Printf("Warning: failed to load workstation state: %v\n", err)
		state = NewEmptyState()
	}

	return &Workstation{
		OS:            DetectOS(),
		SplitDNS:      NewSplitDNSManager(),
		Tunnel:        NewTunnelManager(),
		WorkspaceRoot: workspaceRoot,
		StateMgr:      stateMgr,
		State:         state,
	}
}

// CheckPrerequisites verifies user-managed dependencies (ssh >= 8.0, go >= 1.22),
// ensures tool-managed dependencies (sshuttle, pulumi, ocm), and validates pull secret availability if config is provided.
func (w *Workstation) CheckPrerequisites(cfg ...*config.Config) error {
	// 1. Validate user-managed dependencies (OpenSSH >= 8.0, Go >= 1.22)
	if err := VerifyUserDependencies(); err != nil {
		return err
	}

	// 2. Ensure tool-managed dependencies (sshuttle, pulumi, ocm)
	if err := EnsureToolDependencies(w.State, w.StateMgr, w.OS); err != nil {
		return err
	}

	// 3. Optional utilities check
	if _, err := exec.LookPath("oc"); err != nil {
		fmt.Printf("Note: 'oc' CLI is not found on Workstation PATH. It will be needed to interact with the cluster after install.\n")
	}

	// 4. Validate pull secret availability early (fail fast before remote host operations)
	if len(cfg) > 0 && cfg[0] != nil {
		secretBytes, err := ResolvePullSecret(cfg[0])
		if err != nil {
			return err
		}
		w.PullSecret = secretBytes
	}

	return nil
}

// Prepare executes Phase 1 Workstation actions: verifying tool/user dependencies and pull secret.
func (w *Workstation) Prepare(cfg *config.Config) error {
	return w.CheckPrerequisites(cfg)
}

// Connect executes Phase 4 Workstation actions: configuring split-DNS and launching the sshuttle tunnel.
func (w *Workstation) Connect(cfg *config.Config) error {
	// 1. Configure Workstation split-DNS
	if err := w.SplitDNS.Configure(cfg.ClusterName, cfg.BaseDomain, cfg.Network.GatewayIP); err != nil {
		return fmt.Errorf("workstation: split-dns: %w", err)
	}
	w.State.SplitDNSConfigured = true
	w.State.ClusterName = cfg.ClusterName
	w.State.BaseDomain = cfg.BaseDomain
	w.State.GatewayIP = cfg.Network.GatewayIP
	_ = w.StateMgr.Save(w.State)

	// 2. Start sshuttle tunnel
	if err := w.Tunnel.Start(cfg.HostSSH.User, cfg.HostSSH.Host, cfg.Network.CIDR, cfg.Network.GatewayIP); err != nil {
		return fmt.Errorf("workstation: tunnel: %w", err)
	}
	running, pid := w.Tunnel.IsRunning()
	w.State.TunnelActive = running
	w.State.TunnelPID = pid
	_ = w.StateMgr.Save(w.State)

	return nil
}

// Teardown cleanly reverses all Workstation configuration and uninstalls any
// tool-managed dependencies that were installed by virtlab (undo invariant).
func (w *Workstation) Teardown(cfg *config.Config) error {
	var errs []string

	// 1. Stop tunnel first
	if err := w.Tunnel.Stop(); err != nil {
		errs = append(errs, fmt.Sprintf("stop tunnel: %v", err))
	} else {
		w.State.TunnelActive = false
		w.State.TunnelPID = 0
	}

	// 2. Revert split-DNS
	clusterName := ""
	baseDomain := ""
	if cfg != nil {
		clusterName = cfg.ClusterName
		baseDomain = cfg.BaseDomain
	}
	if clusterName == "" && w.State != nil {
		clusterName = w.State.ClusterName
	}
	if baseDomain == "" && w.State != nil {
		baseDomain = w.State.BaseDomain
	}

	if clusterName != "" || baseDomain != "" {
		if err := w.SplitDNS.Teardown(clusterName, baseDomain); err != nil {
			errs = append(errs, fmt.Sprintf("revert split-dns: %v", err))
		} else {
			w.State.SplitDNSConfigured = false
			w.State.ClusterName = ""
			w.State.BaseDomain = ""
			w.State.GatewayIP = ""
		}
	}

	// 3. Uninstall any tools that were installed by virtlab
	if err := UninstallManagedDependencies(w.State, w.StateMgr, w.OS); err != nil {
		errs = append(errs, fmt.Sprintf("uninstall managed dependencies: %v", err))
	}

	// 4. Clean up state file if empty and no errors occurred
	if w.State.IsEmpty() && len(errs) == 0 {
		if err := w.StateMgr.Remove(); err != nil {
			errs = append(errs, fmt.Sprintf("remove state file: %v", err))
		} else {
			fmt.Printf("✔ Workstation state cleaned up (%s removed)\n", w.StateMgr.Path)
		}
	} else {
		if err := w.StateMgr.Save(w.State); err != nil {
			errs = append(errs, fmt.Sprintf("save state file: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("workstation teardown issues:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
