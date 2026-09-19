import assert from "node:assert/strict";
import { test } from "node:test";
import { pathToFileURL } from "node:url";
import { browserAddressToURL } from "../src/browser/helpers.ts";

test("address bar treats native absolute paths as local files, not hosts or searches", () => {
  for (const path of [
    "/Users/yanggang/Desktop/workspace/pelican-bicycle.html",
    "/tmp/中文 页面 (demo).html",
    "/tmp/literal #?%20.html",
    "/tmp/no-extension",
  ]) {
    assert.equal(browserAddressToURL(path), pathToFileURL(path).href);
  }
  assert.equal(browserAddressToURL("  /tmp/page.html  "), "file:///tmp/page.html");
  assert.equal(browserAddressToURL("C:\\work\\中文 页面.html"), "file:///C:/work/%E4%B8%AD%E6%96%87%20%E9%A1%B5%E9%9D%A2.html");
  assert.equal(browserAddressToURL("C:/work/page.html"), "file:///C:/work/page.html");
});

test("explicit file URLs retain URL query and fragment semantics", () => {
  for (const url of ["file:///tmp/page.html?q=1#intro", "file://localhost/tmp/a%23b.html", "FILE:///tmp/page.html"]) {
    assert.equal(browserAddressToURL(url), url);
  }
});

test("ordinary web navigation and explicit searches remain unchanged", () => {
  for (const [input, expected] of [
    ["", ""],
    ["https://example.com/a?q=1#b", "https://example.com/a?q=1#b"],
    ["example.com/demo.html", "https://example.com/demo.html"],
    ["localhost:5173/demo.html", "http://localhost:5173/demo.html"],
    ["127.0.0.1:8080", "http://127.0.0.1:8080"],
    ["github", "https://github.com"],
    ["hello world", "https://www.google.com/search?q=hello%20world"],
    ["? /tmp/page.html", "https://www.google.com/search?q=%2Ftmp%2Fpage.html"],
  ]) assert.equal(browserAddressToURL(input), expected);
});
