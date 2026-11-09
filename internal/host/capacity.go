package host

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/y0n1/virtlab/internal/config"
)

// InspectCapacity connects to the Host via SSH and reads hardware limits (vCPUs, RAM, Disk).
func (c *SSHClient) InspectCapacity() (config.HostCapacity, error) {
	script := `
set -e
# 1. vCPUs
vcpus=$(nproc)

# 2. RAM (Total and Available in MiB)
ram=$(awk '/MemTotal/ {total=$2} /MemAvailable/ {avail=$2} END {print int(total/1024), int(avail/1024)}' /proc/meminfo)

# 3. Disk (/home/libvirt or /home in GiB)
disk_target="/home/libvirt"
if [ ! -d "$disk_target" ]; then disk_target="/home"; fi
disk=$(df -BG "$disk_target" | tail -1 | awk '{gsub(/G/,""); print $2, $4}')

echo "${vcpus}|${ram}|${disk}"
`
	out, err := c.RunScript(script)
	if err != nil {
		return config.HostCapacity{}, fmt.Errorf("inspect host capacity: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) != 3 {
		return config.HostCapacity{}, fmt.Errorf("unexpected capacity output format: '%s'", out)
	}

	vcpus, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return config.HostCapacity{}, fmt.Errorf("parse vcpus '%s': %w", parts[0], err)
	}

	ramFields := strings.Fields(parts[1])
	if len(ramFields) < 2 {
		return config.HostCapacity{}, fmt.Errorf("parse ram fields '%s'", parts[1])
	}
	totalRAM, _ := strconv.Atoi(ramFields[0])
	availRAM, _ := strconv.Atoi(ramFields[1])

	diskFields := strings.Fields(parts[2])
	if len(diskFields) < 2 {
		return config.HostCapacity{}, fmt.Errorf("parse disk fields '%s'", parts[2])
	}
	totalDisk, _ := strconv.Atoi(diskFields[0])
	availDisk, _ := strconv.Atoi(diskFields[1])

	return config.HostCapacity{
		VCPUs:            vcpus,
		TotalRAMMiB:      totalRAM,
		AvailableRAMMiB:  availRAM,
		TotalDiskGiB:     totalDisk,
		AvailableDiskGiB: availDisk,
	}, nil
}
