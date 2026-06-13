import { useCallback, useState } from "react";

import { cn } from "@/lib/utils";

// 侧栏拖拽调宽:delta 法(起点宽度 + 指针位移),宽度过 clamp 后
// 持久化到 localStorage。invert 用于右侧栏(向左拖 = 加宽)。
export function useResizableWidth(opts: {
  key: string;
  fallback: number;
  min: number;
  max: number;
  invert?: boolean;
}) {
  const { key, fallback, min, max, invert } = opts;
  const [width, setWidth] = useState(() => {
    const saved = Number(localStorage.getItem(key));
    return Number.isFinite(saved) && saved >= min && saved <= max ? saved : fallback;
  });

  const startDrag = useCallback(
    (event: React.PointerEvent) => {
      event.preventDefault();
      const startX = event.clientX;
      let startWidth = 0;
      setWidth((current) => {
        startWidth = current;
        return current;
      });
      document.body.style.userSelect = "none";
      document.body.style.cursor = "col-resize";
      const onMove = (move: PointerEvent) => {
        const delta = move.clientX - startX;
        const next = Math.min(max, Math.max(min, startWidth + (invert ? -delta : delta)));
        setWidth(next);
        localStorage.setItem(key, String(Math.round(next)));
      };
      const onUp = () => {
        document.removeEventListener("pointermove", onMove);
        document.body.style.userSelect = "";
        document.body.style.cursor = "";
      };
      document.addEventListener("pointermove", onMove);
      document.addEventListener("pointerup", onUp, { once: true });
    },
    [invert, key, max, min],
  );

  return { width, startDrag };
}

// 拖柄:贴栏边缘的 4px 命中带,hover/拖动时显示主题色细线
export function ResizeHandle({
  className,
  onPointerDown,
}: {
  className?: string;
  onPointerDown: (event: React.PointerEvent) => void;
}) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        "absolute top-0 z-20 h-full w-1 cursor-col-resize transition-colors hover:bg-primary/40 active:bg-primary/60",
        className,
      )}
      onPointerDown={onPointerDown}
    />
  );
}
