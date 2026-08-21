import {
  closestCenter,
  DndContext,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  horizontalListSortingStrategy,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { FileCode2, FileDiff, Folders, Globe, X } from "@/components/icons";
import { memo, useEffect } from "react";

import type { BrowserTab } from "@/api/client";
import { BrowserFavicon } from "@/browser/BrowserFavicon";
import { browserTabFaviconURL, browserTabTitle } from "@/browser/helpers";
import { CanvasKindIcon, titleForCanvasItem } from "@/components/canvas/CanvasKindIcon";
import { workspaceTabActiveClassName, workspaceTabClassName } from "@/components/workspace/WorkspaceSurfaceControls";
import type { CanvasItem } from "@/contracts/api";
import type { WorkspaceSurface } from "@/components/workspace/types";
import { Spinner } from "@/components/Spinner";
import { useHorizontalScrollMask } from "@/hooks/useHorizontalScrollMask";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import {
  mergeWorkspaceTabOrder,
  reconcileWorkspaceTabOrder,
  setWorkspaceTabOrder,
  useWorkspaceTabOrder,
} from "@/state/workspaceTabOrderStore";

function BrowserTabIcon({ faviconURL, pageURL }: { faviconURL?: string; pageURL: string }) {
  return (
    <span
      aria-hidden="true"
      className="inline-flex h-(--workspace-toolbar-tab-icon) w-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center overflow-hidden rounded-[5px] bg-transparent text-current"
    >
      <BrowserFavicon
        className="h-full w-full object-cover"
        fallback={<Globe className="h-3.5 w-3.5" />}
        faviconURL={faviconURL}
        pageURL={pageURL}
      />
    </span>
  );
}

function FilePreviewTabIcon({ kind }: { kind: WorkspaceFilePreviewTab["kind"] }) {
  return (
    <span
      aria-hidden="true"
      className="inline-flex h-(--workspace-toolbar-tab-icon) w-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px] text-current"
    >
      {kind === "diff" ? <FileDiff className="h-3.5 w-3.5" /> : <FileCode2 className="h-3.5 w-3.5" />}
    </span>
  );
}

export type WorkspaceFilePreviewTab = {
  id: string;
  kind: "diff" | "file";
  label: string;
  openedAt: number;
  path: string;
};

type SurfaceTab =
  | { kind: "project"; id: "project"; sortAt: number }
  | { kind: "browser"; id: string; sortAt: number; browser: BrowserTab }
  | { kind: "file"; id: string; sortAt: number; file: WorkspaceFilePreviewTab }
  | { kind: "widget"; id: string; sortAt: number; widget: CanvasItem };

export const WorkspaceResourceTabs = memo(function WorkspaceResourceTabs({
  activeBrowserTabID,
  activeCanvasItemID,
  activeFilePreviewID,
  activeSurface,
  browserTabs,
  canvasItems,
  closingCanvasItemID,
  closingBrowserTabID,
  filePreviewActive,
  filePreviewTabs,
  orderScope,
  projectLabel,
  projectTabVisible,
  onCloseBrowser,
  onCloseCanvasItem,
  onCloseFilePreview,
  onCloseProject,
  onSelectBrowser,
  onSelectCanvasItem,
  onSelectFilePreview,
  onSelectProject,
}: {
  activeBrowserTabID?: string;
  activeCanvasItemID?: string;
  activeFilePreviewID?: string;
  activeSurface: WorkspaceSurface;
  browserTabs: BrowserTab[];
  canvasItems: CanvasItem[];
  closingCanvasItemID?: string;
  closingBrowserTabID?: string;
  filePreviewActive: boolean;
  filePreviewTabs: WorkspaceFilePreviewTab[];
  orderScope: string;
  projectLabel: string;
  projectTabVisible: boolean;
  onCloseBrowser: (tabID: string) => void;
  onCloseCanvasItem: (itemID: string) => void;
  onCloseFilePreview: (previewID: string) => void;
  onCloseProject: () => void;
  onSelectBrowser: (tabID: string) => void;
  onSelectCanvasItem: (itemID: string) => void;
  onSelectFilePreview: (previewID: string) => void;
  onSelectProject: () => void;
}) {
  const scrollMask = useHorizontalScrollMask<HTMLDivElement>();
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 180, tolerance: 6 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
      keyboardCodes: { start: ["Space"], cancel: ["Escape"], end: ["Space"] },
    }),
  );
  const createdTabs: SurfaceTab[] = [
    ...browserTabs.map((browser) => ({ kind: "browser" as const, id: browser.id, sortAt: Date.parse(browser.createdAt), browser })),
    ...filePreviewTabs.map((file) => ({ kind: "file" as const, id: file.id, sortAt: file.openedAt, file })),
    ...canvasItems.map((widget) => ({ kind: "widget" as const, id: widget.id, sortAt: Date.parse(widget.createdAt), widget })),
    ...(projectTabVisible ? [{ kind: "project" as const, id: "project" as const, sortAt: Number.MAX_SAFE_INTEGER }] : []),
  ].sort((left, right) => left.sortAt - right.sortAt || left.id.localeCompare(right.id));
  const createdTabIDs = createdTabs.map(surfaceTabID);
  const savedOrder = useWorkspaceTabOrder(orderScope);
  const orderedIDs = mergeWorkspaceTabOrder(savedOrder, createdTabIDs);
  const tabByID = new Map(createdTabs.map((tab) => [surfaceTabID(tab), tab]));
  const tabs = orderedIDs.flatMap((id) => {
    const tab = tabByID.get(id);
    return tab ? [tab] : [];
  });
  const orderSignature = createdTabIDs.join("\u0000");

  useEffect(() => {
    reconcileWorkspaceTabOrder(orderScope, createdTabIDs);
  }, [orderScope, orderSignature]);

  if (createdTabs.length === 0) {
    return null;
  }

  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) {
      return;
    }
    const from = orderedIDs.indexOf(String(active.id));
    const to = orderedIDs.indexOf(String(over.id));
    if (from < 0 || to < 0) {
      return;
    }
    setWorkspaceTabOrder(orderScope, arrayMove(orderedIDs, from, to));
  };

  return (
    <DndContext
      autoScroll
      collisionDetection={closestCenter}
      sensors={sensors}
      onDragEnd={handleDragEnd}
    >
      <div
        ref={scrollMask.ref}
        className="no-drag-region w-fit max-w-full min-w-0 overflow-x-auto overflow-y-hidden text-muted-foreground [scrollbar-width:none] [-webkit-overflow-scrolling:touch] [&::-webkit-scrollbar]:hidden"
        style={scrollMask.style}
      >
        <SortableContext items={orderedIDs} strategy={horizontalListSortingStrategy}>
          <div className="flex w-fit max-w-full min-w-0 items-center gap-0.5 px-px py-1">
            {tabs.map((tab) => (
              <SortableSurfaceTabButton
                key={surfaceTabID(tab)}
                activeBrowserTabID={activeBrowserTabID}
                activeCanvasItemID={activeCanvasItemID}
                activeFilePreviewID={activeFilePreviewID}
                activeSurface={activeSurface}
                closingBrowserTabID={closingBrowserTabID}
                closingCanvasItemID={closingCanvasItemID}
                filePreviewActive={filePreviewActive}
                projectLabel={projectLabel}
                tab={tab}
                onCloseBrowser={onCloseBrowser}
                onCloseCanvasItem={onCloseCanvasItem}
                onCloseFilePreview={onCloseFilePreview}
                onCloseProject={onCloseProject}
                onSelectBrowser={onSelectBrowser}
                onSelectCanvasItem={onSelectCanvasItem}
                onSelectFilePreview={onSelectFilePreview}
                onSelectProject={onSelectProject}
              />
            ))}
          </div>
        </SortableContext>
      </div>
    </DndContext>
  );
});

function SortableSurfaceTabButton({
  activeBrowserTabID,
  activeCanvasItemID,
  activeFilePreviewID,
  activeSurface,
  closingBrowserTabID,
  closingCanvasItemID,
  filePreviewActive,
  projectLabel,
  tab,
  onCloseBrowser,
  onCloseCanvasItem,
  onCloseFilePreview,
  onCloseProject,
  onSelectBrowser,
  onSelectCanvasItem,
  onSelectFilePreview,
  onSelectProject,
}: {
  activeBrowserTabID?: string;
  activeCanvasItemID?: string;
  activeFilePreviewID?: string;
  activeSurface: WorkspaceSurface;
  closingBrowserTabID?: string;
  closingCanvasItemID?: string;
  filePreviewActive: boolean;
  projectLabel: string;
  tab: SurfaceTab;
  onCloseBrowser: (tabID: string) => void;
  onCloseCanvasItem: (itemID: string) => void;
  onCloseFilePreview: (previewID: string) => void;
  onCloseProject: () => void;
  onSelectBrowser: (tabID: string) => void;
  onSelectCanvasItem: (itemID: string) => void;
  onSelectFilePreview: (previewID: string) => void;
  onSelectProject: () => void;
}) {
  const { t } = useI18n();
  const browser = tab.kind === "browser" ? tab.browser : undefined;
  const project = tab.kind === "project";
  const file = tab.kind === "file" ? tab.file : undefined;
  const widget = tab.kind === "widget" ? tab.widget : undefined;
  const label = project
    ? projectLabel
    : browser
      ? browserTabTitle(browser, t("browser.newTab"), t("browser.newTab"))
      : widget
        ? titleForCanvasItem(widget, t)
        : file?.label || t("uiContext.filePreview");
  const selected =
    (tab.kind === "project" && activeSurface === "project") ||
    (tab.kind === "browser" && activeSurface === "browser" && tab.id === activeBrowserTabID) ||
    (tab.kind === "file" && filePreviewActive && tab.id === activeFilePreviewID) ||
    (tab.kind === "widget" && activeSurface === "canvas" && !filePreviewActive && tab.id === activeCanvasItemID);
  const closePending =
    (tab.kind === "browser" && tab.id === closingBrowserTabID) ||
    (tab.kind === "widget" && tab.id === closingCanvasItemID);
  const { attributes, isDragging, listeners, setNodeRef, transform, transition } = useSortable({
    id: surfaceTabID(tab),
    disabled: closePending,
  });
  const horizontalTransform = transform ? { ...transform, y: 0, scaleX: 1, scaleY: 1 } : null;
  const closeLabel =
    tab.kind === "browser"
      ? t("browser.release")
      : tab.kind === "project"
        ? t("workspace.closeProject")
        : tab.kind === "widget"
          ? t("canvas.delete")
          : t("canvas.filePreviewClose");
  const closeTab = () => {
    if (tab.kind === "browser") {
      onCloseBrowser(tab.id);
    } else if (tab.kind === "project") {
      onCloseProject();
    } else if (tab.kind === "file") {
      onCloseFilePreview(tab.id);
    } else {
      onCloseCanvasItem(tab.id);
    }
  };

  return (
    <button
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      aria-label={label}
      aria-selected={selected}
      className={cn(
        workspaceTabClassName,
        "group relative w-28 min-w-20 shrink whitespace-nowrap",
        selected && workspaceTabActiveClassName,
        isDragging && "cursor-grabbing opacity-80 shadow-md",
      )}
      data-active={selected}
      disabled={closePending}
      style={{
        transform: CSS.Transform.toString(horizontalTransform),
        transition,
        zIndex: isDragging ? 1 : undefined,
      }}

      type="button"
      onClick={() => {
        if (tab.kind === "browser") {
          onSelectBrowser(tab.id);
        } else if (tab.kind === "project") {
          onSelectProject();
        } else if (tab.kind === "file") {
          onSelectFilePreview(tab.id);
        } else {
          onSelectCanvasItem(tab.id);
        }
      }}
    >
      <span className="relative inline-flex size-(--workspace-toolbar-tab-icon) shrink-0">
        <span className="inline-flex size-full items-center justify-center">
          {project ? (
            <span className="inline-flex size-full items-center justify-center text-current">
              <Folders className="h-3.5 w-3.5" />
            </span>
          ) : browser ? (
            <BrowserTabIcon faviconURL={browserTabFaviconURL(browser)} pageURL={browser.url} />
          ) : widget ? (
            <CanvasKindIcon className="!bg-transparent !text-current" kind={widget.kind} size="xs" />
          ) : (
            <FilePreviewTabIcon kind={file?.kind || "file"} />
          )}
        </span>
      </span>
      <span className="min-w-0 flex-1 overflow-hidden text-left whitespace-nowrap">{label}</span>
      <span
        aria-hidden="true"
        className={cn(
          "pointer-events-none absolute inset-y-0 right-0 z-[5] w-8 rounded-r-[7px]",
          selected
            ? "bg-[linear-gradient(to_right,transparent_0%,var(--workspace-tab-active-mask-background)_40%,var(--workspace-tab-active-mask-background)_100%)]"
            : "bg-[linear-gradient(to_right,transparent_0%,var(--workspace-tab-action-background)_68%,var(--workspace-tab-action-background)_100%)] [--workspace-tab-action-background:var(--workspace-chrome-background)] group-hover:bg-[linear-gradient(to_right,transparent_0%,var(--workspace-tab-action-background)_40%,var(--workspace-tab-action-background)_100%)] group-hover:[--workspace-tab-action-background:var(--workspace-tab-hover-mask-background)]",
        )}
      />
      <span
        aria-label={closeLabel}
        className={cn(
          "absolute top-[calc((var(--workspace-toolbar-tab-h)-var(--workspace-toolbar-tab-icon))/2)] right-1 z-10 inline-flex size-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[4px] bg-transparent hover:bg-foreground/14 hover:text-foreground active:bg-foreground/20",
          selected || closePending
            ? "pointer-events-auto opacity-100"
            : "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100",
        )}
        data-pending={closePending}
        role="button"
        tabIndex={-1}
        onPointerDown={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation();
          closeTab();
        }}
      >
        {closePending ? <Spinner className="h-3 w-3" /> : <X className="h-3 w-3" />}
      </span>
    </button>
  );
}

function surfaceTabID(tab: SurfaceTab) {
  return `${tab.kind}:${tab.id}`;
}
