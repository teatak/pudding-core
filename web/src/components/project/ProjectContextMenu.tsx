import { FilePlus2, FolderPlus, LocateFixed, SquareTerminal, Trash2 } from "lucide-react";
import type { ReactNode } from "react";

import {
  AppContextMenuContent as ProjectMenuContent,
  AppContextMenuItem as ProjectMenuItem,
  AppContextMenuSeparator as ContextMenuSeparator,
} from "@/components/AppMenu";

import {
  ContextMenu,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useI18n } from "@/i18n";

import type { ProjectEntryTarget, ProjectTab } from "./types";

export function ProjectEntryContextMenu({
  children,
  isRoot = false,
  target,
  canPaste,
  onCopyAbsolutePath,
  onCopyEntry,
  onCopyPath,
  onCreate,
  onCutEntry,
  onDelete,
  onDuplicate,
  onOpenTerminal,
  onPaste,
  onReference,
  onRename,
  onRevealInFinder,
}: {
  children: ReactNode;
  isRoot?: boolean;
  target: ProjectEntryTarget;
  canPaste: boolean;
  onCopyAbsolutePath: (target: ProjectEntryTarget) => void;
  onCopyEntry: (target: ProjectEntryTarget) => void;
  onCopyPath: (target: ProjectEntryTarget) => void;
  onCreate: (target: ProjectEntryTarget, type: "dir" | "file") => void;
  onCutEntry: (target: ProjectEntryTarget) => void;
  onDelete: (target: ProjectEntryTarget) => void;
  onDuplicate: (target: ProjectEntryTarget) => void;
  onOpenTerminal: (target: ProjectEntryTarget) => void;
  onPaste: (target: ProjectEntryTarget) => void;
  onReference: (target: ProjectEntryTarget) => void;
  onRename: (target: ProjectEntryTarget) => void;
  onRevealInFinder: (target: ProjectEntryTarget) => void;
}) {
  const { t } = useI18n();
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ProjectMenuContent>
        <ProjectMenuItem onSelect={() => onReference(target)}>{t("project.browserReferenceSession")}</ProjectMenuItem>
        <ContextMenuSeparator />
        {target.type === "dir" ? (
          <>
            <ProjectMenuItem onSelect={() => onCreate(target, "file")}><FilePlus2 />{t("project.browserNewFile")}</ProjectMenuItem>
            <ProjectMenuItem onSelect={() => onCreate(target, "dir")}><FolderPlus />{t("project.browserNewFolder")}</ProjectMenuItem>
            <ContextMenuSeparator />
          </>
        ) : null}
        {!isRoot ? (
          <>
            <ProjectMenuItem onSelect={() => onCopyEntry(target)}>{t("project.browserCopyEntry")}</ProjectMenuItem>
            <ProjectMenuItem onSelect={() => onCutEntry(target)}>{t("project.browserCutEntry")}</ProjectMenuItem>
            <ProjectMenuItem onSelect={() => onDuplicate(target)}>{t("project.browserDuplicateEntry")}</ProjectMenuItem>
          </>
        ) : null}
        {target.type === "dir" ? (
          <ProjectMenuItem disabled={!canPaste} onSelect={() => onPaste(target)}>{t("project.browserPasteEntry")}</ProjectMenuItem>
        ) : null}
        {!isRoot ? <ProjectMenuItem onSelect={() => onRename(target)}>{t("common.rename")}</ProjectMenuItem> : null}
        <ContextMenuSeparator />
        <ProjectMenuItem onSelect={() => onRevealInFinder(target)}>{t("project.browserRevealFinder")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onOpenTerminal(target)}><SquareTerminal />{t("project.browserOpenTerminal")}</ProjectMenuItem>
        <ContextMenuSeparator />
        <ProjectMenuItem onSelect={() => onCopyPath(target)}>{t("project.browserCopyRelativePath")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onCopyAbsolutePath(target)}>{t("project.browserCopyAbsolutePath")}</ProjectMenuItem>
        {!isRoot ? (
          <>
            <ContextMenuSeparator />
            <ProjectMenuItem variant="destructive" onSelect={() => onDelete(target)}><Trash2 />{t("common.delete")}</ProjectMenuItem>
          </>
        ) : null}
      </ProjectMenuContent>
    </ContextMenu>
  );
}

export function ProjectTabContextMenu({
  children,
  tab,
  onClose,
  onCloseOthers,
  onCloseRight,
  onReveal,
}: {
  children: ReactNode;
  tab: ProjectTab;
  onClose: (tab: ProjectTab) => void;
  onCloseOthers: (tab: ProjectTab) => void;
  onCloseRight: (tab: ProjectTab) => void;
  onReveal: (tab: ProjectTab) => void;
}) {
  const { t } = useI18n();
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ProjectMenuContent>
        <ProjectMenuItem onSelect={() => onClose(tab)}>{t("project.browserCloseTab")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onCloseOthers(tab)}>{t("project.browserCloseOthers")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onCloseRight(tab)}>{t("project.browserCloseRight")}</ProjectMenuItem>
        <ContextMenuSeparator />
        <ProjectMenuItem onSelect={() => onReveal(tab)}><LocateFixed />{t("project.browserRevealInTree")}</ProjectMenuItem>
      </ProjectMenuContent>
    </ContextMenu>
  );
}
