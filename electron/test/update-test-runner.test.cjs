const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  assertBundleWritable,
  findRunningPuddingProcesses,
  findUnwritableEntries,
  installedAppPath,
  readInstalledVersion,
} = require("../../scripts/run-update-test.cjs");

test("update verification detects any running Pudding app bundle", () => {
  const processes = `
  101 /Applications/Pudding.app/Contents/MacOS/Pudding
  102 /tmp/release/Pudding.app/Contents/Frameworks/Pudding Helper.app/Contents/MacOS/Pudding Helper
  103 node scripts/run-update-test.cjs
  104 /tmp/release/Pudding.app/Contents/MacOS/Pudding --test
  `;

  assert.deepEqual(findRunningPuddingProcesses(processes), [
    "101 /Applications/Pudding.app/Contents/MacOS/Pudding",
    "104 /tmp/release/Pudding.app/Contents/MacOS/Pudding --test",
  ]);
});

test("update verification resolves the app root and rejects entries the current user cannot write", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-update-runner-"));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));

  const app = path.join(root, "Pudding.app");
  const executable = path.join(app, "Contents", "MacOS", "Pudding");
  const resource = path.join(app, "Contents", "Resources", "license.txt");
  fs.mkdirSync(path.dirname(executable), { recursive: true });
  fs.mkdirSync(path.dirname(resource), { recursive: true });
  fs.writeFileSync(executable, "binary");
  fs.writeFileSync(
    path.join(app, "Contents", "Info.plist"),
    `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleShortVersionString</key><string>0.1.2</string></dict></plist>`,
  );
  fs.writeFileSync(resource, "license");
  fs.chmodSync(resource, 0o444);

  assert.equal(installedAppPath(executable), app);
  assert.equal(readInstalledVersion(executable), "0.1.2");
  assert.deepEqual(findUnwritableEntries(app), [resource]);
  assert.throws(
    () => assertBundleWritable(app, "test bundle"),
    /test bundle is not writable by the current user/,
  );

  fs.chmodSync(resource, 0o644);
  assert.deepEqual(findUnwritableEntries(app), []);
  assert.doesNotThrow(() => assertBundleWritable(app, "test bundle"));
});

test("stable and preview update targets use matching artifacts and channels", () => {
  const makefile = fs.readFileSync(path.resolve(__dirname, "..", "..", "Makefile"), "utf8");

  assert.match(
    makefile,
    /desktop-update-test: desktop-verify\n\t@PUDDING_RELEASE_CHANNEL=stable node scripts\/run-update-test\.cjs/,
  );
  assert.match(
    makefile,
    /desktop-preview-update-test: desktop-preview-verify\n\t@PUDDING_RELEASE_CHANNEL=preview node scripts\/run-update-test\.cjs/,
  );
  assert.match(
    makefile,
    /desktop-computer-use-update-test: desktop-verify\n\t@PUDDING_RELEASE_CHANNEL=stable PUDDING_UPDATE_TEST_REQUIRE_COMPUTER_USE_IDENTITY=1 node scripts\/run-update-test\.cjs/,
  );
  assert.match(
    makefile,
    /desktop-preview-computer-use-update-test: desktop-preview-verify\n\t@PUDDING_RELEASE_CHANNEL=preview PUDDING_UPDATE_TEST_REQUIRE_COMPUTER_USE_IDENTITY=1 node scripts\/run-update-test\.cjs/,
  );
});
