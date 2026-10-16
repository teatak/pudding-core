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
  kind: z.enum(["doc", "table", "widget", "rich-document"]),
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

export const documentContent = z.object({
  itemID: z.string(), body: z.string(), contentHash: itemHash, revisionID: itemHash,
});
export type DocumentContent = z.infer<typeof documentContent>;
export type DocumentEdit = { old: string; new: string };
export type DocumentWrite = {
  operationID?: string; leaseToken?: string; replacement?: string;
  clientRequestID: string; expectedHash?: string; body?: string;
  edits?: DocumentEdit[]; restoreRevision?: string; preserveOnly?: boolean; undoRevision?: string;
};

// Source offsets are zero-based UTF-8 bytes, with an exclusive end.
export const documentEditOperation = z.object({
  operationID: z.string(), itemID: z.string(), state: z.enum(["active", "committed", "cancelled", "expired"]),
  from: z.number().int().nonnegative(), to: z.number().int().nonnegative(),
  contentHash: itemHash, revisionID: itemHash.optional(),
  author: z.object({ kind: z.enum(["user", "session"]), sessionID: z.string().optional(), turnID: z.string().optional() }),
  createdAt: z.number(), updatedAt: z.number(), expiresAt: z.number(), sequence: z.number().int(),
  leaseToken: z.string().optional(), target: z.string().optional(),
});
export const documentEditState = z.object({ contentHash: itemHash, operations: z.array(documentEditOperation) });
export type DocumentEditOperation = z.infer<typeof documentEditOperation>;
export type DocumentEditState = z.infer<typeof documentEditState>;
export type DocumentEditRequest = {
  action: "begin" | "renew" | "cancel"; clientRequestID: string;
  operationID?: string; leaseToken?: string; expectedHash?: string;
  from?: number; to?: number; target?: string;
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

// Rich documents retain structured content and owned, immutable binary assets.
export type RichNode = { type: string; attrs?: Record<string, unknown>; text?: string; content?: RichNode[]; marks?: {type: string; attrs?: Record<string, unknown>}[] };
export const richNode: z.ZodType<RichNode> = z.lazy(() => z.object({type:z.string(),attrs:z.record(z.string(),z.unknown()).optional(),text:z.string().optional(),content:z.array(richNode).optional(),marks:z.array(z.object({type:z.string(),attrs:z.record(z.string(),z.unknown()).optional()})).optional()}));
export const richDocumentBody = z.object({
 schemaVersion:z.union([z.literal(1),z.literal(2)]),body:richNode,
 headerFooter:z.object({header:z.object({default:richNode,first:richNode.optional(),even:richNode.optional()}),footer:z.object({default:richNode,first:richNode.optional(),even:richNode.optional()}),headerDistance:z.number().int().nonnegative(),footerDistance:z.number().int().nonnegative(),differentFirstPage:z.boolean(),differentOddEven:z.boolean()}).optional(),
 page:z.object({width:z.number().int(),height:z.number().int(),top:z.number().int(),right:z.number().int(),bottom:z.number().int(),left:z.number().int()}),
 assets:z.array(z.object({id:z.string(),hash:itemHash,type:z.enum(["image/png","image/jpeg","image/gif"]),width:z.number().optional(),height:z.number().optional()})),
 warnings:z.array(z.string()),original:z.object({name:z.string(),hash:itemHash}).optional(),
});
export type RichDocumentBody = z.infer<typeof richDocumentBody>;
export const richDocumentContent=z.object({itemID:z.string(),body:richDocumentBody,contentHash:itemHash,revisionID:itemHash});
export type RichDocumentContent=z.infer<typeof richDocumentContent>;
export type RichBlob={hash:string;type:string;data:string};
export type RichOperation = { block_id: string } & (
 | {kind:"replace_text";old_text:string;text?:string;content?:RichNode[]}
 | {kind:"format_text";old_text:string;marks:NonNullable<RichNode["marks"]>}
 | {kind:"set_block";node_type?:"paragraph"|"heading";attrs?:{level?:number;textAlign?:"left"|"center"|"right"|"justify"}}
 | {kind:"insert_blocks";placement:"before"|"after";content:RichNode[]}
 | {kind:"replace_block";content:RichNode[]}
 | {kind:"delete_block"}
);
export type RichDocumentWrite={clientRequestID:string;expectedHash:string;body?:RichDocumentBody;blobs?:RichBlob[];restoreRevision?:string;undoRevision?:string;operations?:RichOperation[]};
