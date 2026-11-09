package workstation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ManagedBinary records installation details for a tool-managed dependency.
type ManagedBinary struct {
	InstalledByVirtlab bool      `json:"installedByVirtlab"`
	InstallMethod      string    `json:"installMethod,omitempty"` // e.g. "brew", "dnf", "direct"
	BinaryPath         string    `json:"binaryPath,omitempty"`
	InstalledAt        time.Time `json:"installedAt,omitempty"`
}

// State represents the persistent local workstation state stored at ~/.local/state/virtlab/state.json.
type State struct {
	Version            int                      `json:"version"`
	CreatedAt          time.Time                `json:"createdAt"`
	UpdatedAt          time.Time                `json:"updatedAt"`
	ManagedPackages    map[string]ManagedBinary `json:"managedPackages"`
	ClusterName        string                   `json:"clusterName,omitempty"`
	BaseDomain         string                   `json:"baseDomain,omitempty"`
	GatewayIP          string                   `json:"gatewayIP,omitempty"`
	SplitDNSConfigured bool                     `json:"splitDNSConfigured"`
	TunnelActive       bool                     `json:"tunnelActive"`
	TunnelPID          int                      `json:"tunnelPID,omitempty"`
}

// NewEmptyState returns a fresh default State instance.
func NewEmptyState() *State {
	now := time.Now().UTC()
	return &State{
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
		ManagedPackages: make(map[string]ManagedBinary),
	}
}

// IsInstalledByVirtlab returns whether the binary was installed by virtlab.
func (s *State) IsInstalledByVirtlab(name string) bool {
	if s.ManagedPackages == nil {
		return false
	}
	mb, ok := s.ManagedPackages[name]
	return ok && mb.InstalledByVirtlab
}

// RecordInstalled marks a package as installed by virtlab.
func (s *State) RecordInstalled(name, method, binPath string) {
	if s.ManagedPackages == nil {
		s.ManagedPackages = make(map[string]ManagedBinary)
	}
	s.ManagedPackages[name] = ManagedBinary{
		InstalledByVirtlab: true,
		InstallMethod:      method,
		BinaryPath:         binPath,
		InstalledAt:        time.Now().UTC(),
	}
}

// RecordUninstalled marks a package as uninstalled or clears its virtlab flag.
func (s *State) RecordUninstalled(name string) {
	if s.ManagedPackages == nil {
		return
	}
	delete(s.ManagedPackages, name)
}

// HasManagedPackages returns true if any package is currently recorded as installed by virtlab.
func (s *State) HasManagedPackages() bool {
	for _, mb := range s.ManagedPackages {
		if mb.InstalledByVirtlab {
			return true
		}
	}
	return false
}

// HasActiveLab returns true if any cluster DNS or tunnel configuration is currently recorded.
func (s *State) HasActiveLab() bool {
	return s.SplitDNSConfigured || s.TunnelActive || s.ClusterName != ""
}

// IsEmpty returns true if there are no managed packages or active lab configurations.
func (s *State) IsEmpty() bool {
	return !s.HasManagedPackages() && !s.HasActiveLab()
}

// StateManager handles reading and writing the state file.
type StateManager struct {
	Path string
}

// StateDir returns the XDG-compliant state directory (~/.local/state/virtlab or $XDG_STATE_HOME/virtlab).
func StateDir() string {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "virtlab")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "state", "virtlab")
}

// DefaultStateFilePath resolves ~/.local/state/virtlab/state.json adhering to XDG specs.
func DefaultStateFilePath() string {
	return filepath.Join(StateDir(), "state.json")
}

// NewStateManager creates a StateManager for the given path, defaulting to DefaultStateFilePath.
func NewStateManager(customPath ...string) *StateManager {
	path := DefaultStateFilePath()
	if len(customPath) > 0 && customPath[0] != "" {
		path = customPath[0]
	}
	return &StateManager{Path: path}
}

// Load reads the state from disk. If the file does not exist, an empty state is returned without error.
func (m *StateManager) Load() (*State, error) {
	data, err := os.ReadFile(m.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewEmptyState(), nil
		}
		return nil, fmt.Errorf("read state file '%s': %w", m.Path, err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("corrupt state file '%s': %w", m.Path, err)
	}

	if state.ManagedPackages == nil {
		state.ManagedPackages = make(map[string]ManagedBinary)
	}
	return &state, nil
}

// Save writes the state atomically to disk.
func (m *StateManager) Save(s *State) error {
	s.UpdatedAt = time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
	if s.Version == 0 {
		s.Version = 1
	}

	dir := filepath.Dir(m.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state directory '%s': %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Write to temporary file first then rename for atomic update
	tmpPath := fmt.Sprintf("%s.tmp.%d", m.Path, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}

	if err := os.Rename(tmpPath, m.Path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename state file: %w", err)
	}

	return nil
}

// Remove deletes the state file and cleans up parent directories if empty.
func (m *StateManager) Remove() error {
	if err := os.Remove(m.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove state file '%s': %w", m.Path, err)
	}

	// Try removing the parent directory if empty
	dir := filepath.Dir(m.Path)
	_ = os.Remove(dir)

	return nil
}
