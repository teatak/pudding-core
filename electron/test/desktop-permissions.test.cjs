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
      permissions: async () => ({ accessibility, screenRecording: false }),
    },
    systemPreferences: {
      getMediaAccessStatus: () => "denied",
    },
    platform: "darwin",
    onStateChange: (state) => updates.push(state),
  });

  const initial = await controller.refresh();
  assert.equal(initial.accessibility, false);
  accessibility = true;
  const granted = await controller.refresh();
  assert.equal(granted.accessibility, true);
  assert.equal(controller.currentState().accessibility, true);
  assert.equal(updates.length, 2);
});

test("an explicit fresh refresh restarts Computer Use before checking permissions", async () => {
  const order = [];
  const controller = new DesktopPermissionController({
    computerUseHost: {
      permissions: async () => {
        order.push("permissions");
        return { accessibility: false, screenRecording: false };
      },
    },
    systemPreferences: { getMediaAccessStatus: () => "denied" },
    platform: "darwin",
    restartComputerUse: async () => order.push("restart"),
  });

  await controller.refresh({ restartComputerUse: true });
  assert.deepEqual(order, ["restart", "permissions"]);
});

test("concurrent fresh refreshes share one Computer Use restart", async () => {
  let releaseRestart;
  const restartGate = new Promise((resolve) => {
    releaseRestart = resolve;
  });
  let restartCount = 0;
  let permissionCount = 0;
  const controller = new DesktopPermissionController({
    computerUseHost: {
      permissions: async () => {
        permissionCount += 1;
        return { accessibility: true, screenRecording: true };
      },
    },
    systemPreferences: { getMediaAccessStatus: () => "denied" },
    platform: "darwin",
    restartComputerUse: async () => {
      restartCount += 1;
      await restartGate;
    },
  });

  const first = controller.refresh({ restartComputerUse: true });
  const second = controller.refresh({ restartComputerUse: true });
  assert.equal(restartCount, 1);
  releaseRestart();
  const [firstState, secondState] = await Promise.all([first, second]);

  assert.equal(permissionCount, 1);
  assert.deepEqual(firstState, secondState);
});

test("desktop permission state uses Computer Use as the screen recording source", async () => {
  const state = await desktopPermissionState(
    { permissions: async () => ({ accessibility: true, screenRecording: false, inputMonitoring: true }) },
    {
      getMediaAccessStatus: (permission) => permission === "camera" ? "granted" : "denied",
    },
    "darwin",
  );
  assert.deepEqual(state, {
    supported: true,
    accessibility: true,
    screenRecording: false,
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

test("first screen recording request uses the native request API", async () => {
  const calls = [];
  const state = await requestDesktopPermission(
    {
      permissions: async (params) => {
        calls.push(params);
        return { accessibility: false, screenRecording: false };
      },
    },
    { getMediaAccessStatus: () => "not-determined" },
    "screenRecording",
    "darwin",
  );

  assert.equal(state.screenRecording, false);
  assert.deepEqual(calls[0], { promptAccessibility: false, promptScreenRecording: true });
});

test("a repeated screen recording request opens System Settings", async () => {
  const calls = [];
  const opened = [];
  const controller = new DesktopPermissionController({
    computerUseHost: {
      permissions: async (params) => {
        calls.push(params);
        return { accessibility: false, screenRecording: false };
      },
    },
    systemPreferences: { getMediaAccessStatus: () => "denied" },
    platform: "darwin",
    openSettings: async (permission) => opened.push(permission),
  });

  await controller.request("screenRecording");
  assert.deepEqual(opened, []);
  assert.deepEqual(calls[0], { promptAccessibility: false, promptScreenRecording: true });

  await controller.request("screenRecording");

  assert.deepEqual(opened, ["screenRecording"]);
  assert.equal(calls.filter((params) => params?.promptScreenRecording === true).length, 1);
});

test("desktop permission settings URLs cover every managed macOS permission", () => {
  assert.match(desktopPermissionSettingsURL("accessibility", "darwin"), /Privacy_Accessibility$/);
  assert.match(desktopPermissionSettingsURL("screenRecording", "darwin"), /Privacy_ScreenCapture$/);
  assert.match(desktopPermissionSettingsURL("camera", "darwin"), /Privacy_Camera$/);
  assert.match(desktopPermissionSettingsURL("microphone", "darwin"), /Privacy_Microphone$/);
  assert.equal(desktopPermissionSettingsURL("speaker", "darwin"), "");
  assert.equal(desktopPermissionSettingsURL("camera", "win32"), "");
});
