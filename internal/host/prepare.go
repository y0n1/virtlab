package host

import (
	"fmt"

	"github.com/y0n1/virtlab/internal/config"
)

// PrepareHost configures base packages, services, sysctl forwarding, and NetworkManager DNS on the Host.
func (c *SSHClient) PrepareHost(cfg *config.Config) error {
	fmt.Printf("\n[Host] Preparing remote hypervisor (%s)\n", c.Target())

	const prepareScript = `
set -euo pipefail

CLUSTER_DIR="$1"
POOL_DIR="$2"
INSTALL_DIR="$3"
LOGS_DIR="$4"
BASE_DOMAIN="$5"
CLUSTER_NAME="$6"
GATEWAY_IP="$7"

echo "==> Ensuring required base packages"
dnf install -y libvirt libvirt-client qemu-kvm qemu-img dnsmasq chrony nmstate python3 virt-install curl tar haproxy

echo "==> Enabling core hypervisor services"
systemctl enable --now libvirtd chronyd

echo "==> Opening ingress ports (80/443) in the libvirt firewall zone"
if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --permanent --zone=libvirt --add-port=80/tcp >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=libvirt --add-port=443/tcp >/dev/null 2>&1 || true
  firewall-cmd --reload >/dev/null 2>&1 || true
fi

echo "==> Ensuring IPv4 forwarding is active"
printf 'net.ipv4.ip_forward=1\n' >/etc/sysctl.d/99-${CLUSTER_NAME}-forward.conf
sysctl --system >/dev/null

echo "==> Ensuring /home/libvirt and cluster directories exist"
mkdir -p /home/libvirt "$CLUSTER_DIR" "$POOL_DIR" "$LOGS_DIR"
chmod 0755 /home/libvirt "$CLUSTER_DIR" "$POOL_DIR" "$LOGS_DIR"
mkdir -p "$INSTALL_DIR"
chmod 0700 "$INSTALL_DIR"

echo "==> Configuring NetworkManager split-DNS for zone '${BASE_DOMAIN}'"
mkdir -p /etc/NetworkManager/conf.d /etc/NetworkManager/dnsmasq.d
printf '[main]\ndns=dnsmasq\n' >"/etc/NetworkManager/conf.d/00-${CLUSTER_NAME}-dnsmasq.conf"
printf 'server=/%s/%s\n' "$BASE_DOMAIN" "$GATEWAY_IP" >"/etc/NetworkManager/dnsmasq.d/${CLUSTER_NAME}.conf"
systemctl reload NetworkManager

echo "==> Host preparation complete"
`

	if out, err := c.RunScript(prepareScript,
		cfg.ClusterDir(),
		cfg.StoragePoolDir(),
		cfg.InstallDir(),
		cfg.LogsDir(),
		cfg.BaseDomain,
		cfg.ClusterName,
		cfg.Network.GatewayIP,
	); err != nil {
		return fmt.Errorf("host preparation failed: %w", err)
	} else {
		fmt.Println(out)
	}

	return nil
}
