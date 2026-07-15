import assert from "node:assert/strict";
import test from "node:test";
import { normalizeUpdatePreferences } from "./update-preferences.js";

test("uses safe defaults", () => {
  assert.deepEqual(normalizeUpdatePreferences(), { channel: "stable", enabled: true });
  assert.deepEqual(normalizeUpdatePreferences({}), { channel: "stable", enabled: true });
});

test("accepts only supported channels", () => {
  assert.deepEqual(normalizeUpdatePreferences({ channel: "preview", enabled: false }), { channel: "preview", enabled: false });
  assert.deepEqual(normalizeUpdatePreferences({ channel: "nightly", enabled: 1 }), { channel: "stable", enabled: true });
});
