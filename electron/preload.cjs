const { contextBridge, ipcRenderer } = require("electron");

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

contextBridge.exposeInMainWorld("puddingElectronBrowser", {
  ensure: (request) => ipcRenderer.invoke("pudding:browser:ensure", request),
  registerWebview: (request) => ipcRenderer.invoke("pudding:browser:webview-register", request),
  resolveWebviewCapture: (response) => ipcRenderer.invoke("pudding:browser:webview-capture-result", response),
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
  onWebviewCaptureRequest: (listener) => {
    const wrapped = (_event, request) => listener(request);
    ipcRenderer.on("pudding:browser:webview-capture-request", wrapped);
    return () => ipcRenderer.off("pudding:browser:webview-capture-request", wrapped);
  },
});
