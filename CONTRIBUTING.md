# Contributing to Pudding

Thank you for helping improve Pudding. Keep changes focused, explain the user-visible behavior, and include tests
for behavior changes.

## Before you start

- Read `AGENTS.md` and `docs/technology-decisions.md`.
- Search existing issues and pull requests before opening a duplicate.
- Discuss architectural or product-scope changes in an issue before implementing them.
- Never include credentials, personal data, production databases, signing material, or generated release bundles.

## Development setup

Pudding currently targets macOS. Install Go 1.25.1, Node.js 24, npm, Xcode command-line tools, and PortAudio,
then run:

```bash
brew install portaudio
npm ci
npm --prefix web ci
go test ./...
npm test
npm --prefix web run build
```

Start the development desktop app with `make desktop-dev`. Development data is stored separately from release
data as described in `AGENTS.md`.

## Pull requests

1. Create a short-lived branch from `main`.
2. Keep one behavior change per pull request.
3. Update tests and documentation together with the implementation.
4. Run `npm run check:secrets`, `go test ./...`, `npm test`, and `npm --prefix web run build`.
5. Explain verification and any remaining limitations in the pull request description.

By submitting a contribution, you agree that it may be distributed under the repository's
AGPL-3.0-only license. Pudding names and brand assets remain governed by `TRADEMARKS.md`.
