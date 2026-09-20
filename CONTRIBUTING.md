# Contributing to Pudding Core

Read [AGENTS.md](AGENTS.md) and [the documentation index](docs/README.md) first.
Keep session ownership, canonical message storage and lifecycle transactions intact.

On Apple Silicon, follow the [README setup](README.md#开发) to install Go, the native build tools,
PortAudio and the pinned Abseil version, and export `PKG_CONFIG_PATH`. Then run:

```sh
make test
make schema-check
make daemon
```

Tests must use temporary data directories. Do not include credentials, production data or generated bundles.
Desktop UI, Electron and the native Computer Use helper are maintained in `pudding-desktop`.
A wire-contract change must update the public `contracts/` definitions and its consumer tests;
update the desktop core pin when integrating the change. Runtime JSON is the sole source of the handshake
version and browser limits. Do not duplicate these values in a client.

Pudding Core is licensed under [Apache License 2.0](LICENSE). Unless explicitly stated otherwise,
contributions intentionally submitted for inclusion are provided under the same license.
Preserve third-party copyright and license notices. This license change does not rewrite historical
releases or revoke rights already granted under their licenses.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
