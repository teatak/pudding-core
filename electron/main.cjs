const {
  app,
  BrowserWindow,
  Menu,
  Tray,
  dialog,
  ipcMain,
  nativeImage,
  nativeTheme,
  safeStorage,
  screen,
  session,
  shell,
  systemPreferences,
  webContents,
} = require("electron");
const { autoUpdater } = require("electron-updater");
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const packageMetadata = require("../package.json");

const { BrowserBridgeServer } = require("./browser-bridge-server.cjs");
const {
  BrowserCredentialController,
  BrowserCredentialVault,
  parseChromePasswordCSV,
} = require("./browser-credentials.cjs");
const { resolveBrowserFavicon } = require("./browser-favicon.cjs");
const { BrowserHost } = require("./browser-host.cjs");
const { configureManagedBrowserPermissions, managedBrowserPartition } = require("./browser-permissions.cjs");
const { hardenManagedBrowserWebview } = require("./browser-webview-security.cjs");
const { probePuddingDaemon } = require("./daemon-health.cjs");
const { installConsoleFileLogging } = require("./file-logger.cjs");
const { MobileAccessBridge } = require("./mobile-access-bridge.cjs");
const { buildEditContextMenuTemplate } = require("./context-menu.cjs");
const { ComputerUseBridgeServer } = require("./computer-use-bridge-server.cjs");
const { ComputerUseHost } = require("./computer-use-host.cjs");
const { ComputerUsePermissionCoordinator } = require("./computer-use-permissions.cjs");
const {
  DesktopPermissionController,
  desktopPermissionSettingsURL,
} = require("./desktop-permissions.cjs");
const { nativeText, normalizeNativeLocale } = require("./native-i18n.cjs");
const { ProjectFileWatcher } = require("./project-file-watcher.cjs");
const { UpdateManager, updateStatuses } = require("./update-manager.cjs");
const { readPreviewUpdatePreference, writePreviewUpdatePreference } = require("./update-preferences.cjs");
const { createSystemTerminalOpener } = require("./system-terminal.cjs");

const repoRoot = app.isPackaged ? path.join(process.resourcesPath, "app") : path.resolve(__dirname, "..");
const browserPreloadPath = path.join(__dirname, "browser-preload.cjs");
const appDisplayName = "Pudding";
const repositoryURL = "https://github.com/teatak/pudding";
const issueTrackerURL = `${repositoryURL}/issues`;
installConsoleFileLogging({ logsDir: path.join(puddingHomePath(), "logs"), prefix: "electron" });
process.on("uncaughtExceptionMonitor", (error, origin) => {
  console.error(`[electron] uncaught exception origin=${origin}`, error);
});
const releasePageOverride = normalizeUpdatePageURL(process.env.PUDDING_UPDATE_DOWNLOAD_URL);
const releasePageURL = releasePageOverride || `${repositoryURL}/releases/latest`;
const openSystemTerminal = createSystemTerminalOpener({ spawn });

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
console.info("[electron] starting", {
  version: packageMetadata.version,
  channel: packageMetadata.puddingReleaseChannel || (app.isPackaged ? "stable" : "dev"),
  packaged: app.isPackaged,
  home: puddingHomePath(),
  daemonAddr,
});
const oauthReturnScheme = normalizeURLScheme(process.env.PUDDING_OAUTH_RETURN_SCHEME || "pudding");
const macTrafficLightPosition = { x: 18, y: 18 };
const defaultWindowBounds = { width: 1440, height: 920 };
const minWindowBounds = { width: 560, height: 680 };
const browserFaviconCache = new Map();
const browserAutomationLifecycleWaiters = new Map();
const browserCredentialFillWaiters = new Map();
let mainWindow = null;
let themePreference = normalizeThemePreference(process.env.PUDDING_THEME || "system");
nativeTheme.themeSource = themePreference;
const browserHost = new BrowserHost(
  (snapshot) => {
    if (snapshot?.status === "lost") {
      browserCredentials.release(snapshot);
    }
    broadcastToTrustedRenderers("pudding:browser:updated", snapshot);
  },
  (cursor) => {
    broadcastToTrustedRenderers("pudding:browser:cursor", cursor);
  },
  (event, target) => requestBrowserAutomationLifecycle("start", event, target),
  (request) => {
    broadcastToTrustedRenderers("pudding:browser:webview-required", request);
  },
  (event, target) => requestBrowserAutomationLifecycle("end", event, target),
  {
    options: () => ({ backgroundColor: themeBackgroundColor() }),
    resolveFavicon: resolveCachedBrowserFavicon,
    created: (window) => {
      window.setBackgroundColor(themeBackgroundColor());
      bindEditContextMenu(window.webContents, window);
    },
    blockedNavigation: ({ url, window }) => {
      if (!handleOAuthReturnURL(url)) {
        return false;
      }
      if (window && !window.isDestroyed()) {
        window.close();
      }
      return true;
    },
    selectionChanged: (selection) => {
      broadcastToTrustedRenderers("pudding:browser:selection-updated", selection);
    },
    foundInPage: (result) => {
      broadcastToTrustedRenderers("pudding:browser:found-in-page", result);
    },
    interaction: (interaction) => {
      broadcastToTrustedRenderers("pudding:browser:interaction", interaction);
    },
  },
);
const browserCredentialVault = new BrowserCredentialVault({
  filePath: path.join(puddingHomePath(), "browser", "credentials.vault"),
  safeStorage,
});
const browserCredentials = new BrowserCredentialController({ vault: browserCredentialVault });
const browserBridgeServer = new BrowserBridgeServer(browserHost);
const computerUseHost = new ComputerUseHost({ binaryPath: resolveComputerUseHelperBinary() });
let computerUsePermissionCoordinator = null;
async function openDesktopPermissionSettings(permission) {
  const url = desktopPermissionSettingsURL(permission);
  if (!url) {
    return false;
  }
  await shell.openExternal(url);
  return true;
}
const desktopPermissionController = new DesktopPermissionController({
  computerUseHost,
  systemPreferences,
  openSettings: openDesktopPermissionSettings,
  restartComputerUse: () => computerUseHost.stop(),
  onStateChange: (state) => {
    broadcastToTrustedRenderers("pudding:desktop:permissions-updated", state);
    computerUsePermissionCoordinator?.reconcile(state);
  },
});
computerUsePermissionCoordinator = new ComputerUsePermissionCoordinator({
  permissions: desktopPermissionController,
  restartHelper: () => computerUseHost.stop(),
  onGuideChange: (guide) => {
    broadcastToTrustedRenderers("pudding:desktop:computer-use-permission-guide", guide);
  },
});
const computerUseBridgeServer = new ComputerUseBridgeServer(computerUseHost, {
  permissionCoordinator: computerUsePermissionCoordinator,
});
const projectFileWatcher = new ProjectFileWatcher();
const mobileAccessBridge = new MobileAccessBridge({ apiBase, webBase: devURL || apiBase });

let daemonProcess = null;
let daemonStartupPromise = null;
let quitting = false;
let shutdownPromise = null;
let shutdownComplete = false;
let appTray = null;
let shellLocale = "en";
const pendingOAuthReturnURLs = [];
const updateFeedURL = normalizeUpdateFeedURL(process.env.PUDDING_UPDATE_FEED_URL);
const storedPreviewUpdatePreference = readPreviewUpdatePreference(previewUpdatePreferencePath());
const receivePreviewUpdates =
  normalizeOptionalBoolean(process.env.PUDDING_RECEIVE_PREVIEW_UPDATES) ??
  storedPreviewUpdatePreference ??
  packageMetadata.puddingReleaseChannel === "preview";
const simulatedUpdateVersion =
  !app.isPackaged && process.env.PUDDING_UPDATE_TEST_STATE === "available" ? "99.0.0-test" : "";
const updateManager = new UpdateManager({
  updater: autoUpdater,
  isPackaged: app.isPackaged,
  disabled: process.env.PUDDING_DISABLE_UPDATE_CHECK === "1",
  receivePreviewUpdates,
  feedURL: updateFeedURL,
  simulatedVersion: simulatedUpdateVersion,
  beforeInstall: prepareForUpdateInstall,
  onDownloadRequest: confirmUpdateDownload,
  onError: (error) => console.warn("[electron] update failed", error),
  onInteractiveResult: showInteractiveUpdateResult,
  onSimulatedInstall: showSimulatedUpdateResult,
  onStateChange: (state) => {
    if (app.isReady()) {
      updateApplicationMenu();
      broadcastToTrustedRenderers("pudding:desktop:update-state", state);
    }
  },
});

if (hasSingleInstanceLock) {
  registerOAuthReturnProtocol();
  captureOAuthReturnArgs(process.argv);
}

app.on("open-url", (event, rawURL) => {
  event.preventDefault();
  handleOAuthReturnURL(rawURL);
});

app.whenReady().then(async () => {
  if (!hasSingleInstanceLock) {
    return;
  }
  setApplicationIcon();
  shellLocale = normalizeNativeLocale(process.env.PUDDING_LOCALE || app.getLocale());
  updateApplicationMenu();
  try {
    configureManagedBrowserPermissions(session.fromPartition(managedBrowserPartition));
    const [browserBridge, computerBridge] = await Promise.all([
      browserBridgeServer.start(),
      computerUseBridgeServer.start(),
    ]);
    const token = await ensureDaemon(browserBridge, computerBridge);
    const window = createMainWindow();
    createTray();
    await loadRenderer(window, token);
    console.info("[electron] ready");
    flushPendingOAuthReturnURLs();
    updateManager.start();
    app.on("activate", activateApplication);
  } catch (error) {
    console.error("[electron] startup failed", error);
    app.quit();
  }
});

app.on("second-instance", (_event, commandLine) => {
  captureOAuthReturnArgs(commandLine);
  const window = getMainWindow();
  if (window && !window.isDestroyed()) {
    showMainWindow(window);
  }
});

function activateApplication() {
  void refreshComputerUsePermissions();
  if (!hasSingleInstanceLock) {
    return;
  }
  const window = getMainWindow();
  if (window) {
    showMainWindow(window);
    return;
  }
  void Promise.all([browserBridgeServer.start(), computerUseBridgeServer.start()])
    .then(([browserBridge, computerBridge]) => ensureDaemon(browserBridge, computerBridge))
    .then((token) => {
      const window = createMainWindow();
      createTray();
      return loadRenderer(window, token).then(() => flushPendingOAuthReturnURLs());
    })
    .catch((error) => {
      console.error("[electron] activate failed", error);
      app.quit();
    });
}

async function refreshComputerUsePermissions() {
  if (!computerUsePermissionCoordinator.currentGuide() && !desktopPermissionController.currentState().supported) {
    return;
  }
  try {
    await computerUsePermissionCoordinator.refresh();
  } catch (error) {
    console.warn("[electron] Computer Use permission refresh failed", error);
  }
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", (event) => {
  quitting = true;
  updateManager.stop();
  const window = getMainWindow();
  if (window) {
    saveWindowState(window);
  }
  projectFileWatcher.closeAll();
  if (shutdownComplete) {
    return;
  }
  event.preventDefault();
  if (shutdownPromise) {
    return;
  }
  console.info("[electron] shutting down");
  shutdownPromise = stopDesktopResources()
    .catch((error) => console.error("[electron] shutdown failed", error))
    .finally(() => {
      shutdownComplete = true;
      app.quit();
    });
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.once(signal, () => {
    console.info(`[electron] received ${signal}, requesting graceful shutdown`);
    app.quit();
  });
}

nativeTheme.on("updated", () => {
  broadcastThemeState();
});

function createMainWindow() {
  const savedBounds = readWindowState();
  const window = new BrowserWindow({
    ...savedBounds,
    minWidth: minWindowBounds.width,
    minHeight: minWindowBounds.height,
    title: nativeAppName(),
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
  mainWindow = window;
  window.on("focus", () => void refreshComputerUsePermissions());
  window.once("closed", () => {
    if (mainWindow === window) {
      mainWindow = null;
    }
  });

  bindEditContextMenu(window.webContents, window);
  window.webContents.on("will-attach-webview", (event, webPreferences, params) => {
    if (!hardenManagedBrowserWebview(
      event,
      webPreferences,
      params,
      managedBrowserPartition,
      browserPreloadPath,
    )) {
      console.warn("[electron] blocked unmanaged browser webview attachment");
    }
  });
  window.webContents.on("did-attach-webview", (_event, guestContents) => {
    guestContents.setWindowOpenHandler(() => ({ action: "deny" }));
    bindEditContextMenu(guestContents, window);
  });

  window.webContents.setWindowOpenHandler(({ url }) => {
    openAllowedExternalURL(url);
    return { action: "deny" };
  });

  window.webContents.on("will-navigate", (event, url) => {
    if (isTrustedRendererURL(url)) {
      return;
    }
    event.preventDefault();
    openAllowedExternalURL(url);
  });
  window.webContents.on("did-finish-load", () => {
    if (isTrustedRendererURL(window.webContents.getURL())) {
      window.webContents.send("pudding:theme:updated", themeState());
      window.webContents.send("pudding:desktop:update-state", updateManager.getState());
      sendShellFullscreenState(window);
    }
  });
  bindWindowState(window);
  bindShellState(window);
  window.on("close", (event) => {
    saveWindowState(window);
    if (process.platform === "darwin" && !quitting) {
      event.preventDefault();
      if (window.isFullScreen()) {
        window.once("leave-full-screen", () => {
          if (!window.isDestroyed()) {
            window.hide();
          }
        });
        window.setFullScreen(false);
      } else {
        window.hide();
      }
      return;
    }
  });

  return window;
}

function bindEditContextMenu(contents, ownerWindow) {
  contents.on("context-menu", (event, params) => {
    event.preventDefault();
    const template = buildEditContextMenuTemplate(contents, params, nativeMenuText, {
      platform: process.platform,
      openExternal: (url) => void shell.openExternal(url),
    });
    if (template.length === 0 || ownerWindow.isDestroyed()) {
      return;
    }
    Menu.buildFromTemplate(template).popup({
      window: ownerWindow,
      ...(params.frame ? { frame: params.frame } : {}),
      ...(process.platform !== "darwin" && params.menuSourceType ? { sourceType: params.menuSourceType } : {}),
    });
  });
}

function createTray() {
  if (appTray) {
    updateTrayMenu();
    return appTray;
  }
  const icon = trayIcon();
  if (icon.isEmpty()) {
    console.warn("[electron] tray icon not found");
    return null;
  }
  appTray = new Tray(icon);
  appTray.setToolTip(nativeAppName());
  updateTrayMenu();
  return appTray;
}

function setApplicationIcon() {
  if (process.platform !== "darwin" || !app.dock) {
    return;
  }
  const candidates = [
    process.resourcesPath ? path.join(process.resourcesPath, "AppIcon.png") : "",
    path.join(repoRoot, "assets", "macos", "AppIcon.png"),
  ].filter(Boolean);
  for (const candidate of candidates) {
    if (!fs.existsSync(candidate)) {
      continue;
    }
    const image = nativeImage.createFromPath(candidate);
    if (!image.isEmpty()) {
      app.dock.setIcon(image);
      return;
    }
  }
  console.warn("[electron] application icon not found");
}

function updateTrayMenu() {
  if (!appTray) {
    return;
  }
  appTray.setContextMenu(
    Menu.buildFromTemplate([
      {
        label: nativeMenuText("openApp"),
        click: () => {
          const target = getMainWindow();
          if (!target || target.isDestroyed()) {
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
  const updateItem = updateMenuItem();
  if (process.platform === "darwin") {
    template.push({
      label: nativeAppName(),
      submenu: [
        { label: nativeMenuText("about"), role: "about" },
        { type: "separator" },
        updateItem,
        {
          accelerator: "CmdOrCtrl+,",
          label: nativeMenuText("settings"),
          click: () => sendDesktopMenuCommand("settings"),
        },
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

  const fileSubmenu = [
    {
      accelerator: "CmdOrCtrl+N",
      label: nativeMenuText("newSession"),
      click: () => sendDesktopMenuCommand("new-session"),
    },
    {
      accelerator: "CmdOrCtrl+K",
      label: nativeMenuText("searchSessions"),
      click: () => sendDesktopMenuCommand("search-sessions"),
    },
    { type: "separator" },
    { label: nativeMenuText("closeWindow"), role: "close" },
  ];
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
      { type: "separator" },
      {
        accelerator: "CmdOrCtrl+F",
        label: nativeMenuText("searchConversation"),
        click: () => sendDesktopMenuCommand("search-conversation"),
      },
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
  const helpSubmenu = [
    {
      label: nativeMenuText("puddingHelp"),
      click: () => void shell.openExternal(repositoryURL),
    },
    {
      label: nativeMenuText("downloadLatest"),
      click: () => void openUpdateDownloadPage(),
    },
    {
      label: nativeMenuText("reportIssue"),
      click: () => void shell.openExternal(issueTrackerURL),
    },
    { type: "separator" },
    {
      label: nativeMenuText("showLogs"),
      click: () => void openLogsDirectory(),
    },
  ];
  if (process.platform !== "darwin") {
    helpSubmenu.unshift(updateItem, { type: "separator" });
  }
  template.push({ label: nativeMenuText("help"), role: "help", submenu: helpSubmenu });
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function updateMenuItem() {
  const state = updateManager.getState();
  if (state.status === updateStatuses.available) {
    return {
      label: nativeMenuText("downloadUpdate"),
      click: () => void updateManager.download(),
    };
  }
  if (state.status === updateStatuses.downloaded || state.status === updateStatuses.installing) {
    return {
      enabled: state.status === updateStatuses.downloaded,
      label: nativeMenuText(state.status === updateStatuses.installing ? "restartingToUpdate" : "restartToUpdate"),
      click: () => void activateUpdate(),
    };
  }
  const busy = state.status === updateStatuses.checking || state.status === updateStatuses.downloading;
  return {
    enabled: !busy,
    label: nativeMenuText(state.status === updateStatuses.downloading ? "downloadingUpdate" : busy ? "checkingForUpdates" : "checkForUpdates"),
    click: () => void updateManager.check(true),
  };
}

function setShellLocale(locale) {
  shellLocale = normalizeNativeLocale(locale);
  const window = getMainWindow();
  if (window) {
    window.setTitle(nativeAppName());
  }
  appTray?.setToolTip(nativeAppName());
  updateApplicationMenu();
  updateTrayMenu();
  return shellLocale;
}

function nativeAppName() {
  return nativeText(shellLocale, "appName");
}

function nativeMenuText(key, values = {}) {
  return nativeText(shellLocale, key, { app: nativeAppName(), ...values });
}

function sendDesktopMenuCommand(command) {
  const window = getMainWindow();
  if (!window || window.isDestroyed()) {
    return;
  }
  showMainWindow(window);
  if (isTrustedRendererURL(window.webContents.getURL())) {
    window.webContents.send("pudding:desktop:menu-command", command);
  }
}

async function showInteractiveUpdateResult(result) {
  if (result.kind === "development") {
    await showUpdateMessage(
      {
        buttons: [nativeMenuText("ok")],
        message: nativeMenuText("updatesDevOnlyMessage"),
        title: nativeMenuText("updatesDevOnlyTitle"),
        type: "info",
      },
      true,
    );
    return;
  }
  if (result.kind === "up-to-date") {
    await showUpdateMessage(
      {
        buttons: [nativeMenuText("ok")],
        message: nativeText(shellLocale, "upToDateMessage", { version: app.getVersion() }),
        title: nativeMenuText("upToDateTitle"),
        type: "info",
      },
      true,
    );
    return;
  }
  if (result.kind === "downloaded") {
    return;
  }
  const installError = result.kind === "install-error";
  console.warn(`[electron] ${installError ? "update install" : "update check"} failed`, result.error || result.kind);
  await showUpdateMessage(
    {
      buttons: [nativeMenuText("ok")],
      detail: String(result.error || ""),
      message: nativeMenuText(installError ? "updateInstallFailedMessage" : "updateCheckFailedMessage"),
      title: nativeMenuText(installError ? "updateInstallFailedTitle" : "updateCheckFailedTitle"),
      type: "warning",
    },
    true,
  );
  if (installError) {
    quitting = true;
    app.relaunch();
    app.exit(1);
  }
}

async function confirmUpdateDownload({ version }) {
  const result = await showUpdateMessage(
    {
      buttons: [nativeMenuText("downloadUpdate"), nativeMenuText("notNow")],
      cancelId: 1,
      defaultId: 0,
      message: nativeText(shellLocale, "updateAvailableMessage", { version }),
      noLink: true,
      title: nativeMenuText("updateAvailableTitle"),
      type: "info",
    },
    true,
  );
  return result.response === 0;
}

function showSimulatedUpdateResult() {
  return showUpdateMessage(
    {
      buttons: [nativeMenuText("ok")],
      message: nativeMenuText("updateSimulationMessage"),
      title: nativeMenuText("updateSimulationTitle"),
      type: "info",
    },
    true,
  );
}

function showUpdateMessage(options, bringToFront) {
  const window = getMainWindow();
  if (window && !window.isDestroyed() && (bringToFront || window.isVisible())) {
    if (bringToFront) {
      showMainWindow(window);
    }
    return dialog.showMessageBox(window, options);
  }
  return dialog.showMessageBox(options);
}

async function activateUpdate() {
  return updateManager.install();
}

async function openUpdateDownloadPage() {
  try {
    await shell.openExternal(updateDownloadPageURL(updateManager.getState()));
    return true;
  } catch (error) {
    console.warn("[electron] open update download page failed", error);
    return false;
  }
}

async function openLogsDirectory() {
  const logsDir = path.join(puddingHomePath(), "logs");
  try {
    await fsp.mkdir(logsDir, { recursive: true });
    const error = await shell.openPath(logsDir);
    if (error) {
      throw new Error(error);
    }
  } catch (error) {
    console.warn("[electron] open logs directory failed", error);
  }
}

function getMainWindow() {
  return mainWindow && !mainWindow.isDestroyed() ? mainWindow : null;
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
  if (process.platform === "darwin" && process.defaultApp && oauthReturnScheme === "pudding-dev") {
    console.info("[electron] using macOS pudding-dev OAuth relay");
    return;
  }
  try {
    let registered = false;
    if (process.defaultApp) {
      const scriptPath = path.resolve(process.argv[1] || path.join(repoRoot, "electron", "main.cjs"));
      registered = app.setAsDefaultProtocolClient(oauthReturnScheme, process.execPath, [scriptPath]);
    } else {
      registered = app.setAsDefaultProtocolClient(oauthReturnScheme);
    }
    if (!registered) {
      console.warn(`[electron] oauth return protocol registration failed scheme=${oauthReturnScheme}`);
    }
  } catch (error) {
    console.warn("[electron] register oauth return protocol failed", error);
  }
}

function captureOAuthReturnArgs(args) {
  for (const value of Array.isArray(args) ? args : []) {
    if (typeof value === "string" && value.startsWith(`${oauthReturnScheme}://`)) {
      handleOAuthReturnURL(value);
    }
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
  const window = getMainWindow();
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
    return {
      provider: decodeURIComponent(url.pathname.slice("/connected/".length)),
      ticket: url.searchParams.get("ticket") || "",
      state: url.searchParams.get("state") || "",
      error: url.searchParams.get("error") || "",
    };
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

function broadcastCredentialState(context, state) {
  broadcastToTrustedRenderers("pudding:browser:credential-state", {
    ...state,
    sessionID: context.sessionID,
    tabID: context.tabID,
  });
}

function broadcastCredentialsChanged() {
  broadcastToTrustedRenderers("pudding:browser:credentials-changed", { updatedAt: new Date().toISOString() });
}

function emptyBrowserCredentialState() {
  const availability = browserCredentialVault.availability();
  return { ...availability, origin: "", formDetected: false, credentials: [], prompt: null };
}

function browserCredentialContextIsBlank(context) {
  try {
    const url = new URL(context?.url || "");
    return url.protocol !== "https:" && !(url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname));
  } catch {
    return true;
  }
}

function credentialPayloadMatchesContext(payload, context) {
  try {
    return String(payload?.origin || "") === new URL(context?.url || "").origin;
  } catch {
    return false;
  }
}

function sendCredentialFill(request, credential) {
  const context = browserHost.credentialContext(request);
  if (new URL(context.url).origin !== credential.origin || !context.webContentsID) {
    throw new Error("browser credential origin mismatch");
  }
  const requestID = `credential_fill_${crypto.randomUUID().replaceAll("-", "")}`;
  return new Promise((resolve, reject) => {
    let settled = false;
    const complete = (result) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      browserCredentialFillWaiters.delete(requestID);
      if (result.ok) {
        resolve(result);
      } else {
        reject(new Error(result.reason || "browser credential fill failed"));
      }
    };
    const timer = setTimeout(() => complete({ ok: false, reason: "credential_fill_timeout" }), 2_000);
    timer.unref?.();
    browserCredentialFillWaiters.set(requestID, { complete, webContentsID: context.webContentsID });
    try {
      browserHost.sendCredentialMessage(request, "pudding:browser:credential-fill", {
        requestID,
        origin: credential.origin,
        username: credential.username,
        password: credential.password,
      });
    } catch (error) {
      browserCredentialFillWaiters.delete(requestID);
      clearTimeout(timer);
      settled = true;
      reject(error);
    }
  });
}

function requestBrowserAutomationLifecycle(phase, event, target) {
  const channel = `pudding:browser:automation-${phase}`;
  if (event?.action !== "click") {
    broadcastToTrustedRenderers(channel, event);
    return undefined;
  }
  const sender = target?.hostWebContents;
  if (!sender || sender.isDestroyed() || !isTrustedRendererURL(sender.getURL())) {
    if (phase === "end") {
      broadcastToTrustedRenderers(channel, event);
    }
    return false;
  }
  const requestID = `browser_automation_${crypto.randomUUID().replaceAll("-", "")}`;
  return new Promise((resolve) => {
    let settled = false;
    const complete = (ok) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      browserAutomationLifecycleWaiters.delete(requestID);
      resolve(Boolean(ok));
    };
    const timer = setTimeout(() => complete(false), 1_500);
    timer.unref?.();
    browserAutomationLifecycleWaiters.set(requestID, { complete, senderID: sender.id });
    try {
      sender.send(channel, { ...event, requestID });
    } catch {
      complete(false);
    }
  });
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

ipcMain.handle("pudding:desktop:reveal-path", async (event, rawPath) => {
  assertTrustedSender(event);
  const target = String(rawPath || "").trim();
  if (!path.isAbsolute(target)) {
    return false;
  }
  let info;
  try {
    info = await fsp.stat(target);
  } catch {
    return false;
  }
  if (info.isDirectory()) {
    return (await shell.openPath(target)) === "";
  }
  shell.showItemInFolder(target);
  return true;
});

ipcMain.handle("pudding:desktop:open-system-terminal", async (event, rawPath) => {
  assertTrustedSender(event);
  const target = String(rawPath || "").trim();
  if (!path.isAbsolute(target)) {
    return false;
  }
  let info;
  try {
    info = await fsp.stat(target);
  } catch {
    return false;
  }
  if (!info.isDirectory()) {
    return false;
  }
  return openSystemTerminal(target);
});

ipcMain.handle("pudding:desktop:get-home-directory", (event) => {
  assertTrustedSender(event);
  return os.homedir();
});

ipcMain.handle("pudding:desktop:create-mobile-pairing", async (event) => {
  assertTrustedSender(event);
  const token = await readUsableToken();
  if (!token) {
    throw new Error("pudding daemon is unavailable");
  }
  return mobileAccessBridge.createPairing(token);
});

ipcMain.handle("pudding:desktop:application-identity", async (event, rawAppID) => {
  assertTrustedSender(event);
  const appID = String(rawAppID || "").trim();
  if (!isValidBundleID(appID)) {
    return null;
  }
  try {
    const identity = await computerUseHost.applicationIdentity({ bundleID: appID });
    const iconPNGBase64 = String(identity?.iconPNGBase64 || "").trim();
    return {
      appID,
      name: String(identity?.name || appID),
      iconURL: iconPNGBase64 ? `data:image/png;base64,${iconPNGBase64}` : "",
    };
  } catch {
    return null;
  }
});

ipcMain.handle("pudding:desktop:permissions:get", (event) => {
  assertTrustedSender(event);
  return desktopPermissionController.refresh({ restartComputerUse: true });
});

ipcMain.handle("pudding:desktop:permissions:request", (event, permission) => {
  assertTrustedSender(event);
  return desktopPermissionController.request(permission);
});

ipcMain.handle("pudding:desktop:permissions:open-settings", async (event, permission) => {
  assertTrustedSender(event);
  return openDesktopPermissionSettings(permission);
});

ipcMain.handle("pudding:desktop:computer-use-permission-guide:get", (event) => {
  assertTrustedSender(event);
  return computerUsePermissionCoordinator.currentGuide();
});

ipcMain.handle("pudding:desktop:computer-use-permission-guide:deny", (event, requestID) => {
  assertTrustedSender(event);
  return computerUsePermissionCoordinator.deny(requestID);
});

ipcMain.handle("pudding:desktop:restart", (event) => {
  assertTrustedSender(event);
  app.relaunch();
  app.quit();
  return true;
});

ipcMain.handle("pudding:desktop:editor-context-menu", (event, request) => {
  const window = assertTrustedSender(event);
  const selectionText = String(request?.selectionText || "").slice(0, 16 * 1024);
  let command = null;
  const template = buildEditContextMenuTemplate(event.sender, {
    editFlags: {
      canCopy: Boolean(request?.canCopy),
      canCut: Boolean(request?.canCut),
      canDelete: Boolean(request?.canDelete),
      canPaste: true,
      canRedo: Boolean(request?.canRedo),
      canSelectAll: Boolean(request?.canSelectAll),
      canUndo: Boolean(request?.canUndo),
    },
    isEditable: true,
    selectionText,
  }, nativeMenuText, {
    platform: process.platform,
    openExternal: (url) => void shell.openExternal(url),
    runEditCommand: (next) => {
      command = next;
    },
  });
  if (template.length === 0 || window.isDestroyed()) {
    return null;
  }
  return new Promise((resolve) => {
    Menu.buildFromTemplate(template).popup({
      window,
      callback: () => resolve(command),
    });
  });
});

ipcMain.handle("pudding:desktop:set-locale", (event, locale) => {
  assertTrustedSender(event);
  return setShellLocale(locale);
});

ipcMain.handle("pudding:desktop:update:get-state", (event) => {
  assertTrustedSender(event);
  return updateManager.getState();
});

ipcMain.handle("pudding:desktop:update:download", async (event) => {
  assertTrustedSender(event);
  return updateManager.download();
});

ipcMain.handle("pudding:desktop:update:activate", async (event) => {
  assertTrustedSender(event);
  return activateUpdate();
});

ipcMain.handle("pudding:desktop:update:set-preview", async (event, enabled) => {
  assertTrustedSender(event);
  const previous = updateManager.getState().receivePreviewUpdates;
  const next = updateManager.setReceivePreviewUpdates(Boolean(enabled));
  try {
    writePreviewUpdatePreference(previewUpdatePreferencePath(), next.receivePreviewUpdates);
  } catch (error) {
    updateManager.setReceivePreviewUpdates(previous);
    throw error;
  }
  if (next.receivePreviewUpdates !== previous && next.status === updateStatuses.idle) {
    void updateManager.check(false);
  }
  return next;
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

ipcMain.handle("pudding:project-file:watch", (event, request) => {
  assertTrustedSender(event);
  return projectFileWatcher.subscribe(event.sender, request);
});

ipcMain.handle("pudding:project-file:unwatch", (event, request) => {
  assertTrustedSender(event);
  return projectFileWatcher.unsubscribe(event.sender, request);
});

ipcMain.handle("pudding:browser:ensure", (event, request) => {
  assertTrustedSender(event);
  return browserHost.ensure(untrustedBrowserRequest(request));
});

ipcMain.handle("pudding:browser:resolve-favicon", (event, request) => {
  assertTrustedSender(event);
  return resolveCachedBrowserFavicon(request);
});

ipcMain.handle("pudding:browser:webview-register", (event, request) => {
  assertTrustedSender(event);
  const target = webContents.fromId(Number(request?.webContentsID));
  if (!target || target.getType() !== "webview" || target.hostWebContents !== event.sender) {
    throw new Error("browser webview target not found");
  }
  return browserHost.registerWebContents(untrustedBrowserRequest(request), target);
});

ipcMain.handle("pudding:browser:automation-lifecycle-complete", (event, request) => {
  assertTrustedSender(event);
  const requestID = String(request?.requestID || "").trim();
  const waiter = browserAutomationLifecycleWaiters.get(requestID);
  if (!waiter || waiter.senderID !== event.sender.id) {
    return false;
  }
  waiter.complete(Boolean(request?.ok));
  return true;
});

ipcMain.handle("pudding:browser:load-url", (event, request) => {
  assertTrustedSender(event);
  return browserHost.loadURL(untrustedBrowserRequest(request));
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

ipcMain.handle("pudding:browser:read-selection", (event, request) => {
  assertTrustedSender(event);
  return browserHost.readSelection(request || {});
});

ipcMain.handle("pudding:browser:find", (event, request) => {
  assertTrustedSender(event);
  return browserHost.findInPage(untrustedBrowserRequest(request));
});

ipcMain.handle("pudding:browser:stop-find", (event, request) => {
  assertTrustedSender(event);
  return browserHost.stopFindInPage(untrustedBrowserRequest(request));
});

ipcMain.handle("pudding:browser:get-zoom", (event, request) => {
  assertTrustedSender(event);
  return browserHost.getZoom(untrustedBrowserRequest(request));
});

ipcMain.handle("pudding:browser:zoom", (event, request) => {
  assertTrustedSender(event);
  return browserHost.zoom(untrustedBrowserRequest(request));
});

ipcMain.handle("pudding:browser:print", (event, request) => {
  assertTrustedSender(event);
  return browserHost.print(untrustedBrowserRequest(request));
});

ipcMain.on("pudding:browser:selection-changed", (event, payload) => {
  const selection = browserHost.noteSelection(event.sender, payload);
  if (selection) {
    broadcastToTrustedRenderers("pudding:browser:selection-updated", selection);
  }
});

ipcMain.on("pudding:browser:credential-form", (event, payload) => {
  const context = browserHost.contextForWebContents(event.sender);
  if (!context) {
    return;
  }
  void browserCredentials
    .noteForm(context, Boolean(payload?.detected))
    .then((state) => broadcastCredentialState(context, state))
    .catch((error) => {
      if (!browserCredentialContextIsBlank(context)) {
        console.warn("[electron] browser credential form detection failed", error);
      }
    });
});

ipcMain.on("pudding:browser:credential-focus", (event, payload) => {
  const context = browserHost.contextForWebContents(event.sender);
  if (!context || !credentialPayloadMatchesContext(payload, context)) {
    return;
  }
  browserHost.discardCredentialUserGesture(event.sender);
  void browserCredentials
    .state(context)
    .then((state) => {
      browserHost.sendCredentialMessage(
        { sessionID: context.sessionID, tabID: context.tabID },
        "pudding:browser:credential-suggestions",
        {
          origin: state.origin,
          credentials: state.available ? state.credentials : [],
          title: nativeText(shellLocale, "browserAccountsForSite"),
          manageLabel: nativeText(shellLocale, "browserManagePasswords"),
          dark: themeState().resolved === "dark",
        },
      );
    })
    .catch((error) => console.warn("[electron] browser credential suggestions failed", error));
});

ipcMain.on("pudding:browser:credential-fill-request", (event, payload) => {
  const context = browserHost.contextForWebContents(event.sender);
  if (
    !context ||
    !credentialPayloadMatchesContext(payload, context) ||
    !browserHost.consumeCredentialUserGesture(event.sender)
  ) {
    return;
  }
  const request = { sessionID: context.sessionID, tabID: context.tabID };
  void browserCredentials
    .fill(context, payload?.credentialID)
    .then((credential) => sendCredentialFill(request, credential))
    .catch((error) => console.warn("[electron] browser credential fill request failed", error));
});

ipcMain.on("pudding:browser:credential-manage-request", (event) => {
  const context = browserHost.contextForWebContents(event.sender);
  if (!context || !browserHost.consumeCredentialUserGesture(event.sender)) {
    return;
  }
  broadcastToTrustedRenderers("pudding:browser:credential-manage", {
    sessionID: context.sessionID,
    tabID: context.tabID,
  });
});

ipcMain.on("pudding:browser:credential-candidate", (event, payload) => {
  const context = browserHost.contextForWebContents(event.sender);
  if (!context) {
    return;
  }
  void browserCredentials
    .noteCandidate(context, payload)
    .then(() => browserCredentials.state(context))
    .then((state) => broadcastCredentialState(context, state))
    .catch((error) => console.warn("[electron] browser credential candidate rejected", error));
});

ipcMain.on("pudding:browser:credential-fill-result", (event, payload) => {
  const requestID = String(payload?.requestID || "").trim();
  const waiter = browserCredentialFillWaiters.get(requestID);
  if (!waiter || waiter.webContentsID !== event.sender.id) {
    return;
  }
  browserCredentialFillWaiters.delete(requestID);
  waiter.complete({
    ok: Boolean(payload?.ok),
    reason: String(payload?.reason || ""),
  });
});

ipcMain.handle("pudding:browser:credentials:get-state", async (event, request) => {
  assertTrustedSender(event);
  const context = browserHost.credentialContextIfLive(untrustedBrowserRequest(request));
  if (!context || browserCredentialContextIsBlank(context)) {
    return emptyBrowserCredentialState();
  }
  return browserCredentials.state(context);
});

ipcMain.handle("pudding:browser:credentials:list", async (event) => {
  assertTrustedSender(event);
  const availability = browserCredentialVault.availability();
  if (!availability.available) {
    return { ...availability, credentials: [], neverSaveOrigins: [] };
  }
  return { ...availability, ...(await browserCredentialVault.list()) };
});

ipcMain.handle("pudding:browser:credentials:save", async (event, request) => {
  assertTrustedSender(event);
  const context = browserHost.credentialContext(untrustedBrowserRequest(request));
  const credential = await browserCredentials.commit(context, request?.pendingID);
  broadcastCredentialsChanged();
  broadcastCredentialState(context, await browserCredentials.state(context));
  return credential;
});

ipcMain.handle("pudding:browser:credentials:dismiss", async (event, request) => {
  assertTrustedSender(event);
  const context = browserHost.credentialContext(untrustedBrowserRequest(request));
  await browserCredentials.dismiss(context, request?.pendingID, Boolean(request?.neverSave));
  broadcastCredentialsChanged();
  broadcastCredentialState(context, await browserCredentials.state(context));
});

ipcMain.handle("pudding:browser:credentials:delete", async (event, request) => {
  assertTrustedSender(event);
  await browserCredentialVault.delete(request?.credentialID);
  broadcastCredentialsChanged();
});

ipcMain.handle("pudding:browser:credentials:clear", async (event) => {
  assertTrustedSender(event);
  await browserCredentialVault.clear();
  broadcastCredentialsChanged();
});

ipcMain.handle("pudding:browser:credentials:allow-origin", async (event, request) => {
  assertTrustedSender(event);
  await browserCredentialVault.setNeverSave(request?.origin, false);
  broadcastCredentialsChanged();
});

ipcMain.handle("pudding:browser:credentials:import-chrome", async (event) => {
  assertTrustedSender(event);
  const window = BrowserWindow.fromWebContents(event.sender) || getMainWindow();
  const result = await dialog.showOpenDialog(window, {
    title: nativeText(shellLocale, "browserImportPasswordsTitle"),
    properties: ["openFile"],
    filters: [{ name: "CSV", extensions: ["csv"] }],
  });
  if (result.canceled || result.filePaths.length !== 1) {
    return { canceled: true, imported: 0, updated: 0, unchanged: 0, skipped: 0, sourceDeleted: false };
  }
  const sourcePath = result.filePaths[0];
  const csv = await fsp.readFile(sourcePath, "utf8");
  const parsed = parseChromePasswordCSV(csv);
  const imported = await browserCredentialVault.importRecords(parsed.records);
  broadcastCredentialsChanged();
  const deleteResult = await dialog.showMessageBox(window, {
    type: "warning",
    title: nativeText(shellLocale, "browserDeletePasswordCSVTitle"),
    message: nativeText(shellLocale, "browserDeletePasswordCSVMessage"),
    detail: nativeText(shellLocale, "browserDeletePasswordCSVDetail"),
    buttons: [nativeText(shellLocale, "browserDeletePasswordCSVDelete"), nativeText(shellLocale, "browserDeletePasswordCSVKeep")],
    defaultId: 0,
    cancelId: 1,
    noLink: true,
  });
  let sourceDeleted = false;
  if (deleteResult.response === 0) {
    await fsp.rm(sourcePath);
    sourceDeleted = true;
  }
  return { canceled: false, ...imported, skipped: parsed.skipped, sourceDeleted };
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

async function ensureDaemon(browserBridge, computerBridge) {
  if (daemonStartupPromise) {
    return daemonStartupPromise;
  }
  const attempt = startOrAttachDaemon(browserBridge, computerBridge);
  daemonStartupPromise = attempt;
  try {
    return await attempt;
  } finally {
    if (daemonStartupPromise === attempt) {
      daemonStartupPromise = null;
    }
  }
}

async function startOrAttachDaemon(browserBridge, computerBridge) {
  const attachedToken = await readUsableToken();
  if (attachedToken) {
    console.info(`[electron] attached to daemon addr=${daemonAddr}`);
    return attachedToken;
  }

  const daemonBin = resolveDaemonBinary();
  if (!daemonBin) {
    throw new Error("puddingd binary not found. Run `make desktop-dev` so the dev binary is built first.");
  }

  console.info(`[electron] starting managed daemon addr=${daemonAddr} binary=${daemonBin}`);
  const child = spawn(daemonBin, ["-addr", daemonAddr], {
    cwd: repoRoot,
    env: {
      ...process.env,
      PUDDING_ELECTRON_MANAGED: "1",
      PUDDING_ELECTRON_BROWSER_BRIDGE_URL: browserBridge?.url || "",
      PUDDING_ELECTRON_BROWSER_BRIDGE_TOKEN: browserBridge?.token || "",
      PUDDING_ELECTRON_COMPUTER_BRIDGE_URL: computerBridge?.url || "",
      PUDDING_ELECTRON_COMPUTER_BRIDGE_TOKEN: computerBridge?.token || "",
    },
    stdio: ["pipe", "pipe", "pipe"],
  });
  daemonProcess = child;
  child.stdin.on("error", () => {});

  let ready = false;
  let stderrTail = "";
  child.stdout.on("data", (chunk) => process.stdout.write(`[puddingd] ${chunk}`));
  child.stderr.on("data", (chunk) => {
    stderrTail = `${stderrTail}${chunk}`.slice(-4_000);
    process.stderr.write(`[puddingd] ${chunk}`);
  });
  const exited = new Promise((_, reject) => {
    child.once("error", (error) => {
      if (daemonProcess === child) {
        daemonProcess = null;
      }
      reject(new Error(`failed to start puddingd: ${error.message}`));
    });
    child.once("exit", (code, signal) => {
      if (daemonProcess === child) {
        daemonProcess = null;
      }
      const error = daemonExitError(code, signal, stderrTail);
      if (!ready) {
        reject(error);
        return;
      }
      if (!quitting) {
        console.error(`[electron] ${error.message}`);
        app.quit();
      } else {
        console.info(`[electron] managed daemon exited code=${code} signal=${signal}`);
      }
    });
  });

  try {
    const token = await Promise.race([waitForDaemon(), exited]);
    if (child.exitCode !== null) {
      throw daemonExitError(child.exitCode, child.signalCode, stderrTail);
    }
    ready = true;
    console.info(`[electron] managed daemon ready addr=${daemonAddr}`);
    return token;
  } catch (error) {
    if (daemonProcess === child && child.exitCode === null) {
      child.kill("SIGTERM");
    }
    throw error;
  }
}

function daemonExitError(code, signal, stderr) {
  const detail = String(stderr || "").trim();
  const suffix = detail ? `: ${detail}` : "";
  return new Error(`managed daemon exited code=${code} signal=${signal}${suffix}`);
}

async function readUsableToken() {
  try {
    const token = await readDaemonToken();
    if (!token) {
      return "";
    }
    if (await probePuddingDaemon(apiBase, token)) {
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
      if (token && (await probePuddingDaemon(apiBase, token))) {
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
  return path.join(puddingHomePath(), "daemon.token");
}

function puddingHomePath() {
  const defaultHome = app.isPackaged ? ".pudding" : ".pudding-dev";
  return (process.env.PUDDING_HOME || path.join(os.homedir(), defaultHome)).trim();
}

function previewUpdatePreferencePath() {
  return path.join(puddingHomePath(), "config", "desktop-preferences.json");
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

function resolveComputerUseHelperBinary() {
  if (process.platform !== "darwin") {
    return "";
  }
  return [
    process.env.PUDDING_COMPUTER_USE_HELPER_BIN,
    path.join(repoRoot, "bin", "Pudding Computer Use.app", "Contents", "MacOS", "PuddingComputerUseHelper"),
  ]
    .filter(Boolean)
    .find((candidate) => fs.existsSync(candidate)) || "";
}

async function prepareForUpdateInstall() {
  quitting = true;
  updateManager.stop();
  const window = getMainWindow();
  if (window) {
    saveWindowState(window);
  }
  await stopDesktopResources();
  // quitAndInstall owns the following quit/relaunch sequence. Do not turn it
  // into the ordinary delayed app.quit path in the before-quit handler.
  shutdownComplete = true;
}

async function stopDesktopResources() {
  const results = await Promise.allSettled([
    stopManagedDaemonAndWait(),
    browserBridgeServer.stop(),
    computerUseBridgeServer.stop(),
    computerUseHost.stop(),
    mobileAccessBridge.stop(),
  ]);
  const failure = results.find((result) => result.status === "rejected");
  if (failure) {
    throw failure.reason;
  }
}

function stopManagedDaemonAndWait(graceMs = 7_000) {
  const child = daemonProcess;
  if (!child || child.exitCode !== null) {
    return Promise.resolve();
  }
  return new Promise((resolve, reject) => {
    let forceTimer = null;
    let failureTimer = null;
    let settled = false;
    const cleanup = () => {
      clearTimeout(forceTimer);
      clearTimeout(failureTimer);
      child.off("exit", finish);
    };
    const finish = () => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      resolve();
    };
    child.once("exit", finish);
    child.kill("SIGTERM");
    forceTimer = setTimeout(() => {
      if (child.exitCode !== null) {
        finish();
        return;
      }
      child.kill("SIGKILL");
      failureTimer = setTimeout(() => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        reject(new Error("managed daemon did not stop before shutdown"));
      }, 1_000);
    }, graceMs);
  });
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

function untrustedBrowserRequest(request) {
  return { ...(request || {}), _fileAuthorized: false, fileRoot: "", fileRoots: [] };
}

function isAllowedExternalURL(rawURL) {
  try {
    const url = new URL(rawURL);
    return url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:";
  } catch {
    return false;
  }
}

function isValidBundleID(value) {
  return value.length <= 255 && /^[a-z0-9_-]+(?:\.[a-z0-9_-]+)+$/i.test(value);
}

function openAllowedExternalURL(rawURL) {
  const url = String(rawURL || "").trim();
  if (!isAllowedExternalURL(url)) {
    return false;
  }
  void shell.openExternal(url).catch((error) => {
    console.warn("[electron] open external URL failed", error);
  });
  return true;
}

function resolveCachedBrowserFavicon(request = {}) {
  const url = String(request.url || "").trim();
  const pageURL = String(request.pageURL || "").trim();
  const key = `${pageURL}\n${url}`;
  const cached = browserFaviconCache.get(key);
  if (cached) {
    return cached;
  }
  const pending = resolveBrowserFavicon({
    url,
    pageURL,
    fetch: (resource, init) => session.fromPartition(managedBrowserPartition).fetch(resource, init),
    nativeImage,
  }).then((resolvedURL) => {
    if (!resolvedURL && browserFaviconCache.get(key) === pending) {
      browserFaviconCache.delete(key);
    }
    return resolvedURL;
  });
  browserFaviconCache.set(key, pending);
  if (browserFaviconCache.size > 128) {
    browserFaviconCache.delete(browserFaviconCache.keys().next().value);
  }
  return pending;
}

function normalizeUpdateFeedURL(rawURL) {
  const clean = String(rawURL || "").trim();
  if (!clean) {
    return "";
  }
  try {
    const url = new URL(clean);
    const loopback = url.hostname === "127.0.0.1" || url.hostname === "localhost" || url.hostname === "[::1]";
    if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) {
      throw new Error("update feed must use HTTPS or loopback HTTP");
    }
    return url.toString().replace(/\/$/, "");
  } catch (error) {
    console.warn("[electron] ignoring invalid PUDDING_UPDATE_FEED_URL", error);
    return "";
  }
}

function normalizeUpdatePageURL(rawURL) {
  const clean = String(rawURL || "").trim();
  if (!clean) {
    return "";
  }
  try {
    const url = new URL(clean);
    const loopback = url.hostname === "127.0.0.1" || url.hostname === "localhost" || url.hostname === "[::1]";
    if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) {
      throw new Error("update download page must use HTTPS or loopback HTTP");
    }
    return url.toString();
  } catch (error) {
    console.warn("[electron] ignoring invalid PUDDING_UPDATE_DOWNLOAD_URL", error);
    return "";
  }
}

function normalizeOptionalBoolean(value) {
  const normalized = String(value || "").trim().toLowerCase();
  if (normalized === "1" || normalized === "true") {
    return true;
  }
  if (normalized === "0" || normalized === "false") {
    return false;
  }
  return null;
}

function updateDownloadPageURL(state) {
  if (releasePageOverride) {
    return releasePageOverride;
  }
  const version = String(state?.version || "").trim();
  if (state?.receivePreviewUpdates && /^\d+\.\d+\.\d+-beta\.\d+$/.test(version)) {
    return `${repositoryURL}/releases/tag/v${encodeURIComponent(version)}`;
  }
  return releasePageURL;
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
