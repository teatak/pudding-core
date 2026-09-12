import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const server = await createServer({
  root: fileURLToPath(new URL("..", import.meta.url)),
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
});
after(() => server.close());
const { turnFilePathParts } = await server.ssrLoadModule("/src/lib/turnFileChanges.ts");

test("路径拆成目录与文件名,窄卡片只留文件名", () => {
  assert.deepEqual(turnFilePathParts("docs/workspace-resize-performance-plan.md"), {
    base: "workspace-resize-performance-plan.md",
    dir: "docs/",
  });
  assert.deepEqual(turnFilePathParts("pudding-core/docs/a.md"), { base: "a.md", dir: "pudding-core/docs/" });
});

test("没有目录的标签原样保留", () => {
  assert.deepEqual(turnFilePathParts("README.md"), { base: "README.md", dir: "" });
  assert.deepEqual(turnFilePathParts(""), { base: "", dir: "" });
});

test("拆分结果能还原原始标签", () => {
  for (const label of ["docs/a.md", "a/b/c.txt", "README.md", "root/docs/a.md"]) {
    const { base, dir } = turnFilePathParts(label);
    assert.equal(`${dir}${base}`, label);
  }
});
