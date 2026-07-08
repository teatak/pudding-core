type DirectoryPickerOptions = {
  buttonLabel?: string;
  message?: string;
  title?: string;
};

type ElectronDesktopBridge = {
  getDroppedFilePath?: (file: File) => string;
  openExternal: (url: string) => Promise<boolean>;
  pickDirectories: (options?: DirectoryPickerOptions) => Promise<string[]>;
};

type NativePathFile = File & { path?: string };

declare global {
  interface Window {
    puddingElectronDesktop?: ElectronDesktopBridge;
  }
}

export async function openExternalURL(url: string) {
  const clean = url.trim();
  if (!clean) {
    return;
  }
  const bridge = desktopBridge();
  if (bridge) {
    try {
      if (await bridge.openExternal(clean)) {
        return;
      }
    } catch {
      // Fall through to browser fallback.
    }
  }
  window.open(clean, "_blank", "noopener,noreferrer");
}

export function getDroppedFilePath(file: File) {
  const bridge = desktopBridge();
  if (!bridge?.getDroppedFilePath) {
    return "";
  }
  try {
    return bridge.getDroppedFilePath(file).trim();
  } catch {
    return "";
  }
}

export function getLocalFilePath(file: File | null) {
  const nativeFile = file as NativePathFile | null;
  const path = (file ? getDroppedFilePath(file) : "") || (typeof nativeFile?.path === "string" ? nativeFile.path.trim() : "");
  if (/^\/[^/]/.test(path) || /^[A-Za-z]:[\\/]/.test(path)) {
    return path;
  }
  return "";
}

export async function pickDirectories(options?: DirectoryPickerOptions) {
  const bridge = desktopBridge();
  if (!bridge) {
    return [];
  }
  try {
    return await bridge.pickDirectories(options);
  } catch {
    return [];
  }
}

function desktopBridge() {
  return typeof window === "undefined" ? undefined : window.puddingElectronDesktop;
}
