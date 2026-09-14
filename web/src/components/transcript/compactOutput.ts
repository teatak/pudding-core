import type { Message } from "@/api/client";
import type { AssistantOverlay } from "@/state/overlayStore";

export function isCompactMessage(message: Message) {
  const metadata = message.metadata;
  return message.role === "summary" && Boolean(metadata && typeof metadata === "object" && "compact" in metadata && metadata.compact);
}

// A summary is an ordered output boundary, including inside an unfinished turn.
export function splitCompactMessages(messages: Message[]) {
  const groups: Message[][] = [];
  let current: Message[] = [];
  for (const message of messages) {
    if (isCompactMessage(message)) {
      if (current.length) groups.push(current);
      groups.push([message]);
      current = [];
    } else {
      current.push(message);
    }
  }
  if (current.length) groups.push(current);
  return groups;
}

// Render committed output through the latest summary from canonical messages.
// The overlay may still contain those streamed tool exchanges; their call IDs
// identify the prefix to omit. After reconnect, absent IDs mean it contains only
// fresh output. No summary body or extra boundary state lives in the overlay.
export function compactLiveOutput(messages: Message[], overlay: AssistantOverlay) {
  let boundary = messages.length - 1;
  while (boundary >= 0 && !isCompactMessage(messages[boundary])) boundary--;
  if (boundary < 0) return { messages: [], overlay };
  const prefix = messages.slice(0, boundary + 1);
  const committedCalls = new Set(prefix.flatMap(message => message.parts.flatMap(part =>
    part.type === "tool_result" ? [part.id] : [],
  )));
  let end = overlay.parts.length - 1;
  while (end >= 0) {
    const part = overlay.parts[end];
    if ((part.type === "tool" || part.type === "approval") && committedCalls.has(part.callID || "")) break;
    end--;
  }
  if (end < 0) return { messages: prefix, overlay };
  const parts = overlay.parts.slice(end + 1);
  const text = parts.flatMap(part => part.type === "text" ? [part.text] : []).join("");
  return { messages: prefix, overlay: { ...overlay, parts, text } };
}
