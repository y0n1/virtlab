package host

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
)

// ResolveNetworkSubnet inspects the hypervisor to determine a non-conflicting CIDR
// and gateway IP for the virtual network.
// If customCIDR is provided and autoAlloc is false:
//
//	Validates the CIDR, derives the gateway, and checks for conflicts on the host.
//
// If autoAlloc is true (default):
//  1. Reuses existing subnet & gateway if networkName is already defined in libvirt.
//  2. Otherwise finds the lowest available 192.168.X.0/24 subnet (starting at X=100)
//     that does not overlap with any host routes, interfaces, or libvirt networks.
func (h *Host) ResolveNetworkSubnet(networkName, customCIDR string, autoAlloc bool) (string, string, error) {
	return h.Client.ResolveNetworkSubnet(networkName, customCIDR, autoAlloc)
}

const subnetProbeScript = `
set -e
NET_NAME="$1"

# 1. Existing IP and netmask for this specific network (if defined in libvirt)
existing_ip=""
existing_netmask=""
xml=$(virsh net-dumpxml "$NET_NAME" 2>/dev/null || true)
if [ -n "$xml" ]; then
    existing_ip=$(echo "$xml" | grep -o "<ip address=['\"][^'\"]*" | head -1 | cut -d"'" -f2 | cut -d'"' -f2 || true)
    existing_netmask=$(echo "$xml" | grep -o "netmask=['\"][^'\"]*" | head -1 | cut -d"'" -f2 | cut -d'"' -f2 || true)
fi
echo "EXISTING_IP:${existing_ip}"
echo "EXISTING_NETMASK:${existing_netmask}"

# 2. Host IPv4 routes
echo "ROUTES:"
ip -o -4 route show 2>/dev/null | awk '{print $1}' || true

# 3. Host interface IPv4 addresses
echo "ADDRESSES:"
ip -o -4 addr show 2>/dev/null | awk '{print $4}' || true

# 4. All subnets defined in any libvirt networks
echo "LIBVIRT_SUBNETS:"
for net in $(virsh net-list --all --name 2>/dev/null); do
    nxml=$(virsh net-dumpxml "$net" 2>/dev/null || true)
    nip=$(echo "$nxml" | grep -o "<ip address=['\"][^'\"]*" | head -1 | cut -d"'" -f2 | cut -d'"' -f2 || true)
    nmask=$(echo "$nxml" | grep -o "netmask=['\"][^'\"]*" | head -1 | cut -d"'" -f2 | cut -d'"' -f2 || true)
    if [ -n "$nip" ] && [ -n "$nmask" ]; then
        echo "${nip}/${nmask}"
    fi
done
`

// ResolveNetworkSubnet runs the probe on the host and computes the CIDR and gateway.
func (c *SSHClient) ResolveNetworkSubnet(networkName, customCIDR string, autoAlloc bool) (string, string, error) {
	out, err := c.RunScript(subnetProbeScript, networkName)
	if err != nil {
		return "", "", fmt.Errorf("probe host network subnets: %w", err)
	}

	return parseSubnetProbeOutput(out, networkName, customCIDR, autoAlloc)
}

func parseSubnetProbeOutput(out, networkName, customCIDR string, autoAlloc bool) (string, string, error) {
	lines := strings.Split(out, "\n")
	var existingIP, existingNetmask string
	var inUseSubnets []*net.IPNet
	section := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "EXISTING_IP:") {
			existingIP = strings.TrimPrefix(line, "EXISTING_IP:")
			continue
		}
		if strings.HasPrefix(line, "EXISTING_NETMASK:") {
			existingNetmask = strings.TrimPrefix(line, "EXISTING_NETMASK:")
			continue
		}
		if line == "ROUTES:" {
			section = "routes"
			continue
		}
		if line == "ADDRESSES:" {
			section = "addresses"
			continue
		}
		if line == "LIBVIRT_SUBNETS:" {
			section = "libvirt"
			continue
		}

		switch section {
		case "routes":
			if line == "default" || strings.HasPrefix(line, "default ") {
				continue
			}
			_, ipNet, err := net.ParseCIDR(line)
			if err == nil && ipNet.IP.To4() != nil {
				inUseSubnets = append(inUseSubnets, ipNet)
			}
		case "addresses":
			_, ipNet, err := net.ParseCIDR(line)
			if err == nil && ipNet.IP.To4() != nil {
				inUseSubnets = append(inUseSubnets, ipNet)
			}
		case "libvirt":
			parts := strings.Split(line, "/")
			if len(parts) == 2 {
				ip := net.ParseIP(parts[0])
				maskIP := net.ParseIP(parts[1])
				if ip != nil && maskIP != nil && ip.To4() != nil {
					mask := net.IPMask(maskIP.To4())
					netObj := &net.IPNet{
						IP:   ip.To4().Mask(mask),
						Mask: mask,
					}
					inUseSubnets = append(inUseSubnets, netObj)
				}
			}
		}
	}

	// 1. If autoAlloc is true and this network is already defined, reuse its subnet
	if autoAlloc && existingIP != "" && existingNetmask != "" {
		ip := net.ParseIP(existingIP)
		maskIP := net.ParseIP(existingNetmask)
		if ip != nil && maskIP != nil && ip.To4() != nil {
			mask := net.IPMask(maskIP.To4())
			netIP := ip.To4().Mask(mask)
			ones, _ := mask.Size()
			cidr := fmt.Sprintf("%s/%d", netIP.String(), ones)
			return cidr, existingIP, nil
		}
	}

	// 2. If customCIDR is specified by the user (advanced override)
	if !autoAlloc && customCIDR != "" {
		_, customNet, err := net.ParseCIDR(customCIDR)
		if err != nil {
			return "", "", fmt.Errorf("invalid custom CIDR '%s': %w", customCIDR, err)
		}
		for _, inUse := range inUseSubnets {
			if subnetsOverlap(customNet, inUse) {
				return "", "", fmt.Errorf("specified network.cidr '%s' conflicts with existing host route/subnet %s", customCIDR, inUse.String())
			}
		}
		gw, err := deriveGatewayForNet(customNet)
		if err != nil {
			return "", "", err
		}
		return customCIDR, gw, nil
	}

	// 3. Auto-allocate: find first available 192.168.X.0/24 starting at X=100
	for x := 100; x <= 254; x++ {
		candidateCIDR := fmt.Sprintf("192.168.%d.0/24", x)
		_, candidateNet, _ := net.ParseCIDR(candidateCIDR)
		hasOverlap := false
		for _, inUse := range inUseSubnets {
			if subnetsOverlap(candidateNet, inUse) {
				hasOverlap = true
				break
			}
		}
		if !hasOverlap {
			gw := fmt.Sprintf("192.168.%d.1", x)
			return candidateCIDR, gw, nil
		}
	}

	return "", "", fmt.Errorf("exhausted available 192.168.X.0/24 subnets (100-254) on host")
}

func subnetsOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

func deriveGatewayForNet(ipNet *net.IPNet) (string, error) {
	ip := ipNet.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("subnet must be IPv4")
	}
	val := binary.BigEndian.Uint32(ip) + 1
	gw := make(net.IP, 4)
	binary.BigEndian.PutUint32(gw, val)
	return gw.String(), nil
}
