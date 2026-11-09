package workstation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpenSSHVersion(t *testing.T) {
	tests := []struct {
		input       string
		expectMajor int
		expectMinor int
		expectErr   bool
	}{
		{"OpenSSH_10.5p1, LibreSSL 3.3.6", 10, 5, false},
		{"OpenSSH_8.7p1, OpenSSL 1.1.1k  FIPS 25 Mar 2021", 8, 7, false},
		{"OpenSSH_8.0p1", 8, 0, false},
		{"OpenSSH_7.4p1, OpenSSL 1.0.2k-fips  26 Jan 2017", 7, 4, false},
		{"OpenSSH_9.0", 9, 0, false},
		{"Sun_SSH_1.1", 0, 0, true},
		{"unknown", 0, 0, true},
	}

	for _, tt := range tests {
		major, minor, err := parseOpenSSHVersion(tt.input)
		if tt.expectErr {
			if err == nil {
				t.Errorf("expected error for '%s', got major=%d minor=%d", tt.input, major, minor)
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error for '%s': %v", tt.input, err)
			continue
		}
		if major != tt.expectMajor || minor != tt.expectMinor {
			t.Errorf("for '%s': expected %d.%d, got %d.%d", tt.input, tt.expectMajor, tt.expectMinor, major, minor)
		}
	}
}

func TestParseGoVersion(t *testing.T) {
	tests := []struct {
		input       string
		expectMajor int
		expectMinor int
		expectErr   bool
	}{
		{"go version go1.27.3 darwin/arm64", 1, 27, false},
		{"go version go1.22.0 linux/amd64", 1, 22, false},
		{"go version go1.21.9 linux/amd64", 1, 21, false},
		{"go version go2.1.0 linux/amd64", 2, 1, false},
		{"invalid version output", 0, 0, true},
	}

	for _, tt := range tests {
		major, minor, err := parseGoVersion(tt.input)
		if tt.expectErr {
			if err == nil {
				t.Errorf("expected error for '%s', got major=%d minor=%d", tt.input, major, minor)
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error for '%s': %v", tt.input, err)
			continue
		}
		if major != tt.expectMajor || minor != tt.expectMinor {
			t.Errorf("for '%s': expected %d.%d, got %d.%d", tt.input, tt.expectMajor, tt.expectMinor, major, minor)
		}
	}
}

func TestVerifyUserDependencies_Scenarios(t *testing.T) {
	origLookPath := lookPathFn
	origCmdOutput := commandOutputFn
	defer func() {
		lookPathFn = origLookPath
		commandOutputFn = origCmdOutput
	}()

	tests := []struct {
		name        string
		mockPaths   map[string]string
		mockOutputs map[string]string
		expectErr   bool
		errContains []string
	}{
		{
			name: "All valid dependencies",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
				"go":  "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh":      "OpenSSH_9.6p1, LibreSSL 3.3.6",
				"/usr/local/bin/go": "go version go1.23.2 darwin/arm64",
			},
			expectErr: false,
		},
		{
			name: "OpenSSH exact minimum version 8.0 and Go 1.22",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
				"go":  "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh":      "OpenSSH_8.0p1",
				"/usr/local/bin/go": "go version go1.22.0 linux/amd64",
			},
			expectErr: false,
		},
		{
			name: "Go devel version allowed",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
				"go":  "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh":      "OpenSSH_8.9p1",
				"/usr/local/bin/go": "go version devel go1.24-5d9c228 linux/amd64",
			},
			expectErr: false,
		},
		{
			name: "SSH missing",
			mockPaths: map[string]string{
				"go": "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/local/bin/go": "go version go1.23.0 linux/amd64",
			},
			expectErr: true,
			errContains: []string{
				"'ssh' (OpenSSH >= 8.0) is not found",
				"user-managed dependency",
				"brew install openssh",
				"dnf install -y openssh-clients",
			},
		},
		{
			name: "SSH outdated (7.4)",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
				"go":  "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh":      "OpenSSH_7.4p1, OpenSSL 1.0.2k-fips  26 Jan 2017",
				"/usr/local/bin/go": "go version go1.23.0 linux/amd64",
			},
			expectErr: true,
			errContains: []string{
				"'ssh' version < 8.0 detected",
				"OpenSSH >= 8.0 is required",
				"brew upgrade openssh",
			},
		},
		{
			name: "SSH non-OpenSSH",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
				"go":  "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh":      "Dropbear v2020.81",
				"/usr/local/bin/go": "go version go1.23.0 linux/amd64",
			},
			expectErr: true,
			errContains: []string{
				"does not appear to be OpenSSH",
				"OpenSSH >= 8.0 is required",
			},
		},
		{
			name: "Go missing",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh": "OpenSSH_8.8p1",
			},
			expectErr: true,
			errContains: []string{
				"'go' (Go >= 1.22) is not found",
				"https://go.dev/dl/",
				"brew install go",
				"dnf install -y golang",
			},
		},
		{
			name: "Go outdated (1.21)",
			mockPaths: map[string]string{
				"ssh": "/usr/bin/ssh",
				"go":  "/usr/local/bin/go",
			},
			mockOutputs: map[string]string{
				"/usr/bin/ssh":      "OpenSSH_9.0p1",
				"/usr/local/bin/go": "go version go1.21.6 linux/amd64",
			},
			expectErr: true,
			errContains: []string{
				"'go' version < 1.22 detected",
				"Go >= 1.22 is required",
				"https://go.dev/dl/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookPathFn = func(file string) (string, error) {
				if p, ok := tt.mockPaths[file]; ok {
					return p, nil
				}
				return "", errors.New("command not found")
			}
			commandOutputFn = func(name string, args ...string) ([]byte, error) {
				if out, ok := tt.mockOutputs[name]; ok {
					return []byte(out), nil
				}
				return nil, errors.New("execution failed")
			}

			err := VerifyUserDependencies()
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				for _, substr := range tt.errContains {
					if !strings.Contains(err.Error(), substr) {
						t.Errorf("expected error message to contain '%s', got:\n%s", substr, err.Error())
					}
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestEnsureToolDependencies_Lifecycle(t *testing.T) {
	origLookPath := lookPathFn
	origInstall := installToolFn
	defer func() {
		lookPathFn = origLookPath
		installToolFn = origInstall
	}()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	sm := NewStateManager(statePath)
	state := NewEmptyState()

	installedCalls := make(map[string]int)
	installToolFn = func(name string, osInfo Info) (string, string, error) {
		installedCalls[name]++
		return "mock-install", "/mock/bin/" + name, nil
	}

	// 1. Simulate: sshuttle is installed, pulumi and ocm are missing
	lookPathFn = func(file string) (string, error) {
		if file == "sshuttle" {
			return "/opt/homebrew/bin/sshuttle", nil
		}
		return "", errors.New("not found")
	}

	dummyOS := Info{Type: OSDarwin, Distro: "macos", Architecture: "arm64"}
	err := EnsureToolDependencies(state, sm, dummyOS)
	if err != nil {
		t.Fatalf("unexpected error in EnsureToolDependencies: %v", err)
	}

	// Verify installed calls
	if installedCalls["sshuttle"] != 0 {
		t.Errorf("sshuttle was already present, should not have been installed")
	}
	if installedCalls["pulumi"] != 1 {
		t.Errorf("expected pulumi to be installed once, got %d", installedCalls["pulumi"])
	}
	if installedCalls["ocm"] != 1 {
		t.Errorf("expected ocm to be installed once, got %d", installedCalls["ocm"])
	}

	// Verify state tracking
	if state.IsInstalledByVirtlab("sshuttle") {
		t.Errorf("sshuttle should NOT be marked installedByVirtlab")
	}
	if !state.IsInstalledByVirtlab("pulumi") {
		t.Errorf("pulumi should be marked installedByVirtlab")
	}
	if !state.IsInstalledByVirtlab("ocm") {
		t.Errorf("ocm should be marked installedByVirtlab")
	}

	// Verify state was saved to disk
	loadedState, err := sm.Load()
	if err != nil {
		t.Fatalf("failed to reload state from disk: %v", err)
	}
	if !loadedState.IsInstalledByVirtlab("ocm") || !loadedState.IsInstalledByVirtlab("pulumi") {
		t.Errorf("reloaded state does not reflect installedByVirtlab flags")
	}

	// 2. Test idempotency: run EnsureToolDependencies again where now all tools exist
	lookPathFn = func(file string) (string, error) {
		return "/mock/bin/" + file, nil
	}

	err = EnsureToolDependencies(loadedState, sm, dummyOS)
	if err != nil {
		t.Fatalf("unexpected error in idempotent EnsureToolDependencies: %v", err)
	}
	if installedCalls["pulumi"] != 1 || installedCalls["ocm"] != 1 {
		t.Errorf("tools should not have been reinstalled")
	}
	if !loadedState.IsInstalledByVirtlab("ocm") || !loadedState.IsInstalledByVirtlab("pulumi") {
		t.Errorf("state flags must be preserved across idempotent runs")
	}
}

func TestUninstallManagedDependencies_Lifecycle(t *testing.T) {
	origUninstall := uninstallToolFn
	defer func() {
		uninstallToolFn = origUninstall
	}()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	sm := NewStateManager(statePath)
	state := NewEmptyState()

	// Setup:
	// - "sshuttle": pre-existing (NOT installed by virtlab)
	// - "pulumi": installed by virtlab
	// - "ocm": installed by virtlab
	state.RecordInstalled("pulumi", "curl-script", "/tmp/.pulumi/bin/pulumi")
	state.RecordInstalled("ocm", "direct-binary", "/tmp/.local/bin/ocm")
	if err := sm.Save(state); err != nil {
		t.Fatalf("failed to save setup state: %v", err)
	}

	uninstalledCalls := make(map[string]int)
	uninstallToolFn = func(name string, info ManagedBinary, osInfo Info) error {
		uninstalledCalls[name]++
		return nil
	}

	dummyOS := Info{Type: OSLinux, Distro: "fedora", Architecture: "amd64"}
	err := UninstallManagedDependencies(state, sm, dummyOS)
	if err != nil {
		t.Fatalf("unexpected error in UninstallManagedDependencies: %v", err)
	}

	// Assertions
	if uninstalledCalls["sshuttle"] != 0 {
		t.Errorf("sshuttle should NOT have been uninstalled")
	}
	if uninstalledCalls["pulumi"] != 1 {
		t.Errorf("expected pulumi to be uninstalled, got %d calls", uninstalledCalls["pulumi"])
	}
	if uninstalledCalls["ocm"] != 1 {
		t.Errorf("expected ocm to be uninstalled, got %d calls", uninstalledCalls["ocm"])
	}

	// State should have cleared managed packages
	if state.HasManagedPackages() {
		t.Errorf("state should no longer have managed packages after uninstall")
	}

	// Reloaded state from disk should also reflect the cleanup
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}
	if loaded.HasManagedPackages() {
		t.Errorf("reloaded state should have no managed packages")
	}
}

func TestWorkstation_Teardown_CleansUpState(t *testing.T) {
	origUninstall := uninstallToolFn
	defer func() {
		uninstallToolFn = origUninstall
	}()

	uninstallToolFn = func(name string, info ManagedBinary, osInfo Info) error {
		return nil
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	ws := New(tmpDir, statePath)
	ws.State.RecordInstalled("ocm", "brew", "/opt/homebrew/bin/ocm")
	if err := ws.StateMgr.Save(ws.State); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Verify state file exists before teardown
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file to exist before teardown")
	}

	// Teardown with nil config (tests fallback and complete cleanup)
	if err := ws.Teardown(nil); err != nil {
		t.Fatalf("unexpected error in ws.Teardown: %v", err)
	}

	// State file should be deleted because state is now completely empty
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("expected state file '%s' to be deleted on teardown", statePath)
	}
}

func TestEnsureLocalBinInPATH(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	EnsureLocalBinInPATH()

	currPath := os.Getenv("PATH")
	home, _ := os.UserHomeDir()
	expectedLocalBin := filepath.Join(home, ".local", "bin")
	expectedPulumiBin := filepath.Join(home, ".pulumi", "bin")

	if !strings.Contains(currPath, expectedLocalBin) {
		t.Errorf("expected PATH to contain %s, got: %s", expectedLocalBin, currPath)
	}
	if !strings.Contains(currPath, expectedPulumiBin) {
		t.Errorf("expected PATH to contain %s, got: %s", expectedPulumiBin, currPath)
	}
}

func TestVerifyUserDependencies_RealHost(t *testing.T) {
	// Should pass on the test workstation where Go >= 1.22 and OpenSSH >= 8.0 are installed
	err := VerifyUserDependencies()
	if err != nil {
		t.Fatalf("expected VerifyUserDependencies to pass on dev workstation, got: %v", err)
	}
}

func TestInstallToolLinux_Sshuttle_ElevationPrompt(t *testing.T) {
	origExec := execDnfCmdFn
	origLookPath := lookPathFn
	defer func() {
		execDnfCmdFn = origExec
		lookPathFn = origLookPath
	}()

	execDnfCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	lookPathFn = func(file string) (string, error) {
		return "/usr/bin/sshuttle", nil
	}

	linuxInfo := Info{Type: OSLinux, Distro: "fedora", Architecture: "amd64"}
	out := captureStdout(func() {
		_, _, err := installToolLinux("sshuttle", linuxInfo)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to install system packages via dnf."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}

func TestUninstallToolLinux_Dnf_ElevationPrompt(t *testing.T) {
	origExec := execDnfCmdFn
	origLookPath := lookPathFn
	defer func() {
		execDnfCmdFn = origExec
		lookPathFn = origLookPath
	}()

	execDnfCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	lookPathFn = func(file string) (string, error) {
		return "/usr/bin/sshuttle", nil
	}

	pkgInfo := ManagedBinary{
		InstallMethod: "dnf",
		BinaryPath:    "/usr/bin/sshuttle",
	}

	out := captureStdout(func() {
		err := uninstallToolLinux("sshuttle", pkgInfo)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	expectedElevation := "• Elevation: Administrator privileges (sudo) are required to remove system packages via dnf."
	if !strings.Contains(out, expectedElevation) {
		t.Errorf("expected elevation notice in output, got:\n%s", out)
	}
	if !strings.Contains(out, "You may be prompted for your password.") {
		t.Errorf("expected password prompt notice in output, got:\n%s", out)
	}
}
