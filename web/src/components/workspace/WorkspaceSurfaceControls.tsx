import { Folders, Trash2, Undo2 } from "lucide-react";

import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import type { ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export const workspaceTabClassName = "inline-flex h-(--workspace-toolbar-tab-h) items-center gap-1.5 rounded-[7px] px-2 text-xs font-medium text-[var(--workspace-tab-foreground)] transition-colors hover:bg-[var(--workspace-tab-hover-background)] hover:text-foreground";
export const workspaceTabActiveClassName = "bg-[var(--workspace-tab-active-background)] text-foreground hover:bg-[var(--workspace-tab-active-background)]";

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
      <span className="inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[5px] text-amber-700 dark:text-amber-300">
        <Folders className="h-3.5 w-3.5" />
      </span>
      <span>{t("workspace.project")}</span>
    </button>
  );
}

export function CanvasLibraryMenuSections({
  closedItems,
  savedItems,
  onClearClosed,
  onDismiss,
  onRemoveClosed,
  onRemoveSaved,
  onOpenSaved,
  onRestoreClosed,
}: {
  closedItems: ClosedCanvasItem[];
  savedItems: SavedCanvasItem[];
  onClearClosed: () => void;
  onDismiss: () => void;
  onRemoveClosed: (entry: ClosedCanvasItem) => void;
  onRemoveSaved: (entry: SavedCanvasItem) => void;
  onOpenSaved: (entry: SavedCanvasItem) => void;
  onRestoreClosed: (entry: ClosedCanvasItem) => void;
}) {
  const { t } = useI18n();
  if (savedItems.length === 0 && closedItems.length === 0) {
    return null;
  }
  return (
    <div className="-mx-1 mt-1 border-t border-border pt-1">
      {savedItems.length > 0 ? (
        <div className="pb-1">
          <div className="flex h-7 items-center justify-between px-2.5 text-[11px] font-medium text-muted-foreground">
            <span>{t("canvas.savedWidgets")}</span>
            <span className="tabular-nums text-muted-foreground/70">{savedItems.length}</span>
          </div>
          <div className="max-h-56 overflow-y-auto">
            {savedItems.map((entry) => (
              <SavedCanvasItemRow
                key={entry.id}
                entry={entry}
                onOpen={() => {
                  onOpenSaved(entry);
                  onDismiss();
                }}
                onRemove={() => onRemoveSaved(entry)}
              />
            ))}
          </div>
        </div>
      ) : null}
      {closedItems.length > 0 ? (
        <div className={cn(savedItems.length > 0 && "border-t border-border pt-1")}>
          <div className="flex h-7 items-center px-2.5 text-[11px] font-medium text-muted-foreground">
            <span>{t("canvas.recentClosed")}</span>
            <button className="ml-auto h-6 shrink-0 rounded px-1.5 text-[10px] text-muted-foreground hover:bg-accent hover:text-accent-foreground" type="button" onClick={onClearClosed}>
              {t("canvas.clearRecentClosed")}
            </button>
          </div>
          <div className="max-h-56 overflow-y-auto">
            {closedItems.map((entry) => (
              <ClosedCanvasItemRow
                key={entry.id}
                entry={entry}
                onRemove={() => onRemoveClosed(entry)}
                onRestore={() => {
                  onRestoreClosed(entry);
                  onDismiss();
                }}
              />
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function SavedCanvasItemRow({ entry, onOpen, onRemove }: { entry: SavedCanvasItem; onOpen: () => void; onRemove: () => void }) {
  const { t } = useI18n();
  const title = entry.title || entry.kind;
  return (
    <div className="group/saved mx-1 flex h-8 min-w-0 items-center rounded-md pr-2.5 hover:bg-accent focus-within:bg-accent">
      <button className="flex h-full min-w-0 flex-1 items-center gap-1.5 px-2.5 text-sm focus-visible:outline-none" title={title} type="button" onClick={onOpen}>
        <CanvasKindIcon className="!h-4 !w-4 !bg-transparent [&>svg]:!h-4 [&>svg]:!w-4" kind={entry.kind} size="xs" />
        <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      </button>
      <span className="flex items-center gap-1 opacity-0 transition-opacity group-hover/saved:opacity-100 group-focus-within/saved:opacity-100">
        <button aria-label={t("canvas.deleteSavedWidget")} className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-destructive hover:bg-destructive/10" type="button" onClick={onRemove}>
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </span>
    </div>
  );
}

function ClosedCanvasItemRow({ entry, onRemove, onRestore }: { entry: ClosedCanvasItem; onRemove: () => void; onRestore: () => void }) {
  const { t } = useI18n();
  const title = entry.title || entry.kind;
  return (
    <div className="group/closed mx-1 flex h-8 min-w-0 items-center rounded-md pr-2.5 hover:bg-accent focus-within:bg-accent">
      <button className="flex h-full min-w-0 flex-1 items-center gap-1.5 px-2.5 text-sm focus-visible:outline-none" title={title} type="button" onClick={onRestore}>
        <CanvasKindIcon className="!h-4 !w-4 !bg-transparent [&>svg]:!h-4 [&>svg]:!w-4" kind={entry.kind} size="xs" />
        <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      </button>
      <div className="relative flex h-6 w-12 shrink-0 items-center justify-end">
        <span className="absolute inset-y-0 right-0 flex items-center text-xs text-muted-foreground transition-opacity group-hover/closed:opacity-0 group-focus-within/closed:opacity-0">
          {formatClosedTime(entry.closedAt)}
        </span>
        <span className="absolute inset-y-0 right-0 flex items-center gap-1 opacity-0 transition-opacity group-hover/closed:opacity-100 group-focus-within/closed:opacity-100">
          <button aria-label={t("canvas.restore")} className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-background/80 hover:text-foreground" type="button" onClick={onRestore}>
            <Undo2 className="h-3.5 w-3.5" />
          </button>
          <button aria-label={t("canvas.delete")} className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-destructive hover:bg-destructive/10" type="button" onClick={onRemove}>
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
