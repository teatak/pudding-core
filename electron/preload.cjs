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
  attach: (request) => ipcRenderer.invoke("pudding:browser:attach", request),
  updateBounds: (request) => ipcRenderer.invoke("pudding:browser:update-bounds", request),
  detach: (request) => ipcRenderer.invoke("pudding:browser:detach", request),
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
});
