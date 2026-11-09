package workstation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	execCmdFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command(name, args...)
	}
	macOSResolverDir = "/etc/resolver"
	linuxDropInDir   = "/etc/systemd/resolved.conf.d"
	statFileFn       = os.Stat
)

// SplitDNSManager manages Workstation DNS split-tunneling.
type SplitDNSManager struct {
	OS Info
}

// NewSplitDNSManager creates a new DNS manager for the detected Workstation OS.
func NewSplitDNSManager() *SplitDNSManager {
	return &SplitDNSManager{OS: DetectOS()}
}

// Configure sets up targeted split-DNS resolution for the cluster base domain.
func (m *SplitDNSManager) Configure(clusterName, baseDomain, gatewayIP string) error {
	switch m.OS.Type {
	case OSDarwin:
		return m.configureMacOS(baseDomain, gatewayIP)
	case OSLinux:
		return m.configureLinux(clusterName, baseDomain, gatewayIP)
	default:
		return fmt.Errorf("split-dns: unsupported workstation OS '%s'", m.OS.Type)
	}
}

// Teardown cleanly removes the split-DNS resolution and flushes DNS caches.
func (m *SplitDNSManager) Teardown(clusterName, baseDomain string) error {
	switch m.OS.Type {
	case OSDarwin:
		return m.teardownMacOS(baseDomain)
	case OSLinux:
		return m.teardownLinux(clusterName)
	default:
		return fmt.Errorf("split-dns: unsupported workstation OS '%s'", m.OS.Type)
	}
}

// safeChildPath validates that filename is a single-level child file within parentDir,
// preventing directory traversal attacks. It strictly rejects empty filenames,
// path separators ('/' or '\'), and parent traversal ('..').
func safeChildPath(parentDir, filename string) (string, error) {
	trimmed := strings.TrimSpace(filename)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return "", fmt.Errorf("invalid child path: filename cannot be empty or dot")
	}
	if strings.ContainsAny(filename, `/\`) || strings.Contains(filename, "..") {
		return "", fmt.Errorf("invalid child path '%s': contains path separators or traversal characters", filename)
	}

	cleanParent := filepath.Clean(parentDir)
	cleanTarget := filepath.Clean(filepath.Join(cleanParent, filename))

	parentWithSep := cleanParent
	if !strings.HasSuffix(parentWithSep, string(filepath.Separator)) {
		parentWithSep += string(filepath.Separator)
	}

	if !strings.HasPrefix(cleanTarget, parentWithSep) {
		return "", fmt.Errorf("unsafe target path '%s' escapes parent directory '%s'", cleanTarget, cleanParent)
	}

	rel, err := filepath.Rel(cleanParent, cleanTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe target path '%s' escapes parent directory '%s'", cleanTarget, cleanParent)
	}

	return cleanTarget, nil
}

// macOS implementation: writes /etc/resolver/<baseDomain>
func (m *SplitDNSManager) configureMacOS(baseDomain, gatewayIP string) error {
	resolverFile, err := safeChildPath(macOSResolverDir, baseDomain)
	if err != nil {
		return fmt.Errorf("macOS split-dns: invalid baseDomain '%s': %w", baseDomain, err)
	}
	resolverDir := macOSResolverDir

	fmt.Printf("\n[Workstation] Configuring macOS Split-DNS for '%s' -> %s\n", baseDomain, gatewayIP)
	fmt.Printf("  • Action:    Create %s with 'nameserver %s'\n", resolverFile, gatewayIP)
	fmt.Printf("  • Reason:    Enables the Workstation to resolve *.apps and API endpoints through the Lab Tunnel\n")
	fmt.Printf("               without touching or altering your general internet DNS.\n")
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to write to %s and flush mDNSResponder.\n", resolverDir)
	fmt.Printf("               You may be prompted for your password.\n\n")

	// Create directory if missing
	if err := execCmdFn("sudo", "mkdir", "-p", resolverDir).Run(); err != nil {
		return fmt.Errorf("macOS split-dns: create /etc/resolver: %w", err)
	}

	content := fmt.Sprintf("nameserver %s\n", gatewayIP)
	cmd := execCmdFn("sudo", "tee", resolverFile)
	cmd.Stdin = strings.NewReader(content)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("macOS split-dns: write %s: %w", resolverFile, err)
	}

	// Flush macOS DNS caches
	_ = execCmdFn("sudo", "dscacheutil", "-flushcache").Run()
	_ = execCmdFn("sudo", "killall", "-HUP", "mDNSResponder").Run()

	fmt.Printf("✔ macOS Split-DNS active: %s\n", resolverFile)
	return nil
}

func (m *SplitDNSManager) teardownMacOS(baseDomain string) error {
	resolverFile, err := safeChildPath(macOSResolverDir, baseDomain)
	if err != nil {
		return fmt.Errorf("macOS split-dns: invalid baseDomain '%s': %w", baseDomain, err)
	}
	if _, err := statFileFn(resolverFile); os.IsNotExist(err) {
		return nil // already clean
	}

	fmt.Printf("\n[Workstation] Reverting macOS Split-DNS for '%s'\n", baseDomain)
	fmt.Printf("  • Action:    Remove %s and flush DNS cache\n", resolverFile)
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to delete %s and reload mDNSResponder.\n", resolverFile)
	fmt.Printf("               You may be prompted for your password.\n\n")

	if err := execCmdFn("sudo", "rm", "-f", resolverFile).Run(); err != nil {
		return fmt.Errorf("macOS split-dns: remove %s: %w", resolverFile, err)
	}

	_ = execCmdFn("sudo", "dscacheutil", "-flushcache").Run()
	_ = execCmdFn("sudo", "killall", "-HUP", "mDNSResponder").Run()

	fmt.Printf("✔ macOS Split-DNS removed: %s\n", resolverFile)
	return nil
}

// Linux (Fedora / systemd-resolved) implementation
func (m *SplitDNSManager) configureLinux(clusterName, baseDomain, gatewayIP string) error {
	dropInFile, err := safeChildPath(linuxDropInDir, fmt.Sprintf("lab-%s.conf", clusterName))
	if err != nil {
		return fmt.Errorf("linux split-dns: invalid clusterName '%s': %w", clusterName, err)
	}
	if strings.ContainsAny(baseDomain, "\r\n") {
		return fmt.Errorf("linux split-dns: invalid baseDomain '%s': contains newline characters", baseDomain)
	}
	dropInDir := linuxDropInDir

	fmt.Printf("\n[Workstation] Configuring Linux Split-DNS (systemd-resolved) for '%s' -> %s\n", baseDomain, gatewayIP)
	fmt.Printf("  • Action:    Create %s\n", dropInFile)
	fmt.Printf("  • Reason:    Directs queries for ~%s to the Lab Gateway via systemd-resolved.\n", baseDomain)
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to write %s and restart systemd-resolved.\n", dropInDir)
	fmt.Printf("               You may be prompted for your password.\n\n")

	if err := execCmdFn("sudo", "mkdir", "-p", dropInDir).Run(); err != nil {
		return fmt.Errorf("linux split-dns: create %s: %w", dropInDir, err)
	}

	content := fmt.Sprintf("[Resolve]\nDNS=%s\nDomains=~%s\n", gatewayIP, baseDomain)
	cmd := execCmdFn("sudo", "tee", dropInFile)
	cmd.Stdin = strings.NewReader(content)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("linux split-dns: write %s: %w", dropInFile, err)
	}

	_ = execCmdFn("sudo", "systemctl", "restart", "systemd-resolved").Run()

	fmt.Printf("✔ Linux Split-DNS active: %s\n", dropInFile)
	return nil
}

func (m *SplitDNSManager) teardownLinux(clusterName string) error {
	dropInFile, err := safeChildPath(linuxDropInDir, fmt.Sprintf("lab-%s.conf", clusterName))
	if err != nil {
		return fmt.Errorf("linux split-dns: invalid clusterName '%s': %w", clusterName, err)
	}
	if _, err := statFileFn(dropInFile); os.IsNotExist(err) {
		return nil
	}

	fmt.Printf("\n[Workstation] Reverting Linux Split-DNS for 'lab-%s'\n", clusterName)
	fmt.Printf("  • Action:    Remove %s and reload systemd-resolved\n", dropInFile)
	fmt.Printf("  • Elevation: Administrator privileges (sudo) are required to delete %s and restart systemd-resolved.\n", dropInFile)
	fmt.Printf("               You may be prompted for your password.\n\n")

	if err := execCmdFn("sudo", "rm", "-f", dropInFile).Run(); err != nil {
		return fmt.Errorf("linux split-dns: remove %s: %w", dropInFile, err)
	}

	_ = execCmdFn("sudo", "systemctl", "restart", "systemd-resolved").Run()

	fmt.Printf("✔ Linux Split-DNS removed: %s\n", dropInFile)
	return nil
}
