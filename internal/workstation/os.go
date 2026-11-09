package workstation

import (
	"os/exec"
	"runtime"
	"strings"
)

// OSType represents the detected Workstation operating system family.
type OSType string

const (
	OSDarwin OSType = "darwin"
	OSLinux  OSType = "linux"
	OSOther  OSType = "other"
)

// Info holds Workstation operating system details.
type Info struct {
	Type         OSType
	Distro       string // e.g. "fedora", "rhel", "ubuntu", or "macos"
	Architecture string // e.g. "amd64", "arm64"
}

// DetectOS inspects the local Workstation environment.
func DetectOS() Info {
	info := Info{
		Architecture: runtime.GOARCH,
	}

	switch runtime.GOOS {
	case "darwin":
		info.Type = OSDarwin
		info.Distro = "macos"
	case "linux":
		info.Type = OSLinux
		info.Distro = detectLinuxDistro()
	default:
		info.Type = OSOther
		info.Distro = runtime.GOOS
	}

	return info
}

func detectLinuxDistro() string {
	// Query os-release
	out, err := exec.Command("sh", "-c", ". /etc/os-release 2>/dev/null && echo $ID").Output()
	if err == nil {
		id := strings.ToLower(strings.TrimSpace(string(out)))
		if id != "" {
			return id
		}
	}
	return "linux"
}
