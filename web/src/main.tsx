import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import React from "react";
import ReactDOM from "react-dom/client";
import { z } from "zod";

import { App } from "@/App";
import "@/styles.css";
import { startLocaleSync } from "@/i18n";
import { initAPIBase } from "@/state/apiBase";
import { initShellMode } from "@/state/shell";
import { applyTheme, initThemeFromLaunch, readStoredTheme, startThemeSync } from "@/theme/theme";

initAPIBase();
initShellMode();
initThemeFromLaunch();

applyTheme(readStoredTheme());
startThemeSync();
startLocaleSync();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      retry: 1,
    },
  },
});

const rootRoute = createRootRoute({
  component: () => <App />,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  validateSearch: z.object({
    session: z.string().optional(),
    draft: z.string().optional(),
    split: z.string().optional(), // 上下分屏的第二个会话(docs/design.md 2.2)
  }),
});

export { indexRoute };

const routeTree = rootRoute.addChildren([indexRoute]);

export const router = createRouter({
  routeTree,
  context: { queryClient },
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
