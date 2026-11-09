package host

import (
	"testing"
)

func TestParseBridgeProbeOutput(t *testing.T) {
	t.Run("Reuses existing network bridge", func(t *testing.T) {
		out := `EXISTING:virbr-lab5
INTERFACES:
eth0
virbr-lab0
virbr-lab1
LIBVIRT_BRIDGES:
virbr-lab0
`
		got, err := parseBridgeProbeOutput(out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "virbr-lab5" {
			t.Errorf("want virbr-lab5, got %s", got)
		}
	})

	t.Run("Allocates virbr-lab0 when host is clean", func(t *testing.T) {
		out := `EXISTING:
INTERFACES:
eth0
lo
virbr0
LIBVIRT_BRIDGES:
virbr0
`
		got, err := parseBridgeProbeOutput(out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "virbr-lab0" {
			t.Errorf("want virbr-lab0, got %s", got)
		}
	})

	t.Run("Allocates virbr-lab1 when virbr-lab0 is taken in interfaces", func(t *testing.T) {
		out := `EXISTING:
INTERFACES:
eth0
lo
virbr-lab0
LIBVIRT_BRIDGES:
`
		got, err := parseBridgeProbeOutput(out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "virbr-lab1" {
			t.Errorf("want virbr-lab1, got %s", got)
		}
	})

	t.Run("Allocates virbr-lab2 when 0 and 1 are taken in libvirt", func(t *testing.T) {
		out := `EXISTING:
INTERFACES:
eth0
LIBVIRT_BRIDGES:
virbr-lab0
virbr-lab1
`
		got, err := parseBridgeProbeOutput(out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "virbr-lab2" {
			t.Errorf("want virbr-lab2, got %s", got)
		}
	})

	t.Run("Fills lowest available gap (0 and 2 taken -> chooses 1)", func(t *testing.T) {
		out := `EXISTING:
INTERFACES:
virbr-lab0
virbr-lab2
LIBVIRT_BRIDGES:
`
		got, err := parseBridgeProbeOutput(out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "virbr-lab1" {
			t.Errorf("want virbr-lab1, got %s", got)
		}
	})
}
