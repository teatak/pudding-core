const { app, BrowserWindow, Menu, Tray, dialog, ipcMain, nativeImage, nativeTheme, screen, shell, webContents } = require("electron");
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");

const { BrowserBridgeServer } = require("./browser-bridge-server.cjs");
const { BrowserHost } = require("./browser-host.cjs");
const { nativeText, normalizeNativeLocale } = require("./native-i18n.cjs");

const repoRoot = path.resolve(__dirname, "..");
const appDisplayName = "Pudding";

app.setName(appDisplayName);
app.setPath("userData", electronUserDataDir());
const hasSingleInstanceLock = app.requestSingleInstanceLock();
if (!hasSingleInstanceLock) {
  app.quit();
}

const defaultAddr = app.isPackaged ? "127.0.0.1:9669" : "127.0.0.1:9679";
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
let appTray = null;
let shellLocale = "en";
const pendingOAuthReturnURLs = [];

if (hasSingleInstanceLock) {
  registerOAuthReturnProtocol();
}

app.on("open-url", (event, rawURL) => {
  event.preventDefault();
  handleOAuthReturnURL(rawURL);
});

app.whenReady().then(async () => {
  if (!hasSingleInstanceLock) {
    return;
  }
  shellLocale = normalizeNativeLocale(process.env.PUDDING_LOCALE || app.getLocale());
  updateApplicationMenu();
  try {
    const browserBridge = await browserBridgeServer.start();
    const token = await ensureDaemon(browserBridge);
    const window = createMainWindow();
    createTray(window);
    await loadRenderer(window, token);
    flushPendingOAuthReturnURLs();
  } catch (error) {
    console.error("[electron] startup failed", error);
    app.quit();
  }
});

app.on("second-instance", () => {
  const window = BrowserWindow.getAllWindows()[0];
  if (window && !window.isDestroyed()) {
    showMainWindow(window);
  }
});

app.on("activate", () => {
  if (!hasSingleInstanceLock) {
    return;
  }
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
      createTray(window);
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
    title: appDisplayName,
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

  window.on("show", () => updateTrayMenu(window));
  window.on("hide", () => updateTrayMenu(window));
  window.on("focus", () => updateTrayMenu(window));
  window.on("closed", () => updateTrayMenu(null));

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

function createTray(window) {
  if (appTray) {
    updateTrayMenu(window);
    return appTray;
  }
  const icon = trayIcon();
  if (icon.isEmpty()) {
    console.warn("[electron] tray icon not found");
    return null;
  }
  appTray = new Tray(icon);
  appTray.setToolTip(appDisplayName);
  updateTrayMenu(window);
  return appTray;
}

function updateTrayMenu(window) {
  if (!appTray) {
    return;
  }
  const current = window && !window.isDestroyed() ? window : BrowserWindow.getAllWindows()[0];
  const visible = Boolean(current && !current.isDestroyed() && current.isVisible());
  appTray.setContextMenu(
    Menu.buildFromTemplate([
      {
        label: nativeMenuText(visible ? "hideApp" : "showApp"),
        click: () => {
          const target = BrowserWindow.getAllWindows()[0];
          if (!target || target.isDestroyed()) {
            return;
          }
          if (target.isVisible()) {
            target.hide();
            return;
          }
          showMainWindow(target);
        },
      },
      { type: "separator" },
      {
        label: nativeMenuText("quitApp"),
        click: () => {
          quitting = true;
          app.quit();
        },
      },
    ]),
  );
}

function trayIcon() {
  const candidates = [
    process.resourcesPath ? path.join(process.resourcesPath, "TrayTemplate.png") : "",
    path.join(repoRoot, "assets", "macos", "TrayTemplate.png"),
  ].filter(Boolean);
  for (const candidate of candidates) {
    if (!fs.existsSync(candidate)) {
      continue;
    }
    const image = nativeImage.createFromPath(candidate);
    if (!image.isEmpty()) {
      const resized = image.resize({ width: 22, height: 22, quality: "best" });
      resized.setTemplateImage(true);
      return resized;
    }
  }
  return nativeImage.createEmpty();
}

function updateApplicationMenu() {
  const template = [];
  if (process.platform === "darwin") {
    template.push({
      label: appDisplayName,
      submenu: [
        { label: nativeMenuText("about"), role: "about" },
        { type: "separator" },
        { label: nativeMenuText("services"), role: "services" },
        { type: "separator" },
        { label: nativeMenuText("hideApp"), role: "hide" },
        { label: nativeMenuText("hideOthers"), role: "hideOthers" },
        { label: nativeMenuText("showAll"), role: "unhide" },
        { type: "separator" },
        { label: nativeMenuText("quitApp"), role: "quit" },
      ],
    });
  }

  const fileSubmenu = [{ label: nativeMenuText("closeWindow"), role: "close" }];
  if (process.platform !== "darwin") {
    fileSubmenu.push({ type: "separator" }, { label: nativeMenuText("quitApp"), role: "quit" });
  }
  template.push({ label: nativeMenuText("file"), submenu: fileSubmenu });
  template.push({
    label: nativeMenuText("edit"),
    submenu: [
      { label: nativeMenuText("undo"), role: "undo" },
      { label: nativeMenuText("redo"), role: "redo" },
      { type: "separator" },
      { label: nativeMenuText("cut"), role: "cut" },
      { label: nativeMenuText("copy"), role: "copy" },
      { label: nativeMenuText("paste"), role: "paste" },
      { label: nativeMenuText("pasteAndMatchStyle"), role: "pasteAndMatchStyle" },
      { label: nativeMenuText("delete"), role: "delete" },
      { label: nativeMenuText("selectAll"), role: "selectAll" },
    ],
  });

  const viewSubmenu = [];
  if (!app.isPackaged) {
    viewSubmenu.push(
      { label: nativeMenuText("reload"), role: "reload" },
      { label: nativeMenuText("forceReload"), role: "forceReload" },
      { label: nativeMenuText("toggleDevTools"), role: "toggleDevTools" },
      { type: "separator" },
    );
  }
  viewSubmenu.push(
    { label: nativeMenuText("actualSize"), role: "resetZoom" },
    { label: nativeMenuText("zoomIn"), role: "zoomIn" },
    { label: nativeMenuText("zoomOut"), role: "zoomOut" },
    { type: "separator" },
    { label: nativeMenuText("fullScreen"), role: "togglefullscreen" },
  );
  template.push({ label: nativeMenuText("view"), submenu: viewSubmenu });

  const windowSubmenu = [{ label: nativeMenuText("minimize"), role: "minimize" }];
  if (process.platform === "darwin") {
    windowSubmenu.push(
      { label: nativeMenuText("zoomWindow"), role: "zoom" },
      { type: "separator" },
      { label: nativeMenuText("bringAllToFront"), role: "front" },
    );
  }
  template.push({ label: nativeMenuText("window"), submenu: windowSubmenu });
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function setShellLocale(locale) {
  shellLocale = normalizeNativeLocale(locale);
  updateApplicationMenu();
  updateTrayMenu(BrowserWindow.getAllWindows()[0]);
  return shellLocale;
}

function nativeMenuText(key) {
  return nativeText(shellLocale, key, { app: appDisplayName });
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

ipcMain.handle("pudding:desktop:set-locale", (event, locale) => {
  assertTrustedSender(event);
  return setShellLocale(locale);
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
  url.searchParams.set("locale", shellLocale);
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

  daemonProcess = spawn(daemonBin, ["-addr", daemonAddr, "-lan"], {
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
  const defaultHome = app.isPackaged ? ".pudding" : ".pudding-dev";
  const home = (process.env.PUDDING_HOME || path.join(os.homedir(), defaultHome)).trim();
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
  const packagedCandidates = [
    process.env.PUDDING_DAEMON_BIN,
    path.join(repoRoot, "bin", exe),
  ].filter(Boolean);
  const devCandidates = [
    process.env.PUDDING_DAEMON_BIN,
    path.join(repoRoot, "bin", exe),
    process.resourcesPath ? path.join(process.resourcesPath, exe) : "",
  ].filter(Boolean);
  const candidates = app.isPackaged ? packagedCandidates : devCandidates;

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

function electronUserDataDir() {
  const configured = String(process.env.PUDDING_ELECTRON_USER_DATA_DIR || "").trim();
  if (configured) {
    return path.resolve(configured);
  }
  return path.join(app.getPath("appData"), app.isPackaged ? appDisplayName : `${appDisplayName} Dev`);
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
