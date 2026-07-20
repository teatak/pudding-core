const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const test = require("node:test");

const { UpdateManager } = require("../update-manager.cjs");

class FakeUpdater extends EventEmitter {
  constructor() {
    super();
    this.checks = 0;
    this.downloads = 0;
    this.installs = 0;
    this.feed = null;
    this.checkResult = null;
    this.downloadResult = [];
    this._channel = null;
    this.allowDowngrade = false;
  }

  get channel() {
    return this._channel;
  }

  set channel(value) {
    this._channel = value;
    this.allowDowngrade = true;
  }

  async checkForUpdates() {
    this.checks += 1;
    return this.checkResult;
  }

  async downloadUpdate() {
    this.downloads += 1;
    return this.downloadResult;
  }

  quitAndInstall() {
    this.installs += 1;
  }

  setFeedURL(feed) {
    this.feed = feed;
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

test("background checks stay silent while interactive checks report their result", async () => {
  const updater = new FakeUpdater();
  const results = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    onInteractiveResult: async (result) => results.push(result),
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
    onInteractiveResult: async (result) => results.push(result),
    onStateChange: (state) => states.push(state),
  });

  await manager.check(true);
  assert.equal(updater.autoDownload, true);
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

test("a downloaded update is rechecked without downloading the same version again", async () => {
  const updater = new FakeUpdater();
  const manager = new UpdateManager({ updater, isPackaged: true });

  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("update-downloaded", { version: "1.2.0" });
  updater.checkResult = {
    isUpdateAvailable: true,
    updateInfo: { version: "1.2.0" },
  };

  assert.equal(await manager.check(false), true);
  assert.equal(updater.checks, 1);
  assert.equal(updater.downloads, 0);
  assert.deepEqual(manager.getState(), {
    status: "downloaded",
    receivePreviewUpdates: false,
    version: "1.2.0",
    percent: 100,
  });
});

test("a newer release replaces the pending downloaded update", async () => {
  const updater = new FakeUpdater();
  const states = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    onStateChange: (state) => states.push(state),
  });

  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("update-downloaded", { version: "1.2.0" });
  updater.checkResult = {
    isUpdateAvailable: true,
    updateInfo: { version: "1.3.0" },
  };
  updater.downloadUpdate = async () => {
    updater.downloads += 1;
    updater.emit("download-progress", { percent: 54 });
    updater.emit("update-downloaded", { version: "1.3.0" });
    return [];
  };

  assert.equal(await manager.check(false), true);
  assert.equal(updater.downloads, 1);
  assert.deepEqual(states.slice(-3).map((state) => [state.status, state.version, state.percent]), [
    ["downloading", "1.3.0", 0],
    ["downloading", "1.3.0", 54],
    ["downloaded", "1.3.0", 100],
  ]);
});

test("a failed pending-update refresh keeps the previously downloaded version", async () => {
  const updater = new FakeUpdater();
  const errors = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    onError: (error) => errors.push(error.message),
  });

  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("update-downloaded", { version: "1.2.0" });
  updater.checkForUpdates = async () => {
    updater.checks += 1;
    const error = new Error("offline");
    updater.emit("error", error);
    throw error;
  };

  assert.equal(await manager.check(false), false);
  assert.deepEqual(errors, ["offline"]);
  assert.equal(manager.getState().status, "downloaded");
  assert.equal(manager.getState().version, "1.2.0");
});

test("install refreshes and installs the newest downloaded release", async () => {
  const updater = new FakeUpdater();
  const manager = new UpdateManager({ updater, isPackaged: true });

  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("update-downloaded", { version: "1.2.0" });
  updater.checkResult = {
    isUpdateAvailable: true,
    updateInfo: { version: "1.3.0" },
  };
  updater.downloadUpdate = async () => {
    updater.downloads += 1;
    updater.emit("update-downloaded", { version: "1.3.0" });
    return [];
  };

  assert.equal(await manager.install(), true);
  assert.equal(updater.checks, 1);
  assert.equal(updater.downloads, 1);
  assert.equal(updater.installs, 1);
  assert.equal(manager.getState().version, "1.3.0");
  assert.equal(manager.getState().status, "installing");
});

test("development builds explain why update checks are unavailable", async () => {
  const results = [];
  const manager = new UpdateManager({
    updater: new FakeUpdater(),
    isPackaged: false,
    onInteractiveResult: async (result) => results.push(result),
  });

  assert.equal(await manager.check(true), false);
  assert.deepEqual(results, [{ kind: "development" }]);
});

test("can simulate a downloaded update without checking or quitting", async () => {
  const updater = new FakeUpdater();
  const states = [];
  let simulatedInstalls = 0;
  const manager = new UpdateManager({
    updater,
    isPackaged: false,
    simulatedVersion: "99.0.0-test",
    onSimulatedInstall: async () => {
      simulatedInstalls += 1;
    },
    onStateChange: (state) => states.push(state),
  });

  manager.start();
  assert.equal(manager.getState().status, "downloaded");
  assert.equal(await manager.install(), true);
  assert.equal(simulatedInstalls, 1);
  assert.equal(updater.checks, 0);
  assert.equal(updater.installs, 0);
  assert.deepEqual(states.map((state) => state.status), ["downloaded", "installing", "downloaded"]);
});

test("uses an explicit generic feed for local packaged update tests", () => {
  const updater = new FakeUpdater();
  new UpdateManager({
    updater,
    isPackaged: true,
    feedURL: "http://127.0.0.1:8099",
  });

  assert.deepEqual(updater.feed, { provider: "generic", url: "http://127.0.0.1:8099" });
  assert.equal(updater.autoDownload, true);
  assert.equal(updater.autoInstallOnAppQuit, false);
  assert.equal(updater.channel, "latest");
  assert.equal(updater.allowPrerelease, false);
  assert.equal(updater.allowDowngrade, false);
});

test("preview updates use the beta channel without enabling downgrades", () => {
  const updater = new FakeUpdater();
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    receivePreviewUpdates: true,
  });

  assert.equal(updater.channel, "beta");
  assert.equal(updater.allowPrerelease, true);
  assert.equal(updater.allowDowngrade, false);
  assert.equal(manager.getState().receivePreviewUpdates, true);

  const state = manager.setReceivePreviewUpdates(false);
  assert.equal(updater.channel, "latest");
  assert.equal(updater.allowPrerelease, false);
  assert.equal(updater.allowDowngrade, false);
  assert.equal(state.receivePreviewUpdates, false);
});

test("cannot change update channel while an update is active", async () => {
  const manager = new UpdateManager({ updater: new FakeUpdater(), isPackaged: true });

  await manager.check(false);
  assert.throws(
    () => manager.setReceivePreviewUpdates(true),
    /update channel cannot change while an update is active/,
  );
});

test("reports an interactively started download that later fails", async () => {
  const updater = new FakeUpdater();
  const results = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    onInteractiveResult: async (result) => results.push(result),
  });

  await manager.check(true);
  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("error", new Error("signature rejected"));

  assert.equal(manager.getState().status, "idle");
  assert.deepEqual(results, [
    { kind: "downloading", version: "1.2.0" },
    { kind: "error", error: "signature rejected" },
  ]);
});

test("an asynchronous install error clears the installing state", async () => {
  const updater = new FakeUpdater();
  const results = [];
  const manager = new UpdateManager({
    updater,
    isPackaged: true,
    onInteractiveResult: async (result) => results.push(result),
  });

  updater.emit("update-available", { version: "1.2.0" });
  updater.emit("update-downloaded", { version: "1.2.0" });
  assert.equal(await manager.install(), true);
  updater.emit("error", new Error("signature rejected"));

  assert.equal(manager.getState().status, "idle");
  assert.deepEqual(results, [{ kind: "install-error", error: "signature rejected" }]);
});
