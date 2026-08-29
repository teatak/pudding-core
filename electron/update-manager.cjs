const updateStatuses = Object.freeze({
  unavailable: "unavailable",
  idle: "idle",
  checking: "checking",
  available: "available",
  downloading: "downloading",
  downloaded: "downloaded",
  installing: "installing",
});

class UpdateManager {
  constructor({
    updater,
    isPackaged,
    disabled = false,
    receivePreviewUpdates = false,
    feedURL = "",
    simulatedVersion = "",
    initialDelayMs = 15_000,
    intervalMs = 6 * 60 * 60 * 1_000,
    beforeInstall = async () => {},
    onDownloadRequest = async () => false,
    onError = () => {},
    onInteractiveResult = async () => {},
    onSimulatedInstall = async () => {},
    onStateChange = () => {},
    setTimeoutFn = setTimeout,
    clearTimeoutFn = clearTimeout,
  }) {
    this.updater = updater;
    this.feedURL = String(feedURL || "").trim();
    this.simulatedVersion = cleanVersion(simulatedVersion);
    this.simulated = Boolean(this.simulatedVersion);
    this.receivePreviewUpdates = Boolean(receivePreviewUpdates);
    this.enabled = this.simulated || Boolean(updater && isPackaged && !disabled);
    this.initialDelayMs = initialDelayMs;
    this.intervalMs = intervalMs;
    this.beforeInstall = beforeInstall;
    this.onDownloadRequest = onDownloadRequest;
    this.onError = onError;
    this.onInteractiveResult = onInteractiveResult;
    this.onSimulatedInstall = onSimulatedInstall;
    this.onStateChange = onStateChange;
    this.setTimeoutFn = setTimeoutFn;
    this.clearTimeoutFn = clearTimeoutFn;
    this.timer = null;
    this.started = false;
    this.interactiveCheck = false;
    this.installPromise = null;
    this.state = {
      status: this.simulated ? updateStatuses.available : this.enabled ? updateStatuses.idle : updateStatuses.unavailable,
      receivePreviewUpdates: this.receivePreviewUpdates,
      version: this.simulatedVersion,
      percent: null,
    };

    if (this.enabled && !this.simulated) {
      this.configureUpdater();
      this.bindUpdaterEvents();
    }
  }

  configureUpdater() {
    this.updater.autoDownload = false;
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
      const interactive = this.takeInteractiveCheck();
      const version = cleanVersion(info?.version);
      this.setState({ status: updateStatuses.available, version, percent: null });
      if (interactive) {
        void this.confirmAvailableDownload(version);
      }
    });
    this.updater.on("update-not-available", () => {
      const interactive = this.takeInteractiveCheck();
      this.setState({ status: updateStatuses.idle, version: "", percent: null });
      if (interactive) {
        void this.onInteractiveResult({ kind: "up-to-date" });
      }
    });
    this.updater.on("download-progress", (progress) => {
      this.setState({
        status: updateStatuses.downloading,
        percent: clampPercent(progress?.percent),
      });
    });
    this.updater.on("update-downloaded", (info) => {
      this.interactiveCheck = false;
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
      this.state.status === updateStatuses.available ||
      this.state.status === updateStatuses.downloading ||
      this.state.status === updateStatuses.downloaded ||
      this.state.status === updateStatuses.installing
    ) {
      throw new Error("update channel cannot change while an update is active");
    }

    this.receivePreviewUpdates = next;
    this.interactiveCheck = false;
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

  async check(interactive = false) {
    if (this.simulated) {
      if (interactive) {
        return this.confirmAvailableDownload(this.state.version);
      }
      return true;
    }
    if (!this.enabled) {
      if (interactive) {
        await this.onInteractiveResult({ kind: "development" });
      }
      return false;
    }
    if (this.state.status === updateStatuses.available && interactive) {
      return this.confirmAvailableDownload(this.state.version);
    }
    if (
      this.state.status === updateStatuses.checking ||
      this.state.status === updateStatuses.downloading ||
      this.state.status === updateStatuses.downloaded ||
      this.state.status === updateStatuses.installing
    ) {
      if (interactive) {
        await this.onInteractiveResult({ kind: this.state.status, version: this.state.version });
      }
      return false;
    }

    const refreshingAvailable = this.state.status === updateStatuses.available;
    this.interactiveCheck = Boolean(interactive);
    if (!refreshingAvailable) {
      this.setState({ status: updateStatuses.checking, percent: null });
    }
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
    if (this.installPromise) {
      return this.installPromise;
    }
    const installPromise = this.finishInstall();
    this.installPromise = installPromise;
    try {
      return await installPromise;
    } finally {
      if (this.installPromise === installPromise) {
        this.installPromise = null;
      }
    }
  }

  async confirmAvailableDownload(version) {
    try {
      const accepted = await this.onDownloadRequest({ version });
      if (!accepted || this.state.status !== updateStatuses.available || this.state.version !== version) {
        return false;
      }
      return this.download();
    } catch (error) {
      this.onError(error);
      await this.onInteractiveResult({ kind: "error", error: errorMessage(error) });
      return false;
    }
  }

  async download() {
    if (!this.enabled || this.state.status !== updateStatuses.available) {
      return false;
    }
    this.setState({ status: updateStatuses.downloading, percent: 0 });
    if (this.simulated) {
      return this.simulateDownload();
    }
    try {
      await this.updater.downloadUpdate();
      return true;
    } catch (error) {
      if (this.state.status === updateStatuses.downloading) {
        this.handleError(error);
      }
      return false;
    }
  }

  async simulateDownload() {
    for (const percent of [8, 19, 33, 48, 64, 79, 91, 100]) {
      await new Promise((resolve) => this.setTimeoutFn(resolve, 250));
      if (this.state.status !== updateStatuses.downloading) {
        return false;
      }
      this.setState({ percent });
    }
    this.setState({ status: updateStatuses.downloaded });
    return true;
  }

  async finishInstall() {
    if (!this.enabled || this.state.status !== updateStatuses.downloaded) {
      return false;
    }

    const downloadedState = this.getState();
    this.setState({ status: updateStatuses.installing });
    if (this.simulated) {
      await this.onSimulatedInstall(downloadedState);
      this.state = { ...downloadedState, status: updateStatuses.available, percent: null };
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
      await this.onInteractiveResult({ kind: "install-error", error: errorMessage(error) });
      return false;
    }
  }

  handleError(error) {
    this.onError(error);
    const installing = this.state.status === updateStatuses.installing;
    const interactive = this.takeInteractiveCheck() || this.state.status === updateStatuses.downloading;
    if (this.state.status === updateStatuses.available && !interactive) {
      return;
    }
    this.setState({ status: updateStatuses.idle, version: "", percent: null });
    if (installing) {
      void this.onInteractiveResult({ kind: "install-error", error: errorMessage(error) });
    } else if (interactive) {
      void this.onInteractiveResult({ kind: "error", error: errorMessage(error) });
    }
  }

  takeInteractiveCheck() {
    const interactive = this.interactiveCheck;
    this.interactiveCheck = false;
    return interactive;
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
