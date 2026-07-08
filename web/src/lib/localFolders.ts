import { newClientID } from "@/lib/id";
import { getLocalFilePath, pickDirectories } from "@/lib/desktopBridge";

export type LocalFolderPath = {
  id: string;
  name: string;
  path: string;
};

export type DroppedLocalItems = {
  files: File[];
  fileSourcePaths: string[];
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
  const paths = await pickDirectories({
    buttonLabel: t("composer.folderPickButton"),
    message: t("composer.folderPickMessage"),
    title: t("composer.folderPickTitle"),
  });
  return dedupeStrings(paths.map((path) => path.trim()).filter(Boolean));
}

export function droppedLocalItemsFromDataTransfer(dataTransfer: DataTransfer): DroppedLocalItems {
  const files: File[] = [];
  const fileSourcePaths: string[] = [];
  const folderPaths: string[] = [];
  let folderPathUnavailable = false;
  const uriPaths = parseFileURIList(dataTransfer.getData("text/uri-list"));
  const items = Array.from(dataTransfer.items || []);
  const droppedFiles = Array.from(dataTransfer.files || []);

  if (items.length > 0) {
    for (const item of items) {
      if (item.kind !== "file") {
        continue;
      }
      const entry = (item as WebkitDataTransferItem).webkitGetAsEntry?.();
      if (entry?.isDirectory) {
        const path = directoryPathFromDroppedItem(item, entry, uriPaths, droppedFiles);
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
        fileSourcePaths.push(getLocalFilePath(file));
      }
    }
  } else {
    for (const file of Array.from(dataTransfer.files || [])) {
      files.push(file);
      fileSourcePaths.push(getLocalFilePath(file));
    }
  }

  return {
    files,
    fileSourcePaths,
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

function directoryPathFromDroppedItem(
  item: DataTransferItem,
  entry: WebkitFileSystemEntry,
  uriPaths: string[],
  droppedFiles: File[],
) {
  const filePath = nativePath(item.getAsFile?.() || null);
  if (filePath) {
    return filePath;
  }
  const matchingDroppedFilePath = nativePath(droppedFiles.find((file) => file.name === entry.name) || null);
  if (matchingDroppedFilePath) {
    return matchingDroppedFilePath;
  }
  const matchingURIPath = uriPaths.find((path) => localFolderName(path) === entry.name);
  return matchingURIPath || "";
}

function nativePath(file: File | null) {
  return getLocalFilePath(file);
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

type WebkitDataTransferItem = DataTransferItem & {
  webkitGetAsEntry?: () => WebkitFileSystemEntry | null;
};

type WebkitFileSystemEntry = {
  isDirectory?: boolean;
  name?: string;
};
