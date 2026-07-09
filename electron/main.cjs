const { app, BrowserWindow, dialog, ipcMain, nativeTheme, screen, shell, webContents } = require("electron");
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");

const { BrowserBridgeServer } = require("./browser-bridge-server.cjs");
const { BrowserHost } = require("./browser-host.cjs");

const repoRoot = path.resolve(__dirname, "..");
const defaultAddr = "127.0.0.1:9679";
const daemonAddr = (process.env.PUDDING_DAEMON_ADDR || defaultAddr).trim();
const apiBase = trimTrailingSlash(process.env.PUDDING_API_BASE || `http://${daemonAddr}`);
const devURL = trimTrailingSlash(process.env.PUDDING_DEV_URL || "");
const oauthReturnScheme = normalizeURLScheme(process.env.PUDDING_OAUTH_RETURN_SCHEME || "pudding");
const macTrafficLightPosition = { x: 18, y: 18 };
const defaultWindowBounds = { width: 1440, height: 920 };
const minWindowBounds = { width: 1000, height: 680 };
let themePreference = normalizeThemePreference(process.env.PUDDING_THEME || "system");
nativeTheme.themeSource = themePreference;
const browserHost = new BrowserHost(
  (snapshot) => {
    broadcastToTrustedRenderers("pudding:browser:updated", snapshot);
  },
  (cursor) => {
    broadcastToTrustedRenderers("pudding:browser:cursor", cursor);
  },
  (event) => {
    broadcastToTrustedRenderers("pudding:browser:automation-start", event);
  },
);
browserHost.setWebviewCaptureHandler(captureVisibleWebview);
const browserBridgeServer = new BrowserBridgeServer(browserHost);
const webviewCaptureRequests = new Map();
let webviewCaptureSeq = 0;

let daemonProcess = null;
let quitting = false;
const pendingOAuthReturnURLs = [];

app.setName("Pudding");
registerOAuthReturnProtocol();

app.on("open-url", (event, rawURL) => {
  event.preventDefault();
  handleOAuthReturnURL(rawURL);
});

app.whenReady().then(async () => {
  try {
    const browserBridge = await browserBridgeServer.start();
    const token = await ensureDaemon(browserBridge);
    const window = createMainWindow();
    await loadRenderer(window, token);
    flushPendingOAuthReturnURLs();
  } catch (error) {
    console.error("[electron] startup failed", error);
    app.quit();
  }
});

app.on("activate", () => {
  const window = BrowserWindow.getAllWindows()[0];
  if (window) {
    showMainWindow(window);
    return;
  }
  void browserBridgeServer
    .start()
    .then((browserBridge) => ensureDaemon(browserBridge))
    .then((token) => {
      const window = createMainWindow();
      return loadRenderer(window, token).then(() => flushPendingOAuthReturnURLs());
    })
    .catch((error) => {
      console.error("[electron] activate failed", error);
      app.quit();
    });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", () => {
  quitting = true;
  for (const window of BrowserWindow.getAllWindows()) {
    saveWindowState(window);
  }
  stopManagedDaemon();
  void browserBridgeServer.stop();
});

nativeTheme.on("updated", () => {
  broadcastThemeState();
});

function createMainWindow() {
  const savedBounds = readWindowState();
  const window = new BrowserWindow({
    ...savedBounds,
    minWidth: minWindowBounds.width,
    minHeight: minWindowBounds.height,
    title: "Pudding",
    backgroundColor: themeBackgroundColor(),
    ...(process.platform === "darwin"
      ? {
          titleBarStyle: "hidden",
          trafficLightPosition: macTrafficLightPosition,
        }
      : {}),
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webviewTag: true,
    },
  });

  window.webContents.setWindowOpenHandler(({ url }) => {
    void shell.openExternal(url);
    return { action: "deny" };
  });

  window.webContents.on("will-navigate", (event, url) => {
    if (isTrustedRendererURL(url)) {
      return;
    }
    event.preventDefault();
    void shell.openExternal(url);
  });
  window.webContents.on("did-finish-load", () => {
    if (isTrustedRendererURL(window.webContents.getURL())) {
      window.webContents.send("pudding:theme:updated", themeState());
      sendShellFullscreenState(window);
    }
  });
  bindWindowState(window);
  bindShellState(window);
  window.on("close", (event) => {
    saveWindowState(window);
    if (process.platform === "darwin" && !quitting) {
      event.preventDefault();
      window.hide();
      return;
    }
  });

  return window;
}

function showMainWindow(window) {
  if (window.isMinimized()) {
    window.restore();
  }
  window.show();
  window.focus();
}

function registerOAuthReturnProtocol() {
  if (!oauthReturnScheme) {
    return;
  }
  try {
    if (process.defaultApp) {
      const scriptPath = path.resolve(process.argv[1] || path.join(repoRoot, "electron", "main.cjs"));
      app.setAsDefaultProtocolClient(oauthReturnScheme, process.execPath, [scriptPath]);
    } else {
      app.setAsDefaultProtocolClient(oauthReturnScheme);
    }
  } catch (error) {
    console.warn("[electron] register oauth return protocol failed", error);
  }
}

function handleOAuthReturnURL(rawURL) {
  const payload = oauthReturnPayload(rawURL);
  if (!payload) {
    return false;
  }
  if (!app.isReady()) {
    pendingOAuthReturnURLs.push(rawURL);
    return true;
  }
  processOAuthReturn(payload);
  return true;
}

function flushPendingOAuthReturnURLs() {
  if (pendingOAuthReturnURLs.length === 0) {
    return;
  }
  const urls = pendingOAuthReturnURLs.splice(0);
  for (const rawURL of urls) {
    const payload = oauthReturnPayload(rawURL);
    if (payload) {
      processOAuthReturn(payload);
    }
  }
}

function processOAuthReturn(payload) {
  const window = BrowserWindow.getAllWindows()[0];
  if (window) {
    showMainWindow(window);
  }
  broadcastToTrustedRenderers("pudding:oauth:connected", payload);
}

function oauthReturnPayload(rawURL) {
  try {
    const url = new URL(String(rawURL || ""));
    if (url.protocol !== `${oauthReturnScheme}:` || url.hostname !== "oauth" || !url.pathname.startsWith("/connected/")) {
      return null;
    }
    return { provider: decodeURIComponent(url.pathname.slice("/connected/".length)) };
  } catch {
    return null;
  }
}

function bindWindowState(window) {
  let timer = null;
  const scheduleSave = () => {
    if (timer) {
      clearTimeout(timer);
    }
    timer = setTimeout(() => {
      timer = null;
      saveWindowState(window);
    }, 400);
  };

  window.on("resize", scheduleSave);
  window.on("move", scheduleSave);
  window.on("maximize", scheduleSave);
  window.on("unmaximize", scheduleSave);
  window.on("leave-full-screen", scheduleSave);
}

function bindShellState(window) {
  window.on("enter-full-screen", () => sendShellFullscreenState(window));
  window.on("leave-full-screen", () => sendShellFullscreenState(window));
}

function sendShellFullscreenState(window) {
  if (!window || window.webContents.isDestroyed() || !isTrustedRendererURL(window.webContents.getURL())) {
    return;
  }
  window.webContents.send("pudding:shell:fullscreen", window.isFullScreen());
}

function readWindowState() {
  try {
    const raw = fs.readFileSync(windowStatePath(), "utf8");
    const state = JSON.parse(raw);
    const bounds = normalizeWindowBounds(state?.bounds || state);
    if (bounds && windowBoundsVisible(bounds)) {
      return bounds;
    }
  } catch {
    // Missing or invalid state should fall back to the default window size.
  }
  return { ...defaultWindowBounds };
}

function saveWindowState(window) {
  if (!window || window.isDestroyed() || window.isFullScreen()) {
    return;
  }
  const bounds = normalizeWindowBounds(window.getBounds());
  if (!bounds) {
    return;
  }
  try {
    const file = windowStatePath();
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, `${JSON.stringify({ bounds, updatedAt: new Date().toISOString() }, null, 2)}\n`);
  } catch (error) {
    console.warn("[electron] save window state failed", error);
  }
}

function windowStatePath() {
  return path.join(app.getPath("userData"), "window-state.json");
}

function normalizeWindowBounds(value) {
  const width = Math.round(Number(value?.width));
  const height = Math.round(Number(value?.height));
  if (!Number.isFinite(width) || !Number.isFinite(height)) {
    return null;
  }
  const bounds = {
    width: Math.max(minWindowBounds.width, width),
    height: Math.max(minWindowBounds.height, height),
  };
  const x = Math.round(Number(value?.x));
  const y = Math.round(Number(value?.y));
  if (Number.isFinite(x) && Number.isFinite(y)) {
    bounds.x = x;
    bounds.y = y;
  }
  return bounds;
}

function windowBoundsVisible(bounds) {
  if (!Number.isFinite(bounds.x) || !Number.isFinite(bounds.y)) {
    return true;
  }
  const displays = screen.getAllDisplays();
  if (displays.length === 0) {
    return true;
  }
  return displays.some((display) => rectsIntersect(display.workArea, bounds));
}

function rectsIntersect(a, b) {
  return a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
}

function broadcastToTrustedRenderers(channel, payload) {
  for (const window of BrowserWindow.getAllWindows()) {
    if (!window.webContents.isDestroyed() && isTrustedRendererURL(window.webContents.getURL())) {
      window.webContents.send(channel, payload);
    }
  }
}

ipcMain.handle("pudding:theme:get", (event) => {
  assertTrustedSender(event);
  return themeState();
});

ipcMain.handle("pudding:theme:set", (event, theme) => {
  assertTrustedSender(event);
  themePreference = normalizeThemePreference(theme);
  nativeTheme.themeSource = themePreference;
  broadcastThemeState();
  return themeState();
});

ipcMain.handle("pudding:shell:is-fullscreen", (event) => {
  const window = assertTrustedSender(event);
  return window.isFullScreen();
});

ipcMain.handle("pudding:desktop:open-external", async (event, rawURL) => {
  assertTrustedSender(event);
  const url = String(rawURL || "").trim();
  if (!isAllowedExternalURL(url)) {
    return false;
  }
  await shell.openExternal(url);
  return true;
});

ipcMain.handle("pudding:desktop:pick-directories", async (event, options) => {
  const window = assertTrustedSender(event);
  const result = await dialog.showOpenDialog(window, {
    buttonLabel: stringOption(options?.buttonLabel),
    message: stringOption(options?.message),
    properties: ["openDirectory", "multiSelections", "createDirectory"],
    title: stringOption(options?.title),
  });
  return result.canceled ? [] : result.filePaths;
});

ipcMain.handle("pudding:browser:ensure", (event, request) => {
  assertTrustedSender(event);
  return browserHost.ensure(request || {});
});

ipcMain.handle("pudding:browser:webview-register", (event, request) => {
  assertTrustedSender(event);
  const target = webContents.fromId(Number(request?.webContentsID));
  return browserHost.registerWebContents(request || {}, target);
});

ipcMain.handle("pudding:browser:webview-capture-result", (event, response) => {
  assertTrustedSender(event);
  return settleWebviewCapture(response || {});
});

ipcMain.handle("pudding:browser:load-url", (event, request) => {
  assertTrustedSender(event);
  return browserHost.loadURL(request || {});
});

ipcMain.handle("pudding:browser:back", (event, request) => {
  assertTrustedSender(event);
  return browserHost.back(request || {});
});

ipcMain.handle("pudding:browser:forward", (event, request) => {
  assertTrustedSender(event);
  return browserHost.forward(request || {});
});

ipcMain.handle("pudding:browser:reload", (event, request) => {
  assertTrustedSender(event);
  return browserHost.reload(request || {});
});

ipcMain.handle("pudding:browser:list-tabs", (event, request) => {
  assertTrustedSender(event);
  return browserHost.listTabs(request || {});
});

ipcMain.handle("pudding:browser:close-session", (event, request) => {
  assertTrustedSender(event);
  browserHost.closeSession(request || {});
});

ipcMain.handle("pudding:browser:close-tab", (event, request) => {
  assertTrustedSender(event);
  return browserHost.closeTab(request || {});
});

function captureVisibleWebview(request) {
  const windows = BrowserWindow.getAllWindows().filter(
    (window) => !window.webContents.isDestroyed() && isTrustedRendererURL(window.webContents.getURL()),
  );
  if (windows.length === 0) {
    return Promise.reject(new Error("no renderer window available"));
  }
  const captureID = `capture_${Date.now()}_${++webviewCaptureSeq}`;
  const payload = {
    captureID,
    sessionID: String(request?.sessionID || ""),
    tabID: String(request?.tabID || ""),
    fullPage: Boolean(request?.fullPage),
  };
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      webviewCaptureRequests.delete(captureID);
      reject(new Error("renderer webview capture timed out"));
    }, 5000);
    webviewCaptureRequests.set(captureID, { resolve, reject, timer });
    for (const window of windows) {
      window.webContents.send("pudding:browser:webview-capture-request", payload);
    }
  });
}

function settleWebviewCapture(response) {
  const captureID = String(response?.captureID || "");
  const pending = webviewCaptureRequests.get(captureID);
  if (!pending) {
    return { ok: false };
  }
  clearTimeout(pending.timer);
  webviewCaptureRequests.delete(captureID);
  const error = String(response?.error || "");
  if (error) {
    pending.reject(new Error(error));
    return { ok: true };
  }
  pending.resolve(response);
  return { ok: true };
}

async function loadRenderer(window, token) {
  const rendererBase = devURL || apiBase;
  const url = new URL(rendererBase);
  url.pathname = "/";
  url.searchParams.set("token", token);
  url.searchParams.set("api", apiBase);
  url.searchParams.set("shell", desktopShell());
  const state = themeState();
  url.searchParams.set("theme", state.theme);
  url.searchParams.set("resolvedTheme", state.resolved);
  await window.loadURL(url.toString());
}

async function ensureDaemon(browserBridge) {
  const attachedToken = await readUsableToken();
  if (attachedToken) {
    return attachedToken;
  }

  const daemonBin = resolveDaemonBinary();
  if (!daemonBin) {
    throw new Error("puddingd binary not found. Run `make desktop-dev` so the dev binary is built first.");
  }

  daemonProcess = spawn(daemonBin, ["-addr", daemonAddr], {
    cwd: repoRoot,
    env: {
      ...process.env,
      PUDDING_ELECTRON_BROWSER_BRIDGE_URL: browserBridge?.url || "",
      PUDDING_ELECTRON_BROWSER_BRIDGE_TOKEN: browserBridge?.token || "",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  daemonProcess.stdout.on("data", (chunk) => process.stdout.write(`[puddingd] ${chunk}`));
  daemonProcess.stderr.on("data", (chunk) => process.stderr.write(`[puddingd] ${chunk}`));
  daemonProcess.on("exit", (code, signal) => {
    daemonProcess = null;
    if (!quitting) {
      console.error(`[electron] managed daemon exited code=${code} signal=${signal}`);
      app.quit();
    }
  });

  return waitForDaemon();
}

async function readUsableToken() {
  try {
    const token = await readDaemonToken();
    if (!token) {
      return "";
    }
    if (await canConnectToDaemon()) {
      return token;
    }
  } catch {
    return "";
  }
  return "";
}

async function waitForDaemon() {
  const started = Date.now();
  let lastError = null;
  while (Date.now() - started < 15000) {
    try {
      const token = await readDaemonToken();
      if (token && (await canConnectToDaemon())) {
        return token;
      }
    } catch (error) {
      lastError = error;
    }
    await delay(250);
  }
  throw new Error(`daemon did not become ready: ${lastError?.message || "timeout"}`);
}

async function readDaemonToken() {
  const token = (await fsp.readFile(daemonTokenPath(), "utf8")).trim();
  return token;
}

function daemonTokenPath() {
  const home = (process.env.PUDDING_HOME || path.join(os.homedir(), ".pudding-dev")).trim();
  return path.join(home, "daemon.token");
}

function canConnectToDaemon() {
  const { hostname, port } = new URL(apiBase);
  return new Promise((resolve) => {
    const socket = net.connect({ host: hostname, port: Number(port) || 80 });
    socket.setTimeout(500);
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("timeout", () => {
      socket.destroy();
      resolve(false);
    });
    socket.once("error", () => resolve(false));
  });
}

function resolveDaemonBinary() {
  const exe = process.platform === "win32" ? "puddingd.exe" : "puddingd";
  const candidates = [
    process.env.PUDDING_DAEMON_BIN,
    path.join(repoRoot, "bin", exe),
    process.resourcesPath ? path.join(process.resourcesPath, exe) : "",
  ].filter(Boolean);

  return candidates.find((candidate) => fs.existsSync(candidate)) || "";
}

function stopManagedDaemon() {
  if (!daemonProcess || daemonProcess.killed) {
    return;
  }
  daemonProcess.kill("SIGTERM");
}

function themeState() {
  return {
    theme: themePreference,
    resolved: nativeTheme.shouldUseDarkColors ? "dark" : "light",
  };
}

function broadcastThemeState() {
  const state = themeState();
  for (const window of BrowserWindow.getAllWindows()) {
    window.setBackgroundColor(themeBackgroundColor());
    if (!window.webContents.isDestroyed() && isTrustedRendererURL(window.webContents.getURL())) {
      window.webContents.send("pudding:theme:updated", state);
    }
  }
}

function themeBackgroundColor() {
  return nativeTheme.shouldUseDarkColors ? "#171717" : "#ffffff";
}

function normalizeThemePreference(theme) {
  return theme === "light" || theme === "dark" || theme === "system" ? theme : "system";
}

function isTrustedRendererURL(rawURL) {
  try {
    const target = new URL(rawURL);
    return [devURL, apiBase]
      .filter(Boolean)
      .some((base) => target.origin === new URL(base).origin);
  } catch {
    return false;
  }
}

function assertTrustedSender(event) {
  const window = BrowserWindow.fromWebContents(event.sender);
  const senderURL = event.senderFrame?.url || event.sender.getURL();
  if (!window || !isTrustedRendererURL(senderURL)) {
    throw new Error("untrusted renderer");
  }
  return window;
}

function isAllowedExternalURL(rawURL) {
  try {
    const url = new URL(rawURL);
    return url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:";
  } catch {
    return false;
  }
}

function stringOption(value) {
  const text = String(value || "").trim();
  return text || undefined;
}

function trimTrailingSlash(value) {
  return String(value || "").replace(/\/+$/, "");
}

function normalizeURLScheme(value) {
  const scheme = String(value || "").trim();
  if (!/^[A-Za-z][A-Za-z0-9+.-]*$/.test(scheme)) {
    return "";
  }
  return scheme;
}

function desktopShell() {
  return process.platform === "darwin" ? "electron-mac" : "electron";
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
