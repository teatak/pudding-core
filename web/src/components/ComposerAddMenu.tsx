import { FolderOpen, Loader2, Paperclip, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

export function ComposerAddMenu({
  attachFolderLabel,
  attachLabel,
  menuTitle,
  pickingFolder,
  onAttachFiles,
  onAttachFolder,
}: {
  attachFolderLabel: string;
  attachLabel: string;
  menuTitle: string;
  pickingFolder: boolean;
  onAttachFiles: () => void;
  onAttachFolder: () => void;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          aria-label={menuTitle}
          className="rounded-full border-0 bg-transparent text-muted-foreground hover:text-foreground"
          size="icon"
          type="button"
          variant="ghost"
        >
          <Plus className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-56 p-1.5" side="top" sideOffset={8}>
        <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">{menuTitle}</div>
        <button
          className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm hover:bg-muted"
          type="button"
          onClick={onAttachFiles}
        >
          <Paperclip className="size-4 text-muted-foreground" />
          <span>{attachLabel}</span>
        </button>
        <button
          className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm hover:bg-muted disabled:opacity-60"
          disabled={pickingFolder}
          type="button"
          onClick={onAttachFolder}
        >
          {pickingFolder ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : <FolderOpen className="size-4 text-muted-foreground" />}
          <span>{attachFolderLabel}</span>
        </button>
      </PopoverContent>
    </Popover>
  );
}
