package cluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func platformTestConfig(preset string) *config.Config {
	return &config.Config{
		ClusterName: "lab",
		BaseDomain:  "example.com",
		Preset:      preset,
	}
}

func TestExpectedPlatform(t *testing.T) {
	tests := []struct {
		preset string
		want   string
	}{
		{config.PresetSNO, "None"},
		{config.PresetCompact, "BareMetal"},
		{config.PresetHA, "BareMetal"},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			mgr := &OpenShiftManager{Config: platformTestConfig(tt.preset)}
			if got := mgr.ExpectedPlatform(); got != tt.want {
				t.Errorf("ExpectedPlatform() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseClusterPlatform(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		want   string
		wantOK bool
	}{
		{"none", "None", "None", true},
		{"baremetal", "BareMetal\n", "BareMetal", true},
		{"warning then value", "some warning\nBareMetal\n", "BareMetal", true},
		{"no cluster", "NO_CLUSTER", "", false},
		{"unknown", "UNKNOWN", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseClusterPlatform(tt.out)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("platform = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsurePlatformCompatible(t *testing.T) {
	t.Run("mismatch fails with teardown guidance", func(t *testing.T) {
		mgr := &OpenShiftManager{
			Config:     platformTestConfig(config.PresetCompact),
			runCommand: func(string) (string, error) { return "None", nil },
		}
		err := mgr.EnsurePlatformCompatible()
		if err == nil {
			t.Fatal("expected a platform mismatch error")
		}
		if !strings.Contains(err.Error(), "immutable") || !strings.Contains(err.Error(), "virtlab down") {
			t.Errorf("error should explain immutability and the fix, got: %v", err)
		}
	})

	t.Run("match passes", func(t *testing.T) {
		mgr := &OpenShiftManager{
			Config:     platformTestConfig(config.PresetCompact),
			runCommand: func(string) (string, error) { return "BareMetal", nil },
		}
		if err := mgr.EnsurePlatformCompatible(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no cluster passes", func(t *testing.T) {
		mgr := &OpenShiftManager{
			Config:     platformTestConfig(config.PresetCompact),
			runCommand: func(string) (string, error) { return "NO_CLUSTER", nil },
		}
		if err := mgr.EnsurePlatformCompatible(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("probe failure warns and proceeds", func(t *testing.T) {
		mgr := &OpenShiftManager{
			Config:     platformTestConfig(config.PresetCompact),
			runCommand: func(string) (string, error) { return "", errors.New("ssh down") },
		}
		if err := mgr.EnsurePlatformCompatible(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBuildIngressReplicasScript(t *testing.T) {
	script := BuildIngressReplicasScript("/home/libvirt/lab/installer", 3)
	required := []string{
		`DIR='/home/libvirt/lab/installer'`,
		`export KUBECONFIG="$DIR/auth/kubeconfig"`,
		`oc -n openshift-ingress-operator patch ingresscontroller default --type=merge`,
		`{"spec":{"replicas":3}}`,
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}
