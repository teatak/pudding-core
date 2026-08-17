const assert = require("node:assert/strict");
const test = require("node:test");

const { ComputerUseError } = require("../computer-use-host.cjs");
const { ComputerUsePermissionCoordinator } = require("../computer-use-permissions.cjs");

test("Computer Use permission guide resumes the pending operation after authorization", async () => {
  const permissions = new FakePermissions({ accessibility: false, screenRecording: true });
  const guides = [];
  let calls = 0;
  const coordinator = new ComputerUsePermissionCoordinator({
    permissions,
    onGuideChange: (guide) => guides.push(guide),
  });

  const result = coordinator.run(["accessibility", "screenRecording"], async () => {
    calls += 1;
    return "completed";
  });
  await nextTurn();
  assert.equal(calls, 0);
  assert.equal(guides.at(-1).permissions.find((item) => item.permission === "accessibility").allowed, false);

  permissions.state.accessibility = true;
  await coordinator.refresh();
  assert.equal(await result, "completed");
  assert.equal(calls, 1);
  assert.equal(guides.at(-1), null);
});

test("Computer Use permission guide retries a not-started permission failure exactly once", async () => {
  const permissions = new FakePermissions({ accessibility: true, screenRecording: true });
  const coordinator = new ComputerUsePermissionCoordinator({ permissions });
  let calls = 0;
  let pendingGuide;
  coordinator.onGuideChange = (guide) => {
    pendingGuide = guide;
  };
  const result = coordinator.run(["accessibility"], async () => {
    calls += 1;
    if (calls === 1) {
      permissions.state.accessibility = false;
      throw permissionError("accessibility");
    }
    return "retried";
  });
  await nextTurn();
  assert.equal(pendingGuide.required[0], "accessibility");
  permissions.state.accessibility = true;
  await coordinator.refresh();
  assert.equal(await result, "retried");
  assert.equal(calls, 2);
});

test("denying the runtime guide returns a structured permission denial", async () => {
  const permissions = new FakePermissions({ accessibility: false, screenRecording: false });
  const coordinator = new ComputerUsePermissionCoordinator({ permissions });
  const result = coordinator.run(["accessibility", "screenRecording"], async () => "unused");
  await nextTurn();
  const guide = coordinator.currentGuide();
  assert.equal(coordinator.deny(guide.requestID), true);
  await assert.rejects(result, (error) => {
    assert.equal(error.code, "computer_permission_denied");
    assert.deepEqual(error.permissions, ["accessibility", "screenRecording"]);
    assert.equal(error.outcome, "not_started");
    return true;
  });
});

test("restart is offered only after granted state and a Helper restart still fail", async () => {
  const permissions = new FakePermissions({ accessibility: true, screenRecording: true });
  let helperRestarts = 0;
  let calls = 0;
  const coordinator = new ComputerUsePermissionCoordinator({
    permissions,
    restartHelper: async () => {
      helperRestarts += 1;
    },
  });
  const result = coordinator.run(["accessibility"], async () => {
    calls += 1;
    throw permissionError("accessibility");
  });
  await nextTurn();
  await nextTurn();
  const guide = coordinator.currentGuide();
  assert.equal(helperRestarts, 1);
  assert.equal(calls, 3);
  assert.equal(guide.restartRequired, true);
  coordinator.deny(guide.requestID);
  await assert.rejects(result, (error) => error.code === "computer_permission_denied");
});

function permissionError(permission) {
  return new ComputerUseError(`permission required: ${permission}`, {
    code: "computer_permission_required",
    permission,
    outcome: "not_started",
  });
}

function nextTurn() {
  return new Promise((resolve) => setImmediate(resolve));
}

class FakePermissions {
  constructor(state) {
    this.state = { supported: true, ...state };
  }

  async refresh() {
    return { ...this.state };
  }
}
