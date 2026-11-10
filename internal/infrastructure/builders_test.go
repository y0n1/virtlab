package infrastructure

import (
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

type infraMocks int

func (infraMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	outputs := args.Inputs.Copy()
	if !outputs["name"].HasValue() {
		outputs["name"] = resource.NewStringProperty(args.Name)
	}
	return args.Name + "_id", outputs, nil
}

func (infraMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return args.Args, nil
}

func TestPhase2Builders_SNO(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.com",
		Preset:      config.PresetSNO,
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: filepath.Join(t.TempDir(), "id_ed25519.pub"),
		},
		Network: config.NetworkConfig{
			Name: "lab-sno-net",
			CIDR: "192.168.100.0/24",
		},
	}

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		pool, err := BuildStoragePool(ctx, cfg)
		if err != nil {
			return err
		}
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		net, err := BuildNetwork(ctx, cfg, "virbr-lab0")
		if err != nil {
			return err
		}
		if net == nil {
			t.Fatal("expected non-nil net")
		}

		guests, err := BuildGuests(ctx, cfg, pool, net)
		if err != nil {
			return err
		}
		if len(guests) != 1 {
			t.Fatalf("expected 1 guest for SNO, got %d", len(guests))
		}
		if guests[0].Lease.Name != "master-0" {
			t.Errorf("expected lease master-0, got %s", guests[0].Lease.Name)
		}
		return nil
	}, pulumi.WithMocks("virtlab", "test", infraMocks(0)))

	if err != nil {
		t.Fatalf("pulumi RunErr failed: %v", err)
	}
}

func TestPhase2Builders_HA(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-ha",
		BaseDomain:  "example.com",
		Preset:      config.PresetHA,
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: filepath.Join(t.TempDir(), "id_ed25519.pub"),
		},
		Network: config.NetworkConfig{
			Name: "lab-ha-net",
			CIDR: "192.168.100.0/24",
		},
	}

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		pool, err := BuildStoragePool(ctx, cfg)
		if err != nil {
			return err
		}
		net, err := BuildNetwork(ctx, cfg, "virbr-lab1")
		if err != nil {
			return err
		}
		guests, err := BuildGuests(ctx, cfg, pool, net)
		if err != nil {
			return err
		}
		if len(guests) != 5 {
			t.Fatalf("expected 5 guests for HA, got %d", len(guests))
		}
		expectedNames := []string{"master-0", "master-1", "master-2", "worker-0", "worker-1"}
		for i, name := range expectedNames {
			if guests[i].Lease.Name != name {
				t.Errorf("guest[%d] name = %s, want %s", i, guests[i].Lease.Name, name)
			}
		}
		return nil
	}, pulumi.WithMocks("virtlab", "test", infraMocks(0)))

	if err != nil {
		t.Fatalf("pulumi RunErr failed: %v", err)
	}
}
