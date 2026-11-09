package cluster

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/infrastructure"
)

// installStateFile is the name of the file that records the declaration which
// produced the running cluster. It lives inside the installer directory.
const installStateFile = ".virtlab-install-state.yaml"

// InstallState is the applied declaration persisted after a successful
// install. It lets a later `virtlab up` detect that config.yaml no longer
// matches the running cluster instead of silently adopting it.
type InstallState struct {
	Preset     string      `yaml:"preset"`
	OCPVersion string      `yaml:"ocpVersion"`
	Nodes      []StateNode `yaml:"nodes"`
}

// StateNode is one cluster node as it was applied to the running cluster.
type StateNode struct {
	Name string `yaml:"name"`
	Role string `yaml:"role"`
	MAC  string `yaml:"mac"`
	IP   string `yaml:"ip"`
}

// InstallStatePath returns the host path of the applied-state record.
func (m *OpenShiftManager) InstallStatePath() string {
	return m.InstallDir() + "/" + installStateFile
}

// DesiredInstallState derives the state that the current configuration would
// apply. It uses the same lease computation as the rendered agent-config, so
// the record and the manifests cannot drift apart.
func (m *OpenShiftManager) DesiredInstallState() (*InstallState, error) {
	leases, err := infrastructure.ComputeNodeLeases(m.Config)
	if err != nil {
		return nil, fmt.Errorf("cluster: compute node leases: %w", err)
	}

	preset := strings.ToLower(strings.TrimSpace(m.Config.Preset))
	if preset == "" {
		preset = config.PresetSNO
	}

	nodes := make([]StateNode, 0, len(leases))
	for _, l := range leases {
		nodes = append(nodes, StateNode{
			Name: l.Name,
			Role: l.Role,
			MAC:  l.MAC,
			IP:   l.IP,
		})
	}

	return &InstallState{
		Preset:     preset,
		OCPVersion: strings.TrimSpace(m.Config.OCPVersion),
		Nodes:      nodes,
	}, nil
}

// RenderInstallState serializes an InstallState as YAML.
func RenderInstallState(state *InstallState) (string, error) {
	data, err := yaml.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("cluster: marshal install state: %w", err)
	}
	return string(data), nil
}

// ParseInstallState parses a persisted InstallState.
func ParseInstallState(data string) (*InstallState, error) {
	var state InstallState
	if err := yaml.Unmarshal([]byte(data), &state); err != nil {
		return nil, fmt.Errorf("cluster: parse install state: %w", err)
	}
	return &state, nil
}

// Diff returns human-readable differences between the applied state and the
// desired state. An empty slice means the declaration still matches.
func (s *InstallState) Diff(desired *InstallState) []string {
	var diffs []string

	if normalizePreset(s.Preset) != normalizePreset(desired.Preset) {
		diffs = append(diffs, fmt.Sprintf("preset: installed %q, declared %q", s.Preset, desired.Preset))
	}
	if normalizeVersion(s.OCPVersion) != normalizeVersion(desired.OCPVersion) {
		diffs = append(diffs, fmt.Sprintf("ocpVersion: installed %q, declared %q", s.OCPVersion, desired.OCPVersion))
	}

	installed := nodeIndex(s.Nodes)
	declared := nodeIndex(desired.Nodes)
	names := make([]string, 0, len(installed)+len(declared))
	for name := range installed {
		names = append(names, name)
	}
	for name := range declared {
		if _, ok := installed[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		was, wasOK := installed[name]
		want, wantOK := declared[name]
		switch {
		case wasOK && !wantOK:
			diffs = append(diffs, fmt.Sprintf("node %q: installed but no longer declared", name))
		case !wasOK && wantOK:
			diffs = append(diffs, fmt.Sprintf("node %q: declared but not installed", name))
		case was != want:
			diffs = append(diffs, fmt.Sprintf("node %q: installed %s %s %s, declared %s %s %s",
				name, was.Role, was.MAC, was.IP, want.Role, want.MAC, want.IP))
		}
	}

	return diffs
}

func normalizePreset(preset string) string {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		return config.PresetSNO
	}
	return preset
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	return strings.TrimPrefix(version, "v")
}

func nodeIndex(nodes []StateNode) map[string]StateNode {
	index := make(map[string]StateNode, len(nodes))
	for _, node := range nodes {
		index[node.Name] = node
	}
	return index
}
