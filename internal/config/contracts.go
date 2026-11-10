package config

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// DefaultRemotePullSecretPath is the standard remote file path where the pull secret
// is securely installed on the Host (mode 0600) and consumed by the cluster installer.
const DefaultRemotePullSecretPath = "/root/pull-secret.json"

// ValidatePullSecret verifies that data is non-empty, valid JSON, and contains an "auths" section.
func ValidatePullSecret(data []byte) error {
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

// ValidatePullSecretJSON is an alias for ValidatePullSecret.
func ValidatePullSecretJSON(data []byte) error {
	return ValidatePullSecret(data)
}

// DomainName returns the canonical libvirt guest domain name for a cluster node:
// <clusterName>-<nodeName>.
func DomainName(clusterName, nodeName string) string {
	return fmt.Sprintf("%s-%s", clusterName, nodeName)
}

// DomainName returns the canonical libvirt guest domain name for nodeName in this cluster.
func (c *Config) DomainName(nodeName string) string {
	return DomainName(c.ClusterName, nodeName)
}

// ClusterDomainPattern returns the regular expression pattern matching libvirt guest domains
// strictly belonging to clusterName (e.g. ^<clusterName>-(master|worker)-[0-9]+$).
func ClusterDomainPattern(clusterName string) string {
	return fmt.Sprintf(`^%s$`, DomainName(regexp.QuoteMeta(clusterName), `(master|worker)-[0-9]+`))
}

// ClusterDomainPattern returns the regular expression pattern matching libvirt guest domains
// strictly belonging to this cluster.
func (c *Config) ClusterDomainPattern() string {
	return ClusterDomainPattern(c.ClusterName)
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
