import React, { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { useI18n } from "@/i18n";

type SteppedSliderProps = {
  options: string[];
  value: string;
  onChange: (value: string) => void;
  onPreviewChange?: (value: string) => void;
  className?: string;
};

export type EffortColorConfig = {
  bg: string;
  text: string;
  glow?: string;
};

export const COLOR_PALETTE: EffortColorConfig[] = [
  {
    // 灰 (最低档)
    bg: "bg-gradient-to-b from-zinc-400 to-zinc-500 dark:from-zinc-500 dark:to-zinc-600",
    text: "text-zinc-600 dark:text-zinc-300 font-semibold",
    glow: "shadow-[0_1px_3px_rgba(113,113,122,0.15)]",
  },
  {
    // 绿
    bg: "bg-gradient-to-b from-emerald-400 to-emerald-500 dark:from-emerald-500 dark:to-emerald-600",
    text: "text-emerald-600 dark:text-emerald-400 font-semibold",
    glow: "shadow-[0_1px_4px_rgba(16,185,129,0.2)]",
  },
  {
    // 蓝
    bg: "bg-gradient-to-b from-sky-400 to-blue-500 dark:from-blue-500 dark:to-blue-600",
    text: "text-blue-600 dark:text-blue-400 font-semibold",
    glow: "shadow-[0_1px_4px_rgba(59,130,246,0.2)]",
  },
  {
    // 紫
    bg: "bg-gradient-to-b from-violet-400 to-violet-500 dark:from-violet-500 dark:to-violet-600",
    text: "text-violet-600 dark:text-violet-400 font-semibold",
    glow: "shadow-[0_1px_4px_rgba(139,92,246,0.2)]",
  },
  {
    // 金/橙 (最高档)
    bg: "bg-gradient-to-b from-amber-400 to-amber-500 dark:from-amber-500 dark:to-amber-600",
    text: "text-amber-600 dark:text-amber-400 font-semibold",
    glow: "shadow-[0_1px_5px_rgba(245,158,11,0.25)]",
  },
];

export function getEffortColor(index: number, totalOptions: number): EffortColorConfig {
  if (totalOptions <= 1) {
    return COLOR_PALETTE[COLOR_PALETTE.length - 1]!;
  }
  const paletteIndex = Math.round((index / (totalOptions - 1)) * (COLOR_PALETTE.length - 1));
  const clamped = Math.max(0, Math.min(paletteIndex, COLOR_PALETTE.length - 1));
  return COLOR_PALETTE[clamped]!;
}

export function SteppedSlider({
  options,
  value,
  onChange,
  onPreviewChange,
  className,
}: SteppedSliderProps) {
  const { t } = useI18n();
  const trackRef = useRef<HTMLDivElement>(null);
  const isDraggingRef = useRef(false);

  const [isDragging, setIsDragging] = useState(false);
  const [activeOption, setActiveOption] = useState(value);
  const activeOptionRef = useRef(value);

  // 外部 value 变更时（非拖拽中）同步内部即时状态
  useEffect(() => {
    if (!isDraggingRef.current) {
      setActiveOption(value);
      activeOptionRef.current = value;
    }
  }, [value]);


  const totalSteps = Math.max(options.length - 1, 1);
  const currentIndex = options.indexOf(activeOption);
  const activeIndex = currentIndex >= 0 ? currentIndex : Math.max(options.indexOf(value), 0);

  const calculateNearestOption = useCallback(
    (clientX: number): string => {
      const track = trackRef.current;
      if (!track) return activeOptionRef.current;
      const rect = track.getBoundingClientRect();
      const innerLeft = rect.left + 4 + 12;
      const innerWidth = Math.max(rect.width - 8 - 24, 1);
      const relativeX = Math.max(0, Math.min(clientX - innerLeft, innerWidth));
      const ratio = relativeX / innerWidth;
      const nearestIndex = Math.round(ratio * totalSteps);
      const clampedIndex = Math.max(0, Math.min(nearestIndex, options.length - 1));
      return options[clampedIndex] || activeOptionRef.current;
    },
    [options, totalSteps],
  );

  const handlePointerDown = (e: React.PointerEvent) => {
    e.preventDefault();
    isDraggingRef.current = true;
    setIsDragging(true);
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    const nextOption = calculateNearestOption(e.clientX);
    setActiveOption(nextOption);
    activeOptionRef.current = nextOption;
    onPreviewChange?.(nextOption);
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    if (!isDraggingRef.current) return;
    const nextOption = calculateNearestOption(e.clientX);
    if (nextOption !== activeOptionRef.current) {
      setActiveOption(nextOption);
      activeOptionRef.current = nextOption;
      onPreviewChange?.(nextOption);
    }
  };

  const handlePointerUp = (e: React.PointerEvent) => {
    if (!isDraggingRef.current) return;
    isDraggingRef.current = false;
    setIsDragging(false);
    (e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId);
    const finalOption = activeOptionRef.current;
    if (finalOption) {
      onChange(finalOption);
    }
  };

  const handleSelect = (opt: string) => {
    setActiveOption(opt);
    activeOptionRef.current = opt;
    onPreviewChange?.(opt);
    onChange(opt);
  };

  const [enableTransition, setEnableTransition] = useState(false);

  useEffect(() => {
    // 挂载初次渲染完成后再开启过渡动画，避免从目录切回滑块时因容器尺寸更新产生从宽变窄的动画
    const frame = requestAnimationFrame(() => {
      setEnableTransition(true);
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  const stepRatio = activeIndex / totalSteps;
  const animClass = enableTransition
    ? "transition-[left,width] duration-200 ease-out"
    : "";
  const currentEffortColor = getEffortColor(activeIndex, options.length);

  return (
    <div className={cn("w-full select-none pt-1 pb-0.5", className)}>
      {/* 滑块轨道区域：拖拽中为 cursor-grabbing，否则为 cursor-default */}
      <div
        ref={trackRef}
        className={cn(
          "relative flex h-8 w-full items-center rounded-full bg-muted/60 p-1 touch-none shadow-[inset_0_1px_2.5px_rgba(0,0,0,0.08),inset_0_0_0_1px_rgba(0,0,0,0.04)] dark:bg-muted/40 dark:shadow-[inset_0_1px_2.5px_rgba(0,0,0,0.35),inset_0_0_0_1px_rgba(255,255,255,0.05)]",
          isDragging ? "cursor-grabbing" : "cursor-default",
        )}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerUp}
      >
        {/* 激活区域的高亮填充条：内层独立胶囊容器，根据档位显示对应颜色并在最大档顶满 */}
        <div className="absolute inset-1 overflow-hidden rounded-full pointer-events-none">
          <div
            className={cn(
              "relative h-full rounded-full pointer-events-none ring-1 ring-inset ring-white/20",
              currentEffortColor.bg,
              animClass,
            )}
            style={{
              width: `calc(${stepRatio} * (100% - 24px) + 24px)`,
            }}
          >
            {/* 细腻质感微光层：仅顶部 1px 极淡微光 + 柔和通透感，保持平整高级，不做圆柱反光与底阴影 */}
            <div className="absolute inset-0 rounded-full bg-gradient-to-b from-white/14 to-transparent pointer-events-none shadow-[inset_0_1px_0_rgba(255,255,255,0.28)]" />
          </div>
        </div>

        {/* 内部相对定位容器 */}
        <div className="relative h-6 w-full pointer-events-none">

          {/* 各刻度圆点：hover 放大动画 + 平滑过渡，保持 cursor-default */}
          {options.map((opt, idx) => {
            const r = idx / totalSteps;
            const isFilled = idx <= activeIndex;
            return (
              <button
                key={opt}
                type="button"
                aria-label={t(`provider.reasoningEffort.${opt}`)}
                className="group/dot pointer-events-auto absolute top-0 bottom-0 flex w-5 -translate-x-1/2 cursor-default items-center justify-center outline-none"
                style={{
                  left: `calc(${r} * (100% - 24px) + 12px)`,
                }}
                onClick={() => handleSelect(opt)}
              >
                <span
                  className={cn(
                    "size-1.5 rounded-full transition-transform duration-200 ease-out group-hover/dot:scale-[1.8]",
                    isFilled ? "bg-white/90 shadow-[0_0.5px_1px_rgba(0,0,0,0.25)]" : "bg-muted-foreground/35",
                  )}
                />
              </button>
            );
          })}

          {/* 实体滑动圆钮 (Thumb)：悬停 grab，拖动 grabbing */}
          <div
            className={cn(
              "pointer-events-auto absolute top-0 bottom-0",
              isDragging ? "cursor-grabbing" : "cursor-grab",
              animClass,
            )}
            style={{
              left: `calc(${stepRatio} * (100% - 24px))`,
            }}
          >
            <div
              className={cn(
                "size-6 rounded-full bg-gradient-to-b from-white to-zinc-50 dark:from-zinc-100 dark:to-zinc-200 shadow-[0_2px_5px_rgba(0,0,0,0.18),0_1px_2px_rgba(0,0,0,0.12)] ring-1 ring-black/10 transition-transform",
                isDragging ? "cursor-grabbing scale-95" : "cursor-grab hover:scale-105 active:scale-95",
              )}
            />
          </div>
        </div>
      </div>

      {/* 下方的刻度标签文字：与上方各刻度圆点像素级严格中心对齐 */}
      <div className="relative mt-1.5 h-4 w-full">
        {options.map((opt, idx) => {
          const r = idx / totalSteps;
          const isActive = idx === activeIndex;
          const color = getEffortColor(idx, options.length);
          return (
            <button
              key={opt}
              type="button"
              className={cn(
                "absolute -translate-x-1/2 cursor-default text-[11px] whitespace-nowrap transition-colors",
                isActive
                  ? color.text
                  : "text-muted-foreground/80 hover:text-foreground",
              )}
              style={{
                left: `calc(${r} * (100% - 32px) + 16px)`,
              }}
              onClick={() => handleSelect(opt)}
            >
              {t(`provider.reasoningEffort.${opt}`)}
            </button>
          );
        })}
      </div>
    </div>
  );
}
