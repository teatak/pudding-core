const assert = require("node:assert/strict");
const test = require("node:test");

const {
  buildReleaseEnvironment,
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
    {},
    "stable",
    "Developer ID Application: Gang Yang (7K47HJ79JA)",
    "token",
  );
  assert.equal(env.PUDDING_RELEASE_CHANNEL, "stable");
  assert.equal(env.APPLE_KEYCHAIN_PROFILE, "pudding-notary");
  assert.equal(env.GH_TOKEN, "token");
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
