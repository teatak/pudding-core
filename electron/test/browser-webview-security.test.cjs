const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
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
  const trustedPreloadPath = "/app/electron/browser-preload.cjs";

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
    { partition: "persist:pudding-default", src: "file:///tmp/unmanaged.html" },
    { partition: "persist:pudding-default", src: "data:text/html,unmanaged" },
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

test("allows a managed tab to reattach at its current URL", () => {
  let prevented = false;
  const sourceURL = "file:///tmp/managed.html";
  assert.equal(hardenManagedBrowserWebview(
    { preventDefault: () => { prevented = true; } },
    {},
    { partition: "persist:pudding-default", src: sourceURL },
    "persist:pudding-default",
    "/app/electron/browser-preload.cjs",
    (url) => url === sourceURL,
  ), true);
  assert.equal(prevented, false);
});

test("rejects an unmanaged remote page in the managed browser partition", () => {
  let prevented = false;
  assert.equal(hardenManagedBrowserWebview(
    { preventDefault: () => { prevented = true; } },
    {},
    { partition: "persist:pudding-default", src: "https://example.com/redirected" },
    "persist:pudding-default",
    "/app/electron/browser-preload.cjs",
  ), false);
  assert.equal(prevented, true);
});

test("credential suggestions require an explicit trusted user selection", () => {
  const preload = fs.readFileSync(path.join(__dirname, "..", "browser-preload.cjs"), "utf8");
  const main = fs.readFileSync(path.join(__dirname, "..", "main.cjs"), "utf8");
  const formHandler = main.slice(
    main.indexOf('ipcMain.on("pudding:browser:credential-form"'),
    main.indexOf('ipcMain.on("pudding:browser:credential-focus"'),
  );

  assert.match(preload, /attachShadow\(\{ mode: "closed" \}\)/);
  assert.match(preload, /if \(!event\.isTrusted\) return;/);
  assert.match(preload, /pudding:browser:credential-fill-request/);
  assert.doesNotMatch(formHandler, /sendCredentialFill|credential-fill/);
});
