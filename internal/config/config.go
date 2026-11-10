package config

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the complete declarative configuration for the lab.
type Config struct {
	ClusterName           string        `yaml:"clusterName"`
	BaseDomain            string        `yaml:"baseDomain"`
	OCPVersion            string        `yaml:"ocpVersion"`
	HostSSH               HostSSHConfig `yaml:"hostSsh"`
	PullSecretFile        string        `yaml:"pullSecretFile,omitempty"`
	Network               NetworkConfig `yaml:"network"`
	Preset                string        `yaml:"preset"`
	ControlPlaneOverrides *NodeSettings `yaml:"controlPlaneOverrides,omitempty"`
	WorkerOverrides       *NodeSettings `yaml:"workerOverrides,omitempty"`
}

// HostSSHConfig specifies SSH connectivity parameters to the remote hypervisor Host.
type HostSSHConfig struct {
	Host          string `yaml:"host"`
	User          string `yaml:"user"`
	PublicKeyFile string `yaml:"publicKeyFile"` // Path to public key file on Workstation (REQUIRED)
}

// Network addressing defaults. Offsets are relative to the network address of
// the resolved IPv4 CIDR and are shared by DNS, leases, VIPs and validation so
// they cannot drift apart.
const (
	DefaultCIDR = "192.168.100.0/24"

	GatewayOffset             = 1
	DefaultAPIVIPOffset       = 5
	DefaultIngressVIPOffset   = 6
	DefaultIngressLBVIPOffset = 7
	ControlPlaneLeaseOffset   = 10
	WorkerLeaseOffset         = 20
	DHCPStartOffset           = 100
	DHCPEndOffset             = 200
)

// NetworkConfig defines parameters for the isolated Lab Network.
type NetworkConfig struct {
	Name            string `yaml:"name,omitempty"`      // Defaults to <clusterName>-net
	CIDR            string `yaml:"cidr,omitempty"`      // Optional override; defaults to auto-allocated 192.168.x.0/24
	CIDRAuto        bool   `yaml:"-"`                   // True if CIDR was auto-assigned rather than explicitly set in YAML
	GatewayIP       string `yaml:"-"`                   // Derived from CIDR (network address + 1)
	CustomGatewayIP string `yaml:"gatewayIp,omitempty"` // Captured to reject user override in validation

	// Cluster-managed VIPs (platform: baremetal). Empty custom values are
	// derived from the CIDR by ResolveVIPs.
	CustomAPIVIP       string `yaml:"apiVip,omitempty"`
	APIVIP             string `yaml:"-"`
	CustomIngressVIP   string `yaml:"ingressVip,omitempty"`
	IngressVIP         string `yaml:"-"`
	CustomIngressLBVIP string `yaml:"ingressLbVip,omitempty"`
	IngressLBVIP       string `yaml:"-"`
}

// NodeSettings defines hardware resources allocated to a cluster Node.
type NodeSettings struct {
	VCPUs     int `yaml:"vcpus"`
	MemoryMiB int `yaml:"memoryMib"`
	DiskGiB   int `yaml:"diskGib"`
}

// LoadConfig parses a YAML configuration file from path into a Config struct.
// It performs full schema validation required for lab provisioning.
func LoadConfig(path string) (*Config, error) {
	return LoadConfigForTier(path, TierProvisioning)
}

// LoadConfigForTier parses a YAML configuration file from path into a Config struct
// and performs schema validation appropriate for the requested tier.
// Provisioning runs full schema validation, while Teardown accepts minimal
// identity/host configs while preserving YAML syntax and known-field checks.
func LoadConfigForTier(path string, tier ValidationTier) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read file '%s': %w", path, err)
	}
	if tier == TierProvisioning {
		if err := ValidateSchema(data); err != nil {
			return nil, fmt.Errorf("config: '%s': %w", path, err)
		}
	}
	return LoadConfigFromBytes(data)
}

// LoadConfigFromBytes parses YAML data into a Config struct and applies defaults.
func LoadConfigFromBytes(data []byte) (*Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: unmarshal yaml: %w", err)
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// applyDefaults fills in standard defaults for unconfigured optional fields.
func (c *Config) applyDefaults() {
	if c.BaseDomain == "" {
		c.BaseDomain = "example.com"
	}
	if c.Preset == "" {
		c.Preset = PresetSNO
	}
	if c.HostSSH.User == "" {
		c.HostSSH.User = "root"
	}
	if c.Network.Name == "" && c.ClusterName != "" {
		c.Network.Name = fmt.Sprintf("%s-net", c.ClusterName)
	}
	if c.Network.CIDR == "" {
		c.Network.CIDR = DefaultCIDR
		c.Network.CIDRAuto = true
	}
	if c.Network.GatewayIP == "" {
		if gw, err := DeriveGatewayIP(c.Network.CIDR); err == nil {
			c.Network.GatewayIP = gw
		}
	}
	// Best-effort: a malformed CIDR is reported by ValidateStatic. The final
	// derivation happens again after the subnet is resolved on the Host.
	_ = c.ResolveVIPs()
}

// DeriveGatewayIP computes the first usable host IP for a given IPv4 CIDR (network address + 1).
func DeriveGatewayIP(cidr string) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR '%s': %w", cidr, err)
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("CIDR '%s' must be an IPv4 subnet", cidr)
	}
	val := binary.BigEndian.Uint32(ip) + GatewayOffset
	gw := make(net.IP, 4)
	binary.BigEndian.PutUint32(gw, val)
	return gw.String(), nil
}

// OffsetIP calculates an IP address offset from the network base address of an IPv4 CIDR.
func OffsetIP(cidr string, offset uint32) (string, error) {
	if cidr == "" {
		cidr = DefaultCIDR
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR '%s': %w", cidr, err)
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("CIDR '%s' must be an IPv4 subnet", cidr)
	}
	base := binary.BigEndian.Uint32(ip)
	target := base + offset
	res := make(net.IP, 4)
	binary.BigEndian.PutUint32(res, target)
	return res.String(), nil
}

// IPOffset returns the host offset of addr within cidr.
func IPOffset(cidr, addr string) (uint32, error) {
	if cidr == "" {
		cidr = DefaultCIDR
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR '%s': %w", cidr, err)
	}
	base := ipNet.IP.To4()
	if base == nil {
		return 0, fmt.Errorf("CIDR '%s' must be an IPv4 subnet", cidr)
	}
	ip := net.ParseIP(strings.TrimSpace(addr)).To4()
	if ip == nil {
		return 0, fmt.Errorf("'%s' is not a valid IPv4 address", addr)
	}
	if !ipNet.Contains(ip) {
		return 0, fmt.Errorf("'%s' is outside CIDR '%s'", addr, cidr)
	}
	return binary.BigEndian.Uint32(ip) - binary.BigEndian.Uint32(base), nil
}

// PrefixLength returns the prefix length of an IPv4 CIDR.
func PrefixLength(cidr string) (int, error) {
	if cidr == "" {
		cidr = DefaultCIDR
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR '%s': %w", cidr, err)
	}
	if ipNet.IP.To4() == nil {
		return 0, fmt.Errorf("CIDR '%s' must be an IPv4 subnet", cidr)
	}
	ones, _ := ipNet.Mask.Size()
	return ones, nil
}

// ResolveVIPs derives the cluster API, ingress and ingress load-balancer VIPs
// from the current CIDR, honoring explicit overrides. It is safe to call
// repeatedly; call it again after the Host resolves an auto-allocated subnet.
func (c *Config) ResolveVIPs() error {
	cidr := c.Network.CIDR
	if cidr == "" {
		cidr = DefaultCIDR
	}
	resolve := func(name, custom string, offset uint32) (string, error) {
		if v := strings.TrimSpace(custom); v != "" {
			return v, nil
		}
		v, err := OffsetIP(cidr, offset)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return v, nil
	}

	var err error
	if c.Network.APIVIP, err = resolve("apiVip", c.Network.CustomAPIVIP, DefaultAPIVIPOffset); err != nil {
		return err
	}
	if c.Network.IngressVIP, err = resolve("ingressVip", c.Network.CustomIngressVIP, DefaultIngressVIPOffset); err != nil {
		return err
	}
	if c.Network.IngressLBVIP, err = resolve("ingressLbVip", c.Network.CustomIngressLBVIP, DefaultIngressLBVIPOffset); err != nil {
		return err
	}
	return nil
}

// UsesVIPs reports whether the selected preset installs with cluster-managed
// VIPs. SNO keeps platform: none and serves the API and ingress from its node.
func (c *Config) UsesVIPs() bool {
	topo, err := c.EffectiveTopology()
	if err != nil {
		return false
	}
	return topo.ControlPlaneReplicas > 1 || topo.WorkerReplicas > 0
}

// ExpandPath resolves tilde (~) to the user's home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ResolvedPublicKeyPath returns the expanded path to the host SSH public key file.
func (c *Config) ResolvedPublicKeyPath() string {
	return ExpandPath(c.HostSSH.PublicKeyFile)
}

// ReadSSHPublicKey reads and returns the trimmed SSH public key string from PublicKeyFile.
func (c *Config) ReadSSHPublicKey() (string, error) {
	path := c.ResolvedPublicKeyPath()
	if path == "" {
		return "", fmt.Errorf("config: hostSsh.publicKeyFile is empty")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("config: cannot read SSH public key file '%s': %w", path, err)
	}
	key := strings.TrimSpace(string(content))
	if key == "" {
		return "", fmt.Errorf("config: SSH public key file '%s' is empty", path)
	}
	return key, nil
}

// ClusterFQDN returns the cluster's base fully qualified domain name: <clusterName>.<baseDomain>.
func (c *Config) ClusterFQDN() string {
	return fmt.Sprintf("%s.%s", c.ClusterName, c.BaseDomain)
}

// APIHost returns the cluster API hostname: api.<clusterName>.<baseDomain>.
func (c *Config) APIHost() string {
	return fmt.Sprintf("api.%s", c.ClusterFQDN())
}

// APIIntHost returns the cluster internal API hostname: api-int.<clusterName>.<baseDomain>.
func (c *Config) APIIntHost() string {
	return fmt.Sprintf("api-int.%s", c.ClusterFQDN())
}

// StoragePoolDir returns the dedicated libvirt storage pool directory path on the Host:
// Invariant: all VM disks and storage volumes must live strictly inside /home/libvirt/<clusterName>/pool.
func (c *Config) StoragePoolDir() string {
	return fmt.Sprintf("/home/libvirt/%s/pool", c.ClusterName)
}

// InstallDir returns the OpenShift cluster installation directory path on the Host:
// Holds install-config.yaml, agent-config.yaml, Agent ISO, auth kubeconfig, and install logs.
func (c *Config) InstallDir() string {
	return fmt.Sprintf("/home/libvirt/%s/installer", c.ClusterName)
}

// LogsDir returns the runtime console and VM log directory path on the Host:
// Invariant: QEMU serial logs live in /home/libvirt/<clusterName>/logs to avoid polluting the storage pool.
func (c *Config) LogsDir() string {
	return fmt.Sprintf("/home/libvirt/%s/logs", c.ClusterName)
}

// ClusterDir returns the parent directory for all cluster artifacts on the Host:
func (c *Config) ClusterDir() string {
	return fmt.Sprintf("/home/libvirt/%s", c.ClusterName)
}
