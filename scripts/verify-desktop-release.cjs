const { execFileSync, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const asar = require("@electron/asar");
const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");

const root = path.resolve(__dirname, "..");
const outputDir = path.join(root, "dist", "release");
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version);
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
if (String(process.env.PUDDING_UPDATE_MODE || "").trim()) {
  fail("PUDDING_UPDATE_MODE is no longer supported; desktop updates are always automatic");
}
if (signingIdentity === "-") {
  fail("desktop releases require PUDDING_MAC_IDENTITY");
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

verifyAppBundle(appPath, "staged app", true);
execFileSync("hdiutil", ["verify", dmgPath], { stdio: "inherit" });
verifyZipArtifact(zipPath);
verifyDmgArtifact(dmgPath);
console.log(`Verified desktop release: version=${version} channel=${releaseChannel.channel} mode=automatic`);

function commandOutput(command, args) {
  const result = spawnSync(command, args, { encoding: "utf8" });
  if (result.status !== 0) {
    fail(`${command} ${args.join(" ")} failed: ${(result.stderr || result.stdout || "").trim()}`);
  }
  return `${result.stdout || ""}\n${result.stderr || ""}`;
}

function verifyAppBundle(bundlePath, label, verifyCustomCode = false) {
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
  if (verifyCustomCode) {
    const appRoot = path.join(bundlePath, "Contents", "Resources", "app");
    const customCode = [
      path.join(appRoot, "bin", "puddingd"),
      path.join(appRoot, "language-servers", "gopls"),
      ...findFiles(path.join(appRoot, "lib"), (filePath) => filePath.endsWith(".dylib")),
    ];
    for (const codePath of customCode) {
      if (!fs.existsSync(codePath)) {
        fail(`${label} is missing signed runtime code: ${path.relative(bundlePath, codePath)}`);
      }
      verifySignedCode(codePath, `${label}:${path.relative(bundlePath, codePath)}`, false);
      verifyPortableDependencies(codePath, label);
    }
  }
  execFileSync("xcrun", ["stapler", "validate", bundlePath], { stdio: "inherit" });
  execFileSync("spctl", ["--assess", "--type", "execute", "--verbose=4", bundlePath], { stdio: "inherit" });
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

function verifyZipArtifact(artifactPath) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-release-zip-"));
  try {
    execFileSync("ditto", ["-x", "-k", artifactPath, tempDir], { stdio: "inherit" });
    verifyAppBundle(path.join(tempDir, "Pudding.app"), "ZIP app");
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

function verifyDmgArtifact(artifactPath) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-release-dmg-"));
  const mountPoint = path.join(tempDir, "mount");
  fs.mkdirSync(mountPoint);
  let mounted = false;
  try {
    execFileSync("hdiutil", ["attach", "-nobrowse", "-readonly", "-mountpoint", mountPoint, artifactPath], {
      stdio: "inherit",
    });
    mounted = true;
    verifyAppBundle(path.join(mountPoint, "Pudding.app"), "DMG app");
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
