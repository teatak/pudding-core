const { WebContentsView } = require("electron");

const browserPartition = "persist:pudding-default";

class BrowserHost {
  constructor(onUpdate) {
    this.slots = new Map();
    this.onUpdate = typeof onUpdate === "function" ? onUpdate : () => {};
  }

  async ensure(request) {
    const slot = this.ensureSlot(request);
    const url = normalizeURL(request.url);
    if (url && slot.webContents.getURL() !== url) {
      await slot.webContents.loadURL(url);
    }
    return snapshot(slot);
  }

  async attach(window, request) {
    const slot = this.ensureSlot(request);
    const bounds = normalizeBounds(request.bounds);
    if (slot.attachedWindow && slot.attachedWindow !== window) {
      detachSlot(slot);
    }
    this.detachOtherSlots(window, slot);
    if (!slot.attachedWindow) {
      window.contentView.addChildView(slot.view);
      slot.attachedWindow = window;
    }
    slot.view.setBounds(bounds);
    return snapshot(slot);
  }

  updateBounds(window, request) {
    const slot = this.getSlot(request);
    if (!slot || slot.attachedWindow !== window) {
      return null;
    }
    slot.view.setBounds(normalizeBounds(request.bounds));
    return snapshot(slot);
  }

  detach(window, request) {
    const slot = this.getSlot(request);
    if (!slot || slot.attachedWindow !== window) {
      return null;
    }
    detachSlot(slot);
    return snapshot(slot);
  }

  detachWindow(window) {
    for (const slot of this.slots.values()) {
      if (slot.attachedWindow === window) {
        detachSlot(slot);
      }
    }
  }

  detachOtherSlots(window, activeSlot) {
    for (const slot of this.slots.values()) {
      if (slot !== activeSlot && slot.attachedWindow === window) {
        detachSlot(slot);
      }
    }
  }

  async loadURL(request) {
    const slot = this.ensureSlot(request);
    const url = normalizeURL(request.url);
    if (!url || slot.webContents.getURL() === url) {
      return snapshot(slot);
    }
    await slot.webContents.loadURL(url);
    return snapshot(slot);
  }

  async back(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    if (!slot.webContents.isDestroyed() && slot.webContents.canGoBack()) {
      slot.webContents.goBack();
    }
    return snapshot(slot);
  }

  async forward(request) {
    const slot = this.getSlot(request);
    if (!slot) {
      throw new Error("browser tab not found");
    }
    if (!slot.webContents.isDestroyed() && slot.webContents.canGoForward()) {
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
      slot.webContents.reload();
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
    const dataBase64 = await captureScreenshot(slot, Boolean(request.fullPage));
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
    return { tab: snapshot(slot), action: "click", result };
  }

  async type(request) {
    const slot = this.requireLiveSlot(request);
    if (!String(request.text || "")) {
      throw new Error("text is required");
    }
    const result = await evaluateJSON(slot, typeScript(request));
    this.noteUpdated(slot);
    return { tab: snapshot(slot), action: "type", result };
  }

  async scroll(request) {
    const slot = this.requireLiveSlot(request);
    if (!Number(request.deltaX) && !Number(request.deltaY)) {
      request.deltaY = 600;
    }
    const result = await evaluateJSON(slot, scrollScript(request));
    this.noteUpdated(slot);
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
    detachSlot(slot);
    this.slots.delete(slot.key);
    if (!slot.webContents.isDestroyed()) {
      slot.webContents.destroy();
    }
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
    const view = new WebContentsView({
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
      view,
      webContents: view.webContents,
      attachedWindow: null,
      version: 0,
    };
    view.webContents.setWindowOpenHandler(({ url }) => {
      void view.webContents.loadURL(normalizeURL(url) || "about:blank");
      return { action: "deny" };
    });
    view.webContents.on("did-navigate", () => {
      this.noteUpdated(slot);
    });
    view.webContents.on("did-navigate-in-page", () => {
      this.noteUpdated(slot);
    });
    view.webContents.on("page-title-updated", () => {
      this.noteUpdated(slot);
    });
    view.webContents.on("destroyed", () => {
      if (this.slots.get(key) === slot) {
        this.slots.delete(key);
      }
    });
    this.slots.set(key, slot);
    return slot;
  }

  noteUpdated(slot) {
    slot.version += 1;
    this.onUpdate(snapshot(slot));
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
  try {
    return await withDebugger(slot, (send) =>
      send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: fullPage,
      }).then((result) => result.data),
    );
  } catch {
    const image = await slot.webContents.capturePage();
    return image.toPNG().toString("base64");
  }
}

async function withDebugger(slot, fn) {
  const debug = slot.webContents.debugger;
  const wasAttached = debug.isAttached();
  if (!wasAttached) {
    debug.attach("1.3");
  }
  try {
    return await fn((method, params) => debug.sendCommand(method, params || {}));
  } finally {
    if (!wasAttached && debug.isAttached()) {
      debug.detach();
    }
  }
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

function detachSlot(slot) {
  const window = slot.attachedWindow;
  if (!window) {
    return;
  }
  try {
    window.contentView.removeChildView(slot.view);
  } catch {
    // The renderer may race with window teardown; keeping the webContents alive
    // is enough for this POC.
  }
  slot.attachedWindow = null;
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

function normalizeBounds(bounds) {
  return {
    x: Math.max(0, Math.round(Number(bounds?.x) || 0)),
    y: Math.max(0, Math.round(Number(bounds?.y) || 0)),
    width: Math.max(0, Math.round(Number(bounds?.width) || 0)),
    height: Math.max(0, Math.round(Number(bounds?.height) || 0)),
  };
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
  return {
    sessionID: slot.sessionID,
    tabID: slot.tabID,
    status: destroyed ? "lost" : slot.attachedWindow ? "live_internal" : "detached",
    url: destroyed ? "" : webContents.getURL(),
    title: destroyed ? "" : webContents.getTitle(),
    canGoBack: destroyed ? false : webContents.canGoBack(),
    canGoForward: destroyed ? false : webContents.canGoForward(),
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

module.exports = { BrowserHost };
