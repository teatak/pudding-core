function hardenManagedBrowserWebview(
  event,
  webPreferences = {},
  params = {},
  managedPartition,
  trustedPreloadPath,
  allowsManagedSource,
) {
  const partition = String(managedPartition || "").trim();
  const requestedPartition = String(params.partition || webPreferences.partition || "").trim();
  const sourceURL = String(params.src || "").trim();
  const sourceAllowed = sourceURL === "about:blank"
    || (typeof allowsManagedSource === "function" && allowsManagedSource(sourceURL));
  if (!partition || requestedPartition !== partition || !sourceAllowed) {
    event?.preventDefault?.();
    return false;
  }

  delete webPreferences.preload;
  const preloadPath = String(trustedPreloadPath || "").trim();
  if (preloadPath) {
    webPreferences.preload = preloadPath;
  }
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
