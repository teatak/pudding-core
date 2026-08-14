#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const readline = require("node:readline");
const { execFileSync, spawn } = require("node:child_process");

const root = path.resolve(__dirname, "..");
const fixtureBundleID = "com.teatak.pudding.computer-use-fixture";
const fixtureApp = path.join(root, "bin", "Pudding Computer Use Fixture.app");
const helper = path.join(
  root,
  "bin",
  "Pudding Computer Use.app",
  "Contents",
  "MacOS",
  "PuddingComputerUseHelper",
);
const launchServices =
  "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";

class HelperClient {
  constructor(executable) {
    this.nextID = 1;
    this.pending = new Map();
    this.stderr = "";
    this.child = spawn(executable, ["serve"], { stdio: ["pipe", "pipe", "pipe"] });
    this.lines = readline.createInterface({ input: this.child.stdout });
    this.lines.on("line", (line) => this.resolveLine(line));
    this.child.stderr.setEncoding("utf8");
    this.child.stderr.on("data", (chunk) => {
      this.stderr += chunk;
    });
    this.child.once("exit", (code, signal) => {
      const detail = this.stderr.trim();
      const error = new Error(
        `Computer Use Helper exited early: code=${code} signal=${signal}${detail ? `: ${detail}` : ""}`,
      );
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timer);
        pending.reject(error);
      }
      this.pending.clear();
    });
  }

  request(command, params = {}, timeoutMs = 15_000) {
    const id = `smoke_${this.nextID++}`;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`Computer Use Helper timed out: ${command}`));
      }, timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      this.child.stdin.write(`${JSON.stringify({ id, command, params })}\n`);
    });
  }

  resolveLine(line) {
    let response;
    try {
      response = JSON.parse(line);
    } catch {
      return;
    }
    const pending = this.pending.get(response.id);
    if (!pending) return;
    this.pending.delete(response.id);
    clearTimeout(pending.timer);
    if (!response.ok) {
      const error = new Error(`${response.error?.code ?? "computer_error"}: ${response.error?.message ?? "unknown error"}`);
      error.code = response.error?.code;
      pending.reject(error);
      return;
    }
    pending.resolve(response.result);
  }

  async close() {
    if (this.child.exitCode !== null) return;
    this.child.stdin.end();
    await Promise.race([
      new Promise((resolve) => this.child.once("exit", resolve)),
      new Promise((resolve) => setTimeout(resolve, 2_000)),
    ]);
    if (this.child.exitCode === null) this.child.kill("SIGTERM");
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForInventory(client) {
  const deadline = Date.now() + 8_000;
  while (Date.now() < deadline) {
    const inventory = await client.request("list_apps");
    const app = inventory.apps.find((item) => item.bundleID === fixtureBundleID);
    const windows = inventory.capturableWindows.filter(
      (item) => item.bundleID === fixtureBundleID,
    );
    if (app && app.windows.length >= 2 && windows.length >= 2) {
      return { inventory, app, windows };
    }
    await sleep(150);
  }
  throw new Error("fixture app did not expose two controllable windows");
}

async function waitForClosedWindows(client) {
  const deadline = Date.now() + 8_000;
  while (Date.now() < deadline) {
    const inventory = await client.request("list_apps");
    const windows = inventory.capturableWindows.filter(
      (item) => item.bundleID === fixtureBundleID,
    );
    if (windows.length === 0) {
      return;
    }
    await sleep(150);
  }
  throw new Error("fixture windows remained visible after close");
}

function elementWithAction(observation, action, predicate = () => true) {
  return observation.elements.find(
    (element) => element.actions.includes(action) && predicate(element),
  );
}

async function main() {
  for (const required of [fixtureApp, helper, launchServices]) {
    assert(fs.existsSync(required), `missing Computer Use smoke dependency: ${required}`);
  }
  execFileSync(launchServices, ["-f", fixtureApp], { stdio: "ignore" });

  const client = new HelperClient(helper);
  let launched;
  try {
    const permissions = await client.request("permissions");
    assert(
      permissions.accessibility && permissions.screenRecording,
      "grant Accessibility and Screen Recording to bin/Pudding Computer Use.app before running this smoke",
    );

    const before = await client.request("list_apps");
    assert(
      !before.apps.some((app) => app.bundleID === fixtureBundleID),
      "Pudding Computer Use Fixture is already running; close it before the smoke",
    );

    launched = await client.request("use_app", { bundleID: fixtureBundleID });
    assert(launched.newlyLaunched === true && launched.pid > 0, "fixture launch was not newly owned");

    const { app, windows } = await waitForInventory(client);
    assert(app.controllable === true, "fixture app is not controllable");
    const primary = windows.find((window) => window.title === "Computer Use Fixture");
    assert(primary?.windowID > 0, "fixture primary window was not discovered");

    let observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
    });
    const editable = elementWithAction(
      observation,
      "set_value",
      (element) => element.role === "AXTextField" && !element.secure,
    );
    assert(editable, "fixture editable text field was not observed");
    const value = `pudding-smoke-${Date.now()}`;
    await client.request("act", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      elementID: editable.elementID,
      action: "set_value",
      value,
    });

    observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
    });
    assert(
      observation.elements.some((element) => element.elementID === editable.elementID && element.value === value),
      "fixture text value did not persist after set_value",
    );
    const secure = observation.elements.find((element) => element.secure);
    assert(secure && secure.value == null && !secure.actions.includes("set_value"), "secure field was not redacted");
    const increment = elementWithAction(
      observation,
      "press",
      (element) => element.label === "Increment" || element.description === "Increment",
    );
    assert(increment, "fixture increment button was not observed");
    await client.request("act", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      elementID: increment.elementID,
      action: "press",
    });

    observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
    });
    assert(
      observation.elements.some(
        (element) =>
          (element.label === "Fixture count" || element.description === "Fixture count") &&
          element.value === "1",
      ),
      "fixture increment action did not update the count",
    );

    const closeWindows = elementWithAction(
      observation,
      "press",
      (element) => element.label === "Close Windows" || element.description === "Close Windows",
    );
    assert(closeWindows, "fixture close-windows button was not observed");
    await client.request("act", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      elementID: closeWindows.elementID,
      action: "press",
    });
    await waitForClosedWindows(client);

    const reused = await client.request("use_app", { bundleID: fixtureBundleID });
    assert(reused.newlyLaunched === false, "using a running fixture app incorrectly created launch ownership");
    assert(reused.pid === launched.pid, "using a running fixture app changed its process");
    await waitForInventory(client);

    const quit = await client.request("quit_app", {
      bundleID: fixtureBundleID,
      pid: launched.pid,
    });
    assert(quit.closed === true, "fixture app did not close normally");
    launched = undefined;
    console.log("Computer Use fixture smoke passed: start, activate/reopen, two windows, set_value, press, secure redaction, quit");
  } finally {
    if (launched?.pid) {
      await client.request("quit_app", { bundleID: fixtureBundleID, pid: launched.pid }).catch(() => {});
    }
    await client.close();
  }
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
