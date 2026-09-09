const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ComputerUseBridgeServer,
  classifyComputerUseError,
  permissionsForRoute,
} = require("../computer-use-bridge-server.cjs");

test("Computer Use routes declare their native permission requirements", () => {
  assert.deepEqual(permissionsForRoute("/computer/apps/list"), []);
  assert.deepEqual(permissionsForRoute("/computer/apps/use"), ["screenRecording"]);
  assert.deepEqual(permissionsForRoute("/computer/observe-capture"), ["screenRecording"]);
  assert.deepEqual(permissionsForRoute("/computer/act"), ["accessibility", "screenRecording"]);
});

test("background preview conflicts preserve outcome without asking for foreground", () => {
  for (const code of ["computer_input_busy", "computer_background_unavailable"]) {
    const result = classifyComputerUseError({code, message:"background delivery stopped", outcome:"unknown"});
    assert.equal(result.status, 409);
    assert.equal(result.code, code);
    assert.equal(result.outcome, "unknown");
    assert.equal(result.retryable, false);
  }
});

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

test("Computer Use bridge routes normalized-window pointer actions", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const response = await fetch(`${identity.url}/computer/pointer`, {
      method: "POST",
      headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({
        sessionID: "sess-pointer", appID: "com.example.App", windowID: 42,
        action: "drag", x: 0.12, y: 0.34, toX: 0.56, toY: 0.78,
      }),
    });
    assert.equal(response.status, 200);
    assert.deepEqual(host.pointerAction, {
      delivery: undefined,
      bundleID: "com.example.App", windowID: 42,
      action: "drag", x: 0.12, y: 0.34, toX: 0.56, toY: 0.78,
      button: undefined, clickCount: undefined, deltaX: undefined, deltaY: undefined,
    });
  } finally {
    await bridge.stop();
  }
});

test("background single-click delivery crosses the existing session-scoped pointer route", async () => {
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host);
  const identity = await bridge.start();
  try {
    const response = await fetch(`${identity.url}/computer/pointer`, {
      method: "POST", headers: authenticatedHeaders(identity.token),
      body: JSON.stringify({sessionID:"background-test", appID:"com.apple.iCal", windowID:42,
        action:"click", x:0.5, y:0.4, delivery:"background"}),
    });
    assert.equal(response.status, 200);
    assert.equal(host.pointerAction.delivery, "background");
    assert.equal(host.pointerAction.action, "click");
  } finally { await bridge.stop(); }
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
    permission: "accessibility",
    retryable: false,
    outcome: "not_started",
  }), {
    status: 403,
    code: "computer_permission_required",
    message: "permission required: accessibility",
    permission: "accessibility",
    permissions: [],
    retryable: false,
    outcome: "not_started",
  });
});

test("Computer Use bridge exposes foreground pointer conflicts", () => {
  assert.deepEqual(classifyComputerUseError({
    code: "computer_app_not_foreground",
    message: "application must be foreground for pointer input: com.example.App",
    permission: "",
    permissions: [],
    retryable: false,
    outcome: "not_started",
  }), {
    status: 409,
    code: "computer_app_not_foreground",
    message: "application must be foreground for pointer input: com.example.App",
    permission: "",
    permissions: [],
    retryable: false,
    outcome: "not_started",
  });
});

test("Computer Use bridge preserves lifecycle failure stages and native diagnostics", () => {
  for (const [code, status] of [
    ["computer_launch_failed", 500],
    ["computer_activation_failed", 409],
    ["computer_window_raise_failed", 409],
  ]) {
    const error = classifyComputerUseError({
      code, message: "native stage detail: AXRaise returned -25206",
      outcome: "unknown", retryable: false,
    });
    assert.equal(error.status, status);
    assert.equal(error.code, code);
    assert.match(error.message, /-25206/);
    assert.equal(error.outcome, "unknown");
    assert.equal(error.retryable, false);
  }
});

test("Computer Use bridge exposes changed pointer targets", () => {
  assert.deepEqual(classifyComputerUseError({
    code: "computer_pointer_target_changed",
    message: "pointer target changed",
    retryable: false,
    outcome: "not_started",
  }), {
    status: 409,
    code: "computer_pointer_target_changed",
    message: "pointer target changed",
    permission: "",
    permissions: [],
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


test("Computer Use routes explicit instances and keyboard without an AX screenshot prerequisite", async () => {
  const calls=[];
  const host={useApp:async p=>{calls.push(p);return {};},keyboard:async p=>{calls.push(p);return {};},observeCapture:async p=>{calls.push(p);return {capture:{windowID:42},observationError:{code:"computer_permission_required"}};}};
  const bridge=new ComputerUseBridgeServer(host);const id=await bridge.start();
  const send=async (route,body)=>{
    const response=await fetch(id.url+route,{method:"POST",headers:authenticatedHeaders(id.token),body:JSON.stringify({sessionID:"session",appID:"com.github.Electron",...body})});
    assert.equal(response.status,200);return response.json();
  };
  try {
    await send("/computer/apps/use",{pid:123,appPath:"/tmp/Test.app"});
    assert.equal(calls[0].pid,123);assert.equal(calls[0].appPath,"/tmp/Test.app");
    await send("/computer/keyboard",{windowID:42,action:"press_key",key:"a",modifiers:["command"]});
    assert.deepEqual(calls[1].modifiers,["command"]);assert.equal(calls[1].key,"a");
    const result=await send("/computer/observe-capture",{windowID:42,includeAccessibility:false,output:"/tmp/screen.png"});
    assert.equal(calls[2].includeAccessibility,false);assert(result.capture);assert(result.observationError);
  }finally{await bridge.stop();}
});

test('preview target is request-scoped and emitted only after native permission approval', async () => {
  const activities = [];
  let allow = false;
  const host = new FakeComputerUseHost();
  const bridge = new ComputerUseBridgeServer(host, {
    onActivity: target => activities.push(target),
    permissionCoordinator: {run: async (_permissions, operation) => {
      if (!allow) throw Object.assign(new Error('denied'), {code:'computer_permission_denied'});
      return operation();
    }},
  });
  const identity = await bridge.start();
  const send = (route, turnID, body = {}) => fetch(identity.url + route, {
    method:'POST', headers:{...authenticatedHeaders(identity.token), ...(turnID ? {'x-pudding-turn-id':turnID} : {})},
    body:JSON.stringify({sessionID:'session-a',appID:'com.example.Test',windowID:42,...body}),
  });
  try {
    assert.equal((await send('/computer/observe', 'turn-a')).status, 403);
    assert.deepEqual(activities, []);
    allow = true;
    assert.equal((await send('/computer/observe', 'turn-a')).status, 200);
    assert.equal((await send('/computer/observe', 'turn-b', {sessionID:'session-b',windowID:43})).status, 200);
    await send('/computer/observe');
    await send('/computer/apps/list', 'turn-a');
    await send('/computer/apps/use', 'turn-a');
    assert.deepEqual(activities, [
      {sessionID:'session-a',turnID:'turn-a',appID:'com.example.Test',windowID:42},
      {sessionID:'session-b',turnID:'turn-b',appID:'com.example.Test',windowID:43},
    ]);
    bridge.onActivity = () => { throw new Error('preview failed'); };
    assert.equal((await send('/computer/observe', 'turn-a')).status, 200, 'preview cannot block the tool');
  } finally { await bridge.stop(); }
});
