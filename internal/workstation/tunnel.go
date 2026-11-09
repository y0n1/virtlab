package workstation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	execTunnelCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command(name, args...)
	}
	lookPathTunnelFn = exec.LookPath
)

const defaultPIDFileName = "sshuttle.pid"

// DefaultTunnelPIDFilePath returns the XDG-compliant path: ~/.local/state/virtlab/sshuttle.pid
func DefaultTunnelPIDFilePath() string {
	return filepath.Join(StateDir(), defaultPIDFileName)
}

// TunnelManager manages the sshuttle proxy lifecycle on the Workstation.
type TunnelManager struct {
	PIDFilePath string
}

// NewTunnelManager creates a tunnel manager using an XDG-compliant PID file.
// If customPath is provided, it can be a directory or a specific file path.
func NewTunnelManager(customPath ...string) *TunnelManager {
	pidPath := DefaultTunnelPIDFilePath()
	if len(customPath) > 0 && customPath[0] != "" {
		p := customPath[0]
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			pidPath = filepath.Join(p, defaultPIDFileName)
		} else if strings.HasSuffix(p, ".pid") {
			pidPath = p
		} else {
			pidPath = filepath.Join(p, defaultPIDFileName)
		}
	}
	return &TunnelManager{
		PIDFilePath: pidPath,
	}
}

// BuildSshuttleArgs constructs the arguments for running sshuttle in targeted mode.
// Note: We intentionally avoid the '--dns' flag because in sshuttle that flag captures
// all system DNS from /etc/resolv.conf, which hijacks and blackholes general workstation
// and corporate VPN traffic. Instead, we explicitly use --ns-hosts and --to-ns to proxy
// only queries destined for the lab gateway IP.
func BuildSshuttleArgs(hostUser, hostAddr, cidr, gatewayIP string) []string {
	remoteTarget := fmt.Sprintf("%s@%s", hostUser, hostAddr)
	nsHostsArg := fmt.Sprintf("--ns-hosts=%s", gatewayIP)
	toNSArg := fmt.Sprintf("--to-ns=%s:53", gatewayIP)

	return []string{
		"sshuttle",
		nsHostsArg,
		toNSArg,
		"-r", remoteTarget,
		cidr,
	}
}

// Start launches the sshuttle transparent proxy tunnel with targeted DNS.
func (t *TunnelManager) Start(hostUser, hostAddr, cidr, gatewayIP string) error {
	running, pid := t.IsRunning()
	if running {
		fmt.Printf("✔ Lab Tunnel is already active (PID %d)\n", pid)
		return nil
	}

	// Verify sshuttle is installed
	if _, err := lookPathTunnelFn("sshuttle"); err != nil {
		return fmt.Errorf("sshuttle binary not found on Workstation PATH. Please install it (e.g. 'brew install sshuttle' on macOS, 'sudo dnf install sshuttle' on Fedora)")
	}

	remoteTarget := fmt.Sprintf("%s@%s", hostUser, hostAddr)
	nsHostsArg := fmt.Sprintf("--ns-hosts=%s", gatewayIP)
	toNSArg := fmt.Sprintf("--to-ns=%s:53", gatewayIP)

	fmt.Printf("\n[Workstation] Establishing Lab Tunnel via sshuttle\n")
	fmt.Printf("  • Command:   sudo sshuttle %s %s -r %s %s\n", nsHostsArg, toNSArg, remoteTarget, cidr)
	fmt.Printf("  • Reason:    Proxies TCP traffic for subnet %s and targeted DNS queries for %s to the Host\n", cidr, gatewayIP)
	fmt.Printf("               over standard SSH, without requiring a VPN daemon on the hypervisor.\n")
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to configure local packet routing rules (pf / iptables).\n")
	fmt.Printf("               You may be prompted for your password.\n\n")

	// Launch sshuttle in background using sudo
	// We use nohup / backgrounding to detach cleanly
	args := BuildSshuttleArgs(hostUser, hostAddr, cidr, gatewayIP)

	// Using --pidfile option of sshuttle if supported, or tracking via wrapper
	_ = os.MkdirAll(filepath.Dir(t.PIDFilePath), 0o755)
	cmd := execTunnelCmdFn("sudo", append(args, "--pidfile", t.PIDFilePath, "--daemon")...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Fallback without --daemon / --pidfile if older sshuttle version
		return fmt.Errorf("failed to start sshuttle tunnel: %w", err)
	}

	// Wait up to 5 seconds for sshuttle to initialize and populate the PID file
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		if running, pid = t.IsRunning(); running {
			break
		}
	}

	if !running {
		return fmt.Errorf("sshuttle started but PID file '%s' was not populated or process died", t.PIDFilePath)
	}

	fmt.Printf("✔ Lab Tunnel established successfully (PID %d)\n", pid)
	return nil
}

// Stop terminates the active sshuttle proxy tunnel and removes the PID file.
func (t *TunnelManager) Stop() error {
	running, pid := t.IsRunning()
	if !running {
		_ = os.Remove(t.PIDFilePath)
		return nil
	}

	fmt.Printf("\n[Workstation] Terminating Lab Tunnel (PID %d)...\n", pid)
	fmt.Printf("  • Action:    Terminate background sshuttle daemon\n")
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to signal the root-owned sshuttle process.\n")
	fmt.Printf("               You may be prompted for your password.\n\n")

	// Attempt graceful SIGTERM via sudo kill
	_ = execTunnelCmdFn("sudo", "kill", strconv.Itoa(pid)).Run()

	// Wait up to 3 seconds for exit
	for range 6 {
		time.Sleep(500 * time.Millisecond)
		if !t.processExists(pid) {
			break
		}
	}

	if t.processExists(pid) {
		// Force kill if still lingering
		_ = execTunnelCmdFn("sudo", "kill", "-9", strconv.Itoa(pid)).Run()
	}

	_ = os.Remove(t.PIDFilePath)
	fmt.Printf("✔ Lab Tunnel terminated.\n")
	return nil
}

// IsRunning reports whether the sshuttle process is alive.
func (t *TunnelManager) IsRunning() (bool, int) {
	data, err := os.ReadFile(t.PIDFilePath)
	if err != nil {
		return false, 0
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false, 0
	}

	if t.processExists(pid) {
		return true, pid
	}

	// Stale PID file
	_ = os.Remove(t.PIDFilePath)
	return false, 0
}

func (t *TunnelManager) processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix systems, FindProcess always succeeds; Signal(0) tests if process is alive.
	// If the process is owned by another user (e.g. root via sudo), Signal(0) returns EPERM,
	// which confirms the process exists. ESRCH means no such process.
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
