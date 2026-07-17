import { X } from "lucide-react";
import type { ReactNode } from "react";

import { CanvasKindIcon, titleForCanvasItem } from "@/components/canvas/CanvasKindIcon";
import { workspaceTabActiveClassName, workspaceTabClassName } from "@/components/workspace/WorkspaceSurfaceControls";
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
    <div className="flex h-9 shrink-0 items-center gap-2 border-b border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] px-2.5">
      <div className="h-full min-w-0 flex-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        <div className="flex h-full w-fit max-w-full min-w-0 items-center gap-0.5 px-px py-1">
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
        workspaceTabClassName,
        "group relative flex w-36 min-w-24 max-w-none shrink pr-6 pl-2",
        active && workspaceTabActiveClassName,
      )}
      title={title}
    >
      <button className="flex h-full min-w-0 flex-1 items-center gap-1.5" type="button" onClick={onSelect}>
        <CanvasKindIcon className="!bg-transparent" kind={item.kind} size="xs" />
        <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      </button>
      <button
        aria-label={t("canvas.delete")}
        className="pointer-events-none absolute right-1 top-1/2 z-10 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded-md bg-transparent opacity-0 transition-colors hover:bg-accent focus-visible:pointer-events-auto focus-visible:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100"
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
