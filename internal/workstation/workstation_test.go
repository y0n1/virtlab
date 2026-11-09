package workstation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(99999999)), 0o600); err != nil {
		t.Fatal(err)
	}

	running, _ := tm.IsRunning()
	if running {
		t.Error("expected running=false for non-existent PID")
	}

	// PID file should have been cleaned up
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
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

	// Write current process PID to PID file so tm.IsRunning() detects active tunnel
	if err := os.WriteFile(tm.PIDFilePath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

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
