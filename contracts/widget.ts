import { z } from "zod";
import { studioItem, studioItemRevision, studioIconImage } from "./studio";

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
      "stateWrite",
      "storageRead",
      "storageWrite",
      "interactionStart",
      "interactionNotify",
      "interactionRequests",
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
    targetID: z.string().optional(),
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

// Persistent data is scoped to the item, never to a session or source revision.
export const widgetData = z.object({
  version: z.number().int().nonnegative(),
  data: z.record(z.string(), z.unknown()),
});
export const widgetDataWrite = z
  .object({
    expectedVersion: z.number().int().nonnegative(),
    data: z.record(z.string(), z.unknown()),
  })
  .strict();
export type WidgetData = z.infer<typeof widgetData>;

export const widgetParticipant = z.object({
  id: z.string(),
  sessionID: z.string().optional(),
  name: z.string(),
  roles: z.array(z.string()),
});
export const widgetNotification = z
  .object({
    id: z.string().min(1).max(100),
    audience: z.discriminatedUnion("kind", [
      z.object({ kind: z.literal("all") }).strict(),
      z
        .object({
          kind: z.literal("selected"),
          participantIDs: z.array(z.string().min(1)).min(1).max(16),
        })
        .strict(),
    ]),
    delivery: z.enum(["inform", "request-action"]),
    topic: z.string().min(1).max(100),
    message: z.string().trim().min(1).max(10000),
    summary: z.string().trim().min(1).max(1000).optional(),
    data: z.json().optional(),
  })
  .strict();
export const widgetRequestKey = z
  .object({ notificationID: z.string(), participantID: z.string() })
  .strict();
export const widgetRun = z.object({
  id: z.string(),
  itemID: z.string(),
  title: z.string(),
  targetID: z.string(),
  revisionHash: widgetHash,
  bindingVersion: z.number().int(),
  participants: z.array(widgetParticipant),
  status: z.enum(["running", "paused", "stopped"]),
  reason: z.string().optional(),
  receipts: z.array(
    widgetNotification.extend({
      actor: z.string().optional(),
      seq: z.number().int(),
      stateVersion: z.number().int(),
      deliveries: z.array(
        z.object({
          attempt: z.number().int().nonnegative(),
          participantID: z.string(),
          status: z.enum([
            "pending",
            "informed",
            "queued",
            "running",
            "completed",
            "cancelled",
            "failed",
          ]),
          active: z.boolean(),
          messageID: z.string().optional(),
          turnID: z.string().optional(),
          error: z.string().optional(),
        }),
      ),
    }),
  ),
});
export const widgetInteractionStart = z
  .object({ roles: z.array(z.string().trim().min(1).max(100)).min(1).max(16) })
  .strict();
export type WidgetRun = z.infer<typeof widgetRun>;
export type WidgetParticipant = z.infer<typeof widgetParticipant>;

// Hub offers only editable source releases. HTML-era releases are not candidates.
export const widgetReleaseRequirements = z.object({ protocolVersion: z.number().int().positive(), sdkVersion: z.string() }).strict();
export const widgetRegistryRelease = z.object({
  version: z.string().regex(/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-zA-Z0-9][a-zA-Z0-9.-]*)?$/),
  package: z.string().min(1), packageHash: widgetHash, requires: widgetReleaseRequirements,
}).strict();
export const widgetRegistry = z.object({
  kind: z.literal("pudding.widget.registry"), schemaVersion: z.literal(2),
  name: z.string(), title: z.record(z.string(), z.string()),
  items: z.array(z.object({
    id: z.string().regex(/^[a-zA-Z0-9_-]+\/[a-zA-Z0-9_.-]+\/widgets\/[a-z][a-z0-9-]{0,63}$/),
    title: z.record(z.string(), z.string()), description: z.record(z.string(), z.string()),
    icon: studioIconImage.optional(),
    releases: z.array(widgetRegistryRelease).max(100),
  }).strict()).max(500),
}).strict();
export type WidgetRegistryItem = z.infer<typeof widgetRegistry>["items"][number];
export type WidgetRegistryRelease = z.infer<typeof widgetRegistryRelease>;

export const widgetLibrarySource = z.object({ id: z.string(), url: z.string().url(), official: z.boolean() });
export const widgetSourcesResponse = z.object({ sources: z.array(widgetLibrarySource) });
export type WidgetLibrarySource = z.infer<typeof widgetLibrarySource>;

// Opening an original from a catalog returns a distinct editable widget.
export const widgetDraft = z.object({ baseRevisionHash: z.string(), draftHash: z.string(), files: z.array(z.string()) });
export const widgetDraftOpened = widgetDraft.extend({ widget: studioItem });

// Durable page snapshots are distinct from item-scoped shared storage.
export const widgetPage = z.object({
 id:z.string(), itemID:z.string(), scope:z.string(), revisionHash:z.string(), targetID:z.string(),
 version:z.number().int().nonnegative(),data:z.record(z.string(),z.unknown()),
});
export const widgetPageSelection = z.object({revisionHash:z.string()});
export const widgetPageOpen = z.object({page:widgetPage,run:widgetRun.nullable()});
