# virtlab

[![CI](https://github.com/y0n1/virtlab/actions/workflows/ci.yml/badge.svg)](https://github.com/y0n1/virtlab/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/y0n1/virtlab?include_prereleases)](https://github.com/y0n1/virtlab/releases)

**virtlab** is an automated, declarative CLI tool that provisions OpenShift clusters on remote Linux KVM/libvirt hypervisors, seamlessly connecting your local workstation to the cluster without manual VPN or DNS setup.

---

## Features

- **Multi-Topology Presets**: Deploy Single Node OpenShift (**SNO**), 3-node **Compact**, or 5-node **HA** (3 control plane + 2 workers) with validated hardware sizing.
- **Zero-Friction Workstation Connectivity**: Automatically manages split-DNS (`/etc/resolver` on macOS, systemd-resolved on Linux) and background `sshuttle` tunneling directly into the isolated lab subnet.
- **Declarative Infrastructure**: Uses Pulumi Automation API with the `libvirt` provider to define dedicated storage pools, isolated virtual networks, and guest VMs.
- **Unattended Agent-Based Installation**: Automatically renders `install-config` and `agent-config`, triggers the OpenShift agent-based installer and monitors bootstrap progress.
- **End-to-End Verification & Kubeconfig Sync**: Verifies split-DNS, probes `/healthz` and Web Console endpoints, and drops a ready-to-use kubeconfig onto your workstation (`~/.kube/config.<clusterName>`).
- **Clean Teardown**: Reverts local DNS configs, terminates tunnels, and purges remote VMs and storage volumes in one command.

---

## Architecture

```text
Local Workstation (macOS / Linux)
  │
  ├── split-DNS (/etc/resolver or systemd-resolved) ──▶ queries routed to lab gateway
  ├── sshuttle tunnel (background daemon) ────────────▶ routes lab CIDR via SSH
  │
  ▼ SSH / Tunnel
Remote Hypervisor (Linux / KVM / libvirt)
  └── virbr-labN (e.g., 192.168.100.0/24)
        ├── Gateway / dnsmasq (192.168.100.1)
        └── Guest VM(s) running OpenShift 4.20+
              ├── api.<cluster>.<domain>:6443
              └── console-openshift-console.apps.<cluster>.<domain>
```

### 4-Phase Provisioning Lifecycle

1. **Phase 1: Host & Workstation Preparation**: Checks Workstation prerequisites, verifies Host connectivity and capacity, prepares the Host, installs the pinned OpenShift toolchain, and delivers the pull secret.
2. **Phase 2: Lab Infrastructure**: Provisions the dedicated libvirt storage pool, virtual network with static DHCP leases, VM domains, and (in `compact`/`ha`) the Host-side Ingress Load Balancer.
3. **Phase 3: OpenShift Installation**: Generates the agent ISO, powers on the nodes, waits for the installation to complete, ejects the ISO, and reconciles the router replicas.
4. **Phase 4: Verification**: Configures split-DNS and starts the `sshuttle` Lab Tunnel, performs live end-to-end checks (DNS, API health, Web Console), and syncs cluster credentials locally.

---

## Prerequisites

### Workstation
- **Go** (>= 1.26) & **OpenSSH** (>= 8.0)
- `sshuttle` (installed automatically if missing via Homebrew or system package manager)
- OpenShift client CLI (`oc`) for interacting with the cluster

### Remote Host (Hypervisor)
- Linux host running KVM / `libvirtd`
- Passwordless SSH access (`ssh-key`) for the provisioning user
- Red Hat OpenShift pull secret (via `pullSecretFile`, the `VIRTLAB_OCM_TOKEN` environment variable, or an authenticated `ocm` CLI)

---

## Quick Start

### 1. Install `virtlab`

#### Option A: Download Pre-built Binary (Recommended)
Download the tarball matching your OS and architecture from [GitHub Releases](https://github.com/y0n1/virtlab/releases):

```bash
# Specify desired release version (or fetch latest release tag):
VERSION="0.1.0"

# macOS (Apple Silicon arm64)
curl -sSfLO "https://github.com/y0n1/virtlab/releases/download/v${VERSION}/virtlab_${VERSION}_darwin_arm64.tar.gz"
tar -xzf "virtlab_${VERSION}_darwin_arm64.tar.gz" && sudo install -m 0755 virtlab /usr/local/bin/virtlab

# Linux (x86_64 amd64)
curl -sSfLO "https://github.com/y0n1/virtlab/releases/download/v${VERSION}/virtlab_${VERSION}_linux_amd64.tar.gz"
tar -xzf "virtlab_${VERSION}_linux_amd64.tar.gz" && sudo install -m 0755 virtlab /usr/local/bin/virtlab
```

Release archives are also published with unversioned aliases (`virtlab_<os>_<arch>.tar.gz`) for direct `latest/download` links:

```bash
# macOS (Apple Silicon arm64)
curl -sSfLO https://github.com/y0n1/virtlab/releases/latest/download/virtlab_darwin_arm64.tar.gz
tar -xzf virtlab_darwin_arm64.tar.gz && sudo install -m 0755 virtlab /usr/local/bin/virtlab

# Linux (x86_64 amd64)
curl -sSfLO https://github.com/y0n1/virtlab/releases/latest/download/virtlab_linux_amd64.tar.gz
tar -xzf virtlab_linux_amd64.tar.gz && sudo install -m 0755 virtlab /usr/local/bin/virtlab
```

Verify the installation:
```bash
virtlab version
```

#### Option B: Build from Source

```bash
make build
# Binary is built at bin/virtlab
```

The commands below use `virtlab`. If you built from source and did not install
the binary on your `PATH`, run `./bin/virtlab` instead.

### 2. Configure Your Lab

Copy the example configuration to `config.yaml`:

```bash
cp examples/config.example.yaml config.yaml
```

Edit `config.yaml` to specify your hypervisor host, topology preset, and domain:

```yaml
clusterName: "demo"
baseDomain: "example.com"
ocpVersion: "4.22.0"
preset: "sno" # "sno" (1 node) | "compact" (3 nodes) | "ha" (5 nodes)

hostSsh:
  host: "hypervisor.internal"
  user: "root"
  publicKeyFile: "~/.ssh/id_ed25519.pub"

# Optional hardware overrides (uncomment to customize sizing)
# controlPlaneOverrides:
#   vcpus: 32
#   memoryMib: 131072
#   disks:
#     - sizeGib: 200
# workerOverrides:
#   vcpus: 4
#   memoryMib: 8192
#   disks:
#     - sizeGib: 100
#     - sizeGib: 300
#     - sizeGib: 300

# Optional: pull secret file. If omitted, virtlab uses VIRTLAB_OCM_TOKEN when it
# is set, then falls back to the 'ocm' CLI session if logged in.
# pullSecretFile: ""

# Network configuration (optional: virtlab automatically allocates a non-conflicting CIDR)
# network:
#   cidr: "192.168.100.0/24"      # optional override for advanced users
#   # Non-'sno' presets derive three VIPs from the CIDR (.5, .6, .7); override only if they collide:
#   # apiVip: "192.168.100.5"        # api/api-int
#   # ingressVip: "192.168.100.6"    # required by platform: baremetal, reserved
#   # ingressLbVip: "192.168.100.7"  # *.apps, served by HAProxy on the Host
```

> In `compact` and `ha`, virtlab installs with `platform: baremetal`: the API
> answers on a keepalived-backed **API VIP**, and `*.apps` resolves to an
> **Ingress Load Balancer** (HAProxy on the Hypervisor) that spreads traffic
> across every router node. `sno` stays on `platform: none` and needs neither.

#### Non-interactive pull secret (CI and headless hosts)

`virtlab up` can authenticate to OCM with a Red Hat offline token in the
`VIRTLAB_OCM_TOKEN` environment variable. When the variable is set and
`pullSecretFile` is empty, `virtlab` uses the token exclusively: it does not
read an ambient `ocm` session and does not prompt.

Create an offline token at <https://console.redhat.com/openshift/token>.

```yaml
# GitHub Actions: inject the token from a repository secret.
- name: Provision the lab
  env:
    VIRTLAB_OCM_TOKEN: ${{ secrets.VIRTLAB_OCM_TOKEN }}
  run: virtlab up -y
```

`pullSecretFile` takes precedence over `VIRTLAB_OCM_TOKEN`. The variable is an
environment-only input; there is no configuration key. `virtlab` never prints
the token and never passes it on a command line.

### 3. Deploy the Lab

```bash
virtlab up
```

*(Use `virtlab up -y` to run non-interactively without prompt pauses.)*

### 4. Check Lab Status

Inspect running tunnels, hypervisor connectivity, and workstation state:

```bash
virtlab status
```

### 5. Access the Cluster

Once provisioned, your kubeconfig is automatically synced:

```bash
export KUBECONFIG=~/.kube/config.<clusterName>

# Query cluster nodes
oc get nodes

# Access web console
open https://console-openshift-console.apps.<clusterName>.<baseDomain>
```

### 6. Teardown

To destroy the cluster, clean up VMs and storage pools, and revert local DNS / tunnel configs:

```bash
virtlab down
```

---

## Topology Presets

| Preset | Total Nodes | Default Specs per Node | Recommended Hypervisor Headroom |
|---|---|---|---|
| **`sno`** | 1 master | 8 vCPU, 16 GiB RAM, 120 GiB Disk | 12+ vCPUs, 32 GiB RAM, 200 GiB Disk |
| **`compact`** | 3 masters | 4 vCPU, 16 GiB RAM, 100 GiB Disk | 16+ vCPUs, 64 GiB RAM, 400 GiB Disk |
| **`ha`** | 3 masters + 2 workers | Masters: 4 vCPU, 16 GiB RAM<br>Workers: 2 vCPU, 8 GiB RAM | 24+ vCPUs, 96 GiB RAM, 600 GiB Disk |

Hardware parameters can be customized per node role via `controlPlaneOverrides` and `workerOverrides` in `config.yaml`.

### Ingress scaling

In `compact` and `ha`, the Host-side HAProxy load-balances `*.apps` across every
node running a router, and virtlab reconciles the default `IngressController` to
one router per ingress-capable node. Because the LB is active/active (unlike the
keepalived-held API VIP), the ingress set can grow. **Adding a node to an
already-installed cluster is day-2 work that is not automated yet** — it needs
an agent `add-nodes` flow, an HAProxy backend refresh, and an
`IngressController` replica refresh.

---

## License

Apache-2.0 (see [LICENSE](LICENSE))
