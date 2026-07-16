const assert = require("node:assert/strict");
const test = require("node:test");

const { BrowserBridgeServer, classifyError } = require("../browser-bridge-server.cjs");

test("concurrent bridge starts share one listener and complete identity", async (t) => {
  const server = new BrowserBridgeServer({});
  t.after(() => server.stop());

  const first = server.start();
  const second = server.start();
  assert.equal(first, second);
  const [left, right] = await Promise.all([first, second]);
  assert.deepEqual(left, right);
  assert.match(left.url, /^http:\/\/127\.0\.0\.1:\d+$/);
  assert.equal(left.token.length, 48);
});

test("a bridge start requested during stop waits for the old listener", async (t) => {
  const server = new BrowserBridgeServer({});
  t.after(() => server.stop());

  const first = await server.start();
  const stopping = server.stop();
  const restarted = server.start();
  await stopping;
  const second = await restarted;

  assert.notEqual(second.url, first.url);
  assert.notEqual(second.token, first.token);
  assert.equal(server.url, second.url);
  assert.equal(server.token, second.token);
});

test("classifies page element failures separately from missing tabs", () => {
  assert.deepEqual(classifyError(new Error("target element not found")), {
    status: 422,
    code: "element_not_found",
    retryable: false,
    message: "target element not found",
  });
  assert.deepEqual(classifyError(new Error("browser tab not found")), {
    status: 404,
    code: "browser_tab_not_found",
    retryable: false,
    message: "browser tab not found",
  });
});

test("classifies transient CDP and webview failures as retryable", () => {
  assert.equal(classifyError(new Error("browser_webview_not_ready")).retryable, true);
  assert.equal(classifyError(new Error("cdp detached during browser navigation")).code, "cdp_detached");
  assert.equal(classifyError(new Error("browser navigation timed out")).code, "navigation_timeout");
});

test("classifies persistent webview limits", () => {
  assert.deepEqual(classifyError(new Error("browser tab limit reached")), {
    status: 429,
    code: "browser_tab_limit_reached",
    retryable: false,
    message: "browser tab limit reached",
  });
});

test("classifies rejected file URLs and only trusts bridge navigation requests", async () => {
  assert.deepEqual(classifyError(new Error("file URL is outside the session project")), {
    status: 403,
    code: "file_url_not_allowed",
    retryable: false,
    message: "file URL is outside the session project",
  });
  let received;
  const server = new BrowserBridgeServer({
    loadURL(request) {
      received = request;
      return { ok: true };
    },
  });
  await server.route("/browser/tabs/open", { fileRoot: "/project" });
  assert.equal(received._fileAuthorized, true);
  assert.equal(received.fileRoot, "/project");
});

test("routes project file grant revocation", async () => {
  let received;
  const server = new BrowserBridgeServer({
    revokeFileAccess(request) {
      received = request;
      return { closedTabIDs: ["tab-file"] };
    },
  });
  const result = await server.route("/browser/session/revoke-file-access", { sessionID: "session-1" });
  assert.deepEqual(received, { sessionID: "session-1" });
  assert.deepEqual(result, { closedTabIDs: ["tab-file"] });
});
