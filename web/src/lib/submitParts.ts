import type { Attachment, ContentPart, LocalFolder } from "@/api/client";

export type DraftPartOrderItem = { type: "attachment"; id: string } | { type: "local_folder"; id: string };

export type OrderedDraftItem<TAttachment, TFolder> =
  | { type: "attachment"; item: TAttachment }
  | { type: "local_folder"; item: TFolder };

export function orderedDraftItems<TAttachment extends { id: string }, TFolder extends { id: string }>(
  attachments: TAttachment[],
  localFolders: TFolder[],
  partOrder: DraftPartOrderItem[],
): OrderedDraftItem<TAttachment, TFolder>[] {
  const attachmentsByID = new Map(attachments.map((item) => [item.id, item]));
  const foldersByID = new Map(localFolders.map((item) => [item.id, item]));
  const seenAttachments = new Set<string>();
  const seenFolders = new Set<string>();
  const out: OrderedDraftItem<TAttachment, TFolder>[] = [];

  for (const orderItem of partOrder) {
    if (orderItem.type === "attachment") {
      const item = attachmentsByID.get(orderItem.id);
      if (item && !seenAttachments.has(item.id)) {
        seenAttachments.add(item.id);
        out.push({ type: "attachment", item });
      }
      continue;
    }
    const item = foldersByID.get(orderItem.id);
    if (item && !seenFolders.has(item.id)) {
      seenFolders.add(item.id);
      out.push({ type: "local_folder", item });
    }
  }

  for (const item of attachments) {
    if (!seenAttachments.has(item.id)) {
      out.push({ type: "attachment", item });
    }
  }
  for (const item of localFolders) {
    if (!seenFolders.has(item.id)) {
      out.push({ type: "local_folder", item });
    }
  }
  return out;
}

export function buildDraftSubmitParts<TAttachment extends { id: string; attachment?: Attachment }>(
  text: string,
  attachments: TAttachment[],
  localFolders: LocalFolder[],
  partOrder: DraftPartOrderItem[],
): ContentPart[] {
  const parts: ContentPart[] = [];
  for (const item of orderedDraftItems(attachments, localFolders, partOrder)) {
    if (item.type === "attachment") {
      if (item.item.attachment) {
        parts.push({ ...item.item.attachment, type: "attachment" });
      }
      continue;
    }
    parts.push({ ...item.item, type: "local_folder" });
  }
  if (text) {
    parts.push({ type: "text", text });
  }
  return parts;
}
