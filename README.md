# Pudding Core

Pudding Core is the source repository for the next Pudding daemon and app core.

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
