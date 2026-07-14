import type { Attachment, ContentPart, LocalFolder, ProjectReference } from "@/api/client";

export type DraftPartOrderItem =
  | { type: "attachment"; id: string }
  | { type: "local_folder"; id: string }
  | { type: "project_reference"; id: string };

export type OrderedDraftItem<TAttachment, TFolder, TProjectReference> =
  | { type: "attachment"; item: TAttachment }
  | { type: "local_folder"; item: TFolder }
  | { type: "project_reference"; item: TProjectReference };

export function orderedDraftItems<
  TAttachment extends { id: string },
  TFolder extends { id: string },
  TProjectReference extends { id: string } = never,
>(
  attachments: TAttachment[],
  localFolders: TFolder[],
  partOrder: DraftPartOrderItem[],
  projectReferences: TProjectReference[] = [],
): OrderedDraftItem<TAttachment, TFolder, TProjectReference>[] {
  const attachmentsByID = new Map(attachments.map((item) => [item.id, item]));
  const foldersByID = new Map(localFolders.map((item) => [item.id, item]));
  const projectReferencesByID = new Map(projectReferences.map((item) => [item.id, item]));
  const seenAttachments = new Set<string>();
  const seenFolders = new Set<string>();
  const seenProjectReferences = new Set<string>();
  const out: OrderedDraftItem<TAttachment, TFolder, TProjectReference>[] = [];

  for (const orderItem of partOrder) {
    if (orderItem.type === "attachment") {
      const item = attachmentsByID.get(orderItem.id);
      if (item && !seenAttachments.has(item.id)) {
        seenAttachments.add(item.id);
        out.push({ type: "attachment", item });
      }
      continue;
    }
    if (orderItem.type === "local_folder") {
      const item = foldersByID.get(orderItem.id);
      if (item && !seenFolders.has(item.id)) {
        seenFolders.add(item.id);
        out.push({ type: "local_folder", item });
      }
      continue;
    }
    const item = projectReferencesByID.get(orderItem.id);
    if (item && !seenProjectReferences.has(item.id)) {
      seenProjectReferences.add(item.id);
      out.push({ type: "project_reference", item });
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
  for (const item of projectReferences) {
    if (!seenProjectReferences.has(item.id)) {
      out.push({ type: "project_reference", item });
    }
  }
  return out;
}

export function buildDraftSubmitParts<TAttachment extends { id: string; attachment?: Attachment }>(
  text: string,
  attachments: TAttachment[],
  localFolders: LocalFolder[],
  partOrder: DraftPartOrderItem[],
  projectReferences: ProjectReference[] = [],
): ContentPart[] {
  const parts: ContentPart[] = [];
  for (const item of orderedDraftItems(attachments, localFolders, partOrder, projectReferences)) {
    if (item.type === "attachment") {
      if (item.item.attachment) {
        parts.push({ ...item.item.attachment, type: "attachment" });
      }
      continue;
    }
    if (item.type === "local_folder") {
      parts.push({ ...item.item, type: "local_folder" });
      continue;
    }
    parts.push({ ...item.item, type: "project_reference" });
  }
  if (text) {
    parts.push({ type: "text", text });
  }
  return parts;
}
