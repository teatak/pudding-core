const { execFileSync, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const asar = require("@electron/asar");
const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");
const { verifyComputerUseHelper } = require("./computer-use-release-verification.cjs");

const root = path.resolve(__dirname, "..");
const outputDir = path.join(root, "dist", "release");
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version);
const signingIdentity = String(process.env.PUDDING_MAC_IDENTITY || "-").trim() || "-";
const signingAuthority =
  signingIdentity === "-" || /^Developer ID Application:/i.test(signingIdentity)
    ? signingIdentity
    : `Developer ID Application: ${signingIdentity}`;
const builds = [
  { arch: "arm64", machOArch: "arm64", appDirectory: "mac-arm64", minimumSystemVersion: "14.0" },
  { arch: "x64", machOArch: "x86_64", appDirectory: "mac", minimumSystemVersion: "15.5" },
].map((build) => ({
  ...build,
  appPath: path.join(outputDir, build.appDirectory, "Pudding.app"),
  dmgPath: path.join(outputDir, `Pudding-${version}-${build.arch}.dmg`),
  zipPath: path.join(outputDir, `Pudding-${version}-${build.arch}.zip`),
}));
const latestPath = path.join(outputDir, releaseChannel.updateInfoFile);

if (!version) {
  fail("package version is empty");
}
if (String(process.env.PUDDING_UPDATE_MODE || "").trim()) {
  fail("PUDDING_UPDATE_MODE is no longer supported; desktop updates are always automatic");
}
if (signingIdentity === "-") {
  fail("desktop releases require PUDDING_MAC_IDENTITY");
}

const requiredFiles = [latestPath];
for (const build of builds) {
  requiredFiles.push(
    path.join(build.appPath, "Contents", "Info.plist"),
    path.join(build.appPath, "Contents", "Resources", "app.asar"),
    build.dmgPath,
    `${build.dmgPath}.blockmap`,
    build.zipPath,
    `${build.zipPath}.blockmap`,
  );
}
for (const filePath of requiredFiles) {
  if (!fs.existsSync(filePath)) {
    fail(`missing release artifact: ${filePath}`);
  }
}

for (const build of builds) {
  const infoPlist = path.join(build.appPath, "Contents", "Info.plist");
  const asarPath = path.join(build.appPath, "Contents", "Resources", "app.asar");
  const plistVersion = execFileSync(
    "plutil",
    ["-extract", "CFBundleShortVersionString", "raw", infoPlist],
    { encoding: "utf8" },
  ).trim();
  if (plistVersion !== version) {
    fail(`${build.arch} Info.plist version ${plistVersion} does not match ${version}`);
  }
  const minimumSystemVersion = execFileSync(
    "plutil",
    ["-extract", "LSMinimumSystemVersion", "raw", infoPlist],
    { encoding: "utf8" },
  ).trim();
  if (minimumSystemVersion !== build.minimumSystemVersion) {
    fail(
      `${build.arch} minimum system version ${minimumSystemVersion} ` +
        `does not match ${build.minimumSystemVersion}`,
    );
  }

  const bundledMetadata = JSON.parse(asar.extractFile(asarPath, "package.json").toString());
  if (bundledMetadata.version !== version) {
    fail(`${build.arch} bundled package version ${bundledMetadata.version} does not match ${version}`);
  }
  if (bundledMetadata.puddingReleaseChannel !== releaseChannel.channel) {
    fail(
      `${build.arch} bundled release channel ${bundledMetadata.puddingReleaseChannel} ` +
        `does not match ${releaseChannel.channel}`,
    );
  }
}

const latestMetadata = fs.readFileSync(latestPath, "utf8");
const latestVersion = latestMetadata.match(/^version:\s*['"]?([^'"\s]+)['"]?\s*$/m)?.[1] || "";
if (latestVersion !== version) {
  fail(`${releaseChannel.updateInfoFile} version ${latestVersion || "<missing>"} does not match ${version}`);
}
for (const build of builds) {
  for (const artifactName of [path.basename(build.dmgPath), path.basename(build.zipPath)]) {
    if (!latestMetadata.includes(artifactName)) {
      fail(`${releaseChannel.updateInfoFile} does not reference ${artifactName}`);
    }
  }
}

for (const build of builds) {
  verifyAppBundle(build.appPath, `${build.arch} staged app`, build.machOArch, true);
  execFileSync("hdiutil", ["verify", build.dmgPath], { stdio: "inherit" });
  verifyZipArtifact(build.zipPath, build.machOArch, build.arch);
  verifyDmgArtifact(build.dmgPath, build.machOArch, build.arch);
}
console.log(
  `Verified desktop release: version=${version} channel=${releaseChannel.channel} ` +
    `architectures=${builds.map((build) => build.arch).join(",")} mode=automatic`,
);

function commandOutput(command, args) {
  const result = spawnSync(command, args, { encoding: "utf8" });
  if (result.status !== 0) {
    fail(`${command} ${args.join(" ")} failed: ${(result.stderr || result.stdout || "").trim()}`);
  }
  return `${result.stdout || ""}\n${result.stderr || ""}`;
}

function verifyAppBundle(bundlePath, label, expectedArch, verifyCustomCode = false) {
  const readOnlyBundleEntries = findReadOnlyEntries(bundlePath);
  if (readOnlyBundleEntries.length > 0) {
    fail(
      `${label} contains entries that prevent Squirrel.Mac from clearing quarantine attributes: ${readOnlyBundleEntries
        .slice(0, 5)
        .map((filePath) => path.relative(bundlePath, filePath))
        .join(", ")}`,
    );
  }

  verifySignedCode(bundlePath, label, true);
  verifyMachOArchitecture(path.join(bundlePath, "Contents", "MacOS", "Pudding"), label, expectedArch);
  verifyHardwareEntitlements(bundlePath, label);
  verifyUsageDescriptions(bundlePath, label);
  verifyLegalNotices(bundlePath, label);
  try {
    verifyComputerUseHelper(bundlePath, { label, expectedArch, signingAuthority });
  } catch (error) {
    fail(error.message);
  }
  if (verifyCustomCode) {
    const appRoot = path.join(bundlePath, "Contents", "Resources", "app");
    const daemonPath = path.join(appRoot, "bin", "puddingd");
    const customCode = [
      daemonPath,
      path.join(appRoot, "language-servers", "gopls"),
      ...findFiles(path.join(appRoot, "lib"), (filePath) => filePath.endsWith(".dylib")),
    ];
    for (const codePath of customCode) {
      if (!fs.existsSync(codePath)) {
        fail(`${label} is missing signed runtime code: ${path.relative(bundlePath, codePath)}`);
      }
      verifySignedCode(codePath, `${label}:${path.relative(bundlePath, codePath)}`, false);
      verifyMachOArchitecture(codePath, label, expectedArch);
      verifyPortableDependencies(codePath, label);
    }
    verifyHardwareEntitlements(daemonPath, `${label}:puddingd`);
  }
  execFileSync("xcrun", ["stapler", "validate", bundlePath], { stdio: "inherit" });
  execFileSync("spctl", ["--assess", "--type", "execute", "--verbose=4", bundlePath], { stdio: "inherit" });
}

function verifyLegalNotices(bundlePath, label) {
  const legalPath = path.join(bundlePath, "Contents", "Resources", "legal");
  const requiredFiles = [
    ["PUDDING-LICENSE.txt", 10_000],
    ["THIRD_PARTY_NOTICES.txt", 10_000],
    ["LICENSES.chromium.html", 10_000],
  ];
  for (const [fileName, minimumSize] of requiredFiles) {
    const filePath = path.join(legalPath, fileName);
    const stat = fs.statSync(filePath, { throwIfNoEntry: false });
    if (!stat?.isFile() || stat.size < minimumSize) {
      fail(`${label} is missing a complete legal notice: ${fileName}`);
    }
  }

  const notices = fs.readFileSync(path.join(legalPath, "THIRD_PARTY_NOTICES.txt"), "utf8");
  for (const component of [
    "Electron",
    "ONNX Runtime",
    "PortAudio",
    "WebRTC Audio Processing",
    "Abseil C++",
    "github.com/teatak/seg",
  ]) {
    if (!notices.includes(component)) {
      fail(`${label} third-party notices do not include ${component}`);
    }
  }
}

function verifyMachOArchitecture(codePath, label, expectedArch) {
  const architectures = commandOutput("lipo", ["-archs", codePath]).trim().split(/\s+/);
  if (architectures.length !== 1 || architectures[0] !== expectedArch) {
    fail(
      `${label}:${path.basename(codePath)} has architecture ${architectures.join(",") || "<unknown>"}; ` +
        `expected ${expectedArch}`,
    );
  }
}

function verifyHardwareEntitlements(codePath, label) {
  const entitlements = commandOutput("codesign", ["-d", "--entitlements", "-", codePath]);
  for (const entitlement of [
    "com.apple.security.device.audio-input",
    "com.apple.security.device.camera",
  ]) {
    if (!entitlements.includes(entitlement)) {
      fail(`${label} is missing ${entitlement}`);
    }
  }
}

function verifyUsageDescriptions(bundlePath, label) {
  const infoPlistPath = path.join(bundlePath, "Contents", "Info.plist");
  for (const usageDescription of [
    "NSCameraUsageDescription",
    "NSLocalNetworkUsageDescription",
    "NSMicrophoneUsageDescription",
    "NSScreenCaptureUsageDescription",
  ]) {
    const value = commandOutput("plutil", ["-extract", usageDescription, "raw", infoPlistPath]).trim();
    if (!value) {
      fail(`${label} is missing ${usageDescription}`);
    }
  }
  const infoPlist = commandOutput("plutil", ["-p", infoPlistPath]);
  for (const unusedDescription of [
    "NSAudioCaptureUsageDescription",
    "NSBluetoothAlwaysUsageDescription",
    "NSBluetoothPeripheralUsageDescription",
  ]) {
    if (infoPlist.includes(`"${unusedDescription}" =>`)) {
      fail(`${label} contains unused privacy declaration ${unusedDescription}`);
    }
  }
}

function verifySignedCode(codePath, label, deep) {
  const args = ["--verify", "--strict", "--verbose=4"];
  if (deep) {
    args.splice(1, 0, "--deep");
  }
  execFileSync("codesign", [...args, codePath], { stdio: "inherit" });
  const details = commandOutput("codesign", ["-dv", "--verbose=4", codePath]);
  if (!details.includes(`Authority=${signingAuthority}`)) {
    fail(`${label} is not signed by ${signingAuthority}`);
  }
  if (details.includes("Signature=adhoc") || !details.includes("TeamIdentifier=")) {
    fail(`${label} does not have a valid Developer ID signature`);
  }
}

function verifyPortableDependencies(codePath, label) {
  const dependencies = commandOutput("otool", ["-L", codePath]);
  if (/\s(?:\/Users\/|\/opt\/homebrew\/|\/usr\/local\/)/.test(dependencies)) {
    fail(`${label} contains a non-portable dependency in ${path.basename(codePath)}`);
  }
}

function verifyZipArtifact(artifactPath, expectedArch, arch) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-release-zip-"));
  try {
    execFileSync("ditto", ["-x", "-k", artifactPath, tempDir], { stdio: "inherit" });
    verifyAppBundle(path.join(tempDir, "Pudding.app"), `${arch} ZIP app`, expectedArch);
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

function verifyDmgArtifact(artifactPath, expectedArch, arch) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-release-dmg-"));
  const mountPoint = path.join(tempDir, "mount");
  fs.mkdirSync(mountPoint);
  let mounted = false;
  try {
    execFileSync("hdiutil", ["attach", "-nobrowse", "-readonly", "-mountpoint", mountPoint, artifactPath], {
      stdio: "inherit",
    });
    mounted = true;
    verifyAppBundle(path.join(mountPoint, "Pudding.app"), `${arch} DMG app`, expectedArch);
  } finally {
    if (mounted) {
      execFileSync("hdiutil", ["detach", mountPoint], { stdio: "inherit" });
    }
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

function findFiles(rootPath, predicate) {
  if (!fs.existsSync(rootPath)) {
    return [];
  }
  const matches = [];
  const pending = [rootPath];
  while (pending.length > 0) {
    const current = pending.pop();
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const entryPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        pending.push(entryPath);
      } else if (entry.isFile() && predicate(entryPath)) {
        matches.push(entryPath);
      }
    }
  }
  return matches.sort();
}

function findReadOnlyEntries(rootPath) {
  const matches = [];
  const pending = [rootPath];
  while (pending.length > 0) {
    const current = pending.pop();
    const stat = fs.lstatSync(current);
    if (stat.isSymbolicLink()) {
      continue;
    }
    if ((stat.mode & 0o200) === 0) {
      matches.push(current);
    }
    if (stat.isDirectory()) {
      for (const entry of fs.readdirSync(current)) {
        pending.push(path.join(current, entry));
      }
    }
  }
  return matches.sort();
}

function fail(message) {
  console.error(`Desktop release verification failed: ${message}`);
  process.exit(1);
}
