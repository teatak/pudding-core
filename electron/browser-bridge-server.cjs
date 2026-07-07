const crypto = require("node:crypto");
const http = require("node:http");

class BrowserBridgeServer {
  constructor(browserHost) {
    this.browserHost = browserHost;
    this.server = null;
    this.token = "";
    this.url = "";
  }

  async start() {
    if (this.server) {
      return { url: this.url, token: this.token };
    }
    this.token = crypto.randomBytes(24).toString("hex");
    this.server = http.createServer((request, response) => {
      void this.handle(request, response);
    });
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
  }

  async stop() {
    if (!this.server) {
      return;
    }
    const server = this.server;
    this.server = null;
    await new Promise((resolve) => server.close(resolve));
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
      writeJSON(response, statusForError(error), { error: error?.message || "browser_bridge_error" });
    }
  }

  route(path, body) {
    switch (path) {
      case "/browser/tabs/ensure":
        return this.browserHost.ensure(body);
      case "/browser/tabs/open":
        return this.browserHost.loadURL(body);
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

function statusForError(error) {
  const message = String(error?.message || "");
  if (message.includes("not found")) {
    return 404;
  }
  if (message.includes("missing") || message.includes("required") || message.includes("invalid")) {
    return 400;
  }
  if (message.includes("unavailable") || message.includes("destroyed")) {
    return 503;
  }
  return 500;
}

module.exports = { BrowserBridgeServer };
