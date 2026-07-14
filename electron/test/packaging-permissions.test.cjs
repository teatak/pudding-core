const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  copyDirectoryWithPortableSymlinks,
  makeBundleOwnerWritable,
  removeUnusedPrivacyUsageDescriptions,
  setMinimumSystemVersion,
} = require("../../packaging/electron-builder-after-pack.cjs");

test("packaging rewrites copied runtime symlinks to stay inside the app", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-packaging-symlinks-"));
  try {
    const source = path.join(root, "runtime", "language-servers");
    const target = path.join(source, "typescript", "node_modules", "typescript", "bin", "tsc");
    const link = path.join(source, "typescript", "node_modules", ".bin", "tsc");
    const destination = path.join(root, "Pudding.app", "Contents", "Resources", "app", "language-servers");
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.mkdirSync(path.dirname(link), { recursive: true });
    fs.writeFileSync(target, "#!/bin/sh\n");
    fs.symlinkSync(target, link);

    copyDirectoryWithPortableSymlinks(source, destination);

    const copiedLink = path.join(destination, path.relative(source, link));
    const copiedTarget = path.join(destination, path.relative(source, target));
    assert.equal(path.isAbsolute(fs.readlinkSync(copiedLink)), false);
    assert.equal(fs.realpathSync(copiedLink), fs.realpathSync(copiedTarget));
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("packaging rejects runtime symlinks that point outside their source", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-packaging-external-symlink-"));
  try {
    const source = path.join(root, "runtime");
    const destination = path.join(root, "Pudding.app", "runtime");
    const external = path.join(root, "external");
    fs.mkdirSync(source);
    fs.writeFileSync(external, "external");
    fs.symlinkSync(external, path.join(source, "external-link"));

    assert.throws(
      () => copyDirectoryWithPortableSymlinks(source, destination),
      /runtime symlink points outside its source directory/,
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("packaging makes bundled regular files owner-writable without replacing symlinks", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-packaging-permissions-"));
  try {
    const nested = path.join(root, "nested");
    const license = path.join(nested, "LICENSE");
    const link = path.join(root, "license-link");
    fs.mkdirSync(nested);
    fs.writeFileSync(license, "license");
    fs.chmodSync(license, 0o444);
    fs.symlinkSync(license, link);
    fs.chmodSync(nested, 0o555);

    makeBundleOwnerWritable(root);

    assert.notEqual(fs.statSync(license).mode & 0o200, 0);
    assert.notEqual(fs.statSync(nested).mode & 0o200, 0);
    assert.equal(fs.lstatSync(link).isSymbolicLink(), true);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("packaging removes privacy declarations for unsupported capabilities", {
  skip: process.platform !== "darwin",
}, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-packaging-plist-"));
  const infoPlistPath = path.join(root, "Info.plist");
  try {
    fs.writeFileSync(
      infoPlistPath,
      `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>NSAudioCaptureUsageDescription</key><string>audio</string>
<key>NSBluetoothAlwaysUsageDescription</key><string>bluetooth</string>
<key>NSBluetoothPeripheralUsageDescription</key><string>bluetooth</string>
<key>NSCameraUsageDescription</key><string>camera</string>
</dict></plist>`,
    );

    removeUnusedPrivacyUsageDescriptions(infoPlistPath);

    const contents = fs.readFileSync(infoPlistPath, "utf8");
    assert.doesNotMatch(contents, /NSAudioCaptureUsageDescription/);
    assert.doesNotMatch(contents, /NSBluetooth/);
    assert.match(contents, /NSCameraUsageDescription/);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("packaging records architecture-specific macOS requirements", {
  skip: process.platform !== "darwin",
}, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-packaging-minimum-macos-"));
  const infoPlistPath = path.join(root, "Info.plist");
  try {
    fs.writeFileSync(
      infoPlistPath,
      `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>LSMinimumSystemVersion</key><string>14.0</string>
</dict></plist>`,
    );

    setMinimumSystemVersion(infoPlistPath, "x64");
    assert.equal(
      execFileSync("plutil", ["-extract", "LSMinimumSystemVersion", "raw", infoPlistPath], {
        encoding: "utf8",
      }).trim(),
      "15.5",
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
