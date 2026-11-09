package host

import (
	"fmt"
	"strconv"
	"strings"
)

// ExistingClusterResources describes resources already allocated on the host for a given cluster.
type ExistingClusterResources struct {
	Exists           bool
	AllocatedRAMMiB  int
	AllocatedDiskGiB int
}

// DetectConflictingLabs inspects the remote hypervisor for any active or defined
// lab networks, storage pools, or /home/libvirt directories belonging to a DIFFERENT cluster.
func (h *Host) DetectConflictingLabs(targetClusterName string) ([]string, error) {
	return h.Client.DetectConflictingLabs(targetClusterName)
}

// DetectConflictingLabs runs the remote probe to find other lab clusters.
func (c *SSHClient) DetectConflictingLabs(targetClusterName string) ([]string, error) {
	script := `
set -e
# 1. Networks
virsh net-list --all --name 2>/dev/null || true

echo "---POOLS---"
# 2. Storage Pools
virsh pool-list --all --name 2>/dev/null || true

echo "---DIRS---"
# 3. Lab Dirs in /home/libvirt (only directories containing lab assets)
if [ -d /home/libvirt ]; then
    for dir in /home/libvirt/*; do
        if [ -d "$dir/pool" ] || [ -d "$dir/installer" ]; then
            basename "$dir"
        fi
    done 2>/dev/null || true
fi
`
	out, err := c.RunScript(script)
	if err != nil {
		return nil, fmt.Errorf("probe host labs: %w", err)
	}

	return parseConflictingLabs(out, targetClusterName), nil
}

// parseConflictingLabs parses the remote output and filters out system/default entries.
func parseConflictingLabs(output, targetClusterName string) []string {
	lines := strings.Split(output, "\n")
	seen := make(map[string]bool)
	var conflicts []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "---POOLS---" || line == "---DIRS---" {
			continue
		}

		cluster := strings.TrimSuffix(line, "-net")
		if isIgnoredLabResource(cluster) {
			continue
		}

		if cluster != targetClusterName && !seen[cluster] {
			seen[cluster] = true
			conflicts = append(conflicts, cluster)
		}
	}
	return conflicts
}

func isIgnoredLabResource(name string) bool {
	if name == "" || name == "default" || name == "lost+found" || name == "images" || strings.HasPrefix(name, ".") {
		return true
	}
	return false
}

// InspectExistingCluster checks whether targetClusterName is already defined on the host,
// and returns its currently allocated RAM (in MiB) and Disk (in GiB).
func (h *Host) InspectExistingCluster(clusterName string) (ExistingClusterResources, error) {
	return h.Client.InspectExistingCluster(clusterName)
}

const inspectClusterScript = `
set -e
DOMAIN_PATTERN="$1"
CLUSTER_NAME="$2"

# Check if at least one VM domain exists for this cluster
doms=$(virsh list --all --name 2>/dev/null | grep -E "$DOMAIN_PATTERN" || true)
if [ -z "$doms" ]; then
    echo "EXISTS:false"
    exit 0
fi

echo "EXISTS:true"

# Sum allocated memory of this cluster's domains
total_ram=0
for dom in $doms; do
    mem=$(virsh dominfo "$dom" 2>/dev/null | awk '/Max memory:/ {print int($3/1024)}' || echo 0)
    total_ram=$((total_ram + mem))
done
echo "ALLOCATED_RAM:${total_ram}"

# Sum size of storage in /home/libvirt/${CLUSTER_NAME}
total_disk=0
if [ -d "/home/libvirt/${CLUSTER_NAME}" ]; then
    total_disk=$(du -BG -s "/home/libvirt/${CLUSTER_NAME}" 2>/dev/null | awk '{gsub(/G/,""); print $1}' || echo 0)
fi
echo "ALLOCATED_DISK:${total_disk}"
`

// InspectExistingCluster queries the host for running or defined VMs and disk space for this cluster.
func (c *SSHClient) InspectExistingCluster(clusterName string) (ExistingClusterResources, error) {
	domainPattern := ClusterDomainPattern(clusterName)
	out, err := c.RunScript(inspectClusterScript, domainPattern, clusterName)
	if err != nil {
		return ExistingClusterResources{}, fmt.Errorf("inspect existing cluster '%s': %w", clusterName, err)
	}

	return parseExistingClusterOutput(out), nil
}

func parseExistingClusterOutput(output string) ExistingClusterResources {
	var res ExistingClusterResources
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "EXISTS:true" {
			res.Exists = true
		} else if strings.HasPrefix(line, "ALLOCATED_RAM:") {
			val, _ := strconv.Atoi(strings.TrimPrefix(line, "ALLOCATED_RAM:"))
			res.AllocatedRAMMiB = val
		} else if strings.HasPrefix(line, "ALLOCATED_DISK:") {
			val, _ := strconv.Atoi(strings.TrimPrefix(line, "ALLOCATED_DISK:"))
			res.AllocatedDiskGiB = val
		}
	}
	return res
}
