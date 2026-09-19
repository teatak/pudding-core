import assert from "node:assert/strict";
import { test } from "node:test";
import { markdownLinkHref, resolveMarkdownLink } from "../src/lib/markdownLinks.ts";

const source = "/work/front/docs/README.md";
const file = (absolutePath: string, anchor?: string) => ({ kind: "file", absolutePath, anchor });

test("source-relative and native absolute paths identify a single file", () => {
  for (const [href, path] of [
    ["guide.md", "/work/front/docs/guide.md"],
    ["./guide.md", "/work/front/docs/guide.md"],
    ["../../back/schema.md", "/work/back/schema.md"],
    ["/etc/hosts", "/etc/hosts"],
    ["/work/front/../back/schema.md", "/work/back/schema.md"],
    ["../", "/work/front"],
  ]) assert.deepEqual(resolveMarkdownLink(href, source), file(path));
});

test("file URLs open the browser while plain paths open documents", () => {
  assert.deepEqual(resolveMarkdownLink("/work/front/docs/README.md?raw=1#使用", source), file(source, "使用"));
  for (const href of ["file:///work/front/docs/README.md?raw=1#使用", "file://localhost/work/front/docs/README.md?raw=1#使用"]) {
    assert.deepEqual(resolveMarkdownLink(href, source), { kind: "browser-file", url: "file:///work/front/docs/README.md?raw=1#%E4%BD%BF%E7%94%A8" });
  }
  assert.deepEqual(resolveMarkdownLink("#安装", source), file(source, "安装"));
  assert.deepEqual(resolveMarkdownLink("#", source), file(source, ""));
  assert.deepEqual(resolveMarkdownLink("code.ts#L12-L20", source), file("/work/front/docs/code.ts", "L12-L20"));
});

test("decode once and do not reinterpret encoded filename punctuation", () => {
  for (const prefix of ["", "/work/front/docs/"]) {
    assert.deepEqual(resolveMarkdownLink(prefix + "%E4%B8%AD%E6%96%87%20(guide)%23%3F%25.md#%E5%AE%89%E8%A3%85", source), file("/work/front/docs/中文 (guide)#?%.md", "安装"));
    assert.deepEqual(resolveMarkdownLink(prefix + "%252e%252e.md", source), file("/work/front/docs/%2e%2e.md"));
  }
  assert.deepEqual(resolveMarkdownLink("next.md", "/work/%E4%23/README.md"), file("/work/%E4%23/next.md"), "native source filenames must not be decoded");
});

test("chat absolute links work without a source, relative links never use the application origin", () => {
  assert.deepEqual(resolveMarkdownLink(source), file(source));
  assert.deepEqual(resolveMarkdownLink(`file://${source}`), { kind: "browser-file", url: `file://${source}` });
  for (const href of ["docs/guide.md", "#section", "../README.md"]) {
    assert.deepEqual(resolveMarkdownLink(href), { kind: "invalid", reason: "missing_source" });
  }
  assert.deepEqual(resolveMarkdownLink("http://127.0.0.1:4321/page?q=1#part"), { kind: "web", url: "http://127.0.0.1:4321/page?q=1#part" });
  assert.deepEqual(resolveMarkdownLink("//example.com/docs"), { kind: "web", url: "https://example.com/docs" });
});

test("untrusted hrefs cannot become executable links or empty application links", () => {
  for (const href of ["", "javascript:alert(1)", "data:text/html,test", "file:relative.md", "file:/work/file.md", "file://remote/work/file.md", "file:////remote/file.md", "bad%ZZ.md", "bad%00.md", "../bad\\name", "doc.md#%00", "java\nscript:alert(1)"]) {
    assert.equal(resolveMarkdownLink(href, source).kind, "invalid", href);
    assert.equal(markdownLinkHref(href), undefined, href);
  }
  assert.equal(markdownLinkHref("guide.md"), undefined);
  assert.equal(markdownLinkHref("file:///work/file.md"), "file:///work/file.md");
  assert.equal(markdownLinkHref("https://example.com/"), "https://example.com/");
});

test("Windows drive paths and file URLs identify the same file", () => {
  assert.deepEqual(resolveMarkdownLink("file:///C:/work/docs/guide.md"), { kind: "browser-file", url: "file:///C:/work/docs/guide.md" });
  assert.deepEqual(resolveMarkdownLink("C:\\work\\docs\\guide.md"), file("C:/work/docs/guide.md"));
  assert.deepEqual(resolveMarkdownLink("../guide.md", "C:/work/docs/README.md"), file("C:/work/guide.md"));
});
