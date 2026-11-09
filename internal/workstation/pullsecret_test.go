package workstation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

func TestResolvePullSecret_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "pull-secret.json")
	validJSON := `{"auths":{"cloud.openshift.com":{"auth":"dGVzdDp0ZXN0","email":"user@example.com"}}}`
	if err := os.WriteFile(secretPath, []byte(validJSON), 0600); err != nil {
		t.Fatalf("failed to write test secret: %v", err)
	}

	cfg := &config.Config{
		PullSecretFile: secretPath,
	}

	data, err := ResolvePullSecret(cfg)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if string(data) != validJSON {
		t.Errorf("expected '%s', got '%s'", validJSON, string(data))
	}
}

func TestResolvePullSecret_MissingFile(t *testing.T) {
	cfg := &config.Config{
		PullSecretFile: "/non/existent/pull-secret.json",
	}

	_, err := ResolvePullSecret(cfg)
	if err == nil {
		t.Fatal("expected error for missing pullSecretFile, got nil")
	}
	if !strings.Contains(err.Error(), "read pullSecretFile") {
		t.Errorf("expected read error, got: %v", err)
	}
}

func TestResolvePullSecret_InvalidJSONFile(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "pull-secret.json")
	if err := os.WriteFile(secretPath, []byte("not-json"), 0600); err != nil {
		t.Fatalf("failed to write test secret: %v", err)
	}

	cfg := &config.Config{
		PullSecretFile: secretPath,
	}

	_, err := ResolvePullSecret(cfg)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "pull secret is not valid JSON") {
		t.Errorf("expected invalid JSON error, got: %v", err)
	}
}

func TestResolvePullSecret_MissingAuthsKey(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "pull-secret.json")
	if err := os.WriteFile(secretPath, []byte(`{"something_else": 123}`), 0600); err != nil {
		t.Fatalf("failed to write test secret: %v", err)
	}

	cfg := &config.Config{
		PullSecretFile: secretPath,
	}

	_, err := ResolvePullSecret(cfg)
	if err == nil {
		t.Fatal("expected error for missing auths key, got nil")
	}
	if !strings.Contains(err.Error(), "does not contain 'auths' section") {
		t.Errorf("expected missing auths error, got: %v", err)
	}
}

func TestResolvePullSecret_OCMSuccess(t *testing.T) {
	oldLookPath := lookPathFn
	oldOCMCommand := ocmCommandFn
	defer func() {
		lookPathFn = oldLookPath
		ocmCommandFn = oldOCMCommand
	}()

	lookPathFn = func(file string) (string, error) {
		if file == "ocm" {
			return "/usr/local/bin/ocm", nil
		}
		return "", errors.New("not found")
	}

	expectedJSON := `{"auths":{"cloud.openshift.com":{"auth":"b2NtOnRva2Vu"}}}`
	ocmCommandFn = func() ([]byte, error) {
		return []byte(expectedJSON), nil
	}

	cfg := &config.Config{
		PullSecretFile: "",
	}

	data, err := ResolvePullSecret(cfg)
	if err != nil {
		t.Fatalf("expected success via OCM, got error: %v", err)
	}
	if string(data) != expectedJSON {
		t.Errorf("expected '%s', got '%s'", expectedJSON, string(data))
	}
}

func TestResolvePullSecret_OCMNotLoggedIn(t *testing.T) {
	oldLookPath := lookPathFn
	oldOCMCommand := ocmCommandFn
	defer func() {
		lookPathFn = oldLookPath
		ocmCommandFn = oldOCMCommand
	}()

	lookPathFn = func(file string) (string, error) {
		if file == "ocm" {
			return "/usr/local/bin/ocm", nil
		}
		return "", errors.New("not found")
	}

	ocmCommandFn = func() ([]byte, error) {
		return nil, errors.New("exit status 1: Not logged in, run 'ocm login'")
	}

	cfg := &config.Config{
		PullSecretFile: "",
	}

	_, err := ResolvePullSecret(cfg)
	if err == nil {
		t.Fatal("expected error when OCM not logged in, got nil")
	}
	if !strings.Contains(err.Error(), "no pull secret found") || !strings.Contains(err.Error(), "ocm login") {
		t.Errorf("expected actionable fallback guidance, got: %v", err)
	}
}

func TestResolvePullSecret_OCMNotInstalled(t *testing.T) {
	oldLookPath := lookPathFn
	defer func() {
		lookPathFn = oldLookPath
	}()

	lookPathFn = func(file string) (string, error) {
		return "", errors.New("not found")
	}

	cfg := &config.Config{
		PullSecretFile: "",
	}

	_, err := ResolvePullSecret(cfg)
	if err == nil {
		t.Fatal("expected error when OCM is missing, got nil")
	}
	if !strings.Contains(err.Error(), "no pull secret found") {
		t.Errorf("expected actionable fallback guidance, got: %v", err)
	}
}

func TestCheckPrerequisites_FailFastOnMissingPullSecret(t *testing.T) {
	oldLookPath := lookPathFn
	oldCommandOutput := commandOutputFn
	oldOCMCommand := ocmCommandFn
	defer func() {
		lookPathFn = oldLookPath
		commandOutputFn = oldCommandOutput
		ocmCommandFn = oldOCMCommand
	}()

	lookPathFn = func(file string) (string, error) {
		switch file {
		case "ssh":
			return "/usr/bin/ssh", nil
		case "go":
			return "/usr/bin/go", nil
		case "sshuttle":
			return "/usr/bin/sshuttle", nil
		case "pulumi":
			return "/usr/bin/pulumi", nil
		case "ocm":
			return "/usr/bin/ocm", nil
		default:
			return "", errors.New("not found")
		}
	}

	commandOutputFn = func(name string, args ...string) ([]byte, error) {
		if name == "/usr/bin/ssh" {
			return []byte("OpenSSH_9.0p1, LibreSSL 3.3.6\n"), nil
		}
		if name == "/usr/bin/go" {
			return []byte("go version go1.22.4 darwin/arm64\n"), nil
		}
		return []byte(""), nil
	}

	// OCM returns failure (not logged in)
	ocmCommandFn = func() ([]byte, error) {
		return nil, errors.New("not logged in")
	}

	tmpDir := t.TempDir()
	ws := New(tmpDir, filepath.Join(tmpDir, "state.json"))

	cfg := &config.Config{
		ClusterName:    "test-lab",
		PullSecretFile: "",
	}

	err := ws.CheckPrerequisites(cfg)
	if err == nil {
		t.Fatal("expected CheckPrerequisites to fail fast on unresolvable pull secret, got nil")
	}
	if !strings.Contains(err.Error(), "no pull secret found") {
		t.Errorf("expected pull secret error, got: %v", err)
	}
}
