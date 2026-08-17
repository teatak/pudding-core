const crypto = require("node:crypto");

const { ComputerUseError } = require("./computer-use-host.cjs");

const computerUsePermissions = Object.freeze(["accessibility", "screenRecording"]);

class ComputerUsePermissionCoordinator {
  constructor(options) {
    this.permissions = options.permissions;
    this.restartHelper = options.restartHelper || (async () => {});
    this.onGuideChange = options.onGuideChange || (() => {});
    this.guide = null;
    this.waiter = null;
  }

  currentGuide() {
    return this.guide;
  }

  async run(required, operation, options = {}) {
    const normalized = normalizeRequired(required);
    if (normalized.length === 0) {
      return operation();
    }
    await this.waitUntilGranted(normalized, options.signal);
    try {
      return await operation();
    } catch (error) {
      if (!isPermissionError(error)) {
        throw error;
      }
      const permission = normalizePermission(error.permission);
      if (!permission) {
        throw error;
      }
      await this.waitUntilGranted([permission], options.signal);
      try {
        return await operation();
      } catch (retryError) {
        if (!isPermissionError(retryError) || normalizePermission(retryError.permission) !== permission) {
          throw retryError;
        }
        await this.restartHelper();
        try {
          return await operation();
        } catch (restartError) {
          if (!isPermissionError(restartError) || normalizePermission(restartError.permission) !== permission) {
            throw restartError;
          }
          await this.waitForRestart([permission], options.signal);
          throw restartError;
        }
      }
    }
  }

  async refresh() {
    if (this.waiter && !this.waiter.restartRequired) {
      await this.restartHelper();
    }
    const state = await this.permissions.refresh();
    this.reconcile(state);
    return state;
  }

  reconcile(state) {
    if (!this.waiter || this.waiter.restartRequired) {
      if (this.guide) {
        this.updateGuideState(state);
      }
      return;
    }
    this.updateGuideState(state);
    if (this.waiter.required.every((permission) => state[permission] === true)) {
      const waiter = this.waiter;
      this.clearGuide();
      waiter.resolve();
    }
  }

  deny(requestID) {
    if (!this.waiter || this.guide?.requestID !== String(requestID || "")) {
      return false;
    }
    const waiter = this.waiter;
    this.clearGuide();
    waiter.reject(permissionDenied(waiter.required));
    return true;
  }

  async waitUntilGranted(required, signal) {
    const state = await this.permissions.refresh();
    if (!state.supported) {
      throw new ComputerUseError("Computer Use permissions are unavailable on this platform", {
        code: "computer_unavailable",
        outcome: "not_started",
      });
    }
    const missing = required.filter((permission) => state[permission] !== true);
    if (missing.length === 0) {
      return;
    }
    await this.waitForGuide(missing, state, false, signal);
  }

  async waitForRestart(required, signal) {
    const state = await this.permissions.refresh();
    await this.waitForGuide(required, state, true, signal);
  }

  waitForGuide(required, state, restartRequired, signal) {
    if (signal?.aborted) {
      return Promise.reject(cancelled());
    }
    if (this.waiter) {
      return Promise.reject(new ComputerUseError("Another Computer Use permission request is pending", {
        code: "computer_unavailable",
        outcome: "not_started",
      }));
    }
    const requestID = crypto.randomUUID();
    const promise = new Promise((resolve, reject) => {
      const abort = () => {
        if (this.guide?.requestID !== requestID) {
          return;
        }
        this.clearGuide();
        reject(cancelled());
      };
      this.waiter = { requestID, required, restartRequired, resolve, reject, signal, abort };
      signal?.addEventListener("abort", abort, { once: true });
    });
    this.guide = guideSnapshot(requestID, required, state, restartRequired);
    this.onGuideChange(this.guide);
    return promise;
  }

  updateGuideState(state) {
    if (!this.guide) {
      return;
    }
    this.guide = guideSnapshot(
      this.guide.requestID,
      this.guide.required,
      state,
      this.guide.restartRequired,
    );
    this.onGuideChange(this.guide);
  }

  clearGuide() {
    const waiter = this.waiter;
    if (waiter?.signal && waiter.abort) {
      waiter.signal.removeEventListener("abort", waiter.abort);
    }
    this.waiter = null;
    this.guide = null;
    this.onGuideChange(null);
  }
}

function guideSnapshot(requestID, required, state, restartRequired) {
  return {
    requestID,
    required,
    restartRequired,
    permissions: computerUsePermissions.map((permission) => ({
      permission,
      allowed: state[permission] === true,
    })),
  };
}

function normalizeRequired(required) {
  return [...new Set((Array.isArray(required) ? required : []).map(normalizePermission).filter(Boolean))];
}

function normalizePermission(permission) {
  const value = String(permission || "").trim();
  if (value === "screen_recording") {
    return "screenRecording";
  }
  return computerUsePermissions.includes(value) ? value : "";
}

function isPermissionError(error) {
  return error?.code === "computer_permission_required" && error?.outcome === "not_started";
}

function permissionDenied(required) {
  const error = new ComputerUseError("Computer Use permission was not granted", {
    code: "computer_permission_denied",
    outcome: "not_started",
  });
  error.permissions = required;
  return error;
}

function cancelled() {
  return new ComputerUseError("Computer Use permission request was cancelled", {
    code: "computer_action_cancelled",
    outcome: "not_started",
  });
}

module.exports = {
  ComputerUsePermissionCoordinator,
  computerUsePermissions,
  normalizePermission,
};
