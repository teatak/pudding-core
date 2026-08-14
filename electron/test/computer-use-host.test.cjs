const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const { PassThrough, Writable } = require("node:stream");
const test = require("node:test");

const { ComputerUseHost } = require("../computer-use-host.cjs");

test("ComputerUseHost correlates out-of-order helper responses", async () => {
  const fake = new FakeHelper();
  const host = createHost(fake);
  const permissions = host.permissions();
  const apps = host.listApps();
  await fake.waitForRequests(2);

  fake.respond(fake.requests[1].id, { apps: ["TextEdit"] });
  fake.respond(fake.requests[0].id, { accessibility: false, screenRecording: true });

  assert.deepEqual(await permissions, { accessibility: false, screenRecording: true });
  assert.deepEqual(await apps, { apps: ["TextEdit"] });
  await host.stop();
});

test("ComputerUseHost preserves structured helper errors", async () => {
  const fake = new FakeHelper();
  const host = createHost(fake);
  const result = host.observe({ bundleID: "com.apple.TextEdit" });
  await fake.waitForRequests(1);
  fake.fail(fake.requests[0].id, {
    code: "computer_permission_required",
    message: "permission required: accessibility",
    retryable: false,
    outcome: "not_started",
  });

  await assert.rejects(result, (error) => {
    assert.equal(error.code, "computer_permission_required");
    assert.equal(error.outcome, "not_started");
    return true;
  });
  await host.stop();
});

test("ComputerUseHost sends lifecycle commands", async () => {
  const fake = new FakeHelper();
  const host = createHost(fake);
  const launched = host.launchApp({ bundleID: "com.apple.calculator" });
  const quit = host.quitApp({ bundleID: "com.apple.calculator", pid: 42 });
  await fake.waitForRequests(2);
  assert.equal(fake.requests[0].command, "launch_app");
  assert.equal(fake.requests[1].command, "quit_app");
  fake.respond(fake.requests[0].id, { pid: 42, newlyLaunched: true });
  fake.respond(fake.requests[1].id, { pid: 42, closed: true });
  assert.equal((await launched).newlyLaunched, true);
  assert.equal((await quit).closed, true);
  await host.stop();
});

test("ComputerUseHost terminates a desynchronized helper", async () => {
  const fake = new FakeHelper();
  const host = createHost(fake);
  const result = host.listApps();
  await fake.waitForRequests(1);
  fake.stdout.write("not-json\n");

  await assert.rejects(result, (error) => error.code === "computer_helper_crashed");
  assert.equal(fake.killed, true);
});

test("ComputerUseHost starts a fresh helper after a crash", async () => {
  const first = new FakeHelper();
  const second = new FakeHelper();
  const helpers = [first, second];
  const host = new ComputerUseHost({
    binaryPath: "/fake/helper",
    platform: "darwin",
    spawnProcess: () => helpers.shift(),
  });
  const failed = host.listApps();
  await first.waitForRequests(1);
  first.stdout.write("not-json\n");
  await assert.rejects(failed, (error) => error.code === "computer_helper_crashed");

  const recovered = host.permissions();
  await second.waitForRequests(1);
  second.respond(second.requests[0].id, { accessibility: false, screenRecording: true });
  assert.deepEqual(await recovered, { accessibility: false, screenRecording: true });
  assert.equal(helpers.length, 0);
  await host.stop();
});

test("ComputerUseHost terminates the helper on timeout", async () => {
  const fake = new FakeHelper();
  const host = createHost(fake, { defaultTimeoutMs: 10 });

  await assert.rejects(host.listApps(), (error) => {
    assert.equal(error.code, "computer_action_timeout");
    assert.equal(error.outcome, "unknown");
    return true;
  });
  assert.equal(fake.killed, true);
});

test("ComputerUseHost rejects an already cancelled request before spawning", async () => {
  const controller = new AbortController();
  controller.abort();
  let spawnCount = 0;
  const host = new ComputerUseHost({
    binaryPath: "/fake/helper",
    platform: "darwin",
    spawnProcess: () => {
      spawnCount += 1;
      return new FakeHelper();
    },
  });

  await assert.rejects(host.listApps({ signal: controller.signal }), (error) => {
    assert.equal(error.code, "computer_action_cancelled");
    assert.equal(error.outcome, "not_started");
    return true;
  });
  assert.equal(spawnCount, 0);
});

test("ComputerUseHost aborts an in-flight action and starts a fresh helper", async () => {
  const first = new FakeHelper();
  const second = new FakeHelper();
  const helpers = [first, second];
  const host = new ComputerUseHost({
    binaryPath: "/fake/helper",
    platform: "darwin",
    spawnProcess: () => helpers.shift(),
  });
  const controller = new AbortController();
  const action = host.act(
    { bundleID: "com.example.App", windowID: 42, elementID: "button", action: "press" },
    { signal: controller.signal },
  );
  await first.waitForRequests(1);
  controller.abort();

  await assert.rejects(action, (error) => {
    assert.equal(error.code, "computer_action_cancelled");
    assert.equal(error.outcome, "unknown");
    return true;
  });
  assert.equal(first.killed, true);
  first.respond(first.requests[0].id, { completed: true });

  const recovered = host.permissions();
  await second.waitForRequests(1);
  second.respond(second.requests[0].id, { accessibility: true, screenRecording: true });
  assert.deepEqual(await recovered, { accessibility: true, screenRecording: true });
  assert.equal(helpers.length, 0);
  await host.stop();
});

function createHost(fake, options = {}) {
  return new ComputerUseHost({
    binaryPath: "/fake/helper",
    platform: "darwin",
    spawnProcess: () => fake,
    ...options,
  });
}

class FakeHelper extends EventEmitter {
  constructor() {
    super();
    this.exitCode = null;
    this.signalCode = null;
    this.killed = false;
    this.requests = [];
    this.waiters = [];
    this.stdout = new PassThrough();
    this.stderr = new PassThrough();
    this.stdin = new Writable({
      write: (chunk, _encoding, callback) => {
        for (const line of String(chunk).trim().split("\n")) {
          if (line) {
            this.requests.push(JSON.parse(line));
          }
        }
        this.flushWaiters();
        callback();
      },
      final: (callback) => {
        this.exitCode = 0;
        queueMicrotask(() => this.emit("exit", 0, null));
        callback();
      },
    });
  }

  respond(id, result) {
    this.stdout.write(`${JSON.stringify({ id, ok: true, result })}\n`);
  }

  fail(id, error) {
    this.stdout.write(`${JSON.stringify({ id, ok: false, error })}\n`);
  }

  kill(signal) {
    this.killed = true;
    this.signalCode = signal;
    queueMicrotask(() => this.emit("exit", null, signal));
    return true;
  }

  waitForRequests(count) {
    if (this.requests.length >= count) {
      return Promise.resolve();
    }
    return new Promise((resolve) => this.waiters.push({ count, resolve }));
  }

  flushWaiters() {
    for (const waiter of this.waiters.splice(0)) {
      if (this.requests.length >= waiter.count) {
        waiter.resolve();
      } else {
        this.waiters.push(waiter);
      }
    }
  }
}
