const assert = require("node:assert/strict");
const test = require("node:test");

const {
  DesktopPermissionController,
  desktopPermissionSettingsURL,
  desktopPermissionState,
  requestDesktopPermission,
} = require("../desktop-permissions.cjs");

test("desktop permission controller is the shared state source", async () => {
  let accessibility = false;
  const updates = [];
  const controller = new DesktopPermissionController({
    computerUseHost: {
      permissions: async (params) => {
        accessibility ||= params?.promptAccessibility === true;
        return { accessibility, screenRecording: false };
      },
    },
    systemPreferences: {
      getMediaAccessStatus: () => "denied",
    },
    platform: "darwin",
    onStateChange: (state) => updates.push(state),
  });

  const initial = await controller.refresh();
  assert.equal(initial.accessibility, false);
  const granted = await controller.request("accessibility");
  assert.equal(granted.accessibility, true);
  assert.equal(controller.currentState().accessibility, true);
  assert.equal(updates.length, 2);
});

test("desktop permission state combines Computer Use and media permissions", async () => {
  const state = await desktopPermissionState(
    { permissions: async () => ({ accessibility: true, screenRecording: false, inputMonitoring: true }) },
    {
      getMediaAccessStatus: (permission) => permission === "camera" || permission === "screen" ? "granted" : "denied",
    },
    "darwin",
  );
  assert.deepEqual(state, {
    supported: true,
    accessibility: true,
    screenRecording: false,
    desktopScreenRecording: true,
    camera: true,
    microphone: false,
  });
  assert.equal("inputMonitoring" in state, false);
  assert.equal("speaker" in state, false);
});

test("desktop permission request uses the owning native API", async () => {
  const computerUseCalls = [];
  const mediaCalls = [];
  let accessibility = false;
  const host = {
    permissions: async (params) => {
      computerUseCalls.push(params);
      accessibility ||= params?.promptAccessibility === true;
      return { accessibility, screenRecording: false };
    },
  };
  const preferences = {
    askForMediaAccess: async (permission) => {
      mediaCalls.push(permission);
      return true;
    },
    getMediaAccessStatus: (permission) => mediaCalls.includes(permission) ? "granted" : "not-determined",
  };

  const accessibilityState = await requestDesktopPermission(host, preferences, "accessibility", "darwin");
  assert.equal(accessibilityState.accessibility, true);
  assert.deepEqual(computerUseCalls[0], { promptAccessibility: true, promptScreenRecording: false });
  assert.deepEqual(mediaCalls, []);

  const cameraState = await requestDesktopPermission(host, preferences, "camera", "darwin");
  assert.equal(cameraState.camera, true);
  assert.deepEqual(mediaCalls, ["camera"]);
  await assert.rejects(
    requestDesktopPermission(host, preferences, "speaker", "darwin"),
    /Unsupported/,
  );
});

test("desktop permission settings URLs cover every managed macOS permission", () => {
  assert.match(desktopPermissionSettingsURL("accessibility", "darwin"), /Privacy_Accessibility$/);
  assert.match(desktopPermissionSettingsURL("screenRecording", "darwin"), /Privacy_ScreenCapture$/);
  assert.match(desktopPermissionSettingsURL("desktopScreenRecording", "darwin"), /Privacy_ScreenCapture$/);
  assert.match(desktopPermissionSettingsURL("camera", "darwin"), /Privacy_Camera$/);
  assert.match(desktopPermissionSettingsURL("microphone", "darwin"), /Privacy_Microphone$/);
  assert.equal(desktopPermissionSettingsURL("speaker", "darwin"), "");
  assert.equal(desktopPermissionSettingsURL("camera", "win32"), "");
});
