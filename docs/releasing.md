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
make desktop-bundle
```

## Publish

Upload a new version as a draft first. Smoke-test its artifacts, then publish it as latest. Never replace assets
under an existing version; publish a new patch version instead. Keep previous releases available for rollback.

The public download page is permanently:

https://github.com/teatak/pudding/releases/latest

## Installing an unsigned macOS build

Manual update mode prevents Pudding from trying to install an unsigned update automatically. It does not bypass
macOS Gatekeeper. Until releases use a Developer ID signature and Apple notarization, first launch requires an
explicit user exception:

1. Move `Pudding.app` to Applications and try to open it once.
2. Open **System Settings > Privacy & Security**.
3. In **Security**, find the blocked Pudding entry and click **Open Anyway**.
4. Authenticate and confirm **Open**.

The exception is saved for that application. Apple exposes **Open Anyway** for about one hour after the blocked
launch. Only use it for an artifact downloaded from the official Releases page. See
[Apple's Gatekeeper instructions](https://support.apple.com/guide/mac-help/open-a-mac-app-from-an-unknown-developer-mh40616/mac/26).

Changing from DMG to ZIP or PKG does not remove this warning. Eliminating it for normal users requires a
Developer ID Application signature and Apple notarization; automatic update mode must remain disabled until then.

## Failure recovery

- Manual mode never installs an update without the user's action.
- The Help menu always contains `Download Latest Version...`, independently of update-check state.
- If update checking breaks but Pudding still opens, use that permanent menu item.
- If Pudding cannot start, download the latest or a previous DMG directly from the public Releases page.
- Keep at least the previous known-good release and its DMG available.
- Before rolling back across a database migration, back up `~/.pudding`.
