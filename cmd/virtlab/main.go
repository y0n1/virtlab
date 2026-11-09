package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/y0n1/virtlab/internal/cluster"
	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/host"
	"github.com/y0n1/virtlab/internal/infrastructure"
	"github.com/y0n1/virtlab/internal/verification"
	"github.com/y0n1/virtlab/internal/workstation"
)

var rootContextFn = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcommand := os.Args[1]
	args := os.Args[2:]

	ctx, stop := rootContextFn()
	defer stop()

	// Dual-signal emergency abort:
	// First signal cancels ctx (graceful cancellation, lock release, subprocess termination).
	// Calling stop() immediately restores the default signal disposition so a second signal
	// will terminate the process immediately.
	go func() {
		<-ctx.Done()
		stop()
	}()

	workstation.EnsureLocalBinInPATH()
	infrastructure.EnsurePulumiPassphrase()
	infrastructure.EnsurePulumiBackend()

	var err error
	switch subcommand {
	case "up":
		err = runUp(ctx, args)
	case "down":
		err = runDown(ctx, args)
	case "status":
		err = runStatus(ctx, args)
	case "version", "-v", "--version":
		err = runVersion(args)
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command '%s'\n", subcommand)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			fmt.Fprintf(os.Stderr, "\n⚠ Operation canceled by user.\n")
			os.Exit(130)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "\n✖ ERROR: Operation timed out: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\n✖ ERROR: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: virtlab <command> [options]

Commands:
  up        Execute Phases 1-4 to provision and verify the OpenShift lab
  down      Destroy all lab resources and revert Workstation/Host configurations
  status    Inspect current status of Workstation tunnel and cluster accessibility
  version   Display version and build information

Options:
  --config <path>   Path to configuration file (default: config.yaml)
  --yes, -y         Skip interactive confirmation prompts
  --version, -v     Print version information
  --help, -h        Show help`)
}

func loadConfig(configPath string) (*config.Config, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Fallback check in current directory: if config.yaml is missing, check config.local.yaml
		if configPath == "config.yaml" {
			if _, errLocal := os.Stat("config.local.yaml"); errLocal == nil {
				configPath = "config.local.yaml"
			} else {
				return nil, fmt.Errorf("config file '%s' not found. Please create one (see examples/config.example.yaml for a template)", configPath)
			}
		} else {
			return nil, fmt.Errorf("config file '%s' does not exist", configPath)
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	// Validate static requirements
	res := config.ValidateStatic(cfg)
	if !res.Valid() {
		return nil, fmt.Errorf("configuration errors in '%s':\n%s", configPath, res.Summary())
	}
	if res.HasWarnings() {
		fmt.Printf("Configuration warnings:\n%s\n", res.Summary())
	}

	return cfg, nil
}

// ensureStackCoversHostResources refuses to provision when the Host already
// holds live resources for the cluster but the Pulumi stack that tracks them is
// missing. It is a no-op for a fresh lab and for a resumed run with state.
func ensureStackCoversHostResources(stackExists, hostHasResources bool, clusterName, host string) error {
	if stackExists || !hostHasResources {
		return nil
	}

	backend := infrastructure.ActiveBackendURL()
	if backend == "" {
		backend = "(unknown)"
	}

	return fmt.Errorf(`phase 2: host '%s' has live resources for cluster '%s', but no Pulumi state for that cluster exists in backend '%s'.

virtlab has no record of those resources, so 'virtlab up' would create duplicates.
Choose one recovery:
  - Discard the leftovers: run 'virtlab down' with the original config, then remove any remaining libvirt pool or network named '%s' and re-run 'virtlab up';
  - Keep the resources: restore the Pulumi state backup for cluster '%s' and re-run 'virtlab up'.

See docs/state.md (Backup and restore) for the state backup procedure`,
		host, clusterName, backend, clusterName, clusterName)
}

func runUp(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	autoApprove := fs.Bool("yes", false, "Skip confirmation prompts")
	fs.BoolVar(autoApprove, "y", false, "Skip confirmation prompts")
	_ = fs.Parse(args)

	// 1. Exclusive Workstation Concurrency Lock (XDG compliant)
	lock, err := workstation.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Release()

	if err := ctx.Err(); err != nil {
		return err
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	// Display node topology breakdown
	topo, err := cfg.EffectiveTopology()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", topo.FormatSummaryTable())

	// Initialize Domain Services
	workspaceRoot, _ := os.Getwd()
	ws := workstation.New(workspaceRoot)
	h := host.New(cfg.HostSSH.User, cfg.HostSSH.Host)
	ocp := cluster.NewOpenShiftManager(cfg, h)
	verifier := verification.NewVerifier(cfg, h)

	// Validate Workstation Prerequisites (fail fast before any remote operations)
	if err := ws.CheckPrerequisites(cfg); err != nil {
		return err
	}

	// Guard against co-existing lab active on Workstation
	if ws.State.ClusterName != "" && ws.State.ClusterName != cfg.ClusterName && ws.State.HasActiveLab() {
		return fmt.Errorf("co-existing lab '%s' detected on workstation (running multiple concurrent labs is not supported; run 'virtlab down' on '%s' before reprovisioning)",
			ws.State.ClusterName, ws.State.ClusterName)
	}

	// Validate Connectivity and Guard against co-existing lab on Host
	fmt.Printf("[Host] Checking remote hypervisor connectivity...\n")
	if err := h.CheckConnectivityContext(ctx); err != nil {
		return fmt.Errorf("cannot connect to Host: %w", err)
	}

	conflicts, err := h.DetectConflictingLabs(cfg.ClusterName)
	if err != nil {
		fmt.Printf("Warning: failed to inspect existing host labs (%v); proceeding anyway\n", err)
	} else if len(conflicts) > 0 {
		return fmt.Errorf("co-existing lab '%s' detected on host '%s' (running multiple concurrent labs is not supported; run 'virtlab down' on '%s' before reprovisioning)",
			conflicts[0], h.Client.Host, conflicts[0])
	}

	// platform is immutable after install: fail before mutating host state.
	if err := ocp.EnsurePlatformCompatible(); err != nil {
		return err
	}

	// Inspect existing host resources once. The result feeds the capacity
	// idempotency credit and the Pulumi state guard in Phase 2.
	existingCluster, existingErr := h.InspectExistingCluster(cfg.ClusterName)
	if existingErr != nil {
		fmt.Printf("Warning: failed to inspect existing cluster resources on host (%v); proceeding anyway\n", existingErr)
	}

	// Validate Capacity against Host (with Idempotency Credit for existing lab)
	fmt.Printf("[Host] Checking remote hypervisor capacity...\n")
	cap, err := h.InspectCapacity()
	if err != nil {
		fmt.Printf("Warning: failed to query host capacity (%v); proceeding anyway\n", err)
	} else {
		if existingCluster.Exists {
			fmt.Printf("✔ Existing cluster '%s' detected on host (reclaiming %d MiB RAM, %d GiB Disk for capacity check)\n",
				cfg.ClusterName, existingCluster.AllocatedRAMMiB, existingCluster.AllocatedDiskGiB)
			cap.AvailableRAMMiB += existingCluster.AllocatedRAMMiB
			cap.AvailableDiskGiB += existingCluster.AllocatedDiskGiB
		}

		capRes := config.ValidateCapacity(cfg, cap)
		if !capRes.Valid() {
			return fmt.Errorf("host capacity check failed:\n%s", capRes.Summary())
		}
		if capRes.HasWarnings() {
			fmt.Printf("Capacity warnings:\n%s\n", capRes.Summary())
			if !*autoApprove {
				if !promptYesNo("Do you want to continue despite these warnings?") {
					return fmt.Errorf("aborted by user")
				}
			}
		} else {
			fmt.Printf("✔ Host capacity verified (CPU, RAM, and Disk headroom sufficient)\n")
		}
	}

	// Resolve dynamic virtual network subnet and gateway
	fmt.Printf("[Host] Resolving virtual network subnet (CIDR)...\n")
	cidr, gw, err := h.ResolveNetworkSubnet(cfg.Network.Name, cfg.Network.CIDR, cfg.Network.CIDRAuto)
	if err != nil {
		return fmt.Errorf("resolve network subnet: %w", err)
	}
	cfg.Network.CIDR = cidr
	cfg.Network.GatewayIP = gw

	// Derive the VIPs against the final subnet (an auto-allocated CIDR may have
	// changed since load time) and validate the complete addressing plan.
	if err := cfg.ResolveVIPs(); err != nil {
		return fmt.Errorf("resolve network VIPs: %w", err)
	}
	addrRes := config.ValidateNetworkAddressing(cfg)
	if !addrRes.Valid() {
		return fmt.Errorf("network addressing check failed:\n%s", addrRes.Summary())
	}
	fmt.Printf("✔ Network subnet resolved to '%s' (gateway: %s)\n", cfg.Network.CIDR, cfg.Network.GatewayIP)
	if addrRes.HasWarnings() {
		fmt.Printf("Network warnings:\n%s\n", addrRes.Summary())
	}
	if cfg.UsesVIPs() {
		fmt.Printf("✔ Network VIPs resolved (API: %s, ingress: %s, ingress LB: %s)\n",
			cfg.Network.APIVIP, cfg.Network.IngressVIP, cfg.Network.IngressLBVIP)
	}

	// =========================================================================
	// Phase 1: Host and Workstation Preparation
	// =========================================================================
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("PHASE 1: Host & Workstation Preparation\n")
	fmt.Printf("================================================================================\n")

	if err := ws.Prepare(cfg); err != nil {
		return fmt.Errorf("phase 1 (workstation) failed: %w", err)
	}

	if err := h.Prepare(cfg, ws.PullSecret); err != nil {
		return fmt.Errorf("phase 1 (host) failed: %w", err)
	}

	// =========================================================================
	// Phase 2: Lab Setup (Infrastructure)
	// =========================================================================
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("PHASE 2: Lab Setup (Storage, Network, Guest VMs)\n")
	fmt.Printf("================================================================================\n")

	fmt.Printf("[Host] Resolving virtual network bridge device...\n")
	bridgeName, err := h.ResolveBridge(cfg.Network.Name)
	if err != nil {
		return fmt.Errorf("phase 2: resolve network bridge: %w", err)
	}
	fmt.Printf("✔ Network bridge resolved to '%s'\n", bridgeName)

	infra := infrastructure.New(cfg, bridgeName)

	// The Pulumi stack is the only record of the resources virtlab created.
	// If the Host already holds a lab for this name but the stack is missing
	// (for example, lost local state or a different backend), Up would create
	// duplicate resources.
	stackExists, err := infra.StackHasState(ctx)
	if err != nil {
		return fmt.Errorf("phase 2: inspect Pulumi stack state: %w", err)
	}
	if existingErr == nil {
		if err := ensureStackCoversHostResources(stackExists, existingCluster.Exists, cfg.ClusterName, h.Client.Host); err != nil {
			return err
		}
	}

	if err := infra.Provision(ctx); err != nil {
		return fmt.Errorf("phase 2 failed: %w", err)
	}

	if cfg.UsesVIPs() {
		fmt.Printf("[Host] Preparing cluster VIP networking on '%s'...\n", bridgeName)

		// VRRP (keepalived) between the guests needs multicast to cross the
		// bridge; the lab bridge has snooping enabled and no querier.
		if err := h.Client.DisableIGMPSnooping(ctx, bridgeName); err != nil {
			return fmt.Errorf("phase 2: disable IGMP snooping on '%s': %w", bridgeName, err)
		}

		leases, err := infrastructure.ComputeNodeLeases(cfg)
		if err != nil {
			return fmt.Errorf("phase 2: compute node leases: %w", err)
		}
		topo, err := cfg.EffectiveTopology()
		if err != nil {
			return fmt.Errorf("phase 2: resolve topology: %w", err)
		}
		ingress := infrastructure.IngressLeases(topo, leases)
		backends := make([]host.IngressBackend, 0, len(ingress))
		for _, l := range ingress {
			backends = append(backends, host.IngressBackend{Name: l.Name, IP: l.IP})
		}
		prefixLen, err := config.PrefixLength(cfg.Network.CIDR)
		if err != nil {
			return fmt.Errorf("phase 2: resolve CIDR prefix: %w", err)
		}
		if err := h.Client.SetupIngressLB(ctx, cfg.ClusterName, bridgeName, cfg.Network.IngressLBVIP, prefixLen, backends); err != nil {
			return fmt.Errorf("phase 2: setup ingress load balancer: %w", err)
		}
		fmt.Printf("✔ Ingress load balancer ready at %s (backends: %d)\n", cfg.Network.IngressLBVIP, len(backends))
	}

	// =========================================================================
	// Phase 3: OpenShift Cluster Installation
	// =========================================================================
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("PHASE 3: OpenShift Cluster Installation (%s)\n", cfg.Preset)
	fmt.Printf("================================================================================\n")

	if err := ocp.Install(ctx); err != nil {
		return fmt.Errorf("phase 3 failed: %w", err)
	}

	// Scale the router set to the ingress node count so the host LB can spread
	// traffic across every node (idempotent on resumed installs).
	if err := ocp.EnsureIngressReplicas(ctx); err != nil {
		return fmt.Errorf("phase 3: reconcile ingress replicas: %w", err)
	}

	// =========================================================================
	// Phase 4: Verification
	// =========================================================================
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("PHASE 4: Verification\n")
	fmt.Printf("================================================================================\n")

	// Connect Workstation to the active Lab Network via split-DNS and sshuttle
	if err := ws.Connect(cfg); err != nil {
		return fmt.Errorf("phase 4 (workstation connect) failed: %w", err)
	}

	if err := verifier.VerifyAll(ctx); err != nil {
		return fmt.Errorf("phase 4 verification failed: %w", err)
	}

	return nil
}

func runDown(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	autoApprove := fs.Bool("yes", false, "Skip confirmation prompts")
	fs.BoolVar(autoApprove, "y", false, "Skip confirmation prompts")
	_ = fs.Parse(args)

	// 1. Exclusive Workstation Concurrency Lock (XDG compliant)
	lock, err := workstation.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Release()

	if err := ctx.Err(); err != nil {
		return err
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		// If config cannot be loaded, inspect whether workstation state has active resources to clean up
		workspaceRoot, _ := os.Getwd()
		ws := workstation.New(workspaceRoot)
		if !ws.State.IsEmpty() {
			fmt.Printf("Notice: config '%s' could not be loaded (%v).\nTearing down active workstation resources from state...\n", *configPath, err)
			_ = ws.Teardown(nil)
		}
		return err
	}

	if !*autoApprove {
		fmt.Printf("\n⚠ WARNING: This will destroy the cluster '%s', all VMs, network, and storage,\n", cfg.ClusterName)
		fmt.Printf("           and revert all Workstation split-DNS and tunnel configurations.\n\n")
		if !promptYesNo(fmt.Sprintf("Are you sure you want to tear down lab '%s'?", cfg.ClusterName)) {
			fmt.Println("Teardown canceled.")
			return nil
		}
	}

	return runTeardownStages(ctx, cfg)
}

var (
	infraTeardownFn = func(ctx context.Context, cfg *config.Config) error {
		infra := infrastructure.New(cfg, "virbr-lab0")
		return infra.Teardown(ctx)
	}
	hostTeardownFn any = func(ctx context.Context, cfg *config.Config) error {
		h := host.New(cfg.HostSSH.User, cfg.HostSSH.Host)
		return h.TeardownContext(ctx, cfg)
	}
	wsTeardownFn = func(cfg *config.Config) error {
		workspaceRoot, _ := os.Getwd()
		ws := workstation.New(workspaceRoot)
		return ws.Teardown(cfg)
	}
)

func callHostTeardown(ctx context.Context, cfg *config.Config) error {
	switch fn := hostTeardownFn.(type) {
	case func(context.Context, *config.Config) error:
		return fn(ctx, cfg)
	case func(*config.Config) error:
		return fn(cfg)
	default:
		return fmt.Errorf("unsupported hostTeardownFn signature: %T", hostTeardownFn)
	}
}

func runTeardownStages(ctx context.Context, cfg *config.Config) error {
	var errs []error

	// 1. Destroy Infrastructure (Guests, Network, Storage Pool)
	if err := infraTeardownFn(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error during infrastructure teardown: %v\n", err)
		errs = append(errs, fmt.Errorf("infrastructure teardown: %w", err))

		// A Host or backend mismatch means the configured Host is not the one
		// that owns the lab. Never run host or workstation cleanup there.
		if errors.Is(err, infrastructure.ErrStackIdentityMismatch) {
			fmt.Fprintln(os.Stderr, "\n✖ Teardown stopped: the Pulumi stack does not belong to the configured host/backend. Host and workstation cleanup were skipped.")
			return errors.Join(errs...)
		}
	}

	// 2. Clean up Host-side artifacts (storage dir, DNS bridge, lingering libvirt resources)
	if err := callHostTeardown(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error during host teardown: %v\n", err)
		errs = append(errs, fmt.Errorf("host teardown: %w", err))
	}

	// 3. Revert Workstation (Tunnel & Split-DNS)
	if err := wsTeardownFn(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error during workstation teardown: %v\n", err)
		errs = append(errs, fmt.Errorf("workstation teardown: %w", err))
	}

	// 4. Clean up Workstation credentials (kubeconfig and kubeadmin-password)
	home, _ := os.UserHomeDir()
	if home != "" {
		_ = os.Remove(filepath.Join(home, ".kube", fmt.Sprintf("config.%s", cfg.ClusterName)))
		_ = os.Remove(filepath.Join(home, ".kube", fmt.Sprintf("kubeadmin-password.%s", cfg.ClusterName)))
	}

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "\n✖ Teardown failed for lab '%s' with %d error(s):\n", cfg.ClusterName, len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %v\n", e)
		}
		return errors.Join(errs...)
	}

	fmt.Printf("\n✔ Lab '%s' successfully torn down and clean.\n", cfg.ClusterName)
	return nil
}

func runStatus(ctx context.Context, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	workspaceRoot, _ := os.Getwd()
	ws := workstation.New(workspaceRoot)
	h := host.New(cfg.HostSSH.User, cfg.HostSSH.Host)

	fmt.Printf("\nStatus for Lab '%s' (%s):\n", cfg.ClusterName, cfg.ClusterFQDN())
	fmt.Println("--------------------------------------------------------------------------------")

	// Tunnel check
	running, pid := ws.Tunnel.IsRunning()
	if running {
		fmt.Printf("• Lab Tunnel (sshuttle):  Active (PID %d)\n", pid)
	} else {
		fmt.Printf("• Lab Tunnel (sshuttle):  Inactive\n")
	}

	// Host check
	if err := h.CheckConnectivityContext(ctx); err == nil {
		fmt.Printf("• Host Hypervisor:        Reachable via SSH (%s)\n", h.Client.Target())
	} else {
		fmt.Printf("• Host Hypervisor:        Unreachable (%v)\n", err)
	}

	// Local Kubeconfig check
	home, _ := os.UserHomeDir()
	kcPath := filepath.Join(home, ".kube", fmt.Sprintf("config.%s", cfg.ClusterName))
	if _, err := os.Stat(kcPath); err == nil {
		fmt.Printf("• Kubeconfig:             Found at %s\n", kcPath)
	} else {
		fmt.Printf("• Kubeconfig:             Not synced yet\n")
	}

	// Local Kubeadmin Password check
	pwPath := filepath.Join(home, ".kube", fmt.Sprintf("kubeadmin-password.%s", cfg.ClusterName))
	if fi, err := os.Stat(pwPath); err == nil {
		fmt.Printf("• Kubeadmin Password:     Saved at %s (%04o)\n", pwPath, fi.Mode().Perm())
	} else {
		fmt.Printf("• Kubeadmin Password:     Not synced yet\n")
	}

	// Workstation State check
	if ws.StateMgr != nil {
		if !ws.State.IsEmpty() {
			fmt.Printf("• Workstation State:      Tracked at %s\n", ws.StateMgr.Path)
			if ws.State.HasManagedPackages() {
				var pkgs []string
				for name, mb := range ws.State.ManagedPackages {
					if mb.InstalledByVirtlab {
						pkgs = append(pkgs, fmt.Sprintf("%s (%s)", name, mb.InstallMethod))
					}
				}
				if len(pkgs) > 0 {
					fmt.Printf("  - Virtlab-managed tools: %s\n", strings.Join(pkgs, ", "))
				}
			}
		} else {
			fmt.Printf("• Workstation State:      Clean (%s)\n", ws.StateMgr.Path)
		}
	}

	fmt.Println("--------------------------------------------------------------------------------")
	return nil
}

func promptYesNo(question string) bool {
	fmt.Printf("%s [y/N]: ", question)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
