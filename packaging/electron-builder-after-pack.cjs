const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

module.exports = async function afterPack(context) {
  if (context.electronPlatformName !== "darwin") {
    return;
  }
  const root = path.resolve(__dirname, "..");
  const appName = context.packager.appInfo.productFilename;
  const appPath = path.join(context.appOutDir, `${appName}.app`);
  const appRoot = path.join(appPath, "Contents", "Resources", "app");
  const daemonPath = path.join(appRoot, "bin", "puddingd");
  const languageServersPath = path.join(appRoot, "language-servers");
  const launcherPath = path.join(languageServersPath, "typescript-language-server");
  const electronNodePath = path.join(languageServersPath, "node");

  execFileSync("bash", [
    path.join(root, "scripts", "macos-bundle-dylibs.sh"),
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
};
