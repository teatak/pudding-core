# Pudding

Pudding is the source repository for the next Pudding daemon and desktop app.

## Download

Download the latest desktop build from [GitHub Releases](https://github.com/teatak/pudding/releases/latest).

Current macOS test builds are not notarized. After trying to open Pudding once, open **System Settings >
Privacy & Security**, find the blocked Pudding entry, and choose **Open Anyway**. Only override Gatekeeper for
builds downloaded from the official release page.

This repo starts from a clean architecture:

- local-first
- fully multi-session
- explicit session routing
- no backend focus state
- daemon-owned hardware resources
- session-owned transports and context

Initial design notes live in [docs/technology-decisions.md](docs/technology-decisions.md).
Phase 1 development plan lives in [docs/phase-1-plan.md](docs/phase-1-plan.md).
App package and connection field notes live in [docs/apps.md](docs/apps.md).
Local tool-usage reporting is documented in [docs/tool-usage-report.md](docs/tool-usage-report.md).
Desktop versioning, packaging, publishing, and update recovery are documented in [docs/releasing.md](docs/releasing.md).
