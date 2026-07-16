import { ChevronDown, Folders, GalleryVerticalEnd, Trash2, Undo2 } from "lucide-react";

import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import { Popover, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import type { ClosedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export const workspaceTabClassName = "inline-flex h-(--workspace-toolbar-tab-h) items-center gap-1.5 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground";
export const workspaceTabActiveClassName = "bg-sidebar-accent text-foreground hover:bg-sidebar-accent";

export function ProjectSurfaceControl({ active, onActivate }: { active: boolean; onActivate: () => void }) {
  const { t } = useI18n();
  return (
    <button
      aria-pressed={active}
      className={cn(workspaceTabClassName, "shrink-0", active && workspaceTabActiveClassName)}
      title={t("workspace.project")}
      type="button"
      onClick={onActivate}
    >
      <span className="inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[5px] bg-amber-50 text-amber-700 dark:bg-amber-400/15 dark:text-amber-300">
        <Folders className="h-3.5 w-3.5" />
      </span>
      <span>{t("workspace.project")}</span>
    </button>
  );
}

export function CanvasSurfaceControl({ active, onActivate }: { active: boolean; onActivate: () => void }) {
  const { t } = useI18n();
  return (
    <button
      aria-pressed={active}
      className={cn(workspaceTabClassName, "shrink-0", active && workspaceTabActiveClassName)}
      title={t("canvas.title")}
      type="button"
      onClick={onActivate}
    >
      <CanvasKindIcon kind="widget" size="xs" />
      <span>{t("canvas.title")}</span>
    </button>
  );
}

export function CanvasLibraryControl({
  closedItems,
  open,
  onClearClosed,
  onOpenChange,
  onRemoveClosed,
  onRestoreClosed,
}: {
  closedItems: ClosedCanvasItem[];
  open: boolean;
  onClearClosed: () => void;
  onOpenChange: (open: boolean) => void;
  onRemoveClosed: (entry: ClosedCanvasItem) => void;
  onRestoreClosed: (entry: ClosedCanvasItem) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="no-drag-region shrink-0 text-muted-foreground">
      <Popover open={open} onOpenChange={onOpenChange}>
        <PopoverTrigger asChild>
          <button
            aria-label={t("canvas.widgetLibrary")}
            aria-expanded={open}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-transparent bg-muted/60 px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground data-[state=open]:bg-background data-[state=open]:text-foreground data-[state=open]:shadow-sm"
            title={t("canvas.widgetLibrary")}
            type="button"
          >
            <span className="inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[5px] bg-violet-50 text-violet-700 dark:bg-violet-400/15 dark:text-violet-300">
              <GalleryVerticalEnd className="h-3.5 w-3.5" />
            </span>
            <span>{t("canvas.widgetLibrary")}</span>
            <ChevronDown className="h-3.5 w-3.5" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="end" className="max-h-[min(34rem,calc(100vh-4rem))] w-72 gap-0 overflow-hidden p-0" collisionPadding={8}>
          <div className="border-b px-3 py-2.5">
            <PopoverTitle className="text-sm">{t("canvas.widgetLibrary")}</PopoverTitle>
          </div>
          <div className="p-2">
            <div className="flex items-center justify-between px-2 py-1 text-xs font-medium text-muted-foreground">
              <span>{t("canvas.recentClosed")}</span>
              {closedItems.length > 0 ? (
                <button className="h-7 shrink-0 rounded-md px-2 text-[11px] text-muted-foreground hover:bg-accent hover:text-accent-foreground" type="button" onClick={onClearClosed}>
                  {t("canvas.clearRecentClosed")}
                </button>
              ) : null}
            </div>
            {closedItems.length > 0 ? (
              <div className="max-h-56 overflow-y-auto">
                {closedItems.map((entry) => (
                  <ClosedCanvasItemRow
                    key={entry.id}
                    entry={entry}
                    onRemove={() => onRemoveClosed(entry)}
                    onRestore={() => { onRestoreClosed(entry); onOpenChange(false); }}
                  />
                ))}
              </div>
            ) : (
              <div className="px-2 py-3 text-xs text-muted-foreground">{t("canvas.widgetLibraryEmpty")}</div>
            )}
          </div>
          <div className="border-t p-2">
            <div className="px-2 py-1 text-xs font-medium text-muted-foreground">{t("canvas.savedWidgets")}</div>
            <div className="px-2 py-3 text-xs text-muted-foreground">{t("canvas.savedWidgetsComingSoon")}</div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

function ClosedCanvasItemRow({ entry, onRemove, onRestore }: { entry: ClosedCanvasItem; onRemove: () => void; onRestore: () => void }) {
  const { t } = useI18n();
  const title = entry.title || entry.kind;
  return (
    <div
      className="group/closed mx-1 flex h-9 min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-sm hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
      role="button"
      tabIndex={0}
      title={title}
      onClick={onRestore}
      onKeyDown={(event) => {
        if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) {
          event.preventDefault();
          onRestore();
        }
      }}
    >
      <CanvasKindIcon kind={entry.kind} size="xs" />
      <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      <div className="relative flex h-6 w-12 shrink-0 items-center justify-end">
        <span className="absolute right-0 text-xs text-muted-foreground transition-opacity group-hover/closed:opacity-0 group-focus-within/closed:opacity-0">
          {formatClosedTime(entry.closedAt)}
        </span>
        <span className="absolute right-0 flex items-center gap-1 opacity-0 transition-opacity group-hover/closed:opacity-100 group-focus-within/closed:opacity-100">
          <button aria-label={t("canvas.restore")} className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-background/80 hover:text-foreground" type="button" onClick={(event) => { event.stopPropagation(); onRestore(); }}>
            <Undo2 className="h-3.5 w-3.5" />
          </button>
          <button aria-label={t("canvas.delete")} className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-destructive hover:bg-destructive/10" type="button" onClick={(event) => { event.stopPropagation(); onRemove(); }}>
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </span>
      </div>
    </div>
  );
}

function formatClosedTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}
