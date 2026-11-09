package host

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/y0n1/virtlab/internal/config"
)

// ClusterDomainPattern returns the regular expression pattern matching libvirt guest domains
// strictly belonging to clusterName (e.g. ^<clusterName>-(master|worker)-[0-9]+$).
func ClusterDomainPattern(clusterName string) string {
	return fmt.Sprintf(`^%s-(master|worker)-[0-9]+$`, regexp.QuoteMeta(clusterName))
}

// IsClusterGuestDomain reports whether domain matches the expected guest domain naming convention
// for the given clusterName.
func IsClusterGuestDomain(clusterName, domain string) bool {
	if clusterName == "" || domain == "" {
		return false
	}
	re, err := regexp.Compile(ClusterDomainPattern(clusterName))
	if err != nil {
		return false
	}
	return re.MatchString(domain)
}

// FilterClusterDomains returns all domain names from the input slice that match the expected
// guest domain pattern for clusterName.
func FilterClusterDomains(domains []string, clusterName string) []string {
	if clusterName == "" || len(domains) == 0 {
		return nil
	}
	re, err := regexp.Compile(ClusterDomainPattern(clusterName))
	if err != nil {
		return nil
	}
	var matched []string
	for _, dom := range domains {
		if re.MatchString(dom) {
			matched = append(matched, dom)
		}
	}
	return matched
}

// TeardownContext cleans up cluster-specific resources on the Host hypervisor with context:
// - Undefines any lingering VMs for this cluster matching ClusterDomainPattern
// - Removes the cluster storage tree /home/libvirt/<clusterName>
// - Removes NetworkManager split-DNS configuration for this cluster
// Note: Virtual networks and storage pools are owned and torn down solely by Pulumi.
func (c *SSHClient) TeardownContext(ctx context.Context, cfg *config.Config) error {
	script, err := BuildTeardownScript(cfg)
	if err != nil {
		return err
	}

	fmt.Printf("\n[Host] Cleaning up Host resources for cluster '%s' (%s)...\n", cfg.ClusterName, c.Target())

	out, err := c.RunScriptContext(ctx, script)
	if err != nil {
		return fmt.Errorf("host teardown failed: %w", err)
	}
	fmt.Println(out)

	// The ingress load balancer is host-owned state outside the Pulumi stack;
	// remove it best-effort so a teardown never fails on a missing unit.
	if err := c.TeardownIngressLB(ctx, cfg.ClusterName); err != nil {
		fmt.Printf("⚠ Warning: failed to remove ingress load balancer: %v\n", err)
	}

	return nil
}

// Teardown cleans up cluster-specific resources on the Host hypervisor.
// Preserved for backward compatibility.
func (c *SSHClient) Teardown(cfg *config.Config) error {
	return c.TeardownContext(context.Background(), cfg)
}

// BuildTeardownScript generates the bash script executed during host teardown.
// It scopes VM teardown strictly to cluster guest domains, removes the cluster storage directory,
// and removes host-side split-DNS configs. Virtual networks and storage pools are omitted to respect
// Pulumi single-ownership.
func BuildTeardownScript(cfg *config.Config) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.ClusterName) == "" {
		return "", fmt.Errorf("host teardown: invalid or empty cluster name")
	}

	clusterName := cfg.ClusterName
	if strings.ToLower(clusterName) == "default" {
		return "", fmt.Errorf("host teardown: refusing to tear down 'default' cluster")
	}

	if cfg.BaseDomain != "" {
		if err := config.ValidateBaseDomain(cfg.BaseDomain); err != nil {
			return "", fmt.Errorf("host teardown: invalid baseDomain: %w", err)
		}
	}

	domainPattern := ClusterDomainPattern(clusterName)

	script := fmt.Sprintf(`
set -euo pipefail

echo "==> Checking for lingering VMs matching '%s'..."
for dom in $(virsh list --all --name 2>/dev/null | grep -E "%s" || true); do
    if [ -n "$dom" ]; then
        echo "Destroying and undefining domain $dom..."
        virsh destroy "$dom" 2>/dev/null || true
        virsh undefine "$dom" --remove-all-storage 2>/dev/null || true
    fi
done

echo "==> Removing cluster storage tree /home/libvirt/%s..."
if [ -d "/home/libvirt/%s" ]; then
    rm -rf "/home/libvirt/%s"
fi

echo "==> Removing Host split-DNS for zone '%s'..."
rm -f "/etc/NetworkManager/conf.d/00-%s-dnsmasq.conf" \
      "/etc/NetworkManager/dnsmasq.d/%s.conf"
systemctl reload NetworkManager 2>/dev/null || true

echo "==> Host cleanup complete"
`, domainPattern, domainPattern, clusterName, clusterName, clusterName, cfg.BaseDomain, clusterName, clusterName)

	return script, nil
}
