const { execFileSync, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const asar = require("@electron/asar");
const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");

const root = path.resolve(__dirname, "..");
const outputDir = path.join(root, "dist", "release");
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version);
const mode = String(process.env.PUDDING_UPDATE_MODE || "automatic").trim().toLowerCase();
const signingIdentity = String(process.env.PUDDING_MAC_IDENTITY || "-").trim() || "-";
const signingAuthority =
  signingIdentity === "-" || /^Developer ID Application:/i.test(signingIdentity)
    ? signingIdentity
    : `Developer ID Application: ${signingIdentity}`;
const appPath = path.join(outputDir, "mac-arm64", "Pudding.app");
const infoPlist = path.join(appPath, "Contents", "Info.plist");
const asarPath = path.join(appPath, "Contents", "Resources", "app.asar");
const dmgPath = path.join(outputDir, `Pudding-${version}-arm64.dmg`);
const zipPath = path.join(outputDir, `Pudding-${version}-arm64.zip`);
const latestPath = path.join(outputDir, releaseChannel.updateInfoFile);

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
if (bundledMetadata.puddingReleaseChannel !== releaseChannel.channel) {
  fail(
    `bundled release channel ${bundledMetadata.puddingReleaseChannel} does not match ${releaseChannel.channel}`,
  );
}

const latestMetadata = fs.readFileSync(latestPath, "utf8");
const latestVersion = latestMetadata.match(/^version:\s*['"]?([^'"\s]+)['"]?\s*$/m)?.[1] || "";
if (latestVersion !== version) {
  fail(`${releaseChannel.updateInfoFile} version ${latestVersion || "<missing>"} does not match ${version}`);
}
for (const artifactName of [path.basename(dmgPath), path.basename(zipPath)]) {
  if (!latestMetadata.includes(artifactName)) {
    fail(`${releaseChannel.updateInfoFile} does not reference ${artifactName}`);
  }
}

execFileSync("codesign", ["--verify", "--deep", "--strict", appPath], { stdio: "inherit" });
const signatureDetails = commandOutput("codesign", ["-dv", "--verbose=4", appPath]);
if (signingIdentity === "-") {
  if (!signatureDetails.includes("Signature=adhoc")) {
    fail("manual ad-hoc build is not marked as ad-hoc signed");
  }
} else {
  if (!signatureDetails.includes(`Authority=${signingAuthority}`)) {
    fail(`app is not signed by ${signingAuthority}`);
  }
  if (signatureDetails.includes("Signature=adhoc") || !signatureDetails.includes("TeamIdentifier=")) {
    fail("Developer ID build has an invalid signing identity");
  }
  execFileSync("xcrun", ["stapler", "validate", appPath], { stdio: "inherit" });
  execFileSync("spctl", ["--assess", "--type", "execute", "--verbose=4", appPath], { stdio: "inherit" });
}
execFileSync("hdiutil", ["verify", dmgPath], { stdio: "inherit" });
console.log(`Verified desktop release: version=${version} channel=${releaseChannel.channel} mode=${mode}`);

function commandOutput(command, args) {
  const result = spawnSync(command, args, { encoding: "utf8" });
  if (result.status !== 0) {
    fail(`${command} ${args.join(" ")} failed: ${(result.stderr || result.stdout || "").trim()}`);
  }
  return `${result.stdout || ""}\n${result.stderr || ""}`;
}

function fail(message) {
  console.error(`Desktop release verification failed: ${message}`);
  process.exit(1);
}
