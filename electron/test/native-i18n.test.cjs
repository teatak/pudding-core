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
  assert.equal(nativeText("zh-CN", "showApp", { app: "Pudding" }), "显示 Pudding");
  assert.equal(nativeText("zh-TW", "quitApp", { app: "Pudding" }), "結束 Pudding");
  assert.equal(nativeText("en", "about", { app: "Pudding" }), "About Pudding");
});
