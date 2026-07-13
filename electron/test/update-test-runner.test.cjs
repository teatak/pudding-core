const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  findReadOnlyEntries,
  installedAppPath,
} = require("../../scripts/run-update-test.cjs");

test("update verification resolves the app root and rejects read-only bundle entries", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-update-runner-"));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));

  const app = path.join(root, "Pudding.app");
  const executable = path.join(app, "Contents", "MacOS", "Pudding");
  const resource = path.join(app, "Contents", "Resources", "license.txt");
  fs.mkdirSync(path.dirname(executable), { recursive: true });
  fs.mkdirSync(path.dirname(resource), { recursive: true });
  fs.writeFileSync(executable, "binary");
  fs.writeFileSync(resource, "license");
  fs.chmodSync(resource, 0o444);

  assert.equal(installedAppPath(executable), app);
  assert.deepEqual(findReadOnlyEntries(app), [resource]);

  fs.chmodSync(resource, 0o644);
  assert.deepEqual(findReadOnlyEntries(app), []);
});
