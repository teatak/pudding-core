import { z } from "zod";

// Content addresses of item versions.
export const itemHash = z.string().regex(/^[a-f0-9]{64}$/);

export const studioItem = z.object({
  id: z.string(),
  kind: z.enum(["doc", "table", "widget"]),
  name: z.string(),
  icon: z.string().optional(),
  iconColor: z.string().optional(),
  sourceSessionID: z.string().optional(),
  revision: z.number().int(),
  headRevision: z.string(),
  activeRevision: z.string(),
  bindings: z.record(z.string(), z.string()),
  bindingVersion: z.number().int(),
  deleted: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export const studioItemRevision = z.object({
  itemID: z.string(),
  hash: itemHash,
  parentRevision: z.string(),
  clientRequestID: z.string(),
  createdAt: z.string(),
  contentHash: itemHash.optional(),
  body: z.string().optional(),
  author: z.object({ kind: z.enum(["user", "session"]), sessionID: z.string().optional(), turnID: z.string().optional() }).optional(),
  buildReceipt: z
    .object({
      revisionHash: itemHash,
      sdkVersion: z.string(),
      compilerVersion: z.string(),
      dependencyHash: z.string(),
      ok: z.literal(true),
    })
    .optional(),
});

export const studioItemsResponse = z.object({
  items: z.array(studioItem),
});
export const studioItemRevisionsResponse = z.object({
  revisions: z.array(studioItemRevision),
});
export type StudioItem = z.infer<typeof studioItem>;
export type StudioItemRevision = z.infer<typeof studioItemRevision>;

export const documentContent = z.object({
  itemID: z.string(), body: z.string(), contentHash: itemHash, revisionID: itemHash,
});
export type DocumentContent = z.infer<typeof documentContent>;
export type DocumentEdit = { old: string; new: string };
export type DocumentWrite = {
  clientRequestID: string; expectedHash?: string; body?: string;
  edits?: DocumentEdit[]; restoreRevision?: string; preserveOnly?: boolean;
};
