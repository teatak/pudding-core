import { z } from "zod";

export const canvasHash = z.string().regex(/^[a-f0-9]{64}$/);
export const canvasPackage = z
  .object({ files: z.record(z.string(), z.string()) })
  .strict();
export const canvasSource = z
  .object({ appID: z.string(), endpoint: z.string() })
  .strict();
export const canvasOperation = z
  .object({
    source: z.string(),
    kind: z.enum(["rest", "graphql"]),
    effectHint: z.enum(["read", "write", "unknown"]),
    description: z.string().optional(),
    inputSchema: z.record(z.string(), z.unknown()),
    request: z
      .object({
        method: z.string().optional(),
        path: z.string().optional(),
        pathParams: z.record(z.string(), z.unknown()).optional(),
        query: z.record(z.string(), z.unknown()).optional(),
        body: z.unknown().optional(),
        document: z.string().optional(),
        operationName: z.string().optional(),
        variables: z.record(z.string(), z.unknown()).optional(),
      })
      .strict(),
    result: z
      .object({
        rows: z.string().optional(),
        total: z.string().optional(),
        cursor: z.string().optional(),
        schema: z.record(z.string(), z.unknown()).optional(),
        success: z
          .object({ pointer: z.string(), equals: z.unknown() })
          .strict()
          .optional(),
      })
      .strict()
      .optional(),
  })
  .strict();
export const canvasManifest = z
  .object({
    schemaVersion: z.literal(1),
    sdkVersion: z.literal("1"),
    entry: z.string(),
    sources: z.record(z.string(), canvasSource),
    operations: z.record(z.string(), canvasOperation),
  })
  .strict();
export type CanvasPackage = z.infer<typeof canvasPackage>;
export type CanvasManifest = z.infer<typeof canvasManifest>;
export type CanvasOperation = z.infer<typeof canvasOperation>;

export const canvasBuildResult = z.object({
  revisionHash: canvasHash,
  sdkVersion: z.string(),
  compilerVersion: z.string(),
  dependencyHash: z.string(),
  ok: z.boolean(),
  html: z.string().optional(),
  diagnostics: z.array(
    z.object({
      file: z.string().optional(),
      line: z.number().optional(),
      message: z.string(),
    }),
  ),
});
export type CanvasBuildResult = z.infer<typeof canvasBuildResult>;

export const canvasBridgeRequest = z
  .object({
    id: z.string().min(1).max(100),
    method: z.enum([
      "query",
      "action",
      "requestAgent",
      "linksList",
      "linksCreate",
      "linksRemove",
      "cancel",
      "exportFile",
    ]),
    operationID: z.string().max(80).optional(),
    params: z.record(z.string(), z.unknown()).optional(),
  })
  .strict();

export const canvas = z.object({
  id: z.string(),
  name: z.string(),
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
export const canvasRevision = z.object({
  canvasID: z.string(),
  hash: canvasHash,
  parentRevision: z.string(),
  clientRequestID: z.string(),
  createdAt: z.string(),
  buildReceipt: z
    .object({
      revisionHash: canvasHash,
      sdkVersion: z.string(),
      compilerVersion: z.string(),
      dependencyHash: z.string(),
      ok: z.literal(true),
    })
    .optional(),
});
export const canvasRevisionResponse = z.object({
  kind: z.literal("app"),
  revision: canvasRevision,
  package: canvasPackage,
  manifest: canvasManifest,
});
export const canvasArchiveEntry = z.object({
  id: z.string(),
  canvasID: z.string(),
  name: z.string(),
  archivedAt: z.string(),
  versionCount: z.number().int(),
  path: z.string(),
  warnings: z.array(z.string()),
});
export const canvasArchivesResponse = z.object({
  archives: z.array(canvasArchiveEntry),
});
export type CanvasArchiveEntry = z.infer<typeof canvasArchiveEntry>;
export const canvasesResponse = z.object({
  canvases: z.array(canvas),
});
export const canvasRevisionsResponse = z.object({
  revisions: z.array(canvasRevision),
});
export type Canvas = z.infer<typeof canvas>;
export type CanvasRevision = z.infer<typeof canvasRevision>;
export const canvasQueryResult = z.object({
  data: z.unknown(),
  requestID: z.string(),
  revisionHash: canvasHash,
  bindingVersion: z.number().int(),
  fetchedAt: z.string(),
});
export const canvasAction = z.object({
  id: z.string(),
  canvasID: z.string(),
  clientRequestID: z.string(),
  requestHash: z.string(),
  state: z.enum(["prepared", "executing", "succeeded", "failed", "unknown"]),
  createdAt: z.string(),
  spec: z.object({
    revisionHash: z.string(),
    resourceRevision: z.number(),
    bindingVersion: z.number(),
    operationID: z.string(),
    operationHash: z.string(),
    bindingFingerprint: z.string(),
    appID: z.string(),
    connectionID: z.string(),
    description: z.string(),
    params: z.record(z.string(), z.unknown()),
    request: z.unknown(),
  }),
  result: z.unknown().optional(),
});
export const canvasActionsResponse = z.object({
  actions: z.array(canvasAction),
});
export type CanvasAction = z.infer<typeof canvasAction>;

export const canvasEntity = z.object({
  appID: z.string(),
  connectionID: z.string(),
  entityType: z.string(),
  entityID: z.string(),
});
export const canvasLink = z.object({
  id: z.string(),
  canvasID: z.string(),
  left: canvasEntity,
  right: canvasEntity,
  createdAt: z.string(),
});
export const canvasLinksResponse = z.object({
  links: z.array(canvasLink),
});
export const canvasEntityInput = z
  .object({
    source: z.string().min(1),
    entityType: z.string().min(1).max(100),
    entityID: z.string().min(1).max(500),
  })
  .strict();
export const canvasLinkInput = z
  .object({ left: canvasEntityInput, right: canvasEntityInput })
  .strict();
export type CanvasLinkInput = z.infer<typeof canvasLinkInput>;
