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
    if (!slot.attachedWindow) {
      window.contentView.addChildView(slot.view);
      slot.attachedWindow = window;
    }
    slot.view.setBounds(bounds);
    const url = normalizeURL(request.url);
    if (url && slot.webContents.getURL() !== url) {
      await slot.webContents.loadURL(url);
    }
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

  listTabs(request) {
    const sessionID = String(request.sessionID || "").trim();
    if (!sessionID) {
      throw new Error("browser session id missing");
    }
    return {
      tabs: Array.from(this.slots.values())
        .filter((slot) => slot.sessionID === sessionID)
        .map(snapshot),
      processMode: "headless",
    };
  }

  closeSession(request) {
    const sessionID = String(request.sessionID || "").trim();
    if (!sessionID) {
      throw new Error("browser session id missing");
    }
    for (const slot of Array.from(this.slots.values())) {
      if (slot.sessionID !== sessionID) {
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
      throw new Error("browser tab not found");
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
      sessionID: request.sessionID,
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
  const sessionID = String(request.sessionID || "").trim();
  if (!sessionID) {
    throw new Error("browser session id missing");
  }
  const tabID = normalizeTabID(request.tabID);
  return `${sessionID}:${tabID}`;
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

module.exports = { BrowserHost };
