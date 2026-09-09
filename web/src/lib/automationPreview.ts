import type { DesktopComputerPreview, DesktopComputerPreviewFrame } from "./desktopBridge";

// Browser results use a viewing delay; native previews use Electron's per-app deadline.
export const AUTOMATION_PREVIEW_DISMISS_DELAY_MS = 30_000;

export function updateComputerPreviews(current: DesktopComputerPreviewFrame[], next: DesktopComputerPreview, now = Date.now()) {
  const frames = current.filter(frame => frame.sessionID === next.sessionID && frame.turnID === next.turnID
    && frame.appID !== next.appID && frame.expiresAt > now);
  // Loading/failure is a removal, never a renderable placeholder or an old image.
  if (next.status === "live" && next.imageURL && next.expiresAt > now) frames.push(next);
  return frames.sort((a, b) => b.activityVersion - a.activityVersion);
}
