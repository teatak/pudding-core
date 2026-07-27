import { File, Folder, FolderOpen } from "@/components/icons";
import { useTheme } from "next-themes";
import { useSyncExternalStore, type ReactNode } from "react";

import { cn } from "@/lib/utils";

type IconLookup = Record<string, string>;
type LookupName = "fileExtensions" | "fileNames" | "folderNames" | "folderNamesExpanded";

type MaterialIconManifest = Record<LookupName, IconLookup> & {
  file: string;
  folder: string;
  folderExpanded: string;
  light?: Partial<Record<LookupName, IconLookup>>;
};

type MaterialIconBundle = {
  icons: Record<string, { markup: string }>;
  manifest: MaterialIconManifest;
};

export function ProjectFileTypeIcon({ className, path }: { className?: string; path: string }) {
  const resources = useMaterialIconTheme();
  const { resolvedTheme } = useTheme();
  const iconName = resources ? resolveFileIcon(resources.manifest, path, resolvedTheme === "light") : undefined;
  return (
    <ProjectEntrySvg
      className={className}
      fallback={<File />}
      iconName={iconName}
      resources={resources}
    />
  );
}

export function ProjectFolderTypeIcon({
  className,
  name,
  open = false,
}: {
  className?: string;
  name: string;
  open?: boolean;
}) {
  const resources = useMaterialIconTheme();
  const { resolvedTheme } = useTheme();
  const iconName = resources
    ? resolveFolderIcon(resources.manifest, name, open, resolvedTheme === "light")
    : undefined;
  return (
    <ProjectEntrySvg
      className={className}
      fallback={open ? <FolderOpen /> : <Folder />}
      iconName={iconName}
      resources={resources}
    />
  );
}

function ProjectEntrySvg({
  className,
  fallback,
  iconName,
  resources,
}: {
  className?: string;
  fallback: ReactNode;
  iconName?: string;
  resources?: MaterialIconBundle;
}) {
  const icon = iconName ? resources?.icons[iconName] : undefined;
  if (!icon) {
    return <span className={cn("inline-flex size-4 shrink-0 [&>svg]:size-4", className)}>{fallback}</span>;
  }
  return (
    <span
      aria-hidden="true"
      className={cn("inline-flex size-4 shrink-0 [&>svg]:size-full", className)}
      dangerouslySetInnerHTML={{ __html: icon.markup }}
    />
  );
}

function resolveFileIcon(manifest: MaterialIconManifest, path: string, light: boolean) {
  const normalizedPath = path.replaceAll("\\", "/").replace(/^\.\/+/, "").toLowerCase();
  const filename = normalizedPath.split("/").pop() || normalizedPath;
  const namedIcon = lookupIcon(manifest, "fileNames", normalizedPath, light)
    || lookupIcon(manifest, "fileNames", filename, light);
  if (namedIcon) return namedIcon;

  const parts = filename.split(".");
  const candidates = [filename, ...parts.slice(1).map((_, index) => parts.slice(index + 1).join("."))];
  for (const candidate of candidates) {
    const icon = lookupIcon(manifest, "fileExtensions", candidate, light);
    if (icon) return icon;
  }
  return manifest.file;
}

function resolveFolderIcon(manifest: MaterialIconManifest, name: string, open: boolean, light: boolean) {
  const key = name.toLowerCase();
  const lookupName = open ? "folderNamesExpanded" : "folderNames";
  return lookupIcon(manifest, lookupName, key, light)
    || (open ? manifest.folderExpanded : manifest.folder);
}

function lookupIcon(manifest: MaterialIconManifest, lookupName: LookupName, key: string, light: boolean) {
  return (light ? manifest.light?.[lookupName]?.[key] : undefined) || manifest[lookupName][key];
}

let loadedTheme: MaterialIconBundle | undefined;
let loadingTheme: Promise<void> | undefined;
const themeListeners = new Set<() => void>();

function useMaterialIconTheme() {
  return useSyncExternalStore(subscribeToTheme, getThemeSnapshot, getThemeSnapshot);
}

function subscribeToTheme(listener: () => void) {
  themeListeners.add(listener);
  if (!loadedTheme && !loadingTheme) {
    loadingTheme = import("virtual:material-icon-theme")
      .then((module) => {
        loadedTheme = module.default as MaterialIconBundle;
        themeListeners.forEach((notify) => notify());
      })
      .catch(() => undefined)
      .finally(() => {
        loadingTheme = undefined;
      });
  }
  return () => themeListeners.delete(listener);
}

function getThemeSnapshot() {
  return loadedTheme;
}
