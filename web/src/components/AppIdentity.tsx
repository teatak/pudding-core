import { BookOpen, FolderTree, GitFork, Globe, PackagePlus, PanelsTopLeft, ScanLine, SearchCode } from "lucide-react";

import { type AppDefinition } from "@/api/client";
import { AppIcon, type AppIconSpec } from "@/components/AppIcon";
import { IdentityIcon, type IdentityIconSize } from "@/components/IdentityIcon";

type Translate = (key: string) => string;

export function appDisplayName(app: AppDefinition, t: Translate) {
  if (app.source !== "builtin") {
    return app.name;
  }
  if (app.id === "browser") {
    return t("apps.builtin.browser.name");
  }
  if (app.id === "canvas") {
    return t("apps.builtin.canvas.name");
  }
  if (app.id === "skill-authoring") {
    return t("apps.builtin.skillAuthoring.name");
  }
  if (app.id === "app-authoring") {
    return t("apps.builtin.appAuthoring.name");
  }
  if (app.id === "project-files") {
    return t("apps.builtin.projectFiles.name");
  }
  if (app.id === "source-control") {
    return t("apps.builtin.sourceControl.name");
  }
  if (app.id === "code-intelligence") {
    return t("apps.builtin.codeIntelligence.name");
  }
  if (app.id === "capture") {
    return t("apps.builtin.capture.name");
  }
  return app.name;
}

export function appDisplayDescription(app: AppDefinition, t: Translate) {
  if (app.source !== "builtin") {
    return app.description || "";
  }
  if (app.id === "browser") {
    return t("apps.builtin.browser.desc");
  }
  if (app.id === "canvas") {
    return t("apps.builtin.canvas.desc");
  }
  if (app.id === "skill-authoring") {
    return t("apps.builtin.skillAuthoring.desc");
  }
  if (app.id === "app-authoring") {
    return t("apps.builtin.appAuthoring.desc");
  }
  if (app.id === "project-files") {
    return t("apps.builtin.projectFiles.desc");
  }
  if (app.id === "source-control") {
    return t("apps.builtin.sourceControl.desc");
  }
  if (app.id === "code-intelligence") {
    return t("apps.builtin.codeIntelligence.desc");
  }
  if (app.id === "capture") {
    return t("apps.builtin.capture.desc");
  }
  return app.description || "";
}

export function BuiltinAppIcon({ appID, size = "md" }: { appID: string; size?: IdentityIconSize }) {
  const Icon = builtinAppIcon(appID);
  return (
    <IdentityIcon
      aria-hidden="true"
      className={`pudding-app-icon ${builtinAppIconClass(appID)}`}
      data-light-background="true"
      size={size}
    >
      <Icon className="size-[56%]" strokeWidth={2} />
    </IdentityIcon>
  );
}

function builtinAppIcon(appID: string) {
  if (appID === "canvas") return PanelsTopLeft;
  if (appID === "skill-authoring") return BookOpen;
  if (appID === "app-authoring") return PackagePlus;
  if (appID === "project-files") return FolderTree;
  if (appID === "source-control") return GitFork;
  if (appID === "code-intelligence") return SearchCode;
  if (appID === "capture") return ScanLine;
  return Globe;
}

export function builtinAppIconClass(appID: string) {
  if (appID === "canvas") {
    return "bg-[#F4F0FF] text-[#6C4DE6] shadow-none dark:bg-[#7C5CFC]/16 dark:text-[#B7A6FF]";
  }
  if (appID === "skill-authoring") {
    return "bg-[#F3F0FF] text-[#6D4AFF] shadow-none dark:bg-[#6D4AFF]/16 dark:text-[#B8A7FF]";
  }
  if (appID === "app-authoring") {
    return "bg-[#EAF8F4] text-[#087F6D] shadow-none dark:bg-[#16A085]/16 dark:text-[#63D6C2]";
  }
  if (appID === "project-files") {
    return "bg-[#FFF7E6] text-[#A45B00] shadow-none dark:bg-[#D88916]/16 dark:text-[#F4B95F]";
  }
  if (appID === "source-control") {
    return "bg-[#EDF7EE] text-[#247A38] shadow-none dark:bg-[#3AA657]/16 dark:text-[#79D58F]";
  }
  if (appID === "code-intelligence") {
    return "bg-[#EEF2FF] text-[#405CC9] shadow-none dark:bg-[#5874DD]/16 dark:text-[#91A5F2]";
  }
  if (appID === "capture") {
    return "bg-[#FFF0F5] text-[#B43A68] shadow-none dark:bg-[#D34C7E]/16 dark:text-[#F08DB1]";
  }
  return "bg-[#EEF6FF] text-[#2672E8] shadow-none dark:bg-[#2672E8]/16 dark:text-[#7DB5FF]";
}

export function AppIdentityIcon({
  app,
  icon,
  iconSrc,
  size = "md",
}: {
  app: AppDefinition;
  icon?: AppIconSpec;
  iconSrc?: string;
  size?: IdentityIconSize;
}) {
  const resolvedIcon = icon ?? app.icon;
  return app.source === "builtin" ? (
    <BuiltinAppIcon appID={app.id} size={size} />
  ) : (
    <AppIcon icon={resolvedIcon} size={size} src={iconSrc} />
  );
}
