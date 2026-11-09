package workstation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/y0n1/virtlab/internal/config"
)

const (
	// OCMTokenEnvVar supplies a Red Hat offline (refresh) token for
	// non-interactive OCM authentication. It is read only from the environment;
	// it is not a configuration key.
	OCMTokenEnvVar = "VIRTLAB_OCM_TOKEN"

	ocmProductionURL      = "https://api.openshift.com"
	ocmProductionTokenURL = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token"
	ocmClientID           = "cloud-services"
)

// ocmConfig is the subset of the ocm CLI configuration file needed to
// authenticate with an offline token.
type ocmConfig struct {
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	TokenURL     string `json:"token_url"`
	URL          string `json:"url"`
}

var (
	ocmCommandFn = func(env []string) ([]byte, error) {
		cmd := exec.Command("ocm", "post", "/api/accounts_mgmt/v1/access_token")
		cmd.Stdin = strings.NewReader("{}")
		cmd.Env = env
		return cmd.CombinedOutput()
	}
	readFileFn  = os.ReadFile
	lookupEnvFn = os.LookupEnv

	// Injectable hooks for the isolated ocm config lifecycle and interrupt
	// cleanup.
	mkdirTempFn    = os.MkdirTemp
	removeAllFn    = os.RemoveAll
	writeFileFn    = os.WriteFile
	chmodFn        = os.Chmod
	notifySignalFn = signal.Notify
	stopSignalFn   = signal.Stop
	resetSignalFn  = signal.Reset
	killProcessFn  = syscall.Kill
)

// ResolvePullSecret resolves and validates the OpenShift pull secret.
//
// Resolution is deterministic and mutually exclusive:
//  1. pullSecretFile from config.
//  2. VIRTLAB_OCM_TOKEN from the environment, used exclusively.
//  3. The ambient ocm CLI session.
//
// It verifies that the pull secret is valid JSON containing an "auths" key.
func ResolvePullSecret(cfg *config.Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// 1. Direct file path from config. This takes precedence over the token.
	if cfg.PullSecretFile != "" {
		p := config.ExpandPath(cfg.PullSecretFile)
		data, err := readFileFn(p)
		if err != nil {
			return nil, fmt.Errorf("read pullSecretFile '%s': %w", p, err)
		}
		secretBytes := bytes.TrimSpace(data)
		if err := validatePullSecretJSON(secretBytes); err != nil {
			return nil, fmt.Errorf("invalid pull secret in '%s': %w", p, err)
		}
		return secretBytes, nil
	}

	// 2. Explicit offline token. Never probe or fall back to the ambient session.
	if token, ok := lookupEnvFn(OCMTokenEnvVar); ok {
		return resolvePullSecretWithToken(token)
	}

	// 3. Automate via the ambient ocm CLI session.
	if ocmPath, err := lookPathFn("ocm"); err == nil {
		fmt.Printf("[Workstation] 'pullSecretFile' not specified; checking 'ocm' CLI authentication (%s)...\n", ocmPath)
		out, err := ocmCommandFn(nil)
		if err == nil {
			secretBytes := bytes.TrimSpace(out)
			if err := validatePullSecretJSON(secretBytes); err == nil {
				fmt.Printf("✔ Successfully retrieved pull secret via OCM\n")
				return secretBytes, nil
			}
			fmt.Printf("⚠ 'ocm' returned invalid pull secret JSON\n")
		} else {
			trimmed := strings.TrimSpace(string(out))
			if trimmed != "" {
				fmt.Printf("⚠ 'ocm' check failed: %s\n", trimmed)
			}
		}
	}

	// 4. Fallback error
	return nil, fmt.Errorf(
		"no pull secret found: 'pullSecretFile' is empty in config.yaml, and 'ocm' is not installed or logged in.\n" +
			"Please either:\n" +
			"  1. Set 'VIRTLAB_OCM_TOKEN' to a Red Hat offline token from https://console.redhat.com/openshift/token, OR\n" +
			"  2. Run 'ocm login' with your Red Hat offline token, OR\n" +
			"  3. Download your pull secret from https://console.redhat.com/openshift/install/pull-secret and set 'pullSecretFile: path/to/pull-secret.json' in config.yaml",
	)
}

// resolvePullSecretWithToken authenticates with an offline token through an
// isolated ocm config. It does not prompt and does not inspect the ambient
// session.
func resolvePullSecretWithToken(token string) ([]byte, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf(
			"'%s' is set but empty. Set it to a Red Hat offline token from https://console.redhat.com/openshift/token, or unset it to use the interactive 'ocm' session",
			OCMTokenEnvVar,
		)
	}

	if _, err := lookPathFn("ocm"); err != nil {
		return nil, fmt.Errorf(
			"'%s' is set, but the 'ocm' CLI is not installed or not found on PATH.\n"+
				"Install 'ocm', or unset '%s' to use the interactive 'ocm' session",
			OCMTokenEnvVar, OCMTokenEnvVar,
		)
	}

	configPath, cleanup, err := writePrivateOCMConfig(token)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	stopInterruptCleanup := cleanupOnInterrupt(cleanup)
	defer stopInterruptCleanup()

	fmt.Printf("[Workstation] '%s' is set; authenticating with 'ocm' non-interactively...\n", OCMTokenEnvVar)
	out, err := ocmCommandFn(ocmTokenEnv(configPath))
	if err != nil {
		detail := redactToken(strings.TrimSpace(string(out)), token)
		if detail == "" {
			detail = redactToken(strings.TrimSpace(err.Error()), token)
		}
		return nil, fmt.Errorf(
			"OCM authentication with '%s' failed: %s\n"+
				"Verify that the token is valid and not expired; create a new offline token at https://console.redhat.com/openshift/token",
			OCMTokenEnvVar, detail,
		)
	}

	secretBytes := bytes.TrimSpace(out)
	if err := validatePullSecretJSON(secretBytes); err != nil {
		return nil, fmt.Errorf("OCM authentication with '%s' returned an invalid pull secret: %w", OCMTokenEnvVar, err)
	}
	fmt.Printf("✔ Successfully retrieved pull secret via OCM\n")
	return secretBytes, nil
}

// writePrivateOCMConfig materializes an isolated ocm config in a private
// temporary directory. The directory is 0700 and the file is 0600. The token
// appears only in the file, never on a command line.
func writePrivateOCMConfig(token string) (path string, cleanup func(), err error) {
	dir, err := mkdirTempFn("", "virtlab-ocm-")
	if err != nil {
		return "", nil, fmt.Errorf("create private OCM config directory: %w", err)
	}

	var once sync.Once
	cleanup = func() {
		once.Do(func() { _ = removeAllFn(dir) })
	}

	if err := chmodFn(dir, 0o700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("set private OCM config directory permissions: %w", err)
	}

	data, err := json.Marshal(ocmConfig{
		RefreshToken: token,
		ClientID:     ocmClientID,
		TokenURL:     ocmProductionTokenURL,
		URL:          ocmProductionURL,
	})
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("encode isolated OCM config: %w", err)
	}

	configPath := filepath.Join(dir, "ocm.json")
	if err := writeFileFn(configPath, data, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write isolated OCM config: %w", err)
	}
	if err := chmodFn(configPath, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("set isolated OCM config permissions: %w", err)
	}

	return configPath, cleanup, nil
}

// ocmTokenEnv returns the environment for the token-authenticated child. It
// removes any inherited OCM_CONFIG and OCM_KEYRING so the isolated config is
// authoritative; a keyring would otherwise take precedence over it.
func ocmTokenEnv(configPath string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "OCM_CONFIG=") || strings.HasPrefix(kv, "OCM_KEYRING=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "OCM_CONFIG="+configPath)
}

// cleanupOnInterrupt removes the isolated ocm config when the process receives
// an interrupt or termination signal while the child runs, then re-delivers the
// signal so the process terminates as usual. The returned function stops the
// handler and must be deferred.
func cleanupOnInterrupt(cleanup func()) func() {
	sigCh := make(chan os.Signal, 1)
	notifySignalFn(sigCh, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigCh:
			cleanup()
			stopSignalFn(sigCh)
			if s, ok := sig.(syscall.Signal); ok {
				resetSignalFn(s)
				_ = killProcessFn(os.Getpid(), s)
			}
		case <-done:
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			stopSignalFn(sigCh)
			close(done)
		})
	}
}

// redactToken removes any occurrence of the offline token from child output or
// error text.
func redactToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "[REDACTED]")
}

func validatePullSecretJSON(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("pull secret data is empty")
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("pull secret is not valid JSON: %w", err)
	}
	if _, ok := parsed["auths"]; !ok {
		return fmt.Errorf("pull secret JSON does not contain 'auths' section")
	}
	return nil
}
