const assert = require("node:assert/strict");
const test = require("node:test");

const {
  closeLeakedApp,
  isRetainedLaunch,
  isRunningInventoryApp,
} = require("../../scripts/computer-use-product-smoke.cjs");

test("Computer Use product smoke ignores installed apps that are not running", () => {
  const appID = "com.apple.calculator";

  assert.equal(isRunningInventoryApp({ bundleID: appID, running: false }, appID), false);
  assert.equal(isRunningInventoryApp({ bundleID: appID, running: true, pid: 42 }, appID), true);
  assert.equal(isRunningInventoryApp({ bundleID: appID, pid: 42 }, appID), false);
  assert.equal(isRunningInventoryApp({ bundleID: "com.example.Other", running: true, pid: 42 }, appID), false);
});

test("Computer Use product smoke verifies that a pre-existing process was retained", () => {
  const appID = "com.example.App";
  assert.equal(isRetainedLaunch({ bundleID: appID, newlyLaunched: false, pid: 42 }, appID, 42), true);
  assert.equal(isRetainedLaunch({ bundleID: appID, newlyLaunched: true, pid: 42 }, appID, 42), false);
  assert.equal(isRetainedLaunch({ bundleID: appID, newlyLaunched: false, pid: 43 }, appID, 42), false);
});

test("Computer Use product smoke cleans only the running process it owns", async () => {
  const requests = [];
  const host = {
    async listApps() {
      return { apps: [{ bundleID: "com.example.App", running: true }] };
    },
    async useApp(params) {
      requests.push(["use", params]);
      return { bundleID: params.bundleID, newlyLaunched: false, pid: 42 };
    },
    async quitApp(params) {
      requests.push(["quit", params]);
    },
  };

  await closeLeakedApp(host, "com.example.App", 42);
  assert.deepEqual(requests, [
    ["use", { bundleID: "com.example.App" }],
    ["quit", { bundleID: "com.example.App", pid: 42 }],
  ]);
});

test("Computer Use product smoke leaves stopped and unrelated processes alone", async () => {
  let used = false;
  const stoppedHost = {
    async listApps() {
      return { apps: [{ bundleID: "com.example.App", running: false }] };
    },
    async useApp() {
      used = true;
    },
  };
  await closeLeakedApp(stoppedHost, "com.example.App", 42);
  assert.equal(used, false);

  let quit = false;
  const replacedHost = {
    async listApps() {
      return { apps: [{ bundleID: "com.example.App", running: true }] };
    },
    async useApp() {
      return { bundleID: "com.example.App", newlyLaunched: false, pid: 43 };
    },
    async quitApp() {
      quit = true;
    },
  };
  await closeLeakedApp(replacedHost, "com.example.App", 42);
  assert.equal(quit, false);
});
