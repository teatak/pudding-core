type DirectoryPickerOptions = {
  buttonLabel?: string;
  message?: string;
  title?: string;
};

export type DesktopMenuCommand = "new-session" | "search-sessions" | "settings";
export type DesktopEditorCommand = "copy" | "cut" | "delete" | "paste" | "pasteAndMatchStyle" | "redo" | "selectAll" | "undo";

export type DesktopEditorContextMenuRequest = {
  canCopy: boolean;
  canCut: boolean;
  canDelete: boolean;
  canRedo: boolean;
  canSelectAll: boolean;
  canUndo: boolean;
  selectionText: string;
};

export type DesktopUpdateState = {
  status: "unavailable" | "idle" | "checking" | "downloading" | "downloaded" | "installing";
  receivePreviewUpdates: boolean;
  version: string;
  percent: number | null;
};

type ElectronDesktopBridge = {
  getDroppedFilePath?: (file: File) => string;
  getUpdateState?: () => Promise<DesktopUpdateState>;
  setPreviewUpdatesEnabled?: (enabled: boolean) => Promise<DesktopUpdateState>;
  activateUpdate?: () => Promise<boolean>;
  onMenuCommand?: (listener: (command: DesktopMenuCommand) => void) => () => void;
  onOAuthConnected?: (listener: (payload: { provider?: string }) => void) => () => void;
  onUpdateState?: (listener: (state: DesktopUpdateState) => void) => () => void;
  openExternal: (url: string) => Promise<boolean>;
  revealPath?: (path: string) => Promise<boolean>;
  showEditorContextMenu?: (request: DesktopEditorContextMenuRequest) => Promise<DesktopEditorCommand | null>;
  pickDirectories: (options?: DirectoryPickerOptions) => Promise<string[]>;
  setLocale?: (locale: "zh-CN" | "zh-TW" | "en") => Promise<string>;
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

export async function revealDesktopPath(path: string) {
  const clean = path.trim();
  const bridge = desktopBridge();
  if (!clean || !bridge?.revealPath) {
    return false;
  }
  try {
    return await bridge.revealPath(clean);
  } catch {
    return false;
  }
}

export async function showDesktopEditorContextMenu(request: DesktopEditorContextMenuRequest) {
  const bridge = desktopBridge();
  if (!bridge?.showEditorContextMenu) {
    return null;
  }
  try {
    return await bridge.showEditorContextMenu(request);
  } catch {
    return null;
  }
}

export function onOAuthConnected(listener: (payload: { provider?: string }) => void) {
  const bridge = desktopBridge();
  if (!bridge?.onOAuthConnected) {
    return () => {};
  }
  try {
    return bridge.onOAuthConnected(listener);
  } catch {
    return () => {};
  }
}

export function onDesktopMenuCommand(listener: (command: DesktopMenuCommand) => void) {
  const bridge = desktopBridge();
  if (!bridge?.onMenuCommand) {
    return () => {};
  }
  try {
    return bridge.onMenuCommand(listener);
  } catch {
    return () => {};
  }
}

export async function getDesktopUpdateState(): Promise<DesktopUpdateState> {
  const bridge = desktopBridge();
  if (!bridge?.getUpdateState) {
    return { status: "unavailable", receivePreviewUpdates: false, version: "", percent: null };
  }
  try {
    return await bridge.getUpdateState();
  } catch {
    return { status: "unavailable", receivePreviewUpdates: false, version: "", percent: null };
  }
}

export async function setDesktopPreviewUpdatesEnabled(enabled: boolean) {
  const bridge = desktopBridge();
  if (!bridge?.setPreviewUpdatesEnabled) {
    return null;
  }
  return bridge.setPreviewUpdatesEnabled(enabled);
}

export function onDesktopUpdateState(listener: (state: DesktopUpdateState) => void) {
  const bridge = desktopBridge();
  if (!bridge?.onUpdateState) {
    return () => {};
  }
  try {
    return bridge.onUpdateState(listener);
  } catch {
    return () => {};
  }
}

export async function activateDesktopUpdate() {
  const bridge = desktopBridge();
  if (!bridge?.activateUpdate) {
    return false;
  }
  try {
    return await bridge.activateUpdate();
  } catch {
    return false;
  }
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

export function setDesktopLocale(locale: "zh-CN" | "zh-TW" | "en") {
  const bridge = desktopBridge();
  if (!bridge?.setLocale) {
    return;
  }
  void bridge.setLocale(locale).catch(() => {});
}

function desktopBridge() {
  return typeof window === "undefined" ? undefined : window.puddingElectronDesktop;
}
