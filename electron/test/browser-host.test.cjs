const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { pathToFileURL } = require("node:url");

const { BrowserHost } = require("../browser-host.cjs");

class FakeDebugger extends EventEmitter {
  constructor(webContents) {
    super();
    this.webContents = webContents;
    this.attached = false;
    this.attachCount = 0;
    this.commands = [];
    this.currentLoaderID = "loader-current";
  }

  isAttached() {
    return this.attached;
  }

  attach() {
    this.attached = true;
    this.attachCount += 1;
  }

  detach() {
    this.attached = false;
    this.emit("detach");
  }

  async sendCommand(method, params = {}) {
    this.commands.push({ method, params });
    if (method === "Page.enable" && this.failPageEnableOnce) {
      this.failPageEnableOnce = false;
      throw new Error("Page.enable failed once");
    }
    if (method === "Page.navigate") {
      const loaderID = `loader-${this.commands.length}`;
      this.webContents.url = params.url;
      if (!this.suppressNavigationEvent) {
        queueMicrotask(() => {
          this.currentLoaderID = loaderID;
          this.emit("message", {}, "Page.frameNavigated", { frame: { id: "main", loaderId: loaderID, url: params.url } });
        });
      }
      return { frameId: "main", loaderId: loaderID };
    }
    if (method === "Page.reload" || method === "Page.navigateToHistoryEntry") {
      const loaderID = `loader-${this.commands.length}`;
      if (this.deferNavigationCommand) {
        await new Promise((resolve) => {
          this.resolveNavigationCommand = resolve;
        });
      }
      if (!this.suppressNavigationEvent) {
        queueMicrotask(() => {
          this.currentLoaderID = loaderID;
          this.emit("message", {}, "Page.frameNavigated", { frame: { id: "main", loaderId: loaderID, url: this.webContents.url } });
        });
      }
      return {};
    }
    if (method === "Page.getNavigationHistory") {
      if (this.navigationHistoryFailures > 0) {
        this.navigationHistoryFailures -= 1;
        throw new Error("Not attached to an active page");
      }
      return this.history || { currentIndex: 0, entries: [{ id: 1, url: this.webContents.url }] };
    }
    if (method === "Page.getFrameTree") {
      return { frameTree: { frame: { id: "main", loaderId: this.currentLoaderID, url: this.webContents.url } } };
    }
    if (method === "Page.getLayoutMetrics") {
      const size = this.layoutSize || { width: 800, height: 600 };
      return {
        cssLayoutViewport: { clientWidth: 800, clientHeight: 600 },
        cssContentSize: size,
      };
    }
    if (method === "Runtime.evaluate") {
      if (params.expression.includes("document.title") && params.expression.includes('link[rel~="icon"]') && this.pageMetadata) {
        return { result: { value: JSON.stringify(this.pageMetadata) } };
      }
      if (this.evaluateValues?.length) {
        return { result: { value: this.evaluateValues.shift() } };
      }
      return { result: { value: '{"deviceScaleFactor":1}' } };
    }
    if (method === "Page.captureScreenshot") {
      return { data: this.screenshotData || "" };
    }
    return {};
  }
}

class FakeWebContents extends EventEmitter {
  constructor(id) {
    super();
    this.id = id;
    this.url = "about:blank";
    this.destroyed = false;
    this.reloadCount = 0;
    this.findRequestID = 0;
    this.findRequests = [];
    this.stopFindActions = [];
    this.zoomFactor = 1;
    this.debugger = new FakeDebugger(this);
  }

  isDestroyed() {
    return this.destroyed;
  }

  getURL() {
    return this.url;
  }

  getTitle() {
    return this.url;
  }

  reload() {
    this.reloadCount += 1;
    if (this.suppressReloadNavigationEvent) {
      return;
    }
    const loaderID = `loader-native-reload-${this.reloadCount}`;
    queueMicrotask(() => {
      this.debugger.currentLoaderID = loaderID;
      this.debugger.emit("message", {}, "Page.frameNavigated", {
        frame: { id: "main", loaderId: loaderID, url: this.url },
      });
    });
  }

  findInPage(text, options) {
    this.findRequestID += 1;
    this.findRequests.push({ text, options });
    return this.findRequestID;
  }

  stopFindInPage(action) {
    this.stopFindActions.push(action);
  }

  getZoomFactor() {
    return this.zoomFactor;
  }

  setZoomFactor(factor) {
    this.zoomFactor = factor;
  }

  print(options, callback) {
    this.printOptions = options;
    callback(this.printSuccess !== false, this.printFailureReason || "");
  }

  setWindowOpenHandler(handler) {
    this.windowOpenHandler = handler;
  }

  destroy() {
    this.destroyed = true;
    this.emit("destroyed");
  }
}

class FakeBrowserWindow extends EventEmitter {
  constructor(webContents) {
    super();
    this.webContents = webContents;
    this.destroyed = false;
  }

  isDestroyed() {
    return this.destroyed;
  }

  destroy() {
    this.destroyed = true;
    this.emit("closed");
  }
}

test("waits for a renderer webview and navigates it through persistent CDP", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-1", tabID: "tab-1", url: "https://example.com/" };
  const opening = host.ensure(request);

  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(required.length, 1);
  assert.match(required[0].requestID, /^webview_/);
  assert.equal(required[0].url, request.url);

  const webContents = new FakeWebContents(42);
  await host.registerWebContents({ ...required[0], webContentsID: 42 }, webContents);
  const opened = await opening;
  assert.equal(opened.url, request.url);
  assert.equal(opened.runtimeID, "webContents:42");
  assert.equal(webContents.debugger.attachCount, 1);
  assert.ok(webContents.debugger.commands.some(({ method }) => method === "Page.navigate"));

  await host.loadURL({ ...request, url: "https://example.org/" });
  assert.equal(webContents.debugger.attachCount, 1);
  assert.equal(webContents.debugger.isAttached(), true);
  assert.equal(webContents.url, "https://example.org/");

  host.closeAll();
});

test("managed browser cancels Web Bluetooth device selection", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const opening = host.ensure({ sessionID: "session-bluetooth", tabID: "tab-bluetooth" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(43);
  await host.registerWebContents(required[0], webContents);
  await opening;

  let prevented = false;
  let selectedDevice = "device-1";
  webContents.emit(
    "select-bluetooth-device",
    { preventDefault: () => { prevented = true; } },
    [{ deviceId: "device-1" }],
    (deviceID) => { selectedDevice = deviceID; },
  );
  assert.equal(prevented, true);
  assert.equal(selectedDevice, "");
  host.closeAll();
});

test("uses native webContents for page find, print, and tab zoom", async () => {
  const required = [];
  const found = [];
  const interactions = [];
  const host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => required.push(request),
    undefined,
    {
      foundInPage: (result) => found.push(result),
      interaction: (event) => interactions.push(event),
    },
  );
  const request = { sessionID: "session-page-controls", tabID: "tab-page-controls" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(44);
  await host.registerWebContents(required[0], webContents);
  await opening;

  assert.deepEqual(host.findInPage({ ...request, text: "pudding" }), { requestID: 1 });
  assert.deepEqual(webContents.findRequests, [{ text: "pudding", options: { forward: true, findNext: false, matchCase: false } }]);
  webContents.emit("found-in-page", {}, { requestId: 1, activeMatchOrdinal: 2, matches: 3, finalUpdate: true });
  assert.deepEqual(found, [{ ...request, requestID: 1, activeMatchOrdinal: 2, matches: 3, finalUpdate: true }]);
  assert.deepEqual(host.findInPage({ ...request, text: "browser" }), { requestID: 2 });
  webContents.emit("found-in-page", {}, { requestId: 1, activeMatchOrdinal: 3, matches: 3, finalUpdate: true });
  assert.equal(found.length, 1);
  assert.deepEqual(host.stopFindInPage(request), { ok: true });
  assert.equal(webContents.stopFindActions.at(-1), "clearSelection");

  assert.deepEqual(host.zoom({ ...request, action: "in" }), { factor: 1.1, percent: 110 });
  assert.deepEqual(host.zoom({ ...request, action: "out" }), { factor: 1, percent: 100 });
  assert.deepEqual(host.zoom({ ...request, action: "reset" }), { factor: 1, percent: 100 });
  assert.deepEqual(host.getZoom(request), { factor: 1, percent: 100 });

  assert.deepEqual(await host.print(request), { ok: true, canceled: false, reason: "" });
  assert.deepEqual(webContents.printOptions, { printBackground: true, usePrinterDefaultPageSize: true });
  webContents.printSuccess = false;
  webContents.printFailureReason = "Print job canceled";
  assert.deepEqual(await host.print(request), { ok: false, canceled: true, reason: "Print job canceled" });
  webContents.emit("before-mouse-event", {}, { type: "mouseDown" });
  webContents.emit("before-input-event", {}, { type: "keyDown", key: "Escape" });
  assert.deepEqual(interactions, [
    { ...request, kind: "pointer" },
    { ...request, kind: "keyboard", key: "Escape" },
  ]);
  webContents.destroyed = true;
  assert.deepEqual(host.stopFindInPage(request), { ok: true });
  assert.deepEqual(host.stopFindInPage({ sessionID: "missing", tabID: "missing" }), { ok: true });
  host.closeAll();
});

test("reads the current non-editable browser text selection", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-selection", tabID: "tab-selection" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(62);
  await host.registerWebContents(required[0], webContents);
  await opening;

  webContents.debugger.evaluateValues = [JSON.stringify({ selectionText: "selected browser text" })];
  assert.deepEqual(await host.readSelection(request), { selectionText: "selected browser text" });
  const evaluation = webContents.debugger.commands.filter(({ method }) => method === "Runtime.evaluate").at(-1);
  assert.match(evaluation.params.expression, /window\.getSelection/);
  assert.match(evaluation.params.expression, /contenteditable/);

  host.closeAll();
});

test("browser observations and click results redact password input values", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-password", tabID: "tab-password" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(63);
  await host.registerWebContents(required[0], webContents);
  await opening;

  webContents.debugger.evaluateValues = [JSON.stringify({
    title: "Login",
    url: "https://example.com/login",
    readyState: "complete",
    text: "",
    textChars: 0,
    truncated: false,
    elements: [],
  })];
  await host.observe(request);
  const observeEvaluation = webContents.debugger.commands.filter(({ method }) => method === "Runtime.evaluate").at(-1);
  assert.match(observeEvaluation.params.expression, /isPasswordInput/);
  assert.match(observeEvaluation.params.expression, /isPasswordInput\(el\) \? "" : el\.value/);

  webContents.debugger.evaluateValues = [JSON.stringify({
    ok: true,
    tag: "input",
    text: "Password",
    x: 20,
    y: 20,
    cursorX: 20,
    cursorY: 20,
    method: "pointer",
  })];
  await host.click({ ...request, selector: "#password" });
  const clickEvaluation = webContents.debugger.commands
    .filter(({ method, params }) => method === "Runtime.evaluate" && params.expression.includes("elementText"))
    .at(-1);
  assert.match(clickEvaluation.params.expression, /isPasswordInput\(node\) \? "" : node\.value/);

  host.closeAll();
});

test("reads browser text selected inside a child frame", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-frame-selection", tabID: "tab-frame-selection" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(66);
  webContents.mainFrame = {
    framesInSubtree: [
      {
        isDestroyed: () => false,
        executeJavaScript: async () => JSON.stringify({ selectionText: "" }),
      },
      {
        isDestroyed: () => false,
        executeJavaScript: async () => JSON.stringify({ selectionText: "selected child frame text" }),
      },
    ],
  };
  await host.registerWebContents(required[0], webContents);
  await opening;

  assert.deepEqual(await host.readSelection(request), {
    selectionText: "selected child frame text",
  });

  host.closeAll();
});

test("falls back to the webview selection cache after focus moves to the composer", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-selection-cache", tabID: "tab-selection-cache" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(63);
  await host.registerWebContents(required[0], webContents);
  await opening;

  assert.deepEqual(
    host.noteSelection(webContents, { selectionText: "cached browser text" }),
    {
      selected: true,
      selectionText: "cached browser text",
      sessionID: request.sessionID,
      tabID: request.tabID,
    },
  );
  const evaluationsBeforeCachedRead = webContents.debugger.commands.filter(
    ({ method }) => method === "Runtime.evaluate",
  ).length;
  assert.deepEqual(await host.readSelection(request), { selectionText: "cached browser text" });
  assert.equal(
    webContents.debugger.commands.filter(({ method }) => method === "Runtime.evaluate").length,
    evaluationsBeforeCachedRead + 1,
  );

  assert.deepEqual(
    host.noteSelection(webContents, { selectionText: "" }),
    {
      selected: false,
      selectionText: "",
      sessionID: request.sessionID,
      tabID: request.tabID,
    },
  );
  webContents.debugger.evaluateValues = [JSON.stringify({ selectionText: "" })];
  assert.deepEqual(await host.readSelection(request), { selectionText: "" });

  host.closeAll();
});

test("prefers the current live selection over an older cache in the same tab", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-live-selection", tabID: "tab-live-selection" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(67);
  await host.registerWebContents(required[0], webContents);
  await opening;

  host.noteSelection(webContents, { selectionText: "older cached selection" });
  webContents.debugger.evaluateValues = [JSON.stringify({ selectionText: "current live selection" })];
  assert.deepEqual(await host.readSelection(request), { selectionText: "current live selection" });
  webContents.debugger.evaluateValues = [JSON.stringify({ selectionText: "" })];
  assert.deepEqual(await host.readSelection(request), { selectionText: "current live selection" });

  host.closeAll();
});

test("does not leak a cached selection from another browser tab", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const sessionID = "session-selection-tabs";
  const firstRequest = { sessionID, tabID: "tab-first", url: "https://first.example/" };
  const firstOpening = host.ensure(firstRequest);
  await new Promise((resolve) => setImmediate(resolve));
  const firstContents = new FakeWebContents(64);
  await host.registerWebContents(required.shift(), firstContents);
  await firstOpening;

  const secondRequest = { sessionID, tabID: "tab-second", url: "https://second.example/" };
  const secondOpening = host.ensure(secondRequest);
  await new Promise((resolve) => setImmediate(resolve));
  const secondContents = new FakeWebContents(65);
  await host.registerWebContents(required.shift(), secondContents);
  await secondOpening;

  host.noteSelection(secondContents, { selectionText: "Google selection" });
  firstContents.debugger.evaluateValues = [JSON.stringify({ selectionText: "" })];
  assert.deepEqual(await host.readSelection(firstRequest), { selectionText: "" });
  assert.deepEqual(await host.readSelection(secondRequest), { selectionText: "Google selection" });

  host.closeAll();
});

test("tracks browser selection changes through the CDP binding", async () => {
  const required = [];
  const selectionChanges = [];
  const host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => required.push(request),
    undefined,
    { selectionChanged: (selection) => selectionChanges.push(selection) },
  );
  const request = { sessionID: "session-cdp-selection", tabID: "tab-cdp-selection" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(68);
  await host.registerWebContents(required[0], webContents);
  await opening;

  assert.ok(webContents.debugger.commands.some(
    ({ method, params }) => method === "Runtime.addBinding" && params.name === "__puddingBrowserSelectionChanged",
  ));
  assert.ok(webContents.debugger.commands.some(
    ({ method, params }) => method === "Page.addScriptToEvaluateOnNewDocument"
      && params.source.includes("selectionchange"),
  ));
  webContents.debugger.emit("message", {}, "Runtime.bindingCalled", {
    name: "__puddingBrowserSelectionChanged",
    payload: JSON.stringify({ selectionText: "selected through cdp" }),
  });
  assert.deepEqual(selectionChanges, [{
    selected: true,
    selectionText: "selected through cdp",
    sessionID: request.sessionID,
    tabID: request.tabID,
  }]);
  webContents.debugger.evaluateValues = [JSON.stringify({ selectionText: "" })];
  assert.deepEqual(await host.readSelection(request), { selectionText: "selected through cdp" });

  host.closeAll();
});

test("captures dynamically updated favicons and publishes the resolved local image", async () => {
  const required = [];
  const snapshots = [];
  const resolutions = [];
  const resolved = "data:image/png;base64,cG5n";
  const host = new BrowserHost(
    (snapshot) => snapshots.push(snapshot),
    undefined,
    undefined,
    (request) => required.push(request),
    undefined,
    {
      resolveFavicon: async (request) => {
        resolutions.push(request);
        return resolved;
      },
    },
  );
  const opening = host.ensure({ sessionID: "session-favicon", tabID: "tab-favicon", url: "https://discord.com/login" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(49);
  await host.registerWebContents(required[0], webContents);
  await opening;

  webContents.emit("page-favicon-updated", {}, ["javascript:alert(1)", "https://discord.com/assets/favicon.ico"]);
  assert.equal(snapshots.at(-1).faviconURL, "https://discord.com/assets/favicon.ico");
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(resolutions, [{
    url: "https://discord.com/assets/favicon.ico",
    pageURL: "https://discord.com/login",
  }]);
  assert.equal(snapshots.at(-1).faviconURL, resolved);
  host.closeAll();
});

test("keeps the current favicon during navigation until the page reports a replacement", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const request = { sessionID: "session-favicon-navigation", tabID: "tab-favicon-navigation", url: "https://www.google.com/" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(71);
  await host.registerWebContents(required[0], webContents);
  await opening;

  const faviconURL = "https://www.google.com/favicon.ico";
  webContents.emit("page-favicon-updated", {}, [faviconURL]);
  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-search", url: "https://www.google.com/search?q=weather" },
  });
  assert.equal(host.listTabs(request).tabs[0].faviconURL, faviconURL);

  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-example", url: "https://example.com/" },
  });
  assert.equal(host.listTabs(request).tabs[0].faviconURL, faviconURL);

  const nextFaviconURL = "https://example.com/favicon.ico";
  webContents.emit("page-favicon-updated", {}, [nextFaviconURL]);
  assert.equal(host.listTabs(request).tabs[0].faviconURL, nextFaviconURL);

  webContents.emit("page-favicon-updated", {}, []);
  assert.equal(host.listTabs(request).tabs[0].faviconURL, "");
  host.closeAll();
});

test("captures title and favicon when the page loaded before webview registration", async () => {
  const required = [];
  const snapshots = [];
  const resolved = "data:image/png;base64,cG5n";
  const host = new BrowserHost(
    (snapshot) => snapshots.push(snapshot),
    undefined,
    undefined,
    (request) => required.push(request),
    undefined,
    { resolveFavicon: async () => resolved },
  );
  const url = "https://buzzhive.example/admin/#dashboard";
  const faviconURL = "https://buzzhive.example/assets/favicon.png";
  const opening = host.ensure({ sessionID: "session-loaded-metadata", tabID: "tab-loaded-metadata", url });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(70);
  webContents.url = url;
  webContents.debugger.pageMetadata = { url, title: "BuzzHive Admin", faviconURL };

  const registered = await host.registerWebContents(required[0], webContents);
  await opening;

  assert.equal(registered.title, "BuzzHive Admin");
  assert.equal(registered.faviconURL, resolved);
  assert.equal(snapshots.at(-1).faviconURL, resolved);
  host.closeAll();
});

test("opens tab-like blank targets as managed browser tabs", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const opening = host.ensure({ sessionID: "session-window-tab", tabID: "tab-opener", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(44);
  await host.registerWebContents(required[0], webContents);
  await opening;
  webContents.url = "https://example.com/source";

  const result = webContents.windowOpenHandler({
    url: "https://example.com/next",
    frameName: "_blank",
    features: "",
    disposition: "foreground-tab",
    referrer: { url: "https://example.com/source", policy: "strict-origin-when-cross-origin" },
  });

  assert.deepEqual(result, { action: "deny" });
  const tabs = host.listTabs({ sessionID: "session-window-tab" }).tabs;
  assert.equal(tabs.length, 2);
  const nextTab = tabs.find((tab) => tab.tabID !== "tab-opener");
  assert.equal(nextTab.url, "https://example.com/next");
  assert.equal(nextTab.activate, true);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(required.length, 2);
  const nextContents = new FakeWebContents(47);
  await host.registerWebContents(required[1], nextContents);
  const navigation = nextContents.debugger.commands.find(({ method }) => method === "Page.navigate");
  assert.deepEqual(navigation.params, {
    url: "https://example.com/next",
    referrer: "https://example.com/source",
    referrerPolicy: "strictOriginWhenCrossOrigin",
  });
  host.closeAll();
});

test("keeps background link targets inactive", async () => {
  const required = [];
  const host = new BrowserHost(undefined, undefined, undefined, (request) => required.push(request));
  const opening = host.ensure({ sessionID: "session-background-tab", tabID: "tab-opener", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(48);
  await host.registerWebContents(required[0], webContents);
  await opening;

  assert.deepEqual(webContents.windowOpenHandler({
    url: "https://example.com/background",
    frameName: "_blank",
    features: "",
    disposition: "background-tab",
  }), { action: "deny" });
  const background = host.listTabs({ sessionID: "session-background-tab" }).tabs.find((tab) => tab.tabID !== "tab-opener");
  assert.equal(background.activate, false);
  host.closeAll();
});

test("allows real child windows when native WindowProxy semantics are required", async () => {
  const required = [];
  const created = [];
  const blocked = [];
  const host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => required.push(request),
    undefined,
    {
      options: () => ({ backgroundColor: "#123456", webPreferences: { javascript: true, nodeIntegration: true } }),
      created: (window, context) => created.push({ window, context }),
      blockedNavigation: (context) => blocked.push(context),
    },
  );
  const opening = host.ensure({ sessionID: "session-window-popup", tabID: "tab-opener", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const openerContents = new FakeWebContents(45);
  await host.registerWebContents(required[0], openerContents);
  await opening;
  openerContents.url = "https://example.com/source";

  const result = openerContents.windowOpenHandler({
    url: "https://example.com/popup",
    frameName: "oauth-popup",
    features: "width=480,height=640",
    disposition: "new-window",
  });
  assert.equal(result.action, "allow");
  assert.equal(result.outlivesOpener, false);
  assert.equal(result.overrideBrowserWindowOptions.backgroundColor, "#123456");
  assert.equal(result.overrideBrowserWindowOptions.webPreferences.partition, "persist:pudding-default");
  assert.equal(result.overrideBrowserWindowOptions.webPreferences.contextIsolation, true);
  assert.equal(result.overrideBrowserWindowOptions.webPreferences.nodeIntegration, false);
  assert.equal(result.overrideBrowserWindowOptions.webPreferences.sandbox, true);
  assert.equal(result.overrideBrowserWindowOptions.webPreferences.webviewTag, false);

  const noOpenerResult = openerContents.windowOpenHandler({
    url: "https://example.com/detached",
    frameName: "_blank",
    features: "noopener,noreferrer,width=480",
    disposition: "new-window",
  });
  assert.equal(noOpenerResult.action, "allow");
  assert.equal(noOpenerResult.outlivesOpener, true);
  assert.equal(openerContents.windowOpenHandler({
    url: "",
    frameName: "",
    features: "",
    disposition: "foreground-tab",
  }).action, "allow");
  assert.equal(openerContents.windowOpenHandler({
    url: "blob:https://example.com/7f3f05ba-25d6-4f6a-b35c-e53fb7ec87d1",
    frameName: "blob-popup",
    features: "width=480",
    disposition: "new-window",
  }).action, "allow");
  assert.deepEqual(openerContents.windowOpenHandler({
    url: "blob:https://evil.example/7f3f05ba-25d6-4f6a-b35c-e53fb7ec87d1",
    frameName: "blob-popup",
    features: "width=480",
    disposition: "new-window",
  }), { action: "deny" });

  const popupContents = new FakeWebContents(46);
  const popupWindow = new FakeBrowserWindow(popupContents);
  openerContents.emit("did-create-window", popupWindow, { url: "https://example.com/popup" });
  assert.equal(created.length, 1);
  assert.equal(created[0].window, popupWindow);
  assert.equal(created[0].context.sessionID, "session-window-popup");
  assert.equal(typeof popupContents.windowOpenHandler, "function");
  let blobPrevented = false;
  popupContents.emit(
    "will-navigate",
    { preventDefault: () => { blobPrevented = true; } },
    "blob:https://example.com/7f3f05ba-25d6-4f6a-b35c-e53fb7ec87d1",
  );
  assert.equal(blobPrevented, false);

  let prevented = false;
  popupContents.emit("will-navigate", { preventDefault: () => { prevented = true; } }, "javascript:alert(1)");
  assert.equal(prevented, true);
  assert.equal(blocked.length, 1);
  assert.equal(blocked[0].url, "javascript:alert(1)");
  host.closeAll();
  assert.equal(popupWindow.isDestroyed(), true);
});

test("rejects a stale renderer registration", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-2", tabID: "tab-2", url: "https://example.com/" });
  await new Promise((resolve) => setImmediate(resolve));

  await assert.rejects(
    host.registerWebContents({ ...required, requestID: "webview_stale" }, new FakeWebContents(43)),
    /stale browser webview registration/,
  );
  await assert.rejects(
    host.registerWebContents({ ...required, requestID: "" }, new FakeWebContents(46)),
    /stale browser webview registration/,
  );
  host.closeAll();
  await assert.rejects(opening, /browser tab closed/);
});

test("retries CDP setup when the same webview registers again", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-retry", tabID: "tab-retry", url: "https://example.com/" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(45);
  webContents.debugger.failPageEnableOnce = true;

  await assert.rejects(host.registerWebContents(required, webContents), /Page\.enable failed once/);
  await host.registerWebContents(required, webContents);
  const opened = await opening;

  assert.equal(opened.url, "https://example.com/");
  assert.equal(webContents.debugger.attachCount, 2);
  assert.equal(webContents.debugger.isAttached(), true);
  host.closeAll();
});

test("rejects oversized full-page screenshots before capture", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-3", tabID: "tab-3", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(44);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.layoutSize = { width: 20_000, height: 2_000 };

  await assert.rejects(
    host.screenshot({ sessionID: "session-3", tabID: "tab-3", fullPage: true }),
    /screenshot dimensions exceed limit/,
  );
  assert.equal(webContents.debugger.commands.some(({ method }) => method === "Page.captureScreenshot"), false);
  host.closeAll();
});

test("rejects navigation immediately when the webview reports a main-frame load failure", async () => {
  let required;
  let latestSnapshot;
  const host = new BrowserHost((snapshot) => {
    latestSnapshot = snapshot;
  }, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-fail", tabID: "tab-fail", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(47);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.suppressNavigationEvent = true;

  const navigation = host.loadURL({ sessionID: "session-fail", tabID: "tab-fail", url: "https://bad.example/" });
  await new Promise((resolve) => setImmediate(resolve));
  webContents.emit("did-fail-load", {}, -105, "ERR_NAME_NOT_RESOLVED", "https://bad.example/", true);
  await assert.rejects(navigation, /browser navigation failed: ERR_NAME_NOT_RESOLVED/);
  assert.equal(latestSnapshot.loadError.code, "ERR_NAME_NOT_RESOLVED");
  host.closeAll();
});

test("rejects a second live webview for the same session tab", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-live", tabID: "tab-live", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  await host.registerWebContents(required, new FakeWebContents(48));
  await opening;

  await assert.rejects(
    host.registerWebContents(required, new FakeWebContents(49)),
    /browser webview already registered/,
  );
  host.closeAll();
});

test("click type and scroll use only the expected CDP command sequences", async () => {
  let required;
  const host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => {
      required = request;
    },
  );
  const opening = host.ensure({ sessionID: "session-actions", tabID: "tab-actions", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(50);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.commands = [];
  webContents.debugger.evaluateValues = [
    '{"ok":true,"tag":"button","x":20,"y":30,"method":"pointer"}',
    '{"ok":true,"tag":"input","cursorX":20,"cursorY":30,"expectedValueLength":7,"expectedValueHash":"12345678"}',
    '{"ok":true,"tag":"input"}',
    '{"ok":true,"tag":"input","textLength":7,"valueLength":7,"matchesExpected":true,"cursorX":20,"cursorY":30,"method":"target"}',
    '{"ok":true,"x":400,"y":300,"cursorX":400,"cursorY":300}',
    '{"ok":true,"x":0,"y":600,"cursorX":400,"cursorY":300,"method":"target"}',
  ];

  await host.click({ sessionID: "session-actions", tabID: "tab-actions", selector: "#save" });
  await host.type({ sessionID: "session-actions", tabID: "tab-actions", selector: "#name", text: "Pudding", clear: true });
  await host.scroll({ sessionID: "session-actions", tabID: "tab-actions", deltaY: 600 });

  const methods = webContents.debugger.commands.map(({ method }) => method);
  assert.deepEqual(methods, [
    "Runtime.evaluate",
    "Input.dispatchMouseEvent",
    "Input.dispatchMouseEvent",
    "Input.dispatchMouseEvent",
    "Runtime.evaluate",
    "Runtime.evaluate",
    "Runtime.evaluate",
    "Runtime.evaluate",
    "Runtime.evaluate",
  ]);
  assert.equal(webContents.debugger.commands[0].params.userGesture, undefined);
  assert.doesNotMatch(webContents.debugger.commands[0].params.expression, /el\.click\(\)|focusTarget\.focus/);
  assert.match(webContents.debugger.commands[0].params.expression, /pudding\.browser\.lastClickTarget/);
  assert.deepEqual(webContents.debugger.commands.slice(1, 4).map(({ params }) => params.type), [
    "mouseMoved",
    "mousePressed",
    "mouseReleased",
  ]);
  assert.deepEqual(webContents.debugger.commands.slice(1, 4).map(({ params }) => params.button), ["none", "left", "left"]);
  assert.ok(webContents.debugger.commands.slice(1, 4).every(({ params }) => params.x === 20 && params.y === 30));
  assert.match(webContents.debugger.commands[4].params.expression, /replace\(\/\uFEFF\/g, ""\)/);
  assert.match(webContents.debugger.commands[4].params.expression, /lastClickTarget\?\.isConnected/);
  assert.match(webContents.debugger.commands[4].params.expression, /if \(!clear\) range\.collapse\(false\)/);
  assert.match(webContents.debugger.commands[5].params.expression, /inputEvent\("beforeinput", true\)/);
  assert.match(webContents.debugger.commands[5].params.expression, /setter\.call\(el, nextValue\)/);
  assert.match(webContents.debugger.commands[5].params.expression, /if \(inserted && !sawInput\) dispatchInput\(\)/);
  assert.match(webContents.debugger.commands[6].params.expression, /replace\(\/\uFEFF\/g, ""\)/);
  assert.match(webContents.debugger.commands[7].params.expression, /target\.scrollBy/);
  assert.match(webContents.debugger.commands[0].params.expression, /elementFromPoint/);
  assert.match(webContents.debugger.commands[0].params.expression, /not hittable/);
  host.closeAll();
});

test("rejects controlled input when the resulting value does not match", async () => {
  let required;
  const completed = [];
  const host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => {
      required = request;
    },
    (event) => completed.push(event),
  );
  const opening = host.ensure({ sessionID: "session-controlled", tabID: "tab-controlled", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(51);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.evaluateValues = [
    '{"ok":true,"tag":"input","expectedValueLength":1,"expectedValueHash":"12345678"}',
    '{"ok":true,"tag":"input"}',
    '{"ok":true,"tag":"input","valueLength":1,"matchesExpected":false,"method":"target"}',
  ];

  await assert.rejects(
    host.type({ sessionID: "session-controlled", tabID: "tab-controlled", selector: "#name", text: "P" }),
    /did not produce the expected value/,
  );
  assert.equal(completed.length, 1);
  assert.equal(completed[0].action, "type");
  host.closeAll();
});

test("dispatches a real pointer click and target-scoped input", async () => {
  let required;
  const host = new BrowserHost(
    undefined,
    undefined,
    undefined,
    (request) => {
      required = request;
    },
  );
  const opening = host.ensure({ sessionID: "session-unfocused", tabID: "tab-unfocused", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(54);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.commands = [];
  webContents.debugger.evaluateValues = [
    '{"ok":true,"tag":"button","x":20,"y":30,"method":"pointer"}',
    '{"ok":true,"tag":"input","expectedValueLength":1,"expectedValueHash":"12345678"}',
    '{"ok":true,"tag":"input"}',
    '{"ok":true,"tag":"input","valueLength":1,"matchesExpected":true,"method":"target"}',
  ];

  await host.click({ sessionID: "session-unfocused", tabID: "tab-unfocused", selector: "#save" });
  await host.type({ sessionID: "session-unfocused", tabID: "tab-unfocused", selector: "#name", text: "P" });
  assert.deepEqual(webContents.debugger.commands.map(({ method }) => method), [
    "Runtime.evaluate",
    "Input.dispatchMouseEvent",
    "Input.dispatchMouseEvent",
    "Input.dispatchMouseEvent",
    "Runtime.evaluate",
    "Runtime.evaluate",
    "Runtime.evaluate",
  ]);
  assert.equal(webContents.debugger.commands[0].params.userGesture, undefined);
  assert.deepEqual(webContents.debugger.commands.slice(1, 4).map(({ params }) => params.type), [
    "mouseMoved",
    "mousePressed",
    "mouseReleased",
  ]);
  assert.equal(webContents.debugger.commands.slice(4).some(({ params }) => params.userGesture), false);
  host.closeAll();
});

test("does not dispatch a click when renderer focus preparation fails", async () => {
  let required;
  const completed = [];
  const host = new BrowserHost(
    undefined,
    undefined,
    () => false,
    (request) => {
      required = request;
    },
    (event) => completed.push(event),
  );
  const opening = host.ensure({ sessionID: "session-focus-failed", tabID: "tab-focus-failed", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(55);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.commands = [];

  await assert.rejects(
    host.click({ sessionID: "session-focus-failed", tabID: "tab-focus-failed", selector: "#save" }),
    /browser_webview_not_ready: click focus preparation failed/,
  );
  assert.deepEqual(webContents.debugger.commands, []);
  assert.equal(completed.length, 1);
  assert.equal(completed[0].action, "click");
  host.closeAll();
});

test("reload ignores stale main-frame events until a new loader commits", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-reload", tabID: "tab-reload", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(52);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.suppressReloadNavigationEvent = true;

  let settled = false;
  const reload = host.reload({ sessionID: "session-reload", tabID: "tab-reload" }).finally(() => {
    settled = true;
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(webContents.reloadCount, 1);
  assert.equal(webContents.debugger.commands.some(({ method }) => method === "Page.reload"), false);
  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-current", url: "about:blank" },
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(settled, false);

  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-reloaded", url: "about:blank" },
  });
  await reload;
  assert.equal(settled, true);
  host.closeAll();
});

test("waits for readable history after a document swap", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const request = { sessionID: "session-history-swap", tabID: "tab-history-swap", url: "about:blank" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(60);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.commands = [];
  webContents.debugger.navigationHistoryFailures = 1;

  const snapshot = await host.loadURL({ ...request, url: "https://example.com/after-swap" });
  assert.equal(snapshot.url, "https://example.com/after-swap");
  assert.equal(
    webContents.debugger.commands.filter(({ method }) => method === "Page.getNavigationHistory").length,
    2,
  );
  host.closeAll();
});

test("refreshes back and forward availability after page-driven navigation", async () => {
  let required;
  const updates = [];
  const host = new BrowserHost(
    (snapshot) => updates.push(snapshot),
    undefined,
    undefined,
    (request) => {
      required = request;
    },
  );
  const opening = host.ensure({ sessionID: "session-history", tabID: "tab-history", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(56);
  await host.registerWebContents(required, webContents);
  await opening;

  webContents.debugger.history = {
    currentIndex: 1,
    entries: [
      { id: 1, url: "https://example.com/first" },
      { id: 2, url: "https://example.com/second" },
    ],
  };
  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-second", url: "https://example.com/second" },
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(updates.at(-1).canGoBack, true);
  assert.equal(updates.at(-1).canGoForward, false);

  webContents.debugger.history = {
    currentIndex: 0,
    entries: [
      { id: 1, url: "https://example.com/first" },
      { id: 2, url: "https://example.com/second" },
    ],
  };
  webContents.debugger.emit("message", {}, "Page.navigatedWithinDocument", {
    frameId: "main",
    url: "https://example.com/first",
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(updates.at(-1).canGoBack, false);
  assert.equal(updates.at(-1).canGoForward, true);
  host.closeAll();
});

test("history navigation uses the target entry instead of frame URL events", async () => {
  let required;
  const updates = [];
  const host = new BrowserHost(
    (snapshot) => updates.push(snapshot),
    undefined,
    undefined,
    (request) => {
      required = request;
    },
  );
  const request = { sessionID: "session-hash-history", tabID: "tab-hash-history", url: "about:blank" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(57);
  await host.registerWebContents(required, webContents);
  await opening;

  const targetURL = "https://example.com/admin/#dashboard";
  webContents.debugger.history = {
    currentIndex: 0,
    entries: [
      { id: 1, url: "about:blank" },
      { id: 2, url: targetURL },
    ],
  };
  webContents.debugger.suppressNavigationEvent = true;
  const sendCommand = webContents.debugger.sendCommand.bind(webContents.debugger);
  webContents.debugger.sendCommand = async (method, params = {}) => {
    if (method === "Page.navigateToHistoryEntry") {
      const nextIndex = webContents.debugger.history.entries.findIndex((entry) => entry.id === params.entryId);
      webContents.debugger.history.currentIndex = nextIndex;
      webContents.url = webContents.debugger.history.entries[nextIndex].url;
    }
    return sendCommand(method, params);
  };

  const forwarded = await host.forward(request);
  assert.equal(forwarded.url, targetURL);
  assert.equal(forwarded.canGoBack, true);
  assert.equal(updates.some((snapshot) => snapshot.url === targetURL), true);

  const backed = await host.back(request);
  assert.equal(backed.url, "about:blank");
  assert.equal(backed.canGoBack, false);
  assert.equal(backed.canGoForward, true);
  host.closeAll();
});

test("page metadata events do not enqueue CDP work ahead of navigation", async () => {
  const required = [];
  const updates = [];
  const host = new BrowserHost(
    (snapshot) => updates.push(snapshot),
    undefined,
    undefined,
    (request) => required.push(request),
  );
  const opening = host.ensure({ sessionID: "session-metadata", tabID: "tab-metadata", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(59);
  await host.registerWebContents(required[0], webContents);
  await opening;
  webContents.debugger.commands = [];

  webContents.emit("page-title-updated", {}, "Dashboard");
  webContents.debugger.emit("message", {}, "Page.loadEventFired", {});
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(updates.at(-1).title, "Dashboard");
  assert.equal(webContents.debugger.commands.some(({ method }) => method === "Runtime.evaluate"), false);
  host.closeAll();
});

test("credential state lookup tolerates a tab close race", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const request = { sessionID: "session-credential-close", tabID: "tab-credential-close", url: "about:blank" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(58);
  await host.registerWebContents(required, webContents);
  await opening;
  assert.equal(host.credentialContextIfLive(request)?.webContentsID, 58);

  host.closeTab(request);
  assert.equal(host.credentialContextIfLive(request), null);
  assert.throws(() => host.credentialContext(request), /browser tab not found/);
  host.closeAll();
});

test("credential fill consumes one native user gesture and rejects automated input", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const request = { sessionID: "session-credential-gesture", tabID: "tab-credential-gesture", url: "about:blank" };
  const opening = host.ensure(request);
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(69);
  await host.registerWebContents(required, webContents);
  await opening;

  webContents.emit("before-mouse-event", {}, { type: "mouseDown" });
  assert.equal(host.consumeCredentialUserGesture(webContents), true);
  assert.equal(host.consumeCredentialUserGesture(webContents), false);

  webContents.emit("before-input-event", {}, { type: "keyDown" });
  host.discardCredentialUserGesture(webContents);
  assert.equal(host.consumeCredentialUserGesture(webContents), false);

  const slot = host.getSlot(request);
  host.noteAutomationStart(slot, "click");
  webContents.emit("before-mouse-event", {}, { type: "mouseDown" });
  assert.equal(host.consumeCredentialUserGesture(webContents), false);
  host.noteAutomationEnd(slot, "click");

  webContents.emit("before-input-event", {}, { type: "keyDown" });
  assert.equal(host.consumeCredentialUserGesture(webContents), true);
  host.closeAll();
});

test("allows trusted project files and blocks file navigation outside the grant", async (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-browser-file-"));
  const outsideRoot = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-browser-outside-"));
  t.after(() => {
    fs.rmSync(root, { recursive: true, force: true });
    fs.rmSync(outsideRoot, { recursive: true, force: true });
  });
  const inside = path.join(root, "inside.html");
  const outside = path.join(outsideRoot, "outside.html");
  fs.writeFileSync(inside, "<h1>inside</h1>");
  fs.writeFileSync(outside, "<h1>outside</h1>");
  const insideURL = pathToFileURL(fs.realpathSync(inside)).toString();
  const outsideURL = pathToFileURL(fs.realpathSync(outside)).toString();

  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({
    sessionID: "session-file",
    tabID: "tab-file",
    url: insideURL,
    fileRoot: root,
    _fileAuthorized: true,
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(required.url, insideURL);
  const webContents = new FakeWebContents(53);
  await host.registerWebContents(required, webContents);
  assert.equal((await opening).url, insideURL);

  await assert.rejects(
    host.loadURL({ sessionID: "session-file", tabID: "tab-file", url: outsideURL }),
    /outside the session project/,
  );
  let prevented = false;
  webContents.emit("will-navigate", { preventDefault: () => { prevented = true; } }, outsideURL);
  assert.equal(prevented, true);
  host.closeAll();
});

test("does not accept file grants from an untrusted renderer request", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-browser-untrusted-"));
  const file = path.join(root, "inside.html");
  fs.writeFileSync(file, "<h1>inside</h1>");
  const host = new BrowserHost();
  const request = { sessionID: "session-untrusted", tabID: "tab-untrusted", url: pathToFileURL(file).toString(), fileRoot: root };
  await assert.rejects(host.loadURL(request), /outside the session project/);
  host.closeAll();
  fs.rmSync(root, { recursive: true, force: true });
});

test("revokes project file grants while preserving ordinary web tabs", async (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-browser-revoke-"));
  const file = path.join(root, "inside.html");
  fs.writeFileSync(file, "<h1>inside</h1>");
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const fileURL = pathToFileURL(fs.realpathSync(file)).toString();
  const required = new Map();
  const updates = [];
  const host = new BrowserHost((snapshot) => updates.push(snapshot), undefined, undefined, (request) => {
    required.set(request.tabID, request);
  });

  const fileOpening = host.ensure({ sessionID: "session-revoke", tabID: "tab-file", url: fileURL, fileRoot: root, _fileAuthorized: true });
  const webOpening = host.ensure({ sessionID: "session-revoke", tabID: "tab-web", url: "https://example.com/", fileRoot: root, _fileAuthorized: true });
  await new Promise((resolve) => setImmediate(resolve));
  const fileContents = new FakeWebContents(60);
  const webContents = new FakeWebContents(61);
  await host.registerWebContents(required.get("tab-file"), fileContents);
  await host.registerWebContents(required.get("tab-web"), webContents);
  await Promise.all([fileOpening, webOpening]);

  const result = await host.revokeFileAccess({ sessionID: "session-revoke" });
  assert.deepEqual(result, { closedTabIDs: ["tab-file"] });
  assert.equal(fileContents.isDestroyed(), true);
  assert.equal(webContents.isDestroyed(), false);
  assert.equal(host.listTabs({ sessionID: "session-revoke" }).tabs.length, 1);
  assert.equal(updates.at(-1).status, "lost");
  await assert.rejects(
    host.loadURL({ sessionID: "session-revoke", tabID: "tab-web", url: fileURL }),
    /outside the session project/,
  );
  host.closeAll();
});

test("caps persistent webviews per session and globally", () => {
  const host = new BrowserHost();
  for (let index = 0; index < 8; index += 1) {
    host.ensureSlot({ sessionID: "session-limit", tabID: `tab-${index}`, url: "about:blank" });
  }
  assert.throws(
    () => host.ensureSlot({ sessionID: "session-limit", tabID: "tab-over", url: "about:blank" }),
    /browser tab limit reached/,
  );
  for (let index = 0; index < 8; index += 1) {
    host.ensureSlot({ sessionID: "session-limit-2", tabID: `tab-second-${index}`, url: "about:blank" });
  }
  assert.throws(
    () => host.ensureSlot({ sessionID: "session-limit-3", tabID: "tab-global-over", url: "about:blank" }),
    /browser tab limit reached/,
  );
  host.closeAll();
});
