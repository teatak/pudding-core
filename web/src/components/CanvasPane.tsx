import { Shapes, X } from "lucide-react";

import { ResizeHandle, useResizableWidth } from "@/components/ResizeHandle";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { setCanvasOpen } from "@/state/canvasStore";

// canvas 栏(docs/design.md 2.4):第三栏布局插槽。本版只交付
// 开合交互与空态占位,内容(canvas / 小组件 / 工具大块输出 / artifacts)
// 在能力解封后落位,不改此骨架。
export function CanvasPane() {
  const { t } = useI18n();
  const { width, startDrag } = useResizableWidth({
    key: "pudding.canvasWidth",
    fallback: 320,
    min: 240,
    max: 560,
    invert: true, // 右侧栏:向左拖 = 加宽
  });
  return (
    <aside
      className="relative flex h-full shrink-0 flex-col border-l bg-sidebar text-sidebar-foreground"
      style={{ width }}
    >
      <ResizeHandle className="-left-1" onPointerDown={startDrag} />
      <div className="flex h-(--toolbar-h) shrink-0 items-center justify-between pr-2 pl-4">
        <div className="text-sm font-medium">{t("canvas.title")}</div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button aria-label={t("canvas.close")} size="icon-sm" variant="ghost" onClick={() => setCanvasOpen(false)}>
              <X />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t("canvas.close")}</TooltipContent>
        </Tooltip>
      </div>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
        <Shapes className="h-8 w-8 text-muted-foreground/60" />
        <div className="text-sm text-muted-foreground">{t("canvas.empty")}</div>
      </div>
    </aside>
  );
}
