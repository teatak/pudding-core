const { spawn, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");

const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");
const {
  hasComputerUseHelper,
  verifyComputerUseHelper,
} = require("./computer-use-release-verification.cjs");

const root = path.resolve(process.argv[2] || path.join(__dirname, "..", "dist", "release"));
const port = positiveInt(process.env.PUDDING_UPDATE_TEST_PORT, 8099);
const feedURL = `http://127.0.0.1:${port}`;
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version);
const appExecutable =
  process.env.PUDDING_UPDATE_TEST_APP || "/Applications/Pudding.app/Contents/MacOS/Pudding";
const expectedVersion = version;
const timeoutMs = positiveInt(process.env.PUDDING_UPDATE_TEST_TIMEOUT_SECONDS, 600) * 1000;
const requireComputerUseIdentity = process.env.PUDDING_UPDATE_TEST_REQUIRE_COMPUTER_USE_IDENTITY === "1";

let appProcess = null;
let feedProcess = null;
let verificationTimer = null;
let timeoutTimer = null;
let expectedVersionSeenAt = 0;
let stopping = false;
let installedComputerUseIdentity = null;

if (require.main === module) {
  void main().catch((error) => {
    console.error(`Unable to run update test: ${error.message}`);
    shutdown(1);
  });

  process.on("SIGINT", () => shutdown(0));
  process.on("SIGTERM", () => shutdown(0));
}

async function main() {
  if (!fs.existsSync(appExecutable)) {
    throw new Error(`Pudding executable not found: ${appExecutable}`);
  }
  if (!fs.existsSync(path.join(root, releaseChannel.updateInfoFile))) {
    throw new Error(`${releaseChannel.updateInfoFile} not found in ${root}`);
  }
  assertAppStopped();

  const installed = readInstalledBuild(appExecutable);
  if (!installed.version) {
    throw new Error("unable to read the installed Pudding version");
  }
  if (installed.version === expectedVersion) {
    throw new Error(`installed Pudding is already ${expectedVersion}; install an older version first`);
  }
  const sourceAppPath = installedAppPath(appExecutable);
  assertBundleWritable(sourceAppPath, "installed source bundle");
  if (hasComputerUseHelper(sourceAppPath)) {
    installedComputerUseIdentity = verifyComputerUseHelper(sourceAppPath, {
      label: "installed source bundle",
    });
    console.log(
      `Installed Computer Use identity: ${installedComputerUseIdentity.identifier} ` +
        `team=${installedComputerUseIdentity.teamIdentifier}`,
    );
  } else if (requireComputerUseIdentity) {
    throw new Error("installed source bundle does not contain Pudding Computer Use.app");
  }
  console.log(`Installed Pudding: version=${installed.version} channel=${installed.channel}`);
  if (!(await feedReady())) {
    feedProcess = spawn(process.execPath, [path.join(__dirname, "serve-update-feed.cjs"), root, String(port)], {
      stdio: "inherit",
    });
    await waitForFeed();
  }

  console.log(`Launching Pudding with update feed ${feedURL}`);
  appProcess = spawn(appExecutable, [], {
    env: {
      ...process.env,
      PUDDING_RECEIVE_PREVIEW_UPDATES: releaseChannel.channel === "preview" ? "1" : "0",
      PUDDING_UPDATE_FEED_URL: feedURL,
    },
    stdio: "inherit",
  });
  appProcess.on("error", (error) => {
    console.error(`Unable to launch Pudding: ${error.message}`);
    shutdown(1);
  });
  console.log(
    `Waiting up to ${Math.round(timeoutMs / 1000)}s for update approval and installation of ${expectedVersion}`,
  );
  verificationTimer = setInterval(checkInstalledUpdate, 1_000);
  timeoutTimer = setTimeout(() => {
    console.error(`Update test timed out before Pudding ${expectedVersion} was installed`);
    shutdown(1);
  }, timeoutMs);
}

async function waitForFeed() {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (await feedReady()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`update feed did not start at ${feedURL}`);
}

function feedReady() {
  return new Promise((resolve) => {
    const request = http.get(`${feedURL}/${releaseChannel.updateInfoFile}`, (response) => {
      response.resume();
      resolve(response.statusCode === 200);
    });
    request.setTimeout(500, () => request.destroy());
    request.on("error", () => resolve(false));
  });
}

function readInstalledBuild(executable) {
  const asarPath = path.resolve(path.dirname(executable), "..", "Resources", "app.asar");
  if (!fs.existsSync(asarPath)) {
    return { version: "", channel: "" };
  }
  try {
    const asar = require("@electron/asar");
    const metadata = JSON.parse(asar.extractFile(asarPath, "package.json").toString());
    return {
      version: readInstalledVersion(executable),
      channel: String(metadata.puddingReleaseChannel || "stable"),
    };
  } catch (error) {
    console.warn(`Unable to verify installed Pudding test build: ${error.message}`);
    return { version: "", channel: "" };
  }
}

function assertAppStopped() {
  const result = spawnSync("ps", ["-axo", "pid=,command="], { encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error("unable to inspect running Pudding processes");
  }
  const running = findRunningPuddingProcesses(result.stdout);
  if (running.length > 0) {
    throw new Error(
      `Pudding is already running; quit it before starting the update test: ${running.join(", ")}`,
    );
  }
}

function findRunningPuddingProcesses(output) {
  return String(output || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.includes("/Pudding.app/Contents/MacOS/Pudding"));
}

function checkInstalledUpdate() {
  if (stopping) {
    return;
  }
  if (readInstalledVersion(appExecutable) !== expectedVersion) {
    expectedVersionSeenAt = 0;
    return;
  }
  if (!expectedVersionSeenAt) {
    expectedVersionSeenAt = Date.now();
    return;
  }
  if (Date.now() - expectedVersionSeenAt < 2_000) {
    return;
  }
  try {
    verifyInstalledApp(installedAppPath(appExecutable), installedComputerUseIdentity);
    console.log(`Local update verified: ${expectedVersion}`);
    shutdown(0);
  } catch (error) {
    console.error(`Installed update verification failed: ${error.message}`);
    shutdown(1);
  }
}

function readInstalledVersion(executable) {
  const infoPlist = path.join(installedAppPath(executable), "Contents", "Info.plist");
  const result = spawnSync(
    "plutil",
    ["-extract", "CFBundleShortVersionString", "raw", infoPlist],
    { encoding: "utf8" },
  );
  return result.status === 0 ? String(result.stdout || "").trim() : "";
}

function verifyInstalledApp(appPath, expectedComputerUseIdentity = null) {
  assertBundleWritable(appPath, "installed update bundle");
  runCheck("codesign", ["--verify", "--deep", "--strict", "--verbose=2", appPath]);
  runCheck("xcrun", ["stapler", "validate", appPath]);
  runCheck("spctl", ["--assess", "--type", "execute", "--verbose=2", appPath]);
  verifyComputerUseHelper(appPath, {
    label: "installed update bundle",
    expectedIdentity: expectedComputerUseIdentity,
  });
}

function installedAppPath(executable) {
  return path.resolve(path.dirname(executable), "..", "..");
}

function assertBundleWritable(rootPath, label) {
  const unwritable = findUnwritableEntries(rootPath);
  if (unwritable.length > 0) {
    throw new Error(
      `${label} is not writable by the current user: ${unwritable.slice(0, 3).join(", ")}`,
    );
  }
}

function findUnwritableEntries(rootPath) {
  const found = [];
  const visit = (current) => {
    const stat = fs.lstatSync(current);
    if (stat.isSymbolicLink()) {
      return;
    }
    try {
      fs.accessSync(current, fs.constants.W_OK);
    } catch {
      found.push(current);
    }
    if (stat.isDirectory()) {
      for (const name of fs.readdirSync(current)) {
        visit(path.join(current, name));
      }
    }
  };
  visit(rootPath);
  return found;
}

function runCheck(command, args) {
  const result = spawnSync(command, args, { encoding: "utf8" });
  if (result.status !== 0) {
    const detail = String(result.stderr || result.stdout || result.error?.message || "").trim();
    throw new Error(`${command} ${args.join(" ")} failed${detail ? `: ${detail}` : ""}`);
  }
}

function shutdown(code) {
  if (stopping) {
    return;
  }
  stopping = true;
  clearInterval(verificationTimer);
  clearTimeout(timeoutTimer);
  if (appProcess && appProcess.exitCode === null) {
    appProcess.kill("SIGTERM");
  }
  if (feedProcess && feedProcess.exitCode === null) {
    feedProcess.kill("SIGTERM");
  }
  setTimeout(() => process.exit(code), 250);
}

function positiveInt(value, fallback) {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 && parsed <= 65535 ? parsed : fallback;
}

module.exports = {
  assertBundleWritable,
  findUnwritableEntries,
  findRunningPuddingProcesses,
  installedAppPath,
  readInstalledBuild,
  readInstalledVersion,
  verifyInstalledApp,
};
