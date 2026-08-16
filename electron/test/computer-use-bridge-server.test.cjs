const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ComputerUseBridgeServer,
  classifyComputerUseError,
} = require("../computer-use-bridge-server.cjs");

test("Computer Use bridge requires authentication and explicit session routing", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const unauthorized = await fetch(`${identity.url}/computer/apps/list`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ sessionID: "sess-a" }),
    });
    assert.equal(unauthorized.status, 401);

    const missingSession = await fetch(`${identity.url}/computer/apps/list`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({}),
    });
    assert.equal(missingSession.status, 400);
    assert.equal((await missingSession.json()).code, "computer_invalid_request");

    const listed = await fetch(`${identity.url}/computer/apps/list`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({ sessionID: "sess-a" }),
    });
    assert.equal(listed.status, 200);
	assert.deepEqual(await listed.json(), { apps: [{ bundleID: "com.apple.TextEdit", controllable: true }] });
  } finally {
    await bridge.stop();
  }
});

test("Computer Use bridge maps external app IDs to Helper bundle IDs", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const response = await fetch(`${identity.url}/computer/observe`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({
        sessionID: "sess-b",
        appID: "com.apple.TextEdit",
        windowID: 42,
        maxElements: 50,
      }),
    });
    assert.equal(response.status, 200);
    assert.deepEqual(host.observed, {
      bundleID: "com.apple.TextEdit",
      windowID: 42,
      maxElements: 50,
    });
  } finally {
    await bridge.stop();
  }
});

test("Computer Use bridge routes app lifecycle targets", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const launch = await fetch(`${identity.url}/computer/apps/use`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({ sessionID: "sess-c", appID: "com.apple.calculator", foreground: true }),
    });
    assert.equal(launch.status, 200);
    assert.deepEqual(host.launched, { bundleID: "com.apple.calculator", foreground: true });

    const quit = await fetch(`${identity.url}/computer/apps/quit`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({ sessionID: "sess-c", appID: "com.apple.calculator", pid: 42 }),
    });
    assert.equal(quit.status, 200);
    assert.deepEqual(host.quit, { bundleID: "com.apple.calculator", pid: 42 });
  } finally {
    await bridge.stop();
  }
});

test("Computer Use bridge accepts the full set_value schema limit", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const value = "🙂".repeat(20_000);
    const response = await fetch(`${identity.url}/computer/act`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({
        sessionID: "sess-large-value",
        appID: "com.example.App",
        windowID: 42,
        elementID: "field",
        action: "set_value",
        value,
      }),
    });
    assert.equal(response.status, 200);
    assert.equal(host.acted.value, value);
  } finally {
    await bridge.stop();
  }
});

test("Computer Use bridge routes screenshot pointer actions", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const response = await fetch(`${identity.url}/computer/pointer`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({
        sessionID: "sess-pointer", appID: "com.example.App", windowID: 42,
        action: "drag", x: 12, y: 34, toX: 56, toY: 78,
        captureWidth: 200, captureHeight: 100, scaleFactor: 2,
      }),
    });
    assert.equal(response.status, 200);
    assert.deepEqual(host.pointerAction, {
      bundleID: "com.example.App", windowID: 42,
      action: "drag", x: 12, y: 34, toX: 56, toY: 78,
      button: undefined, clickCount: undefined, deltaX: undefined, deltaY: undefined,
      captureWidth: 200, captureHeight: 100, scaleFactor: 2,
    });
  } finally {
    await bridge.stop();
  }
});

test("Computer Use bridge propagates a disconnected action request to the Helper signal", async () => {
  const host = new FakeComputerUseHost();
  let markStarted;
  let markAborted;
  const started = new Promise((resolve) => {
    markStarted = resolve;
  });
  const aborted = new Promise((resolve) => {
    markAborted = resolve;
  });
  host.act = (_params, options = {}) => new Promise((_resolve, reject) => {
    markStarted();
    options.signal.addEventListener("abort", () => {
      const error = new Error("Computer Use request was cancelled");
      error.code = "computer_action_cancelled";
      error.outcome = "unknown";
      markAborted();
      reject(error);
    }, { once: true });
  });
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const controller = new AbortController();
    const request = fetch(`${identity.url}/computer/act`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({
        sessionID: "sess-cancel",
        appID: "com.example.App",
        windowID: 42,
        elementID: "button",
        action: "press",
      }),
      signal: controller.signal,
    });
    await started;
    controller.abort();
    await assert.rejects(request, (error) => error.name === "AbortError");
    await aborted;
  } finally {
    await bridge.stop();
  }
});

test("Computer Use bridge exposes permission errors with outcome", () => {
  assert.deepEqual(classifyComputerUseError({
    code: "computer_permission_required",
    message: "permission required: accessibility",
    retryable: false,
    outcome: "not_started",
  }), {
    status: 403,
    code: "computer_permission_required",
    message: "permission required: accessibility",
    retryable: false,
    outcome: "not_started",
  });
});

test("Computer Use bridge exposes foreground pointer conflicts", () => {
  assert.deepEqual(classifyComputerUseError({
    code: "computer_app_not_foreground",
    message: "application must be foreground for pointer input: com.example.App",
    retryable: false,
    outcome: "not_started",
  }), {
    status: 409,
    code: "computer_app_not_foreground",
    message: "application must be foreground for pointer input: com.example.App",
    retryable: false,
    outcome: "not_started",
  });
});

function authenticatedHeaders(token) {
  return {
    authorization: `Bearer ${token}`,
    "content-type": "application/json",
  };
}

class FakeComputerUseHost {
  constructor() {
    this.observed = null;
    this.acted = null;
    this.launched = null;
    this.quit = null;
    this.clicked = null;
  }

  permissions() {
    return { accessibility: false, screenRecording: true };
  }

  listApps() {
    return { apps: [{ bundleID: "com.apple.TextEdit", controllable: true }] };
  }

  useApp(params) {
    this.launched = params;
    return { ...params, pid: 42, newlyLaunched: true, windows: [{ windowID: 7 }] };
  }

  quitApp(params) {
    this.quit = params;
    return { ...params, closed: true };
  }

  observe(params) {
    this.observed = params;
    return { bundleID: params.bundleID, windows: [], elements: [] };
  }

  observeCapture(params) {
    return {
      observation: { bundleID: params.bundleID, windowID: params.windowID, elements: [] },
      capture: { windowID: params.windowID, width: 100, height: 100 },
    };
  }

  act(params) {
    this.acted = params;
    return { completed: true };
  }

  pointer(params) {
    this.pointerAction = params;
    return { bundleID: params.bundleID, action: params.action, completed: true, x: params.x, y: params.y };
  }
}
