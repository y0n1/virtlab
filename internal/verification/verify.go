package verification

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/host"
	"github.com/y0n1/virtlab/internal/infrastructure"
)

// Verifier executes Phase 4 E2E verification checks from the Workstation.
type Verifier struct {
	Config *config.Config
	Host   *host.Host
	Output io.Writer

	// lookupHost overrides net.LookupHost in tests.
	lookupHost func(string) ([]string, error)
	// httpClient overrides the probe client in tests.
	httpClient *http.Client
}

func (v *Verifier) lookup(host string) ([]string, error) {
	if v.lookupHost != nil {
		return v.lookupHost(host)
	}
	return net.LookupHost(host)
}

// client returns the HTTP client used by probes. The lab clusters use
// self-signed certificates and the console answers with a 302 to its own
// host, so redirects are surfaced to the caller instead of followed.
func (v *Verifier) client() *http.Client {
	if v != nil && v.httpClient != nil {
		return v.httpClient
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// resolveWithRetry resolves a name, retrying to tolerate DNS propagation.
func (v *Verifier) resolveWithRetry(host string, attempts int) ([]string, error) {
	var ips []string
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		ips, err = v.lookup(host)
		if err == nil && len(ips) > 0 {
			return ips, nil
		}
		if attempt < attempts {
			time.Sleep(time.Second)
		}
	}
	return ips, err
}

// expectedDNS returns the address each published name must resolve to: the
// cluster VIPs for platform: baremetal presets, the single node for SNO.
func (v *Verifier) expectedDNS() (apiIP, appsIP string, err error) {
	if v.Config.UsesVIPs() {
		return v.Config.Network.APIVIP, v.Config.Network.IngressLBVIP, nil
	}
	leases, err := infrastructure.ComputeNodeLeases(v.Config)
	if err != nil {
		return "", "", err
	}
	if len(leases) == 0 {
		return "", "", fmt.Errorf("no node leases computed")
	}
	return leases[0].IP, leases[0].IP, nil
}

// NewVerifier creates a new verification service.
func NewVerifier(cfg *config.Config, h *host.Host) *Verifier {
	return &Verifier{
		Config: cfg,
		Host:   h,
		Output: os.Stdout,
	}
}

func (v *Verifier) outputWriter() io.Writer {
	if v != nil && v.Output != nil {
		return v.Output
	}
	return os.Stdout
}

// VerifyAll runs the complete post-install verification suite.
func (v *Verifier) VerifyAll(ctx context.Context) error {
	fmt.Printf("\n[Verification] Starting Phase 4 E2E verification from Workstation...\n")

	apiFQDN := v.Config.APIHost()
	consoleFQDN := fmt.Sprintf("console-openshift-console.apps.%s", v.Config.ClusterFQDN())
	apiURL := fmt.Sprintf("https://%s:6443", apiFQDN)
	consoleURL := fmt.Sprintf("https://%s", consoleFQDN)

	// 1. Check DNS Resolution from Workstation across the tunnel (with retry to tolerate propagation)
	fmt.Printf("[Verification] Testing Workstation DNS resolution...\n")
	wantAPI, wantApps, err := v.expectedDNS()
	if err != nil {
		return fmt.Errorf("resolve expected DNS answers: %w", err)
	}

	apiIPs, err := v.resolveWithRetry(apiFQDN, 10)
	if err != nil || len(apiIPs) == 0 {
		return fmt.Errorf("DNS resolution failed for '%s'. Check Workstation split-DNS and sshuttle tunnel: %w", apiFQDN, err)
	}
	if !slices.Contains(apiIPs, wantAPI) {
		return fmt.Errorf("DNS for '%s' returned %v, want %s", apiFQDN, apiIPs, wantAPI)
	}
	fmt.Printf("✔ '%s' resolved to %s\n", apiFQDN, wantAPI)

	apiIntIPs, err := v.resolveWithRetry(v.Config.APIIntHost(), 10)
	if err != nil || !slices.Contains(apiIntIPs, wantAPI) {
		return fmt.Errorf("DNS for '%s' returned %v, want %s", v.Config.APIIntHost(), apiIntIPs, wantAPI)
	}
	fmt.Printf("✔ '%s' resolved to %s\n", v.Config.APIIntHost(), wantAPI)

	consoleIPs, err := v.resolveWithRetry(consoleFQDN, 10)
	if err != nil || !slices.Contains(consoleIPs, wantApps) {
		return fmt.Errorf("DNS for '%s' returned %v, want %s", consoleFQDN, consoleIPs, wantApps)
	}
	fmt.Printf("✔ '%s' resolved to %s\n", consoleFQDN, wantApps)

	// 2. Check API /healthz endpoint
	fmt.Printf("[Verification] Probing cluster API healthz (%s/healthz)...\n", apiURL)
	if err := v.probeEndpoint(apiURL+"/healthz", "ok"); err != nil {
		return fmt.Errorf("cluster API healthz check failed: %w", err)
	}
	fmt.Printf("✔ Cluster API responds 'ok'\n")

	// 3. Check Web Console endpoint
	fmt.Printf("[Verification] Probing OpenShift web console (%s)...\n", consoleURL)
	if err := v.probeConsole(consoleURL, consoleFQDN); err != nil {
		return fmt.Errorf("web console probe failed: %w", err)
	}
	fmt.Printf("✔ Web console responds with HTTP 200/302\n")

	// 4. Retrieve Kubeconfig & Kubeadmin credentials
	localKubeconfig, localPassword, err := v.syncCredentials()
	if err != nil {
		return fmt.Errorf("sync credentials: %w", err)
	}

	// 5. 'oc' authentication and node readiness
	expectedNodes := 1
	if topo, err := v.Config.EffectiveTopology(); err == nil {
		expectedNodes = topo.ControlPlaneReplicas + topo.WorkerReplicas
	}
	if _, err := exec.LookPath("oc"); err != nil {
		return fmt.Errorf("'oc' CLI is required for Phase 4 verification; install it or add it to PATH")
	}
	fmt.Printf("[Verification] Running 'oc get nodes' smoke test...\n")
	cmd := exec.Command("oc", "--kubeconfig", localKubeconfig, "get", "nodes")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("'oc get nodes' failed: %w\n%s", err, string(out))
	}
	fmt.Printf("\n%s\n", string(out))
	if err := verifyNodeReadiness(string(out), expectedNodes); err != nil {
		return fmt.Errorf("node readiness check failed: %w", err)
	}
	fmt.Printf("✔ 'oc' CLI verified successfully (%d nodes Ready).\n", expectedNodes)

	// 6. Print Access Instructions
	instrCfg := AccessInstructionsConfig{
		ClusterName:    v.Config.ClusterName,
		ConsoleURL:     consoleURL,
		KubeconfigPath: localKubeconfig,
		PasswordPath:   localPassword,
	}
	return v.PrintAccessInstructions(instrCfg)
}

func (v *Verifier) probeEndpoint(url, expectedSubstr string) error {
	resp, err := v.client().Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected HTTP 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), expectedSubstr) {
		return fmt.Errorf("expected body to contain '%s', got '%s'", expectedSubstr, string(body))
	}
	return nil
}

func (v *Verifier) probeConsole(rawURL, expectedHost string) error {
	resp, err := v.client().Get(rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if location == "" {
			return fmt.Errorf("console returned 302 without a Location header")
		}
		target, err := url.Parse(location)
		if err != nil {
			return fmt.Errorf("parse console redirect %q: %w", location, err)
		}
		if target.Host != "" && !strings.EqualFold(target.Host, expectedHost) {
			return fmt.Errorf("console redirects to unexpected host %q, want %q", target.Host, expectedHost)
		}
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200 or 302, got %d", resp.StatusCode)
	}
	return nil
}

// verifyNodeReadiness requires every reported node to be Ready and the total
// count to match the topology.
func verifyNodeReadiness(output string, expected int) error {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "NAME" {
			continue
		}
		count++
		if !strings.HasPrefix(fields[1], "Ready") {
			return fmt.Errorf("node %q is %q, want Ready", fields[0], fields[1])
		}
	}
	if count != expected {
		return fmt.Errorf("expected %d nodes, got %d", expected, count)
	}
	return nil
}

func (v *Verifier) syncCredentials() (string, string, error) {
	remoteDir := fmt.Sprintf("%s/auth", v.Config.InstallDir())

	// Fetch kubeadmin password
	password, err := v.Host.Client.Run(fmt.Sprintf("cat %s/kubeadmin-password", remoteDir))
	if err != nil {
		return "", "", fmt.Errorf("read kubeadmin password: %w", err)
	}
	cleanPassword := strings.TrimSpace(password)

	// Fetch kubeconfig content
	kubeconfigData, err := v.Host.Client.Run(fmt.Sprintf("cat %s/kubeconfig", remoteDir))
	if err != nil {
		return "", "", fmt.Errorf("read kubeconfig: %w", err)
	}

	// Save to ~/.kube on Workstation
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	kubeDir := filepath.Join(home, ".kube")

	localKubeconfig, localPassword, err := SaveLocalCredentials(kubeDir, v.Config.ClusterName, kubeconfigData, cleanPassword)
	if err != nil {
		return "", "", err
	}

	return localKubeconfig, localPassword, nil
}

// SaveLocalCredentials writes the kubeconfig and kubeadmin-password files to kubeDir with 0600 permissions.
func SaveLocalCredentials(kubeDir, clusterName, kubeconfigData, password string) (string, string, error) {
	if err := os.MkdirAll(kubeDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create kube dir '%s': %w", kubeDir, err)
	}

	cleanPassword := strings.TrimSpace(password)
	localKubeconfig := filepath.Join(kubeDir, fmt.Sprintf("config.%s", clusterName))
	if err := os.WriteFile(localKubeconfig, []byte(kubeconfigData), 0o600); err != nil {
		return "", "", fmt.Errorf("write local kubeconfig '%s': %w", localKubeconfig, err)
	}
	_ = os.Chmod(localKubeconfig, 0o600)

	localPassword := filepath.Join(kubeDir, fmt.Sprintf("kubeadmin-password.%s", clusterName))
	if err := os.WriteFile(localPassword, []byte(cleanPassword+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write local password '%s': %w", localPassword, err)
	}
	_ = os.Chmod(localPassword, 0o600)

	return localKubeconfig, localPassword, nil
}

// AccessInstructionsConfig holds configuration for formatting lab access instructions.
type AccessInstructionsConfig struct {
	ClusterName    string
	ConsoleURL     string
	KubeconfigPath string
	PasswordPath   string
}

// FormatAccessInstructions formats the post-verification success banner and access instructions.
func FormatAccessInstructions(cfg AccessInstructionsConfig) string {
	var b strings.Builder
	b.WriteString("\n================================================================================\n")
	b.WriteString("🎉 LAB VERIFICATION SUCCESSFUL!\n")
	b.WriteString("================================================================================\n")
	fmt.Fprintf(&b, "• Web Console:    %s\n", cfg.ConsoleURL)
	b.WriteString("• Username:       kubeadmin\n")
	fmt.Fprintf(&b, "• Password:       %s\n", cfg.PasswordPath)
	fmt.Fprintf(&b, "• Kubeconfig:     %s\n\n", cfg.KubeconfigPath)
	b.WriteString("To use 'oc' in your current terminal:\n")
	fmt.Fprintf(&b, "  export KUBECONFIG=%s\n", cfg.KubeconfigPath)
	b.WriteString("  oc get nodes\n")
	b.WriteString("================================================================================\n\n")
	return b.String()
}

// PrintAccessInstructions prints the post-verification access instructions to the configured writer.
func (v *Verifier) PrintAccessInstructions(cfg AccessInstructionsConfig) error {
	_, err := fmt.Fprint(v.outputWriter(), FormatAccessInstructions(cfg))
	return err
}
