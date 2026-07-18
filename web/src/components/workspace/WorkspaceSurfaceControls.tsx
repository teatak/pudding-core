import { Trash2, Undo2 } from "lucide-react";

import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export const workspaceTabClassName = "inline-flex h-(--workspace-toolbar-tab-h) items-center gap-1.5 rounded-[7px] px-2 text-xs font-medium text-[var(--workspace-tab-foreground)] transition-colors hover:bg-[var(--workspace-tab-hover-background)] hover:text-foreground";
export const workspaceTabActiveClassName = "bg-[var(--workspace-tab-active-background)] text-foreground hover:bg-[var(--workspace-tab-active-background)]";

export function CanvasLibraryMenuSections({
  closedItems,
  layout = "menu",
  savedItems,
  onClearClosed,
  onDismiss,
  onRemoveClosed,
  onRemoveSaved,
  onOpenSaved,
  onRestoreClosed,
}: {
  closedItems: ClosedCanvasItem[];
  layout?: "menu" | "start";
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
  const startLayout = layout === "start";
  const savedRows = savedItems.map((entry) => (
    <SavedCanvasItemRow
      key={entry.id}
      entry={entry}
      onOpen={() => {
        onOpenSaved(entry);
        onDismiss();
      }}
      onRemove={() => onRemoveSaved(entry)}
    />
  ));
  const closedRows = closedItems.map((entry) => (
    <ClosedCanvasItemRow
      key={entry.id}
      entry={entry}
      onRemove={() => onRemoveClosed(entry)}
      onRestore={() => {
        onRestoreClosed(entry);
        onDismiss();
      }}
    />
  ));
  if (startLayout) {
    return (
      <div className="mx-auto mt-6 grid max-w-md min-w-0 grid-cols-1 gap-2 text-left">
        {savedItems.length > 0 ? (
          <Card className="min-w-0 gap-1.5 py-2" size="sm">
            <CardHeader className="min-h-7 items-center px-3">
              <CardTitle className="text-[13px] font-medium text-muted-foreground">{t("canvas.savedWidgets")}</CardTitle>
            </CardHeader>
            <CardContent className="max-h-56 overflow-y-auto px-0">{savedRows}</CardContent>
          </Card>
        ) : null}
        {closedItems.length > 0 ? (
          <Card className="min-w-0 gap-1.5 py-2" size="sm">
            <CardHeader className="min-h-7 items-center px-3">
              <CardTitle className="text-[13px] font-medium text-muted-foreground">{t("canvas.recentClosed")}</CardTitle>
              <CardAction className="self-center">
                <Button className="-mr-1 px-1 font-normal text-muted-foreground" size="xs" type="button" variant="ghost" onClick={onClearClosed}>
                  {t("canvas.clearRecentClosed")}
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="max-h-56 overflow-y-auto px-0">{closedRows}</CardContent>
          </Card>
        ) : null}
      </div>
    );
  }
  return (
    <div className="-mx-1 mt-1 border-t border-border pt-1">
      {savedItems.length > 0 ? (
        <div className="pb-1">
          <div className="flex h-7 items-center px-3 text-[11px] font-medium text-muted-foreground">
            <span>{t("canvas.savedWidgets")}</span>
          </div>
          <div className="max-h-56 overflow-y-auto">{savedRows}</div>
        </div>
      ) : null}
      {closedItems.length > 0 ? (
        <div className={cn(savedItems.length > 0 && "border-t border-border pt-1")}>
          <div className="flex h-7 items-center px-3 text-[11px] font-medium text-muted-foreground">
            <span>{t("canvas.recentClosed")}</span>
            <button className="-mr-1 ml-auto h-6 shrink-0 rounded px-1 text-[10px] text-muted-foreground hover:bg-accent hover:text-accent-foreground" type="button" onClick={onClearClosed}>
              {t("canvas.clearRecentClosed")}
            </button>
          </div>
          <div className="max-h-56 overflow-y-auto">{closedRows}</div>
        </div>
      ) : null}
    </div>
  );
}

function SavedCanvasItemRow({ entry, onOpen, onRemove }: { entry: SavedCanvasItem; onOpen: () => void; onRemove: () => void }) {
  const { t } = useI18n();
  const title = entry.title || entry.kind;
  return (
    <div className="group/saved mx-1 flex h-8 min-w-0 items-center rounded-md pr-2 hover:bg-accent focus-within:bg-accent">
      <button className="flex h-full min-w-0 flex-1 items-center gap-1.5 px-2 text-sm focus-visible:outline-none" title={title} type="button" onClick={onOpen}>
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
    <div className="group/closed mx-1 flex h-8 min-w-0 items-center rounded-md pr-2 hover:bg-accent focus-within:bg-accent">
      <button className="flex h-full min-w-0 flex-1 items-center gap-1.5 px-2 text-sm focus-visible:outline-none" title={title} type="button" onClick={onRestore}>
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
