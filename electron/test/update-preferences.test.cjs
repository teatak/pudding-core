const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { readPreviewUpdatePreference, writePreviewUpdatePreference } = require("../update-preferences.cjs");

test("preview update preference is optional and persists explicit choices", (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-update-preferences-"));
  const filePath = path.join(directory, "config", "desktop-preferences.json");
  t.after(() => fs.rmSync(directory, { force: true, recursive: true }));

  assert.equal(readPreviewUpdatePreference(filePath), null);

  writePreviewUpdatePreference(filePath, true);
  assert.equal(readPreviewUpdatePreference(filePath), true);

  fs.writeFileSync(filePath, `${JSON.stringify({ futurePreference: "keep", receivePreviewUpdates: true })}\n`);
  writePreviewUpdatePreference(filePath, false);
  assert.equal(readPreviewUpdatePreference(filePath), false);
  assert.equal(JSON.parse(fs.readFileSync(filePath, "utf8")).futurePreference, "keep");
});

test("invalid preview update preferences fall back to the build channel", (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "pudding-update-preferences-"));
  const filePath = path.join(directory, "desktop-preferences.json");
  t.after(() => fs.rmSync(directory, { force: true, recursive: true }));

  fs.writeFileSync(filePath, "{invalid", "utf8");
  assert.equal(readPreviewUpdatePreference(filePath), null);
});
