import assert from "node:assert/strict";
import test from "node:test";
import { validateReleaseAssets } from "./release-assets.js";

const complete = ["Pudding-arm64.dmg", "Pudding-arm64.zip", "Pudding-x64.dmg", "Pudding-x64.zip", "latest-mac.yml"];

test("accepts a complete set with unrelated files", () => {
  assert.deepEqual(validateReleaseAssets([...complete, "notes.txt"]), { ok: true, missing: [], duplicates: [] });
});

test("reports missing required assets", () => {
  assert.deepEqual(validateReleaseAssets(["Pudding-arm64.dmg", "one", "two", "three", "four"]), {
    ok: false,
    missing: ["Pudding-arm64.zip", "Pudding-x64.dmg", "Pudding-x64.zip", "latest-mac.yml"],
    duplicates: [],
  });
});

test("rejects duplicate required assets", () => {
  assert.deepEqual(validateReleaseAssets([...complete, "latest-mac.yml"]), {
    ok: false,
    missing: [],
    duplicates: ["latest-mac.yml"],
  });
});
