const http = require("node:http");
const os = require("node:os");

const API_PREFIXES = [
  "/sessions",
  "/projects",
  "/settings",
  "/providers",
  "/tools",
  "/skills",
  "/skill-assets",
  "/usage",
  "/mobile",
  "/apps",
  "/app-assets",
  "/app-skills",
  "/app-connections",
  "/app-oauth",
  "/oauth",
  "/mcp",
  "/desktop",
];

class MobileAccessBridge {
  constructor({ apiBase, webBase = apiBase, networkInterfaces = os.networkInterfaces } = {}) {
    this.apiBase = normalizeHTTPBase(apiBase);
    this.webBase = normalizeHTTPBase(webBase);
    this.networkInterfaces = networkInterfaces;
    this.server = null;
    this.startPromise = null;
    this.stopPromise = null;
    this.publicURL = "";
    this.loopbackURL = "";
  }

  start() {
    if (this.stopPromise) {
      return this.stopPromise.then(() => this.start());
    }
    if (this.server && this.publicURL) {
      return Promise.resolve({ url: this.publicURL });
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
    if (!this.apiBase || !this.webBase) {
      throw new Error("mobile access bridge requires HTTP origins");
    }
    const host = preferredLANHost(this.networkInterfaces());
    if (!host) {
      throw new Error("no local network interface is available");
    }
    const server = http.createServer((request, response) => this.proxyHTTP(request, response));
    server.on("upgrade", (request, socket, head) => this.proxyUpgrade(request, socket, head));
    this.server = server;
    try {
      await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(0, "0.0.0.0", () => {
          server.off("error", reject);
          resolve();
        });
      });
      const address = server.address();
      if (!address || typeof address === "string") {
        throw new Error("mobile access bridge failed to bind");
      }
      this.publicURL = `http://${host}:${address.port}`;
      this.loopbackURL = `http://127.0.0.1:${address.port}`;
      return { url: this.publicURL };
    } catch (error) {
      server.close();
      this.server = null;
      this.publicURL = "";
      this.loopbackURL = "";
      throw error;
    }
  }

  async createPairing(token) {
    await this.start();
    const response = await requestJSON(new URL("/mobile/pairings", this.loopbackURL), {
      method: "POST",
      headers: {
        Authorization: `Bearer ${String(token || "").trim()}`,
        Host: new URL(this.publicURL).host,
      },
    });
    if (response.statusCode !== 201) {
      throw new Error(response.body?.error || `mobile pairing failed (${response.statusCode})`);
    }
    return response.body;
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
    if (this.startPromise) {
      try {
        await this.startPromise;
      } catch {
        return;
      }
    }
    const server = this.server;
    this.server = null;
    this.publicURL = "";
    this.loopbackURL = "";
    if (!server) {
      return;
    }
    await new Promise((resolve) => {
      const timer = setTimeout(() => {
        server.closeAllConnections?.();
        resolve();
      }, 2_000);
      server.close(() => {
        clearTimeout(timer);
        resolve();
      });
    });
  }

  proxyHTTP(request, response) {
    const target = this.targetFor(request.url || "/");
    const proxyRequest = http.request({
      protocol: target.protocol,
      hostname: target.hostname,
      port: target.port,
      method: request.method,
      path: request.url,
      headers: forwardedHeaders(request.headers),
    }, (proxyResponse) => {
      response.writeHead(proxyResponse.statusCode || 502, proxyResponse.statusMessage, proxyResponse.headers);
      proxyResponse.pipe(response);
    });
    proxyRequest.on("error", (error) => {
      if (!response.headersSent) {
        response.writeHead(502, { "Content-Type": "application/json" });
      }
      response.end(JSON.stringify({ error: "mobile_bridge_unavailable", detail: error.message }));
    });
    request.on("aborted", () => proxyRequest.destroy());
    request.pipe(proxyRequest);
  }

  proxyUpgrade(request, socket, head) {
    const target = this.targetFor(request.url || "/");
    const proxyRequest = http.request({
      protocol: target.protocol,
      hostname: target.hostname,
      port: target.port,
      method: request.method,
      path: request.url,
      headers: forwardedHeaders(request.headers, true),
    });
    proxyRequest.on("upgrade", (proxyResponse, proxySocket, proxyHead) => {
      socket.write(serializeUpgradeResponse(proxyResponse));
      if (head.length > 0) {
        proxySocket.write(head);
      }
      if (proxyHead.length > 0) {
        socket.write(proxyHead);
      }
      proxySocket.pipe(socket).pipe(proxySocket);
    });
    proxyRequest.on("response", (proxyResponse) => {
      socket.write(serializeUpgradeResponse(proxyResponse));
      socket.destroy();
    });
    proxyRequest.on("error", () => socket.destroy());
    socket.on("error", () => proxyRequest.destroy());
    proxyRequest.end();
  }

  targetFor(rawPath) {
    const pathname = new URL(rawPath, "http://bridge.local").pathname;
    const base = API_PREFIXES.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))
      ? this.apiBase
      : this.webBase;
    return new URL(base);
  }
}

function forwardedHeaders(headers, upgrade = false) {
  const result = { ...headers };
  delete result["proxy-connection"];
  if (!upgrade) {
    delete result.upgrade;
  }
  return result;
}

function preferredLANHost(interfaces) {
  const candidates = [];
  for (const [name, addresses] of Object.entries(interfaces || {})) {
    for (const address of addresses || []) {
      const family = typeof address.family === "string" ? address.family : address.family === 4 ? "IPv4" : "";
      if (family !== "IPv4" || address.internal || !isPrivateIPv4(address.address)) {
        continue;
      }
      candidates.push({ name, address: address.address, priority: interfacePriority(name) });
    }
  }
  candidates.sort((left, right) => left.priority - right.priority || left.name.localeCompare(right.name));
  return candidates[0]?.address || "";
}

function interfacePriority(name) {
  if (/^en\d+$/.test(name)) return 0;
  if (/^(eth|wlan)\d+$/.test(name)) return 1;
  if (/^(bridge|docker|vmenet|utun|awdl|llw)/.test(name)) return 3;
  return 2;
}

function isPrivateIPv4(value) {
  const parts = String(value || "").split(".").map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return false;
  }
  return parts[0] === 10 ||
    (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) ||
    (parts[0] === 192 && parts[1] === 168) ||
    (parts[0] === 100 && parts[1] >= 64 && parts[1] <= 127);
}

function normalizeHTTPBase(value) {
  try {
    const url = new URL(String(value || "").trim());
    if (url.protocol !== "http:") {
      return "";
    }
    url.pathname = "/";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return "";
  }
}

function requestJSON(url, options) {
  return new Promise((resolve, reject) => {
    const request = http.request(url, options, (response) => {
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => {
        const text = Buffer.concat(chunks).toString("utf8");
        try {
          resolve({ statusCode: response.statusCode || 0, body: text ? JSON.parse(text) : null });
        } catch (error) {
          reject(new Error(`mobile pairing returned invalid JSON: ${error.message}`));
        }
      });
    });
    request.on("error", reject);
    request.end();
  });
}

function serializeUpgradeResponse(response) {
  const status = `HTTP/${response.httpVersion || "1.1"} ${response.statusCode || 500} ${response.statusMessage || ""}`.trimEnd();
  const lines = [status];
  for (const [name, value] of Object.entries(response.headers || {})) {
    if (Array.isArray(value)) {
      for (const entry of value) lines.push(`${name}: ${entry}`);
    } else if (value !== undefined) {
      lines.push(`${name}: ${value}`);
    }
  }
  return `${lines.join("\r\n")}\r\n\r\n`;
}

module.exports = { MobileAccessBridge, preferredLANHost };
