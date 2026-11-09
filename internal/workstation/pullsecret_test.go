package workstation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/y0n1/virtlab/internal/config"
)

const validPullSecretJSON = `{"auths":{"cloud.openshift.com":{"auth":"dGVzdDp0ZXN0","email":"user@example.com"}}}`

func savePullSecretHooks(t *testing.T) {
	t.Helper()
	oldLookPath := lookPathFn
	oldOCMCommand := ocmCommandFn
	oldLookupEnv := lookupEnvFn
	t.Cleanup(func() {
		lookPathFn = oldLookPath
		ocmCommandFn = oldOCMCommand
		lookupEnvFn = oldLookupEnv
	})
}

// withoutOCMToken isolates a test from any VIRTLAB_OCM_TOKEN in the host
// environment.
func withoutOCMToken(t *testing.T) {
	t.Helper()
	savePullSecretHooks(t)
	lookupEnvFn = func(string) (string, bool) { return "", false }
}

func withOCMToken(t *testing.T, token string) {
	t.Helper()
	savePullSecretHooks(t)
	lookupEnvFn = func(key string) (string, bool) {
		if key == OCMTokenEnvVar {
			return token, true
		}
		return "", false
	}
}

func ocmOnPath(t *testing.T) {
	t.Helper()
	lookPathFn = func(string) (string, error) { return "/usr/local/bin/ocm", nil }
}

func envValue(env []string, prefix string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

func countEnvPrefix(env []string, prefix string) int {
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			n++
		}
	}
	return n
}

func TestResolvePullSecret_ValidFile(t *testing.T) {
	withoutOCMToken(t)

	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "pull-secret.json")
	if err := os.WriteFile(secretPath, []byte(validPullSecretJSON), 0600); err != nil {
		t.Fatalf("failed to write test secret: %v", err)
	}

	cfg := &config.Config{
		PullSecretFile: secretPath,
	}

	data, err := ResolvePullSecret(cfg)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if string(data) != validPullSecretJSON {
		t.Errorf("expected '%s', got '%s'", validPullSecretJSON, string(data))
	}
}

func TestResolvePullSecret_MissingFile(t *testing.T) {
	withoutOCMToken(t)

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
	withoutOCMToken(t)

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
	withoutOCMToken(t)

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
	withoutOCMToken(t)
	ocmOnPath(t)

	ocmCommandFn = func(env []string) ([]byte, error) {
		if env != nil {
			t.Errorf("ambient session must inherit the environment, got %v", env)
		}
		return []byte(validPullSecretJSON), nil
	}

	cfg := &config.Config{
		PullSecretFile: "",
	}

	data, err := ResolvePullSecret(cfg)
	if err != nil {
		t.Fatalf("expected success via OCM, got error: %v", err)
	}
	if string(data) != validPullSecretJSON {
		t.Errorf("expected '%s', got '%s'", validPullSecretJSON, string(data))
	}
}

func TestResolvePullSecret_OCMNotLoggedIn(t *testing.T) {
	withoutOCMToken(t)
	ocmOnPath(t)

	ocmCommandFn = func([]string) ([]byte, error) {
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
	withoutOCMToken(t)

	lookPathFn = func(string) (string, error) {
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

func TestResolvePullSecret_TokenSuccess(t *testing.T) {
	const token = "offline-token-abc"
	withOCMToken(t, token)
	ocmOnPath(t)

	var configPath string
	ocmCommandFn = func(env []string) ([]byte, error) {
		configPath = envValue(env, "OCM_CONFIG=")
		if configPath == "" {
			t.Fatal("OCM_CONFIG is not set for the child process")
		}
		if countEnvPrefix(env, "OCM_KEYRING=") != 0 {
			t.Errorf("OCM_KEYRING must be cleared for the child, got %v", env)
		}
		if strings.Contains(strings.Join(env, "\n"), token) {
			t.Error("token leaked into the child environment")
		}

		dirInfo, err := os.Stat(filepath.Dir(configPath))
		if err != nil {
			t.Fatalf("stat isolated config directory: %v", err)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("expected config directory mode 0700, got %#o", perm)
		}

		fileInfo, err := os.Stat(configPath)
		if err != nil {
			t.Fatalf("stat isolated config file: %v", err)
		}
		if perm := fileInfo.Mode().Perm(); perm != 0o600 {
			t.Errorf("expected config file mode 0600, got %#o", perm)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read isolated config file: %v", err)
		}
		var parsed ocmConfig
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("isolated config is not valid JSON: %v", err)
		}
		if parsed.RefreshToken != token {
			t.Errorf("expected refresh_token %q, got %q", token, parsed.RefreshToken)
		}
		if parsed.ClientID != ocmClientID {
			t.Errorf("expected client_id %q, got %q", ocmClientID, parsed.ClientID)
		}
		if parsed.TokenURL != ocmProductionTokenURL {
			t.Errorf("expected token_url %q, got %q", ocmProductionTokenURL, parsed.TokenURL)
		}
		if parsed.URL != ocmProductionURL {
			t.Errorf("expected url %q, got %q", ocmProductionURL, parsed.URL)
		}

		return []byte(validPullSecretJSON), nil
	}

	data, err := ResolvePullSecret(&config.Config{})
	if err != nil {
		t.Fatalf("expected success via token, got error: %v", err)
	}
	if string(data) != validPullSecretJSON {
		t.Errorf("expected '%s', got '%s'", validPullSecretJSON, string(data))
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("temporary OCM config was not removed after success: %v", err)
	}
}

func TestResolvePullSecret_TokenInvalid(t *testing.T) {
	const token = "expired-token-xyz"
	withOCMToken(t, token)
	ocmOnPath(t)

	ocmCommandFn = func([]string) ([]byte, error) {
		return []byte("error: token " + token + " is invalid"), errors.New("exit status 1")
	}

	_, err := ResolvePullSecret(&config.Config{})
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
	msg := err.Error()
	for _, want := range []string{OCMTokenEnvVar, "valid and not expired", "[REDACTED]"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to contain %q, got: %v", want, err)
		}
	}
	if strings.Contains(msg, token) {
		t.Errorf("token leaked into the error message: %v", err)
	}
}

func TestResolvePullSecret_TokenMissingOCM(t *testing.T) {
	withOCMToken(t, "some-token")

	lookPathFn = func(string) (string, error) {
		return "", errors.New("not found")
	}
	called := false
	ocmCommandFn = func([]string) ([]byte, error) {
		called = true
		return nil, nil
	}

	_, err := ResolvePullSecret(&config.Config{})
	if err == nil {
		t.Fatal("expected error when ocm is missing, got nil")
	}
	if !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), OCMTokenEnvVar) {
		t.Errorf("expected actionable missing-ocm error, got: %v", err)
	}
	if called {
		t.Error("must not run ocm when the binary is missing")
	}
}

func TestResolvePullSecret_TokenCleanupOnFailure(t *testing.T) {
	withOCMToken(t, "some-token")
	ocmOnPath(t)

	var configPath string
	ocmCommandFn = func(env []string) ([]byte, error) {
		configPath = envValue(env, "OCM_CONFIG=")
		return nil, errors.New("authentication failed")
	}

	if _, err := ResolvePullSecret(&config.Config{}); err == nil {
		t.Fatal("expected error, got nil")
	}
	if configPath == "" {
		t.Fatal("OCM_CONFIG was not passed to the child")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("temporary OCM config was not removed after failure: %v", err)
	}
}

func TestResolvePullSecret_TokenCleanupOnInterrupt(t *testing.T) {
	withOCMToken(t, "some-token")
	ocmOnPath(t)

	oldNotify := notifySignalFn
	oldStop := stopSignalFn
	oldReset := resetSignalFn
	oldKill := killProcessFn
	t.Cleanup(func() {
		notifySignalFn = oldNotify
		stopSignalFn = oldStop
		resetSignalFn = oldReset
		killProcessFn = oldKill
	})

	var sigCh chan<- os.Signal
	notifySignalFn = func(c chan<- os.Signal, _ ...os.Signal) {
		sigCh = c
	}
	stopSignalFn = func(chan<- os.Signal) {}

	var resetCalled, killCalled atomic.Bool
	resetSignalFn = func(...os.Signal) { resetCalled.Store(true) }
	interrupted := make(chan struct{})
	killProcessFn = func(int, syscall.Signal) error {
		killCalled.Store(true)
		close(interrupted)
		return nil
	}

	var configPath string
	ocmCommandFn = func(env []string) ([]byte, error) {
		configPath = envValue(env, "OCM_CONFIG=")
		if configPath == "" {
			t.Fatal("OCM_CONFIG is not set for the child process")
		}
		sigCh <- os.Interrupt
		select {
		case <-interrupted:
		case <-time.After(5 * time.Second):
			t.Error("interrupt handler did not re-deliver the signal")
		}
		return []byte(validPullSecretJSON), nil
	}

	if _, err := ResolvePullSecret(&config.Config{}); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !resetCalled.Load() {
		t.Error("expected signal disposition to be reset before re-delivery")
	}
	if !killCalled.Load() {
		t.Error("expected the interrupt to be re-delivered to the process")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("temporary OCM config was not removed after interrupt: %v", err)
	}
}

func TestResolvePullSecret_TokenPrecedenceOverAmbientSession(t *testing.T) {
	withOCMToken(t, "some-token")
	ocmOnPath(t)

	var isolatedConfig bool
	ocmCommandFn = func(env []string) ([]byte, error) {
		if env == nil {
			t.Fatal("token flow must not use the ambient environment")
		}
		isolatedConfig = envValue(env, "OCM_CONFIG=") != ""
		return []byte(validPullSecretJSON), nil
	}

	if _, err := ResolvePullSecret(&config.Config{}); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !isolatedConfig {
		t.Error("expected the token flow to use an isolated OCM config")
	}
}

func TestResolvePullSecret_FileTakesPrecedenceOverToken(t *testing.T) {
	withOCMToken(t, "some-token")
	// Neither the token nor the ocm CLI may be consulted.
	lookupEnvFn = func(string) (string, bool) {
		t.Fatal("must not read the token environment when pullSecretFile is set")
		return "", false
	}
	lookPathFn = func(string) (string, error) {
		t.Fatal("must not look for ocm when pullSecretFile is set")
		return "", nil
	}
	ocmCommandFn = func([]string) ([]byte, error) {
		t.Fatal("must not run ocm when pullSecretFile is set")
		return nil, nil
	}

	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "pull-secret.json")
	if err := os.WriteFile(secretPath, []byte(validPullSecretJSON), 0600); err != nil {
		t.Fatalf("failed to write test secret: %v", err)
	}

	data, err := ResolvePullSecret(&config.Config{PullSecretFile: secretPath})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if string(data) != validPullSecretJSON {
		t.Errorf("expected '%s', got '%s'", validPullSecretJSON, string(data))
	}
}

func TestResolvePullSecret_TokenEmpty(t *testing.T) {
	withOCMToken(t, "")

	called := false
	ocmCommandFn = func([]string) ([]byte, error) {
		called = true
		return nil, nil
	}

	_, err := ResolvePullSecret(&config.Config{})
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "set but empty") {
		t.Errorf("expected empty-token error, got: %v", err)
	}
	if called {
		t.Error("must not run ocm when the token is empty")
	}
}

func TestOCMTokenEnv_ClearsConflictingVariables(t *testing.T) {
	t.Setenv("OCM_KEYRING", "keychain")
	t.Setenv("OCM_CONFIG", "/inherited/ocm.json")

	env := ocmTokenEnv("/isolated/ocm.json")

	if got := envValue(env, "OCM_CONFIG="); got != "/isolated/ocm.json" {
		t.Errorf("expected isolated OCM_CONFIG, got %q", got)
	}
	if n := countEnvPrefix(env, "OCM_CONFIG="); n != 1 {
		t.Errorf("expected exactly one OCM_CONFIG entry, got %d", n)
	}
	if n := countEnvPrefix(env, "OCM_KEYRING="); n != 0 {
		t.Errorf("expected OCM_KEYRING to be cleared, got %d entries", n)
	}
}

func TestRedactToken(t *testing.T) {
	const token = "secret-token"
	got := redactToken("token="+token+" rejected", token)
	if strings.Contains(got, token) {
		t.Errorf("token was not redacted: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected redaction marker, got %q", got)
	}
}

func TestCheckPrerequisites_FailFastOnMissingPullSecret(t *testing.T) {
	oldLookPath := lookPathFn
	oldCommandOutput := commandOutputFn
	oldOCMCommand := ocmCommandFn
	oldLookupEnv := lookupEnvFn
	defer func() {
		lookPathFn = oldLookPath
		commandOutputFn = oldCommandOutput
		ocmCommandFn = oldOCMCommand
		lookupEnvFn = oldLookupEnv
	}()

	lookupEnvFn = func(string) (string, bool) { return "", false }

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
	ocmCommandFn = func([]string) ([]byte, error) {
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
