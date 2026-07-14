import {
  Blocks,
  CalendarDays,
  ChartPie,
  FileCode2,
  FileText,
  Image,
  Sheet,
  type LucideIcon,
} from "lucide-react";

import type { CanvasItem } from "@/contracts/api";
import { builtinAppIconClass } from "@/components/AppIdentity";
import { cn } from "@/lib/utils";

import { titleFromPayload } from "./canvasPayload";

const KIND_ICON: Record<string, LucideIcon> = {
  chart: ChartPie,
  code: FileCode2,
  form: FileText,
  gallery: Image,
  grid: Blocks,
  iframe: Blocks,
  image: Image,
  markdown: FileText,
  metric: ChartPie,
  table: Sheet,
  timeline: CalendarDays,
  widget: Blocks,
};

const KIND_TILE_CLASS: Record<string, string> = {
  chart: "bg-amber-50 text-amber-700 dark:bg-amber-400/15 dark:text-amber-300",
  code: "bg-blue-50 text-blue-700 dark:bg-blue-400/15 dark:text-blue-300",
  form: "bg-violet-50 text-violet-700 dark:bg-violet-400/15 dark:text-violet-300",
  gallery: "bg-pink-50 text-pink-700 dark:bg-pink-400/15 dark:text-pink-300",
  grid: "bg-indigo-50 text-indigo-700 dark:bg-indigo-400/15 dark:text-indigo-300",
  iframe: "bg-sky-50 text-sky-700 dark:bg-sky-400/15 dark:text-sky-300",
  image: "bg-pink-50 text-pink-700 dark:bg-pink-400/15 dark:text-pink-300",
  markdown: "bg-blue-50 text-blue-700 dark:bg-blue-400/15 dark:text-blue-300",
  metric: "bg-sky-50 text-sky-700 dark:bg-sky-400/15 dark:text-sky-300",
  table: "bg-emerald-50 text-emerald-700 dark:bg-emerald-400/15 dark:text-emerald-300",
  timeline: "bg-cyan-50 text-cyan-700 dark:bg-cyan-400/15 dark:text-cyan-300",
};

export function CanvasKindIcon({ kind, size = "sm" }: { kind?: string; size?: "xs" | "sm" }) {
  const Icon = KIND_ICON[kind || ""] || Blocks;
  const sizeClass = size === "xs" ? "h-[18px] w-[18px] rounded-[5px]" : "h-5 w-5 rounded-md";
  const iconClass = size === "xs" ? "h-3.5 w-3.5" : "h-4 w-4";
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex shrink-0 items-center justify-center",
        sizeClass,
        kind === "widget"
          ? builtinAppIconClass("canvas")
          : KIND_TILE_CLASS[kind || ""] || "bg-muted text-muted-foreground",
      )}
    >
      <Icon className={iconClass} />
    </span>
  );
}

export function titleForCanvasItem(item: CanvasItem, t: (key: string) => string) {
  return item.title?.trim() || titleFromPayload(item.item) || item.kind || t("canvas.untitled");
}
