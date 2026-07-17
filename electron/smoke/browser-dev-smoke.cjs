const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow, ipcMain, webContents } = require("electron");

const { BrowserHost } = require("../browser-host.cjs");

const smokeHome = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-electron-browser-smoke-"));
app.setPath("userData", path.join(smokeHome, "user-data"));

let window;
let server;
let host;
let failed = false;

const timeout = setTimeout(() => finish(new Error("Electron browser dev smoke timed out")), 60_000);

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
    ({ sessionID, tabID }) => window.webContents.executeJavaScript(`(() => {
      const target = document.querySelector(${JSON.stringify(`webview[data-browser-key="${sessionID}:${tabID}"]`)});
      if (!target) return false;
      for (let ancestor = target.parentElement; ancestor; ancestor = ancestor.parentElement) {
        if (ancestor.inert) ancestor.inert = false;
      }
      if (getComputedStyle(target).visibility === "hidden") {
        target.style.opacity = "0";
        target.style.visibility = "visible";
      }
      target.focus();
      return document.activeElement === target;
    })()`),
  );
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
  const runtimeID = webTab.runtimeID;
  await host.loadURL({ sessionID: webTab.sessionID, tabID: webTab.tabID, url: `${pageBaseURL}/two` });
  assert.equal((await host.back({ sessionID: webTab.sessionID, tabID: webTab.tabID })).url, `${pageBaseURL}/one`);
  assert.equal((await host.forward({ sessionID: webTab.sessionID, tabID: webTab.tabID })).url, `${pageBaseURL}/two`);

  await host.loadURL({ sessionID: webTab.sessionID, tabID: webTab.tabID, url: `${pageBaseURL}/form` });
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
  assert.equal(await window.webContents.executeJavaScript(`document.getElementById("host-composer").value`), "host-draft");
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

  const screenshot = await host.screenshot({ sessionID: webTab.sessionID, tabID: webTab.tabID });
  assert.equal(screenshot.mime, "image/png");
  assert.ok(screenshot.size > 0);
  assert.deepEqual(await host.revokeFileAccess({ sessionID: "smoke-session-a" }), { closedTabIDs: ["smoke-file"] });
  assert.equal(host.listTabs({ sessionID: "smoke-session-a" }).tabs.length, 1);

  process.stdout.write(JSON.stringify({ ok: true, checks: ["file", "multi-tab", "multi-session", "history", "focus-isolation", "screenshot", "revoke"] }) + "\n");
  finish();
}

function startPageServer() {
  server = http.createServer((request, response) => {
    const pages = {
      "/one": "<!doctype html><title>One</title><main>Page one</main>",
      "/two": "<!doctype html><title>Two</title><main>Page two</main>",
      "/other": "<!doctype html><title>Other</title><main>Other session</main>",
      "/form": "<!doctype html><title>Form</title><label>Target <input id=\"target\"></label>",
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

function finish(error) {
  if (failed) return;
  failed = true;
  clearTimeout(timeout);
  const exitCode = error ? 1 : 0;
  if (error) {
    console.error(error);
  }
  host?.closeAll();
  ipcMain.removeAllListeners("pudding-browser-smoke:webview-register");
  if (window && !window.isDestroyed()) window.destroy();
  const quit = () => {
    fs.rmSync(smokeHome, { recursive: true, force: true });
    app.exit(exitCode);
  };
  if (server) server.close(quit);
  else quit();
}
