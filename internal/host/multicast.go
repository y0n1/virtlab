package host

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// bridgeNameRegex constrains bridge device names before they are used in a
// sysfs path or passed to iproute2.
var bridgeNameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

// ValidateBridgeDeviceName rejects names that could escape the sysfs path or
// alter a shell command.
func ValidateBridgeDeviceName(name string) error {
	if !bridgeNameRegex.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid bridge device name %q", name)
	}
	return nil
}

// BuildDisableIGMPSnoopingScript returns the remote script that turns off IGMP
// snooping on a Linux bridge. Keepalived VRRP uses multicast (224.0.0.18) and
// the lab bridge has no multicast querier, so snooping can drop the adverts.
func BuildDisableIGMPSnoopingScript() string {
	return `set -euo pipefail
BR="$1"
SYSFS="/sys/class/net/${BR}/bridge/multicast_snooping"
if [ ! -e "$SYSFS" ]; then
  echo "SKIP: $SYSFS not present (not a Linux bridge?)"
  exit 0
fi
if [ "$(cat "$SYSFS")" != "0" ]; then
  echo 0 >"$SYSFS"
fi
echo "multicast_snooping=$(cat "$SYSFS")"
`
}

// DisableIGMPSnooping turns off IGMP snooping on the given bridge.
func (c *SSHClient) DisableIGMPSnooping(ctx context.Context, bridge string) error {
	if err := ValidateBridgeDeviceName(bridge); err != nil {
		return err
	}
	out, err := c.RunScriptContext(ctx, BuildDisableIGMPSnoopingScript(), bridge)
	if err != nil {
		return fmt.Errorf("disable IGMP snooping on bridge %q: %w", bridge, err)
	}
	fmt.Println(out)
	return nil
}
