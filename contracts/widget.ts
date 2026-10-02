import { z } from "zod";
import { studioItemRevision } from "./studio";

export const widgetHash = z.string().regex(/^[a-f0-9]{64}$/);
export const widgetPackage = z
  .object({ files: z.record(z.string(), z.string()) })
  .strict();
export const widgetSource = z
  .object({ pluginID: z.string(), endpoint: z.string() })
  .strict();
export const widgetOperation = z
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
export const widgetManifest = z
  .object({
    schemaVersion: z.literal(1),
    sdkVersion: z.literal("1"),
    entry: z.string(),
    sources: z.record(z.string(), widgetSource),
    operations: z.record(z.string(), widgetOperation),
  })
  .strict();
export type WidgetPackage = z.infer<typeof widgetPackage>;
export type WidgetManifest = z.infer<typeof widgetManifest>;
export type WidgetOperation = z.infer<typeof widgetOperation>;

export const widgetBuildResult = z.object({
  revisionHash: widgetHash,
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
export type WidgetBuildResult = z.infer<typeof widgetBuildResult>;

export const widgetBridgeRequest = z
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

export const widgetQueryResult = z.object({
  data: z.unknown(),
  requestID: z.string(),
  revisionHash: widgetHash,
  bindingVersion: z.number().int(),
  fetchedAt: z.string(),
});
export const widgetAction = z.object({
  id: z.string(),
  itemID: z.string(),
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
    pluginID: z.string(),
    connectionID: z.string(),
    description: z.string(),
    params: z.record(z.string(), z.unknown()),
    request: z.unknown(),
  }),
  result: z.unknown().optional(),
});
export const widgetActionsResponse = z.object({
  actions: z.array(widgetAction),
});
export type WidgetAction = z.infer<typeof widgetAction>;

export const widgetEntity = z.object({
  pluginID: z.string(),
  connectionID: z.string(),
  entityType: z.string(),
  entityID: z.string(),
});
export const widgetLink = z.object({
  id: z.string(),
  itemID: z.string(),
  left: widgetEntity,
  right: widgetEntity,
  createdAt: z.string(),
});
export const widgetLinksResponse = z.object({
  links: z.array(widgetLink),
});
export const widgetEntityInput = z
  .object({
    source: z.string().min(1),
    entityType: z.string().min(1).max(100),
    entityID: z.string().min(1).max(500),
  })
  .strict();
export const widgetLinkInput = z
  .object({ left: widgetEntityInput, right: widgetEntityInput })
  .strict();
export type WidgetLinkInput = z.infer<typeof widgetLinkInput>;

export const widgetRevisionResponse = z.object({
  kind: z.literal("widget"),
  revision: studioItemRevision,
  package: widgetPackage,
  manifest: widgetManifest,
});
