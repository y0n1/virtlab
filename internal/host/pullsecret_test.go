package host

import (
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func TestHost_EnsurePullSecret_Validation(t *testing.T) {
	client := NewSSHClient("root", "mock-host")

	tests := []struct {
		name        string
		data        []byte
		errContains string
	}{
		{
			name:        "empty secret bytes",
			data:        nil,
			errContains: "pull secret data is empty",
		},
		{
			name:        "invalid JSON",
			data:        []byte("not json"),
			errContains: "pull secret is not valid JSON",
		},
		{
			name:        "missing auths",
			data:        []byte(`{"key": "value"}`),
			errContains: "does not contain 'auths' section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.EnsurePullSecret(tt.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestHost_DefaultRemotePullSecretPath(t *testing.T) {
	if DefaultRemotePullSecretPath != config.DefaultRemotePullSecretPath {
		t.Errorf("DefaultRemotePullSecretPath = %q; want %q", DefaultRemotePullSecretPath, config.DefaultRemotePullSecretPath)
	}
}
