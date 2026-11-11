package config

import (
	"fmt"
	"strconv"
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

// TotalDiskGiB returns the total storage in GiB requested across all cluster nodes and all disks.
func (t *NodeTopology) TotalDiskGiB() int {
	return (t.ControlPlaneReplicas * t.ControlPlaneSettings.TotalDiskGiB()) +
		(t.WorkerReplicas * t.WorkerSettings.TotalDiskGiB())
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

	topo.ControlPlaneSettings.Disks = topo.ControlPlaneSettings.ResolvedDisks()
	topo.WorkerSettings.Disks = topo.WorkerSettings.ResolvedDisks()

	applyNodeOverrides(&topo.ControlPlaneSettings, c.ControlPlaneOverrides)
	if topo.WorkerReplicas > 0 {
		applyNodeOverrides(&topo.WorkerSettings, c.WorkerOverrides)
	}

	return topo, nil
}

func applyNodeOverrides(dst, src *NodeSettings) {
	if src == nil {
		return
	}
	if src.VCPUs > 0 {
		dst.VCPUs = src.VCPUs
	}
	if src.MemoryMiB > 0 {
		dst.MemoryMiB = src.MemoryMiB
	}
	if len(src.Disks) > 0 {
		disks := make([]DiskSettings, len(src.Disks))
		copy(disks, src.Disks)
		dst.Disks = disks
		dst.DiskGiB = disks[0].SizeGiB
	} else if src.DiskGiB > 0 {
		dst.DiskGiB = src.DiskGiB
		dst.Disks = []DiskSettings{{SizeGiB: src.DiskGiB}}
	}
}

func formatNodeDisks(s NodeSettings) string {
	disks := s.ResolvedDisks()
	if len(disks) <= 1 {
		return fmt.Sprintf("%-6d GiB", s.TotalDiskGiB())
	}
	parts := make([]string, len(disks))
	for i, d := range disks {
		parts[i] = strconv.Itoa(d.SizeGiB)
	}
	return fmt.Sprintf("%d GiB (%s)", s.TotalDiskGiB(), strings.Join(parts, "+"))
}

// FormatSummaryTable renders an aligned human-readable summary table for CLI output.
func (t *NodeTopology) FormatSummaryTable() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cluster Topology Preset: %s (%d total nodes)\n", strings.ToUpper(t.Preset), t.TotalNodes())
	b.WriteString("--------------------------------------------------------------------------------\n")
	fmt.Fprintf(&b, "%-16s | %-8s | %-12s | %-14s | %-10s\n", "Role", "Count", "vCPUs/node", "RAM/node", "Disk/node")
	b.WriteString("--------------------------------------------------------------------------------\n")

	fmt.Fprintf(&b, "%-16s | %-8d | %-12d | %-6d MiB (%-2dG) | %s\n",
		"Control Plane",
		t.ControlPlaneReplicas,
		t.ControlPlaneSettings.VCPUs,
		t.ControlPlaneSettings.MemoryMiB,
		t.ControlPlaneSettings.MemoryMiB/1024,
		formatNodeDisks(t.ControlPlaneSettings),
	)

	if t.WorkerReplicas > 0 {
		fmt.Fprintf(&b, "%-16s | %-8d | %-12d | %-6d MiB (%-2dG) | %s\n",
			"Worker",
			t.WorkerReplicas,
			t.WorkerSettings.VCPUs,
			t.WorkerSettings.MemoryMiB,
			t.WorkerSettings.MemoryMiB/1024,
			formatNodeDisks(t.WorkerSettings),
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
