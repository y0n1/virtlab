package workstation

import (
	"encoding/json"
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

	// readProcessIdentityFn reads the command line and start time of a live
	// PID. Tests replace it to simulate PID reuse and permission failures.
	readProcessIdentityFn = readProcessIdentity
)

const defaultPIDFileName = "sshuttle.pid"

// processIdentity describes a live process. StartTime stays constant for the
// life of the process, so it detects PID reuse after a crash or reboot.
type processIdentity struct {
	CommandLine string
	StartTime   string
}

// pidFileRecord is the content of the tunnel PID file. Command and StartTime
// are empty in legacy files that contain only the bare PID written by
// sshuttle itself.
type pidFileRecord struct {
	PID       int    `json:"pid"`
	Command   string `json:"command,omitempty"`
	StartTime string `json:"start_time,omitempty"`
}

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

	// Record the start-time fingerprint so a later PID reuse is detected even
	// when the new process also happens to be named sshuttle.
	t.persistPIDRecord(pid)

	fmt.Printf("✔ Lab Tunnel established successfully (PID %d)\n", pid)
	return nil
}

// Stop terminates the active sshuttle proxy tunnel and removes the PID file.
// It never signals a PID whose identity is not verified as sshuttle.
func (t *TunnelManager) Stop() error {
	rec, ok := t.readPIDRecord()
	if !ok || !t.verifyRecord(rec) {
		// Do not signal an unverified PID; drop the stale file instead.
		_ = os.Remove(t.PIDFilePath)
		return nil
	}
	pid := rec.PID

	fmt.Printf("\n[Workstation] Terminating Lab Tunnel (PID %d)...\n", pid)
	fmt.Printf("  • Action:    Terminate background sshuttle daemon\n")
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to signal the root-owned sshuttle process.\n")
	fmt.Printf("               You may be prompted for your password.\n\n")

	// Attempt graceful SIGTERM via sudo kill
	_ = execTunnelCmdFn("sudo", "kill", strconv.Itoa(pid)).Run()

	// Wait up to 3 seconds for exit
	for range 6 {
		time.Sleep(500 * time.Millisecond)
		if !t.verifyRecord(rec) {
			break
		}
	}

	// Re-verify before the force kill: the PID could have been reused since the
	// SIGTERM was sent.
	if t.verifyRecord(rec) {
		_ = execTunnelCmdFn("sudo", "kill", "-9", strconv.Itoa(pid)).Run()
	}

	_ = os.Remove(t.PIDFilePath)
	fmt.Printf("✔ Lab Tunnel terminated.\n")
	return nil
}

// IsRunning reports whether the PID file points at a live sshuttle process.
// A PID file whose identity cannot be verified is stale: it is removed, and
// no signal is ever sent to that PID.
func (t *TunnelManager) IsRunning() (bool, int) {
	rec, ok := t.readPIDRecord()
	if !ok {
		return false, 0
	}
	if t.verifyRecord(rec) {
		return true, rec.PID
	}

	// Stale PID file
	_ = os.Remove(t.PIDFilePath)
	return false, 0
}

// readPIDRecord parses the PID file. It accepts both the JSON record written
// by persistPIDRecord and the legacy bare-PID file written by sshuttle.
func (t *TunnelManager) readPIDRecord() (pidFileRecord, bool) {
	data, err := os.ReadFile(t.PIDFilePath)
	if err != nil {
		return pidFileRecord{}, false
	}
	content := strings.TrimSpace(string(data))

	if strings.HasPrefix(content, "{") {
		var rec pidFileRecord
		if err := json.Unmarshal([]byte(content), &rec); err != nil || rec.PID <= 0 {
			return pidFileRecord{}, false
		}
		return rec, true
	}

	pid, err := strconv.Atoi(content)
	if err != nil || pid <= 0 {
		return pidFileRecord{}, false
	}
	return pidFileRecord{PID: pid}, true
}

// persistPIDRecord rewrites the PID file with the verified process identity.
// sshuttle creates the file as root, so write a user-owned file and rename it
// into place; the user owns the parent directory and can replace the entry.
func (t *TunnelManager) persistPIDRecord(pid int) {
	ident, err := readProcessIdentityFn(pid)
	if err != nil {
		return
	}
	data, err := json.Marshal(pidFileRecord{
		PID:       pid,
		Command:   ident.CommandLine,
		StartTime: ident.StartTime,
	})
	if err != nil {
		return
	}

	dir := filepath.Dir(t.PIDFilePath)
	tmp, err := os.CreateTemp(dir, ".sshuttle.pid.*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, t.PIDFilePath)
}

// verifyRecord reports whether rec still points at the sshuttle process that
// virtlab started. It returns false when the process is gone, when its identity
// cannot be read (for example EPERM on a root-owned process), or when the
// command line or start time does not match.
func (t *TunnelManager) verifyRecord(rec pidFileRecord) bool {
	if !t.processExists(rec.PID) {
		return false
	}

	ident, err := readProcessIdentityFn(rec.PID)
	if err != nil {
		return false
	}
	if !isSshuttleProcess(ident.CommandLine) {
		return false
	}

	// An empty StartTime means a legacy bare-PID file; the command check is the
	// only identity available in that case.
	return rec.StartTime == "" || ident.StartTime == rec.StartTime
}

// isSshuttleProcess reports whether a command line belongs to sshuttle. It
// accepts both the direct form ("/usr/local/bin/sshuttle ...") and script
// installs that show the interpreter first ("python3 /path/to/sshuttle ...").
func isSshuttleProcess(commandLine string) bool {
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return false
	}
	if filepath.Base(fields[0]) == "sshuttle" {
		return true
	}
	if len(fields) > 1 && strings.HasPrefix(filepath.Base(fields[0]), "python") {
		return filepath.Base(fields[1]) == "sshuttle"
	}
	return false
}

// readProcessIdentity reads the command line and start time for pid using ps.
func readProcessIdentity(pid int) (processIdentity, error) {
	commandLine, err := psField(pid, "args=")
	if err != nil {
		return processIdentity{}, err
	}
	startTime, err := psField(pid, "lstart=")
	if err != nil {
		return processIdentity{}, err
	}
	return processIdentity{CommandLine: commandLine, StartTime: startTime}, nil
}

// psField returns one field from `ps` for pid. LC_ALL=C keeps the lstart
// timestamp stable across locales so fingerprints compare reliably.
func psField(pid int, field string) (string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", field)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
