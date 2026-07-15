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

## Pre-release assessment

Every stable or preview release must complete the same assessment before changing the version or creating a tag:

1. Use the latest published tag as the baseline and review the complete commit range plus uncommitted changes.
2. Review `internal/store/schema.sql`, `internal/store/sqlitestore/migrations.go`, migration tests, and all changed
   persistence call sites.
3. Compare the schema fingerprint with the baseline. A schema change must increment `currentSchemaVersion`, add
   one transactional forward migration, append the immutable released fingerprint, and test rollback plus backup.
   When the schema is unchanged, do not add an empty migration or increment `PRAGMA user_version`.
4. Open the local release database read-only and check `PRAGMA user_version`, `PRAGMA quick_check`, and the current
   schema contract. Never test a migration directly against `~/.pudding/data/pudding.db`; copy it to a temporary
   directory when a real migration rehearsal is required.
5. Run the tagged Go tests, Electron tests, web production build, and `git diff --check`.
6. Create `docs/release-report-<version>.md` with the baseline, change summary, impact matrix, database analysis,
   compatibility, completed verification, remaining release gates, and a grouped `## Release Notes 草案`
   section. The release gate rejects a missing or empty feature list.

The report must explicitly state whether the release rewrites canonical data, rebuilds derived indexes, creates a
migration backup, changes downgrade behavior, or leaves the database untouched. Resolve every blocking item in
the report before starting the packaging and publishing flow below.

## Build

The supported macOS packaging command is:

```bash
make desktop-bundle
```

This is the only supported packaging entry point. Do not invoke Electron Builder or files under `scripts/`
directly. The target builds the web UI, release daemon, and language server before packaging, then runs the full
release verifier. It cross-compiles a separate Intel runtime on Apple Silicon and produces independent `arm64`
and `x64` apps; it does not create a universal app. Direct Electron Builder configuration loads are rejected by
a pipeline guard.

The first x64 build downloads checksum-pinned WebRTC Audio Processing and PortAudio sources, then caches their
build inputs under ignored `dist/` directories. Xcode command-line tools, Meson, Ninja, and pkg-config are
required. The current Sherpa ONNX binaries set the effective minimum versions to macOS 14.0 for arm64 and macOS
15.5 for x64; the verifier checks that each app advertises the matching requirement.

For a preview version, use `make desktop-preview-bundle`. Stable builds require an `x.y.z` version and preview
builds require `x.y.z-beta.n`; packaging fails when the version and release channel disagree.

Desktop updates are always automatic: they check in the background, download a signed update, and wait for the
user to choose **Restart to Update**. Every package therefore requires a Developer ID identity and notarization
credentials. It produces arm64 and x64 DMGs, ZIPs, their blockmaps, and one combined `latest-mac.yml` under
`dist/release`, then verifies:

- `Info.plist`, bundled `package.json`, and `latest-mac.yml` use the canonical version.
- All nine release artifacts exist and the update metadata references both architectures.
- The staged app plus the apps extracted from ZIP and DMG contain no read-only files that can block Squirrel.Mac.
- The app, daemon, language server, and bundled dylibs use the expected Developer ID and portable dependencies.
- The app and daemon retain camera/audio-input entitlements, and the bundle declares camera, microphone, screen
  capture, and local-network usage descriptions. Unused Bluetooth and system-audio declarations are removed.
- The managed-browser partition denies remote-page permission and device requests by default; native camera and
  microphone access remains daemon-owned.
- ZIP/DMG signatures, notarization tickets, Gatekeeper assessments, and the DMG checksum are valid.

The packaging pipeline automatically selects the only installed Developer ID Application identity and defaults
to the `pudding-notary` keychain profile. Set `PUDDING_MAC_IDENTITY` only when the keychain contains multiple
Developer ID identities. Alternative complete Apple ID or App Store Connect API notarization credentials are
also accepted; partial or competing credential methods are rejected.

The optional `Developer ID Application:` prefix is accepted and stripped before invoking Electron Builder.
`PUDDING_UPDATE_MODE` is no longer supported. The release verifier rejects unsigned or unstapled apps, unexpected
signing authorities, non-portable dylib paths, read-only bundle files, and artifacts that fail Gatekeeper.

## Publish

Publishing is local and tag-driven. Before the first release on a Mac:

- Install a valid Developer ID Application certificate and private key in the login keychain.
- Store notarization credentials as the `pudding-notary` keychain profile.
- Run `gh auth login` with an account that can create Releases in `teatak/pudding`.

After changing the version, commit and push the version commit, then run one of:

```bash
# x.y.z
make desktop-publish

# x.y.z-beta.n
make desktop-preview-publish
```

These commands validate the version, public release state, clean worktree, upstream state, GitHub login, signing
identity, and notarization setup. They verify the clean pushed checkout, run Go and Electron tests, then call the
same complete bundle pipeline used above. Only after that package passes signing, notarization, ZIP, DMG, nested
binary, and permission checks does the script create and push an annotated tag such as `v0.1.2` or
`v0.1.3-beta.1` to `teatak/pudding-core`. It then uploads the already verified artifacts to a Draft Release in
`teatak/pudding`.

Before creating the public Draft Release, the script commits `releases/v<version>.json` to the public repository.
The manifest records the source tag and commit, channel, user-facing feature list, and SHA-256 plus size of every
release asset. Publishing the Draft creates the public tag at that unique manifest commit, so public tags are
meaningful release anchors and GitHub can order releases consistently. Do not create the public tag before the
Draft is published; GitHub detaches drafts that collide with an existing tag. The Release title keeps the full
`v<version>` form and its body is generated from the report's feature list.

Packaging and uploading are separate phases. The apps are fully built and verified first; the release script
then creates one draft and uploads its nine assets sequentially. This avoids partially published or duplicate
drafts.

The draft is not visible to update clients. The publish command verifies it automatically; it can also be
checked again with:

```bash
make desktop-release-status
```

The status command succeeds only when both DMGs, both ZIPs, their four blockmaps, and channel metadata are fully
uploaded.
It accepts `RELEASE_TAG=v0.1.2` when checking a version other than the current `package.json`. After inspection,
publish explicitly with:

```bash
make desktop-release-finalize
```

Finalizing a stable tag publishes a normal GitHub Release and marks it latest. Finalizing a beta tag publishes a
GitHub Prerelease without changing latest. Stable clients keep `allowPrerelease=false` and never receive a
preview package. Publishing fails before building if the public repository already contains that release or
tag.

The source tag in `teatak/pudding-core` identifies the exact private source commit. The matching public tag in
`teatak/pudding` identifies that version's public manifest commit. Keep previous public releases available for
rollback.

The developer setting **Receive Pudding preview releases** opts the existing app into the beta channel. Preview
and stable builds intentionally share `Pudding.app`, the bundle identifier, and `~/.pudding`. Turning the setting
off disables future beta updates but never downgrades the installed app; the next higher stable release returns
the installation to stable code. Database migrations in previews must therefore be forward-only and remain in
the eventual stable release.

The public download page is permanently:

https://github.com/teatak/pudding/releases/latest

## Local package tests

Local DMG/ZIP tests use the same Developer ID signing, notarization, and automatic-update configuration as a
published release. Use the build command above; unsigned desktop packages are intentionally unsupported.

To test a real update from an older installed version, close Pudding and run:

```bash
make desktop-update-test
```

The command starts a loopback update feed, launches `/Applications/Pudding.app`, and waits for the installed app
to reach the package version. After choosing **Restart to Update**, it verifies the installed version, signature,
notarization ticket, Gatekeeper assessment, and bundle permissions before reporting success. Use
`make desktop-verify` to recheck existing artifacts without rebuilding them.

Keep the user-facing installation instructions in the public
[`teatak/pudding` README](https://github.com/teatak/pudding#install), next to the release downloads.

## Failure recovery

- Updates download in the background but never install until the user chooses **Restart to Update**.
- If publishing fails before the source tag is pushed, fix the problem and rerun the original publish command.
- If publishing fails after the source tag is pushed but before creating a draft, rerun
  `PUDDING_RELEASE_CHANNEL=stable make desktop-publish-from-tag`. Use `preview` for a beta tag. Do not move the
  source tag.
- If the local artifacts already passed the complete verifier and only Draft creation or uploading failed, run
  `PUDDING_RELEASE_CHANNEL=stable make desktop-publish-upload-resume`. Use `preview` for a beta tag. It verifies
  the existing package again, reuses the draft, skips complete assets, and uploads only missing assets without
  rebuilding or repeating notarization.
- Use the full from-tag recovery command when verified local artifacts are unavailable. Delete a draft only when
  it is corrupt or duplicated. Never replace assets after final publication.
- If the tagged source itself is wrong, bump the version and create a new tag.
- The from-tag target refuses an untagged or dirty checkout.
- The Help menu always contains `Download Latest Version...`, independently of update-check state.
- If update checking breaks but Pudding still opens, use that permanent menu item.
- If Pudding cannot start, download the latest or a previous DMG directly from the public Releases page.
- Keep at least the previous known-good release and its DMG available.
- Before rolling back across a database migration, back up `~/.pudding`.
