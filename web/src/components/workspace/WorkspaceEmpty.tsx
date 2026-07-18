import { Compass, Folders, PanelsTopLeft, SquareTerminal } from "lucide-react";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import type { ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { CanvasLibraryMenuSections } from "./WorkspaceSurfaceControls";

export function WorkspaceEmpty({
  disabled,
  creatingBrowser,
  creatingTerminal,
  hasProject,
  closedItems,
  savedItems,
  onClearClosed,
  onCreateBrowser,
  onCreateTerminal,
  onOpenProject,
  onRemoveClosed,
  onRemoveSaved,
  onOpenSaved,
  onRestoreClosed,
}: {
  disabled: boolean;
  creatingBrowser: boolean;
  creatingTerminal: boolean;
  hasProject: boolean;
  closedItems: ClosedCanvasItem[];
  savedItems: SavedCanvasItem[];
  onClearClosed: () => void;
  onCreateBrowser: () => void;
  onCreateTerminal: () => void;
  onOpenProject: () => void;
  onRemoveClosed: (entry: ClosedCanvasItem) => void;
  onRemoveSaved: (entry: SavedCanvasItem) => void;
  onOpenSaved: (entry: SavedCanvasItem) => void;
  onRestoreClosed: (entry: ClosedCanvasItem) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="h-full overflow-y-auto bg-[var(--workspace-background)]">
      <div className="flex min-h-full items-center justify-center px-6 py-8">
        <div className="w-full max-w-xl text-center">
          <div className="mx-auto max-w-sm">
            <PanelsTopLeft className="mx-auto mb-4 size-8 text-muted-foreground" strokeWidth={1.6} />
            <h1 className="text-base font-semibold text-foreground">{t("workspace.startTitle")}</h1>
          </div>
          <div className={cn("mx-auto mt-5 grid gap-2", hasProject ? "max-w-md grid-cols-3" : "max-w-sm grid-cols-2")}>
            {hasProject ? (
              <Button
                className="h-10 justify-center gap-2 rounded-lg bg-[var(--workspace-tab-hover-background)] px-3 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-active-background)] hover:text-foreground [&_svg]:size-4"
                disabled={disabled || creatingBrowser || creatingTerminal}
                type="button"
                variant="ghost"
                onClick={onOpenProject}
              >
                <Folders />
                {t("workspace.project")}
              </Button>
            ) : null}
            <Button
              className="h-10 justify-center gap-2 rounded-lg bg-[var(--workspace-tab-hover-background)] px-3 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-active-background)] hover:text-foreground [&_svg]:size-4"
              disabled={disabled || creatingBrowser || creatingTerminal}
              type="button"
              variant="ghost"
              onClick={onCreateBrowser}
            >
              {creatingBrowser ? <Spinner /> : <Compass />}
              {t("browser.create")}
            </Button>
            <Button
              className="h-10 justify-center gap-2 rounded-lg bg-[var(--workspace-tab-hover-background)] px-3 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-active-background)] hover:text-foreground [&_svg]:size-4"
              disabled={disabled || creatingBrowser || creatingTerminal}
              type="button"
              variant="ghost"
              onClick={onCreateTerminal}
            >
              {creatingTerminal ? <Spinner /> : <SquareTerminal />}
              {t("terminal.create")}
            </Button>
          </div>
          <CanvasLibraryMenuSections
            closedItems={closedItems}
            layout="start"
            savedItems={savedItems}
            onClearClosed={onClearClosed}
            onDismiss={() => undefined}
            onRemoveClosed={onRemoveClosed}
            onRemoveSaved={onRemoveSaved}
            onOpenSaved={onOpenSaved}
            onRestoreClosed={onRestoreClosed}
          />
        </div>
      </div>
    </div>
  );
}
