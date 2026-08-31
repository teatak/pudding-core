# Pudding

Pudding is the source repository for the Pudding daemon and Electron desktop app.

> This is the source repository. Product downloads and update artifacts are published from
> [`teatak/pudding`](https://github.com/teatak/pudding).

Chinese text segmentation is provided by the separately maintained MIT-licensed
[`teatak/seg`](https://github.com/teatak/seg) module.

This repo starts from a clean architecture:

- local-first
- fully multi-session
- explicit session routing
- no backend focus state
- daemon-owned hardware resources
- session-owned transports and context

The documentation index and status guide live in [docs/README.md](docs/README.md).
Current architecture decisions live in [docs/technology-decisions.md](docs/technology-decisions.md).
App package and connection field notes live in [docs/apps.md](docs/apps.md).
Local tool-usage reporting is documented in [docs/tool-usage-report.md](docs/tool-usage-report.md).
Desktop versioning, packaging, publishing, and update recovery are documented in [docs/releasing.md](docs/releasing.md).

## Development

Pudding currently targets macOS. Development requires Go 1.25.1, Node.js 24, npm, Xcode command-line tools,
and PortAudio. On macOS with Homebrew:

```bash
brew install portaudio
npm ci
npm --prefix web ci
make desktop-dev
```

Run the primary checks with:

```bash
go test ./...
npm test
npm --prefix web run build
```

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request and [SECURITY.md](SECURITY.md) for private
vulnerability reporting.

## Releases

Source development and source tags live in this repository. Signed DMGs, update metadata, and public release
notes live in [`teatak/pudding`](https://github.com/teatak/pudding/releases).

## License

Pudding source code is licensed under the [GNU Affero General Public License v3.0](LICENSE).
The Pudding name, logos, icons, and other brand assets are not granted under that license; see
[TRADEMARKS.md](TRADEMARKS.md). Third-party components remain under their respective licenses.
