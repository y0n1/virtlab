package workstation

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// stubProcessIdentity makes every PID appear to have the given command line
// and start time for the duration of the test.
func stubProcessIdentity(t *testing.T, commandLine, startTime string) {
	t.Helper()
	orig := readProcessIdentityFn
	readProcessIdentityFn = func(int) (processIdentity, error) {
		return processIdentity{CommandLine: commandLine, StartTime: startTime}, nil
	}
	t.Cleanup(func() { readProcessIdentityFn = orig })
}

// stubProcessIdentityError makes identity reads fail for the duration of the
// test, simulating EPERM on a root-owned process.
func stubProcessIdentityError(t *testing.T, err error) {
	t.Helper()
	orig := readProcessIdentityFn
	readProcessIdentityFn = func(int) (processIdentity, error) {
		return processIdentity{}, err
	}
	t.Cleanup(func() { readProcessIdentityFn = orig })
}

// stubTunnelCommands records every command the manager tries to run and
// replaces it with a no-op so tests never signal a real process.
func stubTunnelCommands(t *testing.T) *[]string {
	t.Helper()
	orig := execTunnelCmdFn
	var calls []string
	execTunnelCmdFn = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return exec.Command("true")
	}
	t.Cleanup(func() { execTunnelCmdFn = orig })
	return &calls
}

func writePIDFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectOS(t *testing.T) {
	info := DetectOS()
	if info.Type != OSDarwin && info.Type != OSLinux {
		t.Logf("running on unexpected OS type: %s", info.Type)
	}
	if info.Architecture == "" {
		t.Error("expected non-empty architecture")
	}
}

func TestTunnelManager_ProcessExists(t *testing.T) {
	tm := NewTunnelManager(t.TempDir())

	// Current process must exist
	if !tm.processExists(os.Getpid()) {
		t.Errorf("expected current PID %d to exist", os.Getpid())
	}

	// PID 1 (init / launchd) is root-owned on Unix systems and must be detected as alive (EPERM handling)
	if !tm.processExists(1) {
		t.Error("expected PID 1 to be reported as existing (root-owned process)")
	}

	// Extremely large PID should not exist
	if tm.processExists(99999999) {
		t.Error("expected non-existent PID to return false")
	}
}

func TestTunnelManager_StalePID(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	// Write an invalid/stale PID
	pidFile := filepath.Join(dir, defaultPIDFileName)
	writePIDFile(t, pidFile, strconv.Itoa(99999999))

	running, _ := tm.IsRunning()
	if running {
		t.Error("expected running=false for non-existent PID")
	}

	// PID file should have been cleaned up
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestTunnelManager_IsRunning_AcceptsVerifiedSshuttle(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)
	writePIDFile(t, tm.PIDFilePath, strconv.Itoa(os.Getpid()))
	stubProcessIdentity(t, "/usr/local/bin/sshuttle --pidfile /tmp/x --daemon", "Mon Nov  9 10:00:00 2026")

	running, pid := tm.IsRunning()
	if !running || pid != os.Getpid() {
		t.Errorf("expected verified sshuttle PID %d to be running, got running=%v pid=%d", os.Getpid(), running, pid)
	}
}

func TestTunnelManager_IsRunning_RejectsNonSshuttlePID(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)
	writePIDFile(t, tm.PIDFilePath, strconv.Itoa(os.Getpid()))
	stubProcessIdentity(t, "/usr/sbin/cupsd -l", "Mon Nov  9 10:00:00 2026")

	running, pid := tm.IsRunning()
	if running || pid != 0 {
		t.Errorf("expected non-sshuttle PID to be stale, got running=%v pid=%d", running, pid)
	}
	if _, err := os.Stat(tm.PIDFilePath); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestTunnelManager_IsRunning_DetectsPIDReuse(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	// The PID file records an sshuttle process that started at 09:00.
	record, err := json.Marshal(pidFileRecord{
		PID:       os.Getpid(),
		Command:   "/usr/local/bin/sshuttle --daemon",
		StartTime: "Mon Nov  9 09:00:00 2026",
	})
	if err != nil {
		t.Fatal(err)
	}
	writePIDFile(t, tm.PIDFilePath, string(record))

	// The live PID now belongs to a different sshuttle invocation that started
	// at 10:00, so the PID was reused.
	stubProcessIdentity(t, "/usr/local/bin/sshuttle --daemon", "Mon Nov  9 10:00:00 2026")

	running, pid := tm.IsRunning()
	if running || pid != 0 {
		t.Errorf("expected reused PID to be stale, got running=%v pid=%d", running, pid)
	}
	if _, err := os.Stat(tm.PIDFilePath); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestTunnelManager_IsRunning_EPERMIdentityIsStale(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	// PID 1 exists but is root-owned. If its command line cannot be read
	// (EPERM), the PID is unverified and must be treated as stale.
	writePIDFile(t, tm.PIDFilePath, "1")
	stubProcessIdentityError(t, syscall.EPERM)

	running, pid := tm.IsRunning()
	if running || pid != 0 {
		t.Errorf("expected unverifiable PID to be stale, got running=%v pid=%d", running, pid)
	}
	if _, err := os.Stat(tm.PIDFilePath); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestTunnelManager_Stop_DoesNotSignalUnverifiedPID(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)
	writePIDFile(t, tm.PIDFilePath, strconv.Itoa(os.Getpid()))
	stubProcessIdentity(t, "/usr/sbin/cupsd -l", "Mon Nov  9 10:00:00 2026")
	calls := stubTunnelCommands(t)

	if err := tm.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("expected no signals for an unverified PID, ran: %v", *calls)
	}
	if _, err := os.Stat(tm.PIDFilePath); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestTunnelManager_Stop_NoSignalOnEPERM(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)
	writePIDFile(t, tm.PIDFilePath, "1")
	stubProcessIdentityError(t, syscall.EPERM)
	calls := stubTunnelCommands(t)

	if err := tm.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("expected no signals when identity cannot be read, ran: %v", *calls)
	}
	if _, err := os.Stat(tm.PIDFilePath); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestIsSshuttleProcess(t *testing.T) {
	tests := []struct {
		name        string
		commandLine string
		want        bool
	}{
		{"absolute path", "/usr/local/bin/sshuttle --ns-hosts=10.0.0.1 --daemon", true},
		{"bare name", "sshuttle --daemon", true},
		{"python shebang install", "/opt/homebrew/bin/python3 /opt/homebrew/bin/sshuttle --daemon", true},
		{"macOS Python.framework", "/opt/homebrew/Cellar/python@3.15/3.15.2/Frameworks/Python.framework/Versions/3.15/Resources/Python.app/Contents/MacOS/Python /opt/homebrew/bin/sshuttle --ns-hosts=192.168.100.1 --to-ns=192.168.100.1:53 -r root@host 192.168.100.0/24 --pidfile /tmp/sshuttle.pid --daemon", true},
		{"macOS Python.framework non-sshuttle", "/opt/homebrew/Cellar/python@3.15/3.15.2/Frameworks/Python.framework/Versions/3.15/Resources/Python.app/Contents/MacOS/Python /tmp/other.py", false},
		{"unrelated daemon", "/usr/sbin/cupsd -l", false},
		{"sshuttle as argument", "vim /tmp/sshuttle", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSshuttleProcess(tt.commandLine); got != tt.want {
				t.Errorf("isSshuttleProcess(%q) = %v, want %v", tt.commandLine, got, tt.want)
			}
		})
	}
}

func TestBuildSshuttleArgs(t *testing.T) {
	args := BuildSshuttleArgs("root", "lab-host", "192.168.100.0/24", "192.168.100.1")

	// 1. Must NOT include --dns (which intercepts system /etc/resolv.conf and breaks workstation internet)
	for _, arg := range args {
		if arg == "--dns" {
			t.Errorf("BuildSshuttleArgs must NOT include '--dns' flag to avoid hijacking workstation DNS")
		}
	}

	// 2. Must include --ns-hosts targeting the lab gateway
	expectedNSHosts := "--ns-hosts=192.168.100.1"
	hasNSHosts := false
	for _, arg := range args {
		if arg == expectedNSHosts {
			hasNSHosts = true
			break
		}
	}
	if !hasNSHosts {
		t.Errorf("BuildSshuttleArgs missing %s", expectedNSHosts)
	}

	// 3. Must include --to-ns targeting the lab gateway on port 53
	expectedToNS := "--to-ns=192.168.100.1:53"
	hasToNS := false
	for _, arg := range args {
		if arg == expectedToNS {
			hasToNS = true
			break
		}
	}
	if !hasToNS {
		t.Errorf("BuildSshuttleArgs missing %s", expectedToNS)
	}

	// 4. Must include remote target and CIDR
	expectedRemote := "-r"
	expectedTarget := "root@lab-host"
	expectedCIDR := "192.168.100.0/24"
	hasRemote := false
	hasTarget := false
	hasCIDR := false
	for i, arg := range args {
		if arg == expectedRemote && i+1 < len(args) && args[i+1] == expectedTarget {
			hasRemote = true
			hasTarget = true
		}
		if arg == expectedCIDR {
			hasCIDR = true
		}
	}
	if !hasRemote || !hasTarget {
		t.Errorf("BuildSshuttleArgs missing -r %s", expectedTarget)
	}
	if !hasCIDR {
		t.Errorf("BuildSshuttleArgs missing %s", expectedCIDR)
	}
}

func TestTunnelManager_Start_ElevationPrompt(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	orig := execTunnelCmdFn
	origLookPath := lookPathTunnelFn
	execTunnelCmdFn = func(name string, args ...string) *exec.Cmd {
		// Mock command that writes current process PID into the PID file so IsRunning succeeds
		return exec.Command("sh", "-c", "echo "+strconv.Itoa(os.Getpid())+" > "+tm.PIDFilePath)
	}
	lookPathTunnelFn = func(file string) (string, error) {
		return "/mock/bin/sshuttle", nil
	}
	defer func() {
		execTunnelCmdFn = orig
		lookPathTunnelFn = origLookPath
	}()
	stubProcessIdentity(t, "/usr/local/bin/sshuttle --daemon", "Mon Nov  9 10:00:00 2026")

	out := captureStdout(func() {
		_ = tm.Start("root", "192.168.1.50", "192.168.100.0/24", "192.168.100.1")
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to configure local packet routing rules (pf / iptables)."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestTunnelManager_Start_RecordsIdentity(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	orig := execTunnelCmdFn
	origLookPath := lookPathTunnelFn
	execTunnelCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo "+strconv.Itoa(os.Getpid())+" > "+tm.PIDFilePath)
	}
	lookPathTunnelFn = func(file string) (string, error) {
		return "/mock/bin/sshuttle", nil
	}
	defer func() {
		execTunnelCmdFn = orig
		lookPathTunnelFn = origLookPath
	}()
	const startTime = "Mon Nov  9 10:00:00 2026"
	stubProcessIdentity(t, "/usr/local/bin/sshuttle --daemon", startTime)

	if err := tm.Start("root", "192.168.1.50", "192.168.100.0/24", "192.168.100.1"); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	data, err := os.ReadFile(tm.PIDFilePath)
	if err != nil {
		t.Fatalf("reading PID file: %v", err)
	}
	var rec pidFileRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("PID file is not a JSON identity record: %v (%s)", err, data)
	}
	if rec.PID != os.Getpid() {
		t.Errorf("expected recorded PID %d, got %d", os.Getpid(), rec.PID)
	}
	if rec.StartTime != startTime {
		t.Errorf("expected recorded start time %q, got %q", startTime, rec.StartTime)
	}
	if !isSshuttleProcess(rec.Command) {
		t.Errorf("expected recorded command to be sshuttle, got %q", rec.Command)
	}
}

func TestTunnelManager_Start_MissingSshuttle(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	origLookPath := lookPathTunnelFn
	lookPathTunnelFn = func(file string) (string, error) {
		return "", errors.New("not found")
	}
	defer func() { lookPathTunnelFn = origLookPath }()

	err := tm.Start("root", "192.168.1.50", "192.168.100.0/24", "192.168.100.1")
	if err == nil || !strings.Contains(err.Error(), "sshuttle binary not found") {
		t.Errorf("expected missing sshuttle error, got: %v", err)
	}
}

func TestTunnelManager_Stop_ElevationPrompt(t *testing.T) {
	dir := t.TempDir()
	tm := NewTunnelManager(dir)

	// A verified sshuttle record lets Stop proceed to the signal path.
	writePIDFile(t, tm.PIDFilePath, strconv.Itoa(os.Getpid()))
	stubProcessIdentity(t, "/usr/local/bin/sshuttle --daemon", "Mon Nov  9 10:00:00 2026")

	orig := execTunnelCmdFn
	execTunnelCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	defer func() { execTunnelCmdFn = orig }()

	out := captureStdout(func() {
		_ = tm.Stop()
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to signal the root-owned sshuttle process."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestWorkstation_Teardown_FailurePreservesStateFile(t *testing.T) {
	origUninstall := uninstallToolFn
	defer func() {
		uninstallToolFn = origUninstall
	}()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	ws := New(tmpDir, statePath)
	ws.State.RecordInstalled("pulumi", "curl-script", "/tmp/pulumi")
	if err := ws.StateMgr.Save(ws.State); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	uninstallToolFn = func(name string, info ManagedBinary, osInfo Info) error {
		return errors.New("simulated uninstall error")
	}

	err := ws.Teardown(nil)
	if err == nil {
		t.Fatalf("expected teardown to fail when dependency uninstall fails")
	}

	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatalf("state file should not have been removed on failure")
	}
}
