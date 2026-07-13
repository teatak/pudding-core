# Internal Scripts

Use Make targets as the public entry points. Files in this directory are pipeline internals and should not be
called directly unless debugging the pipeline itself.

## Supported commands

| Goal | Command |
| --- | --- |
| Run the desktop app in development | `make desktop-dev` |
| Build and fully verify a stable package | `make desktop-bundle` |
| Build and fully verify a preview package | `make desktop-preview-bundle` |
| Recheck existing stable artifacts | `make desktop-verify` |
| Test an older installed app updating to the built version | `make desktop-update-test` |
| Create a stable Draft Release | `make desktop-publish` |
| Create a preview Draft Release | `make desktop-preview-publish` |
| Publish a verified Draft Release | `make desktop-release-finalize` |

`package-desktop.cjs` is guarded by `PUDDING_PACKAGING_PIPELINE=1`; only the matching Make targets set it. The
Electron Builder config has the same guard. This prevents packaging a stale daemon or skipping artifact checks.

The release path is fixed:

1. Verify a clean checkout that exactly matches its upstream.
2. Run Go and Electron tests.
3. Build, sign, notarize, and verify the app, ZIP, DMG, daemon, language server, dylibs, and bundle permissions.
4. Create and push the immutable source tag only after the package passes.
5. Upload all artifacts to a Draft Release and verify the complete asset set.
6. Publish the draft explicitly.

Packaging helpers belong under `packaging/`; Electron smoke fixtures belong under `electron/smoke/`. Do not add
one-off asset or test helpers back to this directory.
