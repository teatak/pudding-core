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
  edits?: DocumentEdit[]; restoreRevision?: string; preserveOnly?: boolean; undoRevision?: string;
};

export const tableCell = z.union([z.string(), z.number().finite(), z.boolean(), z.null()]);
export const tableColumn = z.object({
  id: z.string().regex(/^[a-zA-Z0-9_-]{1,100}$/), name: z.string().min(1).max(200),
  type: z.enum(["text", "number", "date", "select", "checkbox", "link"]),
  options: z.array(z.string()).optional(),
}).strict();
export const tableRow = z.object({ id: z.string().regex(/^[a-zA-Z0-9_-]{1,100}$/), cells: z.record(z.string(), tableCell) }).strict();
export const tableBody = z.object({ columns: z.array(tableColumn), rows: z.array(tableRow) }).strict();
export const tableContent = z.object({ itemID: z.string(), body: tableBody, contentHash: itemHash, revisionID: itemHash });
export const tableOperation = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("set_cell"), rowID: z.string(), columnID: z.string(), value: tableCell, expected: tableCell.optional() }).strict(),
  z.object({ kind: z.literal("add_row"), row: tableRow, beforeID: z.string().optional() }).strict(),
  z.object({ kind: z.literal("delete_row"), rowID: z.string() }).strict(),
  z.object({ kind: z.literal("move_row"), rowID: z.string(), beforeID: z.string().optional() }).strict(),
  z.object({ kind: z.literal("add_column"), column: tableColumn, beforeID: z.string().optional() }).strict(),
  z.object({ kind: z.literal("update_column"), columnID: z.string(), column: tableColumn }).strict(),
  z.object({ kind: z.literal("delete_column"), columnID: z.string() }).strict(),
  z.object({ kind: z.literal("move_column"), columnID: z.string(), beforeID: z.string().optional() }).strict(),
]);
export type TableCell = z.infer<typeof tableCell>;
export type TableColumn = z.infer<typeof tableColumn>;
export type TableRow = z.infer<typeof tableRow>;
export type TableBody = z.infer<typeof tableBody>;
export type TableContent = z.infer<typeof tableContent>;
export type TableOperation = z.infer<typeof tableOperation>;
export type TableWrite = { clientRequestID: string; operations?: TableOperation[]; restoreRevision?: string; expectedHash?: string; undoRevision?: string; rowID?: string };
