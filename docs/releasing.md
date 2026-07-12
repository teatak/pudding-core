# Desktop Release

## Version source

`package.json` is the canonical Pudding app version. `package-lock.json` must carry the same root version.

For a formal release, update both files with:

```bash
npm version 0.1.2 --no-git-tag-version
```

Commit and push that version before building or creating the release tag. `PUDDING_APP_VERSION` is only for
local cross-version update tests; do not use it for a formal release.

## Build

The supported macOS packaging command is:

```bash
make desktop-bundle
```

The default update mode is always `manual`, with or without a signing identity. The build produces the DMG,
ZIP, blockmaps, and `latest-mac.yml` under `dist/release`. It then verifies:

- `Info.plist`, bundled `package.json`, and `latest-mac.yml` use the canonical version.
- The bundled update mode matches the requested mode.
- All release artifacts exist.
- The app code-signature structure and DMG checksum are valid.
- An ad-hoc build cannot accidentally use automatic updates.

Automatic updates remain opt-in for a future signed release:

```bash
PUDDING_UPDATE_MODE=automatic \
PUDDING_MAC_IDENTITY="Developer ID Application: ..." \
APPLE_KEYCHAIN_PROFILE="pudding-notary" \
make desktop-bundle
```

Developer ID builds require notarization credentials. The release verifier rejects an ad-hoc automatic build,
an unstapled app, an unexpected signing authority, or an app that fails Gatekeeper assessment.

## Publish

Upload a new version as a draft first. Smoke-test its artifacts, then publish it as latest. Never replace assets
under an existing version; publish a new patch version instead. Keep previous releases available for rollback.

The public download page is permanently:

https://github.com/teatak/pudding/releases/latest

## Unsigned macOS distribution

Manual update mode prevents Pudding from installing an unsigned update automatically, but it does not bypass
macOS Gatekeeper. Changing from DMG to ZIP or PKG does not remove the warning. Automatic update mode must remain
disabled until releases use a Developer ID Application signature and Apple notarization.

Keep the user-facing installation instructions in the public
[`teatak/pudding` README](https://github.com/teatak/pudding#install), next to the release downloads.

## Failure recovery

- Manual mode never installs an update without the user's action.
- The Help menu always contains `Download Latest Version...`, independently of update-check state.
- If update checking breaks but Pudding still opens, use that permanent menu item.
- If Pudding cannot start, download the latest or a previous DMG directly from the public Releases page.
- Keep at least the previous known-good release and its DMG available.
- Before rolling back across a database migration, back up `~/.pudding`.
