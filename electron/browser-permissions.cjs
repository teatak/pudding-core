const managedBrowserPartition = "persist:pudding-default";
const configuredSessions = new WeakSet();

function configureManagedBrowserPermissions(browserSession) {
  if (!browserSession || typeof browserSession !== "object") {
    throw new TypeError("managed browser session is required");
  }
  if (configuredSessions.has(browserSession)) {
    return;
  }

  browserSession.setPermissionCheckHandler(() => false);
  browserSession.setPermissionRequestHandler((_webContents, _permission, callback) => callback(false));
  browserSession.setDevicePermissionHandler(() => false);

  for (const eventName of ["select-hid-device", "select-serial-port", "select-usb-device"]) {
    browserSession.on(eventName, denyDeviceSelection);
  }
  configuredSessions.add(browserSession);
}

function denyDeviceSelection(event, ...args) {
  event?.preventDefault?.();
  const callback = args.findLast((value) => typeof value === "function");
  callback?.("");
}

module.exports = {
  configureManagedBrowserPermissions,
  managedBrowserPartition,
};
