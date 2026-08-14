# Internal Scripts

Use Make targets as the public entry points. Files in this directory are pipeline internals and should not be
called directly unless debugging the pipeline itself.

## Supported commands

| Goal | Command |
| --- | --- |
| Run the desktop app in development | `make desktop-dev` |
| Build the macOS Computer Use C0 helper | `make computer-use-helper-dev` |
| Test the macOS Computer Use C0 helper | `make computer-use-helper-test` |
| Run the native Computer Use fixture smoke | `make computer-use-fixture-smoke` |
| Run the full Computer Use product smoke | `make computer-use-product-smoke` |
| Run the real Calculator product smoke | `make computer-use-calculator-smoke` |
| Verify an already-running Calculator is not session-owned | `make computer-use-calculator-existing-smoke` |
| Build and fully verify a stable package | `make desktop-bundle` |
| Build and fully verify a preview package | `make desktop-preview-bundle` |
| Recheck existing stable artifacts | `make desktop-verify` |
| Recheck existing preview artifacts | `make desktop-preview-verify` |
| Test an older installed app updating to the built stable version | `make desktop-update-test` |
| Test an older installed app updating to the built preview version | `make desktop-preview-update-test` |
| Verify Computer Use identity remains stable across a stable update | `make desktop-computer-use-update-test` |
| Verify Computer Use identity remains stable across a preview update | `make desktop-preview-computer-use-update-test` |
| Create a stable Draft Release | `make desktop-publish` |
| Create a preview Draft Release | `make desktop-preview-publish` |
| Resume only Draft creation or asset upload | `make desktop-publish-upload-resume` |
| Publish a verified Draft Release | `make desktop-release-finalize` |

`package-desktop.cjs` is guarded by `PUDDING_PACKAGING_PIPELINE=1`; only the matching Make targets set it. The
Electron Builder config has the same guard. This prevents packaging a stale daemon or skipping artifact checks.

The release path is fixed:

1. Verify a clean checkout that exactly matches its upstream.
2. Run Go and Electron tests.
3. Build separate arm64 and x64 runtimes and apps, then sign, notarize, and verify both ZIPs, both DMGs, nested
   code, update metadata, and bundle permissions.
4. Create and push the immutable source tag only after the package passes.
5. Commit `releases/v<version>.json` to `teatak/pudding`, including the feature list and artifact hashes.
6. Create a `v<version>` titled Draft Release targeting that public commit, upload all nine artifacts, and verify
   the complete asset set plus generated feature list. GitHub does not create the real tag while the release is a
   draft.
7. Publish the draft explicitly, then verify that the resulting public tag points to the manifest commit.

Packaging helpers belong under `packaging/`; Electron smoke fixtures belong under `electron/smoke/`. Do not add
one-off asset or test helpers back to this directory.
