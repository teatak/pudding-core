const assert = require("node:assert/strict");
const test = require("node:test");

const { nativeText, normalizeNativeLocale } = require("../native-i18n.cjs");

test("normalizes supported native locales", () => {
  assert.equal(normalizeNativeLocale("zh-CN"), "zh-CN");
  assert.equal(normalizeNativeLocale("zh_Hant_HK"), "zh-TW");
  assert.equal(normalizeNativeLocale("en-US"), "en");
  assert.equal(normalizeNativeLocale("fr-FR"), "en");
});

test("translates and interpolates native menu labels", () => {
  assert.equal(nativeText("zh-CN", "openApp", { app: "Pudding" }), "打开 Pudding");
  assert.equal(nativeText("en", "openApp", { app: "Pudding" }), "Open Pudding");
  assert.equal(nativeText("zh-TW", "quitApp", { app: "Pudding" }), "結束 Pudding");
  assert.equal(nativeText("en", "about", { app: "Pudding" }), "About Pudding");
  assert.equal(nativeText("zh-CN", "restartToUpdate"), "重新启动以更新");
  assert.equal(nativeText("zh-CN", "downloadUpdate"), "下载更新…");
  assert.equal(nativeText("en", "downloadLatest"), "Download Latest Version…");
});
