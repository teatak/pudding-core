const permissionSettingsURLs = Object.freeze({
  accessibility: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
  screenRecording: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
  desktopScreenRecording: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
  camera: "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera",
  microphone: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone",
});

const mediaPermissions = new Set(["camera", "microphone"]);

async function desktopPermissionState(computerUseHost, systemPreferences, platform = process.platform) {
  if (platform !== "darwin") {
    return unsupportedState();
  }
  const computerUse = await computerUseHost.permissions();
  return {
    supported: true,
    accessibility: Boolean(computerUse?.accessibility),
    screenRecording: Boolean(computerUse?.screenRecording),
    desktopScreenRecording: systemPreferences.getMediaAccessStatus("screen") === "granted",
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
  } else if (normalized === "desktopScreenRecording") {
    throw new Error("Desktop Screen Recording must be granted in System Settings");
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
    desktopScreenRecording: false,
    camera: false,
    microphone: false,
  };
}

module.exports = {
  desktopPermissionSettingsURL,
  desktopPermissionState,
  requestDesktopPermission,
};
