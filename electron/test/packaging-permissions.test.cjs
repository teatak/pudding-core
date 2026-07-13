const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { makeBundleOwnerWritable } = require("../../packaging/electron-builder-after-pack.cjs");

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
