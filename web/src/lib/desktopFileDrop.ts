import type { Attachment } from "@/api/client";

const DESKTOP_FILE_DROP_EVENT = "pudding:file-drop";

export type DesktopFileDropResult = {
  attachments: Attachment[];
  directories: string[];
  failedFileCount: number;
};

type DesktopFileDropTarget = {
  kind: "conversation" | "draft";
  sessionID?: string;
};

type DesktopFileDropPayload = {
  attachments: Attachment[];
  directories: string[];
  failedFiles: string[];
  target: {
    kind: string;
    sessionID: string;
  };
};

type RuntimeEvent = {
  data?: unknown;
};

export function bindDesktopFileDrop(target: DesktopFileDropTarget, onDrop: (drop: DesktopFileDropResult) => void) {
  let active = true;
  let off: (() => void) | undefined;

  void import("@wailsio/runtime")
    .then(({ Events }) => {
      if (!active) {
        return;
      }
      off = Events.On(DESKTOP_FILE_DROP_EVENT, (event: RuntimeEvent) => {
        const payload = parseDesktopFileDropPayload(event.data);
        if (!payload || !desktopDropTargetMatches(payload.target, target)) {
          return;
        }
        if (payload.directories.length === 0 && payload.attachments.length === 0 && payload.failedFiles.length === 0) {
          return;
        }
        onDrop({
          attachments: payload.attachments,
          directories: payload.directories,
          failedFileCount: payload.failedFiles.length,
        });
      });
    })
    .catch(() => {});

  return () => {
    active = false;
    off?.();
  };
}

export function nativeFileDropLikelyAvailable() {
  const flags = (window as typeof window & { _wails?: { flags?: { enableFileDrop?: unknown } } })._wails?.flags;
  return flags?.enableFileDrop === true;
}

function parseDesktopFileDropPayload(data: unknown): DesktopFileDropPayload | null {
  if (!isRecord(data)) {
    return null;
  }
  const target = isRecord(data.target) ? data.target : {};
  return {
    attachments: attachmentArray(data.attachments),
    directories: stringArray(data.directories),
    failedFiles: stringArray(data.failedFiles),
    target: {
      kind: stringValue(target.kind),
      sessionID: stringValue(target.sessionID),
    },
  };
}

function desktopDropTargetMatches(actual: DesktopFileDropPayload["target"], expected: DesktopFileDropTarget) {
  if (actual.kind !== expected.kind) {
    return false;
  }
  if (expected.kind === "conversation") {
    return Boolean(expected.sessionID) && actual.sessionID === expected.sessionID;
  }
  return true;
}

function attachmentArray(value: unknown): Attachment[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => {
    if (!isRecord(item)) {
      return [];
    }
    const id = stringValue(item.id);
    const name = stringValue(item.name);
    const attachmentKey = stringValue(item.attachmentKey);
    const url = stringValue(item.url);
    const mime = stringValue(item.mime);
    const size = numberValue(item.size);
    if (!id || !name || !attachmentKey || !url || !mime || size < 0) {
      return [];
    }
    return [
      {
        id,
        name,
        attachmentKey,
        url,
        mime,
        size,
        origin: optionalString(item.origin),
        createdAt: optionalString(item.createdAt),
        audioTranscript: optionalString(item.audioTranscript),
      },
    ];
  });
}

function stringArray(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return dedupeStrings(value.map(stringValue).map((item) => item.trim()).filter(Boolean));
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function optionalString(value: unknown) {
  return typeof value === "string" && value ? value : undefined;
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : -1;
}

function dedupeStrings(values: string[]) {
  const seen = new Set<string>();
  return values.filter((value) => {
    if (seen.has(value)) {
      return false;
    }
    seen.add(value);
    return true;
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
