import { Shapes } from "lucide-react";

import { useI18n } from "@/i18n";

// canvas 栏(docs/design.md 2.4):第三栏布局插槽。本版只交付
// 开合交互与空态占位,内容(canvas / 小组件 / 工具大块输出 / artifacts)
// 在能力解封后落位,不改此骨架。
export function CanvasPane() {
  const { t } = useI18n();
  return (
    <aside className="flex h-full shrink-0 flex-col bg-sidebar text-sidebar-foreground">
      <div className="flex h-(--toolbar-h) shrink-0 items-center pr-12 pl-4">
        <div className="text-sm font-medium">{t("canvas.title")}</div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
        <Shapes className="h-8 w-8 text-muted-foreground/60" />
        <div className="text-sm text-muted-foreground">{t("canvas.empty")}</div>
      </div>
    </aside>
  );
}
