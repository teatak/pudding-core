const { spawn } = require("node:child_process");

const supportedCommands = new Set([
  "permissions",
  "list_apps",
  "launch_app",
  "quit_app",
  "observe",
  "capture",
  "act",
]);

class ComputerUseError extends Error {
  constructor(message, options = {}) {
    super(message);
    this.name = "ComputerUseError";
    this.code = options.code || "computer_unavailable";
    this.retryable = Boolean(options.retryable);
    this.outcome = options.outcome || "unknown";
  }
}

class ComputerUseHost {
  constructor(options = {}) {
    this.binaryPath = String(options.binaryPath || "").trim();
    this.spawnProcess = options.spawnProcess || spawn;
    this.platform = options.platform || process.platform;
    this.defaultTimeoutMs = positiveInteger(options.defaultTimeoutMs, 15_000);
    this.maximumMessageBytes = positiveInteger(options.maximumMessageBytes, 1024 * 1024);
    this.child = null;
    this.stdoutBuffer = "";
    this.pending = new Map();
    this.nextRequestID = 1;
    this.stopPromise = null;
  }

  permissions(params = {}, options = {}) {
    return this.request("permissions", params, options);
  }

  listApps(options = {}) {
    return this.request("list_apps", {}, options);
  }

  launchApp(params, options = {}) {
    return this.request("launch_app", params, options);
  }

  quitApp(params, options = {}) {
    return this.request("quit_app", params, options);
  }

  observe(params, options = {}) {
    return this.request("observe", params, options);
  }

  capture(params, options = {}) {
    return this.request("capture", params, options);
  }

  act(params, options = {}) {
    return this.request("act", params, options);
  }

  request(command, params = {}, options = {}) {
    if (this.platform !== "darwin") {
      return Promise.reject(new ComputerUseError("Computer Use is only available on macOS", {
        code: "computer_unavailable",
        outcome: "not_started",
      }));
    }
    if (!supportedCommands.has(command)) {
      return Promise.reject(new ComputerUseError(`unsupported Computer Use command: ${command}`, {
        code: "computer_invalid_request",
        outcome: "not_started",
      }));
    }
    if (!params || typeof params !== "object" || Array.isArray(params)) {
      return Promise.reject(new ComputerUseError("Computer Use params must be an object", {
        code: "computer_invalid_request",
        outcome: "not_started",
      }));
    }
    if (this.stopPromise) {
      return Promise.reject(new ComputerUseError("Computer Use is stopping", {
        code: "computer_unavailable",
        outcome: "not_started",
      }));
    }
    const signal = options.signal;
    if (signal?.aborted) {
      return Promise.reject(cancelledError("not_started"));
    }

    const id = `computer-${this.nextRequestID++}`;
    const line = JSON.stringify({ id, command, params });
    if (Buffer.byteLength(line, "utf8") > this.maximumMessageBytes) {
      return Promise.reject(new ComputerUseError("Computer Use request is too large", {
        code: "computer_invalid_request",
        outcome: "not_started",
      }));
    }
    let child;
    try {
      child = this.ensureProcess();
    } catch (error) {
      return Promise.reject(asComputerUseError(error, "computer_unavailable", "not_started"));
    }
    const timeoutMs = positiveInteger(options.timeoutMs, this.defaultTimeoutMs);

    return new Promise((resolve, reject) => {
      const pending = {
        resolve,
        reject,
        timer: null,
        signal,
        abortListener: null,
      };
      pending.timer = setTimeout(() => {
        this.abortRequest(id, new ComputerUseError("Computer Use request timed out", {
          code: "computer_action_timeout",
          outcome: "unknown",
        }));
      }, timeoutMs);
      if (signal) {
        pending.abortListener = () => {
          this.abortRequest(id, cancelledError("unknown"));
        };
        signal.addEventListener("abort", pending.abortListener, { once: true });
      }
      this.pending.set(id, pending);
      try {
        child.stdin.write(`${line}\n`, (error) => {
          if (error && this.pending.has(id)) {
            this.failProcess(child, asComputerUseError(error, "computer_helper_crashed", "unknown"));
          }
        });
      } catch (error) {
        this.failProcess(child, asComputerUseError(error, "computer_helper_crashed", "unknown"));
      }
    });
  }

  stop() {
    if (this.stopPromise) {
      return this.stopPromise;
    }
    const attempt = this.stopProcess();
    this.stopPromise = attempt;
    const clear = () => {
      if (this.stopPromise === attempt) {
        this.stopPromise = null;
      }
    };
    void attempt.then(clear, clear);
    return attempt;
  }

  ensureProcess() {
    if (this.child) {
      return this.child;
    }
    if (!this.binaryPath) {
      throw new Error("Computer Use helper binary is not configured");
    }
    const child = this.spawnProcess(this.binaryPath, ["serve"], {
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true,
    });
    this.child = child;
    this.stdoutBuffer = "";
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => this.handleStdout(child, chunk));
    child.stderr.on("data", () => {});
    child.once("error", (error) => {
      this.failProcess(child, asComputerUseError(error, "computer_helper_crashed", "unknown"));
    });
    child.once("exit", (code, signal) => {
      this.failProcess(
        child,
        new ComputerUseError(`Computer Use helper exited code=${code} signal=${signal}`, {
          code: "computer_helper_crashed",
          outcome: "unknown",
        }),
      );
    });
    child.stdin.on("error", () => {});
    return child;
  }

  handleStdout(child, chunk) {
    if (this.child !== child) {
      return;
    }
    this.stdoutBuffer += chunk;
    while (true) {
      const newline = this.stdoutBuffer.indexOf("\n");
      if (newline < 0) {
        if (Buffer.byteLength(this.stdoutBuffer, "utf8") > this.maximumMessageBytes) {
          this.failProcess(child, oversizedResponseError());
        }
        return;
      }
      const line = this.stdoutBuffer.slice(0, newline).replace(/\r$/, "");
      this.stdoutBuffer = this.stdoutBuffer.slice(newline + 1);
      if (Buffer.byteLength(line, "utf8") > this.maximumMessageBytes) {
        this.failProcess(child, oversizedResponseError());
        return;
      }
      if (!line) {
        this.failProcess(child, protocolError("Computer Use helper returned an empty response"));
        return;
      }
      if (!this.handleResponseLine(child, line)) {
        return;
      }
    }
  }

  handleResponseLine(child, line) {
    let response;
    try {
      response = JSON.parse(line);
    } catch {
      this.failProcess(child, protocolError("Computer Use helper returned invalid JSON"));
      return false;
    }
    const id = typeof response?.id === "string" ? response.id : "";
    const pending = this.pending.get(id);
    if (!pending || typeof response.ok !== "boolean") {
      this.failProcess(child, protocolError("Computer Use helper returned an unmatched response"));
      return false;
    }
    this.pending.delete(id);
    cleanupPending(pending);
    if (response.ok) {
      pending.resolve(response.result);
      return true;
    }
    const detail = response.error || {};
    pending.reject(new ComputerUseError(String(detail.message || "Computer Use request failed"), {
      code: String(detail.code || "computer_unavailable"),
      retryable: Boolean(detail.retryable),
      outcome: validOutcome(detail.outcome),
    }));
    return true;
  }

  abortRequest(id, error) {
    if (!this.pending.has(id)) {
      return;
    }
    const child = this.child;
    this.child = null;
    this.stdoutBuffer = "";
    for (const [pendingID, pending] of this.pending) {
      cleanupPending(pending);
      pending.reject(
        pendingID === id
          ? error
          : new ComputerUseError("Computer Use helper was stopped with another request", {
              code: "computer_helper_crashed",
              outcome: "unknown",
            }),
      );
    }
    this.pending.clear();
    terminateChild(child);
  }

  failProcess(child, error) {
    if (this.child !== child) {
      return;
    }
    this.child = null;
    this.stdoutBuffer = "";
    for (const pending of this.pending.values()) {
      cleanupPending(pending);
      pending.reject(error);
    }
    this.pending.clear();
    terminateChild(child);
  }

  async stopProcess() {
    const child = this.child;
    this.child = null;
    this.stdoutBuffer = "";
    for (const pending of this.pending.values()) {
      cleanupPending(pending);
      pending.reject(cancelledError("unknown"));
    }
    this.pending.clear();
    if (!child || child.exitCode !== null) {
      return;
    }
    await new Promise((resolve) => {
      let timer = null;
      let settled = false;
      const finish = () => {
        if (settled) {
          return;
        }
        settled = true;
        clearTimeout(timer);
        resolve();
      };
      child.once("exit", finish);
      child.once("error", finish);
      child.stdin.end();
      timer = setTimeout(() => {
        terminateChild(child);
        finish();
      }, 2_000);
    });
  }
}

function cleanupPending(pending) {
  clearTimeout(pending.timer);
  if (pending.signal && pending.abortListener) {
    pending.signal.removeEventListener("abort", pending.abortListener);
  }
}

function terminateChild(child) {
  if (child && child.exitCode === null && child.signalCode === null) {
    child.kill("SIGTERM");
  }
}

function cancelledError(outcome) {
  return new ComputerUseError("Computer Use request was cancelled", {
    code: "computer_action_cancelled",
    outcome,
  });
}

function protocolError(message) {
  return new ComputerUseError(message, {
    code: "computer_helper_crashed",
    outcome: "unknown",
  });
}

function oversizedResponseError() {
  return new ComputerUseError("Computer Use helper response is too large", {
    code: "computer_helper_crashed",
    outcome: "unknown",
  });
}

function asComputerUseError(error, code, outcome) {
  if (error instanceof ComputerUseError) {
    return error;
  }
  return new ComputerUseError(String(error?.message || error || "Computer Use unavailable"), {
    code,
    outcome,
  });
}

function validOutcome(value) {
  return value === "not_started" || value === "completed" || value === "unknown"
    ? value
    : "unknown";
}

function positiveInteger(value, fallback) {
  return Number.isSafeInteger(value) && value > 0 ? value : fallback;
}

module.exports = { ComputerUseError, ComputerUseHost };
