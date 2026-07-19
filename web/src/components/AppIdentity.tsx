import { Blocks, Compass, SquareTerminal } from "lucide-react";

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
  const Icon = appID === "terminal" ? SquareTerminal : appID === "canvas" ? Blocks : Compass;
  return (
    <IdentityIcon
      aria-hidden="true"
      className={`pudding-app-icon ${builtinAppIconClass(appID)}`}
      data-light-background="true"
      size={size}
    >
      <Icon className="size-[68%]" strokeWidth={2.15} />
    </IdentityIcon>
  );
}

export function builtinAppIconClass(appID: string) {
  if (appID === "terminal") {
    return "bg-emerald-50 text-emerald-700 shadow-none dark:bg-emerald-400/15 dark:text-emerald-300";
  }
  if (appID === "canvas") {
    return "bg-violet-50 text-violet-700 shadow-none dark:bg-violet-400/15 dark:text-violet-300";
  }
  return "bg-blue-50 text-blue-700 shadow-none dark:bg-blue-400/15 dark:text-blue-300";
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
