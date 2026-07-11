const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "..", "..");

test("desktop packaging defaults to manual updates without a certificate", () => {
  assert.equal(loadUpdateMode(), "manual");
});

test("a signing identity does not implicitly enable automatic updates", () => {
  assert.equal(loadUpdateMode({ PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)" }), "manual");
});

test("automatic updates require an explicit build mode", () => {
  assert.equal(
    loadUpdateMode({
      PUDDING_MAC_IDENTITY: "Developer ID Application: Test (TEAMID)",
      PUDDING_UPDATE_MODE: "automatic",
    }),
    "automatic",
  );
});

function loadUpdateMode(overrides = {}) {
  const script = "process.stdout.write(require('./packaging/electron-builder.config.cjs').extraMetadata.puddingUpdateMode)";
  return execFileSync(process.execPath, ["-e", script], {
    cwd: root,
    encoding: "utf8",
    env: {
      ...process.env,
      PUDDING_MAC_IDENTITY: "",
      PUDDING_UPDATE_MODE: "",
      ...overrides,
    },
  }).trim();
}
