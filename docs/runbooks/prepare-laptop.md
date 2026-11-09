# SOP: Prepare the laptop

> **Purpose:** make the laptop ready to run `virtlab`: user toolchains
> present, `config.yaml` valid, and state encryption chosen.
> **Risk:** low — no lab state changes; a wrong config only fails the next
> `up`.
> **Success:** `ssh -V` and `go version` answer, and `virtlab status` loads
> `config.yaml` without configuration errors.

## Prereqs
- Shell access to the laptop with this repository checked out.
- A pull secret file, the `VIRTLAB_OCM_TOKEN` offline token, or an authenticated
  `ocm` CLI (see
  [`store-pull-secret.md`](store-pull-secret.md)).
- `sshuttle`, `pulumi`, and `ocm` are installed automatically by `virtlab up`
  and tracked in `~/.local/state/virtlab/state.json`.

## Steps

1. **Verify the user-managed toolchains:**

   ```bash
   ssh -V
   go version
   ```

   `Expected:` OpenSSH >= 8.0 and Go >= 1.22. `go.mod` pins Go 1.26.6 for
   source builds.

2. **Create the lab configuration:**

   ```bash
   cp examples/config.example.yaml config.yaml
   ```

   Edit `clusterName`, `baseDomain`, `ocpVersion`, `hostSsh`, and `preset`.
   `config.yaml` is gitignored; never commit it.

3. **Choose the state encryption passphrase** (optional, recommended):

   ```bash
   export PULUMI_CONFIG_PASSPHRASE='<passphrase-from-your-password-manager>'
   ```

   `Expected:` no output. When unset, `virtlab` uses an empty passphrase so
   unattended runs never prompt; that leaves local stack state unencrypted.
   Set it before the first `virtlab up`; changing it later makes existing
   stack state unreadable.

4. **Validate the configuration:**

   ```bash
   virtlab status
   ```

   `Expected:` the lab banner and status lines, with no configuration error.
   Host reachability may report unreachable until the host SOPs are complete.

## Verify

- `ssh -V` → OpenSSH >= 8.0.
- `go version` → Go >= 1.22.
- `virtlab status` → prints `Status for Lab '<clusterName>'` without a
  configuration error.

## Troubleshooting

- **`config file 'config.yaml' not found`** → repeat step 2. With the default
  path, `virtlab` also falls back to `config.local.yaml`.
- **`configuration errors in 'config.yaml'`** → fix the entries named in the
  summary; the schema is `examples/config.example.yaml`.
- **`go` missing or too old** → install Go >= 1.22 from https://go.dev/dl/.
- **`ssh` missing or too old** → install OpenSSH >= 8.0.
- **`pulumi: command not found`** → `virtlab up` installs it during Phase 1.

## Never do

- **Never commit `config.yaml`, `~/.pulumi` state, or the passphrase** — state
  and secrets stay on the laptop.
- **Never run bare `pulumi login`** — it switches this user to a cloud
  backend; `virtlab` selects the local `file://<home>` backend itself when
  none is configured.
- **Never add secrets to `config.yaml`** — the pull secret lives on the host.

## Related

- Configuration schema and defaults: `examples/config.example.yaml`,
  `internal/config/config.schema.json`, and `internal/config/config.go`.
- Backend selection: `internal/infrastructure/infrastructure.go`
  (`EnsurePulumiBackend`).
- Next: [`bring-up-lab.md`](bring-up-lab.md).
