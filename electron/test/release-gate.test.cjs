const assert = require("node:assert/strict");
const test = require("node:test");

const {
  expectedTagForVersion,
  inferReleaseChannelFromTag,
  validateManifestVersions,
} = require("../../scripts/release-gate.cjs");
const { assertUnusedStatuses } = require("../../scripts/check-public-release.cjs");

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

test("local bundles stay tag-free and raw builder publishing stays disabled", () => {
  const scripts = require("../../package.json").scripts;
  assert.doesNotMatch(scripts["desktop:package"], /release-gate/);
  assert.equal(scripts["desktop:publish"], undefined);
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
