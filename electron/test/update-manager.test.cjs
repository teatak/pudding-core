const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const test = require("node:test");

const { UpdateManager } = require("../update-manager.cjs");

class FakeUpdater extends EventEmitter {
  constructor() {
    super();
    this.checks = 0;
    this.installs = 0;
  }

  async checkForUpdates() {
    this.checks += 1;
  }

  quitAndInstall() {
    this.installs += 1;
  }
}

test("schedules the first check and repeats every six hours", async () => {
  const updater = new FakeUpdater();
  const timers = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    setTimeoutFn: (callback, delay) => {
      const timer = { callback, delay, unref() {} };
      timers.push(timer);
      return timer;
    },
    clearTimeoutFn: () => {},
  });

  manager.start();
  assert.equal(timers[0].delay, 15_000);
  await timers[0].callback();
  assert.equal(updater.checks, 1);
  assert.equal(timers[1].delay, 6 * 60 * 60 * 1_000);
});

test("background checks stay silent while manual checks report their result", async () => {
  const updater = new FakeUpdater();
  const results = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    onManualResult: async (result) => results.push(result),
  });

  await manager.check(false);
  updater.emit("update-not-available");
  assert.deepEqual(results, []);

  await manager.check(true);
  updater.emit("update-not-available");
  assert.deepEqual(results, [{ kind: "up-to-date" }]);
});

test("download completion exposes a restart state without a completion dialog", async () => {
  const updater = new FakeUpdater();
  const results = [];
  const states = [];
  const calls = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    beforeInstall: async () => calls.push("stop-services"),
    onManualResult: async (result) => results.push(result),
    onStateChange: (state) => states.push(state),
  });

  await manager.check(true);
  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("download-progress", { percent: 42.4 });
  updater.emit("update-downloaded", { version: "1.2.0" });

  assert.deepEqual(results, [{ kind: "downloading", version: "1.2.0" }]);
  assert.equal(states.at(-1).status, "downloaded");
  assert.equal(states.at(-1).percent, 100);

  assert.equal(await manager.install(), true);
  assert.deepEqual(calls, ["stop-services"]);
  assert.equal(updater.installs, 1);
  assert.equal(states.at(-1).status, "installing");
});

test("development builds explain why update checks are unavailable", async () => {
  const results = [];
  const manager = new UpdateManager({
    updater: new FakeUpdater(),
    isPackaged: false,
    onManualResult: async (result) => results.push(result),
  });

  assert.equal(await manager.check(true), false);
  assert.deepEqual(results, [{ kind: "development" }]);
});
