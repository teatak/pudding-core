const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const test = require("node:test");

const { createSystemTerminalOpener } = require("../system-terminal.cjs");

test("opens macOS Terminal at the requested directory", async () => {
  const calls = [];
  const openTerminal = createSystemTerminalOpener({
    platform: "darwin",
    spawn: successfulSpawn(calls),
  });

  assert.equal(await openTerminal("/tmp/project"), true);
  assert.deepEqual(calls[0], {
    args: ["-a", "Terminal", "/tmp/project"],
    command: "open",
    options: { cwd: undefined, detached: true, stdio: "ignore", windowsHide: false },
  });
});

test("starts Windows command prompt with the requested working directory", async () => {
  const calls = [];
  const openTerminal = createSystemTerminalOpener({
    env: { ComSpec: "C:\\Windows\\System32\\cmd.exe" },
    platform: "win32",
    spawn: successfulSpawn(calls),
  });

  assert.equal(await openTerminal("C:\\work"), true);
  assert.equal(calls[0].command, "C:\\Windows\\System32\\cmd.exe");
  assert.deepEqual(calls[0].args, ["/d", "/k"]);
  assert.equal(calls[0].options.cwd, "C:\\work");
});

test("falls through unavailable Linux terminal commands", async () => {
  const calls = [];
  const spawn = (command, args, options) => {
    calls.push({ args, command, options });
    const child = new EventEmitter();
    child.unref = () => {};
    queueMicrotask(() => child.emit(command === "missing-terminal" ? "error" : "spawn"));
    return child;
  };
  const openTerminal = createSystemTerminalOpener({
    env: { TERMINAL: "missing-terminal" },
    platform: "linux",
    spawn,
  });

  assert.equal(await openTerminal("/tmp/project"), true);
  assert.deepEqual(calls.map((call) => call.command), ["missing-terminal", "x-terminal-emulator"]);
});

function successfulSpawn(calls) {
  return (command, args, options) => {
    calls.push({ args, command, options });
    const child = new EventEmitter();
    child.unref = () => {};
    queueMicrotask(() => child.emit("spawn"));
    return child;
  };
}
