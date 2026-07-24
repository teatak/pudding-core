import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { defineConfig, loadEnv, type Plugin } from "vite";

import packageMetadata from "../package.json";

const nodeRequire = createRequire(import.meta.url);
const dependencyRoot = fs.realpathSync(path.resolve(__dirname, "node_modules"));

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const daemon = env.PUDDING_DAEMON_URL || "http://127.0.0.1:9679";

  return {
    cacheDir: path.resolve(__dirname, ".vite-cache"),
    define: {
      __PUDDING_APP_VERSION__: JSON.stringify(packageMetadata.version),
      "process.env.DRAGGABLE_DEBUG": "false",
    },
    plugins: [materialIconTheme(), react(), tailwindcss()],
    resolve: {
      alias: {
        "@/contracts": path.resolve(__dirname, "contracts"),
        "@": path.resolve(__dirname, "src"),
      },
    },
    optimizeDeps: {
      include: ["recharts"],
    },
    server: {
      fs: {
        allow: [__dirname, dependencyRoot],
      },
      proxy: {
        "/sessions": daemon,
        "/projects": daemon,
        "/settings": daemon,
        "/providers": daemon,
        "/apps": daemon,
        "/app-assets": daemon,
        "/app-connections": daemon,
        "/browser-test-form": daemon,
      },
    },
  };
});

function materialIconTheme(): Plugin {
  const moduleID = "virtual:material-icon-theme";
  const resolvedModuleID = `\0${moduleID}`;
  let generatedModule: string | undefined;

  return {
    name: "pudding-material-icon-theme",
    resolveId(id) {
      return id === moduleID ? resolvedModuleID : undefined;
    },
    load(id) {
      if (id !== resolvedModuleID) return undefined;
      if (generatedModule) return generatedModule;

      const packagePath = nodeRequire.resolve("material-icon-theme/package.json");
      const packageRoot = path.dirname(packagePath);
      const manifestPath = path.join(packageRoot, "dist/material-icons.json");
      const source = JSON.parse(fs.readFileSync(manifestPath, "utf8")) as {
        iconDefinitions: Record<string, { iconPath: string }>;
        [key: string]: unknown;
      };
      const { iconDefinitions, ...manifest } = source;
      const icons = Object.fromEntries(Object.entries(iconDefinitions).map(([name, definition]) => {
        const iconPath = path.resolve(path.dirname(manifestPath), definition.iconPath);
        const svg = fs.readFileSync(iconPath, "utf8");
        return [name, { markup: svg }];
      }));

      generatedModule = `export default ${JSON.stringify({ icons, manifest })};`;
      return generatedModule;
    },
  };
}
