const { WebContentsView } = require("electron");

const browserPartition = "persist:pudding-default";

class BrowserHost {
  constructor(onUpdate, onCursor) {
    this.slots = new Map();
    this.onUpdate = typeof onUpdate === "function" ? onUpdate : () => {};
    this.onCursor = typeof onCursor === "function" ? onCursor : () => {};
    this.captureWebview = null;
  }

  setWebviewCaptureHandler(handler) {
    this.captureWebview = typeof handler === "function" ? handler : null;
  }

  async ensure(request) {
    const slot = this.ensureSlot(request);
    const url = normalizeURL(request.url);
    if (url && slot.webContents.getURL() !== url) {
      markSlotNavigationIntent(slot, url);
      startSlotURL(slot, url);
    }
    return snapshot(slot);
  }

  async loadURL(request) {
    const slot = this.ensureSlot(request);
    const url = normalizeURL(request.url);
    if (!url || slot.webContents.getURL() === url) {
      return snapshot(slot);
    }
    markSlotNavigationIntent(slot, url);
    startSlotURL(slot, url);
    return snapshot(slot);
  }

  async back(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    if (!slot.webContents.isDestroyed() && webContentsCanGoBack(slot.webContents)) {
      slot.webContents.goBack();
    }
    return snapshot(slot);
  }

  async forward(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    if (!slot.webContents.isDestroyed() && webContentsCanGoForward(slot.webContents)) {
      slot.webContents.goForward();
    }
    return snapshot(slot);
  }

  async reload(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    if (!slot.webContents.isDestroyed()) {
      const reloadURL = normalizeURL(request.url) || normalizeURL(slot.displayURL);
      const actualURL = normalizeURL(slot.webContents.getURL());
      if (reloadURL && !sameNormalizedURL(reloadURL, actualURL)) {
        try {
          await loadSlotURL(slot, reloadURL);
        } catch (error) {
          if (isNavigationAbort(error)) {
            throw error;
          }
          this.noteUpdated(slot);
        }
      } else {
        slot.webContents.reload();
      }
    }
    return snapshot(slot);
  }

  async observe(request) {
    const slot = this.requireLiveSlot(request);
    const maxText = clampInt(request.maxTextChars, 6000, 20000);
    const maxElements = clampInt(request.maxElements, 30, 100);
    const result = await evaluateJSON(slot, observeScript(maxText, maxElements));
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
    const viewport = await viewportMetrics(slot);
    const fullPage = Boolean(request.fullPage);
    let dataBase64 = "";
    if (this.captureWebview) {
      try {
        const result = await this.captureWebview({
          sessionID: slot.sessionID,
          tabID: slot.tabID,
          fullPage,
        });
        dataBase64 = assertPNGData(result?.dataBase64 || dataURLToBase64(result?.dataURL), "webview.capturePage");
      } catch (error) {
        throw new Error(`renderer capture failed: ${errorMessage(error)}`);
      }
    }
    if (!dataBase64) {
      dataBase64 = await captureScreenshot(slot, fullPage);
    }
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
    const method = normalizeClickMethod(request.method);
    let result;
    if (method === "dom") {
      result = await evaluateJSON(slot, clickScript(request, "dom"));
    } else {
      try {
        result = await evaluateJSON(slot, clickTargetScript(request, "pointer"));
        await dispatchMouseClick(slot, result.x, result.y);
      } catch (error) {
        if (method !== "auto") {
          throw error;
        }
        result = await evaluateJSON(slot, clickScript(request, "dom"));
      }
    }
    this.noteUpdated(slot);
    this.noteCursor(slot, "click", result);
    return { tab: snapshot(slot), action: "click", result };
  }

  async type(request) {
    const slot = this.requireLiveSlot(request);
    if (!String(request.text || "")) {
      throw new Error("text is required");
    }
    const result = await evaluateJSON(slot, typeScript(request));
    this.noteUpdated(slot);
    this.noteCursor(slot, "type", result);
    return { tab: snapshot(slot), action: "type", result };
  }

  async scroll(request) {
    const slot = this.requireLiveSlot(request);
    if (!Number(request.deltaX) && !Number(request.deltaY)) {
      request.deltaY = 600;
    }
    const result = await evaluateJSON(slot, scrollScript(request));
    this.noteUpdated(slot);
    this.noteCursor(slot, "scroll", result);
    return { tab: snapshot(slot), action: "scroll", result };
  }

  listTabs(request) {
    const sessionID = normalizeSessionID(request.sessionID);
    return {
      tabs: Array.from(this.slots.values())
        .filter((slot) => slotBelongsToSession(slot, sessionID))
        .map(snapshot),
      processMode: "headless",
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

  destroySlot(slot) {
    slot.disposed = true;
    this.slots.delete(slot.key);
    if (!slot.webContents.isDestroyed()) {
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
    const existing = this.slots.get(key);
    if (existing?.webContents === webContents) {
      const requestedURL = normalizeURL(request.url);
      if (requestedURL) {
        existing.displayURL = requestedURL;
      }
      if (request.loadError) {
        existing.displayTitle = requestedURL || existing.displayURL || "";
        existing.navigationError = normalizeLoadError(request.loadError);
        this.noteUpdated(existing);
      }
      return snapshot(existing);
    }
    const previousURL = existing && !existing.webContents.isDestroyed() ? existing.displayURL || existing.webContents.getURL() : "";
    if (existing) {
      this.destroySlot(existing);
    }
    const targetURL = normalizeURL(request.url) || normalizeURL(previousURL) || normalizeURL(webContents.getURL()) || "about:blank";
    const loadError = normalizeLoadError(request.loadError);
    const slot = {
      key,
      sessionID,
      tabID,
      headlessView: null,
      webContents,
      disposed: false,
      version: 0,
      displayURL: targetURL,
      displayTitle: loadError ? targetURL : "",
      navigationError: loadError,
    };
    this.bindSlotEvents(slot);
    this.slots.set(key, slot);
    if (!loadError && targetURL && targetURL !== webContents.getURL()) {
      await loadSlotURL(slot, targetURL);
    }
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
    if (slot.webContents.isDestroyed()) {
      throw new Error("browser tab destroyed");
    }
    return slot;
  }

  ensureSlot(request) {
    const key = slotKey(request);
    const existing = this.slots.get(key);
    if (existing && !existing.webContents.isDestroyed()) {
      return existing;
    }
    // Only used as an invisible tool target before the renderer webview registers.
    const headlessView = new WebContentsView({
      webPreferences: {
        partition: browserPartition,
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
      },
    });
    const slot = {
      key,
      sessionID: normalizeSessionID(request.sessionID),
      tabID: normalizeTabID(request.tabID),
      headlessView,
      webContents: headlessView.webContents,
      disposed: false,
      version: 0,
      displayURL: normalizeURL(request.url) || "about:blank",
      displayTitle: "",
      navigationError: null,
    };
    this.bindSlotEvents(slot);
    this.slots.set(key, slot);
    return slot;
  }

  bindSlotEvents(slot) {
    slot.webContents.setWindowOpenHandler(({ url }) => {
      void loadSlotURL(slot, normalizeURL(url) || "about:blank").catch(() => undefined);
      return { action: "deny" };
    });
    slot.webContents.on("did-start-navigation", (_event, url, _isInPlace, isMainFrame) => {
      if (isMainFrame === false) {
        return;
      }
      const nextURL = normalizeURL(url);
      if (!nextURL) {
        return;
      }
      slot.displayURL = nextURL;
      slot.displayTitle = "";
      slot.navigationError = null;
      this.noteUpdated(slot);
    });
    slot.webContents.on("did-navigate", (_event, url) => {
      const nextURL = normalizeURL(url) || normalizeURL(slot.webContents.getURL());
      if (nextURL) {
        slot.displayURL = nextURL;
        slot.displayTitle = "";
        slot.navigationError = null;
      }
      this.noteUpdated(slot);
    });
    slot.webContents.on("did-navigate-in-page", (_event, url) => {
      const nextURL = normalizeURL(url) || normalizeURL(slot.webContents.getURL());
      if (nextURL) {
        slot.displayURL = nextURL;
        slot.navigationError = null;
      }
      this.noteUpdated(slot);
    });
    slot.webContents.on("page-title-updated", () => {
      if (sameNormalizedURL(slot.displayURL, slot.webContents.getURL())) {
        slot.displayTitle = slot.webContents.getTitle();
      }
      this.noteUpdated(slot);
    });
    slot.webContents.on("did-fail-load", (_event, errorCode, errorDescription, validatedURL, isMainFrame) => {
      if (isMainFrame === false || isNavigationAbortCode(errorCode)) {
        return;
      }
      const failedURL = normalizeURL(validatedURL) || normalizeURL(slot.displayURL) || normalizeURL(slot.webContents.getURL());
      if (failedURL) {
        slot.displayURL = failedURL;
        slot.displayTitle = failedURL;
      }
      slot.navigationError = {
        code: String(errorDescription || errorCode || "ERR_FAILED"),
        description: String(errorDescription || ""),
      };
      this.noteUpdated(slot);
    });
    slot.webContents.on("destroyed", () => {
      if (this.slots.get(slot.key) === slot) {
        this.slots.delete(slot.key);
        if (!slot.disposed && !slot.headlessView) {
          this.restoreHeadlessSlot(slot);
        }
      }
    });
  }

  noteUpdated(slot) {
    slot.version += 1;
    this.onUpdate(snapshot(slot));
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

  restoreHeadlessSlot(previous) {
    const url = normalizeURL(previous.displayURL) || "about:blank";
    const slot = this.ensureSlot({
      sessionID: previous.sessionID,
      tabID: previous.tabID,
      url,
    });
    slot.version = previous.version + 1;
    slot.displayURL = url;
    slot.displayTitle = browserURLIsBlank(url) ? "" : previous.displayTitle || url;
    slot.navigationError = previous.navigationError || null;
    this.onUpdate(snapshot(slot));
    if (!browserURLIsBlank(url)) {
      startSlotURL(slot, url);
    }
  }
}

async function evaluateJSON(slot, script) {
  let raw;
  try {
    const evaluated = await withDebugger(slot, (send) =>
      send("Runtime.evaluate", {
        expression: script,
        returnByValue: true,
        awaitPromise: true,
      }),
    );
    raw = evaluated?.result?.value;
  } catch {
    raw = await slot.webContents.executeJavaScript(script, true);
  }
  if (typeof raw === "string") {
    return JSON.parse(raw);
  }
  return raw || {};
}

async function loadSlotURL(slot, url) {
  const targetURL = normalizeURL(url);
  if (targetURL) {
    slot.displayURL = targetURL;
    slot.displayTitle = "";
    slot.navigationError = null;
  }
  try {
    await slot.webContents.loadURL(url);
  } catch (error) {
    if (!isNavigationAbort(error)) {
      if (targetURL) {
        slot.displayURL = targetURL;
        slot.displayTitle = targetURL;
      }
      slot.navigationError = normalizeLoadError(error);
      return;
    }
    await waitForNavigationToSettle(slot.webContents);
  }
}

function startSlotURL(slot, url) {
  void loadSlotURL(slot, url).catch(() => undefined);
}

function markSlotNavigationIntent(slot, url) {
  slot.displayURL = url;
  slot.displayTitle = "";
  slot.navigationError = null;
  slot.version += 1;
}

function isNavigationAbort(error) {
  const message = String(error?.message || error || "");
  return message.includes("ERR_ABORTED") || message.includes("(-3)");
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

function waitForNavigationToSettle(webContents) {
  if (!webContents || webContents.isDestroyed()) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    let done = false;
    let timer = 0;
    const finish = () => {
      if (done) {
        return;
      }
      done = true;
      clearTimeout(timer);
      webContents.removeListener("did-stop-loading", finish);
      webContents.removeListener("did-finish-load", finish);
      webContents.removeListener("destroyed", finish);
      resolve();
    };
    webContents.once("did-stop-loading", finish);
    webContents.once("did-finish-load", finish);
    webContents.once("destroyed", finish);
    timer = setTimeout(finish, 500);
  });
}

async function viewportMetrics(slot) {
  try {
    const metrics = await evaluateJSON(
      slot,
      `(() => JSON.stringify({width: window.innerWidth, height: window.innerHeight, deviceScaleFactor: window.devicePixelRatio || 1}))()`,
    );
    return {
      width: Math.max(0, Math.round(Number(metrics.width) || 0)),
      height: Math.max(0, Math.round(Number(metrics.height) || 0)),
      deviceScaleFactor: Number(metrics.deviceScaleFactor) || 1,
    };
  } catch {
    return { width: 0, height: 0, deviceScaleFactor: 1 };
  }
}

async function captureScreenshot(slot, fullPage) {
  const errors = [];
  if (!fullPage) {
    try {
      return await capturePagePNG(slot);
    } catch (error) {
      errors.push(errorMessage(error));
    }
  }

  for (const fromSurface of [false, true]) {
    try {
      return await captureCDPScreenshot(slot, fullPage, fromSurface);
    } catch (error) {
      errors.push(errorMessage(error));
    }
  }

  try {
    return await capturePagePNG(slot);
  } catch (error) {
    errors.push(errorMessage(error));
  }

  throw new Error(`screenshot failed: ${errors.filter(Boolean).join("; ")}`);
}

async function captureCDPScreenshot(slot, fullPage, fromSurface) {
  const data = await withDebugger(slot, (send) =>
    send("Page.captureScreenshot", {
      format: "png",
      fromSurface,
      captureBeyondViewport: fullPage,
    }).then((result) => result.data),
  );
  return assertPNGData(data, `Page.captureScreenshot fromSurface=${fromSurface}`);
}

async function withDebugger(slot, fn) {
  const debug = slot.webContents.debugger;
  const wasAttached = debug.isAttached();
  if (!wasAttached) {
    debug.attach("1.3");
  }
  try {
    return await fn((method, params) =>
      withTimeout(debug.sendCommand(method, params || {}), 5000, `${method} timed out`),
    );
  } finally {
    if (!wasAttached && debug.isAttached()) {
      debug.detach();
    }
  }
}

async function capturePagePNG(slot) {
  const viewport = await viewportMetrics(slot);
  const rect =
    viewport.width > 0 && viewport.height > 0
      ? { x: 0, y: 0, width: viewport.width, height: viewport.height }
      : undefined;
  const image = await withTimeout(slot.webContents.capturePage(rect), 5000, "capturePage timed out");
  if (!image || typeof image.toPNG !== "function" || image.isEmpty?.()) {
    throw new Error("capturePage returned empty image");
  }
  return assertPNGData(image.toPNG().toString("base64"), "capturePage");
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
  const size = imageSize(Buffer.from(data, "base64"));
  if (!size.width || !size.height) {
    throw new Error(`${source} returned invalid image`);
  }
  return data;
}

function dataURLToBase64(dataURL) {
  const value = String(dataURL || "");
  const marker = "base64,";
  const index = value.indexOf(marker);
  return index >= 0 ? value.slice(index + marker.length) : "";
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error || "");
}

async function dispatchMouseClick(slot, x, y) {
  const point = {
    x: Math.max(0, Math.round(Number(x) || 0)),
    y: Math.max(0, Math.round(Number(y) || 0)),
  };
  try {
    await withDebugger(slot, async (send) => {
      for (const event of [
        { type: "mouseMoved", ...point, button: "none", buttons: 0, clickCount: 0, pointerType: "mouse" },
        { type: "mousePressed", ...point, button: "left", buttons: 1, clickCount: 1, pointerType: "mouse" },
        { type: "mouseReleased", ...point, button: "left", buttons: 0, clickCount: 1, pointerType: "mouse" },
      ]) {
        await send("Input.dispatchMouseEvent", event);
      }
    });
    return;
  } catch {
    slot.webContents.focus();
    slot.webContents.sendInputEvent({ type: "mouseMove", ...point, movementX: 0, movementY: 0 });
    slot.webContents.sendInputEvent({ type: "mouseDown", ...point, button: "left", clickCount: 1 });
    slot.webContents.sendInputEvent({ type: "mouseUp", ...point, button: "left", clickCount: 1 });
  }
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

function normalizeClickMethod(method) {
  const value = String(method || "auto").trim().toLowerCase() || "auto";
  if (value === "auto" || value === "pointer" || value === "dom") {
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

function normalizeURL(rawURL) {
  const value = String(rawURL || "").trim();
  if (!value) {
    return "";
  }
  if (value === "about:blank") {
    return value;
  }
  try {
    const url = new URL(value);
    if (url.protocol === "http:" || url.protocol === "https:" || url.protocol === "about:") {
      return url.toString();
    }
  } catch {
    return "";
  }
  return "";
}

function browserURLIsBlank(rawURL) {
  const url = String(rawURL || "").trim().toLowerCase();
  return !url || url === "about:blank";
}

function sameNormalizedURL(left, right) {
  const leftURL = normalizeURL(left);
  const rightURL = normalizeURL(right);
  return Boolean(leftURL && rightURL && leftURL === rightURL);
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
  return clickResolveScript(input, method, false);
}

function clickScript(input, method) {
  return clickResolveScript(input, method, true);
}

function clickResolveScript(input, method, useDOMClick) {
  const selector = JSON.stringify(String(input.selector || ""));
  const x = input.x === undefined || input.x === null ? "null" : JSON.stringify(Number(input.x));
  const y = input.y === undefined || input.y === null ? "null" : JSON.stringify(Number(input.y));
  const methodValue = JSON.stringify(method);
  const clickLine = useDOMClick ? "el.click();" : "";
  const centerX = useDOMClick ? "rect.left + rect.width / 2" : "selector ? rect.left + rect.width / 2 : x";
  const centerY = useDOMClick ? "rect.top + rect.height / 2" : "selector ? rect.top + rect.height / 2 : y";
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
  if (selector || ${JSON.stringify(useDOMClick)}) el.scrollIntoView({block: "center", inline: "center"});
  const rect = el.getBoundingClientRect();
  const cx = ${centerX};
  const cy = ${centerY};
  if (cx === null || cy === null) throw new Error("target coordinates not found");
  ${clickLine}
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), text: (el.innerText || el.value || "").trim().slice(0, 160), x: cx, y: cy, cursorX: cx, cursorY: cy, method});
})()`;
}

function typeScript(input) {
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const text = ${JSON.stringify(String(input.text || ""))};
  const clear = ${JSON.stringify(Boolean(input.clear))};
  let el = selector ? document.querySelector(selector) : document.activeElement;
  if (!el || el === document.body) throw new Error("target input not found");
  el.scrollIntoView({block: "center", inline: "center"});
  el.focus();
  if ("value" in el) {
    el.value = clear ? text : String(el.value || "") + text;
    el.dispatchEvent(new InputEvent("input", {bubbles: true, inputType: "insertText", data: text}));
    el.dispatchEvent(new Event("change", {bubbles: true}));
  } else if (el.isContentEditable) {
    if (clear) el.textContent = "";
    el.textContent = String(el.textContent || "") + text;
    el.dispatchEvent(new InputEvent("input", {bubbles: true, inputType: "insertText", data: text}));
  } else {
    throw new Error("target is not editable");
  }
  const rect = el.getBoundingClientRect();
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + Math.min(rect.height / 2, 18);
  return JSON.stringify({ok: true, tag: el.tagName.toLowerCase(), textLength: text.length, cursorX: cx, cursorY: cy});
})()`;
}

function scrollScript(input) {
  const dx = Number(input.deltaX) || 0;
  const dy = Number(input.deltaY) || 0;
  return `(() => {
  const selector = ${JSON.stringify(String(input.selector || ""))};
  const dx = ${JSON.stringify(dx)};
  const dy = ${JSON.stringify(dy)};
  const target = selector ? document.querySelector(selector) : window;
  if (!target) throw new Error("scroll target not found");
  let cursorX = window.innerWidth / 2;
  let cursorY = window.innerHeight / 2;
  if (target !== window) {
    const rect = target.getBoundingClientRect();
    cursorX = rect.left + rect.width / 2;
    cursorY = rect.top + rect.height / 2;
  }
  if (target === window) window.scrollBy(dx, dy);
  else target.scrollBy(dx, dy);
  return JSON.stringify({ok: true, x: window.scrollX, y: window.scrollY, cursorX, cursorY});
})()`;
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
  const destroyed = webContents.isDestroyed();
  const actualURL = destroyed ? "" : webContents.getURL();
  const url = destroyed ? "" : normalizeURL(slot.displayURL) || actualURL;
  const actualTitle = destroyed ? "" : webContents.getTitle();
  const title = destroyed ? "" : sameNormalizedURL(url, actualURL) ? slot.displayTitle || actualTitle : slot.displayTitle || url;
  return {
    sessionID: slot.sessionID,
    tabID: slot.tabID,
    status: destroyed ? "lost" : "detached",
    url,
    title,
    canGoBack: destroyed ? false : webContentsCanGoBack(webContents),
    canGoForward: destroyed ? false : webContentsCanGoForward(webContents),
    profileID: "default",
    runtimeID: destroyed ? "" : `webContents:${webContents.id}`,
    version: slot.version,
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
  };
}

function webContentsCanGoBack(webContents) {
  if (webContents.navigationHistory?.canGoBack) {
    return Boolean(webContents.navigationHistory.canGoBack());
  }
  return Boolean(webContents.canGoBack?.());
}

function webContentsCanGoForward(webContents) {
  if (webContents.navigationHistory?.canGoForward) {
    return Boolean(webContents.navigationHistory.canGoForward());
  }
  return Boolean(webContents.canGoForward?.());
}

module.exports = { BrowserHost };
