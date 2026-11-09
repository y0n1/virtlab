package infrastructure

import (
	"fmt"
	"net"
	"strings"

	"github.com/pulumi/pulumi-terraform-provider/sdks/go/libvirt/libvirt"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

// NodeLease contains static DHCP and DNS mappings for a cluster Node.
type NodeLease struct {
	Name string
	MAC  string
	IP   string
	Role string
}

// ComputeNodeLeases generates predictable static leases for control plane and worker nodes.
func ComputeNodeLeases(cfg *config.Config) ([]NodeLease, error) {
	topo, err := cfg.EffectiveTopology()
	if err != nil {
		return nil, err
	}

	cidr := cfg.Network.CIDR
	if cidr == "" {
		cidr = config.DefaultCIDR
	}

	var leases []NodeLease

	// Control plane nodes: master-0 (.10), master-1 (.11), master-2 (.12)
	for i := 0; i < topo.ControlPlaneReplicas; i++ {
		ip, err := config.OffsetIP(cidr, uint32(config.ControlPlaneLeaseOffset+i))
		if err != nil {
			return nil, fmt.Errorf("infrastructure: compute IP for master-%d: %w", i, err)
		}
		leases = append(leases, NodeLease{
			Name: fmt.Sprintf("master-%d", i),
			MAC:  fmt.Sprintf("52:54:00:a1:0c:%02x", i+1),
			IP:   ip,
			Role: "master",
		})
	}

	// Worker nodes: worker-0 (.20), worker-1 (.21)...
	for j := 0; j < topo.WorkerReplicas; j++ {
		ip, err := config.OffsetIP(cidr, uint32(config.WorkerLeaseOffset+j))
		if err != nil {
			return nil, fmt.Errorf("infrastructure: compute IP for worker-%d: %w", j, err)
		}
		leases = append(leases, NodeLease{
			Name: fmt.Sprintf("worker-%d", j),
			MAC:  fmt.Sprintf("52:54:00:a1:0c:%02x", 20+j),
			IP:   ip,
			Role: "worker",
		})
	}

	return leases, nil
}

// dnsHostRecord is a pure-data DNS host mapping. Keeping it free of Pulumi
// inputs lets the network topology be unit-tested without a Pulumi runtime.
type dnsHostRecord struct {
	IP        string
	Hostnames []string
}

// IngressLeases returns the leases that serve ingress traffic. In sno/compact
// the control-plane nodes also serve ingress; in ha dedicated workers do.
func IngressLeases(topo *config.NodeTopology, leases []NodeLease) []NodeLease {
	if topo.WorkerReplicas == 0 {
		return leases[:topo.ControlPlaneReplicas]
	}
	return leases[topo.ControlPlaneReplicas:]
}

// buildDNSHostRecords returns the network's DNS host records:
//  1. api.<fqdn> and api-int.<fqdn>. Presets installed with platform: baremetal
//     resolve them to the cluster API VIP (keepalived); SNO resolves them to its
//     single node.
//  2. The per-node FQDN and short hostname for every node.
func buildDNSHostRecords(cfg *config.Config, leases []NodeLease, topo *config.NodeTopology) []dnsHostRecord {
	records := make([]dnsHostRecord, 0, len(leases)+topo.ControlPlaneReplicas)
	if cfg.UsesVIPs() {
		records = append(records, dnsHostRecord{
			IP:        cfg.Network.APIVIP,
			Hostnames: []string{cfg.APIHost(), cfg.APIIntHost()},
		})
	} else {
		for _, l := range leases[:topo.ControlPlaneReplicas] {
			records = append(records, dnsHostRecord{
				IP:        l.IP,
				Hostnames: []string{cfg.APIHost(), cfg.APIIntHost()},
			})
		}
	}
	for _, l := range leases {
		records = append(records, dnsHostRecord{
			IP:        l.IP,
			Hostnames: []string{fmt.Sprintf("%s.%s", l.Name, cfg.ClusterFQDN()), l.Name},
		})
	}
	return records
}

// buildDnsmasqOptions returns the wildcard apps record. Non-SNO presets point
// *.apps at the HAProxy VIP (which fronts every ingress node); SNO points it at
// its single node. dnsmasq 2.85 cannot express multiple addresses per wildcard,
// so a single LB address is required for horizontal ingress scale.
func buildDnsmasqOptions(cfg *config.Config, leases []NodeLease, topo *config.NodeTopology) []string {
	if cfg.UsesVIPs() {
		return []string{"address=/apps." + cfg.ClusterFQDN() + "/" + cfg.Network.IngressLBVIP}
	}
	ingress := IngressLeases(topo, leases)
	options := make([]string, 0, len(ingress))
	for _, l := range ingress {
		options = append(options, "address=/apps."+cfg.ClusterFQDN()+"/"+l.IP)
	}
	return options
}

// BuildNetwork creates the isolated libvirt virtual network and bridge.
// Invariant: The 'default' network is strictly forbidden.
func BuildNetwork(ctx *pulumi.Context, cfg *config.Config, bridgeName string) (*libvirt.Network, error) {
	if strings.ToLower(cfg.Network.Name) == "default" {
		return nil, fmt.Errorf("infrastructure: 'default' network is strictly forbidden; name must be distinct")
	}

	if bridgeName == "" {
		bridgeName = "virbr-lab0"
	}

	// VIPs feed the DNS records; resolve them even when BuildNetwork is called
	// with a manually constructed Config.
	if err := cfg.ResolveVIPs(); err != nil {
		return nil, fmt.Errorf("infrastructure: resolve VIPs: %w", err)
	}

	leases, err := ComputeNodeLeases(cfg)
	if err != nil {
		return nil, err
	}

	// Build DHCP static leases
	var dhcpHosts libvirt.NetworkIpDhcpHostArray
	for _, l := range leases {
		dhcpHosts = append(dhcpHosts, &libvirt.NetworkIpDhcpHostArgs{
			Ip:   pulumi.String(l.IP),
			Mac:  pulumi.String(l.MAC),
			Name: pulumi.String(l.Name),
		})
	}

	topo, err := cfg.EffectiveTopology()
	if err != nil {
		return nil, err
	}

	// Build DNS host records
	var dnsHosts libvirt.NetworkDnsHostArray
	for _, record := range buildDNSHostRecords(cfg, leases, topo) {
		var hostnames libvirt.NetworkDnsHostHostnameArray
		for _, hostname := range record.Hostnames {
			hostnames = append(hostnames, &libvirt.NetworkDnsHostHostnameArgs{
				Hostname: pulumi.String(hostname),
			})
		}
		dnsHosts = append(dnsHosts, &libvirt.NetworkDnsHostArgs{
			Ip:        pulumi.String(record.IP),
			Hostnames: hostnames,
		})
	}

	cidr := cfg.Network.CIDR
	if cidr == "" {
		cidr = config.DefaultCIDR
	}

	dhcpStart, err := config.OffsetIP(cidr, uint32(config.DHCPStartOffset))
	if err != nil {
		return nil, fmt.Errorf("infrastructure: compute DHCP start IP: %w", err)
	}
	dhcpEnd, err := config.OffsetIP(cidr, uint32(config.DHCPEndOffset))
	if err != nil {
		return nil, fmt.Errorf("infrastructure: compute DHCP end IP: %w", err)
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("infrastructure: parse CIDR '%s': %w", cidr, err)
	}
	netmask := net.IP(ipNet.Mask).String()

	net, err := libvirt.NewNetwork(ctx, "lab-network", &libvirt.NetworkArgs{
		Name:      pulumi.String(cfg.Network.Name),
		Autostart: pulumi.Bool(true),
		Bridge: &libvirt.NetworkBridgeArgs{
			Name: pulumi.String(bridgeName),
		},
		Forward: &libvirt.NetworkForwardArgs{
			Mode: pulumi.String("nat"),
		},
		Domain: &libvirt.NetworkDomainArgs{
			Name:      pulumi.String(cfg.BaseDomain),
			LocalOnly: pulumi.String("yes"),
		},
		Dns: &libvirt.NetworkDnsArgs{
			Hosts: dnsHosts,
		},
		DnsmasqOptions: &libvirt.NetworkDnsmasqOptionsArgs{
			Options: func() libvirt.NetworkDnsmasqOptionsOptionArray {
				var opts libvirt.NetworkDnsmasqOptionsOptionArray
				for _, value := range buildDnsmasqOptions(cfg, leases, topo) {
					opts = append(opts, &libvirt.NetworkDnsmasqOptionsOptionArgs{
						Value: pulumi.String(value),
					})
				}
				return opts
			}(),
		},
		Ips: libvirt.NetworkIpArray{
			&libvirt.NetworkIpArgs{
				Address: pulumi.String(cfg.Network.GatewayIP),
				Netmask: pulumi.String(netmask),
				Dhcp: &libvirt.NetworkIpDhcpArgs{
					Ranges: libvirt.NetworkIpDhcpRangeArray{
						&libvirt.NetworkIpDhcpRangeArgs{
							Start: pulumi.String(dhcpStart),
							End:   pulumi.String(dhcpEnd),
						},
					},
					Hosts: dhcpHosts,
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("infrastructure: create network '%s': %w", cfg.Network.Name, err)
	}

	return net, nil
}
