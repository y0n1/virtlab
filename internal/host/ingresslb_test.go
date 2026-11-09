package host

import (
	"strings"
	"testing"
)

func TestBuildHAProxyConfig(t *testing.T) {
	backends := []IngressBackend{
		{Name: "master-0", IP: "192.168.100.10"},
		{Name: "master-1", IP: "192.168.100.11"},
	}
	cfg := BuildHAProxyConfig("192.168.100.7", "/run/virtlab-lab-haproxy.sock", backends)

	required := []string{
		"mode tcp",
		"stats socket /run/virtlab-lab-haproxy.sock mode 660 level admin",
		"bind 192.168.100.7:80",
		"bind 192.168.100.7:443",
		"balance source",
		"option tcp-check",
		"server master-0 192.168.100.10:80 check",
		"server master-1 192.168.100.11:80 check",
		"server master-0 192.168.100.10:443 check",
		"server master-1 192.168.100.11:443 check",
		"frontend ingress-http",
		"frontend ingress-https",
	}
	for _, want := range required {
		if !strings.Contains(cfg, want) {
			t.Errorf("HAProxy config missing %q:\n%s", want, cfg)
		}
	}
}

func TestIngressLBPaths(t *testing.T) {
	paths := IngressLBPaths("lab")
	if paths.Config != "/etc/virtlab/lab-haproxy.cfg" {
		t.Errorf("config path = %q", paths.Config)
	}
	if paths.Unit != "/etc/systemd/system/virtlab-lab-haproxy.service" {
		t.Errorf("unit path = %q", paths.Unit)
	}
	if paths.PID != "/run/virtlab-lab-haproxy.pid" {
		t.Errorf("pid path = %q", paths.PID)
	}
	if paths.Socket != "/run/virtlab-lab-haproxy.sock" {
		t.Errorf("socket path = %q", paths.Socket)
	}
	if got := IngressLBServiceName("lab"); got != "virtlab-lab-haproxy.service" {
		t.Errorf("service name = %q", got)
	}
}

func TestBuildIngressLBSetupScript(t *testing.T) {
	script, err := BuildIngressLBSetupScript("global\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	required := []string{
		`BR="$1"`,
		`ip addr replace "${VIP}/${PREFIX}" dev "$BR"`,
		"VIRTLAB_HAPROXY_CFG",
		"ExecStart=/usr/sbin/haproxy -Ws -f ${CFG} -p ${PID}",
		`ExecReload=/bin/kill -USR2 \$MAINPID`,
		`systemctl enable "$SVC"`,
		`systemctl restart "$SVC"`,
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("setup script missing %q:\n%s", want, script)
		}
	}

	if _, err := BuildIngressLBSetupScript("contains VIRTLAB_HAPROXY_CFG marker"); err == nil {
		t.Fatal("expected an error when the config contains the heredoc marker")
	}
}

func TestBuildIngressLBTeardownScript(t *testing.T) {
	script := BuildIngressLBTeardownScript()
	required := []string{
		`systemctl disable --now "$SVC"`,
		`rm -f "$CFG" "$UNIT" "$PID" "$SOCK"`,
		"--remove-port=80/tcp",
		"--remove-port=443/tcp",
		"firewall-cmd --reload",
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("teardown script missing %q:\n%s", want, script)
		}
	}
}
