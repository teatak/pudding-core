import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveProjectMarkdownLink } from "../src/components/project/projectMarkdownLinks.ts";

const current = { rootID: "project", path: "docs/README.md" };
const roots = [{ id: "project", name: "Project", path: "/work/project" }];
const resolve = (href: string) => resolveProjectMarkdownLink(href, current, roots);
const file = (path: string, anchor?: string) => ({ kind: "file", selection: { rootID: "project", path }, anchor });

test("Markdown links resolve from their document directory and preserve fragments", () => {
  assert.deepEqual(resolve("README.zh-CN.md"), file("docs/README.zh-CN.md"));
  assert.deepEqual(resolve("../src/./main.ts#L12"), file("src/main.ts", "L12"));
  assert.deepEqual(resolve("/README.md?raw=1#features"), file("README.md", "features"));
  assert.deepEqual(resolve("#quick-install"), file(current.path, "quick-install"));
  assert.deepEqual(resolve("#"), file(current.path, ""));
  assert.deepEqual(resolve("%E4%B8%AD%E6%96%87%20Guide.md#%E5%AE%89%E8%A3%85"), file("docs/中文 Guide.md", "安装"));
  assert.deepEqual(resolve("literal%23name.md"), file("docs/literal#name.md"));
});

test("Web and mail links remain URLs rather than project paths", () => {
  assert.deepEqual(resolve("https://example.com/docs?a=1#intro"), { kind: "web", url: "https://example.com/docs?a=1#intro" });
  assert.deepEqual(resolve("http://localhost:4321/"), { kind: "web", url: "http://localhost:4321/" });
  assert.deepEqual(resolve("//example.com/help"), { kind: "web", url: "https://example.com/help" });
  assert.deepEqual(resolve("mailto:hello@example.com"), { kind: "external", url: "mailto:hello@example.com" });
});

test("file URLs only target authorized project roots", () => {
  assert.deepEqual(resolve("file:///work/project/docs/README.md#features"), file(current.path, "features"));
  assert.deepEqual(resolve("file:///work/project/docs/../README.md"), file("README.md"));
  for (const href of ["file:///etc/passwd", "file:///work/project-other/file.md", "file://server/work/project/file.md", "file:///work/project/%2F..%2Foutside.md"]) {
    assert.deepEqual(resolve(href), { kind: "invalid" }, href);
  }
});

test("Traversal, malformed encoding and unsupported protocols never open as web pages", () => {
  for (const href of ["../../outside.md", "%2e%2e/%2e%2e/outside.md", "bad%ZZ.md", "doc.md#%ZZ", "javascript:alert(1)", "data:text/html,test", "custom:launch", "java\nscript:alert(1)", "bad%00.md", "..\\outside.md", ""]) {
    assert.deepEqual(resolve(href), { kind: "invalid" }, href);
  }
});
