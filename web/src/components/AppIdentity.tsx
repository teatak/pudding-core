import { BookOpen, Globe, MousePointerClick, PackagePlus, PanelsTopLeft, ScanLine } from "@/components/icons";

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

export function BuiltinAppIcon({ appID, size = "md" }: { appID: string; size?: IdentityIconSize }) {
  const Icon = builtinAppIcon(appID);
  return (
    <IdentityIcon
      aria-hidden="true"
      className={`pudding-app-icon ${builtinAppIconClass(appID)}`}
      size={size}
    >
      <Icon className="size-[64%]" />
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
    return "bg-[#6C4DE6] text-white shadow-none";
  }
  if (appID === "skill-authoring") {
    return "bg-[#8A3D8F] text-white shadow-none";
  }
  if (appID === "app-authoring") {
    return "bg-[#087F6D] text-white shadow-none";
  }
  if (appID === "capture") {
    return "bg-[#B43A68] text-white shadow-none";
  }
  if (appID === "computer-use") {
    return "bg-[#C76A16] text-white shadow-none";
  }
  return "bg-[#2672E8] text-white shadow-none";
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
