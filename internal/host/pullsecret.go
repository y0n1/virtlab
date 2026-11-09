package host

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// EnsurePullSecret places the pull secret bytes securely at /root/pull-secret.json (0600) on the Host.
func (c *SSHClient) EnsurePullSecret(secretBytes []byte) error {
	if len(secretBytes) == 0 {
		return fmt.Errorf("pull secret bytes cannot be empty")
	}

	// Validate JSON structure
	var parsed map[string]any
	if err := json.Unmarshal(secretBytes, &parsed); err != nil {
		return fmt.Errorf("pull secret is not valid JSON: %w", err)
	}
	if _, ok := parsed["auths"]; !ok {
		return fmt.Errorf("pull secret JSON does not contain 'auths' section")
	}

	// Write securely to Host at /root/pull-secret.json with 0600 permissions
	b64 := base64.StdEncoding.EncodeToString(secretBytes)
	script := fmt.Sprintf(`
set -e
printf '%s' | base64 -d | install -m 0600 -o root -g root /dev/stdin /root/pull-secret.json
test -s /root/pull-secret.json
`, b64)

	if _, err := c.RunScript(script); err != nil {
		return fmt.Errorf("failed to deploy /root/pull-secret.json on Host: %w", err)
	}

	fmt.Printf("✔ Pull secret securely placed at /root/pull-secret.json (0600) on Host\n")
	return nil
}
