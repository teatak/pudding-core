const semver = require("semver");

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
    receivePreviewUpdates = false,
    feedURL = "",
    simulatedVersion = "",
    initialDelayMs = 15_000,
    intervalMs = 6 * 60 * 60 * 1_000,
    beforeInstall = async () => {},
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
    this.onError = onError;
    this.onInteractiveResult = onInteractiveResult;
    this.onSimulatedInstall = onSimulatedInstall;
    this.onStateChange = onStateChange;
    this.setTimeoutFn = setTimeoutFn;
    this.clearTimeoutFn = clearTimeoutFn;
    this.timer = null;
    this.started = false;
    this.interactiveCheck = false;
    this.interactiveDownload = false;
    this.downloadedRefreshFallback = null;
    this.downloadedRefreshInteractive = false;
    this.downloadedRefreshPromise = null;
    this.installPromise = null;
    this.state = {
      status: this.simulated ? updateStatuses.downloaded : this.enabled ? updateStatuses.idle : updateStatuses.unavailable,
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
    this.updater.autoDownload = true;
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
      if (this.downloadedRefreshFallback) {
        return;
      }
      const interactive = this.takeInteractiveCheck();
      const version = cleanVersion(info?.version);
      this.interactiveDownload = interactive;
      this.setState({ status: updateStatuses.downloading, version, percent: 0 });
      if (interactive) {
        void this.onInteractiveResult({ kind: "downloading", version });
      }
    });
    this.updater.on("update-not-available", () => {
      if (this.downloadedRefreshFallback) {
        return;
      }
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
      this.interactiveDownload = false;
      this.clearDownloadedRefreshContext();
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
    this.interactiveCheck = false;
    this.interactiveDownload = false;
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
        await this.onInteractiveResult({ kind: "downloaded", version: this.state.version });
      }
      return true;
    }
    if (!this.enabled) {
      if (interactive) {
        await this.onInteractiveResult({ kind: "development" });
      }
      return false;
    }
    if (this.state.status === updateStatuses.downloaded) {
      return this.refreshDownloadedUpdate(interactive);
    }
    if (
      this.state.status === updateStatuses.checking ||
      this.state.status === updateStatuses.downloading ||
      this.state.status === updateStatuses.installing
    ) {
      if (interactive) {
        await this.onInteractiveResult({ kind: this.state.status, version: this.state.version });
      }
      return false;
    }

    this.interactiveCheck = Boolean(interactive);
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
    if (this.installPromise) {
      return this.installPromise;
    }
    const installPromise = this.installLatestDownloadedUpdate();
    this.installPromise = installPromise;
    try {
      return await installPromise;
    } finally {
      if (this.installPromise === installPromise) {
        this.installPromise = null;
      }
    }
  }

  async installLatestDownloadedUpdate() {
    if (!this.enabled || this.state.status !== updateStatuses.downloaded) {
      return false;
    }
    if (!this.simulated) {
      if (this.downloadedRefreshPromise) {
        await this.downloadedRefreshPromise;
      } else {
        await this.refreshDownloadedUpdate(false);
      }
      if (this.state.status !== updateStatuses.downloaded) {
        return false;
      }
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
      await this.onInteractiveResult({ kind: "install-error", error: errorMessage(error) });
      return false;
    }
  }

  async refreshDownloadedUpdate(interactive = false) {
    if (this.downloadedRefreshPromise) {
      const refreshed = await this.downloadedRefreshPromise;
      if (interactive) {
        await this.reportCurrentUpdateState();
      }
      return refreshed;
    }
    if (!this.enabled || this.simulated || this.state.status !== updateStatuses.downloaded) {
      return false;
    }

    const fallback = this.getState();
    const previousAutoDownload = this.updater.autoDownload;
    this.downloadedRefreshFallback = fallback;
    this.downloadedRefreshInteractive = Boolean(interactive);
    this.updater.autoDownload = false;

    const refreshPromise = (async () => {
      try {
        const result = await this.updater.checkForUpdates();
        const version = cleanVersion(result?.updateInfo?.version);
        if (!result?.isUpdateAvailable || !isVersionGreater(version, fallback.version)) {
          this.clearDownloadedRefreshContext();
          if (interactive) {
            await this.onInteractiveResult({ kind: "downloaded", version: fallback.version });
          }
          return true;
        }

        this.setState({ status: updateStatuses.downloading, version, percent: 0 });
        if (interactive) {
          await this.onInteractiveResult({ kind: "downloading", version });
        }
        await this.updater.downloadUpdate(result.cancellationToken);
        return true;
      } catch (error) {
        if (this.downloadedRefreshFallback) {
          this.handleError(error);
        }
        return false;
      } finally {
        this.updater.autoDownload = previousAutoDownload;
      }
    })();

    this.downloadedRefreshPromise = refreshPromise;
    try {
      return await refreshPromise;
    } finally {
      if (this.downloadedRefreshPromise === refreshPromise) {
        this.downloadedRefreshPromise = null;
      }
    }
  }

  async reportCurrentUpdateState() {
    if (this.state.status === updateStatuses.downloaded) {
      await this.onInteractiveResult({ kind: "downloaded", version: this.state.version });
    } else {
      await this.onInteractiveResult({ kind: this.state.status, version: this.state.version });
    }
  }

  handleError(error) {
    this.onError(error);
    if (this.downloadedRefreshFallback) {
      const fallback = this.downloadedRefreshFallback;
      const interactive = this.downloadedRefreshInteractive;
      this.clearDownloadedRefreshContext();
      this.state = fallback;
      this.onStateChange(this.getState());
      if (interactive) {
        void this.onInteractiveResult({ kind: "error", error: errorMessage(error) });
      }
      return;
    }
    const installing = this.state.status === updateStatuses.installing;
    const interactive = this.takeInteractiveCheck() || this.takeInteractiveDownload();
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

  takeInteractiveDownload() {
    const interactive = this.interactiveDownload;
    this.interactiveDownload = false;
    return interactive;
  }

  clearDownloadedRefreshContext() {
    this.downloadedRefreshFallback = null;
    this.downloadedRefreshInteractive = false;
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

function isVersionGreater(candidate, current) {
  const next = semver.valid(cleanVersion(candidate));
  const previous = semver.valid(cleanVersion(current));
  if (next && previous) {
    return semver.gt(next, previous);
  }
  return Boolean(candidate && candidate !== current);
}

module.exports = { UpdateManager, updateStatuses };
