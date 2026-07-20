const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const path = require("node:path");
const test = require("node:test");

const { ProjectFileWatcher, projectFileChangedChannel } = require("../project-file-watcher.cjs");

class FakeWatcher extends EventEmitter {
  close() {
    this.closed = true;
  }
}

class FakeSender extends EventEmitter {
  constructor(id) {
    super();
    this.id = id;
    this.events = [];
    this.destroyed = false;
  }

  isDestroyed() {
    return this.destroyed;
  }

  send(channel, payload) {
    this.events.push({ channel, payload });
  }

  destroy() {
    this.destroyed = true;
    this.emit("destroyed");
  }
}

test("watches the parent directory and debounces changes for the selected file", async () => {
  const watched = [];
  const service = new ProjectFileWatcher({
    debounceMs: 5,
    watchDirectory: (directory, options, listener) => {
      const watcher = new FakeWatcher();
      watched.push({ directory, listener, options, watcher });
      return watcher;
    },
  });
  const sender = new FakeSender(1);
  const filePath = path.resolve("/tmp/project/main.go");

  assert.deepEqual(service.subscribe(sender, { id: "active", path: filePath }), { id: "active", path: filePath });
  assert.equal(watched[0].directory, path.dirname(filePath));
  assert.deepEqual(watched[0].options, { encoding: "utf8", persistent: false });

  watched[0].listener("change", "other.go");
  watched[0].listener("change", "main.go");
  watched[0].listener("rename", "main.go");
  await new Promise((resolve) => setTimeout(resolve, 15));

  assert.deepEqual(sender.events, [{
    channel: projectFileChangedChannel,
    payload: { eventType: "rename", id: "active", path: filePath },
  }]);
  service.closeAll();
});

test("watches project directories recursively and only ignores configured generated paths", async () => {
  const watched = [];
  const service = new ProjectFileWatcher({
    debounceMs: 5,
    watchDirectory: (directory, options, listener) => {
      const watcher = new FakeWatcher();
      watched.push({ directory, listener, options, watcher });
      return watcher;
    },
  });
  const sender = new FakeSender(6);
  const projectPath = path.resolve("/tmp/project");

  service.subscribe(sender, { id: "project", kind: "directory", path: projectPath });
  assert.equal(watched[0].directory, projectPath);
  assert.deepEqual(watched[0].options, { encoding: "utf8", persistent: false, recursive: true });

  watched[0].listener("change", "node_modules/pkg/index.js");
  watched[0].listener("change", ".git/index");
  watched[0].listener("rename", "src/main.go");
  await new Promise((resolve) => setTimeout(resolve, 15));

  assert.deepEqual(sender.events, [{
    channel: projectFileChangedChannel,
    payload: {
      eventType: "rename",
      id: "project",
      path: projectPath,
      changedPath: path.join(projectPath, "src/main.go"),
    },
  }]);

  watched[0].listener("change", ".claude/settings.json");
  await new Promise((resolve) => setTimeout(resolve, 15));

  assert.deepEqual(sender.events[1], {
    channel: projectFileChangedChannel,
    payload: {
      eventType: "change",
      id: "project",
      path: projectPath,
      changedPath: path.join(projectPath, ".claude/settings.json"),
    },
  });
  service.closeAll();
});

test("unsubscribe and renderer destruction close native watchers", () => {
  const watchers = [];
  const service = new ProjectFileWatcher({
    watchDirectory: () => {
      const watcher = new FakeWatcher();
      watchers.push(watcher);
      return watcher;
    },
  });
  const sender = new FakeSender(2);

  service.subscribe(sender, { id: "first", path: path.resolve("/tmp/first.txt") });
  assert.equal(service.unsubscribe(sender, { id: "first" }), true);
  assert.equal(watchers[0].closed, true);

  service.subscribe(sender, { id: "second", path: path.resolve("/tmp/second.txt") });
  sender.destroy();
  assert.equal(watchers[1].closed, true);
});

test("main-frame navigation closes watchers without leaking sender listeners", () => {
  const watcher = new FakeWatcher();
  const service = new ProjectFileWatcher({ watchDirectory: () => watcher });
  const sender = new FakeSender(4);

  service.subscribe(sender, { id: "active", path: path.resolve("/tmp/file.txt") });
  sender.emit("did-start-navigation", {}, "file://same", true, true);
  assert.equal(watcher.closed, undefined);

  sender.emit("did-start-navigation", {}, "file://reload", false, true);
  assert.equal(watcher.closed, true);
  assert.equal(sender.listenerCount("destroyed"), 0);
  assert.equal(sender.listenerCount("did-start-navigation"), 0);
});

test("watch setup and renderer send failures do not leak subscriptions", async () => {
  const setupService = new ProjectFileWatcher({
    watchDirectory: () => {
      throw new Error("watch failed");
    },
  });
  const setupSender = new FakeSender(5);
  assert.throws(
    () => setupService.subscribe(setupSender, { id: "active", path: path.resolve("/tmp/file.txt") }),
    /watch failed/,
  );
  assert.equal(setupService.subscriptions.size, 0);
  assert.equal(setupService.boundSenders.size, 0);

  const watcher = new FakeWatcher();
  let listener;
  const sendService = new ProjectFileWatcher({
    debounceMs: 5,
    watchDirectory: (_directory, _options, nextListener) => {
      listener = nextListener;
      return watcher;
    },
  });
  const sendSender = new FakeSender(6);
  sendSender.send = () => {
    throw new Error("renderer disappeared");
  };
  sendService.subscribe(sendSender, { id: "active", path: path.resolve("/tmp/file.txt") });
  listener("change", "file.txt");
  await new Promise((resolve) => setTimeout(resolve, 15));

  assert.equal(watcher.closed, true);
  assert.equal(sendService.subscriptions.size, 0);
  assert.equal(sendService.boundSenders.size, 0);
});

test("rejects relative paths and invalid subscription ids", () => {
  const service = new ProjectFileWatcher({ watchDirectory: () => new FakeWatcher() });
  const sender = new FakeSender(3);

  assert.throws(() => service.subscribe(sender, { id: "active", path: "relative.txt" }), /invalid project watch path/);
  assert.throws(() => service.subscribe(sender, { id: "", path: path.resolve("/tmp/file.txt") }), /invalid project file subscription id/);
});
