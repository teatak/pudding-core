const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { fileURLToPath, pathToFileURL } = require("node:url");

const webviewReadyTimeoutMS = 10_000;
const cdpCommandTimeoutMS = 8_000;
const navigationTimeoutMS = 15_000;
const screenshotMaxDimension = 16_384;
const screenshotMaxPixels = 32 * 1024 * 1024;
const screenshotMaxBytes = 64 * 1024 * 1024;
const maxTabsPerSession = 8;
const maxTabsTotal = 16;
const maxPopupWindowsTotal = 8;

class BrowserHost {
  constructor(onUpdate, onCursor, onAutomationStart, onWebviewRequired, onAutomationEnd, windowHooks) {
    this.slots = new Map();
    this.inputQueue = Promise.resolve();
    this.onUpdate = typeof onUpdate === "function" ? onUpdate : () => {};
    this.onCursor = typeof onCursor === "function" ? onCursor : () => {};
    this.onAutomationStart = typeof onAutomationStart === "function" ? onAutomationStart : () => {};
    this.onWebviewRequired = typeof onWebviewRequired === "function" ? onWebviewRequired : () => {};
    this.onAutomationEnd = typeof onAutomationEnd === "function" ? onAutomationEnd : () => {};
    this.popupWindowOptions = typeof windowHooks?.options === "function" ? windowHooks.options : () => ({});
    this.resolveFavicon = typeof windowHooks?.resolveFavicon === "function" ? windowHooks.resolveFavicon : async () => "";
    this.onPopupCreated = typeof windowHooks?.created === "function" ? windowHooks.created : () => {};
    this.onBlockedWindowNavigation = typeof windowHooks?.blockedNavigation === "function" ? windowHooks.blockedNavigation : () => false;
    this.boundPopupContents = new WeakSet();
    this.popupWindows = new Set();
  }

  async ensure(request) {
    const slot = this.ensureSlot(request);
    const url = normalizeURL(request.url, slot.fileRoots);
    if (String(request.url || "").trim() && !url) {
      throw navigationNotAllowedError(request.url);
    }
    await this.requireWebContents(slot);
    if (url && !browserURLIsBlank(url) && !sameNormalizedURL(slot.committedURL, url, slot.fileRoots)) {
      await this.runCommand(slot, () => this.navigate(slot, url));
    }
    return snapshot(slot);
  }

  async loadURL(request) {
    const slot = this.ensureSlot(request);
    const url = normalizeURL(request.url, slot.fileRoots);
    if (!url) {
      throw navigationNotAllowedError(request.url);
    }
    await this.requireWebContents(slot);
    if (sameNormalizedURL(slot.committedURL, url, slot.fileRoots)) {
      return snapshot(slot);
    }
    this.noteAutomationStart(slot, "open");
    await this.runCommand(slot, () => this.navigate(slot, url));
    return snapshot(slot);
  }

  async back(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    await this.requireWebContents(slot);
    this.noteAutomationStart(slot, "back");
    await this.runCommand(slot, async () => {
      const history = await this.navigationHistory(slot);
      if (history.currentIndex > 0) {
        const entry = history.entries[history.currentIndex - 1];
        await this.navigateHistory(slot, entry.id, entry.url);
      }
    });
    return snapshot(slot);
  }

  async forward(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    await this.requireWebContents(slot);
    this.noteAutomationStart(slot, "forward");
    await this.runCommand(slot, async () => {
      const history = await this.navigationHistory(slot);
      if (history.currentIndex + 1 < history.entries.length) {
        const entry = history.entries[history.currentIndex + 1];
        await this.navigateHistory(slot, entry.id, entry.url);
      }
    });
    return snapshot(slot);
  }

  async reload(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    await this.requireWebContents(slot);
    this.noteAutomationStart(slot, "reload");
    const reloadURL = normalizeURL(request.url, slot.fileRoots) || normalizeURL(slot.displayURL, slot.fileRoots);
    const actualURL = normalizeURL(slot.committedURL, slot.fileRoots);
    if (reloadURL && !sameNormalizedURL(reloadURL, actualURL, slot.fileRoots)) {
      await this.runCommand(slot, () => this.navigate(slot, reloadURL));
    } else {
      await this.runCommand(slot, () =>
        this.runNavigation(slot, () => this.sendCDP(slot, "Page.reload", { ignoreCache: false })),
      );
    }
    return snapshot(slot);
  }

  async observe(request) {
    const slot = this.requireLiveSlot(request);
    const maxText = clampInt(request.maxTextChars, 6000, 20000);
    const maxElements = clampInt(request.maxElements, 30, 100);
    const result = await this.runCommand(slot, () => evaluateJSON(slot, observeScript(maxText, maxElements)));
    return {
      tab: snapshot(slot),
      title: String(result.title || ""),
      url: String(result.url || ""),
      readyState: String(result.readyState || ""),
      text: String(result.text || ""),
      textChars: Math.max(0, Math.round(Number(result.textChars) || 0)),
      truncated: Boolean(result.truncated),
      elements: Array.isArray(result.elements) ? result.elements : [],
    };
  }

  async screenshot(request) {
    const slot = this.requireLiveSlot(request);
    const fullPage = Boolean(request.fullPage);
    const { viewport, dataBase64 } = await this.runCommand(slot, async () => ({
      viewport: await viewportMetrics(slot),
      dataBase64: await captureScreenshot(slot, fullPage),
    }));
    const buffer = Buffer.from(dataBase64, "base64");
    const size = imageSize(buffer);
    return {
      tab: snapshot(slot),
      mime: "image/png",
      dataBase64,
      size: buffer.length,
      width: size.width,
      height: size.height,
      viewportWidth: viewport.width,
      viewportHeight: viewport.height,
      deviceScaleFactor: viewport.deviceScaleFactor,
      capturedAt: new Date().toISOString(),
    };
  }

  async click(request) {
    const slot = this.requireLiveSlot(request);
    normalizeClickMethod(request.method);
    return this.runInputCommand(() =>
      this.runCommand(slot, async () => {
        this.noteAutomationStart(slot, "click");
        try {
          const target = await evaluateJSON(slot, clickTargetScript(request, "pointer"), { userGesture: true });
          this.noteUpdated(slot);
          this.noteCursor(slot, "click", target);
          return { tab: snapshot(slot), action: "click", result: target };
        } finally {
          this.noteAutomationEnd(slot, "click");
        }
      }),
    );
  }

  async type(request) {
    const slot = this.requireLiveSlot(request);
    if (!String(request.text || "")) {
      throw new Error("text is required");
    }
    return this.runInputCommand(() =>
      this.runCommand(slot, async () => {
        this.noteAutomationStart(slot, "type");
        try {
          const expectation = await evaluateJSON(slot, typePrepareScript(request));
          await evaluateJSON(slot, typeTargetInputScript(request));
          const typed = await evaluateJSON(slot, typeResultScript(request, "target", expectation));
          if (!typed.matchesExpected) {
            throw new Error("browser input did not produce the expected value");
          }
          delete typed.matchesExpected;
          this.noteUpdated(slot);
          this.noteCursor(slot, "type", typed);
          return { tab: snapshot(slot), action: "type", result: typed };
        } finally {
          this.noteAutomationEnd(slot, "type");
        }
      }),
    );
  }

  async scroll(request) {
    const slot = this.requireLiveSlot(request);
    if (!Number(request.deltaX) && !Number(request.deltaY)) {
      request.deltaY = 600;
    }
    return this.runInputCommand(() =>
      this.runCommand(slot, async () => {
        this.noteAutomationStart(slot, "scroll");
        try {
          const target = await evaluateJSON(slot, scrollTargetScript(request));
          const result = await waitForScrollResult(slot, request, target);
          this.noteUpdated(slot);
          this.noteCursor(slot, "scroll", result);
          return { tab: snapshot(slot), action: "scroll", result };
        } finally {
          this.noteAutomationEnd(slot, "scroll");
        }
      }),
    );
  }

  listTabs(request) {
    const sessionID = normalizeSessionID(request.sessionID);
    return {
      tabs: Array.from(this.slots.values())
        .filter((slot) => slotBelongsToSession(slot, sessionID))
        .map(snapshot),
      processMode: "webview",
    };
  }

  closeSession(request) {
    const sessionID = normalizeSessionID(request.sessionID);
    for (const slot of Array.from(this.slots.values())) {
      if (!slotBelongsToSession(slot, sessionID)) {
        continue;
      }
      const lost = lostSnapshot(slot);
      this.destroySlot(slot);
      this.onUpdate(lost);
    }
  }

  async revokeFileAccess(request) {
    const sessionID = normalizeSessionID(request.sessionID);
    const closedTabIDs = [];
    const slots = Array.from(this.slots.values()).filter((slot) => slotBelongsToSession(slot, sessionID));
    const decisions = slots.map((slot) => this.runCommand(slot, async () => {
      const hadFileGrant = slot.fileRoots.length > 0;
      let currentURL = String(slot.committedURL || slot.displayURL || "").trim();
      let metadataFailed = false;
      if (slot.webContents && !slot.webContents.isDestroyed()) {
        try {
          const metadata = await evaluateJSON(slot, `(() => JSON.stringify({url: String(location.href || "")}))()`);
          currentURL = String(metadata.url || currentURL).trim();
        } catch {
          metadataFailed = true;
        }
      }
      slot.fileRoots = [];
      return { slot, close: isFileURL(currentURL) || (hadFileGrant && metadataFailed) };
    }));
    for (const { slot, close } of await Promise.all(decisions)) {
      if (!close || this.slots.get(slot.key) !== slot) {
        continue;
      }
      const lost = lostSnapshot(slot);
      closedTabIDs.push(slot.tabID);
      this.destroySlot(slot);
      this.onUpdate(lost);
    }
    return { closedTabIDs };
  }

  closeTab(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      return lostSnapshotFromRequest(request);
    }
    const lost = lostSnapshot(slot);
    this.destroySlot(slot);
    this.onUpdate(lost);
    return lost;
  }

  closeAll() {
    for (const slot of Array.from(this.slots.values())) {
      this.destroySlot(slot);
    }
    for (const window of this.popupWindows) {
      if (!window.isDestroyed()) {
        window.destroy();
      }
    }
    this.popupWindows.clear();
  }

  destroySlot(slot) {
    slot.disposed = true;
    this.slots.delete(slot.key);
    clearInterval(slot.webviewRequestTimer);
    slot.webviewRequestTimer = null;
    rejectWebviewWaiters(slot, new Error("browser tab closed"));
    rejectNavigationWaiter(slot, new Error("browser tab closed"));
    if (slot.webContents && !slot.webContents.isDestroyed()) {
      detachDebugger(slot);
      slot.webContents.destroy();
    }
  }

  async registerWebContents(request, webContents) {
    if (!webContents || webContents.isDestroyed()) {
      throw new Error("browser webview target not found");
    }
    const key = slotKey(request);
    const sessionID = normalizeSessionID(request.sessionID);
    const tabID = normalizeTabID(request.tabID);
    const pending = this.slots.get(key) || null;
    const existing = pending || this.ensureSlot(request);
    acceptTrustedFileRoots(existing, request);
    const requestID = String(request.requestID || "");
    if (!existing.webContents && pending?.webviewRequestID && requestID !== pending.webviewRequestID) {
      throw new Error("stale browser webview registration");
    }
    if (existing?.webContents === webContents) {
      await this.ensureDebugger(existing);
      clearInterval(existing.webviewRequestTimer);
      existing.webviewRequestTimer = null;
      resolveWebviewWaiters(existing);
      if (request.loadError) {
        existing.displayTitle = existing.displayURL || "";
        existing.navigationError = normalizeLoadError(request.loadError);
        this.noteUpdated(existing);
      }
      return snapshot(existing);
    }
    const previousURL = existing?.displayURL || "";
    const previousCreatedAt = existing?.createdAt;
    if (existing.webContents && !existing.webContents.isDestroyed()) {
      throw new Error("browser webview already registered");
    }
    // The pending slot owns the latest desired URL. A delayed renderer
    // registration must not restore the stale URL from its event payload.
    const targetURL = normalizeURL(previousURL, existing.fileRoots) || normalizeURL(request.url, existing.fileRoots) || "about:blank";
    const loadError = normalizeLoadError(request.loadError);
    const now = new Date().toISOString();
    const slot = existing;
    Object.assign(slot, {
      key,
      sessionID,
      tabID,
      webContents,
      disposed: false,
      createdAt: previousCreatedAt || normalizeTimestamp(request.createdAt) || now,
      updatedAt: now,
      displayURL: targetURL,
      displayTitle: loadError ? targetURL : "",
      committedURL: "",
      committedTitle: "",
      faviconURL: "",
      faviconSourceURL: "",
      faviconResolveID: 0,
      navigationError: loadError,
      cdpAttached: false,
      cdpReady: null,
      mainFrameID: "",
      mainFrameLoaderID: "",
    });
    this.slots.set(key, slot);
    this.bindSlotEvents(slot);
    await this.ensureDebugger(slot);
    clearInterval(slot.webviewRequestTimer);
    slot.webviewRequestTimer = null;
    const hadWaiters = slot.webviewWaiters.size > 0;
    resolveWebviewWaiters(slot);
    if (!hadWaiters && !loadError && targetURL && !browserURLIsBlank(targetURL) && !sameNormalizedURL(targetURL, slot.committedURL, slot.fileRoots)) {
      await this.runCommand(slot, () => this.navigate(slot, targetURL));
    }
    await this.refreshHistory(slot);
    this.noteUpdated(slot);
    return snapshot(slot);
  }

  getSlot(request) {
    return this.slots.get(slotKey(request)) || null;
  }

  requireLiveSlot(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    if (!slot.webContents || slot.webContents.isDestroyed()) {
      throw new Error("browser tab destroyed");
    }
    return slot;
  }

  ensureSlot(request) {
    const key = slotKey(request);
    const existing = this.slots.get(key);
    if (existing) {
      acceptTrustedFileRoots(existing, request);
      const requestURL = normalizeURL(request.url, existing.fileRoots);
      if (!existing.webContents && requestURL) {
        existing.displayURL = requestURL;
      }
      return existing;
    }
    const sessionID = normalizeSessionID(request.sessionID);
    this.assertSlotCapacity(sessionID);
    const now = new Date().toISOString();
    const fileRoots = trustedFileRoots(request);
    const slot = {
      key,
      sessionID,
      tabID: normalizeTabID(request.tabID),
      webContents: null,
      disposed: false,
      version: 0,
      createdAt: normalizeTimestamp(request.createdAt) || now,
      updatedAt: now,
      displayURL: normalizeURL(request.url, fileRoots) || "about:blank",
      displayTitle: "",
      committedURL: "",
      committedTitle: "",
      faviconURL: "",
      faviconSourceURL: "",
      faviconResolveID: 0,
      fileRoots,
      navigationError: null,
      webviewWaiters: new Set(),
      webviewRequestID: "",
      webviewRequestTimer: null,
      cdpAttached: false,
      cdpReady: null,
      commandQueue: Promise.resolve(),
      mainFrameID: "",
      mainFrameLoaderID: "",
      navigationGeneration: 0,
      navigationWaiter: null,
      historyIndex: 0,
      historyEntries: [],
      activateOnCreate: true,
      pendingOpenNavigation: null,
    };
    slot.sendCDP = (method, params) => this.sendCDP(slot, method, params);
    this.slots.set(key, slot);
    this.requestWebview(slot);
    return slot;
  }

  assertSlotCapacity(sessionID) {
    if (this.slots.size >= maxTabsTotal) {
      throw new Error("browser tab limit reached");
    }
    let sessionTabs = 0;
    for (const slot of this.slots.values()) {
      if (slotBelongsToSession(slot, sessionID)) {
        sessionTabs += 1;
      }
    }
    if (sessionTabs >= maxTabsPerSession) {
      throw new Error("browser tab limit reached");
    }
  }

  requestWebview(slot) {
    if (slot.webContents || slot.disposed) {
      return;
    }
    if (!slot.webviewRequestID) {
      slot.webviewRequestID = `webview_${crypto.randomUUID().replaceAll("-", "")}`;
    }
    if (!slot.webviewRequestTimer) {
      const announce = () => {
        if (slot.webContents || slot.disposed) {
          clearInterval(slot.webviewRequestTimer);
          slot.webviewRequestTimer = null;
          return;
        }
        this.onWebviewRequired({
          requestID: slot.webviewRequestID,
          sessionID: slot.sessionID,
          tabID: slot.tabID,
          url: slot.displayURL || "about:blank",
          createdAt: slot.createdAt,
        });
      };
      queueMicrotask(announce);
      slot.webviewRequestTimer = setInterval(announce, 500);
      slot.webviewRequestTimer.unref?.();
    }
  }

  async requireWebContents(slot) {
    if (slot.webContents && !slot.webContents.isDestroyed()) {
      return slot.webContents;
    }
    this.requestWebview(slot);
    await waitForWebview(slot);
    if (!slot.webContents || slot.webContents.isDestroyed()) {
      throw new Error("browser_webview_not_ready");
    }
    return slot.webContents;
  }

  async ensureDebugger(slot) {
    await this.requireWebContents(slot);
    if (slot.cdpAttached && slot.webContents.debugger.isAttached()) {
      return;
    }
    if (!slot.cdpReady) {
      slot.cdpReady = Promise.resolve().then(async () => {
        const debug = slot.webContents.debugger;
        try {
          if (!debug.isAttached()) {
            debug.attach("1.3");
          }
          await withTimeout(debug.sendCommand("Page.enable"), cdpCommandTimeoutMS, "Page.enable timed out");
          await withTimeout(debug.sendCommand("Runtime.enable"), cdpCommandTimeoutMS, "Runtime.enable timed out");
          const frameTree = await withTimeout(debug.sendCommand("Page.getFrameTree"), cdpCommandTimeoutMS, "Page.getFrameTree timed out");
          slot.mainFrameID = String(frameTree?.frameTree?.frame?.id || "");
          slot.mainFrameLoaderID = String(frameTree?.frameTree?.frame?.loaderId || "");
          const frameURL = String(frameTree?.frameTree?.frame?.url || "");
          slot.committedURL = normalizeURL(frameURL, slot.fileRoots);
          if (frameURL && !slot.committedURL) {
            throw navigationNotAllowedError(frameURL);
          }
          if (browserURLIsBlank(slot.displayURL) && slot.committedURL) {
            slot.displayURL = slot.committedURL;
          }
          slot.cdpAttached = true;
        } catch (error) {
          detachDebugger(slot);
          throw error;
        }
      }).finally(() => {
        slot.cdpReady = null;
      });
    }
    return slot.cdpReady;
  }

  sendCDP(slot, method, params) {
    const execute = async () => {
      await this.ensureDebugger(slot);
      return withTimeout(
        slot.webContents.debugger.sendCommand(method, params || {}),
        cdpCommandTimeoutMS,
        `${method} timed out`,
      );
    };
    return execute();
  }

  runCommand(slot, operation) {
    const result = slot.commandQueue.then(operation, operation);
    slot.commandQueue = result.catch(() => undefined);
    return result;
  }

  runInputCommand(operation) {
    const result = this.inputQueue.then(operation, operation);
    this.inputQueue = result.catch(() => undefined);
    return result;
  }

  async refreshMetadata(slot) {
    const metadata = await evaluateJSON(slot, `(() => JSON.stringify({
      url: String(location.href || ""),
      title: String(document.title || ""),
      faviconURL: String(document.querySelector('link[rel~="icon"]')?.href || "")
    }))()`);
    const rawCommittedURL = String(metadata.url || "");
    const committedURL = normalizeURL(rawCommittedURL, slot.fileRoots);
    if (rawCommittedURL && !committedURL) {
      throw navigationNotAllowedError(rawCommittedURL);
    }
    if (committedURL) {
      slot.committedURL = committedURL;
      slot.displayURL = committedURL;
    }
    slot.committedTitle = String(metadata.title || "");
    slot.displayTitle = slot.committedTitle;
    const faviconURL = normalizeURL(metadata.faviconURL, slot.fileRoots);
    if (faviconURL) {
      this.updateFavicon(slot, [faviconURL]);
    } else if (!slot.faviconSourceURL) {
      slot.faviconURL = "";
    }
  }

  updateFavicon(slot, candidates) {
    const sourceURL = (Array.isArray(candidates) ? candidates : [candidates])
      .map((candidate) => normalizeURL(candidate, slot.fileRoots))
      .find(Boolean) || "";
    if (!sourceURL || (slot.faviconSourceURL === sourceURL && slot.faviconURL)) {
      return false;
    }
    slot.faviconSourceURL = sourceURL;
    slot.faviconURL = sourceURL;
    const resolveID = ++slot.faviconResolveID;
    const pageURL = normalizeURL(slot.committedURL, slot.fileRoots) || normalizeURL(slot.displayURL, slot.fileRoots);
    void Promise.resolve(this.resolveFavicon({ url: sourceURL, pageURL })).then((resolvedURL) => {
      const faviconURL = normalizeResolvedFavicon(resolvedURL);
      if (
        !faviconURL ||
        this.slots.get(slot.key) !== slot ||
        slot.disposed ||
        slot.faviconResolveID !== resolveID ||
        slot.faviconSourceURL !== sourceURL
      ) {
        return;
      }
      slot.faviconURL = faviconURL;
      this.noteUpdated(slot);
    }).catch(() => undefined);
    return true;
  }

  scheduleMetadataRefresh(slot) {
    void this.runCommand(slot, async () => {
      await this.refreshMetadata(slot);
      this.noteUpdated(slot);
    }).catch(() => undefined);
  }

  async navigate(slot, url) {
    const targetURL = normalizeURL(url, slot.fileRoots);
    if (!targetURL) {
      throw navigationNotAllowedError(url);
    }
    markSlotNavigationIntent(slot, targetURL);
    await this.runNavigation(slot, async () => {
      const result = await this.sendCDP(slot, "Page.navigate", takeNavigationParams(slot, targetURL));
      if (result?.errorText) {
        throw new Error(`browser navigation failed: ${result.errorText}`);
      }
      return result;
    }, targetURL);
  }

  async navigateHistory(slot, entryID, targetURL) {
    const normalizedTargetURL = normalizeURL(targetURL, slot.fileRoots);
    if (!normalizedTargetURL) {
      throw navigationNotAllowedError(targetURL);
    }
    await this.runNavigation(
      slot,
      () => this.sendCDP(slot, "Page.navigateToHistoryEntry", { entryId: entryID }),
      normalizedTargetURL,
    );
  }

  async runNavigation(slot, command, targetURL = "") {
    const generation = ++slot.navigationGeneration;
    rejectNavigationWaiter(slot, new Error("browser navigation superseded"));
    const committed = createNavigationWaiter(slot, generation, targetURL);
    try {
      const result = await command();
      const waiter = slot.navigationWaiter;
      if (waiter?.generation === generation) {
        waiter.commandComplete = true;
        waiter.loaderID = String(result?.loaderId || "");
        if (slot.lastMainFrameNavigation) {
          resolveNavigationWaiter(
            slot,
            slot.lastMainFrameNavigation.url,
            slot.lastMainFrameNavigation.loaderID,
            slot.lastMainFrameNavigation.sameDocument,
          );
        }
      }
      await withTimeout(committed, navigationTimeoutMS, "browser navigation timed out");
      await this.refreshHistory(slot);
      await this.refreshMetadata(slot);
    } catch (error) {
      rejectNavigationWaiter(slot, error);
      throw error;
    }
  }

  async navigationHistory(slot) {
    const history = await this.sendCDP(slot, "Page.getNavigationHistory");
    slot.historyIndex = Number(history?.currentIndex) || 0;
    slot.historyEntries = Array.isArray(history?.entries) ? history.entries : [];
    return { currentIndex: slot.historyIndex, entries: slot.historyEntries };
  }

  async refreshHistory(slot) {
    try {
      await this.navigationHistory(slot);
    } catch {
      // A snapshot can remain useful while a page is tearing down.
    }
  }

  bindSlotEvents(slot) {
    const webContents = slot.webContents;
    webContents.on("select-bluetooth-device", (event, _devices, callback) => {
      event.preventDefault();
      callback("");
    });
    webContents.setWindowOpenHandler((details) => this.handleWindowOpen(slot, details, webContents));
    webContents.on("did-create-window", (window, details) => this.bindPopupWindow(slot, window, details, webContents));
    webContents.on("will-navigate", (event, url) => {
      if (slot.webContents !== webContents || normalizeURL(url, slot.fileRoots)) {
        return;
      }
      event.preventDefault();
      const error = navigationNotAllowedError(url);
      slot.navigationError = { code: "ERR_ACCESS_DENIED", description: error.message };
      rejectNavigationWaiter(slot, error);
      this.noteUpdated(slot);
    });
    webContents.on("page-title-updated", () => {
      if (slot.webContents !== webContents) {
        return;
      }
      this.scheduleMetadataRefresh(slot);
    });
    webContents.on("page-favicon-updated", (_event, favicons) => {
      if (slot.webContents !== webContents || !this.updateFavicon(slot, favicons)) {
        return;
      }
      this.noteUpdated(slot);
    });
    webContents.on("did-fail-load", (_event, errorCode, errorDescription, validatedURL, isMainFrame) => {
      if (slot.webContents !== webContents || isMainFrame === false || isNavigationAbortCode(errorCode)) {
        return;
      }
      const failedURL = normalizeURL(validatedURL, slot.fileRoots) || normalizeURL(slot.displayURL, slot.fileRoots) || normalizeURL(slot.committedURL, slot.fileRoots);
      if (failedURL) {
        slot.displayURL = failedURL;
        slot.displayTitle = failedURL;
      }
      slot.navigationError = {
        code: String(errorDescription || errorCode || "ERR_FAILED"),
        description: String(errorDescription || ""),
      };
      rejectNavigationWaiter(slot, new Error(`browser navigation failed: ${errorDescription || errorCode || "ERR_FAILED"}`));
      this.noteUpdated(slot);
    });
    webContents.debugger.on("message", (_event, method, params) => {
      if (slot.webContents !== webContents || slot.disposed) {
        return;
      }
      if (method === "Page.frameNavigated" && !params?.frame?.parentId) {
        const nextURL = normalizeURL(params?.frame?.url, slot.fileRoots);
        if (nextURL) {
          slot.mainFrameID = String(params?.frame?.id || slot.mainFrameID || "");
          slot.mainFrameLoaderID = String(params?.frame?.loaderId || slot.mainFrameLoaderID || "");
          slot.lastMainFrameNavigation = {
            url: nextURL,
            loaderID: String(params?.frame?.loaderId || ""),
            sameDocument: false,
          };
          slot.committedURL = nextURL;
          slot.committedTitle = "";
          slot.faviconURL = "";
          slot.faviconSourceURL = "";
          slot.faviconResolveID += 1;
          slot.displayURL = nextURL;
          slot.displayTitle = "";
          slot.navigationError = null;
          resolveNavigationWaiter(slot, nextURL, slot.lastMainFrameNavigation.loaderID);
          this.noteUpdated(slot);
        }
      } else if (method === "Page.navigatedWithinDocument") {
        if (!slot.mainFrameID || String(params?.frameId || "") !== slot.mainFrameID) {
          return;
        }
        const nextURL = normalizeURL(params?.url, slot.fileRoots);
        if (nextURL) {
          slot.committedURL = nextURL;
          slot.displayURL = nextURL;
          slot.lastMainFrameNavigation = {
            url: nextURL,
            loaderID: slot.mainFrameLoaderID,
            sameDocument: true,
          };
          resolveNavigationWaiter(slot, nextURL, slot.mainFrameLoaderID, true);
          this.noteUpdated(slot);
        }
      } else if (method === "Page.loadEventFired") {
        this.scheduleMetadataRefresh(slot);
      }
    });
    webContents.debugger.on("detach", () => {
      if (slot.webContents === webContents) {
        slot.cdpAttached = false;
        slot.cdpReady = null;
        rejectNavigationWaiter(slot, new Error("cdp detached during browser navigation"));
      }
    });
    webContents.on("destroyed", () => {
      if (this.slots.get(slot.key) === slot && slot.webContents === webContents) {
        this.slots.delete(slot.key);
        rejectWebviewWaiters(slot, new Error("browser tab destroyed"));
        rejectNavigationWaiter(slot, new Error("browser tab destroyed"));
        if (!slot.disposed) this.onUpdate(lostSnapshot(slot));
      }
    });
  }

  handleWindowOpen(slot, details = {}, openerContents = slot.webContents) {
    const managedTargetURL = normalizeURL(details.url, slot.fileRoots);
    const targetURL = managedTargetURL || normalizeWindowURL(details.url, slot.fileRoots, openerContents?.getURL?.());
    if (!targetURL) {
      return { action: "deny" };
    }
    if (managedTargetURL && !browserURLIsBlank(managedTargetURL) && windowOpenUsesManagedTab(details)) {
      const next = this.ensureSlot({
        sessionID: slot.sessionID,
        tabID: newTabID(),
        url: managedTargetURL,
        _fileAuthorized: true,
        fileRoots: slot.fileRoots,
      });
      next.activateOnCreate = String(details.disposition || "") !== "background-tab";
      next.pendingOpenNavigation = windowOpenNavigation(details, managedTargetURL, slot.fileRoots);
      markSlotNavigationIntent(next, managedTargetURL);
      this.noteUpdated(next);
      return { action: "deny" };
    }
    if (this.popupWindows.size >= maxPopupWindowsTotal) {
      return { action: "deny" };
    }
    const applicationOptions = this.popupWindowOptions({
      sessionID: slot.sessionID,
      tabID: slot.tabID,
      details,
    }) || {};
    return {
      action: "allow",
      outlivesOpener: windowOpenHasFeature(details, "noopener") || windowOpenHasFeature(details, "noreferrer"),
      overrideBrowserWindowOptions: {
        show: true,
        autoHideMenuBar: true,
        ...applicationOptions,
        webPreferences: {
          ...(applicationOptions.webPreferences || {}),
          partition: "persist:pudding-default",
          contextIsolation: true,
          nodeIntegration: false,
          nodeIntegrationInWorker: false,
          nodeIntegrationInSubFrames: false,
          sandbox: true,
          webSecurity: true,
          allowRunningInsecureContent: false,
          webviewTag: false,
        },
      },
    };
  }

  bindPopupWindow(slot, window, details = {}, openerContents = slot.webContents) {
    const webContents = window?.webContents;
    if (!webContents || webContents.isDestroyed() || this.boundPopupContents.has(webContents)) {
      return;
    }
    this.boundPopupContents.add(webContents);
    this.popupWindows.add(window);
    window.once("closed", () => this.popupWindows.delete(window));
    webContents.on("select-bluetooth-device", (event, _devices, callback) => {
      event.preventDefault();
      callback("");
    });
    webContents.setWindowOpenHandler((nextDetails) => this.handleWindowOpen(slot, nextDetails, webContents));
    webContents.on("did-create-window", (childWindow, childDetails) => this.bindPopupWindow(slot, childWindow, childDetails, webContents));
    const guardNavigation = (event, url) => {
      if (
        normalizeWindowURL(url, slot.fileRoots, webContents.getURL?.()) ||
        normalizeWindowURL(url, slot.fileRoots, openerContents?.getURL?.())
      ) {
        return;
      }
      event.preventDefault();
      this.onBlockedWindowNavigation({
        sessionID: slot.sessionID,
        tabID: slot.tabID,
        url: String(url || ""),
        window,
      });
    };
    webContents.on("will-navigate", guardNavigation);
    webContents.on("will-redirect", guardNavigation);
    this.onPopupCreated(window, {
      sessionID: slot.sessionID,
      tabID: slot.tabID,
      details,
    });
  }

  noteUpdated(slot) {
    slot.version += 1;
    slot.updatedAt = new Date().toISOString();
    this.onUpdate(snapshot(slot));
  }

  noteAutomationStart(slot, action) {
    this.onAutomationStart({
      sessionID: slot.sessionID,
      tabID: slot.tabID,
      action,
      version: slot.version,
      createdAt: new Date().toISOString(),
    });
  }

  noteAutomationEnd(slot, action) {
    this.onAutomationEnd({
      sessionID: slot.sessionID,
      tabID: slot.tabID,
      action,
      version: slot.version,
      createdAt: new Date().toISOString(),
    });
  }

  noteCursor(slot, action, result) {
    const point = cursorPoint(result);
    if (!point) {
      return;
    }
    this.onCursor({
      sessionID: slot.sessionID,
      tabID: slot.tabID,
      action,
      x: point.x,
      y: point.y,
      version: slot.version,
      createdAt: new Date().toISOString(),
    });
  }

}

async function evaluateJSON(slot, script, options = {}) {
  const params = {
    expression: script,
    returnByValue: true,
    awaitPromise: true,
  };
  if (options.userGesture) params.userGesture = true;
  const evaluated = await slot.sendCDP("Runtime.evaluate", params);
  if (evaluated?.exceptionDetails) {
    throw new Error(evaluated.exceptionDetails.exception?.description || evaluated.exceptionDetails.text || "browser evaluation failed");
  }
  const raw = evaluated?.result?.value;
  if (typeof raw === "string") {
    return JSON.parse(raw);
  }
  return raw || {};
}

function markSlotNavigationIntent(slot, url) {
  slot.displayURL = url;
  slot.displayTitle = "";
  slot.navigationError = null;
  slot.version += 1;
  slot.updatedAt = new Date().toISOString();
}

function isNavigationAbortCode(errorCode) {
  return Number(errorCode) === -3;
}

function normalizeLoadError(error) {
  if (!error) {
    return null;
  }
  if (typeof error === "object") {
    return {
      code: String(error.code || error.errorCode || error.errorDescription || "ERR_FAILED"),
      description: String(error.description || error.errorDescription || error.message || ""),
    };
  }
  return {
    code: String(error),
    description: String(error),
  };
}

async function viewportMetrics(slot) {
  try {
    const [layout, scale] = await Promise.all([
      slot.sendCDP("Page.getLayoutMetrics"),
      evaluateJSON(slot, `(() => JSON.stringify({deviceScaleFactor: window.devicePixelRatio || 1}))()`),
    ]);
    return {
      width: Math.max(0, Math.round(Number(layout?.cssLayoutViewport?.clientWidth || layout?.layoutViewport?.clientWidth) || 0)),
      height: Math.max(0, Math.round(Number(layout?.cssLayoutViewport?.clientHeight || layout?.layoutViewport?.clientHeight) || 0)),
      deviceScaleFactor: Number(scale.deviceScaleFactor) || 1,
    };
  } catch {
    return { width: 0, height: 0, deviceScaleFactor: 1 };
  }
}

async function captureScreenshot(slot, fullPage) {
  const params = { format: "png", fromSurface: true, captureBeyondViewport: fullPage };
  if (fullPage) {
    const layout = await slot.sendCDP("Page.getLayoutMetrics");
    const size = layout?.cssContentSize || layout?.contentSize;
    const width = Math.ceil(Number(size?.width) || 0);
    const height = Math.ceil(Number(size?.height) || 0);
    if (width <= 0 || height <= 0) {
      throw new Error("screenshot content dimensions unavailable");
    }
    if (width > screenshotMaxDimension || height > screenshotMaxDimension || width * height > screenshotMaxPixels) {
      throw new Error(`screenshot dimensions exceed limit: ${width}x${height}`);
    }
    params.clip = { x: 0, y: 0, width, height, scale: 1 };
  }
  const result = await slot.sendCDP("Page.captureScreenshot", params);
  return assertPNGData(result?.data, "Page.captureScreenshot");
}

function withTimeout(promise, ms, message) {
  let timer = 0;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(message)), ms);
    }),
  ]).finally(() => {
    clearTimeout(timer);
  });
}

function assertPNGData(dataBase64, source) {
  const data = String(dataBase64 || "");
  if (!data) {
    throw new Error(`${source} returned empty image`);
  }
  if (data.length > Math.ceil(screenshotMaxBytes * 4 / 3) + 4) {
    throw new Error(`${source} returned image bytes above limit`);
  }
  const buffer = Buffer.from(data, "base64");
  if (buffer.length > screenshotMaxBytes) {
    throw new Error(`${source} returned image bytes above limit`);
  }
  const size = imageSize(buffer);
  if (!size.width || !size.height) {
    throw new Error(`${source} returned invalid image`);
  }
  if (size.width > screenshotMaxDimension || size.height > screenshotMaxDimension || size.width * size.height > screenshotMaxPixels) {
    throw new Error(`${source} returned image dimensions above limit: ${size.width}x${size.height}`);
  }
  return data;
}

function waitForWebview(slot) {
  return new Promise((resolve, reject) => {
    const waiter = { resolve, reject };
    slot.webviewWaiters.add(waiter);
    const timer = setTimeout(() => {
      slot.webviewWaiters.delete(waiter);
      reject(new Error("browser_webview_not_ready"));
    }, webviewReadyTimeoutMS);
    waiter.resolve = () => {
      clearTimeout(timer);
      resolve();
    };
    waiter.reject = (error) => {
      clearTimeout(timer);
      reject(error);
    };
  });
}

function resolveWebviewWaiters(slot) {
  for (const waiter of slot.webviewWaiters) waiter.resolve();
  slot.webviewWaiters.clear();
}

function rejectWebviewWaiters(slot, error) {
  for (const waiter of slot.webviewWaiters || []) waiter.reject(error);
  slot.webviewWaiters?.clear();
}

function createNavigationWaiter(slot, generation, targetURL) {
  slot.lastMainFrameNavigation = null;
  return new Promise((resolve, reject) => {
    slot.navigationWaiter = {
      generation,
      targetURL: normalizeURL(targetURL, slot.fileRoots),
      loaderID: "",
      previousLoaderID: slot.mainFrameLoaderID,
      commandComplete: false,
      resolve,
      reject,
    };
  });
}

function resolveNavigationWaiter(slot, url, loaderID = "", sameDocument = false) {
  const waiter = slot.navigationWaiter;
  if (!waiter || waiter.generation !== slot.navigationGeneration || !waiter.commandComplete) return;
  const committedURL = normalizeURL(url, slot.fileRoots);
  if (sameDocument) {
    if (!waiter.targetURL || !sameNormalizedURL(waiter.targetURL, committedURL, slot.fileRoots)) return;
  } else if (waiter.loaderID) {
    if (!loaderID || waiter.loaderID !== loaderID) return;
  } else {
    if (!loaderID || (waiter.previousLoaderID && waiter.previousLoaderID === loaderID)) return;
    if (waiter.targetURL && !sameNormalizedURL(waiter.targetURL, committedURL, slot.fileRoots)) return;
  }
  slot.navigationWaiter = null;
  waiter.resolve();
}

function rejectNavigationWaiter(slot, error) {
  const waiter = slot.navigationWaiter;
  if (!waiter) return;
  slot.navigationWaiter = null;
  waiter.reject(error);
}

function detachDebugger(slot) {
  try {
    if (slot.webContents?.debugger?.isAttached()) slot.webContents.debugger.detach();
  } catch {
    // The target may already be gone.
  }
  slot.cdpAttached = false;
  slot.cdpReady = null;
}

function cursorPoint(result) {
  if (!result || typeof result !== "object") {
    return null;
  }
  const rawX = result.cursorX ?? result.x;
  const rawY = result.cursorY ?? result.y;
  const x = Number(rawX);
  const y = Number(rawY);
  if (!Number.isFinite(x) || !Number.isFinite(y)) {
    return null;
  }
  return {
    x: Math.max(0, Math.round(x)),
    y: Math.max(0, Math.round(y)),
  };
}

function slotKey(request) {
  const sessionID = normalizeSessionID(request.sessionID);
  const tabID = normalizeTabID(request.tabID);
  return `${sessionID}:${tabID}`;
}

function normalizeSessionID(sessionID) {
  const value = String(sessionID || "").trim();
  if (!value) {
    throw new Error("browser session id missing");
  }
  return value;
}

function slotBelongsToSession(slot, sessionID) {
  return normalizeSessionID(slot.sessionID) === sessionID;
}

function normalizeTabID(tabID) {
  return String(tabID || "default").trim() || "default";
}

function normalizeTimestamp(value) {
  const timestamp = Date.parse(String(value || ""));
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : "";
}

function newTabID() {
  return `tab_${crypto.randomUUID().replaceAll("-", "")}`;
}

function windowOpenUsesManagedTab(details) {
  const disposition = String(details?.disposition || "").toLowerCase();
  if (disposition !== "foreground-tab" && disposition !== "background-tab") {
    return false;
  }
  if (details?.postBody) {
    return false;
  }
  const frameName = String(details?.frameName || "").trim().toLowerCase();
  if (frameName && frameName !== "_blank") {
    return false;
  }
  const features = String(details?.features || "").toLowerCase();
  return !/(^|,)\s*(popup|width|height|left|top)\s*(=|,|$)/.test(features);
}

function windowOpenHasFeature(details, name) {
  const expected = String(name || "").trim().toLowerCase();
  for (const rawFeature of String(details?.features || "").split(",")) {
    const [rawName, rawValue] = rawFeature.split("=", 2);
    if (rawName.trim().toLowerCase() !== expected) {
      continue;
    }
    const value = String(rawValue || "yes").trim().toLowerCase();
    return value !== "no" && value !== "false" && value !== "0";
  }
  return false;
}

function windowOpenNavigation(details, targetURL, fileRoots) {
  const rawReferrer = String(details?.referrer?.url || "").trim();
  const referrer = normalizeURL(rawReferrer, fileRoots);
  return {
    targetURL,
    referrer: referrer && !browserURLIsBlank(referrer) ? referrer : "",
    referrerPolicy: cdpReferrerPolicy(details?.referrer?.policy),
  };
}

function takeNavigationParams(slot, targetURL) {
  const params = { url: targetURL };
  const pending = slot.pendingOpenNavigation;
  slot.pendingOpenNavigation = null;
  if (!pending || !sameNormalizedURL(pending.targetURL, targetURL, slot.fileRoots)) {
    return params;
  }
  if (pending.referrer) {
    params.referrer = pending.referrer;
  }
  if (pending.referrerPolicy) {
    params.referrerPolicy = pending.referrerPolicy;
  }
  return params;
}

function cdpReferrerPolicy(policy) {
  const policies = {
    "no-referrer": "noReferrer",
    "no-referrer-when-downgrade": "noReferrerWhenDowngrade",
    origin: "origin",
    "origin-when-cross-origin": "originWhenCrossOrigin",
    "same-origin": "sameOrigin",
    "strict-origin": "strictOrigin",
    "strict-origin-when-cross-origin": "strictOriginWhenCrossOrigin",
    "unsafe-url": "unsafeUrl",
  };
  return policies[String(policy || "").trim().toLowerCase()] || "";
}

function normalizeClickMethod(method) {
  const value = String(method || "auto").trim().toLowerCase() || "auto";
  if (value === "auto" || value === "pointer") {
    return value;
  }
  throw new Error(`unsupported click method ${JSON.stringify(method)}`);
}

function clampInt(value, fallback, max) {
  const n = Math.round(Number(value) || 0);
  if (n <= 0) {
    return fallback;
  }
  return Math.min(n, max);
}

function normalizeURL(rawURL, fileRoots = []) {
  const value = String(rawURL || "").trim();
  if (!value) {
    return "";
  }
  if (value === "about:blank") {
    return value;
  }
  try {
    const url = new URL(value);
    if (url.protocol === "http:" || url.protocol === "https:" || url.toString() === "about:blank") {
      return url.toString();
    }
    if (url.protocol === "file:" && fileURLAllowed(url, fileRoots)) {
      const canonical = pathToFileURL(fs.realpathSync(fileURLToPath(url)));
      canonical.search = url.search;
      canonical.hash = url.hash;
      return canonical.toString();
    }
  } catch {
    return "";
  }
  return "";
}

function normalizeWindowURL(rawURL, fileRoots = [], openerURL = "") {
  if (!String(rawURL || "").trim()) {
    return "about:blank";
  }
  const normalized = normalizeURL(rawURL, fileRoots);
  if (normalized) {
    return normalized;
  }
  try {
    const target = new URL(String(rawURL || "").trim());
    const opener = new URL(String(openerURL || "").trim());
    if (target.protocol !== "blob:" || target.origin === "null" || target.origin !== opener.origin) {
      return "";
    }
    return target.toString();
  } catch {
    return "";
  }
}

function normalizeResolvedFavicon(rawURL) {
  const value = String(rawURL || "").trim();
  if (
    value.length > 100 * 1024 ||
    !/^data:image\/(?:avif|gif|jpeg|png|vnd\.microsoft\.icon|webp|x-icon);base64,[a-z0-9+/]+={0,2}$/i.test(value)
  ) {
    return "";
  }
  return value;
}

function browserURLIsBlank(rawURL) {
  const url = String(rawURL || "").trim().toLowerCase();
  return !url || url === "about:blank";
}

function isFileURL(rawURL) {
  try {
    return new URL(String(rawURL || "").trim()).protocol === "file:";
  } catch {
    return false;
  }
}

function sameNormalizedURL(left, right, fileRoots = []) {
  const leftURL = normalizeURL(left, fileRoots);
  const rightURL = normalizeURL(right, fileRoots);
  return Boolean(leftURL && rightURL && leftURL === rightURL);
}

function trustedFileRoots(request) {
  if (!request?._fileAuthorized) {
    return [];
  }
  const roots = Array.isArray(request.fileRoots) ? request.fileRoots : [request.fileRoot];
  const normalized = [];
  for (const root of roots) {
    const value = normalizeFileRoot(root);
    if (value && !normalized.includes(value)) {
      normalized.push(value);
    }
  }
  return normalized;
}

function acceptTrustedFileRoots(slot, request) {
  for (const root of trustedFileRoots(request)) {
    if (!slot.fileRoots.includes(root)) {
      slot.fileRoots.push(root);
    }
  }
}

function normalizeFileRoot(rawRoot) {
  const value = String(rawRoot || "").trim();
  if (!value) {
    return "";
  }
  try {
    const resolved = fs.realpathSync(value);
    return fs.statSync(resolved).isDirectory() ? resolved : "";
  } catch {
    return "";
  }
}

function fileURLAllowed(url, roots) {
  if (url.username || url.password || (url.hostname && url.hostname !== "localhost")) {
    return false;
  }
  let target;
  try {
    target = fs.realpathSync(fileURLToPath(url));
    if (!fs.statSync(target).isFile()) {
      return false;
    }
  } catch {
    return false;
  }
  return roots.some((root) => pathIsInside(target, root));
}

function pathIsInside(target, root) {
  const relative = path.relative(root, target);
  return relative === "" || (relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative));
}

function navigationNotAllowedError(rawURL) {
  const value = String(rawURL || "").trim();
  if (value.toLowerCase().startsWith("file:")) {
    return new Error("file URL is outside the session project");
  }
  return new Error("invalid browser url");
}

function observeScript(maxText, maxElements) {
  return `(() => {
  const maxText = ${JSON.stringify(maxText)};
  const maxElements = ${JSON.stringify(maxElements)};
  const pickText = (el) => ((el.innerText || el.value || el.getAttribute("aria-label") || el.textContent || "").trim().replace(/\\s+/g, " "));
  const visible = (el) => {
    const style = window.getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    return style && style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0;
  };
  const selectorFor = (el) => {
    if (!el || !el.tagName) return "";
    if (el.id) return "#" + CSS.escape(el.id);
    const name = el.getAttribute("name");
    if (name) return el.tagName.toLowerCase() + "[name=" + JSON.stringify(name) + "]";
    return el.tagName.toLowerCase();
  };
  const nodes = Array.from(document.querySelectorAll("a,button,input,textarea,select,[role=button],[contenteditable=true]"));
  const elements = [];
  for (const el of nodes) {
    if (elements.length >= maxElements) break;
    if (!visible(el)) continue;
    const text = pickText(el).slice(0, 160);
    const href = el.href || "";
    const ariaLabel = el.getAttribute("aria-label") || "";
    elements.push({
      index: elements.length,
      tag: el.tagName.toLowerCase(),
      text,
      href,
      role: el.getAttribute("role") || "",
      ariaLabel,
      selector: selectorFor(el),
      inputType: el.getAttribute("type") || "",
      disabled: Boolean(el.disabled || el.getAttribute("aria-disabled") === "true")
    });
  }
  const fullText = (document.body ? document.body.innerText : "").trim();
  const text = fullText.slice(0, maxText);
  return JSON.stringify({
    title: document.title,
    url: location.href,
    readyState: document.readyState,
    text,
    textChars: fullText.length,
    truncated: fullText.length > text.length,
    elements
  });
})()`;
}

function clickTargetScript(input, method) {
  const selector = JSON.stringify(String(input.selector || ""));
  const x = input.x === undefined || input.x === null ? "null" : JSON.stringify(Number(input.x));
  const y = input.y === undefined || input.y === null ? "null" : JSON.stringify(Number(input.y));
  const methodValue = JSON.stringify(method);
  return `(() => {
  const selector = ${selector};
  const x = ${x};
  const y = ${y};
  const method = ${methodValue};
  const elementText = (node) => String(node.innerText || node.textContent || node.value || node.getAttribute?.("aria-label") || "").trim();
  const isVisible = (node) => Boolean(node && (node.offsetWidth || node.offsetHeight || node.getClientRects().length));
  const resolveSelector = (rawSelector) => {
    if (!rawSelector) return null;
    const match = rawSelector.match(/^(.*):contains\\((["']?)(.*?)\\2\\)\\s*$/);
    if (match) {
      const base = match[1].trim() || "*";
      const needle = match[3];
      let candidates;
      try {
        candidates = Array.from(document.querySelectorAll(base));
      } catch (err) {
        throw new Error("invalid selector: " + rawSelector);
      }
      return candidates.find((node) => isVisible(node) && elementText(node).includes(needle)) ||
        candidates.find((node) => elementText(node).includes(needle)) ||
        null;
    }
    try {
      return document.querySelector(rawSelector);
    } catch (err) {
      throw new Error("invalid selector: " + rawSelector);
    }
  };
  let el = selector ? resolveSelector(selector) : null;
  if (!el && x !== null && y !== null) el = document.elementFromPoint(x, y);
  if (!el) throw new Error("target element not found");
  if (el.disabled || el.getAttribute?.("aria-disabled") === "true") throw new Error("target element is not interactable");
  if (selector) el.scrollIntoView({behavior: "instant", block: "center", inline: "center"});
  const rect = el.getBoundingClientRect();
  const visibleLeft = Math.max(0, rect.left);
  const visibleTop = Math.max(0, rect.top);
  const visibleRight = Math.min(window.innerWidth, rect.right);
  const visibleBottom = Math.min(window.innerHeight, rect.bottom);
  if (visibleRight <= visibleLeft || visibleBottom <= visibleTop) throw new Error("target element is not visible");
  const cx = selector ? visibleLeft + (visibleRight - visibleLeft) / 2 : x;
  const cy = selector ? visibleTop + (visibleBottom - visibleTop) / 2 : y;
  if (!Number.isFinite(cx) || !Number.isFinite(cy)) throw new Error("target coordinates not found");
  const hit = document.elementFromPoint(cx, cy);
  if (!hit || (hit !== el && !el.contains(hit))) throw new Error("target element is not hittable");
  if (typeof el.click === "function") {
    el.click();
  } else {
    el.dispatchEvent(new MouseEvent("click", {bubbles: true, cancelable: true, composed: true, clientX: cx, clientY: cy, view: window}));
  }
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), text: (el.innerText || el.value || "").trim().slice(0, 160), x: cx, y: cy, cursorX: cx, cursorY: cy, method});
})()`;
}

function typePrepareScript(input) {
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const text = ${JSON.stringify(String(input.text || ""))};
  const clear = ${JSON.stringify(Boolean(input.clear))};
  const textInputTypes = new Set(["text", "search", "email", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week"]);
  const isTextInput = (node) => node instanceof HTMLTextAreaElement ||
    (node instanceof HTMLInputElement && textInputTypes.has(String(node.type || "text").toLowerCase()));
  const isEditable = (node) => isTextInput(node) || Boolean(node?.isContentEditable);
  const fingerprint = (value) => {
    let hash = 2166136261;
    for (let index = 0; index < value.length; index += 1) {
      hash ^= value.charCodeAt(index);
      hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(16).padStart(8, "0");
  };
  let el = selector ? document.querySelector(selector) : document.activeElement;
  if (!el || el === document.body) throw new Error("target input not found");
  if (!isEditable(el)) throw new Error("target is not editable");
  if (el.disabled || el.readOnly || el.getAttribute("aria-disabled") === "true" || el.getAttribute("aria-readonly") === "true") {
    throw new Error("target is not editable");
  }
  el.scrollIntoView({behavior: "instant", block: "center", inline: "center"});
  el.focus();
  if (document.activeElement !== el) throw new Error("target input could not be focused");
  const originalValue = isTextInput(el) ? String(el.value || "") : String(el.textContent || "");
  const expectedValue = clear ? text : originalValue + text;
  if (isTextInput(el)) {
    try {
      if (typeof el.setSelectionRange === "function") {
        const end = String(el.value || "").length;
        el.setSelectionRange(clear ? 0 : end, end);
      }
    } catch (_) {}
  } else {
    const range = document.createRange();
    range.selectNodeContents(el);
    if (!clear) range.collapse(false);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  }
  const rect = el.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0 || rect.right <= 0 || rect.bottom <= 0 || rect.left >= window.innerWidth || rect.top >= window.innerHeight) {
    throw new Error("target input is not visible");
  }
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + Math.min(rect.height / 2, 18);
  return JSON.stringify({
    ok: true,
    tag: el.tagName.toLowerCase(),
    cursorX: cx,
    cursorY: cy,
    expectedValueLength: expectedValue.length,
    expectedValueHash: fingerprint(expectedValue)
  });
})()`;
}

function typeTargetInputScript(input) {
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const text = ${JSON.stringify(String(input.text || ""))};
  const clear = ${JSON.stringify(Boolean(input.clear))};
  const textInputTypes = new Set(["text", "search", "email", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week"]);
  const isTextInput = (node) => node instanceof HTMLTextAreaElement ||
    (node instanceof HTMLInputElement && textInputTypes.has(String(node.type || "text").toLowerCase()));
  const isEditable = (node) => isTextInput(node) || Boolean(node?.isContentEditable);
  const el = selector ? document.querySelector(selector) : document.activeElement;
  if (!el || el === document.body) throw new Error("target input not found");
  if (!isEditable(el)) throw new Error("target is not editable");
  if (document.activeElement !== el) el.focus();
  if (document.activeElement !== el) throw new Error("target input could not be focused");
  const inputEvent = (type, cancelable) => {
    let event;
    try {
      event = new InputEvent(type, {bubbles: true, cancelable, composed: true, inputType: "insertText", data: text});
    } catch (_) {
      event = new Event(type, {bubbles: true, cancelable, composed: true});
    }
    return event;
  };
  const accepted = el.dispatchEvent(inputEvent("beforeinput", true));
  if (!accepted) return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), canceled: true});
  const dispatchInput = () => el.dispatchEvent(inputEvent("input", false));
  if (isTextInput(el)) {
    const currentValue = String(el.value || "");
    const nextValue = clear ? text : currentValue + text;
    let proto = Object.getPrototypeOf(el);
    let setter = null;
    while (proto && !setter) {
      setter = Object.getOwnPropertyDescriptor(proto, "value")?.set || null;
      proto = Object.getPrototypeOf(proto);
    }
    if (!setter) throw new Error("target is not editable: native value setter missing");
    setter.call(el, nextValue);
    try {
      if (typeof el.setSelectionRange === "function") el.setSelectionRange(nextValue.length, nextValue.length);
    } catch (_) {}
    dispatchInput();
  } else if (el.isContentEditable) {
    let inserted = false;
    try {
      inserted = document.execCommand("insertText", false, text);
    } catch (_) {}
    if (!inserted) {
      const selection = window.getSelection();
      const range = selection?.rangeCount ? selection.getRangeAt(0) : document.createRange();
      if (!selection?.rangeCount) {
        range.selectNodeContents(el);
        if (!clear) range.collapse(false);
      }
      if (clear) range.selectNodeContents(el);
      range.deleteContents();
      const node = document.createTextNode(text);
      range.insertNode(node);
      range.setStartAfter(node);
      range.collapse(true);
      selection?.removeAllRanges();
      selection?.addRange(range);
      dispatchInput();
    }
  } else {
    throw new Error("target is not editable");
  }
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase()});
})()`;
}

function typeResultScript(input, method, expectation) {
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const expectedValueLength = ${JSON.stringify(Math.max(0, Math.round(Number(expectation?.expectedValueLength) || 0)))};
  const expectedValueHash = ${JSON.stringify(String(expectation?.expectedValueHash || ""))};
  const textInputTypes = new Set(["text", "search", "email", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week"]);
  const isTextInput = (node) => node instanceof HTMLTextAreaElement ||
    (node instanceof HTMLInputElement && textInputTypes.has(String(node.type || "text").toLowerCase()));
  const isEditable = (node) => isTextInput(node) || Boolean(node?.isContentEditable);
  const fingerprint = (value) => {
    let hash = 2166136261;
    for (let index = 0; index < value.length; index += 1) {
      hash ^= value.charCodeAt(index);
      hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(16).padStart(8, "0");
  };
  const el = selector ? document.querySelector(selector) : document.activeElement;
  if (!el || el === document.body) throw new Error("target input not found after typing");
  if (!isEditable(el)) throw new Error("target is not editable after typing");
  const rect = el.getBoundingClientRect();
  const value = isTextInput(el) ? String(el.value || "") : String(el.textContent || "");
  const matchesExpected = value.length === expectedValueLength && fingerprint(value) === expectedValueHash;
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), textLength: ${JSON.stringify(Array.from(String(input.text || "")).length)}, valueLength: value.length, matchesExpected, cursorX: rect.left + rect.width / 2, cursorY: rect.top + Math.min(rect.height / 2, 18), method: ${JSON.stringify(method)}});
})()`;
}

function scrollTargetScript(input) {
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const deltaX = ${JSON.stringify(Number(input.deltaX) || 0)};
  const deltaY = ${JSON.stringify(Number(input.deltaY) || 0)};
  const target = selector ? document.querySelector(selector) : window;
  if (!target) throw new Error("scroll target not found");
  let cursorX = window.innerWidth / 2;
  let cursorY = window.innerHeight / 2;
  if (target !== window) {
    const rect = target.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0 || rect.right <= 0 || rect.bottom <= 0 || rect.left >= window.innerWidth || rect.top >= window.innerHeight) {
      throw new Error("scroll target is outside the viewport");
    }
    cursorX = Math.max(0, Math.min(window.innerWidth - 1, rect.left + rect.width / 2));
    cursorY = Math.max(0, Math.min(window.innerHeight - 1, rect.top + rect.height / 2));
    const hit = document.elementFromPoint(cursorX, cursorY);
    if (!hit || (hit !== target && !target.contains(hit))) throw new Error("scroll target is not hittable");
  }
  const startX = target === window ? window.scrollX : target.scrollLeft;
  const startY = target === window ? window.scrollY : target.scrollTop;
  target.scrollBy({left: deltaX, top: deltaY, behavior: "instant"});
  return JSON.stringify({ok: true, x: cursorX, y: cursorY, cursorX, cursorY, startX, startY});
})()`;
}

function scrollResultScript(input, target) {
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const scrollTarget = selector ? document.querySelector(selector) : window;
  return JSON.stringify({
    ok: true,
    x: window.scrollX,
    y: window.scrollY,
    targetX: scrollTarget && scrollTarget !== window ? scrollTarget.scrollLeft : window.scrollX,
    targetY: scrollTarget && scrollTarget !== window ? scrollTarget.scrollTop : window.scrollY,
    cursorX: ${JSON.stringify(target.x)},
    cursorY: ${JSON.stringify(target.y)},
    method: "target"
  });
})()`;
}

async function waitForScrollResult(slot, input, target) {
  let latest = {};
  for (let attempt = 0; attempt < 10; attempt += 1) {
    latest = await evaluateJSON(slot, scrollResultScript(input, target));
    const movedX = Number(input.deltaX) !== 0 && Number(latest.targetX) !== Number(target.startX);
    const movedY = Number(input.deltaY) !== 0 && Number(latest.targetY) !== Number(target.startY);
    if (movedX || movedY || (!Number(input.deltaX) && !Number(input.deltaY)) || attempt === 9) {
      return latest;
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  return latest;
}

function imageSize(buffer) {
  if (
    buffer.length >= 24 &&
    buffer[0] === 0x89 &&
    buffer[1] === 0x50 &&
    buffer[2] === 0x4e &&
    buffer[3] === 0x47
  ) {
    return { width: buffer.readUInt32BE(16), height: buffer.readUInt32BE(20) };
  }
  return { width: 0, height: 0 };
}

function snapshot(slot) {
  const webContents = slot.webContents;
  const pending = !webContents;
  const destroyed = Boolean(webContents?.isDestroyed());
  const url = destroyed ? "" : normalizeURL(slot.committedURL, slot.fileRoots) || normalizeURL(slot.displayURL, slot.fileRoots) || "about:blank";
  const title = destroyed ? "" : slot.committedTitle || slot.displayTitle || (browserURLIsBlank(url) ? "" : url);
  return {
    sessionID: slot.sessionID,
    tabID: slot.tabID,
    status: destroyed ? "lost" : pending ? "pending" : "detached",
    url,
    title,
    faviconURL: destroyed ? "" : slot.faviconURL || "",
    canGoBack: pending || destroyed ? false : slot.historyIndex > 0,
    canGoForward: pending || destroyed ? false : slot.historyIndex + 1 < slot.historyEntries.length,
    profileID: "default",
    runtimeID: pending || destroyed ? "" : `webContents:${webContents.id}`,
    version: slot.version,
    activate: slot.activateOnCreate !== false,
    createdAt: slot.createdAt,
    updatedAt: slot.updatedAt,
    loadError: slot.navigationError
      ? {
          code: String(slot.navigationError.code || "ERR_FAILED"),
          description: String(slot.navigationError.description || ""),
        }
      : undefined,
  };
}

function lostSnapshot(slot) {
  return {
    sessionID: slot.sessionID,
    tabID: slot.tabID,
    status: "lost",
    url: "",
    title: "",
    canGoBack: false,
    canGoForward: false,
    profileID: "default",
    runtimeID: "",
    version: slot.version + 1,
    createdAt: slot.createdAt,
    updatedAt: new Date().toISOString(),
  };
}

function lostSnapshotFromRequest(request) {
  return {
    sessionID: String(request.sessionID || "").trim(),
    tabID: normalizeTabID(request.tabID),
    status: "lost",
    url: "",
    title: "",
    canGoBack: false,
    canGoForward: false,
    profileID: "default",
    runtimeID: "",
    version: 0,
    createdAt: "",
    updatedAt: new Date().toISOString(),
  };
}

module.exports = { BrowserHost };
