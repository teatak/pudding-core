const { spawn } = require("node:child_process");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");

const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");

const root = path.resolve(process.argv[2] || path.join(__dirname, "..", "dist", "release"));
const port = positiveInt(process.env.PUDDING_UPDATE_TEST_PORT, 8099);
const feedURL = `http://127.0.0.1:${port}`;
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version);
const appExecutable =
  process.env.PUDDING_UPDATE_TEST_APP || "/Applications/Pudding.app/Contents/MacOS/Pudding";

let appProcess = null;
let feedProcess = null;
let stopping = false;

void main().catch((error) => {
  console.error(`Unable to run update test: ${error.message}`);
  shutdown(1);
});

process.on("SIGINT", () => shutdown(0));
process.on("SIGTERM", () => shutdown(0));

async function main() {
  if (!fs.existsSync(appExecutable)) {
    throw new Error(`Pudding executable not found: ${appExecutable}`);
  }
  if (!fs.existsSync(path.join(root, releaseChannel.updateInfoFile))) {
    throw new Error(`${releaseChannel.updateInfoFile} not found in ${root}`);
  }

  warnForOldInstalledBuild(appExecutable);
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

function warnForOldInstalledBuild(executable) {
  const asarPath = path.resolve(path.dirname(executable), "..", "Resources", "app.asar");
  if (!fs.existsSync(asarPath)) {
    return;
  }
  try {
    const asar = require("@electron/asar");
    const metadata = JSON.parse(asar.extractFile(asarPath, "package.json").toString());
    if (metadata.puddingUpdateMode !== "manual" && metadata.puddingUpdateMode !== "automatic") {
      console.warn("Warning: /Applications/Pudding.app does not contain the latest update mode metadata.");
      return;
    }
    console.log(`Installed Pudding update mode: ${metadata.puddingUpdateMode}`);
  } catch (error) {
    console.warn(`Unable to verify installed Pudding test build: ${error.message}`);
  }
}

function shutdown(code) {
  if (stopping) {
    return;
  }
  stopping = true;
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
