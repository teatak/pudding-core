const permissionSettingsURLs = Object.freeze({
  accessibility: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
  screenRecording: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
  camera: "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera",
  microphone: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone",
});

const mediaPermissions = new Set(["camera", "microphone"]);

class DesktopPermissionController {
  constructor(options) {
    this.computerUseHost = options.computerUseHost;
    this.systemPreferences = options.systemPreferences;
    this.platform = options.platform || process.platform;
    this.onStateChange = options.onStateChange || (() => {});
    this.state = unsupportedState();
  }

  currentState() {
    return this.state;
  }

  async refresh() {
    const next = await desktopPermissionState(
      this.computerUseHost,
      this.systemPreferences,
      this.platform,
    );
    return this.commit(next);
  }

  async request(permission) {
    const next = await requestDesktopPermission(
      this.computerUseHost,
      this.systemPreferences,
      permission,
      this.platform,
    );
    return this.commit(next);
  }

  commit(next) {
    const changed = !samePermissionState(this.state, next);
    this.state = next;
    if (changed) {
      this.onStateChange(next);
    }
    return next;
  }
}

async function desktopPermissionState(computerUseHost, systemPreferences, platform = process.platform) {
  if (platform !== "darwin") {
    return unsupportedState();
  }
  const computerUse = await computerUseHost.permissions();
  return {
    supported: true,
    accessibility: Boolean(computerUse?.accessibility),
    screenRecording: Boolean(computerUse?.screenRecording),
    camera: systemPreferences.getMediaAccessStatus("camera") === "granted",
    microphone: systemPreferences.getMediaAccessStatus("microphone") === "granted",
  };
}

async function requestDesktopPermission(computerUseHost, systemPreferences, permission, platform = process.platform) {
  if (platform !== "darwin") {
    throw new Error("macOS permissions are unavailable on this platform");
  }
  const normalized = String(permission || "").trim();
  if (!Object.hasOwn(permissionSettingsURLs, normalized)) {
    throw new Error("Unsupported macOS permission");
  }
  if (mediaPermissions.has(normalized)) {
    await systemPreferences.askForMediaAccess(normalized);
  } else {
    await computerUseHost.permissions({
      promptAccessibility: normalized === "accessibility",
      promptScreenRecording: normalized === "screenRecording",
    });
  }
  return desktopPermissionState(computerUseHost, systemPreferences, platform);
}

function desktopPermissionSettingsURL(permission, platform = process.platform) {
  if (platform !== "darwin") {
    return "";
  }
  return permissionSettingsURLs[String(permission || "").trim()] || "";
}

function unsupportedState() {
  return {
    supported: false,
    accessibility: false,
    screenRecording: false,
    camera: false,
    microphone: false,
  };
}

function samePermissionState(left, right) {
  return left.supported === right.supported &&
    left.accessibility === right.accessibility &&
    left.screenRecording === right.screenRecording &&
    left.camera === right.camera &&
    left.microphone === right.microphone;
}

module.exports = {
  DesktopPermissionController,
  desktopPermissionSettingsURL,
  desktopPermissionState,
  requestDesktopPermission,
};
