import { Folders, Globe, SquareTerminal } from "@/components/icons";

import { WorkspaceEmptyIllustration } from "@/components/illustrations/WorkspaceEmptyIllustration";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import type { ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

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
    <div className="pudding-workspace-empty h-full overflow-y-auto bg-[var(--workspace-background)]">
      <div className="flex min-h-full items-start justify-center px-6 pt-[clamp(3rem,8vh,6rem)] pb-16">
        <div className="w-full max-w-2xl">
          <div className="grid w-full justify-items-center text-center">
            <WorkspaceEmptyIllustration />
            <h1 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
              {t("workspace.title")}
            </h1>
            <p className="mt-2 max-w-md text-sm leading-6 text-muted-foreground">
              {t("workspace.startDescription")}
            </p>
          </div>
          <div
            className="pudding-workspace-empty-actions mx-auto mt-7 grid w-full max-w-xl gap-2"
            data-has-project={hasProject}
          >
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
