const { execFileSync } = require("node:child_process");
const { Arch } = require("builder-util");
const fs = require("node:fs");
const path = require("node:path");

module.exports = async function afterPack(context) {
  if (context.electronPlatformName !== "darwin") {
    return;
  }
  const root = path.resolve(__dirname, "..");
  const appName = context.packager.appInfo.productFilename;
  const appPath = path.join(context.appOutDir, `${appName}.app`);
  const infoPlistPath = path.join(appPath, "Contents", "Info.plist");
  const appRoot = path.join(appPath, "Contents", "Resources", "app");
  const arch = Arch[context.arch];
  if (arch !== "arm64" && arch !== "x64") {
    throw new Error(`unsupported macOS desktop architecture: ${arch}`);
  }
  const runtimeRoot = path.join(root, "dist", "runtime", arch);
  const daemonPath = path.join(appRoot, "bin", "puddingd");
  const languageServersPath = path.join(appRoot, "language-servers");
  const launcherPath = path.join(languageServersPath, "typescript-language-server");
  const electronNodePath = path.join(languageServersPath, "node");

  fs.mkdirSync(path.dirname(daemonPath), { recursive: true });
  fs.copyFileSync(path.join(runtimeRoot, "puddingd"), daemonPath);
  copyDirectoryWithPortableSymlinks(
    path.join(runtimeRoot, "language-servers"),
    languageServersPath,
  );
  setMinimumSystemVersion(infoPlistPath, arch);

  execFileSync("bash", [
    path.join(root, "packaging", "macos", "bundle-dylibs.sh"),
    daemonPath,
    path.join(appRoot, "lib"),
  ]);

  fs.writeFileSync(
    launcherPath,
    `#!/bin/sh
set -eu

SERVER_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
export ELECTRON_RUN_AS_NODE=1
exec "$SERVER_ROOT/node" "$SERVER_ROOT/typescript/node_modules/typescript-language-server/lib/cli.mjs" "$@"
`,
    { mode: 0o755 },
  );
  fs.rmSync(electronNodePath, { force: true });
  fs.symlinkSync(`../../../MacOS/${appName}`, electronNodePath);
  fs.chmodSync(daemonPath, 0o755);
  removeUnusedPrivacyUsageDescriptions(infoPlistPath);
  makeBundleOwnerWritable(appPath);
};

function setMinimumSystemVersion(infoPlistPath, arch) {
  const minimumVersion = arch === "x64" ? "15.5" : "14.0";
  execFileSync("plutil", [
    "-replace",
    "LSMinimumSystemVersion",
    "-string",
    minimumVersion,
    infoPlistPath,
  ]);
}

function copyDirectoryWithPortableSymlinks(sourceRoot, destinationRoot) {
  fs.cpSync(sourceRoot, destinationRoot, {
    recursive: true,
    verbatimSymlinks: true,
  });
  const sourceBase = path.resolve(sourceRoot);
  const destinationBase = path.resolve(destinationRoot);
  const pending = [destinationBase];
  while (pending.length > 0) {
    const current = pending.pop();
    const stat = fs.lstatSync(current);
    if (stat.isSymbolicLink()) {
      const target = fs.readlinkSync(current);
      if (!path.isAbsolute(target)) {
        continue;
      }
      const sourceTarget = path.resolve(target);
      const relativeTarget = path.relative(sourceBase, sourceTarget);
      if (!isPathInside(relativeTarget)) {
        throw new Error(`runtime symlink points outside its source directory: ${current} -> ${target}`);
      }
      const copiedTarget = path.join(destinationBase, relativeTarget);
      if (!fs.existsSync(copiedTarget)) {
        throw new Error(`runtime symlink target was not copied: ${current} -> ${target}`);
      }
      const portableTarget = path.relative(path.dirname(current), copiedTarget) || ".";
      fs.rmSync(current);
      fs.symlinkSync(portableTarget, current);
      continue;
    }
    if (stat.isDirectory()) {
      for (const entry of fs.readdirSync(current)) {
        pending.push(path.join(current, entry));
      }
    }
  }
}

function isPathInside(relativePath) {
  return relativePath === "" || (
    relativePath !== ".." &&
    !relativePath.startsWith(`..${path.sep}`) &&
    !path.isAbsolute(relativePath)
  );
}

function removeUnusedPrivacyUsageDescriptions(infoPlistPath) {
  const contents = execFileSync("plutil", ["-p", infoPlistPath], { encoding: "utf8" });
  for (const key of [
    "NSAudioCaptureUsageDescription",
    "NSBluetoothAlwaysUsageDescription",
    "NSBluetoothPeripheralUsageDescription",
  ]) {
    if (contents.includes(`"${key}" =>`)) {
      execFileSync("plutil", ["-remove", key, infoPlistPath]);
    }
  }
}

function makeBundleOwnerWritable(rootPath) {
  const pending = [rootPath];
  while (pending.length > 0) {
    const current = pending.pop();
    const stat = fs.lstatSync(current);
    if (stat.isSymbolicLink()) {
      continue;
    }
    if (stat.isDirectory()) {
      if ((stat.mode & 0o200) === 0) {
        fs.chmodSync(current, stat.mode | 0o200);
      }
      for (const entry of fs.readdirSync(current)) {
        pending.push(path.join(current, entry));
      }
      continue;
    }
    if (stat.isFile() && (stat.mode & 0o200) === 0) {
      fs.chmodSync(current, stat.mode | 0o200);
    }
  }
}

module.exports.makeBundleOwnerWritable = makeBundleOwnerWritable;
module.exports.copyDirectoryWithPortableSymlinks = copyDirectoryWithPortableSymlinks;
module.exports.removeUnusedPrivacyUsageDescriptions = removeUnusedPrivacyUsageDescriptions;
module.exports.setMinimumSystemVersion = setMinimumSystemVersion;
