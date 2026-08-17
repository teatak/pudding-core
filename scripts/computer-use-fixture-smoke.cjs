#!/usr/bin/env node

const fs = require("node:fs");
const os = require("node:os");
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
    if (app?.running) {
      return { inventory, app };
    }
    await sleep(150);
  }
  throw new Error("fixture app did not appear as running");
}

async function waitForClosedWindow(client, windowID) {
  const deadline = Date.now() + 8_000;
  while (Date.now() < deadline) {
    try {
      await client.request("observe", {
        bundleID: fixtureBundleID,
        windowID,
        maxElements: 1,
      });
    } catch (error) {
      if (error.code === "computer_window_not_found") return;
      throw error;
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

function labeledElement(observation, label) {
  return observation.elements.find(
    (element) => element.label === label || element.description === label,
  );
}

async function waitForElementValue(client, windowID, label, value) {
  const deadline = Date.now() + 2_000;
  while (Date.now() < deadline) {
    const observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID,
      maxElements: 300,
    });
    if (labeledElement(observation, label)?.value === value) return;
    await sleep(50);
  }
  throw new Error(`fixture did not report pointer action: ${value}`);
}

async function main() {
  for (const required of [fixtureApp, helper, launchServices]) {
    assert(fs.existsSync(required), `missing Computer Use smoke dependency: ${required}`);
  }
  execFileSync(launchServices, ["-f", fixtureApp], { stdio: "ignore" });

  const client = new HelperClient(helper);
  const captureDir = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-computer-smoke-"));
  let launched;
  try {
    const permissions = await client.request("permissions");
    assert(
      permissions.accessibility && permissions.screenRecording,
      "grant Accessibility and Screen Recording to bin/Pudding Computer Use.app before running this smoke",
    );

    const before = await client.request("list_apps");
    const beforeFixture = before.apps.find((app) => app.bundleID === fixtureBundleID);
    assert(!beforeFixture || !beforeFixture.running, "Pudding Computer Use Fixture is already running; close it before the smoke");

    launched = await client.request("use_app", { bundleID: fixtureBundleID });
    assert(launched.newlyLaunched === true && launched.pid > 0, "fixture launch was not newly owned");
    assert(launched.windowStatus === "ready" && launched.windows.length >= 2, "fixture launch did not return both windows");

    const { app } = await waitForInventory(client);
    assert(app.controllable === true, "fixture app is not controllable");
    const primary = launched.windows.find((window) => window.title === "Computer Use Fixture");
    assert(primary?.windowID > 0, "fixture primary window was not discovered");

    const captureOutput = path.join(captureDir, "fixture.png");
    const observedCapture = await client.request("observe_capture", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
      output: captureOutput,
    });
    assert(
      observedCapture.observation?.windowID === primary.windowID,
      "combined observation targeted the wrong window",
    );
    assert(
      observedCapture.capture?.windowID === primary.windowID,
      "combined capture targeted the wrong window",
    );
    assert(
      observedCapture.capture?.width > 0 && observedCapture.capture?.height > 0,
      "combined capture returned invalid dimensions",
    );
    assert(
      fs.readFileSync(captureOutput).subarray(0, 8).equals(
        Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
      ),
      "combined capture did not write a PNG",
    );

    let observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
    });
    assert(observation.truncated === false, "fixture visible table traversal was unexpectedly truncated");
    const visibleRows = observation.elements.filter((element) => element.role === "AXRow");
    assert(visibleRows.length > 0 && visibleRows.length < 500, "fixture did not expose only visible table rows");
    const initialVisibleRowIDs = new Set(visibleRows.map((element) => element.elementID));
    const editable = elementWithAction(
      observation,
      "set_value",
      (element) => element.role === "AXTextField" && !element.secure,
    );
    assert(editable, "fixture editable text field was not observed");
    const value = "布丁".repeat(100);
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
    const edited = observation.elements.find((element) => element.elementID === editable.elementID);
    assert(
      edited?.valueTruncated === true && edited.value && value.startsWith(edited.value),
      "fixture text value did not persist with the expected bounded observation",
    );
    assert(edited.actions.includes("submit"), "fixture editable text field did not expose submit");
    await client.request("act", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      elementID: edited.elementID,
      action: "submit",
    });

    observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
    });
    assert(
      observation.elements.some(
        (element) =>
          (element.label === "Fixture confirmed value" || element.description === "Fixture confirmed value") &&
          element.value && value.startsWith(element.value),
      ),
      "fixture submit action did not submit the text field",
    );
    const scrolledRows = observation.elements.filter((element) => element.role === "AXRow");
    assert(
      scrolledRows.length > 0 && scrolledRows.every((element) => !initialVisibleRowIDs.has(element.elementID)),
      "fixture reused visible row element IDs after scrolling",
    );
    const selectableRow = elementWithAction(
      observation,
      "select",
      (element) => element.role === "AXRow",
    );
    assert(selectableRow, "fixture table row did not expose select");
    await client.request("act", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      elementID: selectableRow.elementID,
      action: "select",
    });

    observation = await client.request("observe", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
    });
    assert(
      observation.elements.some((element) => element.role === "AXRow" && element.selected === true),
      "fixture select action did not select a table row",
    );
    const secure = observation.elements.find((element) => element.secure);
    assert(
      secure && secure.value == null && secure.actions.length === 0,
      "secure field was not fully redacted and made non-actionable",
    );
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

    const pointerCapture = await client.request("observe_capture", {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
      maxElements: 300,
      output: path.join(captureDir, "fixture-pointer.png"),
    });
    assert(pointerCapture.capture?.windowID === primary.windowID, "pointer capture targeted the wrong window");
    const target = labeledElement(pointerCapture.observation, "Fixture pointer target");
    const observedWindow = pointerCapture.observation.windows.find(
      (window) => window.windowID === primary.windowID,
    );
    assert(target?.frame && observedWindow?.frame, "fixture pointer target geometry was not observed");
    const windowPoint = (fractionX, fractionY) => ({
      x: (target.frame.x - observedWindow.frame.x + target.frame.width * fractionX) / observedWindow.frame.width,
      y: (target.frame.y - observedWindow.frame.y + target.frame.height * fractionY) / observedWindow.frame.height,
    });
    const start = windowPoint(0.25, 0.5);
    const center = windowPoint(0.5, 0.5);
    const end = windowPoint(0.75, 0.5);
    const pointerBase = {
      bundleID: fixtureBundleID,
      windowID: primary.windowID,
    };
    await client.request("use_app", { bundleID: fixtureBundleID, foreground: true });
    for (const test of [
      { action: "click", x: center.x, y: center.y, button: "left", clickCount: 1, expected: "left click" },
      { action: "click", x: center.x, y: center.y, button: "left", clickCount: 2, expected: "double click" },
      { action: "click", x: center.x, y: center.y, button: "right", clickCount: 1, expected: "right click" },
      { action: "drag", x: start.x, y: start.y, toX: end.x, toY: end.y, expected: "drag released right" },
      { action: "scroll", x: center.x, y: center.y, deltaX: 0, deltaY: 120, expected: "scrolled down" },
    ]) {
      const { expected, ...params } = test;
      await client.request("pointer", { ...pointerBase, ...params });
      await waitForElementValue(client, primary.windowID, "Fixture pointer value", expected);
    }

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
    await waitForClosedWindow(client, primary.windowID);

    const reused = await client.request("use_app", {
      bundleID: fixtureBundleID,
      foreground: true,
    });
    assert(reused.newlyLaunched === false, "using a running fixture app incorrectly created launch ownership");
    assert(reused.pid === launched.pid, "using a running fixture app changed its process");
    assert(reused.windowStatus === "ready" && reused.windows.length >= 2, "fixture windows were not reopened");

    const quit = await client.request("quit_app", {
      bundleID: fixtureBundleID,
      pid: launched.pid,
    });
    assert(quit.closed === true, "fixture app did not close normally");
    launched = undefined;
    console.log("Computer Use fixture smoke passed: start, windows, visible rows, observe+capture, click, double-click, right-click, drag, scroll, set_value, submit, select, press, redaction, reopen, quit");
  } finally {
    if (launched?.pid) {
      await client.request("quit_app", { bundleID: fixtureBundleID, pid: launched.pid }).catch(() => {});
    }
    await client.close();
    fs.rmSync(captureDir, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
