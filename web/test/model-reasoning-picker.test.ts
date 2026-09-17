import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { createServer, type ViteDevServer } from "vite";

const webRoot = fileURLToPath(new URL("..", import.meta.url));
const fixtureEntry = fileURLToPath(new URL("./fixtures/model-reasoning-picker.cjs", import.meta.url));
const fixtureModule = fileURLToPath(new URL("./fixtures/model-reasoning-picker.tsx", import.meta.url));
const require = createRequire(import.meta.url);

test("model picker and reasoning slider: isolated Electron interaction regressions", { timeout: 120_000 }, async (t) => {
  const temp = await mkdtemp(join(tmpdir(), "pudding-picker-ui-test-"));
  let server: ViteDevServer | undefined;
  t.after(async () => {
    try {
      await server?.close();
    } finally {
      await rm(temp, { recursive: true, force: true });
    }
  });

  server = await createServer({
    // Avoid inheriting the application's daemon proxies or development cache.
    configFile: false,
    root: webRoot,
    cacheDir: join(temp, "vite-cache"),
    logLevel: "error",
    define: {
      __PUDDING_APP_VERSION__: JSON.stringify("selector-regression"),
      "process.env.DRAGGABLE_DEBUG": "false",
    },
    resolve: { alias: { "@/contracts": join(webRoot, "contracts"), "@": join(webRoot, "src") } },
    optimizeDeps: { entries: [fixtureModule] },
    plugins: [
      react(),
      tailwindcss(),
      {
        name: "isolated-model-picker-fixture",
        configureServer(vite) {
          vite.middlewares.use((request, response, next) => {
            if (request.url?.split("?")[0] !== "/__picker-fixture") return next();
            const html = '<!doctype html><html><head><title>Isolated model picker regression</title><link rel="icon" href="data:,"></head><body><div id="root"></div><script type="module" src="/test/fixtures/model-reasoning-picker.tsx"></script></body></html>';
            void vite.transformIndexHtml("/__picker-fixture", html).then((transformed) => {
              response.setHeader("Content-Type", "text/html");
              response.end(transformed);
            }, next);
          });
        },
      },
    ],
    server: {
      host: "127.0.0.1",
      watch: null,
      fs: { allow: [webRoot, temp] },
    },
  });
  await server.listen();
  const address = server.httpServer?.address();
  assert.ok(address && typeof address !== "string");
  const fixtureURL = `http://127.0.0.1:${address.port}/__picker-fixture`;
  // Resolve this checkout's Electron binary, never an installed Pudding app.
  const electron = require("electron") as string;
  const env = { ...process.env };
  delete env.ELECTRON_RUN_AS_NODE;
  try {
    const { stdout, stderr } = await promisify(execFile)(
      electron,
      [fixtureEntry, fixtureURL, join(temp, "user-data"), electron],
      { cwd: webRoot, env, timeout: 100_000, maxBuffer: 1024 * 1024, signal: t.signal },
    );
    t.diagnostic(stdout.trim());
    assert.match(stdout, /PASS 5 model picker scenarios/);
    if (stderr.trim()) t.diagnostic(stderr.trim());
  } catch (error) {
    const failure = error as Error & { stdout?: string; stderr?: string };
    throw new Error(`Electron picker regression failed:\n${failure.stdout || ""}\n${failure.stderr || ""}`, { cause: error });
  }
});
