function hardenManagedBrowserWebview(event, webPreferences = {}, params = {}, managedPartition) {
  const partition = String(managedPartition || "").trim();
  const requestedPartition = String(params.partition || webPreferences.partition || "").trim();
  const sourceURL = String(params.src || "").trim();
  if (!partition || requestedPartition !== partition || sourceURL !== "about:blank") {
    event?.preventDefault?.();
    return false;
  }

  delete webPreferences.preload;
  webPreferences.additionalArguments = [];
  webPreferences.allowRunningInsecureContent = false;
  webPreferences.contextIsolation = true;
  webPreferences.enableBlinkFeatures = "";
  webPreferences.experimentalFeatures = false;
  webPreferences.javascript = true;
  webPreferences.navigateOnDragDrop = false;
  webPreferences.nodeIntegration = false;
  webPreferences.nodeIntegrationInSubFrames = false;
  webPreferences.nodeIntegrationInWorker = false;
  webPreferences.partition = partition;
  webPreferences.plugins = false;
  webPreferences.sandbox = true;
  webPreferences.webSecurity = true;
  webPreferences.webviewTag = false;
  delete params.preload;
  return true;
}

module.exports = { hardenManagedBrowserWebview };
