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
import { Compass, FileCode2, FileDiff, SquareTerminal, X } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

import type { BrowserTab, Terminal } from "@/api/client";
import { browserTabFaviconURL, browserTabTitle } from "@/browser/helpers";
import { builtinAppIconClass } from "@/components/AppIdentity";
import { workspaceTabActiveClassName, workspaceTabClassName } from "@/components/workspace/WorkspaceSurfaceControls";
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

function BrowserTabIcon({ faviconURL }: { faviconURL?: string }) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [faviconURL]);

  if (faviconURL && !failed) {
    return (
      <span aria-hidden="true" className="inline-flex h-(--workspace-toolbar-tab-icon) w-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center overflow-hidden rounded-[5px]">
        <img alt="" className="h-full w-full object-cover" draggable={false} src={faviconURL} onError={() => setFailed(true)} />
      </span>
    );
  }

  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex h-(--workspace-toolbar-tab-icon) w-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px]",
        builtinAppIconClass("browser"),
      )}
    >
      <Compass className="h-3.5 w-3.5" />
    </span>
  );
}

function TerminalTabIcon({ exited }: { exited: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex h-(--workspace-toolbar-tab-icon) w-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px]",
        builtinAppIconClass("terminal"),
      )}
      data-exited={exited}
    >
      <SquareTerminal className="h-3.5 w-3.5" />
    </span>
  );
}

function FilePreviewTabIcon({ kind }: { kind: WorkspaceFilePreviewTab["kind"] }) {
  return (
    <span
      aria-hidden="true"
      className="inline-flex h-(--workspace-toolbar-tab-icon) w-(--workspace-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px] bg-blue-50 text-blue-700 dark:bg-blue-400/15 dark:text-blue-300"
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
  | { kind: "browser"; id: string; sortAt: number; browser: BrowserTab }
  | { kind: "terminal"; id: string; sortAt: number; terminal: Terminal }
  | { kind: "file"; id: string; sortAt: number; file: WorkspaceFilePreviewTab };

export function WorkspaceResourceTabs({
  activeBrowserTabID,
  activeFilePreviewID,
  activeSurface,
  activeTerminalID,
  browserTabs,
  closingBrowserTabID,
  closingTerminalID,
  filePreviewActive,
  filePreviewTabs,
  leadingTabs,
  orderScope,
  terminalTabs,
  onCloseBrowser,
  onCloseFilePreview,
  onCloseTerminal,
  onSelectBrowser,
  onSelectFilePreview,
  onSelectTerminal,
}: {
  activeBrowserTabID?: string;
  activeFilePreviewID?: string;
  activeSurface: WorkspaceSurface;
  activeTerminalID?: string;
  browserTabs: BrowserTab[];
  closingBrowserTabID?: string;
  closingTerminalID?: string;
  filePreviewActive: boolean;
  filePreviewTabs: WorkspaceFilePreviewTab[];
  leadingTabs?: ReactNode;
  orderScope: string;
  terminalTabs: Terminal[];
  onCloseBrowser: (tabID: string) => void;
  onCloseFilePreview: (previewID: string) => void;
  onCloseTerminal: (terminalID: string) => void;
  onSelectBrowser: (tabID: string) => void;
  onSelectFilePreview: (previewID: string) => void;
  onSelectTerminal: (terminalID: string) => void;
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
    ...terminalTabs.map((terminal) => ({ kind: "terminal" as const, id: terminal.id, sortAt: Date.parse(terminal.createdAt), terminal })),
    ...filePreviewTabs.map((file) => ({ kind: "file" as const, id: file.id, sortAt: file.openedAt, file })),
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
          <div className="flex w-fit max-w-full min-w-0 items-center gap-0.5">
            {leadingTabs}
            {tabs.map((tab) => (
              <SortableSurfaceTabButton
                key={surfaceTabID(tab)}
                activeBrowserTabID={activeBrowserTabID}
                activeFilePreviewID={activeFilePreviewID}
                activeSurface={activeSurface}
                activeTerminalID={activeTerminalID}
                closingBrowserTabID={closingBrowserTabID}
                closingTerminalID={closingTerminalID}
                filePreviewActive={filePreviewActive}
                tab={tab}
                onCloseBrowser={onCloseBrowser}
                onCloseFilePreview={onCloseFilePreview}
                onCloseTerminal={onCloseTerminal}
                onSelectBrowser={onSelectBrowser}
                onSelectFilePreview={onSelectFilePreview}
                onSelectTerminal={onSelectTerminal}
              />
            ))}
          </div>
        </SortableContext>
      </div>
    </DndContext>
  );
}

function SortableSurfaceTabButton({
  activeBrowserTabID,
  activeFilePreviewID,
  activeSurface,
  activeTerminalID,
  closingBrowserTabID,
  closingTerminalID,
  filePreviewActive,
  tab,
  onCloseBrowser,
  onCloseFilePreview,
  onCloseTerminal,
  onSelectBrowser,
  onSelectFilePreview,
  onSelectTerminal,
}: {
  activeBrowserTabID?: string;
  activeFilePreviewID?: string;
  activeSurface: WorkspaceSurface;
  activeTerminalID?: string;
  closingBrowserTabID?: string;
  closingTerminalID?: string;
  filePreviewActive: boolean;
  tab: SurfaceTab;
  onCloseBrowser: (tabID: string) => void;
  onCloseFilePreview: (previewID: string) => void;
  onCloseTerminal: (terminalID: string) => void;
  onSelectBrowser: (tabID: string) => void;
  onSelectFilePreview: (previewID: string) => void;
  onSelectTerminal: (terminalID: string) => void;
}) {
  const { t } = useI18n();
  const browser = tab.kind === "browser" ? tab.browser : undefined;
  const terminal = tab.kind === "terminal" ? tab.terminal : undefined;
  const file = tab.kind === "file" ? tab.file : undefined;
  const label = browser
    ? browserTabTitle(browser, t("browser.newTab"), t("browser.newTab"))
    : terminal
      ? terminalTabTitle(terminal, t("terminal.newTab"))
      : file?.label || t("terminal.newTab");
  const selected =
    (tab.kind === "browser" && activeSurface === "browser" && tab.id === activeBrowserTabID) ||
    (tab.kind === "terminal" && activeSurface === "terminal" && tab.id === activeTerminalID) ||
    (tab.kind === "file" && filePreviewActive && tab.id === activeFilePreviewID);
  const closePending =
    (tab.kind === "browser" && tab.id === closingBrowserTabID) ||
    (tab.kind === "terminal" && tab.id === closingTerminalID);
  const exited = terminal?.status === "exited";
  const { attributes, isDragging, listeners, setNodeRef, transform, transition } = useSortable({
    id: surfaceTabID(tab),
    disabled: closePending,
  });
  const horizontalTransform = transform ? { ...transform, y: 0 } : null;

  return (
    <button
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      aria-label={label}
      aria-selected={selected}
      className={cn(
        workspaceTabClassName,
        "group relative w-36 min-w-24 max-w-none shrink pr-6 pl-2 whitespace-nowrap",
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
      title={exited ? `${label} · ${t("terminal.exited")}` : file?.path || label}
      type="button"
      onClick={() => {
        if (tab.kind === "browser") {
          onSelectBrowser(tab.id);
        } else if (tab.kind === "terminal") {
          onSelectTerminal(tab.id);
        } else {
          onSelectFilePreview(tab.id);
        }
      }}
    >
      {browser ? (
        <BrowserTabIcon faviconURL={browserTabFaviconURL(browser)} />
      ) : terminal ? (
        <TerminalTabIcon exited={exited} />
      ) : (
        <FilePreviewTabIcon kind={file?.kind || "file"} />
      )}
      <span className="min-w-0 max-w-24 flex-1 truncate text-left">{label}</span>
      <span
        aria-label={
          tab.kind === "browser"
            ? t("browser.release")
            : tab.kind === "terminal"
              ? t("terminal.close")
              : t("canvas.filePreviewClose")
        }
        className="pointer-events-none absolute right-1 top-1/2 z-10 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded-full bg-transparent opacity-0 transition-colors hover:bg-accent group-hover:pointer-events-auto group-hover:opacity-100 dark:hover:bg-[#474747] data-[pending=true]:pointer-events-auto data-[pending=true]:opacity-100"
        data-pending={closePending}
        role="button"
        tabIndex={-1}
        onPointerDown={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation();
          if (tab.kind === "browser") {
            onCloseBrowser(tab.id);
          } else if (tab.kind === "terminal") {
            onCloseTerminal(tab.id);
          } else {
            onCloseFilePreview(tab.id);
          }
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

function terminalTabTitle(terminal: Terminal, fallback: string) {
  const title = (terminal.title || "").trim();
  const shellName = basename(terminal.shell);
  if (title && title !== shellName) {
    return title;
  }
  return basename(terminal.cwd) || title || fallback;
}

function basename(path: string) {
  return path.replace(/[/\\]+$/, "").split(/[/\\]/).pop()?.trim() || "";
}
