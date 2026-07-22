# Pudding

Pudding is the source repository for the Pudding daemon and Electron desktop app.

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
