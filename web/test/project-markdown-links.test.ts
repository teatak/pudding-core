import assert from "node:assert/strict";
import { test } from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";

const server = await createTestViteServer();
const { resolveProjectMarkdownLink } = await server.ssrLoadModule("/src/components/project/projectMarkdownLinks.ts");

const current = { rootID: "project", path: "docs/README.md" };
const roots = [{ id: "project", name: "Project", path: "/work/project" }];
const resolve = (href: string) => resolveProjectMarkdownLink(href, current, roots);
const file = (path: string, anchor?: string) => ({ kind: "file", selection: { rootID: "project", path }, anchor });

test("Markdown links resolve from their document directory and preserve fragments", () => {
  assert.deepEqual(resolve("README.zh-CN.md"), file("docs/README.zh-CN.md"));
  assert.deepEqual(resolve("../src/./main.ts#L12"), file("src/main.ts", "L12"));
  assert.deepEqual(resolve("/work/project/README.md?raw=1#features"), file("README.md", "features"));
  assert.deepEqual(resolve("#quick-install"), file(current.path, "quick-install"));
  assert.deepEqual(resolve("#"), file(current.path, ""));
  assert.deepEqual(resolve("%E4%B8%AD%E6%96%87%20Guide.md#%E5%AE%89%E8%A3%85"), file("docs/中文 Guide.md", "安装"));
  assert.deepEqual(resolve("literal%23name.md"), file("docs/literal#name.md"));
});

test("absolute paths never acquire a second project prefix", () => {
  assert.deepEqual(resolve("/work/project/docs/README.md"), file(current.path));
  assert.deepEqual(resolve("/README.md"), { kind: "invalid", reason: "outside_project" });
});

test("relative links can cross into another authorized root", () => {
  const multiRoots = [...roots, { id: "back", name: "Back", path: "/work/back" }];
  assert.deepEqual(resolveProjectMarkdownLink("../../back/schema.md", current, multiRoots), {
    kind: "file", selection: { rootID: "back", path: "schema.md" }, anchor: undefined,
  });
});

test("Web and mail links remain URLs rather than project paths", () => {
  assert.deepEqual(resolve("https://example.com/docs?a=1#intro"), { kind: "web", url: "https://example.com/docs?a=1#intro" });
  assert.deepEqual(resolve("http://localhost:4321/"), { kind: "web", url: "http://localhost:4321/" });
  assert.deepEqual(resolve("//example.com/help"), { kind: "web", url: "https://example.com/help" });
  assert.deepEqual(resolve("mailto:hello@example.com"), { kind: "external", url: "mailto:hello@example.com" });
});

test("file URLs explicitly target the session browser, not the project editor", () => {
  assert.deepEqual(resolve("file:///work/project/docs/README.md#features"), { kind: "browser-file", url: "file:///work/project/docs/README.md#features" });
  assert.deepEqual(resolve("file:///work/project/docs/../README.md"), { kind: "browser-file", url: "file:///work/project/README.md" });
  assert.deepEqual(resolve("file:///outside/preview.html"), { kind: "browser-file", url: "file:///outside/preview.html" });
  for (const href of ["file://server/work/project/file.md", "file:///work/project/%2F..%2Foutside.md"]) {
    assert.equal(resolve(href).kind, "invalid", href);
  }
});

test("Traversal, malformed encoding and unsupported protocols never open as web pages", () => {
  for (const href of ["../../outside.md", "%2e%2e/%2e%2e/outside.md", "bad%ZZ.md", "doc.md#%ZZ", "javascript:alert(1)", "data:text/html,test", "custom:launch", "java\nscript:alert(1)", "bad%00.md", "..\\outside.md", ""]) {
    assert.equal(resolve(href).kind, "invalid", href);
  }
});
