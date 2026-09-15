import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer, type InlineConfig, type ViteDevServer } from "vite";

export async function createTestViteServer(options: Pick<InlineConfig, "configFile"> = {}) {
  // Separate prebundles from the running app and concurrent test processes.
  // Sharing cacheDir lets a test replace the dev server's optimized dependencies.
  const cacheDir = await mkdtemp(join(tmpdir(), "pudding-vite-test-"));
  let server: ViteDevServer | undefined;
  after(async () => {
    try {
      await server?.close();
    } finally {
      await rm(cacheDir, { recursive: true, force: true });
    }
  });
  server = await createServer({
    ...options,
    root: fileURLToPath(new URL("..", import.meta.url)),
    cacheDir,
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  });
  return server;
}
