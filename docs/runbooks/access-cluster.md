# SOP: Access the cluster

> **Purpose:** give the laptop an authenticated `oc` session against the
> running single-node cluster.
> **Risk:** low — no cluster mutation; only credentials on the laptop are
> touched.
> **Success:** `oc get nodes` lists `master-0` as `Ready`.

## Prereqs
- [`bring-up-lab.md`](bring-up-lab.md) complete and the acceptance gate green.
- [`configure-laptop-dns.md`](configure-laptop-dns.md) complete (the API name must resolve).
- `oc` installed on the laptop at the pinned version.

## Steps

1. **Copy the kubeconfig from the host:**

   ```bash
   scp root@<host>:/home/libvirt/demo/cluster/auth/kubeconfig \
       ~/.kube/config.demo
   ```

   `Expected:` the file is transferred.

2. **Use it for this shell:**

   ```bash
   export KUBECONFIG=~/.kube/config.demo
   ```

   `Expected:` no output.

3. **Log in as the install-time administrator:**

   ```bash
   oc login -u kubeadmin https://api.demo.example.com:6443
   ```

   `Expected:` `Login successful`.

4. **Get the password when prompted** from the host:

   ```bash
   ssh root@<host> 'cat /home/libvirt/demo/cluster/auth/kubeadmin-password'
   ```

   `Expected:` the password; it is also the `kubeadminPasswordPath` stack
   output.

## Verify

- `oc whoami` → `kubeadmin`.
- `oc get nodes` → `master-0` with `Ready`.
- `oc get clusterversion` → the installed version.
- `oc get co` → every operator `True` for Available.

## Troubleshooting

- **`connection refused` / name does not resolve** → the laptop resolver or
  the tailnet route is missing; see [`configure-laptop-dns.md`](configure-laptop-dns.md).
- **`x509: certificate is valid for …`** → you reached the wrong endpoint;
  confirm the API name and that `/etc/resolver/example.com` points at the host.
- **`Login failed (401 Unauthorized)`** → wrong password; read it from the
  host path above, not from an old copy.
- **`oc` cannot find a config** → `KUBECONFIG` is not exported in this shell;
  repeat step 2.

## Never do

- **Never commit the kubeconfig or the kubeadmin password** to the repo.
- **Never delete `auth/kubeconfig` on the host** — the bootstrap resume guard
  uses it to adopt a healthy cluster.
- **Never use `kubeadmin` for routine work**; create named users for day-2.
- **Never copy credentials into Pulumi config or stack state.**

## Related

- Outputs: `main.go` (`kubeconfigPath`, `kubeadminPasswordPath`, `apiUrl`).
- Install tree: `internal/cluster/openshift.go`.
- Day-2 recovery: [`recover-failed-install.md`](recover-failed-install.md).
