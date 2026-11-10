package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestDomainName(t *testing.T) {
	tests := []struct {
		cluster string
		node    string
		want    string
	}{
		{"lab", "master-0", "lab-master-0"},
		{"lab", "worker-1", "lab-worker-1"},
		{"my-cluster", "master-2", "my-cluster-master-2"},
	}

	for _, tt := range tests {
		got := DomainName(tt.cluster, tt.node)
		if got != tt.want {
			t.Errorf("DomainName(%q, %q) = %q; want %q", tt.cluster, tt.node, got, tt.want)
		}

		cfg := &Config{ClusterName: tt.cluster}
		if gotMethod := cfg.DomainName(tt.node); gotMethod != tt.want {
			t.Errorf("cfg.DomainName(%q) = %q; want %q", tt.node, gotMethod, tt.want)
		}
	}
}

func TestContracts_ClusterDomainPattern(t *testing.T) {
	tests := []struct {
		clusterName string
		wantPattern string
	}{
		{"lab", `^lab-(master|worker)-[0-9]+$`},
		{"test-lab", `^test-lab-(master|worker)-[0-9]+$`},
		{"special.cluster", `^special\.cluster-(master|worker)-[0-9]+$`},
	}

	for _, tt := range tests {
		t.Run(tt.clusterName, func(t *testing.T) {
			got := ClusterDomainPattern(tt.clusterName)
			if got != tt.wantPattern {
				t.Errorf("ClusterDomainPattern(%q) = %q; want %q", tt.clusterName, got, tt.wantPattern)
			}
			cfg := &Config{ClusterName: tt.clusterName}
			if gotMethod := cfg.ClusterDomainPattern(); gotMethod != tt.wantPattern {
				t.Errorf("cfg.ClusterDomainPattern() = %q; want %q", gotMethod, tt.wantPattern)
			}
		})
	}
}

func TestContracts_IsClusterGuestDomain(t *testing.T) {
	tests := []struct {
		cluster string
		domain  string
		want    bool
	}{
		{"lab", "lab-master-0", true},
		{"lab", "lab-worker-1", true},
		{"lab", "lab-master-99", true},
		{"lab", "other-master-0", false},
		{"lab", "lab-master-", false},
		{"lab", "lab-infra-0", false},
		{"", "lab-master-0", false},
		{"lab", "", false},
	}

	for _, tt := range tests {
		got := IsClusterGuestDomain(tt.cluster, tt.domain)
		if got != tt.want {
			t.Errorf("IsClusterGuestDomain(%q, %q) = %v; want %v", tt.cluster, tt.domain, got, tt.want)
		}
	}
}

func TestContracts_FilterClusterDomains(t *testing.T) {
	domains := []string{
		"lab-master-0",
		"lab-master-1",
		"lab-worker-0",
		"lab-2-master-0",
		"unrelated-vm",
	}

	got := FilterClusterDomains(domains, "lab")
	want := []string{"lab-master-0", "lab-master-1", "lab-worker-0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FilterClusterDomains() = %v; want %v", got, want)
	}

	if gotNil := FilterClusterDomains(nil, "lab"); gotNil != nil {
		t.Errorf("FilterClusterDomains(nil) = %v; want nil", gotNil)
	}
	if gotEmpty := FilterClusterDomains(domains, ""); gotEmpty != nil {
		t.Errorf("FilterClusterDomains('', empty) = %v; want nil", gotEmpty)
	}
}

func TestValidatePullSecret(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty data",
			data:        nil,
			wantErr:     true,
			errContains: "pull secret data is empty",
		},
		{
			name:        "invalid JSON",
			data:        []byte("not a json"),
			wantErr:     true,
			errContains: "pull secret is not valid JSON",
		},
		{
			name:        "missing auths section",
			data:        []byte(`{"other_key": "val"}`),
			wantErr:     true,
			errContains: "does not contain 'auths' section",
		},
		{
			name:    "valid pull secret",
			data:    []byte(`{"auths": {"cloud.openshift.com": {"auth": "dGVzdDp0ZXN0"}}}`),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePullSecret(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePullSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
			}

			errAlias := ValidatePullSecretJSON(tt.data)
			if (errAlias != nil) != tt.wantErr {
				t.Fatalf("ValidatePullSecretJSON() error = %v, wantErr %v", errAlias, tt.wantErr)
			}
		})
	}
}

func TestDefaultRemotePullSecretPath(t *testing.T) {
	if DefaultRemotePullSecretPath != "/root/pull-secret.json" {
		t.Errorf("DefaultRemotePullSecretPath = %q; want /root/pull-secret.json", DefaultRemotePullSecretPath)
	}
}
