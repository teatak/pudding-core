const assert = require("node:assert/strict");
const test = require("node:test");

const {
  buildReleaseEnvironment,
  buildReleaseSteps,
  parseDeveloperIdentities,
} = require("../../scripts/release-local.cjs");

test("finds a single valid Developer ID identity", () => {
  const output = `
    1) ABCDEF "Developer ID Application: Gang Yang (7K47HJ79JA)"
       1 valid identities found
  `;
  assert.deepEqual(parseDeveloperIdentities(output), [
    "Developer ID Application: Gang Yang (7K47HJ79JA)",
  ]);
});

test("local releases default to the local notary profile", () => {
  const env = buildReleaseEnvironment(
    { PUDDING_UPDATE_MODE: "manual" },
    "stable",
    "Developer ID Application: Gang Yang (7K47HJ79JA)",
    "token",
  );
  assert.equal(env.PUDDING_RELEASE_CHANNEL, "stable");
  assert.equal(env.APPLE_KEYCHAIN_PROFILE, "pudding-notary");
  assert.equal(env.GH_TOKEN, "token");
  assert.equal(env.PUDDING_UPDATE_MODE, undefined);
});

test("explicit Apple credentials do not inject a keychain profile", () => {
  const env = buildReleaseEnvironment(
    {
      APPLE_ID: "developer@example.com",
      APPLE_APP_SPECIFIC_PASSWORD: "password",
      APPLE_TEAM_ID: "TEAMID",
    },
    "preview",
    "identity",
    "token",
  );
  assert.equal(env.PUDDING_RELEASE_CHANNEL, "preview");
  assert.equal(env.APPLE_KEYCHAIN_PROFILE, undefined);
});

test("release rejects incomplete or competing notarization credentials", () => {
  assert.throws(
    () => buildReleaseEnvironment({ APPLE_ID: "developer@example.com" }, "stable", "identity", "token"),
    /credentials are incomplete/,
  );
  assert.throws(
    () =>
      buildReleaseEnvironment(
        {
          APPLE_KEYCHAIN_PROFILE: "pudding-notary",
          APPLE_ID: "developer@example.com",
          APPLE_APP_SPECIFIC_PASSWORD: "password",
          APPLE_TEAM_ID: "TEAMID",
        },
        "stable",
        "identity",
        "token",
      ),
    /multiple notarization methods/,
  );
});

test("release builds and verifies the package before creating the immutable tag", () => {
  const steps = buildReleaseSteps("start", "stable", "v0.1.2");
  assert.deepEqual(steps[0], ["node", ["scripts/release-gate.cjs", "prepare"]]);
  assert.deepEqual(steps[3], ["make", ["desktop-bundle"]]);
  assert.deepEqual(steps[4], ["node", ["scripts/release-gate.cjs", "tag"]]);
  assert.deepEqual(steps.at(-1), ["node", ["scripts/release-draft.cjs", "status", "v0.1.2"]]);
});

test("resumed releases rebuild from the existing tag without moving it", () => {
  const steps = buildReleaseSteps("resume", "preview", "v0.1.3-beta.1");
  assert.deepEqual(steps[0], ["node", ["scripts/release-gate.cjs", "check"]]);
  assert.deepEqual(steps[3], ["make", ["desktop-preview-bundle"]]);
  assert.equal(steps.some(([, args]) => args.includes("tag")), false);
});

test("upload-only recovery verifies existing artifacts without rebuilding or notarizing", () => {
  const steps = buildReleaseSteps("upload", "stable", "v0.1.3");
  assert.deepEqual(steps, [
    ["node", ["scripts/release-gate.cjs", "check"]],
    ["make", ["desktop-verify"]],
    ["node", ["scripts/release-draft.cjs", "create", "v0.1.3"]],
    ["node", ["scripts/release-draft.cjs", "status", "v0.1.3"]],
  ]);
});
