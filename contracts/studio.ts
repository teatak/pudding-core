import { z } from "zod";
import widgetPolicy from "./widget.json";

// Embedded images are rendered only through <img>, never as inline SVG.
export const studioIconImage = z.string()
  .max(26 + 4 * Math.ceil(widgetPolicy.distribution.maxIconBytes / 3))
  .regex(/^data:image\/svg\+xml;base64,(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/)
  .min(30);

// Content addresses of item versions.
export const itemHash = z.string().regex(/^[a-f0-9]{64}$/);

export const studioItem = z.object({
  origin: z.object({registryURL: z.string(), packageID: z.string(), version: z.string(), packageHash: itemHash, sourceHash: itemHash, copy: z.boolean()}).optional(),
  id: z.string(),
  kind: z.preprocess((v) => (v === "doc" || v === "note" ? "page" : v), z.enum(["page", "sheet", "widget"])),
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
  archivedAt: z.string().optional(),
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

export const pageContent = z.object({
  itemID: z.string(), body: z.string(), contentHash: itemHash, revisionID: itemHash,
});
export type PageContent = z.infer<typeof pageContent>;
export type PageEdit = { old: string; new: string };
export type PageWrite = {
  operationID?: string; leaseToken?: string; replacement?: string;
  clientRequestID: string; expectedHash?: string; body?: string;
  edits?: PageEdit[]; restoreRevision?: string; preserveOnly?: boolean; undoRevision?: string;
};

// Source offsets are zero-based UTF-8 bytes, with an exclusive end.
export const pageEditOperation = z.object({
  operationID: z.string(), itemID: z.string(), state: z.enum(["active", "committed", "cancelled", "expired"]),
  from: z.number().int().nonnegative(), to: z.number().int().nonnegative(),
  contentHash: itemHash, revisionID: itemHash.optional(),
  author: z.object({ kind: z.enum(["user", "session"]), sessionID: z.string().optional(), turnID: z.string().optional() }),
  createdAt: z.number(), updatedAt: z.number(), expiresAt: z.number(), sequence: z.number().int(),
  leaseToken: z.string().optional(), target: z.string().optional(),
});
export const pageEditState = z.object({ contentHash: itemHash, operations: z.array(pageEditOperation) });
export type PageEditOperation = z.infer<typeof pageEditOperation>;
export type PageEditState = z.infer<typeof pageEditState>;
export type PageEditRequest = {
  action: "begin" | "renew" | "cancel"; clientRequestID: string;
  operationID?: string; leaseToken?: string; expectedHash?: string;
  from?: number; to?: number; target?: string;
};

// Compatibility aliases
export const noteContent = pageContent;
export type NoteContent = PageContent;
export type NoteEdit = PageEdit;
export type NoteWrite = PageWrite;
export const noteEditOperation = pageEditOperation;
export const noteEditState = pageEditState;
export type NoteEditOperation = PageEditOperation;
export type NoteEditState = PageEditState;
export type NoteEditRequest = PageEditRequest;

export const documentContent = pageContent;
export type DocumentContent = PageContent;
export type DocumentEdit = PageEdit;
export type DocumentWrite = PageWrite;
export const documentEditOperation = pageEditOperation;
export const documentEditState = pageEditState;
export type DocumentEditOperation = PageEditOperation;
export type DocumentEditState = PageEditState;
export type DocumentEditRequest = PageEditRequest;

export const sheetCell = z.union([z.string(), z.number().finite(), z.boolean(), z.null()]);
export const sheetColumn = z.object({
  id: z.string().regex(/^[a-zA-Z0-9_-]{1,100}$/), name: z.string().min(1).max(200),
  type: z.enum(["text", "number", "date", "select", "checkbox", "link"]),
  options: z.array(z.string()).optional(),
}).strict();
export const sheetRow = z.object({ id: z.string().regex(/^[a-zA-Z0-9_-]{1,100}$/), cells: z.record(z.string(), sheetCell) }).strict();
export const sheetBody = z.object({ columns: z.array(sheetColumn), rows: z.array(sheetRow) }).strict();
export const sheetContent = z.object({ itemID: z.string(), body: sheetBody, contentHash: itemHash, revisionID: itemHash });
export const sheetOperation = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("set_cell"), rowID: z.string(), columnID: z.string(), value: sheetCell, expected: sheetCell.optional() }).strict(),
  z.object({ kind: z.literal("add_row"), row: sheetRow, beforeID: z.string().optional() }).strict(),
  z.object({ kind: z.literal("delete_row"), rowID: z.string() }).strict(),
  z.object({ kind: z.literal("move_row"), rowID: z.string(), beforeID: z.string().optional() }).strict(),
  z.object({ kind: z.literal("add_column"), column: sheetColumn, beforeID: z.string().optional() }).strict(),
  z.object({ kind: z.literal("update_column"), columnID: z.string(), column: sheetColumn }).strict(),
  z.object({ kind: z.literal("delete_column"), columnID: z.string() }).strict(),
  z.object({ kind: z.literal("move_column"), columnID: z.string(), beforeID: z.string().optional() }).strict(),
]);
export type SheetCell = z.infer<typeof sheetCell>;
export type SheetColumn = z.infer<typeof sheetColumn>;
export type SheetRow = z.infer<typeof sheetRow>;
export type SheetBody = z.infer<typeof sheetBody>;
export type SheetContent = z.infer<typeof sheetContent>;
export type SheetOperation = z.infer<typeof sheetOperation>;
export type SheetWrite = { clientRequestID: string; operations?: SheetOperation[]; restoreRevision?: string; expectedHash?: string; undoRevision?: string; rowID?: string };
