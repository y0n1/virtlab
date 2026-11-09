package cluster

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/y0n1/virtlab/internal/config"
)

func createTestSSHKey(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "id_ed25519.pub")
	err := os.WriteFile(keyFile, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGoF9 user@workstation\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to create temp ssh key: %v", err)
	}
	return keyFile
}

func TestRenderConfigs_SNO(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		Preset:      config.PresetSNO,
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
	}

	mgr := NewOpenShiftManager(cfg, nil)

	ic, err := mgr.RenderInstallConfig()
	if err != nil {
		t.Fatalf("render install-config: %v", err)
	}
	if !strings.Contains(ic, "replicas: 1") {
		t.Errorf("expected controlPlane replicas: 1 in install-config:\n%s", ic)
	}
	if !strings.Contains(ic, "replicas: 0") {
		t.Errorf("expected compute replicas: 0 in install-config:\n%s", ic)
	}

	ac, err := mgr.RenderAgentConfig()
	if err != nil {
		t.Fatalf("render agent-config: %v", err)
	}
	if !strings.Contains(ac, "hostname: master-0.lab-sno.example.com") {
		t.Errorf("expected master-0 FQDN in agent-config:\n%s", ac)
	}
	if strings.Contains(ac, "master-1") {
		t.Errorf("master-1 should NOT exist in SNO agent-config:\n%s", ac)
	}
}

func TestRenderConfigs_HA(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-ha",
		BaseDomain:  "example.com",
		Preset:      config.PresetHA,
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
	}

	mgr := NewOpenShiftManager(cfg, nil)

	ic, err := mgr.RenderInstallConfig()
	if err != nil {
		t.Fatalf("render install-config: %v", err)
	}
	if !strings.Contains(ic, "replicas: 3") {
		t.Errorf("expected controlPlane replicas: 3 in install-config:\n%s", ic)
	}
	if !strings.Contains(ic, "replicas: 2") {
		t.Errorf("expected compute replicas: 2 in install-config:\n%s", ic)
	}

	ac, err := mgr.RenderAgentConfig()
	if err != nil {
		t.Fatalf("render agent-config: %v", err)
	}
	for _, expectedNode := range []string{"master-0", "master-1", "master-2", "worker-0", "worker-1"} {
		if !strings.Contains(ac, expectedNode) {
			t.Errorf("expected %s in HA agent-config:\n%s", expectedNode, ac)
		}
	}
}

type agentConfigDoc struct {
	Hosts []struct {
		Hostname      string `yaml:"hostname"`
		NetworkConfig struct {
			Interfaces []struct {
				IPv4 struct {
					Address []struct {
						IP           string `yaml:"ip"`
						PrefixLength int    `yaml:"prefix-length"`
					} `yaml:"address"`
				} `yaml:"ipv4"`
			} `yaml:"interfaces"`
		} `yaml:"networkConfig"`
	} `yaml:"hosts"`
}

func TestRenderAgentConfig_PrefixLength(t *testing.T) {
	tests := []struct {
		name       string
		preset     string
		cidr       string
		wantPrefix int
		wantHosts  int
	}{
		{"16-bit prefix", config.PresetHA, "192.168.0.0/16", 16, 5},
		{"24-bit prefix", config.PresetHA, "192.168.100.0/24", 24, 5},
		{"28-bit prefix", config.PresetSNO, "192.168.100.0/28", 28, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				ClusterName: "lab-prefix",
				BaseDomain:  "example.com",
				Preset:      tt.preset,
				Network: config.NetworkConfig{
					CIDR:      tt.cidr,
					GatewayIP: "192.168.100.1",
				},
			}
			mgr := NewOpenShiftManager(cfg, nil)

			ac, err := mgr.RenderAgentConfig()
			if err != nil {
				t.Fatalf("render agent-config: %v", err)
			}

			var doc agentConfigDoc
			if err := yaml.Unmarshal([]byte(ac), &doc); err != nil {
				t.Fatalf("parse agent-config: %v\n%s", err, ac)
			}
			if len(doc.Hosts) != tt.wantHosts {
				t.Fatalf("hosts = %d, want %d:\n%s", len(doc.Hosts), tt.wantHosts, ac)
			}
			for _, host := range doc.Hosts {
				if len(host.NetworkConfig.Interfaces) != 1 {
					t.Fatalf("host %s interfaces = %d, want 1", host.Hostname, len(host.NetworkConfig.Interfaces))
				}
				addrs := host.NetworkConfig.Interfaces[0].IPv4.Address
				if len(addrs) != 1 {
					t.Fatalf("host %s addresses = %d, want 1", host.Hostname, len(addrs))
				}
				if addrs[0].PrefixLength != tt.wantPrefix {
					t.Errorf("host %s prefix-length = %d, want %d", host.Hostname, addrs[0].PrefixLength, tt.wantPrefix)
				}
			}
		})
	}
}

type installConfigPlatformDoc struct {
	Platform struct {
		None      *struct{} `yaml:"none"`
		BareMetal *struct {
			APIVIPs             []string `yaml:"apiVIPs"`
			IngressVIPs         []string `yaml:"ingressVIPs"`
			ProvisioningNetwork string   `yaml:"provisioningNetwork"`
		} `yaml:"baremetal"`
	} `yaml:"platform"`
}

func TestRenderInstallConfig_Platform(t *testing.T) {
	tests := []struct {
		preset        string
		wantBaremetal bool
	}{
		{config.PresetSNO, false},
		{config.PresetCompact, true},
		{config.PresetHA, true},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			cfg := &config.Config{
				ClusterName: "lab-platform",
				BaseDomain:  "example.com",
				Preset:      tt.preset,
				HostSSH:     config.HostSSHConfig{PublicKeyFile: createTestSSHKey(t)},
				Network: config.NetworkConfig{
					CIDR:      "192.168.100.0/24",
					GatewayIP: "192.168.100.1",
				},
			}
			mgr := NewOpenShiftManager(cfg, nil)

			ic, err := mgr.RenderInstallConfig()
			if err != nil {
				t.Fatalf("render install-config: %v", err)
			}

			var doc installConfigPlatformDoc
			if err := yaml.Unmarshal([]byte(ic), &doc); err != nil {
				t.Fatalf("parse install-config: %v\n%s", err, ic)
			}

			if !tt.wantBaremetal {
				if doc.Platform.BareMetal != nil {
					t.Fatalf("SNO must use platform: none, got baremetal:\n%s", ic)
				}
				if doc.Platform.None == nil {
					t.Fatalf("expected platform.none for SNO:\n%s", ic)
				}
				return
			}

			if doc.Platform.None != nil {
				t.Fatalf("non-SNO must not use platform: none:\n%s", ic)
			}
			if doc.Platform.BareMetal == nil {
				t.Fatalf("expected platform.baremetal:\n%s", ic)
			}
			if got := doc.Platform.BareMetal.APIVIPs; len(got) != 1 || got[0] != "192.168.100.5" {
				t.Errorf("apiVIPs = %v, want [192.168.100.5]", got)
			}
			if got := doc.Platform.BareMetal.IngressVIPs; len(got) != 1 || got[0] != "192.168.100.6" {
				t.Errorf("ingressVIPs = %v, want [192.168.100.6]", got)
			}
			if doc.Platform.BareMetal.ProvisioningNetwork != "Disabled" {
				t.Errorf("provisioningNetwork = %q, want Disabled", doc.Platform.BareMetal.ProvisioningNetwork)
			}

			ac, err := mgr.RenderAgentConfig()
			if err != nil {
				t.Fatalf("render agent-config: %v", err)
			}
			if strings.Contains(ac, "apiVIPs") || strings.Contains(ac, "ingressVIPs") {
				t.Errorf("agent-config must not carry VIPs:\n%s", ac)
			}
		})
	}
}

func TestBuildWaitScript(t *testing.T) {
	mgr := &OpenShiftManager{}
	dir := "/home/libvirt/test-cluster/installer"
	script := mgr.BuildWaitScript(dir)

	requiredSnippets := []string{
		"umask 0077",
		"DIR='/home/libvirt/test-cluster/installer'",
		`LOG="$DIR/.openshift_install.log"`,
		"LINES=$(wc -l < \"$LOG\" 2>/dev/null || echo 0)",
		"START_LINE=$((LINES + 1))",
		`openshift-install --dir "$DIR" agent wait-for install-complete --log-level=info &`,
		`tail -n +"$START_LINE" -f "$LOG" &`,
		"trap 'kill $INSTALL_PID $TAIL_PID 2>/dev/null || true' EXIT HUP INT TERM",
		"wait $INSTALL_PID || STATUS=$?",
		"kill $TAIL_PID 2>/dev/null || true",
		"exit $STATUS",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Errorf("expected wait script to contain %q, but got:\n%s", snippet, script)
		}
	}
}

func TestBuildSetupScript_SecurityPermissions(t *testing.T) {
	mgr := &OpenShiftManager{}
	dir := "/home/libvirt/test-cluster/installer"
	ic := "fake-install-config"
	ac := "fake-agent-config"

	script := mgr.BuildSetupScript(dir, ic, ac)

	// Positive assertions
	expectedSnippets := []string{
		"umask 0077",
		"mkdir -p -m 0700 \"$DIR\"",
		"chmod 0700 \"$DIR\"",
		"chmod 0600 \"$DIR/install-config.yaml\" \"$DIR/agent-config.yaml\"",
		"os.chmod(p, 0o600)",
		// Credentials are backed up, not deleted.
		"cp -a \"$DIR/auth\" \"$DIR/auth.backup\"",
		// Manifests are replaced only when their content changes.
		"cmp -s \"$IC_TMP\" \"$DIR/install-config.yaml\"",
		"cmp -s \"$AC_TMP\" \"$DIR/agent-config.yaml\"",
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(script, snippet) {
			t.Errorf("expected setup script to contain %q, but got:\n%s", snippet, script)
		}
	}

	// Negative assertions: ensure insecure modes are nowhere in the script,
	// and that no path can wipe the installer directory or its credentials.
	forbiddenSnippets := []string{
		"chmod 0755",
		"chmod 0644",
		"chmod 0666",
		"chmod 0777",
		`find "$DIR" -mindepth 1 -delete`,
		`rm -rf "$DIR"`,
		`rm -f "$DIR/agent.x86_64.iso"`,
		`rm -f "$DIR/auth`,
	}
	for _, forbidden := range forbiddenSnippets {
		if strings.Contains(script, forbidden) {
			t.Errorf("security violation: setup script contains forbidden permissive mode %q:\n%s", forbidden, script)
		}
	}
}

func TestBuildCreateIsoScript_SecureIsoPermissions(t *testing.T) {
	mgr := &OpenShiftManager{}
	dir := "/home/libvirt/test-cluster/installer"

	script := mgr.BuildCreateIsoScript(dir)

	// Positive assertions: must contain umask 0077 and 0640 (scoped to qemu/kvm) or 0600
	if !strings.Contains(script, "umask 0077") {
		t.Errorf("expected build ISO script to enforce 'umask 0077', got:\n%s", script)
	}
	if !strings.Contains(script, `chmod 0640 "$DIR/agent.x86_64.iso"`) && !strings.Contains(script, `chmod 0600 "$DIR/agent.x86_64.iso"`) {
		t.Errorf("expected build ISO script to enforce 0640 or 0600 permissions, got:\n%s", script)
	}
	if !strings.Contains(script, `chmod 0750 "$DIR"`) && !strings.Contains(script, `chmod 0700 "$DIR"`) {
		t.Errorf("expected build ISO script to restrict directory permissions, got:\n%s", script)
	}

	// Negative assertions: world-readable 0644 must NEVER appear
	if strings.Contains(script, `chmod 0644`) {
		t.Errorf("security violation: build ISO script contains insecure 'chmod 0644':\n%s", script)
	}
}

func TestBuildIsoCleanupScript(t *testing.T) {
	mgr := &OpenShiftManager{}
	dir := "/home/libvirt/test-cluster/installer"

	script := mgr.BuildIsoCleanupScript(dir)

	expectedSnippets := []string{
		`rm -f "$DIR/agent.x86_64.iso"`,
		`chmod 0700 "$DIR"`,
		`chmod 0700 "$DIR/auth"`,
		`chmod 0600 "$DIR/auth"/*`,
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(script, snippet) {
			t.Errorf("expected ISO cleanup script to contain %q, got:\n%s", snippet, script)
		}
	}
}

func TestBuildMediaEjectCommand(t *testing.T) {
	mgr := &OpenShiftManager{}
	dom := "test-cluster-master-0"

	cmd := mgr.BuildMediaEjectCommand(dom)

	expectedSnippets := []string{
		"virsh change-media",
		"'test-cluster-master-0'",
		"sda",
		"--eject",
		"--live",
		"--config",
		"|| true",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cmd, snippet) {
			t.Errorf("expected media eject command to contain %q, got: %s", snippet, cmd)
		}
	}
}

func TestSetupScript_LocalFilesystemExecution(t *testing.T) {
	tempDir := t.TempDir()
	installDir := filepath.Join(tempDir, "installer")
	pullSecretFile := filepath.Join(tempDir, "pull-secret.json")
	if err := os.WriteFile(pullSecretFile, []byte(`{"auths":{"cloud.openshift.com":{"auth":"dGVzdDp0ZXN0"}}}`), 0o600); err != nil {
		t.Fatalf("failed to write test pull secret: %v", err)
	}

	mgr := &OpenShiftManager{}
	ic := "apiVersion: v1\nmetadata:\n  name: test-cluster\n"
	ac := "apiVersion: v1alpha1\nkind: AgentConfig\n"

	script := mgr.BuildSetupScript(installDir, ic, ac)
	// Replace /root/pull-secret.json with our local test file for offline local test execution
	script = strings.ReplaceAll(script, "/root/pull-secret.json", pullSecretFile)

	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup script local execution failed: %v (stderr: %s)", err, stderr.String())
	}

	// 1. Verify installer directory permissions are 0700
	dirStat, err := os.Stat(installDir)
	if err != nil {
		t.Fatalf("stat installDir failed: %v", err)
	}
	if perm := dirStat.Mode().Perm(); perm != 0o700 {
		t.Errorf("installDir permissions = %04o; want 0700", perm)
	}

	// 2. Verify install-config.yaml permissions are 0600
	icPath := filepath.Join(installDir, "install-config.yaml")
	icStat, err := os.Stat(icPath)
	if err != nil {
		t.Fatalf("stat install-config.yaml failed: %v", err)
	}
	if perm := icStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("install-config.yaml permissions = %04o; want 0600", perm)
	}

	// 3. Verify agent-config.yaml permissions are 0600
	acPath := filepath.Join(installDir, "agent-config.yaml")
	acStat, err := os.Stat(acPath)
	if err != nil {
		t.Fatalf("stat agent-config.yaml failed: %v", err)
	}
	if perm := acStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("agent-config.yaml permissions = %04o; want 0600", perm)
	}
}

// runSetupScriptLocally executes the setup script against a temporary install
// directory and returns that directory. The pull-secret path is redirected to
// a local test file so the script runs without a Host.
func runSetupScriptLocally(t *testing.T, tempDir string, ic, ac string) string {
	t.Helper()
	installDir := filepath.Join(tempDir, "installer")
	pullSecretFile := filepath.Join(tempDir, "pull-secret.json")
	if err := os.WriteFile(pullSecretFile, []byte(`{"auths":{"cloud.openshift.com":{"auth":"dGVzdDp0ZXN0"}}}`), 0o600); err != nil {
		t.Fatalf("failed to write test pull secret: %v", err)
	}

	mgr := &OpenShiftManager{}
	script := mgr.BuildSetupScript(installDir, ic, ac)
	script = strings.ReplaceAll(script, "/root/pull-secret.json", pullSecretFile)

	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup script local execution failed: %v (stderr: %s)", err, stderr.String())
	}
	return installDir
}

func TestSetupScript_PreservesAuthAndIso(t *testing.T) {
	tempDir := t.TempDir()
	installDir := filepath.Join(tempDir, "installer")
	authDir := filepath.Join(installDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("create auth dir: %v", err)
	}
	kubeconfig := filepath.Join(authDir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	iso := filepath.Join(installDir, "agent.x86_64.iso")
	if err := os.WriteFile(iso, []byte("iso-bytes"), 0o600); err != nil {
		t.Fatalf("write ISO: %v", err)
	}

	runSetupScriptLocally(t, tempDir, "apiVersion: v1\n", "apiVersion: v1alpha1\n")

	if got, err := os.ReadFile(kubeconfig); err != nil || string(got) != "keep-me" {
		t.Errorf("auth/kubeconfig was not preserved: content=%q err=%v", got, err)
	}
	backup := filepath.Join(installDir, "auth.backup", "kubeconfig")
	if got, err := os.ReadFile(backup); err != nil || string(got) != "keep-me" {
		t.Errorf("auth backup was not created: content=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(iso); err != nil || string(got) != "iso-bytes" {
		t.Errorf("Agent ISO was not preserved: content=%q err=%v", got, err)
	}
}

func TestSetupScript_DoesNotRewriteUnchangedManifests(t *testing.T) {
	tempDir := t.TempDir()
	installDir := runSetupScriptLocally(t, tempDir, "apiVersion: v1\n", "apiVersion: v1alpha1\n")

	icPath := filepath.Join(installDir, "install-config.yaml")
	before, err := os.Stat(icPath)
	if err != nil {
		t.Fatalf("stat install-config: %v", err)
	}

	runSetupScriptLocally(t, tempDir, "apiVersion: v1\n", "apiVersion: v1alpha1\n")

	after, err := os.Stat(icPath)
	if err != nil {
		t.Fatalf("stat install-config after second run: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("install-config.yaml was rewritten even though its content did not change")
	}
	for _, leftover := range []string{".install-config.yaml.new", ".agent-config.yaml.new"} {
		if _, err := os.Stat(filepath.Join(installDir, leftover)); !os.IsNotExist(err) {
			t.Errorf("temporary manifest %s was left behind", leftover)
		}
	}
}

func TestBuildPersistStateScript(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "installer")
	body := "preset: ha\nocpVersion: 4.22.0\nnodes: []\n"
	script := BuildPersistStateScript(dir, body)

	for _, want := range []string{
		"umask 0077",
		`STATE="$DIR/.virtlab-install-state.yaml"`,
		`chmod 0600 "$TMP"`,
		`mv -f "$TMP" "$STATE"`,
		body,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("persist script should contain %q, got:\n%s", want, script)
		}
	}

	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("persist script local execution failed: %v (stderr: %s)", err, stderr.String())
	}

	statePath := filepath.Join(dir, ".virtlab-install-state.yaml")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat persisted state: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("persisted state permissions = %04o; want 0600", perm)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	parsed, err := ParseInstallState(string(data))
	if err != nil {
		t.Fatalf("parse persisted state: %v", err)
	}
	if parsed.Preset != "ha" || parsed.OCPVersion != "4.22.0" {
		t.Errorf("persisted state = %+v, want preset ha version 4.22.0", parsed)
	}
}
