const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const asar = require("@electron/asar");
const packageMetadata = require("../package.json");

const root = path.resolve(__dirname, "..");
const outputDir = path.join(root, "dist", "release");
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const mode = String(process.env.PUDDING_UPDATE_MODE || "manual").trim().toLowerCase();
const signingIdentity = String(process.env.PUDDING_MAC_IDENTITY || "-").trim() || "-";
const appPath = path.join(outputDir, "mac-arm64", "Pudding.app");
const infoPlist = path.join(appPath, "Contents", "Info.plist");
const asarPath = path.join(appPath, "Contents", "Resources", "app.asar");
const dmgPath = path.join(outputDir, `Pudding-${version}-arm64.dmg`);
const zipPath = path.join(outputDir, `Pudding-${version}-arm64.zip`);
const latestPath = path.join(outputDir, "latest-mac.yml");

if (!version) {
  fail("package version is empty");
}
if (mode !== "manual" && mode !== "automatic") {
  fail(`invalid update mode: ${mode}`);
}
if (mode === "automatic" && signingIdentity === "-") {
  fail("automatic updates require PUDDING_MAC_IDENTITY; use manual mode for ad-hoc builds");
}

for (const filePath of [
  infoPlist,
  asarPath,
  dmgPath,
  `${dmgPath}.blockmap`,
  zipPath,
  `${zipPath}.blockmap`,
  latestPath,
]) {
  if (!fs.existsSync(filePath)) {
    fail(`missing release artifact: ${filePath}`);
  }
}

const plistVersion = execFileSync("plutil", ["-extract", "CFBundleShortVersionString", "raw", infoPlist], {
  encoding: "utf8",
}).trim();
if (plistVersion !== version) {
  fail(`Info.plist version ${plistVersion} does not match ${version}`);
}

const bundledMetadata = JSON.parse(asar.extractFile(asarPath, "package.json").toString());
if (bundledMetadata.version !== version) {
  fail(`bundled package version ${bundledMetadata.version} does not match ${version}`);
}
if (bundledMetadata.puddingUpdateMode !== mode) {
  fail(`bundled update mode ${bundledMetadata.puddingUpdateMode} does not match ${mode}`);
}

const latestMetadata = fs.readFileSync(latestPath, "utf8");
const latestVersion = latestMetadata.match(/^version:\s*['"]?([^'"\s]+)['"]?\s*$/m)?.[1] || "";
if (latestVersion !== version) {
  fail(`latest-mac.yml version ${latestVersion || "<missing>"} does not match ${version}`);
}
for (const artifactName of [path.basename(dmgPath), path.basename(zipPath)]) {
  if (!latestMetadata.includes(artifactName)) {
    fail(`latest-mac.yml does not reference ${artifactName}`);
  }
}

execFileSync("codesign", ["--verify", "--deep", "--strict", appPath], { stdio: "inherit" });
execFileSync("hdiutil", ["verify", dmgPath], { stdio: "inherit" });
console.log(`Verified desktop release: version=${version} mode=${mode}`);

function fail(message) {
  console.error(`Desktop release verification failed: ${message}`);
  process.exit(1);
}
