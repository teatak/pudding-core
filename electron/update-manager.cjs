const updateStatuses = Object.freeze({
  unavailable: "unavailable",
  idle: "idle",
  checking: "checking",
  downloading: "downloading",
  downloaded: "downloaded",
  installing: "installing",
});

class UpdateManager {
  constructor({
    updater,
    isPackaged,
    disabled = false,
    initialDelayMs = 15_000,
    intervalMs = 6 * 60 * 60 * 1_000,
    beforeInstall = async () => {},
    onError = () => {},
    onManualResult = async () => {},
    onStateChange = () => {},
    setTimeoutFn = setTimeout,
    clearTimeoutFn = clearTimeout,
  }) {
    this.updater = updater;
    this.enabled = Boolean(updater && isPackaged && !disabled);
    this.initialDelayMs = initialDelayMs;
    this.intervalMs = intervalMs;
    this.beforeInstall = beforeInstall;
    this.onError = onError;
    this.onManualResult = onManualResult;
    this.onStateChange = onStateChange;
    this.setTimeoutFn = setTimeoutFn;
    this.clearTimeoutFn = clearTimeoutFn;
    this.timer = null;
    this.started = false;
    this.manualCheck = false;
    this.state = {
      status: this.enabled ? updateStatuses.idle : updateStatuses.unavailable,
      version: "",
      percent: null,
    };

    if (this.enabled) {
      this.configureUpdater();
      this.bindUpdaterEvents();
    }
  }

  configureUpdater() {
    this.updater.autoDownload = true;
    this.updater.autoInstallOnAppQuit = true;
    this.updater.allowPrerelease = false;
  }

  bindUpdaterEvents() {
    this.updater.on("checking-for-update", () => {
      if (this.state.status === updateStatuses.idle) {
        this.setState({ status: updateStatuses.checking, percent: null });
      }
    });
    this.updater.on("update-available", (info) => {
      const manual = this.takeManualCheck();
      const version = cleanVersion(info?.version);
      this.setState({ status: updateStatuses.downloading, version, percent: 0 });
      if (manual) {
        void this.onManualResult({ kind: "downloading", version });
      }
    });
    this.updater.on("update-not-available", () => {
      const manual = this.takeManualCheck();
      this.setState({ status: updateStatuses.idle, version: "", percent: null });
      if (manual) {
        void this.onManualResult({ kind: "up-to-date" });
      }
    });
    this.updater.on("download-progress", (progress) => {
      this.setState({
        status: updateStatuses.downloading,
        percent: clampPercent(progress?.percent),
      });
    });
    this.updater.on("update-downloaded", (info) => {
      this.manualCheck = false;
      this.setState({
        status: updateStatuses.downloaded,
        version: cleanVersion(info?.version) || this.state.version,
        percent: 100,
      });
    });
    this.updater.on("error", (error) => this.handleError(error));
  }

  getState() {
    return { ...this.state };
  }

  start() {
    if (!this.enabled || this.started) {
      return;
    }
    this.started = true;
    this.onStateChange(this.getState());
    this.schedule(this.initialDelayMs);
  }

  stop() {
    this.started = false;
    if (this.timer) {
      this.clearTimeoutFn(this.timer);
      this.timer = null;
    }
  }

  schedule(delayMs) {
    this.timer = this.setTimeoutFn(async () => {
      this.timer = null;
      try {
        await this.check(false);
      } finally {
        if (this.started) {
          this.schedule(this.intervalMs);
        }
      }
    }, delayMs);
    this.timer?.unref?.();
  }

  async check(manual = false) {
    if (!this.enabled) {
      if (manual) {
        await this.onManualResult({ kind: "development" });
      }
      return false;
    }
    if (this.state.status === updateStatuses.downloaded) {
      if (manual) {
        await this.onManualResult({ kind: "downloaded", version: this.state.version });
      }
      return true;
    }
    if (
      this.state.status === updateStatuses.checking ||
      this.state.status === updateStatuses.downloading ||
      this.state.status === updateStatuses.installing
    ) {
      if (manual) {
        await this.onManualResult({ kind: this.state.status, version: this.state.version });
      }
      return false;
    }

    this.manualCheck = Boolean(manual);
    this.setState({ status: updateStatuses.checking, percent: null });
    try {
      await this.updater.checkForUpdates();
      return true;
    } catch (error) {
      if (this.state.status === updateStatuses.checking) {
        this.handleError(error);
      }
      return false;
    }
  }

  async install() {
    if (!this.enabled || this.state.status !== updateStatuses.downloaded) {
      return false;
    }
    const downloadedState = this.getState();
    this.setState({ status: updateStatuses.installing });
    this.stop();
    try {
      await this.beforeInstall();
      this.updater.quitAndInstall(false, true);
      return true;
    } catch (error) {
      this.onError(error);
      this.state = downloadedState;
      this.onStateChange(this.getState());
      await this.onManualResult({ kind: "install-error", error: errorMessage(error) });
      return false;
    }
  }

  handleError(error) {
    this.onError(error);
    const manual = this.takeManualCheck();
    if (this.state.status !== updateStatuses.downloaded && this.state.status !== updateStatuses.installing) {
      this.setState({ status: updateStatuses.idle, version: "", percent: null });
    }
    if (manual) {
      void this.onManualResult({ kind: "error", error: errorMessage(error) });
    }
  }

  takeManualCheck() {
    const manual = this.manualCheck;
    this.manualCheck = false;
    return manual;
  }

  setState(patch) {
    this.state = { ...this.state, ...patch };
    this.onStateChange(this.getState());
  }
}

function cleanVersion(value) {
  return String(value || "").trim();
}

function clampPercent(value) {
  const percent = Number(value);
  if (!Number.isFinite(percent)) {
    return null;
  }
  return Math.max(0, Math.min(100, Math.round(percent)));
}

function errorMessage(error) {
  return String(error?.message || error || "update failed");
}

module.exports = { UpdateManager, updateStatuses };
