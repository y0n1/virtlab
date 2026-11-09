package workstation

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	reOpenSSH = regexp.MustCompile(`OpenSSH_([0-9]+)(?:\.([0-9]+))?`)
	reGo      = regexp.MustCompile(`go([0-9]+)\.([0-9]+)`)

	// Injectable hooks for unit testing
	lookPathFn      = exec.LookPath
	commandOutputFn = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	execDnfCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command(name, args...)
	}
	installToolFn   = installTool
	uninstallToolFn = uninstallTool
)

// ToolDef describes a workstation tool-managed dependency.
type ToolDef struct {
	Name        string
	Description string
	Purpose     string
}

// ManagedToolList defines all binaries managed by virtlab.
var ManagedToolList = []ToolDef{
	{
		Name:        "sshuttle",
		Description: "Transparent proxy VPN over SSH",
		Purpose:     "Transparently routes TCP traffic for the cluster subnet and targeted DNS to the Host hypervisor without a VPN daemon.",
	},
	{
		Name:        "pulumi",
		Description: "Infrastructure as Code CLI",
		Purpose:     "Drives the Pulumi Automation API to declaratively provision storage pools, isolated networks, and guest VMs.",
	},
	{
		Name:        "ocm",
		Description: "OpenShift Cluster Manager CLI",
		Purpose:     "Authenticates with console.redhat.com to fetch the OpenShift pull secret automatically if none is provided, including non-interactively with VIRTLAB_OCM_TOKEN.",
	},
}

// VerifyUserDependencies checks that user-managed dependencies ('ssh' and 'go') are installed
// and meet minimal expected versions: OpenSSH >= 8.0 and Go >= 1.22.
func VerifyUserDependencies() error {
	var errs []string

	// 1. Verify OpenSSH >= 8.0
	sshPath, err := lookPathFn("ssh")
	if err != nil {
		errs = append(errs, "• 'ssh' (OpenSSH >= 8.0) is not found on Workstation PATH.\n"+
			"  This is a user-managed dependency. Please install OpenSSH:\n"+
			"    • macOS:         brew install openssh\n"+
			"    • Fedora/RHEL:   sudo dnf install -y openssh-clients\n"+
			"    • Ubuntu/Debian: sudo apt install -y openssh-client")
	} else {
		out, _ := commandOutputFn(sshPath, "-V")
		outputStr := strings.TrimSpace(string(out))

		major, minor, parseErr := parseOpenSSHVersion(outputStr)
		if parseErr != nil {
			if !strings.Contains(outputStr, "OpenSSH") {
				errs = append(errs, fmt.Sprintf("• 'ssh' detected (%s) but does not appear to be OpenSSH. OpenSSH >= 8.0 is required.", outputStr))
			} else {
				errs = append(errs, fmt.Sprintf("• Could not parse OpenSSH version from '%s'. OpenSSH >= 8.0 is required.", outputStr))
			}
		} else if major < 8 || (major == 8 && minor < 0) {
			errs = append(errs, fmt.Sprintf("• 'ssh' version < 8.0 detected (found: %s). OpenSSH >= 8.0 is required.\n"+
				"  Please upgrade OpenSSH:\n"+
				"    • macOS:         brew upgrade openssh\n"+
				"    • Fedora/RHEL:   sudo dnf upgrade -y openssh-clients\n"+
				"    • Ubuntu/Debian: sudo apt upgrade -y openssh-client", outputStr))
		}
	}

	// 2. Verify Go >= 1.22
	goPath, err := lookPathFn("go")
	if err != nil {
		errs = append(errs, "• 'go' (Go >= 1.22) is not found on Workstation PATH.\n"+
			"  This is a user-managed dependency. Please install Go >= 1.22:\n"+
			"    • Official:      https://go.dev/dl/\n"+
			"    • macOS:         brew install go\n"+
			"    • Fedora/RHEL:   sudo dnf install -y golang")
	} else {
		out, err := commandOutputFn(goPath, "version")
		outputStr := strings.TrimSpace(string(out))
		if err != nil {
			errs = append(errs, fmt.Sprintf("• Failed to execute 'go version': %v", err))
		} else {
			major, minor, parseErr := parseGoVersion(outputStr)
			if parseErr != nil {
				// If development version (e.g. devel go1.24), allow it
				if !strings.Contains(outputStr, "devel") {
					errs = append(errs, fmt.Sprintf("• Could not parse Go version from '%s'. Go >= 1.22 is required.", outputStr))
				}
			} else if major < 1 || (major == 1 && minor < 22) {
				errs = append(errs, fmt.Sprintf("• 'go' version < 1.22 detected (found: %s). Go >= 1.22 is required.\n"+
					"  Please upgrade Go >= 1.22:\n"+
					"    • Official:      https://go.dev/dl/\n"+
					"    • macOS:         brew upgrade go\n"+
					"    • Fedora/RHEL:   sudo dnf upgrade -y golang", outputStr))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("user-managed workstation dependencies missing or outdated:\n\n%s", strings.Join(errs, "\n\n"))
	}

	return nil
}

func parseOpenSSHVersion(output string) (int, int, error) {
	m := reOpenSSH.FindStringSubmatch(output)
	if len(m) < 2 {
		return 0, 0, fmt.Errorf("no OpenSSH version pattern found in '%s'", output)
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor := 0
	if len(m) > 2 && m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return major, minor, nil
}

func parseGoVersion(output string) (int, int, error) {
	m := reGo.FindStringSubmatch(output)
	if len(m) < 3 {
		return 0, 0, fmt.Errorf("no Go version pattern found in '%s'", output)
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}

// EnsureToolDependencies checks for tool-managed binaries (sshuttle, pulumi, ocm).
// If any are missing, it auto-installs them and records their installation in the State.
func EnsureToolDependencies(state *State, stateMgr *StateManager, osInfo Info) error {
	stateChanged := false

	for _, tool := range ManagedToolList {
		if _, err := lookPathFn(tool.Name); err == nil {
			// Already installed
			continue
		}

		// Missing: auto-install
		fmt.Printf("\n[Workstation] Missing tool-managed dependency: '%s'\n", tool.Name)
		fmt.Printf("  • Purpose: %s\n", tool.Purpose)
		fmt.Printf("  • Action: Auto-installing '%s' (will be tracked in %s)...\n", tool.Name, stateMgr.Path)

		method, installedPath, err := installToolFn(tool.Name, osInfo)
		if err != nil {
			return fmt.Errorf("failed to auto-install '%s': %w", tool.Name, err)
		}

		state.RecordInstalled(tool.Name, method, installedPath)
		stateChanged = true
		fmt.Printf("✔ Successfully installed '%s' via %s at %s\n", tool.Name, method, installedPath)
	}

	if stateChanged {
		if err := stateMgr.Save(state); err != nil {
			return fmt.Errorf("failed to save workstation state: %w", err)
		}
	}

	return nil
}

// UninstallManagedDependencies checks if any tool-managed binaries were installed by virtlab,
// and uninstalls them to restore the workstation to its pre-lab state.
func UninstallManagedDependencies(state *State, stateMgr *StateManager, osInfo Info) error {
	if !state.HasManagedPackages() {
		return nil
	}

	fmt.Printf("\n[Workstation] Cleaning up tool-managed dependencies installed by virtlab...\n")

	// Collect keys to allow safe deletion during iteration
	var toUninstall []string
	for name, mb := range state.ManagedPackages {
		if mb.InstalledByVirtlab {
			toUninstall = append(toUninstall, name)
		}
	}

	for _, name := range toUninstall {
		pkgInfo := state.ManagedPackages[name]
		fmt.Printf("  • Uninstalling '%s' (method: %s, path: %s)...\n", name, pkgInfo.InstallMethod, pkgInfo.BinaryPath)

		if err := uninstallToolFn(name, pkgInfo, osInfo); err != nil {
			fmt.Printf("⚠ Warning: failed to uninstall '%s': %v (please remove manually)\n", name, err)
		} else {
			fmt.Printf("✔ Cleanly uninstalled '%s'\n", name)
		}

		state.RecordUninstalled(name)
	}

	if err := stateMgr.Save(state); err != nil {
		return fmt.Errorf("failed to update state after uninstallation: %w", err)
	}

	return nil
}

func installTool(name string, osInfo Info) (string, string, error) {
	switch osInfo.Type {
	case OSDarwin:
		return installToolDarwin(name)
	case OSLinux:
		return installToolLinux(name, osInfo)
	default:
		return "", "", fmt.Errorf("automatic installation of '%s' is not supported on OS type '%s'", name, osInfo.Type)
	}
}

func uninstallTool(name string, info ManagedBinary, osInfo Info) error {
	switch osInfo.Type {
	case OSDarwin:
		return uninstallToolDarwin(name, info)
	case OSLinux:
		return uninstallToolLinux(name, info)
	default:
		return fmt.Errorf("uninstallation of '%s' is not supported on OS type '%s'", name, osInfo.Type)
	}
}

// =========================================================================
// macOS (Darwin) Implementation via Homebrew
// =========================================================================

func installToolDarwin(name string) (method string, path string, err error) {
	brewPath, err := lookPathFn("brew")
	if err != nil {
		// If brew is missing, but tool is ocm, fall back to direct binary download
		if name == "ocm" {
			arch := "arm64"
			if runtime.GOARCH == "amd64" {
				arch = "amd64"
			}
			home, _ := os.UserHomeDir()
			localBin := filepath.Join(home, ".local", "bin")
			dest := filepath.Join(localBin, "ocm")
			url := fmt.Sprintf("https://github.com/openshift-online/ocm-cli/releases/latest/download/ocm-darwin-%s", arch)
			if dlErr := downloadFile(url, dest); dlErr == nil {
				return "direct-binary", dest, nil
			}
		}
		return "", "", fmt.Errorf("homebrew ('brew') is required to auto-install '%s' on macOS. Please install Homebrew (https://brew.sh) or manually install '%s'", name, name)
	}

	var formula string
	switch name {
	case "sshuttle":
		formula = "sshuttle"
	case "pulumi":
		formula = "pulumi"
	case "ocm":
		formula = "ocm"
	default:
		formula = name
	}

	cmd := exec.Command(brewPath, "install", formula)
	cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("brew install %s failed: %w", formula, err)
	}

	installedPath, err := lookPathFn(name)
	if err != nil {
		if _, statErr := os.Stat(filepath.Join("/opt/homebrew/bin", name)); statErr == nil {
			installedPath = filepath.Join("/opt/homebrew/bin", name)
		} else if _, statErr := os.Stat(filepath.Join("/usr/local/bin", name)); statErr == nil {
			installedPath = filepath.Join("/usr/local/bin", name)
		} else {
			installedPath = filepath.Join("/opt/homebrew/bin", name)
		}
	}

	return "brew", installedPath, nil
}

func uninstallToolDarwin(name string, info ManagedBinary) error {
	if info.InstallMethod == "direct-binary" {
		if info.BinaryPath != "" {
			err := os.Remove(info.BinaryPath)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		home, _ := os.UserHomeDir()
		err := os.Remove(filepath.Join(home, ".local", "bin", name))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	brewPath, err := lookPathFn("brew")
	if err != nil {
		if info.BinaryPath != "" {
			_ = os.Remove(info.BinaryPath)
		}
		return nil
	}

	// If binary does not exist on PATH, consider it already uninstalled
	if _, err := lookPathFn(name); err != nil {
		return nil
	}

	cmd := exec.Command(brewPath, "uninstall", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// If brew uninstall reports keg missing or already gone, treat as success
		return nil
	}
	return nil
}

// =========================================================================
// Linux / Fedora Implementation
// =========================================================================

func installToolLinux(name string, osInfo Info) (method string, path string, err error) {
	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(localBin, 0755)

	switch name {
	case "sshuttle":
		// sshuttle on Linux requires system python / dnf
		if osInfo.Distro == "fedora" || osInfo.Distro == "rhel" || osInfo.Distro == "centos" {
			fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to install system packages via dnf.\n")
			fmt.Printf("               You may be prompted for your password.\n\n")
			cmd := execDnfCmdFn("sudo", "dnf", "install", "-y", "sshuttle")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return "", "", fmt.Errorf("sudo dnf install -y sshuttle failed: %w", err)
			}
			p, _ := lookPathFn("sshuttle")
			if p == "" {
				p = "/usr/bin/sshuttle"
			}
			return "dnf", p, nil
		}
		return "", "", fmt.Errorf("please install 'sshuttle' via your Linux package manager (e.g. 'sudo dnf install sshuttle')")

	case "pulumi":
		if _, err := lookPathFn("curl"); err != nil {
			return "", "", fmt.Errorf("'curl' is required to install Pulumi. Please install curl (e.g. 'sudo dnf install curl')")
		}
		// Official standalone installer script into ~/.pulumi/bin
		cmd := exec.Command("sh", "-c", "curl -fsSL https://get.pulumi.com | sh")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", "", fmt.Errorf("failed to install pulumi via get.pulumi.com: %w", err)
		}
		p := filepath.Join(home, ".pulumi", "bin", "pulumi")
		// Symlink to ~/.local/bin/pulumi if ~/.local/bin is present
		targetSymlink := filepath.Join(localBin, "pulumi")
		_ = os.Remove(targetSymlink)
		_ = os.Symlink(p, targetSymlink)

		return "curl-script", p, nil

	case "ocm":
		// Download standalone binary from GitHub releases
		arch := "amd64"
		if osInfo.Architecture == "arm64" {
			arch = "arm64"
		}
		url := fmt.Sprintf("https://github.com/openshift-online/ocm-cli/releases/latest/download/ocm-linux-%s", arch)
		dest := filepath.Join(localBin, "ocm")

		if err := downloadFile(url, dest); err != nil {
			return "", "", fmt.Errorf("failed to download ocm binary from %s: %w", url, err)
		}

		return "direct-binary", dest, nil

	default:
		return "", "", fmt.Errorf("unknown Linux tool '%s'", name)
	}
}

func uninstallToolLinux(name string, info ManagedBinary) error {
	home, _ := os.UserHomeDir()

	switch info.InstallMethod {
	case "dnf":
		if _, err := lookPathFn(name); err != nil {
			return nil // already uninstalled
		}
		fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to remove system packages via dnf.\n")
		fmt.Printf("               You may be prompted for your password.\n\n")
		cmd := execDnfCmdFn("sudo", "dnf", "remove", "-y", name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "direct-binary":
		if info.BinaryPath != "" {
			err := os.Remove(info.BinaryPath)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		err := os.Remove(filepath.Join(home, ".local", "bin", name))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err

	case "curl-script":
		if name == "pulumi" {
			_ = os.Remove(filepath.Join(home, ".local", "bin", "pulumi"))
			_ = os.RemoveAll(filepath.Join(home, ".pulumi", "bin"))
			if info.BinaryPath != "" {
				_ = os.Remove(info.BinaryPath)
			}
			return nil
		}
		return nil

	default:
		if info.BinaryPath != "" {
			err := os.Remove(info.BinaryPath)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		return nil
	}
}

func downloadFile(url, destPath string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP GET %s returned %s", url, resp.Status)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory '%s': %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", destPath, time.Now().UnixNano())
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, resp.Body)
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

// EnsureLocalBinInPATH ensures ~/.local/bin and ~/.pulumi/bin are visible in the current process environment.
func EnsureLocalBinInPATH() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	currPath := os.Getenv("PATH")
	paths := filepath.SplitList(currPath)
	hasPath := func(target string) bool {
		for _, p := range paths {
			if filepath.Clean(p) == filepath.Clean(target) {
				return true
			}
		}
		return false
	}

	var toAdd []string
	localBin := filepath.Join(home, ".local", "bin")
	pulumiBin := filepath.Join(home, ".pulumi", "bin")

	if !hasPath(localBin) {
		toAdd = append(toAdd, localBin)
	}
	if !hasPath(pulumiBin) {
		toAdd = append(toAdd, pulumiBin)
	}

	// Also ensure standard Homebrew directories are in PATH if present
	for _, brewBin := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if _, err := os.Stat(brewBin); err == nil && !hasPath(brewBin) {
			toAdd = append(toAdd, brewBin)
		}
	}

	if len(toAdd) > 0 {
		newPath := strings.Join(toAdd, string(filepath.ListSeparator)) + string(filepath.ListSeparator) + currPath
		_ = os.Setenv("PATH", newPath)
	}
}
