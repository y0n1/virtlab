# SOP: Prepare the host VM

> **Purpose:** turn a fresh CentOS Stream 9 VM into a libvirt host that can
> run the lab guest, serve NTP, and forward the Lab Network subnet.
> **Risk:** medium — a missing package, disabled forwarding, or thin disk
> fails `virtlab up` after the long install has already started.
> **Success:** `libvirtd` and `chronyd` active, `net.ipv4.ip_forward` is `1`,
> and `/home/libvirt` meets the free-space floor.

## Prereqs
- VMware (or equivalent) console access to create and snapshot the host VM.
- A fresh CentOS Stream 9 guest with root SSH access and outbound internet.
- A host address chosen; examples use `hypervisor.example.com`.

## Steps

1. **Install the base packages** (as root on the host):

   ```bash
   dnf install -y libvirt libvirt-client qemu-kvm qemu-img dnsmasq chrony \
     nmstate python3 virt-install
   ```

   `Expected:` exit 0.

2. **Enable libvirtd and prove the client works:**

   ```bash
   systemctl enable --now libvirtd
   virsh version
   ```

   `Expected:` `virsh` prints the libvirt and hypervisor versions.

3. **Enable chrony and confirm it serves UDP :123:**

   ```bash
   systemctl enable --now chronyd
   systemctl is-enabled chronyd && systemctl is-active chronyd
   ss -lun | grep -q ':123 ' && echo "chronyd serving UDP :123"
   ```

   `Expected:` `enabled`, `active`, `chronyd serving UDP :123`.

4. **Enable IPv4 forwarding** (the Lab Network route needs it):

   ```bash
   printf 'net.ipv4.ip_forward=1\n' >/etc/sysctl.d/99-demo-forward.conf
   sysctl --system
   sysctl -n net.ipv4.ip_forward
   ```

   `Expected:` `1`.

5. **Check disk headroom** for the guest disk, ISO and install tree:

   ```bash
   df -BG --output=avail /home/libvirt
   ```

   `Expected:` at least the total disk required by the selected preset
   (`internal/config/presets.go`).

Phase 1 of `virtlab up` installs the same package set and enables the same
services. This SOP is the manual preflight and the fallback when that step
fails.

## Verify

- `systemctl is-active libvirtd chronyd` → `active` twice.
- `sysctl -n net.ipv4.ip_forward` → `1`.
- `df -BG --output=avail /home/libvirt` → at or above the floor.

## Troubleshooting

- **`virsh` cannot connect to `qemu:///system`** → `systemctl status libvirtd`;
  on a nested VM confirm KVM is exposed to the guest.
- **`chronyd` active but `:123` not listening** → check for a conflicting NTP
  daemon; the guest uses this host bridge as its additional NTP source.
- **`/home/libvirt` below the floor** → grow `/home` or move the pool before
  bringing up the stack; a mid-install ENOSPC does not resume cleanly.

## Never do

- **Never disable IPv4 forwarding** — the Workstation reaches the Guests only
  through the Host's forwarding (owner decision: manual step).
- **Never run `virtlab up` below the free-space floor** — the volume and ISO
  are written before the install can fail.
- **Never hand-edit the libvirt network or pool the stack owns** — `up`
  reconciles them and a divergent object blocks adoption.

## Related

- Values and defaults: `config.yaml` (`preset`, `controlPlaneOverrides`,
  `workerOverrides`) and `internal/config/presets.go`.
- Preflight tool list: `internal/cluster/openshift.go`.
- Free-space floor: the selected preset's total disk (`internal/config/presets.go`).
- Next: [`grant-laptop-host-access.md`](grant-laptop-host-access.md).
