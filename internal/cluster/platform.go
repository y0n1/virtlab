package cluster

import (
	"fmt"
	"strings"
)

// ExpectedPlatform returns the platform the current preset installs with.
// SNO keeps platform: none; compact and HA use platform: baremetal so the
// cluster manages API and ingress VIPs.
func (m *OpenShiftManager) ExpectedPlatform() string {
	if m.Config.UsesVIPs() {
		return "BareMetal"
	}
	return "None"
}

// platformProbeScript reads the live cluster platform from the cluster itself.
// Install assets are wiped at the start of Phase 3, so the running cluster is
// the only reliable record of how it was installed.
const platformProbeScript = `set -e
DIR='%s'
if [ ! -f "$DIR/auth/kubeconfig" ]; then
  echo "NO_CLUSTER"
  exit 0
fi
export KUBECONFIG="$DIR/auth/kubeconfig"
oc get infrastructure cluster -o jsonpath='{.status.platform}' 2>/dev/null || echo "UNKNOWN"
`

// parseClusterPlatform extracts the platform value from the probe output.
// It returns ok=false when there is no cluster or the platform is unknown.
func parseClusterPlatform(out string) (string, bool) {
	value := strings.TrimSpace(out)
	if value == "" || strings.Contains(value, "NO_CLUSTER") || strings.Contains(value, "UNKNOWN") {
		return "", false
	}
	lines := strings.Split(value, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return "", false
	}
	return last, true
}

// EnsurePlatformCompatible fails when an existing cluster was installed with a
// platform that the selected preset cannot change in place.
func (m *OpenShiftManager) EnsurePlatformCompatible() error {
	if m.Host == nil && m.Runner == nil && m.runCommand == nil {
		return nil
	}

	out, err := m.run(fmt.Sprintf(platformProbeScript, m.InstallDir()))
	if err != nil {
		fmt.Printf("⚠ Warning: could not determine the existing cluster platform: %v\n", err)
		return nil
	}

	current, ok := parseClusterPlatform(out)
	if !ok {
		return nil
	}

	expected := m.ExpectedPlatform()
	if !strings.EqualFold(current, expected) {
		return fmt.Errorf(
			"existing cluster '%s' uses platform %q, but preset '%s' requires %q; platform is immutable after install — run 'virtlab down' and then 'virtlab up'",
			m.Config.ClusterName, current, m.Config.Preset, expected)
	}
	return nil
}
