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

	prefixLength, err := config.PrefixLength(m.Config.Network.CIDR)
	if err != nil {
		return "", fmt.Errorf("resolve network prefix length: %w", err)
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
                prefix-length: {{$.PrefixLength}}
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
		"PrefixLength": prefixLength,
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

	// 1. Check resume guard. A healthy cluster is adopted only when its applied
	// declaration still matches config.yaml. An unhealthy or unknown probe
	// aborts the run so that a transient failure cannot destroy credentials.
	alreadyInstalled, err := m.checkResume()
	if err != nil {
		return err
	}
	if alreadyInstalled {
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

	// 9. Persist the applied declaration so the next `up` can detect drift.
	// A failure here must not turn a successful install into a failed run.
	if err := m.persistInstallState(); err != nil {
		fmt.Printf("⚠ Warning: cluster is installed but the applied state could not be recorded: %v\n", err)
	}

	fmt.Printf("✔ OpenShift cluster installation completed successfully!\n")
	return nil
}

// Probe markers. They are emitted on their own lines so the parser can find
// them even when SSH adds a banner or a shell warning to stdout.
const (
	probeStatusPrefix = "VIRTLAB_STATUS="
	probeStateBegin   = "VIRTLAB_STATE_BEGIN"
	probeStateEnd     = "VIRTLAB_STATE_END"

	probeStatusNotInstalled = "NOT_INSTALLED"
	probeStatusHealthy      = "HEALTHY"
	probeStatusUnhealthy    = "UNHEALTHY"
	probeStatusUnknown      = "UNKNOWN"
)

// installProbeScript reports whether a cluster is installed, whether its API
// is healthy, and the persisted install state. It never mutates anything.
const installProbeScript = `set -u
DIR='%s'
STATE="$DIR/%s"
if [ ! -f "$DIR/auth/kubeconfig" ]; then
  echo "VIRTLAB_STATUS=NOT_INSTALLED"
  exit 0
fi
if curl -sk -m 3 "https://%s:6443/healthz" 2>/dev/null | grep -qx ok; then
  echo "VIRTLAB_STATUS=HEALTHY"
else
  echo "VIRTLAB_STATUS=UNHEALTHY"
fi
if [ -f "$STATE" ]; then
  echo "VIRTLAB_STATE_BEGIN"
  cat "$STATE"
  echo "VIRTLAB_STATE_END"
fi
`

// resumeProbe is the parsed result of installProbeScript.
type resumeProbe struct {
	Status string
	State  *InstallState
}

// checkResume decides whether Phase 3 can skip installation. It returns true
// only for a healthy cluster whose applied declaration still matches
// config.yaml. Any ambiguous or unsafe state returns an error, so Install
// never falls through to a destructive reinstall.
func (m *OpenShiftManager) checkResume() (bool, error) {
	probe, err := m.probeResume()
	if err != nil {
		return false, fmt.Errorf(
			"cluster: cannot determine whether a cluster is already installed at %s; refusing to modify it: %w",
			m.InstallDir(), err)
	}

	switch probe.Status {
	case probeStatusNotInstalled:
		return false, nil

	case probeStatusHealthy:
		if err := m.verifyAppliedState(probe.State); err != nil {
			return false, err
		}
		return true, nil

	case probeStatusUnhealthy:
		return false, fmt.Errorf(
			"cluster: cluster assets exist at %s but the API did not answer https://%s:6443/healthz; the install state is unknown, so virtlab will not reinstall over it. Inspect it with 'virtlab status', then either recover the cluster or rebuild it with 'virtlab down -y' and 'virtlab up'",
			m.InstallDir(), m.Config.APIHost())

	default:
		return false, fmt.Errorf(
			"cluster: could not determine the install state at %s; refusing to modify it. Check Host connectivity and retry, or rebuild with 'virtlab down -y' and 'virtlab up'",
			m.InstallDir())
	}
}

// EnsureDeclarationCompatible fails in preflight when a healthy cluster was
// installed from a declaration that config.yaml no longer matches. It runs
// after the subnet is resolved so auto-allocated IPs are compared correctly.
//
// It is deliberately permissive when the probe cannot determine the state: a
// flaky probe must not block recovery. Install re-checks and aborts safely
// before touching the install tree.
func (m *OpenShiftManager) EnsureDeclarationCompatible() error {
	if m.Host == nil && m.runCommand == nil {
		return nil
	}

	probe, err := m.probeResume()
	if err != nil {
		fmt.Printf("⚠ Warning: could not verify the installed cluster declaration: %v\n", err)
		return nil
	}
	if probe.Status != probeStatusHealthy || probe.State == nil {
		// Phase 3 reports the legacy missing-state warning and adopts the
		// cluster; there is nothing to compare here.
		return nil
	}
	return m.verifyAppliedState(probe.State)
}

// probeResume runs installProbeScript on the Host and parses the result.
func (m *OpenShiftManager) probeResume() (*resumeProbe, error) {
	if m.runCommand == nil && m.Host == nil {
		return nil, fmt.Errorf("no Host client configured")
	}
	script := fmt.Sprintf(installProbeScript, m.InstallDir(), installStateFile, m.Config.APIHost())
	out, err := m.run(script)
	if err != nil {
		return nil, err
	}
	return parseResumeProbe(out)
}

// parseResumeProbe extracts the status and optional state block from probe
// output. It returns an error when a state block is present but unparseable,
// because silently ignoring a corrupt record would defeat the guard.
func parseResumeProbe(out string) (*resumeProbe, error) {
	probe := &resumeProbe{Status: probeStatusUnknown}
	var state strings.Builder
	inState := false

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(line, probeStatusPrefix):
			probe.Status = strings.TrimSpace(strings.TrimPrefix(line, probeStatusPrefix))
		case line == probeStateBegin:
			inState = true
		case line == probeStateEnd:
			inState = false
		default:
			if inState {
				state.WriteString(line)
				state.WriteString("\n")
			}
		}
	}

	if state.Len() > 0 {
		parsed, err := ParseInstallState(state.String())
		if err != nil {
			return nil, err
		}
		probe.State = parsed
	}
	return probe, nil
}

// verifyAppliedState fails when the healthy cluster was installed from a
// declaration that config.yaml no longer matches.
func (m *OpenShiftManager) verifyAppliedState(installed *InstallState) error {
	if installed == nil {
		fmt.Printf("⚠ Warning: cluster is healthy but %s is missing (installed by an older virtlab); cannot verify that config.yaml matches it.\n", installStateFile)
		return nil
	}

	desired, err := m.DesiredInstallState()
	if err != nil {
		return fmt.Errorf("cluster: compute desired install state: %w", err)
	}

	diffs := installed.Diff(desired)
	if len(diffs) == 0 {
		return nil
	}

	return fmt.Errorf(
		"cluster '%s' is installed but config.yaml no longer matches its applied declaration:\n  - %s\nThe cluster was left untouched. These changes cannot be applied in place; run 'virtlab down -y' and then 'virtlab up' to rebuild it",
		m.Config.ClusterName, strings.Join(diffs, "\n  - "))
}

// persistInstallState writes the applied declaration after a successful
// install. The write is atomic and restricted to the owner.
func (m *OpenShiftManager) persistInstallState() error {
	desired, err := m.DesiredInstallState()
	if err != nil {
		return err
	}
	body, err := RenderInstallState(desired)
	if err != nil {
		return err
	}
	script := BuildPersistStateScript(m.InstallDir(), body)
	if _, err := m.Host.Client.RunScript(script); err != nil {
		return fmt.Errorf("write %s: %w", installStateFile, err)
	}
	return nil
}

// BuildPersistStateScript constructs the remote script that atomically writes
// the applied install state with owner-only permissions.
func BuildPersistStateScript(dir, body string) string {
	return fmt.Sprintf(`
set -euo pipefail
umask 0077
DIR='%s'
STATE="$DIR/%s"
TMP="$STATE.tmp"
mkdir -p -m 0700 "$DIR"
cat <<'EOF' > "$TMP"
%s
EOF
chmod 0600 "$TMP"
mv -f "$TMP" "$STATE"
`, dir, installStateFile, strings.TrimRight(body, "\n"))
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
// and injects the pull secret. It never deletes the directory wholesale: credentials
// are backed up and the manifests are replaced only when their content changes.
func (m *OpenShiftManager) BuildSetupScript(dir, ic, ac string) string {
	return fmt.Sprintf(`
set -euo pipefail
umask 0077
DIR='%s'
mkdir -p -m 0700 "$DIR"
chmod 0700 "$DIR"

# Preserve credentials from any earlier attempt before touching anything.
if [ -d "$DIR/auth" ] && [ ! -e "$DIR/auth.backup" ]; then
  cp -a "$DIR/auth" "$DIR/auth.backup"
  chmod 0700 "$DIR/auth.backup"
  chmod 0600 "$DIR/auth.backup"/* 2>/dev/null || true
fi

CHANGED=0

# install-config.yaml: render to a temp file, inject the pull secret, and
# replace the live manifest only when the content differs.
IC_TMP="$DIR/.install-config.yaml.new"
cat <<'EOF' > "$IC_TMP"
%s
EOF
python3 - /root/pull-secret.json "$IC_TMP" <<'PYEOF'
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
if [ -f "$DIR/install-config.yaml" ] && cmp -s "$IC_TMP" "$DIR/install-config.yaml"; then
  rm -f "$IC_TMP"
else
  mv -f "$IC_TMP" "$DIR/install-config.yaml"
  CHANGED=1
fi

# agent-config.yaml
AC_TMP="$DIR/.agent-config.yaml.new"
cat <<'EOF' > "$AC_TMP"
%s
EOF
if [ -f "$DIR/agent-config.yaml" ] && cmp -s "$AC_TMP" "$DIR/agent-config.yaml"; then
  rm -f "$AC_TMP"
else
  mv -f "$AC_TMP" "$DIR/agent-config.yaml"
  CHANGED=1
fi

chmod 0600 "$DIR/install-config.yaml" "$DIR/agent-config.yaml"

# A changed manifest invalidates the previous image build. Remove only the
# installer's own record; auth/ and the Agent ISO are preserved deliberately.
if [ "$CHANGED" -eq 1 ]; then
  rm -f "$DIR/.openshift_install_state.json"
fi
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
