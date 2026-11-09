package config

import (
	"fmt"
	"strings"
)

// Supported cluster topology presets.
const (
	PresetSNO     = "sno"
	PresetCompact = "compact"
	PresetHA      = "ha"
)

// Official Red Hat OpenShift bare-metal minimum baseline specifications.
// Any resource allocation below these generates warnings during validation.
var (
	MinSNOControlPlane = NodeSettings{
		VCPUs:     8,
		MemoryMiB: 16384, // 16 GiB
		DiskGiB:   120,   // 120 GiB
	}

	MinMultiNodeControlPlane = NodeSettings{
		VCPUs:     4,
		MemoryMiB: 16384, // 16 GiB
		DiskGiB:   100,   // 100 GiB
	}

	MinWorkerNode = NodeSettings{
		VCPUs:     2,
		MemoryMiB: 8192, // 8 GiB
		DiskGiB:   100,  // 100 GiB
	}
)

// NodeTopology describes the complete hardware and replica allocation for a cluster.
type NodeTopology struct {
	Preset               string
	ControlPlaneReplicas int
	ControlPlaneSettings NodeSettings
	WorkerReplicas       int
	WorkerSettings       NodeSettings
}

// TotalNodes returns the aggregate count of cluster nodes.
func (t *NodeTopology) TotalNodes() int {
	return t.ControlPlaneReplicas + t.WorkerReplicas
}

// TotalVCPUs returns the total vCPUs requested across all cluster nodes.
func (t *NodeTopology) TotalVCPUs() int {
	return (t.ControlPlaneReplicas * t.ControlPlaneSettings.VCPUs) +
		(t.WorkerReplicas * t.WorkerSettings.VCPUs)
}

// TotalMemoryMiB returns the total RAM in MiB requested across all cluster nodes.
func (t *NodeTopology) TotalMemoryMiB() int {
	return (t.ControlPlaneReplicas * t.ControlPlaneSettings.MemoryMiB) +
		(t.WorkerReplicas * t.WorkerSettings.MemoryMiB)
}

// TotalMemoryGiB returns the total RAM in GiB requested across all cluster nodes.
func (t *NodeTopology) TotalMemoryGiB() float64 {
	return float64(t.TotalMemoryMiB()) / 1024.0
}

// TotalDiskGiB returns the total storage in GiB requested across all cluster nodes.
func (t *NodeTopology) TotalDiskGiB() int {
	return (t.ControlPlaneReplicas * t.ControlPlaneSettings.DiskGiB) +
		(t.WorkerReplicas * t.WorkerSettings.DiskGiB)
}

// EffectiveTopology computes the cluster node layout, applying presets and any explicit overrides.
func (c *Config) EffectiveTopology() (*NodeTopology, error) {
	preset := strings.ToLower(strings.TrimSpace(c.Preset))
	if preset == "" {
		preset = PresetSNO
	}

	topo := &NodeTopology{Preset: preset}

	switch preset {
	case PresetSNO:
		topo.ControlPlaneReplicas = 1
		topo.ControlPlaneSettings = MinSNOControlPlane
		topo.WorkerReplicas = 0
		topo.WorkerSettings = MinWorkerNode

	case PresetCompact:
		topo.ControlPlaneReplicas = 3
		topo.ControlPlaneSettings = MinMultiNodeControlPlane
		topo.WorkerReplicas = 0
		topo.WorkerSettings = MinWorkerNode

	case PresetHA:
		topo.ControlPlaneReplicas = 3
		topo.ControlPlaneSettings = MinMultiNodeControlPlane
		topo.WorkerReplicas = 2
		topo.WorkerSettings = MinWorkerNode

	default:
		return nil, fmt.Errorf("config: unknown preset '%s'; supported presets are 'sno', 'compact', 'ha'", c.Preset)
	}

	// Apply optional user overrides
	if c.ControlPlaneOverrides != nil {
		if c.ControlPlaneOverrides.VCPUs > 0 {
			topo.ControlPlaneSettings.VCPUs = c.ControlPlaneOverrides.VCPUs
		}
		if c.ControlPlaneOverrides.MemoryMiB > 0 {
			topo.ControlPlaneSettings.MemoryMiB = c.ControlPlaneOverrides.MemoryMiB
		}
		if c.ControlPlaneOverrides.DiskGiB > 0 {
			topo.ControlPlaneSettings.DiskGiB = c.ControlPlaneOverrides.DiskGiB
		}
	}

	if c.WorkerOverrides != nil && topo.WorkerReplicas > 0 {
		if c.WorkerOverrides.VCPUs > 0 {
			topo.WorkerSettings.VCPUs = c.WorkerOverrides.VCPUs
		}
		if c.WorkerOverrides.MemoryMiB > 0 {
			topo.WorkerSettings.MemoryMiB = c.WorkerOverrides.MemoryMiB
		}
		if c.WorkerOverrides.DiskGiB > 0 {
			topo.WorkerSettings.DiskGiB = c.WorkerOverrides.DiskGiB
		}
	}

	return topo, nil
}

// FormatSummaryTable renders an aligned human-readable summary table for CLI output.
func (t *NodeTopology) FormatSummaryTable() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cluster Topology Preset: %s (%d total nodes)\n", strings.ToUpper(t.Preset), t.TotalNodes())
	b.WriteString("--------------------------------------------------------------------------------\n")
	fmt.Fprintf(&b, "%-16s | %-8s | %-12s | %-14s | %-10s\n", "Role", "Count", "vCPUs/node", "RAM/node", "Disk/node")
	b.WriteString("--------------------------------------------------------------------------------\n")

	fmt.Fprintf(&b, "%-16s | %-8d | %-12d | %-6d MiB (%-2dG) | %-6d GiB\n",
		"Control Plane",
		t.ControlPlaneReplicas,
		t.ControlPlaneSettings.VCPUs,
		t.ControlPlaneSettings.MemoryMiB,
		t.ControlPlaneSettings.MemoryMiB/1024,
		t.ControlPlaneSettings.DiskGiB,
	)

	if t.WorkerReplicas > 0 {
		fmt.Fprintf(&b, "%-16s | %-8d | %-12d | %-6d MiB (%-2dG) | %-6d GiB\n",
			"Worker",
			t.WorkerReplicas,
			t.WorkerSettings.VCPUs,
			t.WorkerSettings.MemoryMiB,
			t.WorkerSettings.MemoryMiB/1024,
			t.WorkerSettings.DiskGiB,
		)
	}

	b.WriteString("--------------------------------------------------------------------------------\n")
	fmt.Fprintf(&b, "Total Resource Allocation: %d vCPUs | %.1f GiB RAM | %d GiB Disk\n",
		t.TotalVCPUs(),
		t.TotalMemoryGiB(),
		t.TotalDiskGiB(),
	)
	return b.String()
}
