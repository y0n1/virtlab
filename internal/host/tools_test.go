package host

import (
	"testing"
)

func TestNormalizeArchitecture(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"x86_64", "x86_64", false},
		{"amd64", "x86_64", false},
		{"AMD64", "x86_64", false},
		{"aarch64", "aarch64", false},
		{"arm64", "aarch64", false},
		{"ARM64", "aarch64", false},
		{"ppc64le", "ppc64le", false},
		{"s390x", "s390x", false},
		{"mips64", "", true},
		{"riscv64", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeArchitecture(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeArchitecture(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NormalizeArchitecture(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildMirrorURLs(t *testing.T) {
	tests := []struct {
		arch        string
		version     string
		wantInstall string
		wantClient  string
	}{
		{
			arch:        "x86_64",
			version:     "4.22.0",
			wantInstall: "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.22.0/openshift-install-linux.tar.gz",
			wantClient:  "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.22.0/openshift-client-linux.tar.gz",
		},
		{
			arch:        "aarch64",
			version:     "v4.21.2",
			wantInstall: "https://mirror.openshift.com/pub/openshift-v4/aarch64/clients/ocp/4.21.2/openshift-install-linux.tar.gz",
			wantClient:  "https://mirror.openshift.com/pub/openshift-v4/aarch64/clients/ocp/4.21.2/openshift-client-linux.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.arch+"-"+tt.version, func(t *testing.T) {
			installURL, clientURL := BuildMirrorURLs(tt.arch, tt.version)
			if installURL != tt.wantInstall {
				t.Errorf("installURL = %q, want %q", installURL, tt.wantInstall)
			}
			if clientURL != tt.wantClient {
				t.Errorf("clientURL = %q, want %q", clientURL, tt.wantClient)
			}
		})
	}
}
