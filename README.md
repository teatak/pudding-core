# Pudding Core

A local-first, multi-session agent daemon built with Go, SQLite, and loopback HTTP.
The desktop product is developed separately in the private `pudding-desktop` repository.
Desktop installers and updates are available from [Pudding releases](https://github.com/teatak/pudding/releases).

## Capabilities

- Model providers, streaming output, context building and compaction, and tool execution loops.
- Multiple sessions, message history, input queues, cancellation, approvals, and resumable SSE streams.
- Projects and files, command execution and background processes, Git, LSP, permissions, and sandboxing.
- Skills, Apps, MCP, attachments, resource libraries, canvas data, and usage tracking.
- Audio and camera services in Go, plus protocols and session management for browser and computer-use tools.

The visual browser, native computer-use actions, and frontend canvas tools require the desktop client.
Headless browser capabilities require a local Chrome installation. Hardware features and native dependencies
remain subject to their supported platforms.

## Development

Development on macOS uses Apple Silicon and requires Go 1.25.1, Xcode command-line tools, PortAudio,
and a pinned version of Abseil. Building the daemon does not require the web frontend, Electron,
or access to a private repository.

```sh
brew install portaudio cmake pkgconf
bash scripts/prepare-abseil.sh
export PKG_CONFIG_PATH="$PWD/dist/deps/abseil/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
make daemon
make test
make schema-check
```

The preparation script pins Abseil to match the ABI of the existing arm64 WebRTC library;
do not replace it with the latest system version. Set `PKG_CONFIG_PATH` again when opening a new terminal.
Intel release builds use a separate dependency setup through `make runtime ARCH=x64 OUT=/absolute/path`.

Run `make daemon-dev` to start the development daemon. Development data is stored in `~/.pudding-dev`;
release builds use `~/.pudding`. Tests must use temporary directories. The daemon listens only on loopback,
and API requests require the startup token stored in `<home>/daemon.token`.
Optional language servers can be prepared with `make language-servers`, which also requires Node.js and npm.

See the [API quickstart](docs/api-quickstart.md) for standalone usage examples,
[contracts](contracts/README.md) for protocol definitions and client validation schemas,
and the [documentation index](docs/README.md) for design and feature documentation.

## Desktop Integration

`pudding-desktop/core.lock.json` pins an exact core commit. The desktop build handles the frontend,
Swift helper, application packaging, signing, and updates. Core builds the daemon and its dependencies,
including language servers.

The desktop client can pass `-ui-dir /absolute/path/to/web/dist` to serve external static assets
from the same origin as the API. Without this option, the root path returns 404 and the daemon serves
only its APIs. There is no embedded frontend or placeholder page.

## History and License

Copyright 2026 Pudding Core contributors.

The current version of Pudding Core is licensed under the [Apache License 2.0](LICENSE).
Commercial use, modification, redistribution, and use in proprietary products are permitted subject to
the license terms. Third-party dependencies retain their own copyright notices and licenses.

The complete Git history is preserved, including older desktop source code and the AGPL declarations
in historical commits and tags. This license change does not rewrite history or revoke rights already
granted for earlier versions. The separate `pudding-desktop` repository is outside the scope of this
repository's Apache-2.0 license.

Before contributing, read [AGENTS.md](AGENTS.md) and [CONTRIBUTING.md](CONTRIBUTING.md).
