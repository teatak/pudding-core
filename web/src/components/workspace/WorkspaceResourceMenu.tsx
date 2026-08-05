import { useState } from "react";

import { Folders, Globe, Plus, SquareTerminal } from "@/components/icons";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
} from "@/components/AppMenu";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import type { ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

import { CanvasLibraryMenuSections } from "./WorkspaceSurfaceControls";

export function WorkspaceResourceMenu({
  closedItems,
  creatingBrowser,
  creatingTerminal,
  hasProject,
  projectTabVisible,
  savedItems,
  onClearClosed,
  onCreateBrowser,
  onCreateTerminal,
  onOpenProject,
  onOpenSaved,
  onRemoveClosed,
  onRemoveSaved,
  onRestoreClosed,
}: {
  closedItems: ClosedCanvasItem[];
  creatingBrowser: boolean;
  creatingTerminal: boolean;
  hasProject: boolean;
  projectTabVisible: boolean;
  savedItems: SavedCanvasItem[];
  onClearClosed: () => void;
  onCreateBrowser: () => void;
  onCreateTerminal: () => void;
  onOpenProject: () => void;
  onOpenSaved: (entry: SavedCanvasItem) => void;
  onRemoveClosed: (entry: ClosedCanvasItem) => void;
  onRemoveSaved: (entry: SavedCanvasItem) => void;
  onRestoreClosed: (entry: ClosedCanvasItem) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const creating = creatingBrowser || creatingTerminal;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("workspace.add")}
          className="pudding-toolbar-icon-button no-drag-region shrink-0 rounded-md"
          disabled={creating}
          size="icon-sm"
          type="button"
          variant="ghost"
        >
          {creating ? <Spinner className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64 space-y-0">
        {hasProject && !projectTabVisible ? (
          <DropdownMenuItem className="h-8 px-2.5" onSelect={onOpenProject}>
            <Folders />
            {t("workspace.project")}
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem className="h-8 px-2.5" onSelect={onCreateBrowser}>
          <Globe />
          {t("browser.create")}
        </DropdownMenuItem>
        <DropdownMenuItem className="h-8 px-2.5" onSelect={onCreateTerminal}>
          <SquareTerminal />
          {t("terminal.create")}
        </DropdownMenuItem>
        <CanvasLibraryMenuSections
          closedItems={closedItems}
          savedItems={savedItems}
          onClearClosed={onClearClosed}
          onDismiss={() => setOpen(false)}
          onOpenSaved={onOpenSaved}
          onRemoveClosed={onRemoveClosed}
          onRemoveSaved={onRemoveSaved}
          onRestoreClosed={onRestoreClosed}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
