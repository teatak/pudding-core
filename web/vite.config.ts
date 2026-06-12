import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const daemon = env.PUDDING_DAEMON_URL || "http://127.0.0.1:9679";

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@/contracts": path.resolve(__dirname, "contracts"),
        "@": path.resolve(__dirname, "src"),
      },
    },
    server: {
      proxy: {
        "/sessions": daemon,
        "/settings": daemon,
        "/providers": daemon,
      },
    },
  };
});
