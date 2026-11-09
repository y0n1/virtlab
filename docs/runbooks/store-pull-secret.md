# SOP: Store the pull secret

> **Purpose:** place the OpenShift pull secret at the host path the stack
> expects, readable only by root.
> **Risk:** low — the cluster cannot pull images without it, but nothing is
> destroyed; the real hazard is leaking the secret.
> **Success:** `stat` shows mode `600`, owner `root`, at the fixed Host path
> `/root/pull-secret.json`.

## Prereqs
- Root shell on the host prepared by [`prepare-host-vm.md`](prepare-host-vm.md).
- A Red Hat account able to download the pull secret.

## Steps

1. **Download the pull secret on the host** from
   <https://console.redhat.com/openshift/install/pull-secret>.

   `Expected:` a `pull-secret.json` file under your download path.

2. **Install it at the fixed Host path with restrictive permissions:**

   ```bash
   install -m 600 -o root -g root /path/to/pull-secret.json /root/pull-secret.json
   ```

   `Expected:` exit 0.

3. **Confirm mode and ownership:**

   ```bash
   stat -c '%a %U %n' /root/pull-secret.json
   ```

   `Expected:` `600 root /root/pull-secret.json`.

4. **Confirm the Workstation source, if you use one:**

   ```bash
   grep 'pullSecretFile' config.yaml
   ```

   `Expected:` the path of the pull secret on the Workstation. If you rely on
   the `ocm` CLI instead, the key is absent and `virtlab up` retrieves the
   secret automatically. `virtlab up` always delivers the bytes to
   `/root/pull-secret.json` on the Host and overwrites the manual copy.

The bytes are injected host-side into `install-config.yaml` at image build
time. They never enter the repo, the Pulumi program, or the stack state — only
the path appears in configuration.

## Verify

- `stat -c '%a %U' /root/pull-secret.json` → `600 root`.
- `grep pullSecretFile config.yaml` → the Workstation source path, or no
  output when `virtlab` uses `ocm`.
- `git ls-files | grep -q pull-secret` → no output.

## Troubleshooting

- **Download returns an error page** → sign in to console.redhat.com first;
  the endpoint requires an authenticated session.
- **`stat` shows `644`** → re-run step 2 with `install -m 600`; do not use
  `chmod` after a copy that already exposed the file.
- **Path mismatch** → install the file at `/root/pull-secret.json`; do not add
  a Host path to `config.yaml`.
- **Secret already leaked** (committed, pasted, or shared) → revoke and
  regenerate it in the Red Hat console before continuing.

## Never do

- **Never commit the pull secret** — `.gitignore` excludes it, but the check
  is yours; the file belongs on the host only.
- **Never put the bytes in stack config or environment variables** — only the
  path is configuration.
- **Never print or paste the file contents** into logs, tickets or chat.
- **Never reuse a secret copied from an old capture** — it has been revoked.

## Related

- Path and injection: `config.yaml` (`pullSecretFile`),
  `internal/workstation/pullsecret.go`, and `internal/host/pullsecret.go`.
- Preflight: `internal/cluster/openshift.go`.
- Ignore rule: `.gitignore` (`**/pull-secret.json`).
