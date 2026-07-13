const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const test = require("node:test");

const {
  configureManagedBrowserPermissions,
  managedBrowserPartition,
} = require("../browser-permissions.cjs");

class FakeSession extends EventEmitter {
  setPermissionCheckHandler(handler) {
    this.permissionCheckHandler = handler;
  }

  setPermissionRequestHandler(handler) {
    this.permissionRequestHandler = handler;
  }

  setDevicePermissionHandler(handler) {
    this.devicePermissionHandler = handler;
  }
}

test("managed browser denies remote content permissions and device selection", () => {
  const browserSession = new FakeSession();
  configureManagedBrowserPermissions(browserSession);
  configureManagedBrowserPermissions(browserSession);

  assert.equal(managedBrowserPartition, "persist:pudding-default");
  assert.equal(browserSession.permissionCheckHandler(), false);
  assert.equal(browserSession.devicePermissionHandler(), false);
  let permissionGranted = true;
  browserSession.permissionRequestHandler(undefined, "media", (granted) => {
    permissionGranted = granted;
  });
  assert.equal(permissionGranted, false);

  for (const eventName of ["select-hid-device", "select-serial-port", "select-usb-device"]) {
    let prevented = false;
    let selectedDevice = "device-1";
    browserSession.emit(
      eventName,
      { preventDefault: () => { prevented = true; } },
      {},
      (deviceID) => { selectedDevice = deviceID; },
    );
    assert.equal(prevented, true);
    assert.equal(selectedDevice, "");
    assert.equal(browserSession.listenerCount(eventName), 1);
  }
});
