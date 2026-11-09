package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedSchemaIDMatchesSchemaURL(t *testing.T) {
	var header struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(configSchemaJSON, &header); err != nil {
		t.Fatalf("unmarshal embedded schema: %v", err)
	}
	if header.ID != configSchemaURL {
		t.Fatalf("schema $id %q does not match configSchemaURL %q", header.ID, configSchemaURL)
	}
}

func TestValidateSchema_AcceptsValidConfig(t *testing.T) {
	data := `
clusterName: "demo"
baseDomain: "example.com"
ocpVersion: "4.22.0"
preset: "compact"
hostSsh:
  host: "192.0.2.10"
  user: "root"
  publicKeyFile: "~/.ssh/id_rsa.pub"
network:
  cidr: "192.168.100.0/24"
  apiVip: "192.168.100.5"
  ingressVip: "192.168.100.6"
  ingressLbVip: "192.168.100.7"
controlPlaneOverrides:
  vcpus: 8
  memoryMib: 16384
  diskGib: 120
`
	if err := ValidateSchema([]byte(data)); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidateSchema_AcceptsExampleConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "config.example.yaml"))
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	if err := ValidateSchema(data); err != nil {
		t.Fatalf("shipped example config must satisfy the schema: %v", err)
	}
}

func TestValidateSchema_RejectsUnknownKey(t *testing.T) {
	data := `
clusterName: "demo"
ocpVersion: "4.22.0"
hostSsh:
  host: "192.0.2.10"
  publicKeyFile: "~/.ssh/id_rsa.pub"
unexpectedKey: true
`
	err := ValidateSchema([]byte(data))
	if err == nil {
		t.Fatal("expected schema error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "unexpectedKey") {
		t.Fatalf("expected error to name unexpectedKey, got: %v", err)
	}
}

func TestValidateSchema_RejectsWrongType(t *testing.T) {
	data := `
clusterName: "demo"
ocpVersion: "4.22.0"
hostSsh:
  host: "192.0.2.10"
  publicKeyFile: "~/.ssh/id_rsa.pub"
controlPlaneOverrides:
  vcpus: "eight"
`
	err := ValidateSchema([]byte(data))
	if err == nil {
		t.Fatal("expected schema error for string vcpus, got nil")
	}
	if !strings.Contains(err.Error(), "vcpus") {
		t.Fatalf("expected error to name vcpus, got: %v", err)
	}
}

func TestValidateSchema_RejectsGatewayIP(t *testing.T) {
	data := `
clusterName: "demo"
ocpVersion: "4.22.0"
hostSsh:
  host: "192.0.2.10"
  publicKeyFile: "~/.ssh/id_rsa.pub"
network:
  gatewayIp: "192.168.100.1"
`
	err := ValidateSchema([]byte(data))
	if err == nil {
		t.Fatal("expected schema error for gatewayIp, got nil")
	}
	if !strings.Contains(err.Error(), "gatewayIp") {
		t.Fatalf("expected error to name gatewayIp, got: %v", err)
	}
}

func TestLoadConfig_RejectsInvalidSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `
clusterName: "demo"
ocpVersion: "4.22.0"
hostSsh:
  host: "192.0.2.10"
  publicKeyFile: "~/.ssh/id_rsa.pub"
unsupportedField: 1
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected LoadConfig to reject an unknown field, got nil")
	} else if !strings.Contains(err.Error(), "unsupportedField") {
		t.Fatalf("expected error to name unsupportedField, got: %v", err)
	}
}
