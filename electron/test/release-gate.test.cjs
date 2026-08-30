const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const {
  assertUnusedStatuses,
  expectedTagForVersion,
  inferReleaseChannelFromTag,
  validateManifestVersions,
} = require("../../scripts/release-gate.cjs");

test("release gate requires package and lock versions to match", () => {
  assert.equal(
    validateManifestVersions(
      { version: "0.1.2" },
      { version: "0.1.2", packages: { "": { version: "0.1.2" } } },
    ),
    "0.1.2",
  );
  assert.throws(
    () =>
      validateManifestVersions(
        { version: "0.1.2" },
        { version: "0.1.1", packages: { "": { version: "0.1.1" } } },
      ),
    /package versions differ/,
  );
});

test("release gate maps stable and preview tags to channels", () => {
  assert.equal(expectedTagForVersion("0.1.2"), "v0.1.2");
  assert.equal(inferReleaseChannelFromTag("v0.1.2"), "stable");
  assert.equal(inferReleaseChannelFromTag("v0.1.3-beta.1"), "preview");
});

test("release gate rejects malformed release tags", () => {
  for (const tag of ["0.1.2", "v0.1", "v0.1.2-preview.1", "v0.1.3-beta.0"]) {
    assert.throws(() => inferReleaseChannelFromTag(tag));
  }
});

test("desktop packaging is only exposed through the complete Make pipeline", () => {
  const scripts = require("../../package.json").scripts;
  const makefile = fs.readFileSync(path.resolve(__dirname, "..", "..", "Makefile"), "utf8");
  assert.equal(scripts["desktop:package"], undefined);
  assert.equal(scripts["desktop:publish"], undefined);
  assert.match(makefile, /desktop-bundle: desktop-runtimes/);
  assert.match(makefile, /prepare-runtime\.sh arm64/);
  assert.match(makefile, /prepare-runtime\.sh x64/);
  assert.match(makefile, /PUDDING_PACKAGING_PIPELINE=1/);
});

test("notary credentials are stored through a password-free Make target", () => {
  const makefile = fs.readFileSync(path.resolve(__dirname, "..", "..", "Makefile"), "utf8");
  assert.match(makefile, /PUDDING_NOTARY_PROFILE \?= pudding-notary/);
  assert.match(makefile, /PUDDING_NOTARY_APPLE_ID \?= yangglivecn@icloud\.com/);
  assert.match(makefile, /PUDDING_NOTARY_TEAM_ID \?= 7K47HJ79JA/);
  assert.match(makefile, /desktop-notary-check:/);
  assert.match(makefile, /desktop-notary-store:/);
  assert.doesNotMatch(makefile, /--password/);
});

test("public release gate only accepts unused versions", () => {
  assert.doesNotThrow(() =>
    assertUnusedStatuses("v0.1.2", [
      { label: "release", status: 404 },
      { label: "tag", status: 404 },
    ]),
  );
  assert.throws(
    () => assertUnusedStatuses("v0.1.2", [{ label: "release", status: 200 }]),
    /already exists/,
  );
  assert.throws(
    () => assertUnusedStatuses("v0.1.2", [{ label: "release", status: 403 }]),
    /HTTP 403/,
  );
});
