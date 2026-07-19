import { Globe, LayoutDashboard, Terminal } from "lucide-react";

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
  if (app.id === "terminal") {
    return t("apps.builtin.terminal.name");
  }
  if (app.id === "canvas") {
    return t("apps.builtin.canvas.name");
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
  if (app.id === "terminal") {
    return t("apps.builtin.terminal.desc");
  }
  if (app.id === "canvas") {
    return t("apps.builtin.canvas.desc");
  }
  return app.description || "";
}

export function BuiltinAppIcon({ appID, size = "md" }: { appID: string; size?: IdentityIconSize }) {
  const Icon = appID === "terminal" ? Terminal : appID === "canvas" ? LayoutDashboard : Globe;
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

export function builtinAppIconClass(appID: string) {
  if (appID === "terminal") {
    return "bg-[#ECF9F3] text-[#11875D] shadow-none dark:bg-[#1FA979]/16 dark:text-[#71D8B2]";
  }
  if (appID === "canvas") {
    return "bg-[#F4F0FF] text-[#6C4DE6] shadow-none dark:bg-[#7C5CFC]/16 dark:text-[#B7A6FF]";
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
