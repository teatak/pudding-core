# Bundled language servers

Release builds package trusted, pinned language servers so users do not need to install them
separately.

- `gopls`: `v0.22.0`; built from `golang.org/x/tools/gopls` for the release platform.
- `typescript-language-server`: `5.3.0`.
- `typescript`: `6.0.3`; bundled fallback used when a project does not provide TypeScript.

`scripts/prepare-language-servers.sh` installs these resources into the ignored
`bin/language-servers` staging directory. Runtime processes are still started only on demand.

Upgrades must update the pinned versions, regenerate `package-lock.json`, run both real LSP
integration tests, and review upstream licenses and supported runtime versions.
