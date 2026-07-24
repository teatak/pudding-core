const assert = require("node:assert/strict");
const test = require("node:test");

const { hardenManagedBrowserWebview } = require("../browser-webview-security.cjs");

test("hardens the managed browser webview before attachment", () => {
  let prevented = false;
  const preferences = {
    allowRunningInsecureContent: true,
    contextIsolation: false,
    enableBlinkFeatures: "DangerousFeature",
    experimentalFeatures: true,
    nodeIntegration: true,
    nodeIntegrationInSubFrames: true,
    nodeIntegrationInWorker: true,
    partition: "persist:pudding-default",
    plugins: true,
    preload: "/tmp/untrusted-preload.cjs",
    sandbox: false,
    webSecurity: false,
    webviewTag: true,
  };
  const params = {
    partition: "persist:pudding-default",
    preload: "file:///tmp/untrusted-preload.cjs",
    src: "about:blank",
  };
  const trustedPreloadPath = "/app/electron/browser-selection-preload.cjs";

  assert.equal(hardenManagedBrowserWebview(
    { preventDefault: () => { prevented = true; } },
    preferences,
    params,
    "persist:pudding-default",
    trustedPreloadPath,
  ), true);
  assert.equal(prevented, false);
  assert.equal(preferences.preload, trustedPreloadPath);
  assert.equal(params.preload, undefined);
  assert.equal(preferences.contextIsolation, true);
  assert.equal(preferences.nodeIntegration, false);
  assert.equal(preferences.nodeIntegrationInSubFrames, false);
  assert.equal(preferences.nodeIntegrationInWorker, false);
  assert.equal(preferences.sandbox, true);
  assert.equal(preferences.webSecurity, true);
  assert.equal(preferences.webviewTag, false);
  assert.equal(preferences.allowRunningInsecureContent, false);
  assert.equal(preferences.experimentalFeatures, false);
  assert.equal(preferences.enableBlinkFeatures, "");
  assert.deepEqual(preferences.additionalArguments, []);
});

test("rejects unmanaged webview attachments", () => {
  for (const params of [
    { partition: "persist:other", src: "about:blank" },
    { partition: "persist:pudding-default", src: "https://example.com/" },
  ]) {
    let prevented = false;
    assert.equal(hardenManagedBrowserWebview(
      { preventDefault: () => { prevented = true; } },
      {},
      params,
      "persist:pudding-default",
    ), false);
    assert.equal(prevented, true);
  }
});
