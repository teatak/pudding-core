const { contextBridge, ipcRenderer, webUtils } = require("electron");

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
  openExternal: (url) => ipcRenderer.invoke("pudding:desktop:open-external", url),
  setLocale: (locale) => ipcRenderer.invoke("pudding:desktop:set-locale", locale),
  getUpdateState: () => ipcRenderer.invoke("pudding:desktop:update:get-state"),
  setPreviewUpdatesEnabled: (enabled) => ipcRenderer.invoke("pudding:desktop:update:set-preview", enabled),
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
    const wrapped = (_event, payload) => listener(payload);
    ipcRenderer.on("pudding:oauth:connected", wrapped);
    return () => ipcRenderer.off("pudding:oauth:connected", wrapped);
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
  registerWebview: (request) => ipcRenderer.invoke("pudding:browser:webview-register", request),
  loadURL: (request) => ipcRenderer.invoke("pudding:browser:load-url", request),
  back: (request) => ipcRenderer.invoke("pudding:browser:back", request),
  forward: (request) => ipcRenderer.invoke("pudding:browser:forward", request),
  reload: (request) => ipcRenderer.invoke("pudding:browser:reload", request),
  listTabs: (request) => ipcRenderer.invoke("pudding:browser:list-tabs", request),
  closeTab: (request) => ipcRenderer.invoke("pudding:browser:close-tab", request),
  closeSession: (request) => ipcRenderer.invoke("pudding:browser:close-session", request),
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
  onAutomationStart: (listener) => {
    const wrapped = (_event, automation) => listener(automation);
    ipcRenderer.on("pudding:browser:automation-start", wrapped);
    return () => ipcRenderer.off("pudding:browser:automation-start", wrapped);
  },
  onWebviewRequired: (listener) => {
    const wrapped = (_event, request) => listener(request);
    ipcRenderer.on("pudding:browser:webview-required", wrapped);
    return () => ipcRenderer.off("pudding:browser:webview-required", wrapped);
  },
});
