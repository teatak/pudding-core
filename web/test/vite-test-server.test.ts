import assert from "node:assert/strict";
import { realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { test } from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";

test("Vite tests never share optimized dependencies with development or each other", async () => {
  const first = await createTestViteServer();
  const second = await createTestViteServer({ configFile: false });
  assert.notEqual(first.config.cacheDir, second.config.cacheDir);
  for (const server of [first, second]) {
    assert.equal(await realpath(dirname(server.config.cacheDir)), await realpath(tmpdir()));
    assert.notEqual(server.config.cacheDir, resolve(server.config.root, ".vite-cache"));
  }
});
