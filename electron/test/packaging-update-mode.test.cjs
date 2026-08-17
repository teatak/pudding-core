const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "..", "..");
const packageMetadata = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8"));
const currentReleaseChannel = packageMetadata.version.includes("-beta.") ? "preview" : "stable";

test("desktop development and packaging use the same exact Electron version", () => {
  const webPackage = JSON.parse(fs.readFileSync(path.join(root, "web", "package.json"), "utf8"));
  assert.match(webPackage.devDependencies.electron, /^\d+\.\d+\.\d+$/);
  assert.equal(
    loadConfigValue("electronVersion", signedBuildEnv()),
    webPackage.devDependencies.electron,
  );
});

test("direct Electron Builder packaging is rejected", () => {
  assert.throws(
    () => loadConfigValue("appId", { PUDDING_PACKAGING_PIPELINE: "" }),
    /run make desktop-bundle/,
  );
});

test("desktop packaging always requires a Developer ID certificate", () => {
  assert.throws(() => loadConfigValue("forceCodeSigning"), /PUDDING_MAC_IDENTITY is required/);
});

test("signed desktop packaging always enables hardened code signing", () => {
  assert.equal(loadConfigValue("forceCodeSigning", signedBuildEnv()), "true");
  assert.equal(loadConfigValue("mac.hardenedRuntime", signedBuildEnv()), "true");
  assert.equal(loadConfigValue("mac.extendInfo.LSMinimumSystemVersion", signedBuildEnv()), "14.0");
});

test("signed desktop packaging grants camera and microphone access", () => {
  const entitlements = loadConfigValue("mac.entitlements", signedBuildEnv());
  assert.equal(loadConfigValue("mac.entitlementsInherit", signedBuildEnv()), entitlements);
  const contents = fs.readFileSync(path.join(root, entitlements), "utf8");
  assert.match(contents, /com\.apple\.security\.device\.camera/);
  assert.match(contents, /com\.apple\.security\.device\.audio-input/);
  assert.match(loadConfigValue("mac.extendInfo.NSCameraUsageDescription", signedBuildEnv()), /camera/i);
  assert.match(loadConfigValue("mac.extendInfo.NSMicrophoneUsageDescription", signedBuildEnv()), /microphone/i);
  assert.match(loadConfigValue("mac.extendInfo.NSScreenCaptureUsageDescription", signedBuildEnv()), /screen/i);
  assert.match(loadConfigValue("mac.extendInfo.NSLocalNetworkUsageDescription", signedBuildEnv()), /local network/i);
});

test("legacy update mode overrides are rejected", () => {
  assert.throws(
    () => loadConfigValue("forceCodeSigning", signedBuildEnv({ PUDDING_UPDATE_MODE: "manual" })),
    /PUDDING_UPDATE_MODE is no longer supported/,
  );
});

test("Developer ID builds require notarization credentials", () => {
  assert.throws(
    () => loadConfigValue("forceCodeSigning", { PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)" }),
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
  const overrides = signedBuildEnv({
    PUDDING_APP_VERSION: "0.1.2",
    PUDDING_RELEASE_CHANNEL: "",
  });
  assert.equal(loadConfigValue("extraMetadata.puddingReleaseChannel", overrides), "stable");
  assert.equal(loadConfigValue("publish[0].channel", overrides), "latest");
  assert.equal(loadConfigValue("publish[0].releaseType", overrides), "release");
});

test("preview packaging publishes beta metadata as a GitHub prerelease", () => {
  const overrides = {
    ...signedBuildEnv(),
    PUDDING_APP_VERSION: "0.1.2-beta.1",
    PUDDING_RELEASE_CHANNEL: "preview",
  };
  assert.equal(loadConfigValue("appId", overrides), "com.teatak.pudding");
  assert.equal(loadConfigValue("productName", overrides), "Pudding");
  assert.equal(loadConfigValue("extraMetadata.puddingReleaseChannel", overrides), "preview");
  assert.equal(loadConfigValue("publish[0].channel", overrides), "beta");
  assert.equal(loadConfigValue("publish[0].releaseType", overrides), "prerelease");
});

test("release channel rejects mismatched versions", () => {
  assert.throws(
    () => loadConfigValue("publish[0].channel", signedBuildEnv({
      PUDDING_APP_VERSION: "0.1.2",
      PUDDING_RELEASE_CHANNEL: "preview",
    })),
    /preview release version must match x\.y\.z-beta\.n/,
  );
  assert.throws(
    () => loadConfigValue("publish[0].channel", signedBuildEnv({
      PUDDING_APP_VERSION: "0.1.2-beta.1",
      PUDDING_RELEASE_CHANNEL: "stable",
    })),
    /stable release version must match x\.y\.z/,
  );
});

function loadConfigValue(pathExpression, overrides = {}) {
  const script = `process.stdout.write(String(require('./packaging/electron-builder.config.cjs').${pathExpression}))`;
  return execFileSync(process.execPath, ["-e", script], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...process.env,
      PUDDING_PACKAGING_PIPELINE: "1",
      PUDDING_MAC_IDENTITY: "",
      PUDDING_APP_VERSION: "",
      PUDDING_RELEASE_CHANNEL: currentReleaseChannel,
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

function signedBuildEnv(overrides = {}) {
  return {
    PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
    APPLE_KEYCHAIN_PROFILE: "test-notary",
    ...overrides,
  };
}
