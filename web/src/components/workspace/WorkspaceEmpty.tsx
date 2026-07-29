import { Folders, Globe, PanelsTopLeft, SquareTerminal } from "@/components/icons";

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
      <div className="flex min-h-full items-center justify-center px-6 py-12">
        <div className="w-full max-w-lg -translate-y-[3vh]">
          <div className="flex items-center gap-3 px-1 text-left">
            <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-[var(--workspace-tab-hover-background)] text-muted-foreground">
              <PanelsTopLeft className="size-5" data-icon-weight="subtle" />
            </span>
            <span className="grid min-w-0 gap-0.5">
              <h1 className="text-base font-semibold text-foreground">{t("workspace.title")}</h1>
              <span className="text-xs text-muted-foreground">{t("workspace.startDescription")}</span>
            </span>
          </div>
          <div className={cn("mt-5 grid gap-2", hasProject ? "grid-cols-3" : "grid-cols-2")}>
            {hasProject ? (
              <Button
                className="h-11 justify-start gap-2 rounded-lg border border-[var(--workspace-border)] bg-[var(--workspace-segment-background)] px-3 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-hover-background)] hover:text-foreground active:bg-[var(--workspace-tab-active-background)] [&_svg]:size-4"
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
              className="h-11 justify-start gap-2 rounded-lg border border-[var(--workspace-border)] bg-[var(--workspace-segment-background)] px-3 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-hover-background)] hover:text-foreground active:bg-[var(--workspace-tab-active-background)] [&_svg]:size-4"
              disabled={disabled || creatingBrowser || creatingTerminal}
              type="button"
              variant="ghost"
              onClick={onCreateBrowser}
            >
              {creatingBrowser ? <Spinner /> : <Globe />}
              {t("browser.create")}
            </Button>
            <Button
              className="h-11 justify-start gap-2 rounded-lg border border-[var(--workspace-border)] bg-[var(--workspace-segment-background)] px-3 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-hover-background)] hover:text-foreground active:bg-[var(--workspace-tab-active-background)] [&_svg]:size-4"
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
