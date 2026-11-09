# SOP: Access the cluster

> **Purpose:** give the laptop an authenticated `oc` session against the
> running cluster using the credentials `virtlab up` already synced.
> **Risk:** low — no cluster mutation; only local credentials are read.
> **Success:** `oc get nodes` lists every node as `Ready`.

## Prereqs
- [`bring-up-lab.md`](bring-up-lab.md) complete; Phase 4 finished.
- `oc` installed on the laptop.
- The laptop can resolve the cluster zone (Phase 4 configures split-DNS and
  sshuttle; see [`configure-laptop-dns.md`](configure-laptop-dns.md) and
  [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md) only
  when diagnosing).

## Steps

1. **Point `oc` at the synced kubeconfig:**

   ```bash
   export KUBECONFIG=~/.kube/config.<clusterName>
   ```

   `Expected:` no output. The file is written with `0600` permissions by
   Phase 4.

2. **Confirm admin access:**

   ```bash
   oc get nodes
   ```

   `Expected:` every node listed as `Ready`.

3. **Log in interactively only when you need a fresh session:**

   ```bash
   oc login -u kubeadmin https://api.<clusterName>.<baseDomain>:6443
   ```

   Use the password from `~/.kube/kubeadmin-password.<clusterName>` when
   prompted. `Expected:` `Login successful`.

4. **Open the web console** (URL printed by the access banner):

   ```text
   https://console-openshift-console.apps.<clusterName>.<baseDomain>
   ```

   Log in as `kubeadmin` with the password from step 3.

## Verify

- `oc whoami` → `kubeadmin`.
- `oc get nodes` → every node `Ready`.
- `oc get clusterversion` → the installed version.
- `oc get co` → every operator `Available=True`.

## Troubleshooting

- **`connection refused` / name does not resolve** → the workstation tunnel
  or split-DNS is missing; run `virtlab status`, then re-run `virtlab up`.
- **`x509: certificate is valid for …`** → you reached the wrong endpoint;
  confirm the API name matches `clusterName` and `baseDomain`.
- **`Login failed (401 Unauthorized)`** → wrong password; read it from
  `~/.kube/kubeadmin-password.<clusterName>`.
- **`oc` cannot find a config** → `KUBECONFIG` is not exported in this shell;
  repeat step 1.
- **Kubeconfig missing** → Phase 4 did not finish; re-run `virtlab up`.

## Never do

- **Never commit the kubeconfig or the kubeadmin password** to the repo.
- **Never delete `<installDir>/auth/kubeconfig` on the host** — the bootstrap
  resume guard uses it to adopt a healthy cluster.
- **Never use `kubeadmin` for routine work**; create named users for day-2.
- **Never copy credentials into Pulumi config or stack state.**

## Related

- Credential sync: `internal/verification/verify.go` (`syncCredentials`).
- Paths: `~/.kube/config.<clusterName>`,
  `~/.kube/kubeadmin-password.<clusterName>`.
- Day-2 recovery: [`recover-failed-install.md`](recover-failed-install.md).
