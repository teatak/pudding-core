const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("puddingBrowserSmoke", {
  onWebviewRequired(callback) {
    ipcRenderer.on("pudding-browser-smoke:webview-required", (_event, request) => callback(request));
  },
  registerWebview(request, webContentsID) {
    ipcRenderer.send("pudding-browser-smoke:webview-register", { request, webContentsID });
  },
});
