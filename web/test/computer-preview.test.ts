import assert from "node:assert/strict";
import test from "node:test";
import { updateComputerPreviews } from "../src/lib/automationPreview.ts";
import type { DesktopComputerPreviewFrame } from "../src/lib/desktopBridge.ts";

const frame = (appID: string, activityVersion: number, expiresAt = 31_000): DesktopComputerPreviewFrame => ({
  sessionID:"session",turnID:"turn",appID,windowID:activityVersion,pid:123,version:activityVersion,
  activityVersion,expiresAt,status:"live",imageURL:"data:image/png;base64,YQ==",
});

test("no loading, failed or expired frame is a renderable preview", () => {
  const first = frame("A", 1);
  for (const status of ["loading", "unavailable"] as const) {
    const update = {...first, status, imageURL:undefined};
    assert.deepEqual(updateComputerPreviews([], update, 1_000), []);
    assert.deepEqual(updateComputerPreviews([first], update, 1_000), []);
  }
  assert.deepEqual(updateComputerPreviews([], first, 31_000), []);
});

test("activity controls stacking; ordinary frames neither reorder nor extend other apps", () => {
  const a = frame("A", 1), b = frame("B", 2, 41_000);
  let previews = updateComputerPreviews([a], b, 1_000);
  assert.deepEqual(previews.map(f => f.appID), ["B", "A"]);
  previews = updateComputerPreviews(previews, {...a,version:9}, 2_000);
  assert.deepEqual(previews.map(f => f.appID), ["B", "A"]);
  assert.equal(previews[1].expiresAt, 31_000);
  previews = updateComputerPreviews(previews, {...a,activityVersion:10,version:10,expiresAt:50_000}, 20_000);
  assert.deepEqual(previews.map(f => f.appID), ["A", "B"]);
  assert.equal(previews[1].expiresAt, 41_000);
  previews = updateComputerPreviews(previews, {...b,status:"unavailable",imageURL:undefined}, 21_000);
  assert.deepEqual(previews.map(f => f.appID), ["A"]);
});

test("expired apps and previews from other sessions or turns cannot survive updates", () => {
  const a = frame("A", 1), b = frame("B", 2, 41_000);
  assert.deepEqual(updateComputerPreviews([a], b, 31_000).map(f => f.appID), ["B"]);
  for (const update of [{...b,sessionID:"other"}, {...b,turnID:"other"}]) {
    assert.deepEqual(updateComputerPreviews([a], update, 1_000), [update]);
  }
});
