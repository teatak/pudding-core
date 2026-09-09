import assert from "node:assert/strict";
import test from "node:test";

import { queryKeys } from "../src/api/queryKeys.ts";
import { getDesktopApplicationIdentity } from "../src/lib/desktopBridge.ts";

test("application identity cache separates languages without changing the app ID", () => {
  const key = (locale: string) => queryKeys.desktopApplicationIdentity("com.apple.calculator", locale);
  assert.notDeepEqual(key("zh-CN"), key("en"));
  assert.notDeepEqual(key("zh-CN"), key("zh-TW"));
  assert.deepEqual(key("zh-CN"), ["desktop", "application-identity", "com.apple.calculator", "zh-CN"]);
});

test("desktop identity forwards the requested UI language", async () => {
  const calls: unknown[][] = [];
  const identity = { appID: "com.apple.calculator", name: "计算器", iconURL: "icon" };
  const prior = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", { configurable: true, value: {
    puddingElectronDesktop: { getApplicationIdentity: async (...args: unknown[]) => { calls.push(args); return identity; } },
  } });
  try {
    assert.deepEqual(await getDesktopApplicationIdentity(" com.apple.calculator ", "zh-CN"), identity);
    assert.deepEqual(calls, [["com.apple.calculator", "zh-CN"]]);
  } finally {
    if (prior) Object.defineProperty(globalThis, "window", prior);
    else Reflect.deleteProperty(globalThis, "window");
  }
});
