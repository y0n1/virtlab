package infrastructure

import (
	"slices"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func testClusterConfig(t *testing.T, preset string) *config.Config {
	t.Helper()
	cfg := &config.Config{
		ClusterName: "lab",
		BaseDomain:  "example.org",
		Preset:      preset,
		Network: config.NetworkConfig{
			CIDR: config.DefaultCIDR,
		},
	}
	gw, err := config.DeriveGatewayIP(cfg.Network.CIDR)
	if err != nil {
		t.Fatalf("DeriveGatewayIP() error = %v", err)
	}
	cfg.Network.GatewayIP = gw
	if err := cfg.ResolveVIPs(); err != nil {
		t.Fatalf("ResolveVIPs() error = %v", err)
	}
	return cfg
}

func mustLeasesAndTopology(t *testing.T, cfg *config.Config) ([]NodeLease, *config.NodeTopology) {
	t.Helper()
	leases, err := ComputeNodeLeases(cfg)
	if err != nil {
		t.Fatalf("ComputeNodeLeases() error = %v", err)
	}
	topo, err := cfg.EffectiveTopology()
	if err != nil {
		t.Fatalf("EffectiveTopology() error = %v", err)
	}
	return leases, topo
}

// TestBuildDNSHostRecords_APIEndpoint verifies that api/api-int resolve to the
// cluster API VIP for platform: baremetal presets, and to the single node for
// SNO (which keeps platform: none), while every node keeps its own records.
func TestBuildDNSHostRecords_APIEndpoint(t *testing.T) {
	tests := []struct {
		preset     string
		wantAPIIP  string
		wantNodes  int
		wantRecord int
	}{
		{config.PresetSNO, "192.168.100.10", 1, 2},
		{config.PresetCompact, "192.168.100.5", 3, 4},
		{config.PresetHA, "192.168.100.5", 5, 6},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			cfg := testClusterConfig(t, tt.preset)
			leases, topo := mustLeasesAndTopology(t, cfg)
			records := buildDNSHostRecords(cfg, leases, topo)

			var apiIPs []string
			for _, record := range records {
				hasAPI := slices.Contains(record.Hostnames, cfg.APIHost())
				hasAPIInt := slices.Contains(record.Hostnames, cfg.APIIntHost())
				if hasAPI != hasAPIInt {
					t.Errorf("record %s hosts %v: api and api-int must be registered together", record.IP, record.Hostnames)
				}
				if hasAPI {
					apiIPs = append(apiIPs, record.IP)
				}
			}

			if want := []string{tt.wantAPIIP}; !slices.Equal(apiIPs, want) {
				t.Errorf("api/api-int IPs = %v, want %v", apiIPs, want)
			}
			if len(records) != tt.wantRecord {
				t.Errorf("record count = %d, want %d", len(records), tt.wantRecord)
			}

			for _, lease := range leases {
				wantHostnames := []string{lease.Name + "." + cfg.ClusterFQDN(), lease.Name}
				if !hasDNSRecord(records, lease.IP, wantHostnames) {
					t.Errorf("missing per-node DNS record for %s (%s) with hostnames %v",
						lease.Name, lease.IP, wantHostnames)
				}
			}
		})
	}
}

func hasDNSRecord(records []dnsHostRecord, ip string, hostnames []string) bool {
	for _, record := range records {
		if record.IP == ip && slices.Equal(record.Hostnames, hostnames) {
			return true
		}
	}
	return false
}

// TestBuildDnsmasqOptions_Wildcard verifies that non-SNO presets point the
// wildcard at the HAProxy VIP (which fronts every ingress node), while SNO
// points it at its single node.
func TestBuildDnsmasqOptions_Wildcard(t *testing.T) {
	tests := []struct {
		preset string
		want   []string
	}{
		{config.PresetSNO, []string{
			"address=/apps.lab.example.org/192.168.100.10",
		}},
		{config.PresetCompact, []string{
			"address=/apps.lab.example.org/192.168.100.7",
		}},
		{config.PresetHA, []string{
			"address=/apps.lab.example.org/192.168.100.7",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			cfg := testClusterConfig(t, tt.preset)
			leases, topo := mustLeasesAndTopology(t, cfg)
			if got := buildDnsmasqOptions(cfg, leases, topo); !slices.Equal(got, tt.want) {
				t.Errorf("dnsmasq options = %v, want %v", got, tt.want)
			}
		})
	}
}
