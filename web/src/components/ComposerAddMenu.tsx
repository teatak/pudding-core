import { FolderOpen, Loader2, Paperclip, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export type ComposerAddMenuAction = {
  disabled?: boolean;
  id: "files" | "folder";
  label: string;
  loading?: boolean;
};

export function ComposerAddButton({
  active,
  busy,
  label,
  onClick,
}: {
  active: boolean;
  busy: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button
      aria-expanded={active}
      aria-label={label}
      className={cn(
        "rounded-full border-0 bg-transparent text-muted-foreground hover:text-foreground",
        active && "bg-muted text-foreground",
      )}
      size="icon"
      type="button"
      variant="ghost"
      onClick={onClick}
    >
      {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
    </Button>
  );
}

export function ComposerAddActionMenu({
  actions,
  selectedIndex,
  onHover,
  onSelect,
}: {
  actions: ComposerAddMenuAction[];
  selectedIndex: number;
  onHover: (index: number) => void;
  onSelect: (action: ComposerAddMenuAction) => void;
}) {
  return (
    <div
      className="absolute bottom-full left-16 z-20 w-60 rounded-t-lg border border-border/70 bg-popover/95 p-1 text-sm text-popover-foreground shadow-sm backdrop-blur"
      role="listbox"
    >
      {actions.map((action, index) => (
        <button
          key={action.id}
          aria-selected={index === selectedIndex}
          className={cn(
            "flex h-9 w-full items-center gap-2 rounded-md px-2 text-left hover:bg-muted disabled:opacity-60",
            index === selectedIndex && "bg-muted text-foreground",
          )}
          disabled={action.disabled || action.loading}
          role="option"
          type="button"
          onMouseEnter={() => onHover(index)}
          onMouseDown={(event) => {
            event.preventDefault();
            if (!action.disabled && !action.loading) {
              onSelect(action);
            }
          }}
        >
          {action.loading ? (
            <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />
          ) : action.id === "folder" ? (
            <FolderOpen className="size-4 shrink-0 text-muted-foreground" />
          ) : (
            <Paperclip className="size-4 shrink-0 text-muted-foreground" />
          )}
          <span className="min-w-0 truncate">{action.label}</span>
        </button>
      ))}
    </div>
  );
}
