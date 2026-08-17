const crypto = require("node:crypto");
const http = require("node:http");

const maximumRequestBodyBytes = 256 * 1024;

class ComputerUseBridgeServer {
  constructor(host, options = {}) {
    this.host = host;
    this.permissionCoordinator = options.permissionCoordinator || null;
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
    const clear = () => {
      if (this.startPromise === attempt) {
        this.startPromise = null;
      }
    };
    void attempt.then(clear, clear);
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
        throw new Error("Computer Use bridge failed to bind loopback");
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
    const clear = () => {
      if (this.stopPromise === attempt) {
        this.stopPromise = null;
      }
    };
    void attempt.then(clear, clear);
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
    const controller = new AbortController();
    const abort = () => controller.abort();
    request.once("aborted", abort);
    response.once("close", () => {
      if (!response.writableEnded) {
        abort();
      }
    });
    try {
      if (request.headers.authorization !== `Bearer ${this.token}`) {
        writeJSON(response, 401, { error: "unauthorized", code: "computer_unauthorized" });
        return;
      }
      const path = request.url || "";
      if (request.method === "GET" && path === "/computer/permissions") {
        const result = await this.host.permissions({}, { signal: controller.signal });
        writeJSON(response, 200, result);
        return;
      }
      if (request.method !== "POST") {
        writeJSON(response, 405, { error: "method_not_allowed", code: "computer_invalid_request" });
        return;
      }
      const body = await readJSON(request);
      const operation = () => this.route(path, body, controller.signal);
      const requiredPermissions = permissionsForRoute(path);
      const result = this.permissionCoordinator
        ? await this.permissionCoordinator.run(requiredPermissions, operation, { signal: controller.signal })
        : await operation();
      writeJSON(response, 200, result || { ok: true });
    } catch (error) {
      if (response.writableEnded || response.destroyed) {
        return;
      }
      const failure = classifyComputerUseError(error);
      writeJSON(response, failure.status, {
        error: failure.message,
        code: failure.code,
        ...(failure.permission ? { permission: failure.permission } : {}),
        ...(failure.permissions.length > 0 ? { permissions: failure.permissions } : {}),
        retryable: failure.retryable,
        outcome: failure.outcome,
      });
    } finally {
      request.off("aborted", abort);
    }
  }

  route(path, body, signal) {
    requiredSessionID(body);
    switch (path) {
      case "/computer/apps/list":
        return this.host.listApps({ signal });
      case "/computer/apps/use":
        return this.host.useApp({
          bundleID: body.appID,
          foreground: body.foreground === true,
        }, { signal });
      case "/computer/apps/quit":
        return this.host.quitApp({ bundleID: body.appID, pid: body.pid }, { signal });
      case "/computer/observe":
        return this.host.observe({
          bundleID: body.appID,
          windowID: body.windowID,
          maxElements: body.maxElements,
        }, { signal });
      case "/computer/observe-capture":
        return this.host.observeCapture({
          bundleID: body.appID,
          windowID: body.windowID,
          maxElements: body.maxElements,
          output: body.output,
        }, { signal });
      case "/computer/act":
        return this.host.act({
          bundleID: body.appID,
          windowID: body.windowID,
          elementID: body.elementID,
          action: body.action,
          value: body.value,
        }, { signal });
      case "/computer/pointer":
        return this.host.pointer({
          bundleID: body.appID,
          windowID: body.windowID,
          action: body.action,
          x: body.x,
          y: body.y,
          toX: body.toX,
          toY: body.toY,
          button: body.button,
          clickCount: body.clickCount,
          deltaX: body.deltaX,
          deltaY: body.deltaY,
        }, { signal });
      default:
        throw invalidRequest("Computer Use bridge route not found");
    }
  }

}

function readJSON(request) {
  return new Promise((resolve, reject) => {
    let raw = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => {
      raw += chunk;
      if (Buffer.byteLength(raw, "utf8") > maximumRequestBodyBytes) {
        reject(invalidRequest("Computer Use request body is too large"));
        request.destroy();
      }
    });
    request.on("end", () => {
      if (!raw.trim()) {
        resolve({});
        return;
      }
      try {
        const body = JSON.parse(raw);
        if (!body || typeof body !== "object" || Array.isArray(body)) {
          throw new Error("body must be an object");
        }
        resolve(body);
      } catch {
        reject(invalidRequest("invalid Computer Use JSON body"));
      }
    });
    request.on("error", reject);
  });
}

function requiredSessionID(body) {
  const sessionID = String(body?.sessionID || "").trim();
  if (!sessionID || Buffer.byteLength(sessionID, "utf8") > 128) {
    throw invalidRequest("sessionID is required");
  }
  return sessionID;
}

function writeJSON(response, status, payload) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(payload));
}

function invalidRequest(message) {
  const error = new Error(message);
  error.code = "computer_invalid_request";
  error.outcome = "not_started";
  return error;
}

function classifyComputerUseError(error) {
  const code = String(error?.code || "computer_unavailable");
  const message = String(error?.message || "Computer Use unavailable");
  const outcome = validOutcome(error?.outcome);
  const retryable = Boolean(error?.retryable);
  const permission = String(error?.permission || "").trim();
  const permissions = Array.isArray(error?.permissions)
    ? error.permissions.map((value) => String(value || "").trim()).filter(Boolean)
    : [];
  let status = 500;
  switch (code) {
    case "computer_invalid_request":
      status = 400;
      break;
    case "computer_unauthorized":
    case "computer_permission_required":
    case "computer_permission_denied":
    case "computer_action_blocked":
      status = 403;
      break;
    case "computer_app_not_found":
    case "computer_app_not_installed":
    case "computer_window_not_found":
    case "computer_element_not_found":
      status = 404;
      break;
    case "computer_pointer_target_changed":
    case "computer_app_not_foreground":
    case "computer_element_not_actionable":
      status = 409;
      break;
    case "computer_action_cancelled":
      status = 408;
      break;
    case "computer_action_timeout":
      status = 504;
      break;
    case "computer_helper_crashed":
    case "computer_unavailable":
      status = 503;
      break;
    default:
      status = 500;
  }
  return { status, code, message, permission, permissions, retryable, outcome };
}

function permissionsForRoute(path) {
  switch (path) {
    case "/computer/apps/use":
      return ["screenRecording"];
    case "/computer/observe":
    case "/computer/observe-capture":
    case "/computer/act":
    case "/computer/pointer":
      return ["accessibility", "screenRecording"];
    default:
      return [];
  }
}

function validOutcome(value) {
  return value === "not_started" || value === "completed" || value === "unknown"
    ? value
    : "unknown";
}

module.exports = { ComputerUseBridgeServer, classifyComputerUseError, permissionsForRoute };
