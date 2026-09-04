import { Folders, Globe } from "@/components/icons";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import type { ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

import { CanvasLibraryMenuSections } from "./WorkspaceSurfaceControls";

const workspaceEmptyActionClassName =
  "h-10 justify-center gap-2 rounded-lg border border-[var(--workspace-border)] bg-[var(--workspace-segment-background)] px-4 text-sm font-medium text-foreground shadow-none hover:bg-[var(--workspace-tab-hover-background)] active:bg-[var(--workspace-tab-active-background)] dark:hover:bg-[var(--workspace-tab-hover-background)] dark:active:bg-[var(--workspace-tab-active-background)] [&_svg]:size-4";

export function WorkspaceEmpty({
  disabled,
  creatingBrowser,
  hasProject,
  projectLabel,
  closedItems,
  savedItems,
  onClearClosed,
  onCreateBrowser,
  onOpenProject,
  onRemoveClosed,
  onRemoveSaved,
  onOpenSaved,
  onRestoreClosed,
}: {
  disabled: boolean;
  creatingBrowser: boolean;
  hasProject: boolean;
  projectLabel: string;
  closedItems: ClosedCanvasItem[];
  savedItems: SavedCanvasItem[];
  onClearClosed: () => void;
  onCreateBrowser: () => void;
  onOpenProject: () => void;
  onRemoveClosed: (entry: ClosedCanvasItem) => void;
  onRemoveSaved: (entry: SavedCanvasItem) => void;
  onOpenSaved: (entry: SavedCanvasItem) => void;
  onRestoreClosed: (entry: ClosedCanvasItem) => void;
}) {
  const { t } = useI18n();
  const hasContinuableCanvas = savedItems.length > 0 || closedItems.length > 0;
  return (
    <div className="pudding-workspace-empty h-full overflow-y-auto bg-[var(--workspace-background)]">
      <div className="flex min-h-full items-center justify-center px-6 py-16">
        <div className="w-full max-w-xl">
          <div className="mb-7 text-center">
            <h2 className="text-lg font-semibold tracking-tight text-foreground">
              {t(hasContinuableCanvas ? "workspace.emptyContinueTitle" : "workspace.emptyStartTitle")}
            </h2>
          </div>
          <div
            className={`pudding-workspace-empty-actions mx-auto gap-2 ${
              hasProject ? "grid w-full max-w-sm grid-cols-2" : "flex justify-center"
            }`}
          >
            {hasProject ? (
              <Button
                className={workspaceEmptyActionClassName}
                disabled={disabled || creatingBrowser}
                type="button"
                variant="ghost"
                onClick={onOpenProject}
              >
                <Folders />
                {projectLabel}
              </Button>
            ) : null}
            <Button
              className={workspaceEmptyActionClassName}
              disabled={disabled || creatingBrowser}
              type="button"
              variant="ghost"
              onClick={onCreateBrowser}
            >
              {creatingBrowser ? <Spinner /> : <Globe />}
              {t("browser.create")}
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
