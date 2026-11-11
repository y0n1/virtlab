package infrastructure

import (
	"fmt"

	"github.com/pulumi/pulumi-terraform-provider/sdks/go/libvirt/libvirt"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

// GuestNode represents a declared virtual machine and its underlying storage volumes.
type GuestNode struct {
	Lease   NodeLease
	Domain  *libvirt.Domain
	Volume  *libvirt.Volume   // Primary (root OS) storage volume (/dev/vda)
	Volumes []*libvirt.Volume // All storage volumes attached to the guest in device order
}

// BuildGuests creates the qcow2 volumes and shut-off domains for all cluster nodes in the dedicated pool.
// Lifecycle rule: Domains are created with Running=false; bootstrap phase coordinates booting.
func BuildGuests(ctx *pulumi.Context, cfg *config.Config, pool *libvirt.Pool, net *libvirt.Network) ([]*GuestNode, error) {
	topo, err := cfg.EffectiveTopology()
	if err != nil {
		return nil, err
	}

	leases, err := ComputeNodeLeases(cfg)
	if err != nil {
		return nil, err
	}

	var guests []*GuestNode

	for _, lease := range leases {
		settings := topo.ControlPlaneSettings
		if lease.Role == "worker" {
			settings = topo.WorkerSettings
		}

		disks := settings.ResolvedDisks()
		if len(disks) == 0 {
			return nil, fmt.Errorf("infrastructure: node '%s' has no disks configured", lease.Name)
		}
		if len(disks) > config.MaxVirtioDisks {
			return nil, fmt.Errorf("infrastructure: node '%s' configures %d disks, maximum supported is %d", lease.Name, len(disks), config.MaxVirtioDisks)
		}

		domName := cfg.DomainName(lease.Name)
		serialLog := fmt.Sprintf("%s/%s-serial.log", cfg.LogsDir(), lease.Name)

		// 1. Storage Volumes
		volumes := make([]*libvirt.Volume, 0, len(disks))
		domainDisks := make(libvirt.DomainDevicesDiskArray, 0, len(disks)+1)
		depends := make([]pulumi.Resource, 0, len(disks)+1)

		for i, d := range disks {
			dev := fmt.Sprintf("vd%c", 'a'+i)
			volResName := fmt.Sprintf("vol-%s", lease.Name)
			volName := fmt.Sprintf("%s.qcow2", domName)
			if i > 0 {
				volResName = fmt.Sprintf("vol-%s-%s", lease.Name, dev)
				volName = fmt.Sprintf("%s-%s.qcow2", domName, dev)
			}
			diskPath := fmt.Sprintf("%s/%s", cfg.StoragePoolDir(), volName)

			vol, err := libvirt.NewVolume(ctx, volResName, &libvirt.VolumeArgs{
				Pool:         pool.Name,
				Name:         pulumi.String(volName),
				Capacity:     pulumi.Float64(float64(d.SizeGiB)),
				CapacityUnit: pulumi.String("GiB"),
				Target: &libvirt.VolumeTargetArgs{
					Format: &libvirt.VolumeTargetFormatArgs{Type: pulumi.String("qcow2")},
				},
			}, pulumi.DependsOn([]pulumi.Resource{pool}))
			if err != nil {
				return nil, fmt.Errorf("infrastructure: create volume '%s': %w", volName, err)
			}

			volumes = append(volumes, vol)
			depends = append(depends, vol)
			domainDisks = append(domainDisks, &libvirt.DomainDevicesDiskArgs{
				Device: pulumi.String("disk"),
				Driver: &libvirt.DomainDevicesDiskDriverArgs{
					Name:  pulumi.String("qemu"),
					Type:  pulumi.String("qcow2"),
					Cache: pulumi.String("writeback"),
					Io:    pulumi.String("threads"),
				},
				Source: &libvirt.DomainDevicesDiskSourceArgs{
					File: &libvirt.DomainDevicesDiskSourceFileArgs{
						File: pulumi.String(diskPath),
					},
				},
				Target: &libvirt.DomainDevicesDiskTargetArgs{
					Dev: pulumi.String(dev),
					Bus: pulumi.String("virtio"),
				},
			})
		}

		// CD-ROM drive (source-less until bootstrap attaches agent ISO)
		domainDisks = append(domainDisks, &libvirt.DomainDevicesDiskArgs{
			Device:   pulumi.String("cdrom"),
			Driver:   &libvirt.DomainDevicesDiskDriverArgs{Name: pulumi.String("qemu")},
			ReadOnly: pulumi.Bool(true),
			Target: &libvirt.DomainDevicesDiskTargetArgs{
				Dev: pulumi.String("sda"),
				Bus: pulumi.String("sata"),
			},
		})
		depends = append(depends, net)

		// 2. Libvirt Domain
		dom, err := libvirt.NewDomain(ctx, domName, &libvirt.DomainArgs{
			Name:              pulumi.String(domName),
			Type:              pulumi.String("kvm"),
			Autostart:         pulumi.Bool(true),
			Running:           pulumi.Bool(false), // Boot owned by bootstrap phase
			Memory:            pulumi.Float64(float64(settings.MemoryMiB)),
			MemoryUnit:        pulumi.String("MiB"),
			CurrentMemory:     pulumi.Float64(float64(settings.MemoryMiB)),
			CurrentMemoryUnit: pulumi.String("MiB"),
			Vcpu:              pulumi.Float64(float64(settings.VCPUs)),
			Cpu: &libvirt.DomainCpuArgs{
				Mode:  pulumi.String("host-passthrough"),
				Check: pulumi.String("none"),
			},
			Os: &libvirt.DomainOsArgs{
				Type:        pulumi.String("hvm"),
				TypeArch:    pulumi.String("x86_64"),
				TypeMachine: pulumi.String("pc-q35-rhel9.8.0"),
				BootDevices: libvirt.DomainOsBootDeviceArray{
					&libvirt.DomainOsBootDeviceArgs{Dev: pulumi.String("hd")},
					&libvirt.DomainOsBootDeviceArgs{Dev: pulumi.String("cdrom")},
				},
			},
			Features: &libvirt.DomainFeaturesArgs{
				Acpi: pulumi.Bool(true),
				Apic: &libvirt.DomainFeaturesApicArgs{},
			},
			Clock: &libvirt.DomainClockArgs{
				Offset: pulumi.String("utc"),
				Timers: libvirt.DomainClockTimerArray{
					&libvirt.DomainClockTimerArgs{Name: pulumi.String("rtc"), TickPolicy: pulumi.String("catchup")},
					&libvirt.DomainClockTimerArgs{Name: pulumi.String("pit"), TickPolicy: pulumi.String("delay")},
					&libvirt.DomainClockTimerArgs{Name: pulumi.String("hpet"), Present: pulumi.String("no")},
				},
			},
			Devices: &libvirt.DomainDevicesArgs{
				Emulator: pulumi.String("/usr/libexec/qemu-kvm"),
				Disks:    domainDisks,
				Interfaces: libvirt.DomainDevicesInterfaceArray{
					&libvirt.DomainDevicesInterfaceArgs{
						Mac:   &libvirt.DomainDevicesInterfaceMacArgs{Address: pulumi.String(lease.MAC)},
						Model: &libvirt.DomainDevicesInterfaceModelArgs{Type: pulumi.String("virtio")},
						Source: &libvirt.DomainDevicesInterfaceSourceArgs{
							Network: &libvirt.DomainDevicesInterfaceSourceNetworkArgs{
								Network: pulumi.String(cfg.Network.Name),
							},
						},
					},
				},
				Consoles: libvirt.DomainDevicesConsoleArray{
					&libvirt.DomainDevicesConsoleArgs{
						Source: &libvirt.DomainDevicesConsoleSourceArgs{
							File: &libvirt.DomainDevicesConsoleSourceFileArgs{Path: pulumi.String(serialLog)},
						},
						Target: &libvirt.DomainDevicesConsoleTargetArgs{
							Type: pulumi.String("serial"),
							Port: pulumi.Float64(0),
						},
					},
				},
			},
		}, pulumi.DependsOn(depends))
		if err != nil {
			return nil, fmt.Errorf("infrastructure: create domain '%s': %w", domName, err)
		}

		guests = append(guests, &GuestNode{
			Lease:   lease,
			Domain:  dom,
			Volume:  volumes[0],
			Volumes: volumes,
		})
	}

	return guests, nil
}
