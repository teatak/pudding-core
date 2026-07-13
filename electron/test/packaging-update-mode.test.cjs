const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "..", "..");

test("desktop packaging defaults to automatic updates and requires a certificate", () => {
  assert.throws(() => loadUpdateMode(), /PUDDING_MAC_IDENTITY is required for automatic macOS updates/);
});

test("signed desktop packaging defaults to automatic updates", () => {
  assert.equal(
    loadUpdateMode({
      PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
      APPLE_KEYCHAIN_PROFILE: "test-notary",
    }),
    "automatic",
  );
});

test("unsigned local packaging requires an explicit manual mode", () => {
  assert.equal(loadUpdateMode({ PUDDING_UPDATE_MODE: "manual" }), "manual");
});

test("a signed build can explicitly use manual mode for recovery testing", () => {
  assert.equal(
    loadUpdateMode({
      PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
      PUDDING_UPDATE_MODE: "manual",
      APPLE_KEYCHAIN_PROFILE: "test-notary",
    }),
    "manual",
  );
});

test("Developer ID builds require notarization credentials", () => {
  assert.throws(
    () => loadUpdateMode({ PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)" }),
    /Developer ID builds require Apple notarization credentials/,
  );
});

test("strips the Developer ID prefix before invoking Electron Builder", () => {
  assert.equal(
    loadConfigValue("mac.identity", {
      PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
      APPLE_KEYCHAIN_PROFILE: "test-notary",
    }),
    "Test (TEAMID)",
  );
});

test("desktop packaging defaults to the stable GitHub update channel", () => {
  const overrides = { PUDDING_UPDATE_MODE: "manual" };
  assert.equal(loadConfigValue("extraMetadata.puddingReleaseChannel", overrides), "stable");
  assert.equal(loadConfigValue("publish[0].channel", overrides), "latest");
  assert.equal(loadConfigValue("publish[0].releaseType", overrides), "release");
});

test("preview packaging publishes beta metadata as a GitHub prerelease", () => {
  const overrides = {
    PUDDING_APP_VERSION: "0.1.2-beta.1",
    PUDDING_RELEASE_CHANNEL: "preview",
    PUDDING_UPDATE_MODE: "manual",
  };
  assert.equal(loadConfigValue("appId", overrides), "com.teatak.pudding");
  assert.equal(loadConfigValue("productName", overrides), "Pudding");
  assert.equal(loadConfigValue("extraMetadata.puddingReleaseChannel", overrides), "preview");
  assert.equal(loadConfigValue("publish[0].channel", overrides), "beta");
  assert.equal(loadConfigValue("publish[0].releaseType", overrides), "prerelease");
});

test("local publishing can stage stable and preview packages as draft releases", () => {
  assert.equal(
    loadConfigValue("publish[0].releaseType", {
      PUDDING_RELEASE_DRAFT: "1",
      PUDDING_UPDATE_MODE: "manual",
    }),
    "draft",
  );
  assert.equal(
    loadConfigValue("publish[0].releaseType", {
      PUDDING_APP_VERSION: "0.1.2-beta.1",
      PUDDING_RELEASE_CHANNEL: "preview",
      PUDDING_RELEASE_DRAFT: "1",
      PUDDING_UPDATE_MODE: "manual",
    }),
    "draft",
  );
});

test("release channel rejects mismatched versions", () => {
  assert.throws(
    () => loadConfigValue("publish[0].channel", { PUDDING_RELEASE_CHANNEL: "preview" }),
    /preview release version must match x\.y\.z-beta\.n/,
  );
  assert.throws(
    () => loadConfigValue("publish[0].channel", { PUDDING_APP_VERSION: "0.1.2-beta.1" }),
    /stable release version must match x\.y\.z/,
  );
});

function loadUpdateMode(overrides = {}) {
  return loadConfigValue("extraMetadata.puddingUpdateMode", overrides);
}

function loadConfigValue(pathExpression, overrides = {}) {
  const script = `process.stdout.write(require('./packaging/electron-builder.config.cjs').${pathExpression})`;
  return execFileSync(process.execPath, ["-e", script], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...process.env,
      PUDDING_MAC_IDENTITY: "",
      PUDDING_APP_VERSION: "",
      PUDDING_RELEASE_CHANNEL: "",
      PUDDING_RELEASE_DRAFT: "",
      PUDDING_UPDATE_MODE: "",
      APPLE_KEYCHAIN_PROFILE: "",
      APPLE_ID: "",
      APPLE_APP_SPECIFIC_PASSWORD: "",
      APPLE_TEAM_ID: "",
      APPLE_API_KEY: "",
      APPLE_API_KEY_ID: "",
      APPLE_API_ISSUER: "",
      ...overrides,
    },
  }).trim();
}
