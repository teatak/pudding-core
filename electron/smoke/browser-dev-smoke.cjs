const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow, ipcMain, nativeImage, session, webContents } = require("electron");

const { resolveBrowserFavicon } = require("../browser-favicon.cjs");
const { BrowserHost } = require("../browser-host.cjs");
const { managedBrowserPartition } = require("../browser-permissions.cjs");
const { hardenManagedBrowserWebview } = require("../browser-webview-security.cjs");

const smokeHome = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-electron-browser-smoke-"));
app.setPath("userData", path.join(smokeHome, "user-data"));

let window;
let server;
let host;
let failed = false;
let currentCheck = "startup";
const popupWindows = new Set();
const pendingPopupWaiters = [];

const timeout = setTimeout(() => finish(new Error(`Electron browser dev smoke timed out during ${currentCheck}`)), 60_000);

void app.whenReady().then(run).catch(finish);

async function run() {
  const pageBaseURL = await startPageServer();
  const projectRoot = path.join(smokeHome, "project");
  fs.mkdirSync(projectRoot, { recursive: true });
  const projectFile = path.join(projectRoot, "index.html");
  fs.writeFileSync(projectFile, "<!doctype html><title>Project File</title><main>Project file ready</main>");

  window = new BrowserWindow({
    show: true,
    width: 900,
    height: 700,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
      webviewTag: true,
      backgroundThrottling: false,
      preload: path.join(__dirname, "browser-dev-smoke-preload.cjs"),
    },
  });
  host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => {
      window.webContents.send("pudding-browser-smoke:webview-required", request);
    },
    undefined,
    {
      resolveFavicon: ({ url, pageURL }) => resolveBrowserFavicon({
        url,
        pageURL,
        fetch: (resource, init) => session.fromPartition(managedBrowserPartition).fetch(resource, init),
        nativeImage,
      }),
      created: (popupWindow) => {
        popupWindows.add(popupWindow);
        popupWindow.once("closed", () => popupWindows.delete(popupWindow));
        pendingPopupWaiters.shift()?.(popupWindow);
      },
    },
  );
  window.webContents.on("will-attach-webview", (event, webPreferences, params) => {
    if (!hardenManagedBrowserWebview(event, webPreferences, params, managedBrowserPartition)) {
      finish(new Error("smoke webview rejected by managed attachment policy"));
    }
  });
  ipcMain.on("pudding-browser-smoke:webview-register", (_event, payload) => {
    const target = webContents.fromId(Number(payload?.webContentsID));
    void host.registerWebContents(payload?.request || {}, target).catch(finish);
  });
  await window.loadFile(path.join(__dirname, "browser-dev-smoke.html"));

  const fileTab = await host.ensure({
    sessionID: "smoke-session-a",
    tabID: "smoke-file",
    url: pathToFileURL(projectFile).toString(),
    fileRoot: projectRoot,
    _fileAuthorized: true,
  });
  assert.match((await host.observe({ sessionID: fileTab.sessionID, tabID: fileTab.tabID })).text, /Project file ready/);

  const webTab = await host.ensure({ sessionID: "smoke-session-a", tabID: "smoke-web", url: `${pageBaseURL}/one` });
  currentCheck = "favicon localization";
  let localizedFaviconURL = "";
  await waitUntil(() => {
    localizedFaviconURL = host.listTabs({ sessionID: webTab.sessionID }).tabs.find((tab) => tab.tabID === webTab.tabID)?.faviconURL || "";
    return localizedFaviconURL.startsWith("data:image/png;base64,");
  });
  const runtimeID = webTab.runtimeID;
  await host.loadURL({ sessionID: webTab.sessionID, tabID: webTab.tabID, url: `${pageBaseURL}/two` });
  assert.equal((await host.back({ sessionID: webTab.sessionID, tabID: webTab.tabID })).url, `${pageBaseURL}/one`);
  assert.equal((await host.forward({ sessionID: webTab.sessionID, tabID: webTab.tabID })).url, `${pageBaseURL}/two`);

  await host.loadURL({ sessionID: webTab.sessionID, tabID: webTab.tabID, url: `${pageBaseURL}/form` });
  currentCheck = "focus isolation";
  await window.webContents.executeJavaScript(`(() => {
    const composer = document.getElementById("host-composer");
    const tabs = document.getElementById("tabs");
    const target = document.querySelector('webview[data-browser-key="smoke-session-a:smoke-web"]');
    tabs.inert = true;
    target.style.visibility = "hidden";
    composer.value = "host-draft";
    composer.focus();
    return document.activeElement === composer;
  })()`);
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#target-button" });
  await assert.rejects(
    host.type({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#target-button", text: "must-not-apply", clear: true }),
    /target is not editable/,
  );
  await host.scroll({ sessionID: webTab.sessionID, tabID: webTab.tabID, deltaY: 200 });
  assert.deepEqual(
    await window.webContents.executeJavaScript(`({activeElementID: document.activeElement?.id || "", composerValue: document.getElementById("host-composer").value})`),
    { activeElementID: "host-composer", composerValue: "host-draft" },
  );
  let typed;
  try {
    typed = await host.type({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#target", text: "webview-only", clear: true });
  } catch (error) {
    const observation = await host.observe({ sessionID: webTab.sessionID, tabID: webTab.tabID });
    const hostDraft = await window.webContents.executeJavaScript(`document.getElementById("host-composer").value`);
    console.error(JSON.stringify({ hostDraft, target: observation.elements.find((element) => element.selector === "#target") }));
    throw error;
  }
  assert.equal(typed.result.valueLength, "webview-only".length);
  currentCheck = "textarea input isolation";
  const textareaTyped = await host.type({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#target-textarea", text: "textarea-value", clear: true });
  currentCheck = "contenteditable input isolation";
  let editorTyped;
  try {
    editorTyped = await host.type({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#target-editor", text: "editor-value", clear: true });
  } catch (error) {
    console.error(JSON.stringify(await host.observe({ sessionID: webTab.sessionID, tabID: webTab.tabID })));
    throw error;
  }
  assert.equal(textareaTyped.result.valueLength, "textarea-value".length);
  assert.equal(editorTyped.result.valueLength, "editor-value".length);
  assert.match(
    (await host.observe({ sessionID: webTab.sessionID, tabID: webTab.tabID })).text,
    /Controlled:webview-only[\s\S]*beforeinput:1 input:1[\s\S]*Clicked:1/,
  );
  assert.deepEqual(
    await window.webContents.executeJavaScript(`(() => ({
      activeElementID: document.activeElement?.id || "",
      composerValue: document.getElementById("host-composer").value,
      tabsInert: document.getElementById("tabs").inert,
      webviewVisibility: document.querySelector('webview[data-browser-key="smoke-session-a:smoke-web"]').style.visibility,
    }))()`),
    {
      activeElementID: "host-composer",
      composerValue: "host-draft",
      tabsInert: true,
      webviewVisibility: "hidden",
    },
  );
  await window.webContents.executeJavaScript(`(() => {
    const target = document.querySelector('webview[data-browser-key="smoke-session-a:smoke-web"]');
    document.getElementById("tabs").inert = false;
    target.style.opacity = "";
    target.style.visibility = "";
  })()`);

  const otherTab = await host.ensure({ sessionID: "smoke-session-b", tabID: "smoke-other", url: `${pageBaseURL}/other` });
  assert.equal(host.listTabs({ sessionID: "smoke-session-a" }).tabs.length, 2);
  assert.equal(host.listTabs({ sessionID: "smoke-session-b" }).tabs.length, 1);
  assert.equal((await host.ensure({ sessionID: webTab.sessionID, tabID: webTab.tabID, url: `${pageBaseURL}/two` })).runtimeID, runtimeID);
  assert.match((await host.observe({ sessionID: otherTab.sessionID, tabID: otherTab.tabID })).text, /Other session/);

  await host.loadURL({ sessionID: webTab.sessionID, tabID: webTab.tabID, url: `${pageBaseURL}/popup-parent` });
  currentCheck = "target blank referrer";
  const existingTabIDs = new Set(host.listTabs({ sessionID: webTab.sessionID }).tabs.map((tab) => tab.tabID));
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#open-managed-tab" });
  let linkedTab;
  await waitUntil(() => {
    linkedTab = host.listTabs({ sessionID: webTab.sessionID }).tabs.find((tab) => !existingTabIDs.has(tab.tabID));
    return linkedTab?.status === "detached";
  });
  let linkedTabText = "";
  await waitUntil(async () => {
    linkedTabText = (await host.observe({ sessionID: linkedTab.sessionID, tabID: linkedTab.tabID })).text;
    return /Referrer: .*\/popup-parent/.test(linkedTabText);
  });
  assert.match(linkedTabText, /Referrer: .*\/popup-parent/);

  currentCheck = "window.open about blank creation";
  const blankPopupPromise = nextPopupWindow();
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#open-blank" });
  const blankPopup = await blankPopupPromise;
  currentCheck = "window.open about blank document.write";
  await waitUntil(async () => /about blank ready/.test(await blankPopup.webContents.executeJavaScript("document.body?.innerText || ''")));
  assert.equal(blankPopup.webContents.getURL(), "about:blank");
  assert.equal(await blankPopup.webContents.executeJavaScript("window.opener !== null"), true);
  currentCheck = "window.open parent to child postMessage";
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#message-blank" });
  await waitUntil(async () => /parent ready/.test(await blankPopup.webContents.executeJavaScript("document.body?.innerText || ''")));
  currentCheck = "window.open named reuse";
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#reuse-blank" });
  assert.match((await host.observe({ sessionID: webTab.sessionID, tabID: webTab.tabID })).text, /named window reused/);
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#focus-blank" });
  currentCheck = "window.open parent proxy close";
  const blankClosed = new Promise((resolve) => blankPopup.once("closed", resolve));
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#close-blank" });
  await blankClosed;

  currentCheck = "blob window.open creation";
  const blobPopupPromise = nextPopupWindow();
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#open-blob" });
  const blobPopup = await blobPopupPromise;
  currentCheck = "blob window.open load";
  await waitUntil(async () => /blob popup ready/.test(await blobPopup.webContents.executeJavaScript("document.body?.innerText || ''")));
  assert.match(blobPopup.webContents.getURL(), /^blob:http:\/\/127\.0\.0\.1:/);
  const blobClosed = new Promise((resolve) => blobPopup.once("closed", resolve));
  blobPopup.close();
  await blobClosed;

  currentCheck = "window.open popup creation";
  const popupPromise = nextPopupWindow();
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#open-popup" });
  const popup = await popupPromise;
  currentCheck = "window.open popup load";
  await waitForLoad(popup.webContents);
  assert.equal(await popup.webContents.executeJavaScript("window.opener !== null"), true);
  assert.equal(await popup.webContents.executeJavaScript("typeof window.opener.postMessage"), "function");
  currentCheck = "opener postMessage";
  await waitUntil(async () => /popup ready/.test((await host.observe({ sessionID: webTab.sessionID, tabID: webTab.tabID })).text));
  await popup.webContents.executeJavaScript("window.focus(); true");
  const popupClosed = new Promise((resolve) => popup.once("closed", resolve));
  await popup.webContents.executeJavaScript("setTimeout(() => window.close(), 0); true");
  currentCheck = "window.close";
  await popupClosed;

  currentCheck = "noopener popup creation";
  const detachedPopupPromise = nextPopupWindow();
  await host.click({ sessionID: webTab.sessionID, tabID: webTab.tabID, selector: "#open-detached" });
  const detachedPopup = await detachedPopupPromise;
  currentCheck = "noopener popup load";
  await waitForLoad(detachedPopup.webContents);
  assert.deepEqual(
    await detachedPopup.webContents.executeJavaScript("({ openerMissing: window.opener === null, referrer: document.referrer })"),
    { openerMissing: true, referrer: "" },
  );
  const detachedClosed = new Promise((resolve) => detachedPopup.once("closed", resolve));
  detachedPopup.close();
  await detachedClosed;

  currentCheck = "screenshot";
  const screenshot = await host.screenshot({ sessionID: webTab.sessionID, tabID: webTab.tabID });
  assert.equal(screenshot.mime, "image/png");
  assert.ok(screenshot.size > 0);
  currentCheck = "file grant revocation";
  assert.deepEqual(await host.revokeFileAccess({ sessionID: "smoke-session-a" }), { closedTabIDs: ["smoke-file"] });
  assert.equal(host.listTabs({ sessionID: "smoke-session-a" }).tabs.length, 2);

  process.stdout.write(JSON.stringify({ ok: true, checks: ["file", "favicon", "multi-tab", "multi-session", "history", "focus-isolation", "target-blank-referrer", "window-open-about-blank", "parent-child-window-proxy", "named-window-reuse", "blob-window", "window-open", "opener-post-message", "window-close-focus", "noopener-noreferrer", "screenshot", "revoke"] }) + "\n");
  finish();
}

function startPageServer() {
  server = http.createServer((request, response) => {
    if (request.url === "/favicon.png") {
      response.writeHead(200, { "content-type": "image/png" });
      response.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"));
      return;
    }
    const pages = {
      "/one": "<!doctype html><title>One</title><link rel=icon href=/favicon.png><main>Page one</main>",
      "/two": "<!doctype html><title>Two</title><main>Page two</main>",
      "/other": "<!doctype html><title>Other</title><main>Other session</main>",
      "/form": `<!doctype html><title>Form</title>
        <label>Target <input id="target"></label>
        <output id="target-state">Controlled:Waiting</output>
        <output id="target-events">beforeinput:0 input:0</output>
        <textarea id="target-textarea">old textarea</textarea>
        <div id="target-editor" contenteditable="true">old editor</div>
        <button id="target-button" value="button-value">Click target</button>
        <output id="click-state">Clicked:0</output>
        <div style="height:1200px"></div>
        <script>
          const target = document.getElementById('target');
          const nativeValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
          let trackedValue = nativeValue.get.call(target);
          let beforeInputCount = 0;
          let inputCount = 0;
          const updateEvents = () => document.getElementById('target-events').textContent = 'beforeinput:' + beforeInputCount + ' input:' + inputCount;
          Object.defineProperty(target, 'value', {
            configurable: true,
            get() { return nativeValue.get.call(this); },
            set(value) {
              trackedValue = String(value);
              nativeValue.set.call(this, value);
            },
          });
          target.addEventListener('beforeinput', () => { beforeInputCount += 1; updateEvents(); });
          target.addEventListener('input', () => {
            inputCount += 1;
            updateEvents();
            const current = target.value;
            if (current === trackedValue) return;
            trackedValue = current;
            document.getElementById('target-state').textContent = 'Controlled:' + current;
          });
          let clickCount = 0;
          document.getElementById('target-button').addEventListener('click', () => {
            clickCount += 1;
            document.getElementById('click-state').textContent = 'Clicked:' + clickCount;
          });
        </script>`,
      "/popup-parent": `<!doctype html><title>Popup parent</title>
        <a id="open-managed-tab" href="/referrer-child" target="_blank">Open managed tab</a>
        <button id="open-blank" onclick="openBlankPopup()">Open blank</button>
        <button id="message-blank" onclick="blankPopup.postMessage('parent-ready', location.origin)">Message blank</button>
        <button id="reuse-blank" onclick="reuseBlankPopup()">Reuse blank</button>
        <button id="focus-blank" onclick="blankPopup.focus()">Focus blank</button>
        <button id="close-blank" onclick="blankPopup.close()">Close blank</button>
        <button id="open-blob" onclick="openBlobPopup()">Open blob</button>
        <button id="open-popup" onclick="window.popupRef=window.open('/popup-child','oauth-popup','width=480,height=480')">Open popup</button>
        <button id="open-detached" onclick="window.open('/popup-child','_blank','noopener,noreferrer,width=480,height=480')">Open detached</button>
        <output id="popup-status">waiting</output>
        <output id="reuse-status"></output>
        <script>
          let blankPopup;
          let blobURL;
          function openBlankPopup() {
            blankPopup = window.open();
            blankPopup.name = 'reuse-popup';
            blankPopup.document.write('<!doctype html><title>Blank child</title><main>about blank ready</main><output id="from-parent">waiting</output><scr' + 'ipt>addEventListener("message",event=>{if(event.origin===location.origin&&event.data==="parent-ready")document.getElementById("from-parent").textContent="parent ready"})</scr' + 'ipt>');
            blankPopup.document.close();
          }
          function reuseBlankPopup() {
            const reused = window.open('', 'reuse-popup');
            document.getElementById('reuse-status').textContent = reused === blankPopup ? 'named window reused' : 'named window replaced';
          }
          function openBlobPopup() {
            blobURL = URL.createObjectURL(new Blob(['<!doctype html><title>Blob child</title><main>blob popup ready</main>'], { type: 'text/html' }));
            window.open(blobURL, 'blob-popup', 'width=480,height=480');
          }
          addEventListener('message', (event) => { if (event.origin === location.origin && event.data === 'popup-ready') document.getElementById('popup-status').textContent = 'popup ready'; });
        </script>`,
      "/popup-child": "<!doctype html><title>Popup child</title><main>Popup child</main><script>window.opener?.postMessage('popup-ready', location.origin)</script>",
      "/referrer-child": "<!doctype html><title>Referrer child</title><main>Referrer: <script>document.write(document.referrer)</script></main>",
    };
    const body = pages[request.url];
    if (!body) {
      response.writeHead(404).end("not found");
      return;
    }
    response.writeHead(200, { "content-type": "text/html; charset=utf-8" }).end(body);
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve(`http://127.0.0.1:${server.address().port}`));
  });
}

function nextPopupWindow() {
  return new Promise((resolve) => pendingPopupWaiters.push(resolve));
}

function waitForLoad(contents) {
  if (!contents.isLoadingMainFrame()) {
    return Promise.resolve();
  }
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      contents.off("did-finish-load", loaded);
      contents.off("did-fail-load", failedLoad);
    };
    const loaded = () => {
      cleanup();
      resolve();
    };
    const failedLoad = (_event, code, description, _url, isMainFrame) => {
      if (isMainFrame === false || code === -3) return;
      cleanup();
      reject(new Error(`popup load failed: ${description || code}`));
    };
    contents.once("did-finish-load", loaded);
    contents.on("did-fail-load", failedLoad);
  });
}

async function waitUntil(predicate) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (await predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("popup condition timed out");
}

function finish(error) {
  if (failed) return;
  failed = true;
  clearTimeout(timeout);
  const exitCode = error ? 1 : 0;
  if (error) {
    console.error(error);
  }
  host?.closeAll();
  for (const popup of popupWindows) {
    if (!popup.isDestroyed()) popup.destroy();
  }
  ipcMain.removeAllListeners("pudding-browser-smoke:webview-register");
  if (window && !window.isDestroyed()) window.destroy();
  const quit = () => {
    fs.rmSync(smokeHome, { recursive: true, force: true });
    app.exit(exitCode);
  };
  if (server) server.close(quit);
  else quit();
}
