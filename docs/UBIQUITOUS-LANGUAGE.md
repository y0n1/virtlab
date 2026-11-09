# Ubiquitous Language

This document defines the formal domain language for this project. Domain-Driven Design (DDD) requires a single, shared, unambiguous glossary used identically by engineers, operators, agents, CLI output, configuration files, and documentation.

---

## 1. Domain Entities & Roles

| Term | Definition | Anti-Patterns (Do NOT use) |
|---|---|---|
| **Workstation** | The operator's local development machine (macOS or Fedora Linux) running the `virtlab` CLI. | *Laptop, client, client machine, local PC* |
| **Host** | The remote KVM/libvirt hypervisor machine (CentOS Stream 9 / RHEL 9) reachable from the Workstation via SSH. | *Server, target VM, hypervisor VM, remote machine* |
| **Guest** / **Node** | A virtual machine running on the Host as an active member of the OpenShift cluster. | *VM, instance, box* |
| **Control Plane Node** | A cluster Node running `etcd`, the Kubernetes API server, and core controllers. Schedulable for workloads in `sno` and `compact` presets. | *Master, master node, leader* |
| **Worker Node** | A dedicated compute Node running application workloads (only present in the `ha` preset). | *Compute node, slave, worker VM* |

---

## 2. Infrastructure & Networking Primitives

| Term | Definition | Anti-Patterns (Do NOT use) |
|---|---|---|
| **Lab Network** | The isolated libvirt virtual network and Linux bridge (`virbr-lab<N>`) bound to the configured subnet (e.g. `192.168.100.0/24`). Bridge device names are managed automatically and cannot be overridden. | *Default network, bridge, virbr0* |
| **Lab Pool** | The isolated libvirt directory storage pool located strictly at `/home/libvirt/<cluster-name>/pool`. | *Default pool, storage, disk directory* |
| **Lab Tunnel** | The transparent TCP and split-DNS forwarding tunnel established by `sshuttle` from the Workstation to the Host. | *VPN, tailnet, Tailscale, SOCKS proxy* |
| **Split-DNS** | Targeted DNS resolution directing queries for `<baseDomain>` to the Host's libvirt gateway (`192.168.100.1:53`) while leaving all other Workstation traffic untouched. | *Global DNS, DNS hijacking* |
| **API VIP** | A single address, published as `api`/`api-int`, that the cluster itself owns: keepalived on the Control Plane Nodes holds it and fails over if the holder dies. Present only in **non-SNO** presets; SNO publishes the node's own lease instead. | *Load balancer IP, master IP, HAProxy API VIP* |
| **Ingress VIP** | The cluster-owned address declared in `ingressVIPs`. `platform: baremetal` requires it (the installer rejects mismatched `apiVIPs`/`ingressVIPs` lengths), but it is **reserved and unused by external clients** — `*.apps` resolves to the Ingress LB VIP instead. | *apps VIP, wildcard VIP* |
| **Ingress LB VIP** | The address owned by the Host-side Ingress Load Balancer and the answer for `*.apps.<cluster>.<baseDomain>` in **non-SNO** presets. | *apps IP, ingress IP, wildcard IP* |
| **Ingress Load Balancer** | The `HAProxy` instance `virtlab` runs on the Host, bound to the Ingress LB VIP, spreading `*.apps` traffic across every node running a router. Unlike the API VIP it is active/active, so the router set can grow with the cluster. | *Router LB, cluster LB, VIP manager* |
| **Platform** | The immutable `platform` stanza of `install-config.yaml`: `none` for `sno`, `baremetal` for `compact` and `ha`. Changing it requires a full reinstall. | *provider, cloud type* |

### Addressing plan

Every address is derived from the resolved **Lab Network** CIDR by offset and can
be overridden in `config.yaml` (`network.apiVip`, `network.ingressVip`,
`network.ingressLbVip`). Overrides are validated: IPv4, inside the CIDR, not the
network/broadcast address, not the gateway, not a node lease, and outside the
DHCP pool.

| Name | Default offset | Published as | Used by | Present in |
|---|---|---|---|---|
| Gateway | `.1` | — | Guests' default route and DNS | all presets |
| API VIP | `.5` | `api`, `api-int` | `oc`, API clients | `compact`, `ha` |
| Ingress VIP | `.6` | — (reserved) | the cluster's own keepalived | `compact`, `ha` |
| Ingress LB VIP | `.7` | `*.apps.<cluster>.<baseDomain>` | the console and application routes | `compact`, `ha` |
| Control-plane leases | `.10`–`.12` | `<node>.<cluster>.<baseDomain>` | node addresses | all presets |
| Worker leases | `.20`–`.21` | `<node>.<cluster>.<baseDomain>` | node addresses | `ha` |
| DHCP pool | `.100`–`.200` | — | hand-attached guests | all presets |

In `sno`, `api`, `api-int` and `*.apps` all resolve to the single node's lease
and no VIP is used.

---

## 3. Presets & Sizing

A **Preset** is a predefined OpenShift cluster topology defining node counts and minimum sizing based on official Red Hat requirements (without preinstalled optional operators):

| Preset | Role Breakdown | Min vCPU / Node | Min RAM / Node | Min Disk / Node | Total Cluster Min |
|---|---|---|---|---|---|
| **`sno`** | 1 Control Plane Node (schedulable) | 8 vCPUs | 16 GiB | 120 GiB | 8 vCPUs, 16 GiB, 120 GiB |
| **`compact`** | 3 Control Plane Nodes (schedulable) | 4 vCPUs | 16 GiB | 100 GiB | 12 vCPUs, 48 GiB, 300 GiB |
| **`ha`** | 3 Control Plane Nodes + 2 Worker Nodes | CP: 4 vCPUs<br>Worker: 2 vCPUs | CP: 16 GiB<br>Worker: 8 GiB | CP: 100 GiB<br>Worker: 100 GiB | 16 vCPUs, 64 GiB, 500 GiB |

*Note: Sizing can be overridden in `config.yaml`, but any configured value below the official minimum will generate an explicit warning.*

---

## 4. Lifecycle Phases

The lifecycle is divided into 4 sequential **Phases**:

1. **Phase 1: Preparation (Host & Workstation)**
   * Installs and verifies prerequisites on both the Workstation (macOS or Fedora Linux) and the Host.
   * Validates host capacity against requested cluster requirements (hard error on RAM/Disk exhaustion; warning on vCPU overcommit).
   * Prepares Host base packages and delivers the pull secret.
2. **Phase 2: Setup (Lab Infrastructure)**
   * Provisions the isolated **Lab Pool** at `/home/libvirt/<cluster-name>/pool`.
   * Provisions the isolated **Lab Network** (`virbr-lab<N>`).
   * Provisions the **Guest** domains and disks in an interconnected, powered-off state.
   * In **non-SNO** presets, prepares VIP networking: disables IGMP snooping on the
     bridge (so VRRP multicast reaches every Guest), assigns the **Ingress LB VIP**
     to the bridge, and starts the **Ingress Load Balancer**.
3. **Phase 3: Installation (OpenShift Cluster)**
   * Generates OpenShift Agent-based installer manifests (`install-config.yaml`, `agent-config.yaml`).
     `compact` and `ha` install as **Platform** `baremetal` with `apiVIPs`/`ingressVIPs`;
     `sno` installs as `none`.
   * Builds the bootable Agent ISO.
   * Attaches the ISO to the Guests, powers them on, and monitors bootstrap progress until `install-complete`.
   * Reconciles the default `IngressController` to one router per ingress-capable
     node, so the **Ingress Load Balancer** has a backend on every node.
4. **Phase 4: Verification**
   * Establishes the **Lab Tunnel** (`sshuttle`) and configures Workstation **Split-DNS** to connect to the active cluster.
   * Executes end-to-end connectivity checks from the Workstation across the Lab Tunnel, including that
     `api`/`api-int` resolve to the **API VIP** (or the node in `sno`) and the console
     name resolves to the **Ingress LB VIP** (or the node in `sno`).
   * Confirms `oc` authentication, that every expected Node is `Ready`, and web
     console accessibility (`200 OK` or the expected `302`).

---

## 5. Operations & Commands

| Action | Command | Purpose |
|---|---|---|
| **Up** | `virtlab up` | Executes Phases 1 through 4 end-to-end to create and verify the lab. |
| **Down** | `virtlab down` | Completely destroys the OpenShift cluster, Guests, Lab Network, and Lab Pool, and reverses all Workstation and Host configurations. |
| **Status** | `virtlab status` | Inspects and displays the current state of the Workstation tunnel, Host prerequisites, Guests, and OpenShift cluster. |

---

## 6. Core Rules & Invariants

1. **Isolation:** The `default` libvirt storage pool and `default` libvirt network must NEVER be used or altered. All VM disk storage belongs strictly in `/home/libvirt/<cluster-name>/pool`.
2. **Reversibility:** Anything `virtlab up` configures (especially Workstation `/etc/resolver`, `/etc/hosts`, or local packages), `virtlab down` must completely undo.
3. **Explicit Microcopy:** The `virtlab` tool must never silently modify Workstation files. It must print brief, clear explanations of what it is doing and why before asking for credentials or applying changes.

