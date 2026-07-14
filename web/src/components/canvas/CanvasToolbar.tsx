import { X } from "lucide-react";
import type { ReactNode } from "react";

import { CanvasKindIcon, titleForCanvasItem } from "@/components/canvas/CanvasKindIcon";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export function CanvasToolbar({
  activeItemID,
  items,
  library,
  onClose,
  onSelect,
}: {
  activeItemID?: string;
  items: CanvasItem[];
  library: ReactNode;
  onClose: (item: CanvasItem) => void;
  onSelect: (item: CanvasItem) => void;
}) {
  return (
    <div className="flex h-10 shrink-0 items-center gap-2 bg-[var(--workspace-background)] px-3">
      <div className="min-w-0 flex-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        <div className="inline-flex min-w-max items-center gap-1 rounded-lg bg-muted/60 p-0.5">
          {items.map((item) => (
            <CanvasItemTab
              key={item.id}
              active={item.id === activeItemID}
              item={item}
              onClose={() => onClose(item)}
              onSelect={() => onSelect(item)}
            />
          ))}
        </div>
      </div>
      {library}
    </div>
  );
}

function CanvasItemTab({
  active,
  item,
  onClose,
  onSelect,
}: {
  active: boolean;
  item: CanvasItem;
  onClose: () => void;
  onSelect: () => void;
}) {
  const { t } = useI18n();
  const title = titleForCanvasItem(item, t);

  return (
    <div
      className={cn(
        "group flex h-7 min-w-28 max-w-48 items-center gap-1.5 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-background/60 hover:text-foreground",
        active && "bg-background text-foreground shadow-sm hover:bg-background",
      )}
      title={title}
    >
      <button className="flex min-w-0 flex-1 items-center gap-1.5" type="button" onClick={onSelect}>
        <CanvasKindIcon kind={item.kind} size="xs" />
        <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      </button>
      <button
        aria-label={t("canvas.delete")}
        className="pointer-events-none inline-flex size-5 shrink-0 items-center justify-center rounded opacity-0 transition-opacity hover:bg-foreground/10 group-hover:pointer-events-auto group-hover:opacity-60 group-focus-within:pointer-events-auto group-focus-within:opacity-60 hover:opacity-100"
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onClose();
        }}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}
