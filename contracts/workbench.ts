import { z } from "zod";

export const workbenchHash = z.string().regex(/^[a-f0-9]{64}$/);
export const workbenchPackage = z
  .object({ files: z.record(z.string(), z.string()) })
  .strict();
export const workbenchSource = z
  .object({ appID: z.string(), endpoint: z.string() })
  .strict();
export const workbenchOperation = z
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
export const workbenchManifest = z
  .object({
    schemaVersion: z.literal(1),
    sdkVersion: z.literal("1"),
    entry: z.string(),
    sources: z.record(z.string(), workbenchSource),
    operations: z.record(z.string(), workbenchOperation),
  })
  .strict();
export type WorkbenchPackage = z.infer<typeof workbenchPackage>;
export type WorkbenchManifest = z.infer<typeof workbenchManifest>;
export type WorkbenchOperation = z.infer<typeof workbenchOperation>;

export const workbenchBuildResult = z.object({
  revisionHash: workbenchHash,
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
export type WorkbenchBuildResult = z.infer<typeof workbenchBuildResult>;

export const workbenchBridgeRequest = z
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
    ]),
    operationID: z.string().max(80).optional(),
    params: z.record(z.string(), z.unknown()).optional(),
  })
  .strict();

export const workbench = z.object({
  id: z.string(),
  name: z.string(),
  sourceSessionID: z.string().optional(),
  revision: z.number().int(),
  headRevision: z.string(),
  activeRevision: z.string(),
  bindings: z.record(z.string(), z.string()),
  bindingVersion: z.number().int(),
  grants: z.record(
    z.string(),
    z.object({
      operationHash: z.string(),
      bindingFingerprint: z.string(),
      connectionID: z.string(),
      grantedAt: z.string(),
    }),
  ),
  deleted: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export const workbenchRevision = z.object({
  workbenchID: z.string(),
  hash: workbenchHash,
  parentRevision: z.string(),
  clientRequestID: z.string(),
  createdAt: z.string(),
  buildReceipt: z
    .object({
      revisionHash: workbenchHash,
      sdkVersion: z.string(),
      compilerVersion: z.string(),
      dependencyHash: z.string(),
      ok: z.literal(true),
    })
    .optional(),
});
export const canvasContent = z.object({kind:z.string().min(1),title:z.string(),item:z.unknown()});
export const workbenchRevisionResponse = z.discriminatedUnion("kind",[
 z.object({kind:z.literal("app"),revision:workbenchRevision,package:workbenchPackage,manifest:workbenchManifest}),
 z.object({kind:z.literal("structured"),revision:workbenchRevision,content:canvasContent}),
]);
export const workbenchesResponse = z.object({
  canvases: z.array(workbench),
});
export const workbenchRevisionsResponse = z.object({
  revisions: z.array(workbenchRevision),
});
export type Workbench = z.infer<typeof workbench>;
export type WorkbenchRevision = z.infer<typeof workbenchRevision>;
export const workbenchQueryResult = z.object({
  data: z.unknown(),
  requestID: z.string(),
  revisionHash: workbenchHash,
  bindingVersion: z.number().int(),
  fetchedAt: z.string(),
});
export const workbenchAction = z.object({
  id: z.string(),
  workbenchID: z.string(),
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
export const workbenchActionsResponse = z.object({
  actions: z.array(workbenchAction),
});
export type WorkbenchAction = z.infer<typeof workbenchAction>;

export const workbenchEntity = z.object({
  appID: z.string(),
  connectionID: z.string(),
  entityType: z.string(),
  entityID: z.string(),
});
export const workbenchLink = z.object({
  id: z.string(),
  workbenchID: z.string(),
  left: workbenchEntity,
  right: workbenchEntity,
  createdAt: z.string(),
});
export const workbenchLinksResponse = z.object({
  links: z.array(workbenchLink),
});
export const workbenchEntityInput = z
  .object({
    source: z.string().min(1),
    entityType: z.string().min(1).max(100),
    entityID: z.string().min(1).max(500),
  })
  .strict();
export const workbenchLinkInput = z
  .object({ left: workbenchEntityInput, right: workbenchEntityInput })
  .strict();
export type WorkbenchLinkInput = z.infer<typeof workbenchLinkInput>;
