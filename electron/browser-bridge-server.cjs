const crypto = require("node:crypto");
const http = require("node:http");

class BrowserBridgeServer {
  constructor(browserHost) {
    this.browserHost = browserHost;
    this.server = null;
    this.startPromise = null;
    this.stopPromise = null;
    this.token = "";
    this.url = "";
  }

  start() {
    if (this.stopPromise) {
      return this.stopPromise.then(() => this.start());
    }
    if (this.server && this.url) {
      return Promise.resolve({ url: this.url, token: this.token });
    }
    if (this.startPromise) {
      return this.startPromise;
    }
    const attempt = this.startServer();
    this.startPromise = attempt;
    const clearAttempt = () => {
      if (this.startPromise === attempt) {
        this.startPromise = null;
      }
    };
    void attempt.then(clearAttempt, clearAttempt);
    return attempt;
  }

  async startServer() {
    this.token = crypto.randomBytes(24).toString("hex");
    this.server = http.createServer((request, response) => {
      void this.handle(request, response);
    });
    try {
      await new Promise((resolve, reject) => {
        this.server.once("error", reject);
        this.server.listen(0, "127.0.0.1", () => {
          this.server.off("error", reject);
          resolve();
        });
      });
      const address = this.server.address();
      if (!address || typeof address === "string") {
        throw new Error("browser bridge failed to bind loopback");
      }
      this.url = `http://127.0.0.1:${address.port}`;
      return { url: this.url, token: this.token };
    } catch (error) {
      this.server?.close();
      this.server = null;
      this.token = "";
      this.url = "";
      throw error;
    }
  }

  stop() {
    if (this.stopPromise) {
      return this.stopPromise;
    }
    const attempt = this.stopServer();
    this.stopPromise = attempt;
    const clearAttempt = () => {
      if (this.stopPromise === attempt) {
        this.stopPromise = null;
      }
    };
    void attempt.then(clearAttempt, clearAttempt);
    return attempt;
  }

  async stopServer() {
    this.browserHost.closeAll?.();
    if (this.startPromise) {
      try {
        await this.startPromise;
      } catch {
        return;
      }
    }
    if (!this.server) {
      return;
    }
    const server = this.server;
    this.server = null;
    await new Promise((resolve) => {
      let settled = false;
      const finish = () => {
        if (settled) {
          return;
        }
        settled = true;
        clearTimeout(timer);
        resolve();
      };
      const timer = setTimeout(() => {
        server.closeAllConnections?.();
        finish();
      }, 2_000);
      server.close(finish);
    });
    this.token = "";
    this.url = "";
  }

  async handle(request, response) {
    try {
      if (request.method !== "POST") {
        writeJSON(response, 405, { error: "method_not_allowed" });
        return;
      }
      if (request.headers.authorization !== `Bearer ${this.token}`) {
        writeJSON(response, 401, { error: "unauthorized" });
        return;
      }
      const body = await readJSON(request);
      const result = await this.route(request.url || "", body || {});
      writeJSON(response, 200, result || { ok: true });
    } catch (error) {
      const failure = classifyError(error);
      writeJSON(response, failure.status, {
        error: failure.message,
        code: failure.code,
        retryable: failure.retryable,
      });
    }
  }

  route(path, body) {
    switch (path) {
      case "/browser/tabs/ensure":
        return this.browserHost.ensure(trustedBrowserRequest(body));
      case "/browser/tabs/open":
        return this.browserHost.loadURL(trustedBrowserRequest(body));
      case "/browser/tabs/list":
        return this.browserHost.listTabs(body);
      case "/browser/tabs/back":
        return this.browserHost.back(body);
      case "/browser/tabs/forward":
        return this.browserHost.forward(body);
      case "/browser/tabs/reload":
        return this.browserHost.reload(body);
      case "/browser/tabs/observe":
        return this.browserHost.observe(body);
      case "/browser/tabs/screenshot":
        return this.browserHost.screenshot(body);
      case "/browser/tabs/click":
        return this.browserHost.click(body);
      case "/browser/tabs/type":
        return this.browserHost.type(body);
      case "/browser/tabs/scroll":
        return this.browserHost.scroll(body);
      case "/browser/tabs/close":
        return this.browserHost.closeTab(body);
      case "/browser/session/close":
      case "/browser/session/release":
        this.browserHost.closeSession(body);
        return { ok: true };
      case "/browser/session/revoke-file-access":
        return this.browserHost.revokeFileAccess(body);
      default:
        throw new Error("browser bridge route not found");
    }
  }
}

function readJSON(request) {
  return new Promise((resolve, reject) => {
    let raw = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => {
      raw += chunk;
      if (raw.length > 1024 * 1024) {
        reject(new Error("request body too large"));
        request.destroy();
      }
    });
    request.on("end", () => {
      if (!raw.trim()) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(raw));
      } catch {
        reject(new Error("invalid json body"));
      }
    });
    request.on("error", reject);
  });
}

function writeJSON(response, status, payload) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(payload));
}

function classifyError(error) {
  const message = String(error?.message || "");
  if (message.includes("file URL is outside the session project")) {
    return { status: 403, code: "file_url_not_allowed", retryable: false, message };
  }
  if (message === "browser tab not found") {
    return { status: 404, code: "browser_tab_not_found", retryable: false, message };
  }
  if (message === "browser tab limit reached") {
    return { status: 429, code: "browser_tab_limit_reached", retryable: false, message };
  }
  if (message === "browser bridge route not found") {
    return { status: 404, code: "browser_bridge_route_not_found", retryable: false, message };
  }
  if (
    message.includes("target element not found") ||
    message.includes("target input not found") ||
    message.includes("scroll target not found")
  ) {
    return { status: 422, code: "element_not_found", retryable: false, message };
  }
  if (
    message.includes("not visible") ||
    message.includes("not hittable") ||
    message.includes("not interactable") ||
    message.includes("outside the viewport")
  ) {
    return { status: 422, code: "element_not_interactable", retryable: false, message };
  }
  if (
    message.includes("not editable") ||
    message.includes("could not be focused") ||
    message.includes("did not produce the expected value")
  ) {
    return { status: 422, code: "element_not_editable", retryable: false, message };
  }
  if (message.includes("missing") || message.includes("required") || message.includes("invalid")) {
    return { status: 400, code: "invalid_request", retryable: false, message };
  }
  if (message.includes("unavailable") || message.includes("destroyed") || message.includes("not_ready")) {
    return { status: 503, code: "browser_webview_not_ready", retryable: true, message };
  }
  if (message.includes("cdp detached")) {
    return { status: 503, code: "cdp_detached", retryable: true, message };
  }
  if (message.includes("navigation timed out")) {
    return { status: 504, code: "navigation_timeout", retryable: true, message };
  }
  if (message.includes("navigation failed") || message.includes("navigation did not commit")) {
    return { status: 502, code: "navigation_failed", retryable: false, message };
  }
  if (message.includes("timed out")) {
    return { status: 504, code: "cdp_command_timeout", retryable: true, message };
  }
  if (message.includes("screenshot")) {
    return { status: 500, code: "screenshot_failed", retryable: false, message };
  }
  return { status: 500, code: "cdp_command_failed", retryable: false, message: message || "browser_bridge_error" };
}

function trustedBrowserRequest(body) {
  return { ...(body || {}), _fileAuthorized: true };
}

module.exports = { BrowserBridgeServer, classifyError };
