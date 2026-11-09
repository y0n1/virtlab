package host

import (
	"strings"
	"testing"
)

func TestParseSubnetProbeOutput(t *testing.T) {
	t.Run("Reuses existing network subnet and gateway", func(t *testing.T) {
		out := `EXISTING_IP:192.168.135.1
EXISTING_NETMASK:255.255.255.0
ROUTES:
192.168.1.0/24
ADDRESSES:
192.168.1.50/24
LIBVIRT_SUBNETS:
`
		cidr, gw, err := parseSubnetProbeOutput(out, "my-net", "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cidr != "192.168.135.0/24" {
			t.Errorf("want CIDR 192.168.135.0/24, got %s", cidr)
		}
		if gw != "192.168.135.1" {
			t.Errorf("want gateway 192.168.135.1, got %s", gw)
		}
	})

	t.Run("Allocates 192.168.100.0/24 when clean", func(t *testing.T) {
		out := `EXISTING_IP:
EXISTING_NETMASK:
ROUTES:
192.168.1.0/24
192.168.122.0/24
ADDRESSES:
192.168.1.100/24
LIBVIRT_SUBNETS:
192.168.122.1/255.255.255.0
`
		cidr, gw, err := parseSubnetProbeOutput(out, "my-net", "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cidr != "192.168.100.0/24" {
			t.Errorf("want CIDR 192.168.100.0/24, got %s", cidr)
		}
		if gw != "192.168.100.1" {
			t.Errorf("want gateway 192.168.100.1, got %s", gw)
		}
	})

	t.Run("Skips 192.168.100.0/24 when taken, chooses 101", func(t *testing.T) {
		out := `EXISTING_IP:
EXISTING_NETMASK:
ROUTES:
192.168.100.0/24
ADDRESSES:
LIBVIRT_SUBNETS:
`
		cidr, gw, err := parseSubnetProbeOutput(out, "my-net", "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cidr != "192.168.101.0/24" {
			t.Errorf("want CIDR 192.168.101.0/24, got %s", cidr)
		}
		if gw != "192.168.101.1" {
			t.Errorf("want gateway 192.168.101.1, got %s", gw)
		}
	})

	t.Run("Skips multiple taken subnets", func(t *testing.T) {
		out := `EXISTING_IP:
EXISTING_NETMASK:
ROUTES:
192.168.100.0/24
192.168.101.0/24
192.168.102.0/24
ADDRESSES:
LIBVIRT_SUBNETS:
`
		cidr, gw, err := parseSubnetProbeOutput(out, "my-net", "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cidr != "192.168.103.0/24" {
			t.Errorf("want CIDR 192.168.103.0/24, got %s", cidr)
		}
		if gw != "192.168.103.1" {
			t.Errorf("want gateway 192.168.103.1, got %s", gw)
		}
	})

	t.Run("Custom CIDR override without conflict", func(t *testing.T) {
		out := `EXISTING_IP:
EXISTING_NETMASK:
ROUTES:
192.168.1.0/24
ADDRESSES:
LIBVIRT_SUBNETS:
`
		cidr, gw, err := parseSubnetProbeOutput(out, "my-net", "10.10.0.0/24", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cidr != "10.10.0.0/24" {
			t.Errorf("want CIDR 10.10.0.0/24, got %s", cidr)
		}
		if gw != "10.10.0.1" {
			t.Errorf("want gateway 10.10.0.1, got %s", gw)
		}
	})

	t.Run("Custom CIDR override with conflict produces error", func(t *testing.T) {
		out := `EXISTING_IP:
EXISTING_NETMASK:
ROUTES:
10.10.0.0/16
ADDRESSES:
LIBVIRT_SUBNETS:
`
		_, _, err := parseSubnetProbeOutput(out, "my-net", "10.10.5.0/24", false)
		if err == nil {
			t.Fatal("expected conflict error, got nil")
		}
		if !strings.Contains(err.Error(), "conflicts with existing host route/subnet") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
