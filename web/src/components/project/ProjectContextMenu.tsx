import {
  Copy,
  CopyX,
  FilePlus2,
  FolderPlus,
  LocateFixed,
  PanelRightClose,
  Pencil,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import type { ProjectEntryTarget, ProjectTab } from "./types";

function ProjectMenuContent({ className, ...props }: ComponentProps<typeof ContextMenuContent>) {
  return (
    <ContextMenuContent
      collisionPadding={8}
      className={cn("min-w-36 w-max", className)}
      {...props}
    />
  );
}

function ProjectMenuItem({ className, ...props }: ComponentProps<typeof ContextMenuItem>) {
  return <ContextMenuItem className={cn("whitespace-nowrap", className)} {...props} />;
}

export function ProjectEntryContextMenu({
  children,
  isRoot = false,
  target,
  onCopyAbsolutePath,
  onCopyPath,
  onCreate,
  onDelete,
  onRefresh,
  onRename,
}: {
  children: ReactNode;
  isRoot?: boolean;
  target: ProjectEntryTarget;
  onCopyAbsolutePath: (target: ProjectEntryTarget) => void;
  onCopyPath: (target: ProjectEntryTarget) => void;
  onCreate: (target: ProjectEntryTarget, type: "dir" | "file") => void;
  onDelete: (target: ProjectEntryTarget) => void;
  onRefresh: (target: ProjectEntryTarget) => void;
  onRename: (target: ProjectEntryTarget) => void;
}) {
  const { t } = useI18n();
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ProjectMenuContent>
        {target.type === "dir" ? (
          <>
            <ProjectMenuItem onSelect={() => onCreate(target, "file")}><FilePlus2 />{t("project.browserNewFile")}</ProjectMenuItem>
            <ProjectMenuItem onSelect={() => onCreate(target, "dir")}><FolderPlus />{t("project.browserNewFolder")}</ProjectMenuItem>
            <ContextMenuSeparator />
          </>
        ) : null}
        {!isRoot ? (
          <ProjectMenuItem onSelect={() => onRename(target)}><Pencil />{t("common.rename")}</ProjectMenuItem>
        ) : null}
        <ProjectMenuItem onSelect={() => onCopyPath(target)}><Copy />{t("project.browserCopyRelativePath")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onCopyAbsolutePath(target)}><Copy />{t("project.browserCopyAbsolutePath")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onRefresh(target)}><RefreshCw />{t("common.refresh")}</ProjectMenuItem>
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
        <ProjectMenuItem onSelect={() => onClose(tab)}><X />{t("project.browserCloseTab")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onCloseOthers(tab)}><CopyX />{t("project.browserCloseOthers")}</ProjectMenuItem>
        <ProjectMenuItem onSelect={() => onCloseRight(tab)}><PanelRightClose />{t("project.browserCloseRight")}</ProjectMenuItem>
        <ContextMenuSeparator />
        <ProjectMenuItem onSelect={() => onReveal(tab)}><LocateFixed />{t("project.browserRevealInTree")}</ProjectMenuItem>
      </ProjectMenuContent>
    </ContextMenu>
  );
}
