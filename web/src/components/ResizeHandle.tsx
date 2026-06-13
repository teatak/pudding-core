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

// 拖柄:贴栏边缘的 8px 透明命中带,中间渲染小胶囊抓手
// (shadcn resizable 的 withHandle 形态),hover/拖动只加深抓手,无大色块
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
        "group/handle absolute top-0 z-20 flex h-full w-2 cursor-col-resize items-center justify-center",
        className,
      )}
      onPointerDown={onPointerDown}
    >
      <div className="h-8 w-1 rounded-full bg-border transition-colors group-hover/handle:bg-muted-foreground/40 group-active/handle:bg-muted-foreground/60" />
    </div>
  );
}

// 上下分屏的比例拖拽:按指针位移占容器(handle 父元素)高度的比例调整,
// 持久化到 localStorage。
export function useResizableRatio(opts: { key: string; fallback: number; min: number; max: number }) {
  const { key, fallback, min, max } = opts;
  const [ratio, setRatio] = useState(() => {
    const saved = Number(localStorage.getItem(key));
    return Number.isFinite(saved) && saved >= min && saved <= max ? saved : fallback;
  });

  const startDrag = useCallback(
    (event: React.PointerEvent) => {
      event.preventDefault();
      const container = (event.currentTarget as HTMLElement).parentElement;
      const height = container?.getBoundingClientRect().height || 1;
      const startY = event.clientY;
      let startRatio = 0;
      setRatio((current) => {
        startRatio = current;
        return current;
      });
      document.body.style.userSelect = "none";
      document.body.style.cursor = "row-resize";
      const onMove = (move: PointerEvent) => {
        const next = Math.min(max, Math.max(min, startRatio + (move.clientY - startY) / height));
        setRatio(next);
        localStorage.setItem(key, String(next.toFixed(3)));
      };
      const onUp = () => {
        document.removeEventListener("pointermove", onMove);
        document.body.style.userSelect = "";
        document.body.style.cursor = "";
      };
      document.addEventListener("pointermove", onMove);
      document.addEventListener("pointerup", onUp, { once: true });
    },
    [key, max, min],
  );

  return { ratio, startDrag };
}

// 水平分隔条(上下分屏):中线 1px + 中央小胶囊抓手,与竖向拖柄同款语言
export function SplitHandle({ onPointerDown }: { onPointerDown: (event: React.PointerEvent) => void }) {
  return (
    <div
      aria-hidden="true"
      className="group/handle relative flex h-2 w-full shrink-0 cursor-row-resize items-center justify-center"
      onPointerDown={onPointerDown}
    >
      <div className="absolute inset-x-0 top-1/2 h-px bg-border" />
      <div className="z-10 h-1 w-8 rounded-full bg-border transition-colors group-hover/handle:bg-muted-foreground/40 group-active/handle:bg-muted-foreground/60" />
    </div>
  );
}
