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

  setWindowOpenHandler(handler) {
    this.windowOpenHandler = handler;
  }

  destroy() {
    this.destroyed = true;
    this.emit("destroyed");
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
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
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
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-actions", tabID: "tab-actions", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(50);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.commands = [];
  webContents.debugger.evaluateValues = [
    '{"ok":true,"tag":"button","x":20,"y":30,"method":"pointer"}',
    '{"ok":true,"tag":"input","cursorX":20,"cursorY":30,"expectedValueLength":7,"expectedValueHash":"12345678"}',
    '{"ok":true,"tag":"input","textLength":7,"valueLength":7,"matchesExpected":true,"cursorX":20,"cursorY":30,"method":"keyboard"}',
    '{"ok":true,"x":400,"y":300,"cursorX":400,"cursorY":300}',
    '{"ok":true,"x":0,"y":600,"cursorX":400,"cursorY":300,"method":"wheel"}',
  ];

  await host.click({ sessionID: "session-actions", tabID: "tab-actions", selector: "#save" });
  await host.type({ sessionID: "session-actions", tabID: "tab-actions", selector: "#name", text: "Pudding", clear: true });
  await host.scroll({ sessionID: "session-actions", tabID: "tab-actions", deltaY: 600 });

  const methods = webContents.debugger.commands.map(({ method }) => method);
  assert.equal(methods.length, 34);
  assert.deepEqual(methods.slice(0, 5), [
    "Runtime.evaluate",
    "Input.dispatchMouseEvent",
    "Input.dispatchMouseEvent",
    "Input.dispatchMouseEvent",
    "Runtime.evaluate",
  ]);
  assert.equal(methods.slice(5, 30).every((method) => method === "Input.dispatchKeyEvent"), true);
  assert.deepEqual(methods.slice(30), [
    "Runtime.evaluate",
    "Runtime.evaluate",
    "Input.dispatchMouseEvent",
    "Runtime.evaluate",
  ]);
  assert.equal(webContents.debugger.commands[2].params.type, "mousePressed");
  assert.equal(webContents.debugger.commands[3].params.type, "mouseReleased");
  assert.equal(webContents.debugger.commands[5].params.commands[0], "selectAll");
  assert.equal(webContents.debugger.commands[10].params.text, "P");
  assert.equal(webContents.debugger.commands[32].params.type, "mouseWheel");
  assert.match(webContents.debugger.commands[0].params.expression, /elementFromPoint/);
  assert.match(webContents.debugger.commands[0].params.expression, /not hittable/);
  host.closeAll();
});

test("rejects keyboard input when a controlled value does not match the expected result", async () => {
  let required;
  const host = new BrowserHost(undefined, undefined, undefined, (request) => {
    required = request;
  });
  const opening = host.ensure({ sessionID: "session-controlled", tabID: "tab-controlled", url: "about:blank" });
  await new Promise((resolve) => setImmediate(resolve));
  const webContents = new FakeWebContents(51);
  await host.registerWebContents(required, webContents);
  await opening;
  webContents.debugger.evaluateValues = [
    '{"ok":true,"tag":"input","expectedValueLength":1,"expectedValueHash":"12345678"}',
    '{"ok":true,"tag":"input","valueLength":1,"matchesExpected":false,"method":"keyboard"}',
  ];

  await assert.rejects(
    host.type({ sessionID: "session-controlled", tabID: "tab-controlled", selector: "#name", text: "P" }),
    /did not produce the expected value/,
  );
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
  webContents.debugger.deferNavigationCommand = true;
  webContents.debugger.suppressNavigationEvent = true;

  let settled = false;
  const reload = host.reload({ sessionID: "session-reload", tabID: "tab-reload" }).finally(() => {
    settled = true;
  });
  await new Promise((resolve) => setImmediate(resolve));
  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-current", url: "about:blank" },
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(settled, false);

  webContents.debugger.resolveNavigationCommand();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(settled, false);
  webContents.debugger.emit("message", {}, "Page.frameNavigated", {
    frame: { id: "main", loaderId: "loader-reloaded", url: "about:blank" },
  });
  await reload;
  assert.equal(settled, true);
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
