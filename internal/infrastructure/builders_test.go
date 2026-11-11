package infrastructure

import (
	"path/filepath"
	"sync"
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

type recordingMocks struct {
	mu        sync.Mutex
	resources []pulumi.MockResourceArgs
}

func (m *recordingMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.resources = append(m.resources, args)
	m.mu.Unlock()

	outputs := args.Inputs.Copy()
	if !outputs["name"].HasValue() {
		outputs["name"] = resource.NewStringProperty(args.Name)
	}
	return args.Name + "_id", outputs, nil
}

func (*recordingMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
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

func TestBuildGuests_MultipleDisks(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-disks",
		BaseDomain:  "example.com",
		Preset:      config.PresetHA,
		HostSSH: config.HostSSHConfig{
			PublicKeyFile: filepath.Join(t.TempDir(), "id_ed25519.pub"),
		},
		Network: config.NetworkConfig{
			Name: "lab-disks-net",
			CIDR: "192.168.100.0/24",
		},
		ControlPlaneOverrides: &config.NodeSettings{
			Disks: []config.DiskSettings{
				{SizeGiB: 120},
				{SizeGiB: 250},
			},
		},
		WorkerOverrides: &config.NodeSettings{
			Disks: []config.DiskSettings{
				{SizeGiB: 100},
				{SizeGiB: 300},
				{SizeGiB: 400},
			},
		},
	}

	mocks := &recordingMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		pool, err := BuildStoragePool(ctx, cfg)
		if err != nil {
			return err
		}
		net, err := BuildNetwork(ctx, cfg, "virbr-lab2")
		if err != nil {
			return err
		}
		guests, err := BuildGuests(ctx, cfg, pool, net)
		if err != nil {
			return err
		}
		if len(guests) != 5 {
			t.Fatalf("expected 5 guests, got %d", len(guests))
		}
		for _, g := range guests {
			wantVols := 2
			if g.Lease.Role == "worker" {
				wantVols = 3
			}
			if len(g.Volumes) != wantVols {
				t.Errorf("guest %s: got %d volumes, want %d", g.Lease.Name, len(g.Volumes), wantVols)
			}
			if g.Volume == nil || len(g.Volumes) == 0 || g.Volume != g.Volumes[0] {
				t.Errorf("guest %s: Volume does not point to Volumes[0]", g.Lease.Name)
			}
		}
		return nil
	}, pulumi.WithMocks("virtlab", "test", mocks))
	if err != nil {
		t.Fatalf("pulumi RunErr failed: %v", err)
	}

	volumes := make(map[string]float64)
	domains := make(map[string]resource.PropertyMap)
	for _, r := range mocks.resources {
		switch r.TypeToken {
		case "libvirt:index/volume:Volume":
			name := r.Inputs["name"].StringValue()
			cap := r.Inputs["capacity"].NumberValue()
			volumes[name] = cap
		case "libvirt:index/domain:Domain":
			domains[r.Name] = r.Inputs
		}
	}

	expectedVolumes := map[string]float64{
		"lab-disks-master-0.qcow2":     120,
		"lab-disks-master-0-vdb.qcow2": 250,
		"lab-disks-master-1.qcow2":     120,
		"lab-disks-master-1-vdb.qcow2": 250,
		"lab-disks-master-2.qcow2":     120,
		"lab-disks-master-2-vdb.qcow2": 250,
		"lab-disks-worker-0.qcow2":     100,
		"lab-disks-worker-0-vdb.qcow2": 300,
		"lab-disks-worker-0-vdc.qcow2": 400,
		"lab-disks-worker-1.qcow2":     100,
		"lab-disks-worker-1-vdb.qcow2": 300,
		"lab-disks-worker-1-vdc.qcow2": 400,
	}
	if len(volumes) != len(expectedVolumes) {
		t.Fatalf("created %d volumes, want %d: %v", len(volumes), len(expectedVolumes), volumes)
	}
	for name, wantCap := range expectedVolumes {
		if gotCap, ok := volumes[name]; !ok || gotCap != wantCap {
			t.Errorf("volume %s capacity = %v (ok=%v), want %v", name, gotCap, ok, wantCap)
		}
	}

	workerDom := domains["lab-disks-worker-0"]
	disks := workerDom["devices"].ObjectValue()["disks"].ArrayValue()
	if len(disks) != 4 { // 3 virtio disks + 1 cdrom
		t.Fatalf("worker-0 domain has %d disks, want 4", len(disks))
	}
	wantDevs := []string{"vda", "vdb", "vdc", "sda"}
	for i, wantDev := range wantDevs {
		gotDev := disks[i].ObjectValue()["target"].ObjectValue()["dev"].StringValue()
		if gotDev != wantDev {
			t.Errorf("worker-0 disk[%d] target dev = %q, want %q", i, gotDev, wantDev)
		}
	}
}
