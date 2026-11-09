package workstation

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func captureStdout(f func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()
	f()
	_ = w.Close()
	os.Stdout = orig
	return <-outC
}

func mockExecCmdSuccess() func() {
	orig := execCmdFn
	execCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	return func() {
		execCmdFn = orig
	}
}

func TestSplitDNS_ConfigureMacOS_ElevationPrompt(t *testing.T) {
	cleanup := mockExecCmdSuccess()
	defer cleanup()

	tmpDir := t.TempDir()
	origDir := macOSResolverDir
	macOSResolverDir = tmpDir
	defer func() { macOSResolverDir = origDir }()

	mgr := &SplitDNSManager{OS: Info{Type: OSDarwin}}
	out := captureStdout(func() {
		err := mgr.configureMacOS("example.com", "192.168.100.1")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to write to " + tmpDir + " and flush mDNSResponder."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestSplitDNS_TeardownMacOS_ElevationPrompt(t *testing.T) {
	cleanup := mockExecCmdSuccess()
	defer cleanup()

	tmpDir := t.TempDir()
	origDir := macOSResolverDir
	macOSResolverDir = tmpDir
	defer func() { macOSResolverDir = origDir }()

	// Create dummy resolver file so teardown executes
	resolverFile := filepath.Join(tmpDir, "example.com")
	if err := os.WriteFile(resolverFile, []byte("nameserver 192.168.100.1\n"), 0644); err != nil {
		t.Fatalf("failed to create dummy resolver file: %v", err)
	}

	mgr := &SplitDNSManager{OS: Info{Type: OSDarwin}}
	out := captureStdout(func() {
		err := mgr.teardownMacOS("example.com")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to delete " + resolverFile + " and reload mDNSResponder."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestSplitDNS_ConfigureLinux_ElevationPrompt(t *testing.T) {
	cleanup := mockExecCmdSuccess()
	defer cleanup()

	tmpDir := t.TempDir()
	origDir := linuxDropInDir
	linuxDropInDir = tmpDir
	defer func() { linuxDropInDir = origDir }()

	mgr := &SplitDNSManager{OS: Info{Type: OSLinux}}
	out := captureStdout(func() {
		err := mgr.configureLinux("test-cluster", "example.com", "192.168.100.1")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to write " + tmpDir + " and restart systemd-resolved."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestSplitDNS_TeardownLinux_ElevationPrompt(t *testing.T) {
	cleanup := mockExecCmdSuccess()
	defer cleanup()

	tmpDir := t.TempDir()
	origDir := linuxDropInDir
	linuxDropInDir = tmpDir
	defer func() { linuxDropInDir = origDir }()

	// Create dummy drop-in file so teardown executes
	dropInFile := filepath.Join(tmpDir, "lab-test-cluster.conf")
	if err := os.WriteFile(dropInFile, []byte("[Resolve]\nDNS=192.168.100.1\n"), 0644); err != nil {
		t.Fatalf("failed to create dummy drop-in file: %v", err)
	}

	mgr := &SplitDNSManager{OS: Info{Type: OSLinux}}
	out := captureStdout(func() {
		err := mgr.teardownLinux("test-cluster")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to delete " + dropInFile + " and restart systemd-resolved."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestSplitDNS_PathContainment_MacOS(t *testing.T) {
	// Overwrite execCmdFn to ensure NO command is executed when an attack is attempted
	var cmdCalled bool
	origExec := execCmdFn
	execCmdFn = func(name string, args ...string) *exec.Cmd {
		cmdCalled = true
		t.Fatalf("SECURITY VIOLATION: command %s %v executed for malicious payload", name, args)
		return exec.Command("false")
	}
	defer func() { execCmdFn = origExec }()

	tmpDir := t.TempDir()
	origDir := macOSResolverDir
	macOSResolverDir = tmpDir
	defer func() { macOSResolverDir = origDir }()

	mgr := &SplitDNSManager{OS: Info{Type: OSDarwin}}

	maliciousDomains := []struct {
		name   string
		domain string
	}{
		{"Relative path traversal to /etc/hosts", "../../../../etc/hosts"},
		{"Relative path traversal to parent directory", "../sibling.conf"},
		{"Double slash traversal", "foo/../../etc/passwd"},
		{"Absolute path escaping resolverDir", "/etc/hosts"},
		{"Subdirectory path escape", "sub/domain.com"},
		{"Dot current directory", "."},
		{"Double dot parent directory", ".."},
		{"Backslash traversal", `..\..\windows\system32`},
	}

	for _, tc := range maliciousDomains {
		t.Run("configureMacOS_"+tc.name, func(t *testing.T) {
			cmdCalled = false
			err := mgr.configureMacOS(tc.domain, "192.168.100.1")
			if err == nil {
				t.Fatalf("expected error for malicious baseDomain %q, got nil", tc.domain)
			}
			if cmdCalled {
				t.Errorf("expected zero command invocations, but command was executed")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "escape") &&
				!strings.Contains(strings.ToLower(err.Error()), "invalid") &&
				!strings.Contains(strings.ToLower(err.Error()), "unsafe") {
				t.Errorf("error %q does not state security/containment reason", err.Error())
			}
		})

		t.Run("teardownMacOS_"+tc.name, func(t *testing.T) {
			cmdCalled = false
			err := mgr.teardownMacOS(tc.domain)
			if err == nil {
				t.Fatalf("expected error for malicious baseDomain %q during teardown, got nil", tc.domain)
			}
			if cmdCalled {
				t.Errorf("expected zero command invocations, but command was executed")
			}
		})
	}
}

func TestSplitDNS_PathContainment_Linux(t *testing.T) {
	var cmdCalled bool
	origExec := execCmdFn
	execCmdFn = func(name string, args ...string) *exec.Cmd {
		cmdCalled = true
		t.Fatalf("SECURITY VIOLATION: command %s %v executed for malicious payload", name, args)
		return exec.Command("false")
	}
	defer func() { execCmdFn = origExec }()

	tmpDir := t.TempDir()
	origDir := linuxDropInDir
	linuxDropInDir = tmpDir
	defer func() { linuxDropInDir = origDir }()

	mgr := &SplitDNSManager{OS: Info{Type: OSLinux}}

	maliciousClusters := []struct {
		name        string
		clusterName string
	}{
		{"Relative path traversal to /etc/shadow", "../../../../etc/shadow"},
		{"Relative path traversal to parent directory", "../escape"},
		{"Subdirectory traversal", "foo/bar"},
		{"Dot current directory", "."},
		{"Double dot parent directory", ".."},
	}

	for _, tc := range maliciousClusters {
		t.Run("configureLinux_"+tc.name, func(t *testing.T) {
			cmdCalled = false
			err := mgr.configureLinux(tc.clusterName, "example.com", "192.168.100.1")
			if err == nil {
				t.Fatalf("expected error for malicious clusterName %q, got nil", tc.clusterName)
			}
			if cmdCalled {
				t.Errorf("expected zero command invocations, but command was executed")
			}
		})

		t.Run("teardownLinux_"+tc.name, func(t *testing.T) {
			cmdCalled = false
			err := mgr.teardownLinux(tc.clusterName)
			if err == nil {
				t.Fatalf("expected error for malicious clusterName %q during teardown, got nil", tc.clusterName)
			}
			if cmdCalled {
				t.Errorf("expected zero command invocations, but command was executed")
			}
		})
	}
}

func TestSplitDNS_ValidPathContainment_Success(t *testing.T) {
	var executedCommands [][]string
	origExec := execCmdFn
	execCmdFn = func(name string, args ...string) *exec.Cmd {
		executedCommands = append(executedCommands, append([]string{name}, args...))
		return exec.Command("true")
	}
	defer func() { execCmdFn = origExec }()

	tmpDir := t.TempDir()
	origDir := macOSResolverDir
	macOSResolverDir = tmpDir
	defer func() { macOSResolverDir = origDir }()

	mgr := &SplitDNSManager{OS: Info{Type: OSDarwin}}
	err := mgr.configureMacOS("example.com", "192.168.100.1")
	if err != nil {
		t.Fatalf("unexpected error for valid baseDomain: %v", err)
	}

	// Verify that sudo tee targeted strictly tmpDir/example.com
	expectedTarget := filepath.Join(tmpDir, "example.com")
	foundTee := false
	for _, cmd := range executedCommands {
		if len(cmd) >= 3 && cmd[0] == "sudo" && cmd[1] == "tee" && cmd[2] == expectedTarget {
			foundTee = true
			break
		}
	}
	if !foundTee {
		t.Errorf("expected sudo tee targeting %s, executed commands: %v", expectedTarget, executedCommands)
	}
}
