package cluster

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/host"
)

func TestInstall_HappyPath_Sequencing(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		OCPVersion:  "4.22.0",
		Preset:      config.PresetSNO,
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
	}

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			if strings.Contains(command, installProbeScript[:20]) || strings.Contains(command, "VIRTLAB_STATUS=") {
				return "VIRTLAB_STATUS=NOT_INSTALLED\n", nil
			}
			return "", nil
		},
		RunScriptFn: func(script string, args ...string) (string, error) {
			return "", nil
		},
		RunScriptStreamFn: func(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
			return nil
		},
	}

	mgr := NewOpenShiftManagerWithRunner(cfg, mockRunner)

	err := mgr.Install(context.Background())
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	calls := mockRunner.RecordedCalls()
	if len(calls) == 0 {
		t.Fatal("expected runner calls, got none")
	}

	var stages []string
	for _, c := range calls {
		switch {
		case strings.Contains(c.Command, "VIRTLAB_STATUS") || strings.Contains(c.Command, "installProbeScript"):
			stages = append(stages, "probe")
		case strings.Contains(c.Script, "install-config.yaml"):
			stages = append(stages, "setup")
		case strings.Contains(c.Script, "agent create image"):
			stages = append(stages, "buildIso")
		case strings.Contains(c.Script, "virsh change-media") && strings.Contains(c.Script, "--insert"):
			stages = append(stages, "boot")
		case strings.Contains(c.Script, "agent wait-for install-complete"):
			stages = append(stages, "wait")
		case strings.Contains(c.Command, "virsh change-media") && strings.Contains(c.Command, "--eject"):
			stages = append(stages, "eject")
		case strings.Contains(c.Script, "rm -f \"$DIR/agent.x86_64.iso\""):
			stages = append(stages, "cleanup")
		case strings.Contains(c.Script, ".virtlab-install-state.yaml"):
			stages = append(stages, "persistState")
		}
	}

	expectedOrder := []string{"probe", "setup", "buildIso", "boot", "wait", "eject", "cleanup", "persistState"}
	if len(stages) != len(expectedOrder) {
		t.Fatalf("expected stages %v, got %v", expectedOrder, stages)
	}
	for i, expected := range expectedOrder {
		if stages[i] != expected {
			t.Errorf("stage[%d] = %s, want %s", i, stages[i], expected)
		}
	}
}

func TestInstall_ResumeHealthy_SkipsInstall(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		OCPVersion:  "4.22.0",
		Preset:      config.PresetSNO,
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
	}

	mgr := NewOpenShiftManagerWithRunner(cfg, nil)
	probe := healthyProbe(t, mgr)

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			return probe, nil
		},
	}
	mgr.Runner = mockRunner

	err := mgr.Install(context.Background())
	if err != nil {
		t.Fatalf("Install unexpected error: %v", err)
	}

	calls := mockRunner.RecordedCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 call (probe), got %d", len(calls))
	}
	if calls[0].Method != "Run" {
		t.Errorf("call method = %s, want Run", calls[0].Method)
	}
}

func TestInstall_ResumeMismatch_Aborts(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		OCPVersion:  "4.22.0",
		Preset:      config.PresetSNO,
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
	}

	installed := &OpenShiftManager{Config: resumeTestConfig(config.PresetHA, "4.22.0")}
	probe := healthyProbe(t, installed)

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			return probe, nil
		},
	}

	mgr := NewOpenShiftManagerWithRunner(cfg, mockRunner)
	err := mgr.Install(context.Background())
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "preset:") {
		t.Errorf("error %v should mention preset mismatch", err)
	}

	calls := mockRunner.RecordedCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (probe), got %d", len(calls))
	}
}

func TestInstall_BootFailure_AbortsBeforeWait(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		OCPVersion:  "4.22.0",
		Preset:      config.PresetSNO,
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
	}

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			return "VIRTLAB_STATUS=NOT_INSTALLED\n", nil
		},
		RunScriptFn: func(script string, args ...string) (string, error) {
			if strings.Contains(script, "virsh change-media") && strings.Contains(script, "--insert") {
				return "", errors.New("simulated domain boot failure")
			}
			return "", nil
		},
	}

	mgr := NewOpenShiftManagerWithRunner(cfg, mockRunner)
	err := mgr.Install(context.Background())
	if err == nil {
		t.Fatal("expected boot failure error, got nil")
	}
	if !strings.Contains(err.Error(), "start node 'lab-sno-master-0'") {
		t.Errorf("error %v should mention node start error", err)
	}

	for _, c := range mockRunner.RecordedCalls() {
		if c.Method == "RunScriptStream" {
			t.Error("RunScriptStream (waitScript) should NOT have been called after boot failure")
		}
	}
}

func TestInstall_WaitFailure_StillEjectsMedia(t *testing.T) {
	keyFile := createTestSSHKey(t)
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		OCPVersion:  "4.22.0",
		Preset:      config.PresetSNO,
		Network: config.NetworkConfig{
			CIDR:      "192.168.100.0/24",
			GatewayIP: "192.168.100.1",
		},
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: keyFile,
		},
	}

	ejected := false
	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			if strings.Contains(command, "--eject") {
				ejected = true
				return "", nil
			}
			return "VIRTLAB_STATUS=NOT_INSTALLED\n", nil
		},
		RunScriptFn: func(script string, args ...string) (string, error) {
			return "", nil
		},
		RunScriptStreamFn: func(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
			return errors.New("simulated wait timeout")
		},
	}

	mgr := NewOpenShiftManagerWithRunner(cfg, mockRunner)
	err := mgr.Install(context.Background())
	if err == nil {
		t.Fatal("expected wait failure error, got nil")
	}
	if !strings.Contains(err.Error(), "simulated wait timeout") {
		t.Errorf("error %v should mention wait timeout", err)
	}

	if !ejected {
		t.Error("expected media eject command to still be executed after wait failure")
	}
}

func TestInstall_MissingRunner_Fails(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		Preset:      config.PresetSNO,
	}
	mgr := NewOpenShiftManager(cfg, nil)
	err := mgr.Install(context.Background())
	if err == nil {
		t.Fatal("expected error with nil host/runner, got nil")
	}
}

func TestEnsureIngressReplicas_WithMockRunner(t *testing.T) {
	t.Run("sno skips", func(t *testing.T) {
		cfg := &config.Config{Preset: config.PresetSNO}
		mockRunner := &host.MockCommandRunner{}
		mgr := NewOpenShiftManagerWithRunner(cfg, mockRunner)
		if err := mgr.EnsureIngressReplicas(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mockRunner.RecordedCalls()) != 0 {
			t.Errorf("expected 0 calls for SNO, got %d", len(mockRunner.RecordedCalls()))
		}
	})

	t.Run("ha executes patch", func(t *testing.T) {
		cfg := &config.Config{
			ClusterName: "lab-ha",
			BaseDomain:  "example.com",
			Preset:      config.PresetHA,
			Network: config.NetworkConfig{
				CIDR: "192.168.100.0/24",
			},
		}
		mockRunner := &host.MockCommandRunner{}
		mgr := NewOpenShiftManagerWithRunner(cfg, mockRunner)
		if err := mgr.EnsureIngressReplicas(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		calls := mockRunner.RecordedCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Method != "RunScriptContext" {
			t.Errorf("method = %s, want RunScriptContext", calls[0].Method)
		}
		if !strings.Contains(calls[0].Script, `"replicas":2`) {
			t.Errorf("script should contain 2 replicas for HA, got:\n%s", calls[0].Script)
		}
	})
}
