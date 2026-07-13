# Desktop Release

## Version source

`package.json` is the canonical Pudding app version. `package-lock.json` must carry the same root version.

For a formal release, update both files with:

```bash
npm version 0.1.2 --no-git-tag-version
```

Commit and push that version before building or creating the release tag. `PUDDING_APP_VERSION` is only for
local cross-version update tests; do not use it for a formal release.

Preview releases use the next stable version with a beta suffix:

```bash
npm version 0.1.3-beta.1 --no-git-tag-version
```

Increment only the final beta number (`beta.2`, `beta.3`, ...), then remove the suffix for `0.1.3`. Git tags use
`v0.1.3-beta.1`. `preview` is the product-facing name; `beta` is the update channel understood by
electron-updater.

Ship the preview-channel setting in stable `0.1.2` first. The first preview offered through it should then be
`0.1.3-beta.1`; never publish `0.1.2-beta.*` after stable `0.1.2`, because SemVer treats that preview as older.

## Release invariants

- Local bundle commands do not require a Git tag.
- Published releases always come from an annotated `v<package version>` tag.
- `package.json`, `package-lock.json`, and the lockfile root package must have the same version.
- Formal publishing rejects `PUDDING_APP_VERSION`; that override is only for local update tests.
- The worktree must be clean and the current branch must exactly match its upstream before creating a tag.
- Release tags and published assets are immutable. A correction always uses a new beta or patch version.
- `main` remains the development line. Use short-lived feature branches and temporary hotfix branches rather
  than permanent stable/preview branches.

## Build

The supported macOS packaging command is:

```bash
make desktop-bundle
```

For a preview version, use `make desktop-preview-bundle`. Stable builds require an `x.y.z` version and preview
builds require `x.y.z-beta.n`; packaging fails when the version and release channel disagree.

Release builds default to `automatic`: they check in the background, download a signed update, and wait for the
user to choose **Restart to Update**. The build therefore requires a Developer ID identity and notarization
credentials. It produces the DMG, ZIP, blockmaps, and `latest-mac.yml` under `dist/release`, then verifies:

- `Info.plist`, bundled `package.json`, and `latest-mac.yml` use the canonical version.
- The bundled update mode matches the requested mode.
- All release artifacts exist.
- The app code-signature structure and DMG checksum are valid.
- An unsigned build cannot accidentally use automatic updates.

Build a signed automatic release with:

```bash
PUDDING_MAC_IDENTITY="Certificate Name (TEAMID)" \
APPLE_KEYCHAIN_PROFILE="pudding-notary" \
make desktop-bundle
```

Use the same signing and notarization variables with `make desktop-preview-bundle` for a preview package.

The optional `Developer ID Application:` prefix is accepted and stripped before invoking Electron Builder.
Developer ID builds require notarization credentials. The release verifier rejects an unsigned automatic build,
an unstapled app, an unexpected signing authority, or an app that fails Gatekeeper assessment. For local package
testing only, an unsigned manual build remains available via `PUDDING_UPDATE_MODE=manual make desktop-bundle`.

## Publish

Publishing is tag-driven. After changing the version, commit and push the version commit, then run one of:

```bash
# x.y.z
make desktop-publish

# x.y.z-beta.n
make desktop-preview-publish
```

These commands do not build on the developer machine. They validate the version, clean worktree, upstream
state, and remote tag; create an annotated tag such as `v0.1.2` or `v0.1.3-beta.1`; then push that tag to
`origin`. Pushing the tag starts [the desktop release workflow](../.github/workflows/desktop-release.yml).

The workflow runs on macOS arm64, validates that the tag and package version match, runs Go and Electron tests,
signs and notarizes the app, and publishes to `teatak/pudding`. Stable tags produce a normal GitHub Release with
`latest-mac.yml`; beta tags produce a GitHub Prerelease with `beta-mac.yml`. Stable clients keep
`allowPrerelease=false` and never receive a preview package. The workflow fails before building if the public
repository already contains that release or tag.

Configure these Actions secrets in `teatak/pudding-core` before the first tag-driven release:

| Secret | Purpose |
| --- | --- |
| `PUDDING_RELEASE_TOKEN` | Fine-grained token allowed to create Releases in `teatak/pudding` |
| `MACOS_CERTIFICATE_P12` | Base64-encoded Developer ID Application `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | Password for the `.p12` |
| `PUDDING_MAC_IDENTITY` | Developer ID identity, including the Team ID |
| `APPLE_ID` | Apple Developer account used for notarization |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for notarization |
| `APPLE_TEAM_ID` | Apple Developer Team ID |

The source tag lives in `teatak/pudding-core`; Electron Builder creates the matching public release in
`teatak/pudding`. Keep previous public releases available for rollback.

The developer setting **Receive Pudding preview releases** opts the existing app into the beta channel. Preview
and stable builds intentionally share `Pudding.app`, the bundle identifier, and `~/.pudding`. Turning the setting
off disables future beta updates but never downgrades the installed app; the next higher stable release returns
the installation to stable code. Database migrations in previews must therefore be forward-only and remain in
the eventual stable release.

The public download page is permanently:

https://github.com/teatak/pudding/releases/latest

## Unsigned local builds

Unsigned builds are for local testing only and must not be published. Manual mode prevents them from installing
an update automatically, but it does not bypass macOS Gatekeeper.

Keep the user-facing installation instructions in the public
[`teatak/pudding` README](https://github.com/teatak/pudding#install), next to the release downloads.

## Failure recovery

- Automatic mode downloads in the background but never installs until the user chooses **Restart to Update**.
- If the tag workflow fails before publishing public assets, fix the workflow or secret and rerun the same
  Actions job. Do not move the tag.
- If public assets already exist or the tagged source itself is wrong, bump the version and create a new tag.
- As an emergency fallback, check out the existing tag and run
  `PUDDING_RELEASE_CHANNEL=stable make desktop-publish-from-tag` with all signing, notarization, and GitHub
  credentials configured. Use `preview` for a beta tag. This target refuses an untagged or dirty checkout.
- The Help menu always contains `Download Latest Version...`, independently of update-check state.
- If update checking breaks but Pudding still opens, use that permanent menu item.
- If Pudding cannot start, download the latest or a previous DMG directly from the public Releases page.
- Keep at least the previous known-good release and its DMG available.
- Before rolling back across a database migration, back up `~/.pudding`.
