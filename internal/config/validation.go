package config

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/blang/semver"
)

// SupportedOpenShiftYStreams lists the supported Y-stream major.minor versions.
var SupportedOpenShiftYStreams = []string{"4.20", "4.21", "4.22"}

var (
	rfc1123Regex     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	networkNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	allNumericRegex  = regexp.MustCompile(`^[0-9]+$`)
)

// HostCapacity represents the hardware resource envelope reported by the remote Host hypervisor.
type HostCapacity struct {
	VCPUs            int // Total physical/virtual CPUs on Host
	TotalRAMMiB      int // Total Host RAM in MiB
	AvailableRAMMiB  int // Free/available Host RAM in MiB
	TotalDiskGiB     int // Total capacity of /home/libvirt filesystem in GiB
	AvailableDiskGiB int // Free space available in /home/libvirt in GiB
}

// ValidationResult captures the outcome of domain validation, distinguishing hard blockers from warnings.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// Valid returns true if there are zero hard blocking errors.
func (r *ValidationResult) Valid() bool {
	return len(r.Errors) == 0
}

// HasWarnings returns true if any soft warnings were recorded.
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// AddError records a hard validation blocker.
func (r *ValidationResult) AddError(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

// AddWarning records an informational or overcommit warning.
func (r *ValidationResult) AddWarning(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Merge appends another result's errors and warnings into this one.
func (r *ValidationResult) Merge(other *ValidationResult) {
	if other == nil {
		return
	}
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// Summary returns a formatted multi-line summary of errors and warnings.
func (r *ValidationResult) Summary() string {
	var b strings.Builder
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Errors (%d):\n", len(r.Errors))
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "  ✖ %s\n", e)
		}
	}
	if len(r.Warnings) > 0 {
		if len(r.Errors) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Warnings (%d):\n", len(r.Warnings))
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "  ⚠ %s\n", w)
		}
	}
	return b.String()
}

// ValidateStatic performs syntactic, structural, and local file validation on the Config.
func ValidateStatic(c *Config) *ValidationResult {
	res := &ValidationResult{}

	// 1. Cluster name
	name := strings.TrimSpace(c.ClusterName)
	if name == "" {
		res.AddError("clusterName is required")
	} else if len(name) > 32 {
		res.AddError("clusterName '%s' exceeds 32 characters", name)
	} else if !rfc1123Regex.MatchString(name) {
		res.AddError("clusterName '%s' must be lower-case alphanumeric with optional hyphens (RFC 1123)", name)
	}

	// 2. Base domain
	if err := ValidateBaseDomain(c.BaseDomain); err != nil {
		res.AddError("%v", err)
	}

	// 3. OpenShift version (last 3 Y-streams)
	ver := strings.TrimSpace(c.OCPVersion)
	if ver == "" {
		res.AddError("ocpVersion is required")
	} else {
		sv, err := semver.ParseTolerant(ver)
		if err != nil {
			res.AddError("ocpVersion '%s' is not a valid semantic version (expected MAJOR.MINOR.PATCH, e.g. '4.22.0'): %v", ver, err)
		} else {
			stream := fmt.Sprintf("%d.%d", sv.Major, sv.Minor)
			validStream := false
			for _, y := range SupportedOpenShiftYStreams {
				if stream == y {
					validStream = true
					break
				}
			}
			if !validStream {
				res.AddError("ocpVersion '%s' (Y-stream %s) is not supported; must be in the last 3 supported Y-streams: %s",
					ver, stream, strings.Join(SupportedOpenShiftYStreams, ", "))
			}
		}
	}

	// 4. Host SSH configuration
	if strings.TrimSpace(c.HostSSH.Host) == "" {
		res.AddError("hostSsh.host is required")
	}
	if strings.TrimSpace(c.HostSSH.User) == "" {
		res.AddError("hostSsh.user is required")
	}

	// 5. Host SSH Public Key File (SHOW-STOPPER IF MISSING OR UNREADABLE)
	keyPath := c.ResolvedPublicKeyPath()
	if strings.TrimSpace(c.HostSSH.PublicKeyFile) == "" {
		res.AddError("hostSsh.publicKeyFile is required (path to public key on Workstation; e.g. '~/.ssh/id_ed25519.pub')")
	} else {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			res.AddError("hostSsh.publicKeyFile '%s' cannot be read: %v", keyPath, err)
		} else {
			trimmed := strings.TrimSpace(string(data))
			if trimmed == "" {
				res.AddError("hostSsh.publicKeyFile '%s' is empty", keyPath)
			} else if !isLikelySSHPublicKey(trimmed) {
				res.AddError("hostSsh.publicKeyFile '%s' does not appear to be a valid OpenSSH public key", keyPath)
			}
		}
	}

	// 6. Pull secret file (optional, but if specified must exist)
	if c.PullSecretFile != "" {
		p := ExpandPath(c.PullSecretFile)
		if _, err := os.Stat(p); err != nil {
			res.AddError("pullSecretFile '%s' not found or unreadable: %v", p, err)
		}
	}

	// 7. Preset
	preset := strings.ToLower(strings.TrimSpace(c.Preset))
	if preset != PresetSNO && preset != PresetCompact && preset != PresetHA {
		res.AddError("preset '%s' is invalid; choose from 'sno', 'compact', 'ha'", c.Preset)
	}
	if preset == PresetSNO && c.WorkerOverrides != nil {
		res.AddWarning("workerOverrides is ignored: the 'sno' preset has no worker nodes. Use 'controlPlaneOverrides' to customize the node.")
	}

	// 8. Network configuration
	if c.Network.Name != "" {
		if err := ValidateNetworkName(c.Network.Name); err != nil {
			res.AddError("%v", err)
		}
	} else if strings.TrimSpace(c.ClusterName) == "" {
		res.AddError("network.name is required when clusterName is not specified")
	}

	if c.Network.CustomGatewayIP != "" {
		res.AddError("network.gatewayIp '%s' is not allowed; gateway IP is inferred automatically from network.cidr", c.Network.CustomGatewayIP)
	}

	if c.Network.CIDR != "" {
		_, ipNet, err := net.ParseCIDR(c.Network.CIDR)
		if err != nil {
			res.AddError("network.cidr '%s' is invalid: %v", c.Network.CIDR, err)
		} else if ipNet.IP.To4() == nil {
			res.AddError("network.cidr '%s' must be an IPv4 subnet", c.Network.CIDR)
		} else if err := validateCIDRCapacity(ipNet); err != nil {
			res.AddError("network.cidr '%s' is too small for the network plan: %v", c.Network.CIDR, err)
		}
	}

	// 9. Network VIPs. Full collision checks need the resolved subnet, so only
	// explicit CIDRs are validated here; the rest is validated after the Host
	// resolves the auto-allocated subnet.
	if err := c.ResolveVIPs(); err != nil {
		res.AddError("network VIPs: %v", err)
	} else if !c.Network.CIDRAuto && c.Network.CIDR != "" {
		res.Merge(ValidateNetworkAddressing(c))
	} else if !c.UsesVIPs() {
		if c.Network.CustomAPIVIP != "" || c.Network.CustomIngressVIP != "" || c.Network.CustomIngressLBVIP != "" {
			res.AddWarning("network VIP overrides are ignored for the 'sno' preset; the single node serves the API and ingress")
		}
	} else if c.Network.CustomAPIVIP != "" || c.Network.CustomIngressVIP != "" || c.Network.CustomIngressLBVIP != "" {
		res.AddWarning("network VIP overrides are validated after the subnet is resolved; set network.cidr explicitly for deterministic VIPs")
	}

	return res
}

// validateCIDRCapacity reports an error when ipNet cannot address the static
// offsets reserved by the network plan: the node leases and the DHCP range.
// The network and broadcast addresses are not usable hosts.
func validateCIDRCapacity(ipNet *net.IPNet) error {
	ones, bits := ipNet.Mask.Size()
	hostBits := bits - ones

	leaseBits := hostBitsForOffset(max(ControlPlaneLeaseOffset, WorkerLeaseOffset))
	dhcpBits := hostBitsForOffset(DHCPEndOffset)
	if hostBits >= leaseBits && hostBits >= dhcpBits {
		return nil
	}

	return fmt.Errorf("needs at least a /%d subnet to hold node leases (offsets %d-%d) and the DHCP range (offsets %d-%d)",
		bits-max(leaseBits, dhcpBits), ControlPlaneLeaseOffset, WorkerLeaseOffset, DHCPStartOffset, DHCPEndOffset)
}

// hostBitsForOffset returns the smallest number of host bits whose usable
// range can address offset. The network and broadcast addresses are excluded.
func hostBitsForOffset(offset int) int {
	hostBits := 0
	for (1<<uint(hostBits))-2 < offset {
		hostBits++
	}
	return hostBits
}

// ValidateNetworkAddressing validates the resolved VIP plan against the network
// CIDR, gateway, node leases and DHCP range. Call it after the subnet has been
// resolved (or when the CIDR was set explicitly).
func ValidateNetworkAddressing(c *Config) *ValidationResult {
	res := &ValidationResult{}

	if !c.UsesVIPs() {
		if c.Network.CustomAPIVIP != "" || c.Network.CustomIngressVIP != "" || c.Network.CustomIngressLBVIP != "" {
			res.AddWarning("network VIP overrides are ignored for the 'sno' preset; the single node serves the API and ingress")
		}
		return res
	}

	cidr := c.Network.CIDR
	if cidr == "" {
		cidr = DefaultCIDR
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil || ipNet.IP.To4() == nil {
		res.AddError("network.cidr '%s' is invalid: must be an IPv4 subnet", cidr)
		return res
	}

	gateway := c.Network.GatewayIP
	if gateway == "" {
		gateway, _ = DeriveGatewayIP(cidr)
	}

	topo, _ := c.EffectiveTopology()
	cpStart := uint32(ControlPlaneLeaseOffset)
	workerStart := uint32(WorkerLeaseOffset)

	type vip struct {
		name  string
		value string
	}
	vips := []vip{
		{"network.apiVip", c.Network.APIVIP},
		{"network.ingressVip", c.Network.IngressVIP},
		{"network.ingressLbVip", c.Network.IngressLBVIP},
	}
	seen := make(map[string]vip, len(vips))

	for _, v := range vips {
		offset, err := IPOffset(cidr, v.value)
		if err != nil {
			res.AddError("%s %q: %v", v.name, v.value, err)
			continue
		}
		if prev, ok := seen[v.value]; ok {
			res.AddError("%s %q must differ from %s %q", v.name, v.value, prev.name, prev.value)
			continue
		}
		seen[v.value] = v

		ip := net.ParseIP(v.value).To4()
		network := binary.BigEndian.Uint32(ipNet.IP.To4())
		broadcast := network | ^binary.BigEndian.Uint32(ipNet.Mask)
		value := binary.BigEndian.Uint32(ip)
		switch {
		case value == network:
			res.AddError("%s %q is the network address; use a host address", v.name, v.value)
			continue
		case value == broadcast:
			res.AddError("%s %q is the broadcast address; use a host address", v.name, v.value)
			continue
		case v.value == gateway:
			res.AddError("%s %q must not equal the gateway IP", v.name, v.value)
			continue
		case offset >= DHCPStartOffset && offset <= DHCPEndOffset:
			res.AddError("%s %q is inside the DHCP dynamic range (offsets %d-%d)", v.name, v.value, DHCPStartOffset, DHCPEndOffset)
			continue
		}

		if topo != nil {
			if offset >= cpStart && offset < cpStart+uint32(topo.ControlPlaneReplicas) {
				res.AddError("%s %q collides with a control-plane node lease", v.name, v.value)
				continue
			}
			if offset >= workerStart && offset < workerStart+uint32(topo.WorkerReplicas) {
				res.AddError("%s %q collides with a worker node lease", v.name, v.value)
				continue
			}
		}
	}

	return res
}

// ValidateCapacity evaluates requested cluster resources against reported Host capacity.
// RAM and Disk over-allocation result in hard errors; vCPU overcommit results in a warning.
func ValidateCapacity(c *Config, host HostCapacity) *ValidationResult {
	res := &ValidationResult{}

	topo, err := c.EffectiveTopology()
	if err != nil {
		res.AddError("topology evaluation failed: %v", err)
		return res
	}

	reqVCPUs := topo.TotalVCPUs()
	reqRAMMiB := topo.TotalMemoryMiB()
	reqDiskGiB := topo.TotalDiskGiB()

	// 1. RAM Capacity Check (HARD ERROR)
	availRAM := host.AvailableRAMMiB
	if availRAM == 0 {
		availRAM = host.TotalRAMMiB
	}
	if host.TotalRAMMiB > 0 && reqRAMMiB > availRAM {
		res.AddError("insufficient Host RAM: cluster requires %d MiB (%.1f GiB), but Host only has %d MiB (%.1f GiB) available",
			reqRAMMiB, float64(reqRAMMiB)/1024.0, availRAM, float64(availRAM)/1024.0)
	}

	// 2. Disk Capacity Check (HARD ERROR)
	availDisk := host.AvailableDiskGiB
	if availDisk == 0 {
		availDisk = host.TotalDiskGiB
	}
	if host.TotalDiskGiB > 0 && reqDiskGiB > availDisk {
		res.AddError("insufficient Host Disk: cluster requires %d GiB in /home/libvirt, but Host only has %d GiB available",
			reqDiskGiB, availDisk)
	}

	// 3. vCPU Overcommit Check (SOFT WARNING)
	if host.VCPUs > 0 && reqVCPUs > host.VCPUs {
		res.AddWarning("vCPU overcommit: cluster requires %d vCPUs, which exceeds the Host's %d physical vCPUs (ratio: %.1f:1)",
			reqVCPUs, host.VCPUs, float64(reqVCPUs)/float64(host.VCPUs))
	}

	// 4. Sub-minimum checks against official Red Hat recommendations
	if c.ControlPlaneOverrides != nil {
		minCP := MinMultiNodeControlPlane
		if topo.Preset == PresetSNO {
			minCP = MinSNOControlPlane
		}
		if topo.ControlPlaneSettings.VCPUs < minCP.VCPUs {
			res.AddWarning("controlPlane vCPUs (%d) is below Red Hat's recommended minimum (%d)",
				topo.ControlPlaneSettings.VCPUs, minCP.VCPUs)
		}
		if topo.ControlPlaneSettings.MemoryMiB < minCP.MemoryMiB {
			res.AddWarning("controlPlane RAM (%d MiB) is below Red Hat's recommended minimum (%d MiB)",
				topo.ControlPlaneSettings.MemoryMiB, minCP.MemoryMiB)
		}
		if topo.ControlPlaneSettings.DiskGiB < minCP.DiskGiB {
			res.AddWarning("controlPlane Disk (%d GiB) is below Red Hat's recommended minimum (%d GiB)",
				topo.ControlPlaneSettings.DiskGiB, minCP.DiskGiB)
		}
	}

	if c.WorkerOverrides != nil && topo.WorkerReplicas > 0 {
		if topo.WorkerSettings.VCPUs < MinWorkerNode.VCPUs {
			res.AddWarning("worker vCPUs (%d) is below Red Hat's recommended minimum (%d)",
				topo.WorkerSettings.VCPUs, MinWorkerNode.VCPUs)
		}
		if topo.WorkerSettings.MemoryMiB < MinWorkerNode.MemoryMiB {
			res.AddWarning("worker RAM (%d MiB) is below Red Hat's recommended minimum (%d MiB)",
				topo.WorkerSettings.MemoryMiB, MinWorkerNode.MemoryMiB)
		}
		if topo.WorkerSettings.DiskGiB < MinWorkerNode.DiskGiB {
			res.AddWarning("worker Disk (%d GiB) is below Red Hat's recommended minimum (%d GiB)",
				topo.WorkerSettings.DiskGiB, MinWorkerNode.DiskGiB)
		}
	}

	return res
}

func isLikelySSHPublicKey(s string) bool {
	prefixes := []string{
		"ssh-rsa ",
		"ssh-ed25519 ",
		"ssh-dss ",
		"ecdsa-sha2-nistp256 ",
		"ecdsa-sha2-nistp384 ",
		"ecdsa-sha2-nistp521 ",
		"sk-ssh-ed25519@openssh.com ",
		"sk-ecdsa-sha2-nistp256@openssh.com ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// ValidateBaseDomain verifies that domain is a valid RFC 1123 domain name.
// It enforces length <= 253, at least 2 dot-separated labels, each label 1-63 chars
// matching ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$, rightmost label (TLD) not all-numeric,
// rejects IP addresses, whitespace, path traversal, and shell metacharacters.
func ValidateBaseDomain(domain string) error {
	trimmed := strings.TrimSpace(domain)
	if trimmed == "" {
		return fmt.Errorf("baseDomain is required")
	}
	if strings.ContainsAny(domain, " \t\r\n") {
		return fmt.Errorf("baseDomain '%s' is invalid: must not contain whitespace", domain)
	}
	if len(domain) > 253 {
		return fmt.Errorf("baseDomain '%s' exceeds maximum length of 253 characters", domain)
	}
	if net.ParseIP(domain) != nil {
		return fmt.Errorf("baseDomain '%s' is invalid: must not be an IP address", domain)
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return fmt.Errorf("baseDomain '%s' is invalid: contains empty label (consecutive, leading, or trailing dots)", domain)
	}

	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("baseDomain '%s' must contain at least one dot (e.g. 'example.com')", domain)
	}

	for i, label := range labels {
		if len(label) == 0 {
			return fmt.Errorf("baseDomain '%s' is invalid: contains empty label", domain)
		}
		if len(label) > 63 {
			return fmt.Errorf("baseDomain '%s' label '%s' exceeds maximum length of 63 characters", domain, label)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("baseDomain '%s' label '%s' is invalid: label cannot start or end with a hyphen", domain, label)
		}
		if !rfc1123Regex.MatchString(strings.ToLower(label)) {
			return fmt.Errorf("baseDomain '%s' label '%s' is invalid: must be lower-case alphanumeric with optional hyphens (RFC 1123)", domain, label)
		}
		// Rightmost label (TLD) must not be purely numeric (RFC 1123 / RFC 3696)
		if i == len(labels)-1 && allNumericRegex.MatchString(label) {
			return fmt.Errorf("baseDomain '%s' top-level domain label '%s' is invalid: must not be all-numeric", domain, label)
		}
	}
	return nil
}

// ValidateNetworkName validates a libvirt network name.
// Non-empty names must start with an alphanumeric character, contain only
// alphanumeric, hyphen, and underscore characters, and have length between 1 and 64 characters.
func ValidateNetworkName(networkName string) error {
	trimmed := strings.TrimSpace(networkName)
	if trimmed == "" {
		return fmt.Errorf("network.name is required")
	}
	if strings.ContainsAny(networkName, " \t\r\n") {
		return fmt.Errorf("network.name '%s' is invalid: must not contain whitespace", networkName)
	}
	if len(networkName) > 64 {
		return fmt.Errorf("network.name '%s' is invalid: exceeds maximum length of 64 characters", networkName)
	}
	if !networkNameRegex.MatchString(networkName) {
		if strings.HasPrefix(networkName, "-") || strings.HasPrefix(networkName, "_") {
			return fmt.Errorf("network.name '%s' is invalid: must start with an alphanumeric character", networkName)
		}
		return fmt.Errorf("network.name '%s' is invalid: must start with an alphanumeric character and contain only alphanumeric, hyphen, and underscore characters (1-64 characters)", networkName)
	}
	return nil
}
