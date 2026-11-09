package host

import (
	"context"
	"fmt"
	"strings"
)

// IngressBackend is a node that runs an OpenShift ingress router.
type IngressBackend struct {
	Name string
	IP   string
}

// IngressLBHostPaths returns the host-side files owned by the lab ingress load
// balancer. They are kept under /etc/virtlab and a dedicated systemd unit so the
// distribution's /etc/haproxy/haproxy.cfg and haproxy.service stay untouched.
type IngressLBHostPaths struct {
	Config string
	Unit   string
	PID    string
	Socket string
}

// IngressLBServiceName returns the dedicated systemd unit name.
func IngressLBServiceName(clusterName string) string {
	return fmt.Sprintf("virtlab-%s-haproxy.service", clusterName)
}

// IngressLBPaths returns the paths owned by the ingress load balancer.
func IngressLBPaths(clusterName string) IngressLBHostPaths {
	return IngressLBHostPaths{
		Config: fmt.Sprintf("/etc/virtlab/%s-haproxy.cfg", clusterName),
		Unit:   "/etc/systemd/system/" + IngressLBServiceName(clusterName),
		PID:    fmt.Sprintf("/run/virtlab-%s-haproxy.pid", clusterName),
		Socket: fmt.Sprintf("/run/virtlab-%s-haproxy.sock", clusterName),
	}
}

// BuildHAProxyConfig renders a TCP-mode HAProxy configuration that fronts every
// ingress router. balance source keeps passthrough sessions pinned to the same
// router; tcp-check marks a backend down when its router stops listening.
func BuildHAProxyConfig(vip, socketPath string, backends []IngressBackend) string {
	var b strings.Builder

	b.WriteString("global\n")
	b.WriteString("    log /dev/log local0\n")
	b.WriteString("    maxconn 4096\n")
	fmt.Fprintf(&b, "    stats socket %s mode 660 level admin\n\n", socketPath)

	b.WriteString("defaults\n")
	b.WriteString("    mode tcp\n")
	b.WriteString("    log global\n")
	b.WriteString("    option tcplog\n")
	b.WriteString("    timeout connect 5s\n")
	b.WriteString("    timeout client 1m\n")
	b.WriteString("    timeout server 1m\n\n")

	for _, proto := range []struct {
		name string
		port string
	}{
		{"http", "80"},
		{"https", "443"},
	} {
		fmt.Fprintf(&b, "frontend ingress-%s\n", proto.name)
		fmt.Fprintf(&b, "    bind %s:%s\n", vip, proto.port)
		fmt.Fprintf(&b, "    default_backend ingress-%s\n\n", proto.name)

		fmt.Fprintf(&b, "backend ingress-%s\n", proto.name)
		b.WriteString("    balance source\n")
		b.WriteString("    option tcp-check\n")
		for _, backend := range backends {
			fmt.Fprintf(&b, "    server %s %s:%s check\n", backend.Name, backend.IP, proto.port)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// BuildIngressLBSetupScript returns the remote script that assigns the VIP to
// the lab bridge, writes the HAProxy config and unit, and starts the service.
func BuildIngressLBSetupScript(cfgText string) (string, error) {
	const marker = "VIRTLAB_HAPROXY_CFG"
	if strings.Contains(cfgText, marker) {
		return "", fmt.Errorf("HAProxy config contains the heredoc marker %q", marker)
	}

	return `set -euo pipefail
BR="$1"; VIP="$2"; PREFIX="$3"; CFG="$4"; UNIT="$5"; PID="$6"; SVC="$7"; CLUSTER="$8"

ip addr replace "${VIP}/${PREFIX}" dev "$BR"

mkdir -p /etc/virtlab
cat >"$CFG" <<'` + marker + `'
` + cfgText + `
` + marker + `
chmod 0644 "$CFG"

cat >"$UNIT" <<UNIT
[Unit]
Description=virtlab HAProxy ingress for ${CLUSTER}
After=network-online.target libvirtd.service
Wants=network-online.target

[Service]
ExecStart=/usr/sbin/haproxy -Ws -f ${CFG} -p ${PID}
ExecReload=/bin/kill -USR2 \$MAINPID
Restart=on-failure

[Install]
WantedBy=multi-user.target
UNIT
chmod 0644 "$UNIT"

systemctl daemon-reload
systemctl enable "$SVC"
systemctl restart "$SVC"
`, nil
}

// BuildIngressLBTeardownScript returns the remote script that stops and removes
// the ingress load balancer and its firewall openings.
func BuildIngressLBTeardownScript() string {
	return `set -euo pipefail
SVC="$1"; CFG="$2"; UNIT="$3"; PID="$4"; SOCK="$5"

systemctl disable --now "$SVC" >/dev/null 2>&1 || true
rm -f "$CFG" "$UNIT" "$PID" "$SOCK"
systemctl daemon-reload >/dev/null 2>&1 || true

if command -v firewall-cmd >/dev/null 2>&1; then
  firewall-cmd --permanent --zone=libvirt --remove-port=80/tcp >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=libvirt --remove-port=443/tcp >/dev/null 2>&1 || true
  firewall-cmd --reload >/dev/null 2>&1 || true
fi
`
}

// SetupIngressLB installs and starts the HAProxy frontend for the ingress VIP.
func (c *SSHClient) SetupIngressLB(ctx context.Context, clusterName, bridge, vip string, prefixLen int, backends []IngressBackend) error {
	if err := ValidateBridgeDeviceName(bridge); err != nil {
		return err
	}
	if strings.TrimSpace(vip) == "" {
		return fmt.Errorf("ingress load-balancer VIP is empty")
	}
	if len(backends) == 0 {
		return fmt.Errorf("ingress load-balancer has no backends")
	}

	paths := IngressLBPaths(clusterName)
	cfgText := BuildHAProxyConfig(vip, paths.Socket, backends)
	script, err := BuildIngressLBSetupScript(cfgText)
	if err != nil {
		return err
	}

	out, err := c.RunScriptContext(ctx, script,
		bridge,
		vip,
		fmt.Sprintf("%d", prefixLen),
		paths.Config,
		paths.Unit,
		paths.PID,
		IngressLBServiceName(clusterName),
		clusterName,
	)
	if err != nil {
		return fmt.Errorf("setup ingress load balancer: %w", err)
	}
	fmt.Println(out)
	return nil
}

// TeardownIngressLB stops and removes the HAProxy frontend and firewall ports.
func (c *SSHClient) TeardownIngressLB(ctx context.Context, clusterName string) error {
	paths := IngressLBPaths(clusterName)
	if _, err := c.RunScriptContext(ctx, BuildIngressLBTeardownScript(),
		IngressLBServiceName(clusterName),
		paths.Config,
		paths.Unit,
		paths.PID,
		paths.Socket,
	); err != nil {
		return fmt.Errorf("teardown ingress load balancer: %w", err)
	}
	return nil
}
