import { BookOpen, Globe, MousePointerClick, PackagePlus, PanelsTopLeft, ScanLine } from "@/components/icons";

import { type AppDefinition } from "@/api/client";
import { AppIcon, type AppIconSpec } from "@/components/AppIcon";
import { IdentityIcon, type IdentityIconShape, type IdentityIconSize } from "@/components/IdentityIcon";

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
  if (app.id === "capture") {
    return t("apps.builtin.capture.name");
  }
  if (app.id === "computer-use") {
    return t("apps.builtin.computer-use.name" as any);
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
  if (app.id === "capture") {
    return t("apps.builtin.capture.desc");
  }
  if (app.id === "computer-use") {
    return t("apps.builtin.computer-use.desc" as any);
  }
  return app.description || "";
}

export function BuiltinAppIcon({ appID, shape = "rounded", size = "md" }: { appID: string; shape?: IdentityIconShape; size?: IdentityIconSize }) {
  const Icon = builtinAppIcon(appID);
  return (
    <IdentityIcon
      aria-hidden="true"
      className={`pudding-app-icon ${builtinAppIconClass(appID)}`}
      shape={shape}
      size={size}
    >
      <Icon className="size-[64%]" strokeWidth={2.2} />
    </IdentityIcon>
  );
}

function builtinAppIcon(appID: string) {
  if (appID === "canvas") return PanelsTopLeft;
  if (appID === "skill-authoring") return BookOpen;
  if (appID === "app-authoring") return PackagePlus;
  if (appID === "capture") return ScanLine;
  if (appID === "computer-use") return MousePointerClick;
  return Globe;
}

export function builtinAppIconClass(appID: string) {
  if (appID === "canvas") {
    return "bg-[#EEE9FF] text-[#714CF2] shadow-none ring-1 ring-inset ring-[#714CF2]/12 dark:bg-[#6C4DE6]/20 dark:text-[#B9AAFF] dark:ring-[#8A71F2]/18";
  }
  if (appID === "skill-authoring") {
    return "bg-[#F7EAF8] text-[#98409E] shadow-none ring-1 ring-inset ring-[#98409E]/12 dark:bg-[#8A3D8F]/20 dark:text-[#D899DC] dark:ring-[#A75BAC]/18";
  }
  if (appID === "app-authoring") {
    return "bg-[#E6F8F4] text-[#008C77] shadow-none ring-1 ring-inset ring-[#008C77]/12 dark:bg-[#087F6D]/20 dark:text-[#69D4C1] dark:ring-[#159B86]/18";
  }
  if (appID === "capture") {
    return "bg-[#FFF0F5] text-[#C43D70] shadow-none ring-1 ring-inset ring-[#C43D70]/12 dark:bg-[#B43A68]/20 dark:text-[#F08DB1] dark:ring-[#D65C88]/18";
  }
  if (appID === "computer-use") {
    return "bg-[#FFF2E5] text-[#D57312] shadow-none ring-1 ring-inset ring-[#D57312]/12 dark:bg-[#C76A16]/20 dark:text-[#F3A55F] dark:ring-[#E18839]/18";
  }
  return "bg-[#EAF3FF] text-[#2D7CF0] shadow-none ring-1 ring-inset ring-[#2D7CF0]/12 dark:bg-[#2672E8]/20 dark:text-[#7DB5FF] dark:ring-[#4E8DEE]/18";
}

export function AppIdentityIcon({
  app,
  icon,
  iconSrc,
  shape = "rounded",
  size = "md",
}: {
  app: AppDefinition;
  icon?: AppIconSpec;
  iconSrc?: string;
  shape?: IdentityIconShape;
  size?: IdentityIconSize;
}) {
  const resolvedIcon = icon ?? app.icon;
  return app.source === "builtin" ? (
    <BuiltinAppIcon appID={app.id} shape={shape} size={size} />
  ) : (
    <AppIcon icon={resolvedIcon} shape={shape} size={size} src={iconSrc} />
  );
}
