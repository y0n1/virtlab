package workstation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/y0n1/virtlab/internal/config"
)

var (
	ocmCommandFn = func() ([]byte, error) {
		cmd := exec.Command("ocm", "post", "/api/accounts_mgmt/v1/access_token")
		cmd.Stdin = strings.NewReader("{}")
		return cmd.CombinedOutput()
	}
	readFileFn = os.ReadFile
)

// ResolvePullSecret resolves and validates the OpenShift pull secret from config or ocm CLI.
// It verifies that the pull secret is valid JSON containing an "auths" key.
func ResolvePullSecret(cfg *config.Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// 1. Direct file path from config
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

	// 2. Automate via ocm CLI
	if ocmPath, err := lookPathFn("ocm"); err == nil {
		fmt.Printf("[Workstation] 'pullSecretFile' not specified; checking 'ocm' CLI authentication (%s)...\n", ocmPath)
		out, err := ocmCommandFn()
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

	// 3. Fallback error
	return nil, fmt.Errorf(
		"no pull secret found: 'pullSecretFile' is empty in config.yaml, and 'ocm' is not installed or logged in.\n" +
			"Please either:\n" +
			"  1. Run 'ocm login' with your Red Hat offline token, OR\n" +
			"  2. Download your pull secret from https://console.redhat.com/openshift/install/pull-secret and set 'pullSecretFile: path/to/pull-secret.json' in config.yaml",
	)
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
