package host

import (
	"encoding/base64"
	"fmt"

	"github.com/y0n1/virtlab/internal/config"
)

// DefaultRemotePullSecretPath defines the standard destination path for the pull secret on the Host.
const DefaultRemotePullSecretPath = config.DefaultRemotePullSecretPath

// EnsurePullSecret places the pull secret bytes securely at DefaultRemotePullSecretPath (0600) on the Host.
func (c *SSHClient) EnsurePullSecret(secretBytes []byte) error {
	if err := config.ValidatePullSecret(secretBytes); err != nil {
		return err
	}

	// Write securely to Host at DefaultRemotePullSecretPath with 0600 permissions
	b64 := base64.StdEncoding.EncodeToString(secretBytes)
	script := fmt.Sprintf(`
set -e
printf '%s' | base64 -d | install -m 0600 -o root -g root /dev/stdin %s
test -s %s
`, b64, DefaultRemotePullSecretPath, DefaultRemotePullSecretPath)

	if _, err := c.RunScript(script); err != nil {
		return fmt.Errorf("failed to deploy %s on Host: %w", DefaultRemotePullSecretPath, err)
	}

	fmt.Printf("✔ Pull secret securely placed at %s (0600) on Host\n", DefaultRemotePullSecretPath)
	return nil
}
