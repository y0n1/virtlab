package host

import (
	"reflect"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func TestHost_Teardown_Validation(t *testing.T) {
	client := NewSSHClient("root", "mock-host")

	t.Run("nil config", func(t *testing.T) {
		err := client.Teardown(nil)
		if err == nil {
			t.Fatal("expected error with nil config")
		}
	})

	t.Run("empty clusterName", func(t *testing.T) {
		err := client.Teardown(&config.Config{ClusterName: ""})
		if err == nil {
			t.Fatal("expected error with empty clusterName")
		}
	})

	t.Run("reject default clusterName", func(t *testing.T) {
		err := client.Teardown(&config.Config{ClusterName: "default"})
		if err == nil {
			t.Fatal("expected error with 'default' clusterName")
		}
	})
}

func TestClusterDomainPattern(t *testing.T) {
	tests := []struct {
		clusterName     string
		expectedPattern string
	}{
		{
			clusterName:     "lab",
			expectedPattern: `^lab-(master|worker)-[0-9]+$`,
		},
		{
			clusterName:     "test-lab",
			expectedPattern: `^test-lab-(master|worker)-[0-9]+$`,
		},
		{
			clusterName:     "special.cluster",
			expectedPattern: `^special\.cluster-(master|worker)-[0-9]+$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.clusterName, func(t *testing.T) {
			got := ClusterDomainPattern(tt.clusterName)
			if got != tt.expectedPattern {
				t.Errorf("ClusterDomainPattern(%q) = %q; want %q", tt.clusterName, got, tt.expectedPattern)
			}
		})
	}
}

func TestIsClusterGuestDomain(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		domain      string
		wantMatch   bool
	}{
		// Positive matches
		{
			name:        "lab master-0",
			clusterName: "lab",
			domain:      "lab-master-0",
			wantMatch:   true,
		},
		{
			name:        "lab worker-0",
			clusterName: "lab",
			domain:      "lab-worker-0",
			wantMatch:   true,
		},
		{
			name:        "lab master-1",
			clusterName: "lab",
			domain:      "lab-master-1",
			wantMatch:   true,
		},
		{
			name:        "lab worker-10 multi-digit index",
			clusterName: "lab",
			domain:      "lab-worker-10",
			wantMatch:   true,
		},
		{
			name:        "hyphenated cluster test-lab master-0",
			clusterName: "test-lab",
			domain:      "test-lab-master-0",
			wantMatch:   true,
		},
		{
			name:        "hyphenated cluster test-lab worker-0",
			clusterName: "test-lab",
			domain:      "test-lab-worker-0",
			wantMatch:   true,
		},
		// Negative matches (REJECT)
		{
			name:        "lab rejects lab-2-master-0 (colliding prefix with distinct cluster)",
			clusterName: "lab",
			domain:      "lab-2-master-0",
			wantMatch:   false,
		},
		{
			name:        "lab rejects lab-prod-worker-1 (foreign cluster environment)",
			clusterName: "lab",
			domain:      "lab-prod-worker-1",
			wantMatch:   false,
		},
		{
			name:        "lab rejects lab-bastion (auxiliary VM)",
			clusterName: "lab",
			domain:      "lab-bastion",
			wantMatch:   false,
		},
		{
			name:        "lab rejects lab-storage-appliance (auxiliary appliance)",
			clusterName: "lab",
			domain:      "lab-storage-appliance",
			wantMatch:   false,
		},
		{
			name:        "lab rejects labrador-master-0 (substring prefix match)",
			clusterName: "lab",
			domain:      "labrador-master-0",
			wantMatch:   false,
		},
		{
			name:        "lab rejects missing index lab-master-",
			clusterName: "lab",
			domain:      "lab-master-",
			wantMatch:   false,
		},
		{
			name:        "lab rejects non-numeric index lab-master-x",
			clusterName: "lab",
			domain:      "lab-master-x",
			wantMatch:   false,
		},
		{
			name:        "lab rejects foreign role lab-infra-0",
			clusterName: "lab",
			domain:      "lab-infra-0",
			wantMatch:   false,
		},
		{
			name:        "lab rejects un-prefixed master-0",
			clusterName: "lab",
			domain:      "master-0",
			wantMatch:   false,
		},
		{
			name:        "empty clusterName",
			clusterName: "",
			domain:      "lab-master-0",
			wantMatch:   false,
		},
		{
			name:        "empty domain",
			clusterName: "lab",
			domain:      "",
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsClusterGuestDomain(tt.clusterName, tt.domain)
			if got != tt.wantMatch {
				t.Errorf("IsClusterGuestDomain(%q, %q) = %v; want %v", tt.clusterName, tt.domain, got, tt.wantMatch)
			}
		})
	}
}

func TestFilterClusterDomains(t *testing.T) {
	domains := []string{
		"lab-master-0",
		"lab-2-master-0",
		"lab-worker-0",
		"lab-prod-worker-1",
		"lab-bastion",
		"lab-storage-appliance",
		"labrador-master-0",
		"lab-master-1",
		"other-master-0",
	}

	t.Run("filter for lab", func(t *testing.T) {
		got := FilterClusterDomains(domains, "lab")
		want := []string{
			"lab-master-0",
			"lab-worker-0",
			"lab-master-1",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FilterClusterDomains() = %v; want %v", got, want)
		}
	})

	t.Run("filter for lab-2", func(t *testing.T) {
		got := FilterClusterDomains(domains, "lab-2")
		want := []string{
			"lab-2-master-0",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FilterClusterDomains() = %v; want %v", got, want)
		}
	})

	t.Run("filter for non-existent cluster", func(t *testing.T) {
		got := FilterClusterDomains(domains, "non-existent")
		if len(got) != 0 {
			t.Errorf("FilterClusterDomains() = %v; want empty", got)
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		if got := FilterClusterDomains(nil, "lab"); got != nil {
			t.Errorf("FilterClusterDomains(nil) = %v; want nil", got)
		}
		if got := FilterClusterDomains(domains, ""); got != nil {
			t.Errorf("FilterClusterDomains('', empty cluster) = %v; want nil", got)
		}
	})
}

func TestBuildTeardownScript_PulumiSingleOwnership(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "mylab",
		BaseDomain:  "example.com",
	}

	script, err := BuildTeardownScript(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must NOT contain raw virsh net or virsh pool commands (Pulumi owns them)
	prohibitedSubstrings := []string{
		"virsh net-destroy",
		"virsh net-undefine",
		"virsh pool-destroy",
		"virsh pool-undefine",
		"virsh net-info",
		"virsh pool-info",
	}
	for _, sub := range prohibitedSubstrings {
		if strings.Contains(script, sub) {
			t.Errorf("script contains prohibited virsh command %q (should be owned exclusively by Pulumi)", sub)
		}
	}

	// Must contain domain pattern, storage tree removal, and NetworkManager cleanup
	requiredSubstrings := []string{
		`^mylab-(master|worker)-[0-9]+$`,
		`rm -rf "/home/libvirt/mylab"`,
		`rm -f "/etc/NetworkManager/conf.d/00-mylab-dnsmasq.conf"`,
		`"/etc/NetworkManager/dnsmasq.d/mylab.conf"`,
	}
	for _, req := range requiredSubstrings {
		if !strings.Contains(script, req) {
			t.Errorf("script missing required element %q", req)
		}
	}
}
