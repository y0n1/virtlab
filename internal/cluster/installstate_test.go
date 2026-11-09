package cluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func resumeTestConfig(preset, version string) *config.Config {
	return &config.Config{
		ClusterName: "lab",
		BaseDomain:  "example.com",
		OCPVersion:  version,
		Preset:      preset,
		Network: config.NetworkConfig{
			CIDR: "192.168.100.0/24",
		},
	}
}

// healthyProbe renders a healthy probe response carrying the desired state.
func healthyProbe(t *testing.T, mgr *OpenShiftManager) string {
	t.Helper()
	state, err := mgr.DesiredInstallState()
	if err != nil {
		t.Fatalf("desired install state: %v", err)
	}
	body, err := RenderInstallState(state)
	if err != nil {
		t.Fatalf("render install state: %v", err)
	}
	return probeStatusPrefix + probeStatusHealthy + "\n" +
		probeStateBegin + "\n" + body + probeStateEnd + "\n"
}

func TestCheckResume(t *testing.T) {
	t.Run("healthy matching declaration resumes", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		probe := healthyProbe(t, mgr)
		mgr.runCommand = func(string) (string, error) { return probe, nil }

		resumed, err := mgr.checkResume()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resumed {
			t.Fatal("expected the healthy matching cluster to be resumed")
		}
	})

	t.Run("preset mismatch fails with teardown guidance", func(t *testing.T) {
		installedCfg := resumeTestConfig(config.PresetSNO, "4.22.0")
		installed := &OpenShiftManager{Config: installedCfg}
		probe := healthyProbe(t, installed)

		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return probe, nil }

		resumed, err := mgr.checkResume()
		if err == nil {
			t.Fatal("expected a preset mismatch error")
		}
		if resumed {
			t.Fatal("a mismatched cluster must not be resumed")
		}
		for _, want := range []string{`preset:`, `installed "sno"`, `declared "ha"`, "virtlab down", "virtlab up"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should contain %q, got: %v", want, err)
			}
		}
	})

	t.Run("version mismatch fails with teardown guidance", func(t *testing.T) {
		installed := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.21.0")}
		probe := healthyProbe(t, installed)

		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return probe, nil }

		_, err := mgr.checkResume()
		if err == nil {
			t.Fatal("expected an ocpVersion mismatch error")
		}
		for _, want := range []string{"ocpVersion:", `installed "4.21.0"`, `declared "4.22.0"`, "virtlab down"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should contain %q, got: %v", want, err)
			}
		}
	})

	t.Run("node inventory mismatch fails", func(t *testing.T) {
		installed := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		state, err := installed.DesiredInstallState()
		if err != nil {
			t.Fatalf("desired install state: %v", err)
		}
		state.Nodes = state.Nodes[:len(state.Nodes)-1] // drop the last worker
		body, err := RenderInstallState(state)
		if err != nil {
			t.Fatalf("render install state: %v", err)
		}
		probe := probeStatusPrefix + probeStatusHealthy + "\n" +
			probeStateBegin + "\n" + body + probeStateEnd + "\n"

		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return probe, nil }

		_, err = mgr.checkResume()
		if err == nil {
			t.Fatal("expected a node inventory mismatch error")
		}
		if !strings.Contains(err.Error(), `"worker-1": declared but not installed`) {
			t.Errorf("error should name the missing node, got: %v", err)
		}
	})

	t.Run("healthy cluster without state is adopted with a warning", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) {
			return probeStatusPrefix + probeStatusHealthy + "\n", nil
		}

		resumed, err := mgr.checkResume()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resumed {
			t.Fatal("a healthy cluster installed by an older virtlab should still be resumed")
		}
	})

	t.Run("not installed proceeds", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) {
			return probeStatusPrefix + probeStatusNotInstalled + "\n", nil
		}

		resumed, err := mgr.checkResume()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resumed {
			t.Fatal("a not-installed cluster must not be resumed")
		}
	})

	t.Run("probe failure never proceeds to install", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return "", errors.New("ssh: connection reset") }

		_, err := mgr.checkResume()
		if err == nil {
			t.Fatal("expected a probe failure to abort the run")
		}
		if !strings.Contains(err.Error(), "refusing to modify") {
			t.Errorf("error should state that nothing was modified, got: %v", err)
		}
	})

	t.Run("unhealthy cluster is unknown and never reinstalled", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) {
			return probeStatusPrefix + probeStatusUnhealthy + "\n", nil
		}

		_, err := mgr.checkResume()
		if err == nil {
			t.Fatal("expected an unhealthy probe to abort the run")
		}
		if !strings.Contains(err.Error(), "healthz") || !strings.Contains(err.Error(), "unknown") {
			t.Errorf("error should explain the unknown state, got: %v", err)
		}
	})

	t.Run("unrecognized probe output aborts", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return "garbage", nil }

		_, err := mgr.checkResume()
		if err == nil {
			t.Fatal("expected an unrecognized probe response to abort the run")
		}
	})

	t.Run("corrupt state block aborts", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) {
			return probeStatusPrefix + probeStatusHealthy + "\n" +
				probeStateBegin + "\nnot: [valid\n" + probeStateEnd + "\n", nil
		}

		_, err := mgr.checkResume()
		if err == nil {
			t.Fatal("expected a corrupt state record to abort the run")
		}
	})
}

func TestEnsureDeclarationCompatible(t *testing.T) {
	t.Run("healthy mismatch fails before any change", func(t *testing.T) {
		installed := &OpenShiftManager{Config: resumeTestConfig(config.PresetSNO, "4.22.0")}
		probe := healthyProbe(t, installed)

		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return probe, nil }

		err := mgr.EnsureDeclarationCompatible()
		if err == nil {
			t.Fatal("expected a declaration mismatch error")
		}
		if !strings.Contains(err.Error(), "preset:") || !strings.Contains(err.Error(), "virtlab down") {
			t.Errorf("error should explain the mismatch and the fix, got: %v", err)
		}
	})

	t.Run("healthy match passes", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		probe := healthyProbe(t, mgr)
		mgr.runCommand = func(string) (string, error) { return probe, nil }

		if err := mgr.EnsureDeclarationCompatible(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("probe failure warns and proceeds", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) { return "", errors.New("ssh down") }

		if err := mgr.EnsureDeclarationCompatible(); err != nil {
			t.Fatalf("a flaky probe must not block recovery, got: %v", err)
		}
	})

	t.Run("not installed passes", func(t *testing.T) {
		mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
		mgr.runCommand = func(string) (string, error) {
			return probeStatusPrefix + probeStatusNotInstalled + "\n", nil
		}

		if err := mgr.EnsureDeclarationCompatible(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInstallStateRoundTrip(t *testing.T) {
	mgr := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
	state, err := mgr.DesiredInstallState()
	if err != nil {
		t.Fatalf("desired install state: %v", err)
	}
	if len(state.Nodes) != 5 {
		t.Fatalf("expected 5 HA nodes, got %d", len(state.Nodes))
	}

	body, err := RenderInstallState(state)
	if err != nil {
		t.Fatalf("render install state: %v", err)
	}
	parsed, err := ParseInstallState(body)
	if err != nil {
		t.Fatalf("parse install state: %v", err)
	}
	if diffs := parsed.Diff(state); len(diffs) != 0 {
		t.Errorf("round-tripped state should match itself, got diffs: %v", diffs)
	}
}

func TestInstallStateDiff_NormalizesVersionPrefix(t *testing.T) {
	installed := &InstallState{Preset: "SNO", OCPVersion: "v4.22.0"}
	desired := &InstallState{Preset: "sno", OCPVersion: "4.22.0"}
	if diffs := installed.Diff(desired); len(diffs) != 0 {
		t.Errorf("expected no diffs after normalization, got: %v", diffs)
	}
}
