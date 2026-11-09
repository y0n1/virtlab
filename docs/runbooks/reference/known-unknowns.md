# Deferred items and known unknowns

Reference material, not a procedure. These items are tracked but deliberately
out of scope or not yet proven.

- License archives and pull secrets are supplied at runtime and never committed.
  `.gitignore` excludes `license.zip` and `pull-secret.json`.
- Treat any pull secret that was ever copied outside its intended host as
  compromised and rotate it.
- Day-2 node addition to a running cluster is not automated yet; compact and HA
  presets already provision multiple nodes.
- A clean end-to-end fresh install, and the re-fire after a configuration
  change, remain to be proven against a real Host.
