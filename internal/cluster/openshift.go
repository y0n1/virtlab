package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/host"
	"github.com/y0n1/virtlab/internal/infrastructure"
)

// OpenShiftManager manages Phase 3: Agent-based OpenShift installation.
type OpenShiftManager struct {
	Config *config.Config
	Host   *host.Host

	// runCommand overrides Host.Client.Run in tests.
	runCommand func(string) (string, error)
}

func (m *OpenShiftManager) run(command string) (string, error) {
	if m.runCommand != nil {
		return m.runCommand(command)
	}
	return m.Host.Client.Run(command)
}

// NewOpenShiftManager creates an OpenShift cluster manager.
func NewOpenShiftManager(cfg *config.Config, h *host.Host) *OpenShiftManager {
	return &OpenShiftManager{
		Config: cfg,
		Host:   h,
	}
}

// InstallDir returns the cluster install directory path on the Host hypervisor.
func (m *OpenShiftManager) InstallDir() string {
	return m.Config.InstallDir()
}

// RenderInstallConfig generates install-config.yaml for the chosen preset.
func (m *OpenShiftManager) RenderInstallConfig() (string, error) {
	topo, err := m.Config.EffectiveTopology()
	if err != nil {
		return "", err
	}

	// VIPs are rendered only for presets that use platform: baremetal.
	if err := m.Config.ResolveVIPs(); err != nil {
		return "", fmt.Errorf("cluster: resolve VIPs: %w", err)
	}

	sshKey, err := m.Config.ReadSSHPublicKey()
	if err != nil {
		return "", fmt.Errorf("cluster: sshKey error: %w", err)
	}

	tmplText := `additionalTrustBundlePolicy: Proxyonly
apiVersion: v1
baseDomain: {{.BaseDomain}}
compute:
  - architecture: amd64
    hyperthreading: Enabled
    name: worker
    platform: {}
    replicas: {{.WorkerReplicas}}
controlPlane:
  architecture: amd64
  hyperthreading: Enabled
  name: master
  platform: {}
  replicas: {{.ControlPlaneReplicas}}
metadata:
  creationTimestamp: null
  name: {{.ClusterName}}
networking:
  clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
  machineNetwork:
    - cidr: {{.MachineNetwork}}
  networkType: OVNKubernetes
  serviceNetwork:
    - 172.30.0.0/16
platform:
{{- if .UsesVIPs }}
  baremetal:
    apiVIPs:
      - {{.APIVIP}}
    ingressVIPs:
      - {{.IngressVIP}}
    provisioningNetwork: Disabled
{{- else }}
  none: {}
{{- end }}
publish: External
sshKey: {{.SSHKey}}
`
	data := map[string]any{
		"BaseDomain":           m.Config.BaseDomain,
		"ClusterName":          m.Config.ClusterName,
		"WorkerReplicas":       topo.WorkerReplicas,
		"ControlPlaneReplicas": topo.ControlPlaneReplicas,
		"MachineNetwork":       m.Config.Network.CIDR,
		"SSHKey":               sshKey,
		"UsesVIPs":             m.Config.UsesVIPs(),
		"APIVIP":               m.Config.Network.APIVIP,
		"IngressVIP":           m.Config.Network.IngressVIP,
	}

	t, err := template.New("install-config").Parse(tmplText)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderAgentConfig generates agent-config.yaml for all cluster nodes.
func (m *OpenShiftManager) RenderAgentConfig() (string, error) {
	leases, err := infrastructure.ComputeNodeLeases(m.Config)
	if err != nil {
		return "", err
	}

	rendezvousIP := leases[0].IP

	tmplText := `apiVersion: v1alpha1
kind: AgentConfig
metadata:
  name: {{.ClusterName}}
rendezvousIP: {{.RendezvousIP}}
additionalNTPSources:
  - {{.GatewayIP}}
  - 0.rhel.pool.ntp.org
  - 1.rhel.pool.ntp.org
hosts:
{{- range .Nodes}}
  - hostname: {{.FQDN}}
    role: {{.Role}}
    interfaces:
      - name: enp1s0
        macAddress: {{.MAC}}
    networkConfig:
      interfaces:
        - name: enp1s0
          type: ethernet
          state: up
          mac-address: {{.MAC}}
          ipv4:
            enabled: true
            address:
              - ip: {{.IP}}
                prefix-length: 24
            dhcp: false
      dns-resolver:
        config:
          server:
            - {{$.GatewayIP}}
            - 8.8.8.8
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: {{$.GatewayIP}}
            next-hop-interface: enp1s0
            table-id: 254
    rootDeviceHints:
      deviceName: /dev/vda
{{- end}}
`

	type nodeEntry struct {
		FQDN string
		Role string
		MAC  string
		IP   string
	}

	var nodes []nodeEntry
	for _, l := range leases {
		nodes = append(nodes, nodeEntry{
			FQDN: fmt.Sprintf("%s.%s", l.Name, m.Config.ClusterFQDN()),
			Role: l.Role,
			MAC:  l.MAC,
			IP:   l.IP,
		})
	}

	data := map[string]any{
		"ClusterName":  m.Config.ClusterName,
		"RendezvousIP": rendezvousIP,
		"GatewayIP":    m.Config.Network.GatewayIP,
		"Nodes":        nodes,
	}

	t, err := template.New("agent-config").Parse(tmplText)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Install coordinates the complete unattended OpenShift Agent-based installation.
func (m *OpenShiftManager) Install(ctx context.Context) error {
	dir := m.InstallDir()
	fmt.Printf("\n[OpenShift] Preparing cluster installation in '%s' on Host...\n", dir)

	// 1. Check resume guard (is cluster already healthy?)
	cfg := m.Config

	resumeCheck := fmt.Sprintf(`
if [ -f "%s/auth/kubeconfig" ] && curl -sk -m 3 "https://%s:6443/healthz" 2>/dev/null | grep -qx ok; then
  echo "ALREADY_INSTALLED"
fi
`, dir, cfg.APIHost())

	out, _ := m.Host.Client.Run(resumeCheck)
	if strings.Contains(out, "ALREADY_INSTALLED") {
		fmt.Printf("✔ Cluster is already installed and healthy (/healthz ok). Skipping re-installation.\n")
		return nil
	}

	// Compute static node leases (needed for VM boot/eject steps below)
	leases, err := infrastructure.ComputeNodeLeases(m.Config)
	if err != nil {
		return err
	}

	// 2. Render manifests
	ic, err := m.RenderInstallConfig()
	if err != nil {
		return fmt.Errorf("render install-config: %w", err)
	}
	ac, err := m.RenderAgentConfig()
	if err != nil {
		return fmt.Errorf("render agent-config: %w", err)
	}

	// 3. Write manifests and inject pull-secret host-side
	fmt.Printf("[OpenShift] Writing install-config.yaml and agent-config.yaml...\n")
	setupScript := m.BuildSetupScript(dir, ic, ac)

	if _, err := m.Host.Client.RunScript(setupScript); err != nil {
		return fmt.Errorf("manifest setup failed: %w", err)
	}

	// 4. Generate Agent ISO
	fmt.Printf("[OpenShift] Building bootable Agent ISO (openshift-install agent create image)...\n")
	buildIsoScript := m.BuildCreateIsoScript(dir)

	if _, err := m.Host.Client.RunScript(buildIsoScript); err != nil {
		return fmt.Errorf("agent ISO creation failed: %w", err)
	}
	fmt.Printf("✔ Agent ISO built successfully.\n")

	// 5. Attach ISO to nodes and start VMs
	fmt.Printf("[OpenShift] Attaching Agent ISO and powering on cluster nodes...\n")
	for _, l := range leases {
		domName := fmt.Sprintf("%s-%s", m.Config.ClusterName, l.Name)
		bootScript := fmt.Sprintf(`
DIR='%s'
ISO="$DIR/agent.x86_64.iso"
DOM='%s'
virsh change-media "$DOM" sda --insert --source "$ISO" --config || true
virsh start "$DOM" || true
`, dir, domName)
		if _, err := m.Host.Client.RunScript(bootScript); err != nil {
			return fmt.Errorf("start node '%s': %w", domName, err)
		}
	}

	// 6. Monitor install completion
	fmt.Printf("[OpenShift] Monitoring installation progress (this takes ~25-45 minutes)...\n")
	fmt.Printf("  • Streaming installer log from %s/.openshift_install.log\n", dir)
	fmt.Printf("  • (Serial console logs remain available on host at %s/*-serial.log)\n", m.Config.LogsDir())

	waitScript := m.BuildWaitScript(dir)

	waitErr := m.Host.Client.RunScriptStream(ctx, waitScript, os.Stdout, os.Stderr)

	// 7. Eject ISO after install (both live and persistent config)
	for _, l := range leases {
		domName := fmt.Sprintf("%s-%s", m.Config.ClusterName, l.Name)
		_, _ = m.Host.Client.Run(m.BuildMediaEjectCommand(domName))
	}

	// 8. Remove the bootstrap ISO and restrict installer directory permissions
	cleanupScript := m.BuildIsoCleanupScript(dir)
	if _, err := m.Host.Client.RunScript(cleanupScript); err != nil {
		fmt.Printf("⚠ Warning: failed to clean up ISO or reseal directory on host: %v\n", err)
	}

	if waitErr != nil {
		return fmt.Errorf("openshift install failed or timed out: %w", waitErr)
	}

	fmt.Printf("✔ OpenShift cluster installation completed successfully!\n")
	return nil
}

// BuildIngressReplicasScript returns the host script that reconciles the
// default IngressController to one router per ingress node.
func BuildIngressReplicasScript(dir string, replicas int) string {
	return fmt.Sprintf(`set -e
DIR='%s'
export KUBECONFIG="$DIR/auth/kubeconfig"
oc -n openshift-ingress-operator patch ingresscontroller default --type=merge -p '{"spec":{"replicas":%d}}'
`, dir, replicas)
}

// EnsureIngressReplicas makes every ingress node run a router so HAProxy can
// spread traffic across the whole ingress set. Skip SNO, which has no LB.
func (m *OpenShiftManager) EnsureIngressReplicas(ctx context.Context) error {
	if !m.Config.UsesVIPs() {
		return nil
	}

	topo, err := m.Config.EffectiveTopology()
	if err != nil {
		return err
	}
	leases, err := infrastructure.ComputeNodeLeases(m.Config)
	if err != nil {
		return err
	}
	replicas := len(infrastructure.IngressLeases(topo, leases))
	if replicas == 0 {
		return nil
	}

	script := BuildIngressReplicasScript(m.InstallDir(), replicas)
	if _, err := m.Host.Client.RunScriptContext(ctx, script); err != nil {
		return fmt.Errorf("reconcile ingress controller replicas: %w", err)
	}
	fmt.Printf("✔ Ingress controller reconciled to %d router replicas\n", replicas)
	return nil
}

// BuildSetupScript constructs the remote bash script that prepares the installer directory,
// generates install-config.yaml and agent-config.yaml with secure 0600 permissions,
// and injects the pull secret.
func (m *OpenShiftManager) BuildSetupScript(dir, ic, ac string) string {
	return fmt.Sprintf(`
set -euo pipefail
umask 0077
DIR='%s'
mkdir -p -m 0700 "$DIR"
chmod 0700 "$DIR"
find "$DIR" -mindepth 1 -delete

cat <<'EOF' > "$DIR/install-config.yaml"
%s
EOF

cat <<'EOF' > "$DIR/agent-config.yaml"
%s
EOF

chmod 0600 "$DIR/install-config.yaml" "$DIR/agent-config.yaml"

python3 - /root/pull-secret.json "$DIR/install-config.yaml" <<'PYEOF'
import json, sys, os
raw = open(sys.argv[1]).read()
ps = json.dumps(json.dumps(json.loads(raw)))
p = sys.argv[2]
lines = [l for l in open(p).read().splitlines() if not l.startswith("pullSecret:")]
lines.append("pullSecret: " + ps)
with open(p, "w") as f:
    f.write("\n".join(lines) + "\n")
os.chmod(p, 0o600)
PYEOF
`, dir, ic, ac)
}

// BuildCreateIsoScript constructs the remote bash script that invokes openshift-install
// to build the agent ISO and scopes file and directory permissions safely for QEMU access.
func (m *OpenShiftManager) BuildCreateIsoScript(dir string) string {
	return fmt.Sprintf(`
set -euo pipefail
umask 0077
DIR='%s'
openshift-install --dir "$DIR" agent create image --log-level=info
chgrp qemu "$DIR" 2>/dev/null || chgrp kvm "$DIR" 2>/dev/null || true
chmod 0750 "$DIR"
chgrp qemu "$DIR/agent.x86_64.iso" 2>/dev/null || chgrp kvm "$DIR/agent.x86_64.iso" 2>/dev/null || true
chmod 0640 "$DIR/agent.x86_64.iso"
stat -c %%s "$DIR/agent.x86_64.iso"
`, dir)
}

// BuildIsoCleanupScript constructs the remote bash script to remove the bootable agent ISO,
// reseal the installer directory to 0700 (owner only), and protect auth credentials.
func (m *OpenShiftManager) BuildIsoCleanupScript(dir string) string {
	return fmt.Sprintf(`
set -euo pipefail
DIR='%s'
rm -f "$DIR/agent.x86_64.iso"
chown root:root "$DIR" 2>/dev/null || true
chmod 0700 "$DIR"
if [ -d "$DIR/auth" ]; then
    chmod 0700 "$DIR/auth"
    chmod 0600 "$DIR/auth"/* 2>/dev/null || true
fi
`, dir)
}

// BuildMediaEjectCommand constructs the virsh change-media command to disconnect the installer ISO
// from both the live active domain and the persistent domain configuration.
func (m *OpenShiftManager) BuildMediaEjectCommand(domName string) string {
	return fmt.Sprintf("virsh change-media '%s' sda --eject --live --config || true", domName)
}

// BuildWaitScript constructs the remote bash script that launches openshift-install
// in the background, streams .openshift_install.log from the offset via tail,
// traps cleanup signals, and propagates the installer's exit code.
func (m *OpenShiftManager) BuildWaitScript(dir string) string {
	return fmt.Sprintf(`
set -euo pipefail
umask 0077
DIR='%s'
LOG="$DIR/.openshift_install.log"
touch "$LOG"

# Calculate current line offset so previous stage logs (e.g. image creation) are not replayed
LINES=$(wc -l < "$LOG" 2>/dev/null || echo 0)
START_LINE=$((LINES + 1))

# Start installer in background
openshift-install --dir "$DIR" agent wait-for install-complete --log-level=info &
INSTALL_PID=$!

# Tail installer log starting from current offset in background
tail -n +"$START_LINE" -f "$LOG" &
TAIL_PID=$!

# Trap signals and termination to ensure background processes are cleanly cleaned up
trap 'kill $INSTALL_PID $TAIL_PID 2>/dev/null || true' EXIT HUP INT TERM

STATUS=0
wait $INSTALL_PID || STATUS=$?

kill $TAIL_PID 2>/dev/null || true
wait $TAIL_PID 2>/dev/null || true

exit $STATUS
`, dir)
}
