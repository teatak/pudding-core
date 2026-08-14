#!/usr/bin/env node

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync, spawn } = require("node:child_process");
const { ComputerUseHost } = require("../electron/computer-use-host.cjs");
const { ComputerUseBridgeServer } = require("../electron/computer-use-bridge-server.cjs");

const root = path.resolve(__dirname, "..");
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
const scenarios = {
  fixture: {
    appID: "com.teatak.pudding.computer-use-fixture",
    env: "PUDDING_COMPUTER_USE_PRODUCT_SMOKE",
    test: "TestComputerUseProductSmoke",
    required: [fixtureApp],
  },
  calculator: {
    appID: "com.apple.calculator",
    env: "PUDDING_COMPUTER_USE_CALCULATOR_SMOKE",
    test: "TestComputerUseCalculatorSmoke",
    required: [],
  },
  "calculator-existing": {
    appID: "com.apple.calculator",
    env: "PUDDING_COMPUTER_USE_CALCULATOR_EXISTING_SMOKE",
    test: "TestComputerUseCalculatorExistingSmoke",
    required: [],
    prelaunch: true,
  },
};

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function runGoSmoke(identity, scenario) {
  return new Promise((resolve, reject) => {
    const child = spawn(
      "go",
      ["test", "./internal/engine", "-run", `^${scenario.test}$`, "-count=1", "-timeout=45s", "-v"],
      {
        cwd: root,
        env: {
          ...process.env,
          GOCACHE: process.env.GOCACHE || path.join(os.tmpdir(), "pudding-go-cache"),
          [scenario.env]: "1",
          PUDDING_ELECTRON_COMPUTER_BRIDGE_URL: identity.url,
          PUDDING_ELECTRON_COMPUTER_BRIDGE_TOKEN: identity.token,
        },
        stdio: "inherit",
      },
    );
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`Computer Use product smoke failed: code=${code} signal=${signal}`));
    });
  });
}

async function closeLeakedApp(host, appID, expectedPID = 0) {
  const inventory = await host.listApps().catch(() => null);
  const app = inventory?.apps?.find((item) => item.bundleID === appID);
  if (app?.pid > 0 && (!expectedPID || app.pid === expectedPID)) {
    await host.quitApp({ bundleID: appID, pid: app.pid }).catch(() => {});
  }
}

async function main() {
  assert(process.platform === "darwin", "Computer Use product smoke only runs on macOS");
  const scenarioName = process.argv.includes("--calculator-existing")
    ? "calculator-existing"
    : process.argv.includes("--calculator")
      ? "calculator"
      : "fixture";
  const scenario = scenarios[scenarioName];
  for (const required of [helper, launchServices, ...scenario.required]) {
    assert(fs.existsSync(required), `missing Computer Use product smoke dependency: ${required}`);
  }
  if (scenarioName === "fixture") {
    execFileSync(launchServices, ["-f", fixtureApp], { stdio: "ignore" });
  }

  const host = new ComputerUseHost({ binaryPath: helper });
  const bridge = new ComputerUseBridgeServer(host);
  let cleanupTarget = false;
  let cleanupPID = 0;
  try {
    const permissions = await host.permissions();
    assert(
      permissions.accessibility && permissions.screenRecording,
      "grant Accessibility and Screen Recording to bin/Pudding Computer Use.app before running this smoke",
    );
    const before = await host.listApps();
    assert(
      !before.apps.some((app) => app.bundleID === scenario.appID),
      `${scenarioName} is already running; close it before the product smoke`,
    );
    if (scenario.prelaunch) {
      const launched = await host.launchApp({ bundleID: scenario.appID });
      cleanupPID = launched.pid;
      cleanupTarget = cleanupPID > 0;
      assert(launched.newlyLaunched && launched.pid > 0, `${scenarioName} test setup did not own the launch`);
    } else {
      cleanupTarget = true;
    }

    const identity = await bridge.start();
    await runGoSmoke(identity, scenario);
    if (scenario.prelaunch) {
      const after = await host.listApps();
      assert(
        after.apps.some((app) => app.bundleID === scenario.appID && app.pid === cleanupPID),
        `${scenarioName} was closed even though the session did not own it`,
      );
    }
    console.log(`Computer Use ${scenarioName} smoke passed: model loop, one approval, Engine, Manager, bridge, Helper, app`);
  } finally {
    if (cleanupTarget) {
      await closeLeakedApp(host, scenario.appID, cleanupPID);
    }
    await bridge.stop();
    await host.stop();
  }
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
