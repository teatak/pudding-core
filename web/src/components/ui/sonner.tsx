import { Toaster as Sonner } from "sonner";

import { useTheme } from "@/theme/theme";

// sonner Toaster 接应用主题(跟随 pudding.theme 而非系统),
// 配色走 token,与 popover 表面一致。
export function Toaster() {
  const { resolved } = useTheme();
  return (
    <Sonner
      position="bottom-right"
      theme={resolved}
      toastOptions={{
        style: {
          background: "var(--popover)",
          color: "var(--popover-foreground)",
          border: "1px solid var(--border)",
        },
      }}
    />
  );
}
