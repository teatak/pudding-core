const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "..", "..");

test("desktop packaging defaults to manual updates without a certificate", () => {
  assert.equal(loadUpdateMode(), "manual");
});

test("a signing identity does not implicitly enable automatic updates", () => {
  assert.equal(
    loadUpdateMode({
      PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
      APPLE_KEYCHAIN_PROFILE: "test-notary",
    }),
    "manual",
  );
});

test("automatic updates require an explicit build mode", () => {
  assert.equal(
    loadUpdateMode({
      PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
      PUDDING_UPDATE_MODE: "automatic",
      APPLE_KEYCHAIN_PROFILE: "test-notary",
    }),
    "automatic",
  );
});

test("Developer ID builds require notarization credentials", () => {
  assert.throws(
    () => loadUpdateMode({ PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)" }),
    /Developer ID builds require Apple notarization credentials/,
  );
});

function loadUpdateMode(overrides = {}) {
  const script = "process.stdout.write(require('./packaging/electron-builder.config.cjs').extraMetadata.puddingUpdateMode)";
  return execFileSync(process.execPath, ["-e", script], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...process.env,
      PUDDING_MAC_IDENTITY: "",
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
