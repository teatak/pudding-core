#!/usr/bin/env node

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

if (process.platform !== "darwin") {
  process.exit(0);
}

const root = path.resolve(__dirname, "..");
const electronExecutable = require(path.join(root, "web", "node_modules", "electron"));
const mainScript = path.join(root, "electron", "main.cjs");
const devHome = path.join(os.homedir(), ".pudding-dev");
const relayRoot = path.join(devHome, "oauth-return");
const appBundle = path.join(relayRoot, "Pudding Dev OAuth.app");
const contents = path.join(appBundle, "Contents");
const sourcePath = path.join(relayRoot, "pudding-dev-oauth.applescript");
const logPath = path.join(devHome, "logs", "oauth-relay.log");
const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";

fs.mkdirSync(relayRoot, { recursive: true, mode: 0o700 });
fs.mkdirSync(path.dirname(logPath), { recursive: true, mode: 0o700 });
fs.rmSync(appBundle, { recursive: true, force: true });
fs.writeFileSync(sourcePath, relayAppleScript(), { mode: 0o600 });
const compiled = spawnSync("/usr/bin/osacompile", ["-o", appBundle, sourcePath], { encoding: "utf8" });
if (compiled.status !== 0) {
  const detail = String(compiled.stderr || compiled.stdout || "").trim();
  throw new Error(`failed to compile pudding-dev protocol relay${detail ? `: ${detail}` : ""}`);
}
fs.writeFileSync(path.join(contents, "Info.plist"), infoPlist(), { mode: 0o600 });

const registered = spawnSync(lsregister, ["-f", appBundle], { encoding: "utf8" });
if (registered.status !== 0) {
  const detail = String(registered.stderr || registered.stdout || "").trim();
  throw new Error(`failed to register pudding-dev protocol relay${detail ? `: ${detail}` : ""}`);
}
const defaultHandler = spawnSync("/usr/bin/swift", ["-e", `
import Foundation
import CoreServices
let status = LSSetDefaultHandlerForURLScheme(
  "pudding-dev" as CFString,
  "com.teatak.pudding.dev.oauth" as CFString
)
if status != noErr {
  fatalError("LSSetDefaultHandlerForURLScheme failed: \\(status)")
}
`], { encoding: "utf8" });
if (defaultHandler.status !== 0) {
  const detail = String(defaultHandler.stderr || defaultHandler.stdout || "").trim();
  throw new Error(`failed to select pudding-dev protocol relay${detail ? `: ${detail}` : ""}`);
}
console.info(`>> registered pudding-dev OAuth relay: ${appBundle}`);

function infoPlist() {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleDisplayName</key><string>Pudding Dev OAuth</string>
  <key>CFBundleExecutable</key><string>applet</string>
  <key>CFBundleIdentifier</key><string>com.teatak.pudding.dev.oauth</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Pudding Dev OAuth</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSUIElement</key><true/>
  <key>NSAppleScriptEnabled</key><true/>
  <key>OSAAppletShowStartupScreen</key><false/>
  <key>CFBundleURLTypes</key>
  <array>
    <dict>
      <key>CFBundleTypeRole</key><string>Editor</string>
      <key>CFBundleURLName</key><string>Pudding Development OAuth</string>
      <key>CFBundleURLSchemes</key><array><string>pudding-dev</string></array>
    </dict>
  </array>
</dict>
</plist>
`;
}

function relayAppleScript() {
  const environment = {
    PUDDING_DAEMON_ADDR: process.env.PUDDING_DAEMON_ADDR || "127.0.0.1:9679",
    PUDDING_DAEMON_BIN: process.env.PUDDING_DAEMON_BIN || path.join(root, "bin", "puddingd"),
    PUDDING_DEV_URL: process.env.PUDDING_DEV_URL || "http://127.0.0.1:5174",
    PUDDING_OAUTH_RETURN_SCHEME: "pudding-dev",
  };
  const prefix = Object.entries(environment)
    .map(([name, value]) => `${name}=${shellQuote(value)}`)
    .join(" ");
  const beforeURL = `${prefix} ${shellQuote(electronExecutable)} ${shellQuote(mainScript)} `;
  const afterURL = ` >>${shellQuote(logPath)} 2>&1 &`;
  return `on open location oauthURL
  if oauthURL does not start with "pudding-dev://" then return
  do shell script ${appleScriptString(beforeURL)} & quoted form of oauthURL & ${appleScriptString(afterURL)}
end open location
`;
}

function appleScriptString(value) {
  return `"${String(value).replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", `'\\''`)}'`;
}
