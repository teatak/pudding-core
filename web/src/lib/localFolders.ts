import { newClientID } from "@/lib/id";

export type LocalFolderPath = {
  id: string;
  name: string;
  path: string;
};

export type DroppedLocalItems = {
  files: File[];
  folderPathUnavailable: boolean;
  folderPaths: string[];
};

export function createLocalFolderPath(path: string): LocalFolderPath | null {
  const clean = path.trim();
  if (!clean) {
    return null;
  }
  return {
    id: newClientID(),
    name: localFolderName(clean),
    path: clean,
  };
}

export async function pickLocalFolderPaths(t: (key: string) => string) {
  const { Dialogs } = await import("@wailsio/runtime");
  const result = await Dialogs.OpenFile({
    AllowsMultipleSelection: true,
    ButtonText: t("composer.folderPickButton"),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    CanCreateDirectories: false,
    Message: t("composer.folderPickMessage"),
    Title: t("composer.folderPickTitle"),
  });
  const paths = Array.isArray(result) ? result : result ? [result] : [];
  return dedupeStrings(paths.map((path) => path.trim()).filter(Boolean));
}

export function droppedLocalItemsFromDataTransfer(dataTransfer: DataTransfer): DroppedLocalItems {
  const files: File[] = [];
  const folderPaths: string[] = [];
  let folderPathUnavailable = false;
  const uriPaths = parseFileURIList(dataTransfer.getData("text/uri-list"));
  const items = Array.from(dataTransfer.items || []);

  if (items.length > 0) {
    for (const item of items) {
      if (item.kind !== "file") {
        continue;
      }
      const entry = (item as WebkitDataTransferItem).webkitGetAsEntry?.();
      if (entry?.isDirectory) {
        const path = directoryPathFromDroppedItem(item, entry, uriPaths);
        if (path) {
          folderPaths.push(path);
        } else {
          folderPathUnavailable = true;
        }
        continue;
      }
      const file = item.getAsFile();
      if (file) {
        files.push(file);
      }
    }
  } else {
    files.push(...Array.from(dataTransfer.files || []));
  }

  return {
    files,
    folderPathUnavailable,
    folderPaths: dedupeStrings(folderPaths),
  };
}

export function formatLocalFolderLabel(path: string) {
  const normalized = path.trim().replace(/\\/g, "/");
  const parts = normalized.split("/").filter(Boolean);
  if (parts.length <= 3) {
    return path;
  }
  return `.../${parts.slice(-3).join("/")}`;
}

function localFolderName(path: string) {
  const normalized = path.trim().replace(/\\/g, "/").replace(/\/+$/, "");
  const parts = normalized.split("/").filter(Boolean);
  return parts.at(-1) || normalized || path;
}

function directoryPathFromDroppedItem(item: DataTransferItem, entry: WebkitFileSystemEntry, uriPaths: string[]) {
  const filePath = nativePath((item.getAsFile?.() || null) as NativePathFile | null);
  if (filePath) {
    return filePath;
  }
  const matchingURIPath = uriPaths.find((path) => localFolderName(path) === entry.name);
  return matchingURIPath || "";
}

function nativePath(file: NativePathFile | null) {
  const path = typeof file?.path === "string" ? file.path.trim() : "";
  if (/^\/[^/]/.test(path) || /^[A-Za-z]:[\\/]/.test(path)) {
    return path;
  }
  return "";
}

function parseFileURIList(raw: string) {
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"))
    .flatMap((line) => {
      try {
        const url = new URL(line);
        if (url.protocol !== "file:") {
          return [];
        }
        return [decodeURIComponent(url.pathname)];
      } catch {
        return [];
      }
    });
}

function dedupeStrings(values: string[]) {
  return Array.from(new Set(values));
}

type NativePathFile = File & { path?: string };

type WebkitDataTransferItem = DataTransferItem & {
  webkitGetAsEntry?: () => WebkitFileSystemEntry | null;
};

type WebkitFileSystemEntry = {
  isDirectory?: boolean;
  name?: string;
};
