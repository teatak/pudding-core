# Contributing to Pudding Core

Read [AGENTS.md](AGENTS.md) and [the documentation index](docs/README.md) first.
Keep session ownership, canonical message storage and lifecycle transactions intact.

Install Go 1.25.1, Xcode command-line tools and PortAudio, then run:

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

The existing AGPL declaration remains in place. Report vulnerabilities privately as described in
[SECURITY.md](SECURITY.md).
