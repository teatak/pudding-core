import type { Attachment } from "@/api/client";
import { apiURL } from "@/state/apiBase";

export function attachmentResourceURL(attachment: Pick<Attachment, "url"> | undefined, token: string) {
  const raw = attachment?.url.trim() || "";
  if (!raw) {
    return "";
  }
  const href = apiURL(raw);
  if (!token) {
    return href;
  }
  try {
    const url = new URL(href, window.location.href);
    url.searchParams.set("token", token);
    return url.toString();
  } catch {
    const separator = href.includes("?") ? "&" : "?";
    return `${href}${separator}token=${encodeURIComponent(token)}`;
  }
}
