package host

import (
	"strings"
	"testing"
)

func TestValidateBridgeDeviceName(t *testing.T) {
	valid := []string{"virbr-lab0", "virbr-lab12", "br0", "br_lab.1"}
	for _, name := range valid {
		if err := ValidateBridgeDeviceName(name); err != nil {
			t.Errorf("expected %q to be valid, got %v", name, err)
		}
	}

	invalid := []string{"", "-br0", "../x", "br0/evil", "br0 evil", "br..0", "averylongbridgename1"}
	for _, name := range invalid {
		if err := ValidateBridgeDeviceName(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestBuildDisableIGMPSnoopingScript(t *testing.T) {
	script := BuildDisableIGMPSnoopingScript()
	required := []string{
		`BR="$1"`,
		`/sys/class/net/${BR}/bridge/multicast_snooping`,
		`echo 0 >"$SYSFS"`,
		`echo "SKIP:`,
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}
