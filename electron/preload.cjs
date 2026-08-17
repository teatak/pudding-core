const { contextBridge, ipcRenderer, webUtils } = require("electron");

const oauthReturnListeners = new Set();
const pendingOAuthReturnPayloads = [];
ipcRenderer.on("pudding:oauth:connected", (_event, payload) => {
  if (oauthReturnListeners.size === 0) {
    pendingOAuthReturnPayloads.push(payload);
    if (pendingOAuthReturnPayloads.length > 8) {
      pendingOAuthReturnPayloads.shift();
    }
    return;
  }
  for (const listener of oauthReturnListeners) {
    listener(payload);
  }
});

contextBridge.exposeInMainWorld("puddingElectronTheme", {
  getState: () => ipcRenderer.invoke("pudding:theme:get"),
  setTheme: (theme) => ipcRenderer.invoke("pudding:theme:set", theme),
  onUpdated: (listener) => {
    const wrapped = (_event, state) => listener(state);
    ipcRenderer.on("pudding:theme:updated", wrapped);
    return () => ipcRenderer.off("pudding:theme:updated", wrapped);
  },
});

contextBridge.exposeInMainWorld("puddingElectronShell", {
  isFullscreen: () => ipcRenderer.invoke("pudding:shell:is-fullscreen"),
  onFullscreenChanged: (listener) => {
    const wrapped = (_event, fullscreen) => listener(Boolean(fullscreen));
    ipcRenderer.on("pudding:shell:fullscreen", wrapped);
    return () => ipcRenderer.off("pudding:shell:fullscreen", wrapped);
  },
});

contextBridge.exposeInMainWorld("puddingElectronDesktop", {
  getDroppedFilePath: (file) => {
    if (!file || !webUtils?.getPathForFile) {
      return "";
    }
    try {
      return webUtils.getPathForFile(file) || "";
    } catch {
      return "";
    }
  },
  getHomeDirectory: () => ipcRenderer.invoke("pudding:desktop:get-home-directory"),
	getApplicationIdentity: (appID) => ipcRenderer.invoke("pudding:desktop:application-identity", appID),
	getDesktopPermissions: () => ipcRenderer.invoke("pudding:desktop:permissions:get"),
	requestDesktopPermission: (permission) => ipcRenderer.invoke("pudding:desktop:permissions:request", permission),
	openDesktopPermissionSettings: (permission) => ipcRenderer.invoke("pudding:desktop:permissions:open-settings", permission),
  getComputerUsePermissionGuide: () => ipcRenderer.invoke("pudding:desktop:computer-use-permission-guide:get"),
  denyComputerUsePermissionGuide: (requestID) => ipcRenderer.invoke("pudding:desktop:computer-use-permission-guide:deny", requestID),
  restartDesktopApp: () => ipcRenderer.invoke("pudding:desktop:restart"),
  onDesktopPermissionsUpdated: (listener) => {
    const wrapped = (_event, state) => listener(state);
    ipcRenderer.on("pudding:desktop:permissions-updated", wrapped);
    return () => ipcRenderer.off("pudding:desktop:permissions-updated", wrapped);
  },
  onComputerUsePermissionGuide: (listener) => {
    const wrapped = (_event, guide) => listener(guide);
    ipcRenderer.on("pudding:desktop:computer-use-permission-guide", wrapped);
    return () => ipcRenderer.off("pudding:desktop:computer-use-permission-guide", wrapped);
  },
  openExternal: (url) => ipcRenderer.invoke("pudding:desktop:open-external", url),
  openSystemTerminal: (path) => ipcRenderer.invoke("pudding:desktop:open-system-terminal", path),
  revealPath: (path) => ipcRenderer.invoke("pudding:desktop:reveal-path", path),
  showEditorContextMenu: (request) => ipcRenderer.invoke("pudding:desktop:editor-context-menu", request),
  setLocale: (locale) => ipcRenderer.invoke("pudding:desktop:set-locale", locale),
  getUpdateState: () => ipcRenderer.invoke("pudding:desktop:update:get-state"),
  setPreviewUpdatesEnabled: (enabled) => ipcRenderer.invoke("pudding:desktop:update:set-preview", enabled),
  downloadUpdate: () => ipcRenderer.invoke("pudding:desktop:update:download"),
  activateUpdate: () => ipcRenderer.invoke("pudding:desktop:update:activate"),
  onUpdateState: (listener) => {
    const wrapped = (_event, state) => listener(state);
    ipcRenderer.on("pudding:desktop:update-state", wrapped);
    return () => ipcRenderer.off("pudding:desktop:update-state", wrapped);
  },
  onMenuCommand: (listener) => {
    const wrapped = (_event, command) => listener(command);
    ipcRenderer.on("pudding:desktop:menu-command", wrapped);
    return () => ipcRenderer.off("pudding:desktop:menu-command", wrapped);
  },
  onOAuthConnected: (listener) => {
    oauthReturnListeners.add(listener);
    for (const payload of pendingOAuthReturnPayloads.splice(0)) {
      listener(payload);
    }
    return () => oauthReturnListeners.delete(listener);
  },
  pickDirectories: (options) => ipcRenderer.invoke("pudding:desktop:pick-directories", options),
});

contextBridge.exposeInMainWorld("puddingElectronProjectFiles", {
  watch: (request) => ipcRenderer.invoke("pudding:project-file:watch", request),
  unwatch: (request) => ipcRenderer.invoke("pudding:project-file:unwatch", request),
  onChanged: (listener) => {
    const wrapped = (_event, change) => listener(change);
    ipcRenderer.on("pudding:project-file:changed", wrapped);
    return () => ipcRenderer.off("pudding:project-file:changed", wrapped);
  },
});

contextBridge.exposeInMainWorld("puddingElectronBrowser", {
  ensure: (request) => ipcRenderer.invoke("pudding:browser:ensure", request),
  resolveFavicon: (request) => ipcRenderer.invoke("pudding:browser:resolve-favicon", request),
  registerWebview: (request) => ipcRenderer.invoke("pudding:browser:webview-register", request),
  loadURL: (request) => ipcRenderer.invoke("pudding:browser:load-url", request),
  back: (request) => ipcRenderer.invoke("pudding:browser:back", request),
  forward: (request) => ipcRenderer.invoke("pudding:browser:forward", request),
  reload: (request) => ipcRenderer.invoke("pudding:browser:reload", request),
  readSelection: (request) => ipcRenderer.invoke("pudding:browser:read-selection", request),
  findInPage: (request) => ipcRenderer.invoke("pudding:browser:find", request),
  stopFindInPage: (request) => ipcRenderer.invoke("pudding:browser:stop-find", request),
  getZoom: (request) => ipcRenderer.invoke("pudding:browser:get-zoom", request),
  zoom: (request) => ipcRenderer.invoke("pudding:browser:zoom", request),
  print: (request) => ipcRenderer.invoke("pudding:browser:print", request),
  listTabs: (request) => ipcRenderer.invoke("pudding:browser:list-tabs", request),
  closeTab: (request) => ipcRenderer.invoke("pudding:browser:close-tab", request),
  closeSession: (request) => ipcRenderer.invoke("pudding:browser:close-session", request),
  getCredentialState: (request) => ipcRenderer.invoke("pudding:browser:credentials:get-state", request),
  listCredentials: () => ipcRenderer.invoke("pudding:browser:credentials:list"),
  saveCredential: (request) => ipcRenderer.invoke("pudding:browser:credentials:save", request),
  dismissCredential: (request) => ipcRenderer.invoke("pudding:browser:credentials:dismiss", request),
  deleteCredential: (request) => ipcRenderer.invoke("pudding:browser:credentials:delete", request),
  clearCredentials: () => ipcRenderer.invoke("pudding:browser:credentials:clear"),
  allowCredentialOrigin: (request) => ipcRenderer.invoke("pudding:browser:credentials:allow-origin", request),
  importChromePasswords: () => ipcRenderer.invoke("pudding:browser:credentials:import-chrome"),
  onUpdated: (listener) => {
    const wrapped = (_event, snapshot) => listener(snapshot);
    ipcRenderer.on("pudding:browser:updated", wrapped);
    return () => ipcRenderer.off("pudding:browser:updated", wrapped);
  },
  onCursor: (listener) => {
    const wrapped = (_event, cursor) => listener(cursor);
    ipcRenderer.on("pudding:browser:cursor", wrapped);
    return () => ipcRenderer.off("pudding:browser:cursor", wrapped);
  },
  onSelectionChanged: (listener) => {
    const wrapped = (_event, selection) => listener(selection);
    ipcRenderer.on("pudding:browser:selection-updated", wrapped);
    return () => ipcRenderer.off("pudding:browser:selection-updated", wrapped);
  },
  onFoundInPage: (listener) => {
    const wrapped = (_event, result) => listener(result);
    ipcRenderer.on("pudding:browser:found-in-page", wrapped);
    return () => ipcRenderer.off("pudding:browser:found-in-page", wrapped);
  },
  onInteraction: (listener) => {
    const wrapped = (_event, interaction) => listener(interaction);
    ipcRenderer.on("pudding:browser:interaction", wrapped);
    return () => ipcRenderer.off("pudding:browser:interaction", wrapped);
  },
  onAutomationStart: (listener) => {
    const wrapped = (_event, automation) => listener(automation);
    ipcRenderer.on("pudding:browser:automation-start", wrapped);
    return () => ipcRenderer.off("pudding:browser:automation-start", wrapped);
  },
  onAutomationEnd: (listener) => {
    const wrapped = (_event, automation) => listener(automation);
    ipcRenderer.on("pudding:browser:automation-end", wrapped);
    return () => ipcRenderer.off("pudding:browser:automation-end", wrapped);
  },
  onCredentialState: (listener) => {
    const wrapped = (_event, state) => listener(state);
    ipcRenderer.on("pudding:browser:credential-state", wrapped);
    return () => ipcRenderer.off("pudding:browser:credential-state", wrapped);
  },
  onCredentialsChanged: (listener) => {
    const wrapped = (_event, change) => listener(change);
    ipcRenderer.on("pudding:browser:credentials-changed", wrapped);
    return () => ipcRenderer.off("pudding:browser:credentials-changed", wrapped);
  },
  onCredentialManage: (listener) => {
    const wrapped = (_event, request) => listener(request);
    ipcRenderer.on("pudding:browser:credential-manage", wrapped);
    return () => ipcRenderer.off("pudding:browser:credential-manage", wrapped);
  },
  completeAutomationLifecycle: (request) => ipcRenderer.invoke("pudding:browser:automation-lifecycle-complete", request),
  onWebviewRequired: (listener) => {
    const wrapped = (_event, request) => listener(request);
    ipcRenderer.on("pudding:browser:webview-required", wrapped);
    return () => ipcRenderer.off("pudding:browser:webview-required", wrapped);
  },
});
