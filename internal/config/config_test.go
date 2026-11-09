package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTempSSHKey(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "id_ed25519.pub")
	err := os.WriteFile(keyFile, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGoF9 user@workstation\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to create temp ssh key: %v", err)
	}
	return keyFile
}

func TestLoadConfigFromBytes(t *testing.T) {
	yamlData := `
clusterName: "test-sno"
baseDomain: "example.org"
ocpVersion: "4.22.0"
hostSsh:
  host: "hypervisor.lab"
  user: "root"
  publicKeyFile: "/path/to/key.pub"
preset: "sno"
`
	cfg, err := LoadConfigFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.ClusterName != "test-sno" {
		t.Errorf("want clusterName test-sno, got %s", cfg.ClusterName)
	}
	if cfg.Network.CIDR != "192.168.100.0/24" {
		t.Errorf("want default CIDR 192.168.100.0/24, got %s", cfg.Network.CIDR)
	}
	if cfg.Network.GatewayIP != "192.168.100.1" {
		t.Errorf("want derived gateway 192.168.100.1, got %s", cfg.Network.GatewayIP)
	}
	if !cfg.Network.CIDRAuto {
		t.Errorf("want CIDRAuto true when CIDR omitted, got false")
	}
	if cfg.StoragePoolDir() != "/home/libvirt/test-sno/pool" {
		t.Errorf("want pool dir /home/libvirt/test-sno/pool, got %s", cfg.StoragePoolDir())
	}
	if cfg.InstallDir() != "/home/libvirt/test-sno/installer" {
		t.Errorf("want install dir /home/libvirt/test-sno/installer, got %s", cfg.InstallDir())
	}
	if cfg.LogsDir() != "/home/libvirt/test-sno/logs" {
		t.Errorf("want logs dir /home/libvirt/test-sno/logs, got %s", cfg.LogsDir())
	}
	if cfg.ClusterDir() != "/home/libvirt/test-sno" {
		t.Errorf("want cluster dir /home/libvirt/test-sno, got %s", cfg.ClusterDir())
	}
	if cfg.APIHost() != "api.test-sno.example.org" {
		t.Errorf("want API host api.test-sno.example.org, got %s", cfg.APIHost())
	}
}

func TestLoadConfig_CustomCIDROverride(t *testing.T) {
	yamlData := `
clusterName: "test-custom"
network:
  cidr: "10.0.0.0/24"
`
	cfg, err := LoadConfigFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if cfg.Network.CIDR != "10.0.0.0/24" {
		t.Errorf("want CIDR 10.0.0.0/24, got %s", cfg.Network.CIDR)
	}
	if cfg.Network.GatewayIP != "10.0.0.1" {
		t.Errorf("want gateway 10.0.0.1, got %s", cfg.Network.GatewayIP)
	}
	if cfg.Network.CIDRAuto {
		t.Errorf("want CIDRAuto false when CIDR specified, got true")
	}
}

func TestLoadConfig_CustomGatewayIP_Rejected(t *testing.T) {
	yamlData := `
clusterName: "test-reject-gw"
network:
  gatewayIp: "10.0.0.1"
`
	cfg, err := LoadConfigFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	res := ValidateStatic(cfg)
	if res.Valid() {
		t.Fatalf("expected validation error for custom gatewayIp, got valid")
	}
}

func TestPresets(t *testing.T) {
	tests := []struct {
		preset     string
		wantNodes  int
		wantVCPUs  int
		wantRAMMiB int
		wantDisk   int
	}{
		{PresetSNO, 1, 8, 16384, 120},
		{PresetCompact, 3, 12, 49152, 300},
		{PresetHA, 5, 16, 65536, 500},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			cfg := &Config{Preset: tt.preset}
			topo, err := cfg.EffectiveTopology()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if topo.TotalNodes() != tt.wantNodes {
				t.Errorf("want nodes %d, got %d", tt.wantNodes, topo.TotalNodes())
			}
			if topo.TotalVCPUs() != tt.wantVCPUs {
				t.Errorf("want vcpus %d, got %d", tt.wantVCPUs, topo.TotalVCPUs())
			}
			if topo.TotalMemoryMiB() != tt.wantRAMMiB {
				t.Errorf("want RAM %d MiB, got %d MiB", tt.wantRAMMiB, topo.TotalMemoryMiB())
			}
			if topo.TotalDiskGiB() != tt.wantDisk {
				t.Errorf("want disk %d GiB, got %d GiB", tt.wantDisk, topo.TotalDiskGiB())
			}
			summary := topo.FormatSummaryTable()
			if !strings.Contains(summary, strings.ToUpper(tt.preset)) {
				t.Errorf("summary missing preset name: %s", summary)
			}
		})
	}
}

func TestValidateStatic(t *testing.T) {
	keyPath := createTempSSHKey(t)

	validConfig := func() *Config {
		return &Config{
			ClusterName: "lab-test",
			BaseDomain:  "example.com",
			OCPVersion:  "4.22.0",
			Preset:      PresetSNO,
			HostSSH: HostSSHConfig{
				Host:          "192.168.1.100",
				User:          "root",
				PublicKeyFile: keyPath,
			},
			Network: NetworkConfig{
				CIDR: "192.168.100.0/24",
			},
		}
	}

	t.Run("Valid baseline", func(t *testing.T) {
		res := ValidateStatic(validConfig())
		if !res.Valid() {
			t.Fatalf("expected valid config, got errors: %s", res.Summary())
		}
	})

	t.Run("Supported OCP Versions", func(t *testing.T) {
		for _, v := range []string{"4.20.12", "4.21.0-rc.1", "4.22.5"} {
			c := validConfig()
			c.OCPVersion = v
			res := ValidateStatic(c)
			if !res.Valid() {
				t.Errorf("version %s should be valid, got: %s", v, res.Summary())
			}
		}

		for _, v := range []string{"4.15.0", "4.18.56", "4.19.2", "4.22.x", "invalid-semver"} {
			c := validConfig()
			c.OCPVersion = v
			res := ValidateStatic(c)
			if res.Valid() {
				t.Errorf("version %s should be REJECTED (unsupported Y-stream or invalid semver)", v)
			}
		}
	})

	t.Run("SSH public key file missing / invalid", func(t *testing.T) {
		c := validConfig()
		c.HostSSH.PublicKeyFile = "/non/existent/path/id_rsa.pub"
		res := ValidateStatic(c)
		if res.Valid() {
			t.Fatal("expected failure for non-existent SSH public key file")
		}

		c.HostSSH.PublicKeyFile = ""
		res = ValidateStatic(c)
		if res.Valid() {
			t.Fatal("expected failure for empty SSH public key file path")
		}
	})

	t.Run("Invalid cluster name", func(t *testing.T) {
		c := validConfig()
		c.ClusterName = "Invalid_Name!"
		res := ValidateStatic(c)
		if res.Valid() {
			t.Fatal("expected error for non-RFC1123 clusterName")
		}
	})

	t.Run("Invalid CIDR", func(t *testing.T) {
		c := validConfig()
		c.Network.CIDR = "not-a-cidr"
		res := ValidateStatic(c)
		if res.Valid() {
			t.Fatal("expected error for invalid CIDR")
		}
	})

	t.Run("WorkerOverrides warning with SNO preset", func(t *testing.T) {
		c := validConfig()
		c.Preset = PresetSNO
		c.WorkerOverrides = &NodeSettings{VCPUs: 4}
		res := ValidateStatic(c)
		if !res.Valid() {
			t.Fatalf("expected valid config, got errors: %s", res.Summary())
		}
		if !res.HasWarnings() {
			t.Fatal("expected warning when workerOverrides specified with SNO preset, got none")
		}
		expectedWarning := "workerOverrides is ignored: the 'sno' preset has no worker nodes. Use 'controlPlaneOverrides' to customize the node."
		found := false
		for _, w := range res.Warnings {
			if w == expectedWarning {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning %q, got: %v", expectedWarning, res.Warnings)
		}
	})

	t.Run("WorkerOverrides with HA preset produces no SNO warning", func(t *testing.T) {
		c := validConfig()
		c.Preset = PresetHA
		c.WorkerOverrides = &NodeSettings{VCPUs: 4, MemoryMiB: 8192, DiskGiB: 100}
		res := ValidateStatic(c)
		if !res.Valid() {
			t.Fatalf("expected valid config, got errors: %s", res.Summary())
		}
		for _, w := range res.Warnings {
			if strings.Contains(w, "workerOverrides is ignored") {
				t.Errorf("unexpected warning for HA preset: %s", w)
			}
		}
	})
}

func TestValidateCapacity(t *testing.T) {
	keyPath := createTempSSHKey(t)
	cfg := &Config{
		ClusterName: "capacity-test",
		BaseDomain:  "example.com",
		OCPVersion:  "4.22.0",
		Preset:      PresetHA, // 16 vCPUs, 65536 MiB RAM (64 GiB), 500 GiB Disk
		HostSSH: HostSSHConfig{
			Host:          "host.lab",
			User:          "root",
			PublicKeyFile: keyPath,
		},
		Network: NetworkConfig{
			CIDR: "192.168.100.0/24",
		},
	}

	t.Run("Ample capacity", func(t *testing.T) {
		host := HostCapacity{
			VCPUs:            32,
			TotalRAMMiB:      131072, // 128 GiB
			AvailableRAMMiB:  98304,  // 96 GiB
			TotalDiskGiB:     2000,
			AvailableDiskGiB: 1200,
		}
		res := ValidateCapacity(cfg, host)
		if !res.Valid() {
			t.Fatalf("expected valid capacity, got: %s", res.Summary())
		}
		if res.HasWarnings() {
			t.Errorf("expected zero warnings, got: %s", res.Summary())
		}
	})

	t.Run("vCPU overcommit generates warning", func(t *testing.T) {
		host := HostCapacity{
			VCPUs:            8, // HA requires 16 vCPUs -> 2:1 overcommit
			TotalRAMMiB:      131072,
			AvailableRAMMiB:  98304,
			TotalDiskGiB:     2000,
			AvailableDiskGiB: 1200,
		}
		res := ValidateCapacity(cfg, host)
		if !res.Valid() {
			t.Fatalf("expected valid=true despite overcommit, got: %s", res.Summary())
		}
		if !res.HasWarnings() {
			t.Fatal("expected vCPU overcommit warning, got none")
		}
		if !strings.Contains(res.Warnings[0], "vCPU overcommit") {
			t.Errorf("expected warning about vCPU overcommit, got: %s", res.Warnings[0])
		}
	})

	t.Run("RAM exhaustion is hard error", func(t *testing.T) {
		host := HostCapacity{
			VCPUs:            32,
			TotalRAMMiB:      65536,
			AvailableRAMMiB:  32768, // only 32 GiB available, 64 GiB needed
			TotalDiskGiB:     2000,
			AvailableDiskGiB: 1200,
		}
		res := ValidateCapacity(cfg, host)
		if res.Valid() {
			t.Fatal("expected hard error for RAM exhaustion, but got valid=true")
		}
		if !strings.Contains(res.Errors[0], "insufficient Host RAM") {
			t.Errorf("expected error about RAM, got: %s", res.Errors[0])
		}
	})

	t.Run("Disk exhaustion is hard error", func(t *testing.T) {
		host := HostCapacity{
			VCPUs:            32,
			TotalRAMMiB:      131072,
			AvailableRAMMiB:  98304,
			TotalDiskGiB:     600,
			AvailableDiskGiB: 200, // only 200 GiB available, 500 GiB needed
		}
		res := ValidateCapacity(cfg, host)
		if res.Valid() {
			t.Fatal("expected hard error for Disk exhaustion, but got valid=true")
		}
		if !strings.Contains(res.Errors[0], "insufficient Host Disk") {
			t.Errorf("expected error about Disk, got: %s", res.Errors[0])
		}
	})
}

func networkTestConfig(t *testing.T, preset string) *Config {
	t.Helper()
	c := &Config{Preset: preset, Network: NetworkConfig{CIDR: DefaultCIDR}}
	gw, err := DeriveGatewayIP(c.Network.CIDR)
	if err != nil {
		t.Fatalf("derive gateway: %v", err)
	}
	c.Network.GatewayIP = gw
	if err := c.ResolveVIPs(); err != nil {
		t.Fatalf("resolve VIPs: %v", err)
	}
	return c
}

func TestResolveVIPs(t *testing.T) {
	c := &Config{Preset: PresetCompact, Network: NetworkConfig{CIDR: DefaultCIDR}}
	if err := c.ResolveVIPs(); err != nil {
		t.Fatalf("resolve VIPs: %v", err)
	}
	want := func(field, got, expected string) {
		t.Helper()
		if got != expected {
			t.Errorf("want %s %s, got %s", field, expected, got)
		}
	}
	want("APIVIP", c.Network.APIVIP, "192.168.100.5")
	want("IngressVIP", c.Network.IngressVIP, "192.168.100.6")
	want("IngressLBVIP", c.Network.IngressLBVIP, "192.168.100.7")

	// A new subnet must re-derive non-explicit VIPs.
	c.Network.CIDR = "10.20.0.0/24"
	if err := c.ResolveVIPs(); err != nil {
		t.Fatalf("resolve VIPs: %v", err)
	}
	want("APIVIP", c.Network.APIVIP, "10.20.0.5")
	want("IngressVIP", c.Network.IngressVIP, "10.20.0.6")
	want("IngressLBVIP", c.Network.IngressLBVIP, "10.20.0.7")

	// Explicit overrides win over derivation.
	c.Network.CustomAPIVIP = "10.20.0.9"
	c.Network.CustomIngressLBVIP = "10.20.0.10"
	if err := c.ResolveVIPs(); err != nil {
		t.Fatalf("resolve VIPs: %v", err)
	}
	want("APIVIP", c.Network.APIVIP, "10.20.0.9")
	want("IngressLBVIP", c.Network.IngressLBVIP, "10.20.0.10")
	want("IngressVIP", c.Network.IngressVIP, "10.20.0.6")
}

func TestUsesVIPs(t *testing.T) {
	tests := []struct {
		preset string
		want   bool
	}{
		{PresetSNO, false},
		{PresetCompact, true},
		{PresetHA, true},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			c := &Config{Preset: tt.preset}
			if got := c.UsesVIPs(); got != tt.want {
				t.Errorf("UsesVIPs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPOffset(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    uint32
		wantErr bool
	}{
		{"gateway", "192.168.100.1", 1, false},
		{"api vip", "192.168.100.5", 5, false},
		{"outside", "10.0.0.5", 0, true},
		{"invalid", "not-an-ip", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IPOffset(DefaultCIDR, tt.addr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got offset %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("offset = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateNetworkAddressing(t *testing.T) {
	tests := []struct {
		name        string
		preset      string
		mutate      func(*Config)
		wantErr     string
		wantWarning string
	}{
		{"compact defaults valid", PresetCompact, nil, "", ""},
		{"ha defaults valid", PresetHA, nil, "", ""},
		{"outside CIDR", PresetCompact, func(c *Config) { c.Network.APIVIP = "10.0.0.5" }, "outside CIDR", ""},
		{"gateway", PresetCompact, func(c *Config) { c.Network.APIVIP = "192.168.100.1" }, "gateway", ""},
		{"control-plane lease", PresetCompact, func(c *Config) { c.Network.APIVIP = "192.168.100.10" }, "control-plane", ""},
		{"worker lease in ha", PresetHA, func(c *Config) { c.Network.IngressLBVIP = "192.168.100.20" }, "worker", ""},
		{"dhcp start", PresetCompact, func(c *Config) { c.Network.IngressVIP = "192.168.100.100" }, "DHCP", ""},
		{"dhcp end", PresetCompact, func(c *Config) { c.Network.IngressVIP = "192.168.100.200" }, "DHCP", ""},
		{"before dhcp ok", PresetCompact, func(c *Config) { c.Network.IngressVIP = "192.168.100.99" }, "", ""},
		{"after dhcp ok", PresetCompact, func(c *Config) { c.Network.IngressVIP = "192.168.100.201" }, "", ""},
		{"duplicate VIPs", PresetCompact, func(c *Config) { c.Network.IngressVIP = c.Network.APIVIP }, "must differ", ""},
		{"network address", PresetCompact, func(c *Config) { c.Network.APIVIP = "192.168.100.0" }, "network address", ""},
		{"broadcast address", PresetCompact, func(c *Config) { c.Network.APIVIP = "192.168.100.255" }, "broadcast", ""},
		{"invalid IP", PresetCompact, func(c *Config) { c.Network.APIVIP = "not-an-ip" }, "not a valid IPv4", ""},
		{"sno ignores overrides", PresetSNO, func(c *Config) { c.Network.CustomAPIVIP = "192.168.100.5" }, "", "ignored for the 'sno' preset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := networkTestConfig(t, tt.preset)
			if tt.mutate != nil {
				tt.mutate(c)
			}
			res := ValidateNetworkAddressing(c)

			if tt.wantErr == "" {
				if !res.Valid() {
					t.Fatalf("expected valid, got: %s", res.Summary())
				}
			} else {
				if res.Valid() {
					t.Fatalf("expected error containing %q, got valid", tt.wantErr)
				}
				found := false
				for _, e := range res.Errors {
					if strings.Contains(e, tt.wantErr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, res.Errors)
				}
			}

			if tt.wantWarning != "" {
				found := false
				for _, w := range res.Warnings {
					if strings.Contains(w, tt.wantWarning) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q, got: %v", tt.wantWarning, res.Warnings)
				}
			}
		})
	}
}

func TestValidateStatic_DefersVIPChecksForAutoCIDR(t *testing.T) {
	keyPath := createTempSSHKey(t)
	yamlData := `
clusterName: "lab-auto"
baseDomain: "example.com"
ocpVersion: "4.22.0"
preset: "compact"
hostSsh:
  host: "192.168.1.100"
  user: "root"
  publicKeyFile: "` + keyPath + `"
network:
  apiVip: "10.0.0.5"
`
	cfg, err := LoadConfigFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if !cfg.Network.CIDRAuto {
		t.Fatal("expected CIDRAuto true when cidr is omitted")
	}

	res := ValidateStatic(cfg)
	if !res.Valid() {
		t.Fatalf("expected auto-CIDR VIP check to be deferred, got errors: %s", res.Summary())
	}
	if !res.HasWarnings() {
		t.Fatal("expected a warning about deferred VIP validation")
	}

	// Once the subnet is known, the out-of-CIDR override must fail.
	if err := cfg.ResolveVIPs(); err != nil {
		t.Fatalf("resolve VIPs: %v", err)
	}
	addrRes := ValidateNetworkAddressing(cfg)
	if addrRes.Valid() {
		t.Fatal("expected out-of-CIDR VIP to fail after subnet resolution")
	}
}
