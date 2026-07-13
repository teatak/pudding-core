const updateStatuses = Object.freeze({
  unavailable: "unavailable",
  idle: "idle",
  checking: "checking",
  available: "available",
  downloading: "downloading",
  downloaded: "downloaded",
  installing: "installing",
});

const updateModes = Object.freeze({
  manual: "manual",
  automatic: "automatic",
});

class UpdateManager {
  constructor({
    updater,
    isPackaged,
    disabled = false,
    mode = updateModes.manual,
    receivePreviewUpdates = false,
    feedURL = "",
    simulatedVersion = "",
    initialDelayMs = 15_000,
    intervalMs = 6 * 60 * 60 * 1_000,
    beforeInstall = async () => {},
    onError = () => {},
    onManualResult = async () => {},
    onSimulatedInstall = async () => {},
    onStateChange = () => {},
    setTimeoutFn = setTimeout,
    clearTimeoutFn = clearTimeout,
  }) {
    this.updater = updater;
    this.feedURL = String(feedURL || "").trim();
    this.simulatedVersion = cleanVersion(simulatedVersion);
    this.simulated = Boolean(this.simulatedVersion);
    this.mode = this.simulated ? updateModes.automatic : normalizeUpdateMode(mode);
    this.receivePreviewUpdates = Boolean(receivePreviewUpdates);
    this.enabled = this.simulated || Boolean(updater && isPackaged && !disabled);
    this.initialDelayMs = initialDelayMs;
    this.intervalMs = intervalMs;
    this.beforeInstall = beforeInstall;
    this.onError = onError;
    this.onManualResult = onManualResult;
    this.onSimulatedInstall = onSimulatedInstall;
    this.onStateChange = onStateChange;
    this.setTimeoutFn = setTimeoutFn;
    this.clearTimeoutFn = clearTimeoutFn;
    this.timer = null;
    this.started = false;
    this.manualCheck = false;
    this.manualDownload = false;
    this.state = {
      status: this.simulated ? updateStatuses.downloaded : this.enabled ? updateStatuses.idle : updateStatuses.unavailable,
      mode: this.mode,
      receivePreviewUpdates: this.receivePreviewUpdates,
      version: this.simulatedVersion,
      percent: this.simulated ? 100 : null,
    };

    if (this.enabled && !this.simulated) {
      this.configureUpdater();
      this.bindUpdaterEvents();
    }
  }

  configureUpdater() {
    this.updater.autoDownload = this.mode === updateModes.automatic;
    this.updater.autoInstallOnAppQuit = false;
    this.configureUpdaterChannel();
    if (this.feedURL) {
      this.updater.setFeedURL({ provider: "generic", url: this.feedURL });
    }
  }

  configureUpdaterChannel() {
    this.updater.channel = this.receivePreviewUpdates ? "beta" : "latest";
    this.updater.allowPrerelease = this.receivePreviewUpdates;
    // electron-updater enables downgrades whenever channel is assigned.
    this.updater.allowDowngrade = false;
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
      if (this.mode === updateModes.manual) {
        this.manualDownload = false;
        this.setState({ status: updateStatuses.available, version, percent: null });
        if (manual) {
          void this.onManualResult({ kind: "available", version });
        }
        return;
      }
      this.manualDownload = manual;
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
      this.manualDownload = false;
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

  setReceivePreviewUpdates(enabled) {
    const next = Boolean(enabled);
    if (next === this.receivePreviewUpdates) {
      return this.getState();
    }
    if (
      this.state.status === updateStatuses.checking ||
      this.state.status === updateStatuses.downloading ||
      this.state.status === updateStatuses.downloaded ||
      this.state.status === updateStatuses.installing
    ) {
      throw new Error("update channel cannot change while an update is active");
    }

    this.receivePreviewUpdates = next;
    this.manualCheck = false;
    this.manualDownload = false;
    if (this.enabled && !this.simulated) {
      this.configureUpdaterChannel();
    }
    this.setState({
      status: this.enabled ? updateStatuses.idle : updateStatuses.unavailable,
      receivePreviewUpdates: next,
      version: "",
      percent: null,
    });
    return this.getState();
  }

  start() {
    if (!this.enabled || this.started) {
      return;
    }
    this.started = true;
    this.onStateChange(this.getState());
    if (this.simulated) {
      return;
    }
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
    if (this.simulated) {
      if (manual) {
        await this.onManualResult({ kind: "downloaded", version: this.state.version });
      }
      return true;
    }
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
    if (this.state.status === updateStatuses.available) {
      if (manual) {
        await this.onManualResult({ kind: "available", version: this.state.version });
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
    if (this.mode !== updateModes.automatic || !this.enabled || this.state.status !== updateStatuses.downloaded) {
      return false;
    }
    const downloadedState = this.getState();
    this.setState({ status: updateStatuses.installing });
    if (this.simulated) {
      await this.onSimulatedInstall(downloadedState);
      this.state = downloadedState;
      this.onStateChange(this.getState());
      return true;
    }
    this.stop();
    try {
      await this.beforeInstall();
      this.updater.quitAndInstall(false, true);
      return true;
    } catch (error) {
      this.onError(error);
      this.setState({ status: updateStatuses.idle, version: "", percent: null });
      await this.onManualResult({ kind: "install-error", error: errorMessage(error) });
      return false;
    }
  }

  handleError(error) {
    this.onError(error);
    const installing = this.state.status === updateStatuses.installing;
    const manual = this.takeManualCheck() || this.takeManualDownload();
    this.setState({ status: updateStatuses.idle, version: "", percent: null });
    if (installing) {
      void this.onManualResult({ kind: "install-error", error: errorMessage(error) });
    } else if (manual) {
      void this.onManualResult({ kind: "error", error: errorMessage(error) });
    }
  }

  takeManualCheck() {
    const manual = this.manualCheck;
    this.manualCheck = false;
    return manual;
  }

  takeManualDownload() {
    const manual = this.manualDownload;
    this.manualDownload = false;
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

function normalizeUpdateMode(value) {
  return String(value || "").trim().toLowerCase() === updateModes.automatic
    ? updateModes.automatic
    : updateModes.manual;
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

module.exports = { UpdateManager, updateModes, updateStatuses };
