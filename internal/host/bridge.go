package host

import (
	"fmt"
	"strings"
)

// ResolveBridge inspects the remote hypervisor to determine the bridge name
// following the 'virbr-labN' pattern, where N is the lowest unused counter (starting at 0).
// If the cluster's network already exists on the host, its assigned bridge is reused.
func (h *Host) ResolveBridge(networkName string) (string, error) {
	return h.Client.ResolveBridge(networkName)
}

const bridgeProbeScript = `
set -e
NET_NAME="$1"

# 1. Existing bridge for this specific network (if defined)
existing_bridge=$(virsh net-dumpxml "$NET_NAME" 2>/dev/null | grep -o "bridge name=['\"][^'\"]*" | head -1 | cut -d"'" -f2 | cut -d'"' -f2 || true)
echo "EXISTING:${existing_bridge}"

# 2. All network interfaces currently in /sys/class/net
echo "INTERFACES:"
ls -1 /sys/class/net 2>/dev/null || true

# 3. All bridges declared in any libvirt networks
echo "LIBVIRT_BRIDGES:"
for net in $(virsh net-list --all --name 2>/dev/null); do
    virsh net-dumpxml "$net" 2>/dev/null | grep -o "bridge name=['\"][^'\"]*" | cut -d"'" -f2 | cut -d'"' -f2 || true
done
`

// ResolveBridge executes a remote probe to allocate or reuse a bridge device.
func (c *SSHClient) ResolveBridge(networkName string) (string, error) {
	out, err := c.RunScript(bridgeProbeScript, networkName)
	if err != nil {
		return "", fmt.Errorf("probe host bridges: %w", err)
	}

	return parseBridgeProbeOutput(out)
}

// parseBridgeProbeOutput parses probe output and calculates the bridge name.
func parseBridgeProbeOutput(out string) (string, error) {
	lines := strings.Split(out, "\n")
	var existingBridge string
	usedBridges := make(map[string]bool)
	section := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "EXISTING:") {
			existingBridge = strings.TrimPrefix(line, "EXISTING:")
			continue
		}
		if line == "INTERFACES:" {
			section = "interfaces"
			continue
		}
		if line == "LIBVIRT_BRIDGES:" {
			section = "libvirt"
			continue
		}

		if section == "interfaces" || section == "libvirt" {
			usedBridges[line] = true
		}
	}

	if existingBridge != "" {
		return existingBridge, nil
	}

	// Libvirt default counter semantics: lowest non-negative integer N >= 0
	for n := 0; n < 10000; n++ {
		candidate := fmt.Sprintf("virbr-lab%d", n)
		if !usedBridges[candidate] {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("exhausted bridge counter space for virbr-labN (0-9999)")
}
