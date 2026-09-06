import { arrayMove } from "@dnd-kit/sortable";
import { X } from "@/components/icons";
import { useRef, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { useActiveTabVisibility } from "./useActiveTabVisibility";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { SortableTab, SortableTabList } from "./SortableTabList";
import { mergeWorkspaceTabOrder, openWorkspaceView, setWorkspaceActiveTab, setWorkspaceTabOrder, useWorkspaceActiveTab, useWorkspaceTabOrder, type WorkspaceTopTabKey, type WorkspaceTabKey } from "@/state/workspaceStore";
import { AppContextMenuContent, AppContextMenuItem, AppContextMenuSeparator } from "@/components/AppMenu";
import { ContextMenu, ContextMenuTrigger } from "@/components/ui/context-menu";

export type WorkspaceContentTab = { id: WorkspaceTopTabKey; title: string; icon: ReactNode; kind: "project" | "browser" | "canvas"; closing?: boolean };
export function WorkspaceContentTabs({ sessionID, tabs, trailingAction, actions, className, closing = false, onClose }: { sessionID: string; actions?: ReactNode; className?: string; tabs: WorkspaceContentTab[]; trailingAction?: ReactNode; closing?: boolean; onClose: (ids: WorkspaceTabKey[]) => void }) {
  const { t } = useI18n();
  const activeID = useWorkspaceActiveTab(sessionID);
  const scrollRef = useRef<HTMLDivElement>(null);
  useActiveTabVisibility(scrollRef, activeID);
  const order = useWorkspaceTabOrder(sessionID);
  const byID = new Map(tabs.map((tab) => [tab.id, tab]));
  const ids = mergeWorkspaceTabOrder(order, tabs.map((tab) => tab.id));
  const ordered = ids.map((id) => byID.get(id)!);
  const activate = (id: string) => id === "project" ? openWorkspaceView(sessionID, "project") : setWorkspaceActiveTab(sessionID, id as WorkspaceTabKey);
  const moveTab = (activeID: string, overID: string) => {
    const from = ids.indexOf(activeID as WorkspaceTopTabKey), to = ids.indexOf(overID as WorkspaceTopTabKey);
    if (from >= 0 && to >= 0) setWorkspaceTabOrder(sessionID, arrayMove(ids, from, to));
  };
  return <div className={cn("pudding-workspace-tab-strip drag-region", className)}>
    <SortableTabList ids={ids} onMove={moveTab}>
        <div ref={scrollRef} className="pudding-workspace-tabs-scroll flex h-full min-w-0 items-center gap-0.5 overflow-x-auto overscroll-x-contain">
          {ordered.map((tab, index) => <ContentTab key={tab.id} tab={tab} selected={activeID === tab.id} disabled={closing} sortable={ids.length > 1} onSelect={() => activate(tab.id)} onClose={() => onClose([tab.id])} menu={<AppContextMenuContent>
              <AppContextMenuItem disabled={closing} onSelect={() => onClose([tab.id])}>{t("workspace.closeTab")}</AppContextMenuItem>
              <AppContextMenuItem disabled={closing || ids.length < 2} onSelect={() => onClose(ids.filter((id) => id !== tab.id))}>{t("project.browserCloseOthers")}</AppContextMenuItem>
              <AppContextMenuItem disabled={closing || index === ids.length - 1} onSelect={() => onClose(ids.slice(index + 1))}>{t("project.browserCloseRight")}</AppContextMenuItem>
              <AppContextMenuSeparator />
              <AppContextMenuItem disabled={closing} onSelect={() => onClose(ids)}>{t("workspace.closeAllTabs")}</AppContextMenuItem>
            </AppContextMenuContent>} />)}
        </div>
    </SortableTabList>
    {trailingAction}
    <div className="ml-auto flex h-full shrink-0 items-center gap-1 pl-1">{actions}</div>
  </div>;
}
function ContentTab({ tab, selected, disabled, sortable, onSelect, onClose, menu }: { tab: WorkspaceContentTab; selected: boolean; disabled: boolean; sortable: boolean; onSelect: () => void; onClose: () => void; menu: ReactNode }) {
  const { t } = useI18n();
  return <ContextMenu><ContextMenuTrigger asChild><SortableTab id={tab.id} data-workspace-tab-key={tab.id} selected={selected} disabled={disabled || !sortable} className="no-drag-region w-28 shrink">{(handleProps) => <>
    <button {...handleProps} aria-label={tab.title} aria-pressed={selected} className="pudding-workspace-tab-select" type="button" onClick={onSelect}><span className="flex size-4 shrink-0 items-center justify-center">{tab.icon}</span><span className="pudding-workspace-tab-title min-w-0 flex-1 overflow-hidden whitespace-nowrap">{tab.title}</span></button>
    <button aria-label={`${t("workspace.closeTab")} ${tab.title}`} className="pudding-workspace-tab-close" disabled={disabled || tab.closing} type="button" onClick={onClose}>{tab.closing ? <Spinner className="size-3" /> : <X className="size-3" />}</button>
  </>}</SortableTab></ContextMenuTrigger>{menu}</ContextMenu>;
}
