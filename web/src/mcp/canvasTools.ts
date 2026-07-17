import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

import {
  createCanvasItem,
  deleteCanvasItem,
  listCanvasItems,
  putCanvasItem,
  type CanvasItemPayload,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { CanvasItem } from "@/contracts/api";
import { apiURL } from "@/state/apiBase";
import { requestCanvasReveal } from "@/state/canvasRevealStore";
import { useBrowserMCP, type RuntimeAppDefinition, type ToolDefinition } from "@/mcp/browserMCP";
import { createInputFlowTools } from "@/mcp/inputFlowTools";
import { getRuntimeID, getRuntimeType } from "@/state/runtime";
import { isElectronShell } from "@/state/shell";

const CANVAS_APP_ID = "canvas";

export function useCanvasMCP(token: string) {
  const queryClient = useQueryClient();
  const desktopRuntime = isElectronShell();
  const endpoint = useMemo(() => (token ? mcpWebSocketURL(token) : ""), [token]);
  const runtimeInfo = useMemo(() => ({ id: getRuntimeID(), type: getRuntimeType() }), []);
  const serverInfo = useMemo(
    () => ({ name: desktopRuntime ? "pudding-desktop" : "pudding-web", version: "1.0" }),
    [desktopRuntime],
  );
  const apps = useMemo<RuntimeAppDefinition[]>(
    () => (desktopRuntime ? [canvasRuntimeAppDefinition()] : []),
    [desktopRuntime],
  );
  const canvasTools = useMemo<ToolDefinition[]>(
    () => ([
      {
        name: "canvas_doc_read",
        description: "Read concise on-demand docs for canvas tools. Use when a canvas workflow/schema detail is unclear, or before presenting complex structured query results.",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            ref: {
              type: "string",
              enum: Object.keys(CANVAS_DOCS),
              description:
                "Doc ref: canvas_overview, canvas_items, canvas_markdown, canvas_table, canvas_chart, canvas_gallery, canvas_timeline, or canvas_grid.",
            },
          },
          required: ["ref"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const ref = requiredString(record.ref, "ref");
          const content = CANVAS_DOCS[ref];
          if (!content) {
            return jsonToolResult({ ok: false, reason: "not_found", ref, availableRefs: Object.keys(CANVAS_DOCS) });
          }
          return jsonToolResult({ ok: true, ref, content });
        },
      },
      {
        name: "canvas_item_list",
        description:
          "List canvas widgets in the current session with lightweight summaries. Call this before creating or updating canvas widgets in multi-turn work. For details call canvas_doc_read(ref='canvas_items').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {},
          additionalProperties: false,
        },
        handler: async (args) => {
          const sessionID = sessionIDFromArgs(args);
          const { items } = await listCanvasItems(token, sessionID);
          return jsonToolResult({
            ok: true,
            count: items.length,
            items: items.map(canvasItemSummary),
          });
        },
      },
      {
        name: "canvas_item_inspect",
        description:
          "Read one existing canvas widget in the current session by id with its current content and source details. Use after canvas_item_list when the summary is not enough.",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Canvas widget id to inspect." },
          },
          required: ["id"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const sessionID = sessionIDFromArgs(record);
          const id = requiredString(record.id, "id");
          const { items } = await listCanvasItems(token, sessionID);
          const item = items.find((entry) => entry.id === id);
          if (!item) {
            return jsonToolResult({ ok: false, reason: "not_found", id });
          }
          return jsonToolResult({
            ok: true,
            item: canvasItemInspection(item),
          });
        },
      },
      {
        name: "canvas_item_remove",
        description:
          "Remove one canvas widget from the current session by id. Call only when the user clearly asks to close or remove a widget.",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Canvas widget id to remove." },
          },
          required: ["id"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const sessionID = sessionIDFromArgs(record);
          const id = requiredString(record.id, "id");
          await deleteCanvasItem(token, sessionID, id);
          await queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(sessionID) });
          return jsonToolResult({ ok: true, removed: id });
        },
      },
      {
        name: "canvas_item_clear",
        description:
          "Clear all canvas widgets in the current session. Call only when the user clearly asks to clear the canvas, close everything, or start over.",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {},
          additionalProperties: false,
        },
        handler: async (args) => {
          const sessionID = sessionIDFromArgs(args);
          const { items } = await listCanvasItems(token, sessionID);
          await Promise.all(items.map((item) => deleteCanvasItem(token, sessionID, item.id)));
          await queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(sessionID) });
          return jsonToolResult({ ok: true, cleared: items.length, ids: items.map((item) => item.id) });
        },
      },
      {
        name: "canvas_markdown",
        description:
          "Create or update a markdown item in the current session. For workflow details call canvas_doc_read(ref='canvas_markdown').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Optional stable canvas item id to update." },
            title: { type: "string", description: "Short item title." },
            content: { type: "string", description: "Markdown content to display." },
            window: canvasWindowSchema(),
          },
          required: ["title", "content"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const title = requiredString(record.title, "title");
          const content = requiredString(record.content, "content");
          return saveCanvasItem({
            token,
            queryClient,
            args: record,
            kind: "markdown",
            title,
            item: { kind: "markdown", title, content },
          });
        },
      },
      {
        name: "canvas_table",
        description:
          "Create or update a table item in the current session. Prefer this for complex structured query results, lists, comparisons, inventories, schedules, and exportable rows. For schema details call canvas_doc_read(ref='canvas_table').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Optional stable canvas item id to update." },
            title: { type: "string", description: "Short item title." },
            columns: {
              type: "array",
              description: "Columns to render. Items may be strings or {key,label}.",
              items: {
                anyOf: [
                  { type: "string" },
                  {
                    type: "object",
                    properties: {
                      key: { type: "string" },
                      label: { type: "string" },
                    },
                    required: ["key"],
                    additionalProperties: false,
                  },
                ],
              },
            },
            rows: {
              type: "array",
              description: "Table rows keyed by column key.",
              items: { type: "object", additionalProperties: true },
            },
            caption: { type: "string", description: "Optional caption." },
            window: canvasWindowSchema(),
          },
          required: ["title", "columns", "rows"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const title = requiredString(record.title, "title");
          const columns = requiredArray(record.columns, "columns");
          const rows = requiredArray(record.rows, "rows");
          const caption = stringValue(record.caption);
          return saveCanvasItem({
            token,
            queryClient,
            args: record,
            kind: "table",
            title,
            item: { kind: "table", title, columns, rows, caption },
          });
        },
      },
      {
        name: "canvas_chart",
        description:
          "Create or update a chart item in the current session. Supports bar, line, area, pie, and donut charts. For schema details call canvas_doc_read(ref='canvas_chart').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Optional stable canvas item id to update." },
            title: { type: "string", description: "Short item title." },
            chart: chartSchema(),
            caption: { type: "string", description: "Optional caption." },
            window: canvasWindowSchema(),
          },
          required: ["title", "chart"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const title = requiredString(record.title, "title");
          const chart = normalizeChart(record.chart);
          const caption = stringValue(record.caption);
          return saveCanvasItem({
            token,
            queryClient,
            args: record,
            kind: "chart",
            title,
            item: { kind: "chart", title, chart, caption },
          });
        },
      },
      {
        name: "canvas_gallery",
        description:
          "Create or update an image gallery item in the current session. For schema details call canvas_doc_read(ref='canvas_gallery').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Optional stable canvas item id to update." },
            title: { type: "string", description: "Short item title." },
            items: {
              type: "array",
              description: "Image items with url/src or base64 data.",
              items: galleryItemSchema(),
            },
            layout: { type: "string", enum: ["grid", "row", "column"], description: "Gallery layout. Defaults to grid." },
            caption: { type: "string", description: "Optional caption." },
            mode: { type: "string", enum: ["replace", "append"], description: "Replace items or append to an existing gallery id." },
            window: canvasWindowSchema(),
          },
          required: ["items"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const title = stringValue(record.title);
          const items = normalizeGalleryItems(requiredArray(record.items, "items"));
          const layout = record.layout === undefined ? "" : galleryLayoutValue(record.layout);
          const caption = stringValue(record.caption);
          return saveGalleryItem({
            token,
            queryClient,
            args: record,
            title,
            items,
            layout,
            caption,
            mode: galleryModeValue(record.mode),
          });
        },
      },
      {
        name: "canvas_timeline",
        description:
          "Create or update a timeline item in the current session. Use for ordered plans, schedules, event recaps, milestones, and processes. For schema details call canvas_doc_read(ref='canvas_timeline').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Optional stable canvas item id to update." },
            title: { type: "string", description: "Short item title." },
            items: {
              type: "array",
              description: "Timeline entries in display order.",
              items: timelineItemSchema(),
            },
            caption: { type: "string", description: "Optional caption." },
            window: canvasWindowSchema(),
          },
          required: ["title", "items"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const title = requiredString(record.title, "title");
          const items = normalizeTimelineItems(requiredArray(record.items, "items"));
          const caption = stringValue(record.caption);
          return saveCanvasItem({
            token,
            queryClient,
            args: record,
            kind: "timeline",
            title,
            item: { kind: "timeline", title, items, caption },
          });
        },
      },
      {
        name: "canvas_grid",
        description:
          "Create or update one multi-block grid widget in the current session. Prefer this when complex results need multiple views such as metrics plus table, chart plus notes, or nested detail sections. Supports markdown, metric, table, gallery, chart, timeline, and one-level nested grid blocks. For later partial updates use canvas_grid_patch. For schema details call canvas_doc_read(ref='canvas_grid').",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Optional stable canvas item id to update." },
            title: { type: "string", description: "Grid widget title." },
            items: {
              type: "array",
              description: "Compact content blocks. Put kind-specific fields under data.",
              items: gridItemInputSchema(true),
            },
            columns: { type: "string", enum: ["auto", "1", "2", "3"], description: "Default layout columns." },
            layout: gridLayoutSchema(),
            caption: { type: "string", description: "Optional caption." },
            window: canvasWindowSchema(),
          },
          required: ["title", "items"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          const title = requiredString(record.title, "title");
          const items = normalizeGridItems(requiredArray(record.items, "items"), 0);
          const caption = stringValue(record.caption);
          return saveCanvasItem({
            token,
            queryClient,
            args: record,
            kind: "grid",
            title,
            item: {
              kind: "grid",
              title,
              items,
              ...(gridColumnsValue(record.columns) ? { columns: gridColumnsValue(record.columns) } : {}),
              ...(asRecord(record.layout) ? { layout: asRecord(record.layout) } : {}),
              caption,
            },
          });
        },
      },
      {
        name: "canvas_grid_patch",
        description:
          "Patch an existing grid widget without resending the full items array. Use after canvas_item_list when updating metadata, upserting/replacing/removing blocks by stable item.id, or changing block order. For complex item changes call canvas_doc_read(ref='canvas_grid'). For first render or intentional full replacement, use canvas_grid.",
        capability: "chat",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Existing grid widget id." },
            patch: {
              type: "object",
              description: "Optional grid-level metadata patch.",
              properties: {
                title: { type: "string" },
                columns: { type: "string", enum: ["auto", "1", "2", "3"] },
                layout: gridLayoutSchema(),
                caption: { type: "string" },
              },
              additionalProperties: false,
            },
            ops: {
              type: "array",
              description:
                "Patch operations. upsert shallow-merges by item.id or appends a new item; replace replaces an existing item; remove deletes by itemId; move repositions itemId before/after targetId; reorder moves listed top-level ids to the front and keeps unlisted ids after them.",
              items: {
                type: "object",
                properties: {
                  op: { type: "string", enum: ["upsert", "replace", "remove", "move", "reorder"] },
                  item: gridItemInputSchema(false),
                  itemId: { type: "string", description: "Required for remove and move." },
                  targetId: { type: "string", description: "Required for move." },
                  position: { type: "string", enum: ["before", "after"], description: "Defaults to before." },
                  order: { type: "array", items: { type: "string" }, description: "Required for reorder." },
                },
                required: ["op"],
                additionalProperties: false,
              },
            },
          },
          required: ["id"],
          additionalProperties: false,
        },
        handler: async (args) => {
          const record = requiredRecord(args);
          return patchGridItem({ token, queryClient, args: record });
        },
      },
    ] satisfies ToolDefinition[]).map((tool) => ({ ...tool, appID: CANVAS_APP_ID })),
    [queryClient, token],
  );
  const tools = useMemo(
    () => [...(desktopRuntime ? canvasTools : []), ...createInputFlowTools()],
    [canvasTools, desktopRuntime],
  );
  const handleRegistryChanged = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.apps() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserMCPSessions() });
  }, [queryClient]);

  useBrowserMCP({
    apps,
    endpoint,
    enabled: Boolean(token),
    onRegistryChanged: handleRegistryChanged,
    runtimeInfo,
    serverInfo,
    tools,
  });
}

async function saveCanvasItem({
  token,
  queryClient,
  args,
  kind,
  title,
  item,
}: {
  token: string;
  queryClient: ReturnType<typeof useQueryClient>;
  args: Record<string, unknown>;
  kind: string;
  title: string;
  item: unknown;
}) {
  const sessionID = requiredString(args._pudding_session_id, "_pudding_session_id");
  const id = stringValue(args.id);
  const body: CanvasItemPayload = {
    ...(id ? { id } : {}),
    kind,
    title,
    item,
    window: asRecord(args.window),
  };
  const saved = id
    ? await putCanvasItem(token, sessionID, id, body)
    : await createCanvasItem(token, sessionID, body);
  await queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(sessionID) });
  requestCanvasReveal(sessionID, saved.id);
  return jsonToolResult({
    ok: true,
    id: saved.id,
    kind: saved.kind,
    title: saved.title,
    sourceSessionID: saved.sourceSessionID,
  });
}

async function saveGalleryItem({
  token,
  queryClient,
  args,
  title,
  items,
  layout,
  caption,
  mode,
}: {
  token: string;
  queryClient: ReturnType<typeof useQueryClient>;
  args: Record<string, unknown>;
  title: string;
  items: Array<Record<string, string>>;
  layout: string;
  caption: string;
  mode: "replace" | "append";
}) {
  const sessionID = requiredString(args._pudding_session_id, "_pudding_session_id");
  const id = stringValue(args.id);
  let nextTitle = title || "Gallery";
  let nextItems = items;
  let nextLayout = layout || "grid";
  let nextCaption = caption;
  if (id) {
    const existing = (await listCanvasItems(token, sessionID)).items.find((item) => item.id === id);
    if (existing) {
      const payload = asRecord(existing.item);
      const existingKind = stringValue(payload?.kind) || existing.kind;
      if (existingKind !== "gallery") {
        throw new Error(`canvas item "${id}" is ${existingKind}, not gallery`);
      }
      if (mode === "append") {
        const previousItems = Array.isArray(payload?.items) ? normalizeGalleryItems(payload.items) : [];
        nextTitle = title || existing.title || stringValue(payload?.title) || nextTitle;
        nextItems = [...previousItems, ...items];
        nextLayout = layout || galleryLayoutValue(payload?.layout);
        nextCaption = caption || stringValue(payload?.caption);
      }
    }
  }
  const item = { kind: "gallery", title: nextTitle, items: nextItems, layout: nextLayout, caption: nextCaption };
  const body: CanvasItemPayload = {
    ...(id ? { id } : {}),
    kind: "gallery",
    title: nextTitle,
    item,
    window: asRecord(args.window),
  };
  const saved = id
    ? await putCanvasItem(token, sessionID, id, body)
    : await createCanvasItem(token, sessionID, body);
  await queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(sessionID) });
  requestCanvasReveal(sessionID, saved.id);
  return jsonToolResult({
    ok: true,
    id: saved.id,
    kind: saved.kind,
    title: saved.title,
    sourceSessionID: saved.sourceSessionID,
  });
}

async function patchGridItem({
  token,
  queryClient,
  args,
}: {
  token: string;
  queryClient: ReturnType<typeof useQueryClient>;
  args: Record<string, unknown>;
}) {
  const sessionID = requiredString(args._pudding_session_id, "_pudding_session_id");
  const id = requiredString(args.id, "id");
  const existing = (await listCanvasItems(token, sessionID)).items.find((item) => item.id === id);
  if (!existing) {
    throw new Error(`canvas_grid_patch: grid ${id} not found`);
  }
  const payload = { ...requiredRecord(existing.item) };
  const kind = stringValue(payload.kind) || existing.kind;
  if (kind !== "grid") {
    throw new Error(`canvas_grid_patch: item ${id} is ${kind || "unknown"}, not grid`);
  }

  let title = existing.title || stringValue(payload.title) || id;
  let items = normalizeGridItems(Array.isArray(payload.items) ? payload.items : [], 0);
  let changedPatch = false;
  let changedOps = 0;
  const patch = asRecord(args.patch);
  if (patch) {
    if (typeof patch.title === "string") {
      title = patch.title;
      changedPatch = true;
    }
    if (Object.prototype.hasOwnProperty.call(patch, "columns")) {
      const columns = gridColumnsValue(patch.columns);
      if (columns) {
        payload.columns = columns;
      } else {
        delete payload.columns;
      }
      changedPatch = true;
    }
    if (Object.prototype.hasOwnProperty.call(patch, "layout")) {
      const layout = asRecord(patch.layout);
      if (layout) {
        payload.layout = layout;
      } else {
        delete payload.layout;
      }
      changedPatch = true;
    }
    if (typeof patch.caption === "string") {
      payload.caption = patch.caption;
      changedPatch = true;
    }
  }

  for (const op of Array.isArray(args.ops) ? args.ops : []) {
    const operation = requiredRecord(op);
    const opKind = stringValue(operation.op);
    if (opKind === "remove") {
      const itemID = requiredString(operation.itemId, "itemId");
      const nextItems = removeGridItemList(items, itemID);
      if (nextItems === items) {
        throw new Error(`canvas_grid_patch: item ${itemID} not found`);
      }
      items = nextItems;
      changedOps += 1;
      continue;
    }
    if (opKind === "move") {
      const itemID = requiredString(operation.itemId, "itemId");
      const targetID = requiredString(operation.targetId, "targetId");
      const position = operation.position === "after" ? "after" : "before";
      const nextItems = moveGridItemList(items, itemID, targetID, position);
      if (nextItems === items) {
        throw new Error(`canvas_grid_patch: item ${itemID} already ${position} ${targetID}`);
      }
      items = nextItems;
      changedOps += 1;
      continue;
    }
    if (opKind === "reorder") {
      if (!Array.isArray(operation.order)) {
        throw new Error("canvas_grid_patch: reorder needs order");
      }
      const nextItems = reorderGridItemList(items, operation.order.map(String));
      if (nextItems !== items) {
        items = nextItems;
        changedOps += 1;
      }
      continue;
    }
    if (opKind === "upsert" || opKind === "replace") {
      const itemRecord = gridItemInputRecord(operation.item);
      const itemID = requiredString(itemRecord.id, "item.id");
      const hasKind = Boolean(stringValue(itemRecord.kind));
      if (opKind === "replace" && !hasKind) {
        throw new Error("canvas_grid_patch: replace item.kind is required");
      }
      const nextItems = updateGridItemList(items, itemID, (existingItem) =>
        opKind === "replace"
          ? normalizeGridItems([itemRecord], 0)[0]!
          : mergeGridItemPatch(existingItem, hasKind ? normalizeGridItems([itemRecord], 0)[0]! : itemRecord),
      );
      if (nextItems === items) {
        if (opKind === "replace") {
          throw new Error(`canvas_grid_patch: item ${itemID} not found`);
        }
        if (!hasKind) {
          throw new Error(`canvas_grid_patch: new upsert item ${itemID} needs kind`);
        }
        items = [...items, normalizeGridItems([itemRecord], 0)[0]!];
      } else {
        items = nextItems;
      }
      changedOps += 1;
      continue;
    }
    throw new Error(`canvas_grid_patch: unsupported op ${opKind}`);
  }

  if (!changedPatch && changedOps === 0) {
    return jsonToolResult({ ok: true, id, updated: false, count: items.length, changedOps: 0 });
  }

  const item = {
    ...payload,
    kind: "grid",
    title,
    items: normalizeGridItems(items, 0),
  };
  const body: CanvasItemPayload = {
    id,
    kind: "grid",
    title,
    item,
    window: existing.window,
  };
  const saved = await putCanvasItem(token, sessionID, id, body);
  await queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(sessionID) });
  requestCanvasReveal(sessionID, saved.id);
  return jsonToolResult({
    ok: true,
    id: saved.id,
    kind: saved.kind,
    title: saved.title,
    updated: true,
    count: items.length,
    changedOps,
  });
}

function mcpWebSocketURL(token: string) {
  const httpURL = new URL(apiURL(`/mcp/ws?token=${encodeURIComponent(token)}`), window.location.href);
  httpURL.protocol = httpURL.protocol === "https:" ? "wss:" : "ws:";
  return httpURL.toString();
}

const CANVAS_DOCS: Record<string, string> = {
  canvas_overview: [
    "# Canvas Tool Overview",
    "",
    "canvas_* tools create or update real UI widgets in the current session. Tool arguments must contain the real data to display; chat text is not copied into canvas automatically.",
    "",
    "Default behavior:",
    "",
    "- Put complex structured results on canvas instead of only answering in chat.",
    "- Use canvas when results contain more than a few rows, nested objects, repeated records, multiple sections, exact values users may inspect later, or data that should be exported or compared.",
    "- Keep the chat reply short after creating canvas content: summarize the count, highlight the key result, and mention that details are on canvas.",
    "",
    "- Use markdown for prose, steps, code, links, and compact notes.",
    "- Use table for structured rows, comparisons, query results, and lists.",
    "- Use chart for one chart based on real row data.",
    "- Use gallery for image-first content.",
    "- Use timeline for ordered plans, schedules, event recaps, milestones, and processes.",
    "- Use grid when one topic needs multiple blocks in one widget.",
    "- Before changing previous canvas work in multi-turn tasks, call canvas_item_list and decide whether to update an existing id or create a new widget.",
    "- Use canvas_item_inspect only when a lightweight list summary is not enough.",
    "- Only call canvas_item_remove or canvas_item_clear when the user clearly asks to close, remove, clear, or start over.",
  ].join("\n"),
  canvas_items: [
    "# Canvas Item Management",
    "",
    "Use these tools to manage existing canvas widgets in the current session.",
    "",
    "- canvas_item_list returns lightweight summaries only. Table summaries include row and column counts. Chart summaries include chart type, row count, and series count.",
    "- canvas_item_inspect returns full content for one id. Use it before patch-like updates when exact existing content matters.",
    "- canvas_item_remove removes one widget by id. Do not proactively clean up older widgets.",
    "- canvas_item_clear removes all widgets. Use only for explicit clear-canvas or start-over requests.",
    "- Reuse stable ids when updating existing work so the canvas does not accumulate duplicate widgets.",
  ].join("\n"),
  canvas_markdown: [
    "# Canvas Markdown",
    "",
    "Use canvas_markdown for readable prose, plans, summaries, instructions, code snippets, and short mixed content.",
    "",
    "- Required fields: title, content.",
    "- Optional field: id updates an existing markdown widget with the same id.",
    "- Optional window can provide x, y, w, h, and z. Omit it unless placement matters.",
    "- Keep markdown self-contained. Do not say content is elsewhere unless you also include the relevant content.",
    "- For structured rows, use canvas_table instead of a markdown table when the data may need export, inspection, or later updates.",
  ].join("\n"),
  canvas_table: [
    "# Canvas Table",
    "",
    "Use canvas_table for structured rows, comparisons, query results, rankings, inventories, and schedules. Rows must be real object data.",
    "",
    "- Prefer canvas_table for API/query results with more than a few rows, many fields, or exact values users may inspect, export, or compare later.",
    "- In chat, summarize the table briefly instead of repeating every row.",
    "- Required fields: title, columns, rows.",
    "- columns may be strings or objects like {key,label}. A string column uses the same key and label.",
    "- rows must be objects keyed by column key. Do not pass stringified JSON, placeholders, or omitted data.",
    "- Optional caption appears below the table.",
    "- Optional id updates an existing table widget with the same id.",
    "- For later updates, call canvas_item_list first. If the summary is not enough, call canvas_item_inspect to read the current rows.",
  ].join("\n"),
  canvas_chart: [
    "# Canvas Chart",
    "",
    "Use canvas_chart for one visual chart backed by real row data. Supported types: bar, line, area, pie, donut.",
    "",
    "- Required fields: title, chart.",
    "- chart.data is required and must be an array of objects.",
    "- For bar, line, and area charts, set x_key for the category/time field.",
    "- For pie and donut charts, set name_key for labels and value_key for values.",
    "- For single-series bar/line/area charts, set value_key or provide series.",
    "- series items use {key,label,color}. Omit series to infer numeric fields where possible.",
    "- Optional caption explains the chart. Keep it short.",
    "- If the user needs exact values or exportable data, use canvas_table alongside or instead of a chart.",
  ].join("\n"),
  canvas_gallery: [
    "# Canvas Gallery",
    "",
    "Use canvas_gallery for image-first content: screenshots, result sets, references, and visual comparisons.",
    "",
    "- Required fields: items. Provide title unless appending to an existing gallery.",
    "- Each item should include url or src. For inline base64 images, use data and optional mime.",
    "- Optional item fields: alt, caption.",
    "- Optional layout is grid, row, or column. Default is grid.",
    "- Optional mode is replace or append. append requires id and adds images to the existing gallery.",
    "- Optional id updates an existing gallery widget with the same id. Do not reuse an id from another widget kind.",
    "- Do not use gallery for mostly textual content. Use markdown or grid instead.",
  ].join("\n"),
  canvas_timeline: [
    "# Canvas Timeline",
    "",
    "Use canvas_timeline for time-ordered or sequence-ordered content: plans, schedules, milestones, incident recaps, trip itineraries, match schedules, and approval/deploy flows.",
    "",
    "- Required fields: title, items.",
    "- Each item requires title.",
    "- Optional item fields: group, date, time, status, description, meta, link, color.",
    "- group is the visible section label. If group is omitted, date is used as the section label.",
    "- status may be done, in_progress, planned, or blocked.",
    "- color may be gray, green, amber, red, sky, or violet.",
    "- Keep items in display order. Do not use timeline for unordered lists; use table, markdown, or grid instead.",
  ].join("\n"),
  canvas_grid: [
    "# Canvas Grid",
    "",
    "Use canvas_grid when one topic needs multiple blocks in one widget: dashboard summaries, chart plus notes, table plus explanation, image plus text, or comparisons.",
    "",
    "- Prefer canvas_grid for complex query results that combine summary metrics, detail tables, charts, timelines, galleries, or nested sections.",
    "- In chat, summarize the grid briefly instead of duplicating all block content.",
    "- Required fields: title, items.",
    "- Every item uses the compact shape {id?, kind, title?, variant?, surface?, span?, data}. Put kind-specific fields only inside data.",
    "- markdown data: {content}. metric data: {value, description?, icon?, color?}. table data: {columns, rows, caption?}.",
    "- chart data: {type, data, x_key?, name_key?, value_key?, series?}. gallery data: {items, layout?, caption?}. timeline data: {items, caption?}.",
    "- grid data: {items, layout?}; its child items use the same compact shape. Supported item kinds are markdown, metric, table, gallery, chart, timeline, and grid.",
    "- Create example: {id: 'sales', kind: 'table', title: 'Sales', data: {columns: ['region', 'amount'], rows: [{region: 'APAC', amount: 42}]}}.",
    "- Nested grid is only one level deep. Do not put kind=grid inside a nested grid.",
    "- Use item.id as a stable block id when future updates may need to target the block.",
    "- For partial updates to an existing grid, call canvas_item_list first, then use canvas_grid_patch with stable item.id. Patch item uses the same compact shape. Existing-item upsert may omit kind and include only changed fields in data; replace and new upsert require kind.",
    "- Patch example: {op: 'upsert', item: {id: 'sales', data: {rows: [{region: 'APAC', amount: 43}]}}}.",
    "- Use item.span.xs/sm/md/lg with values 1-12 to control width in the 12-column layout.",
    "- Metric items render as KPI cards. Use item.title plus data.value, optional data.description, data.icon, and data.color: default, green, amber, red, sky, or violet. Unrecognized metric data fields are appended to description.",
    "- Table, gallery, chart, and timeline data fields match their standalone tools.",
    "- Optional columns can be auto, 1, 2, or 3. Optional layout.gap controls inner gap.",
    "- If the user asks for later incremental updates, call canvas_item_list first and reuse the existing grid id.",
  ].join("\n"),
};

function canvasRuntimeAppDefinition(): RuntimeAppDefinition {
  return {
    id: CANVAS_APP_ID,
    name: "Canvas",
    version: "1.0",
    description: "Create and manage visual widgets in the connected Pudding desktop canvas.",
    requiredMode: "chat",
    defaultSkillID: CANVAS_APP_ID,
    skills: [
      {
        id: CANVAS_APP_ID,
        name: "Canvas",
        description: "Present structured or visual results on the desktop canvas.",
        path: "skills/canvas/SKILL.md",
        content: CANVAS_DOCS.canvas_overview || "",
      },
    ],
  };
}

function canvasWindowSchema() {
  return {
    type: "object",
    properties: {
      x: { type: "number" },
      y: { type: "number" },
      w: { type: "number" },
      h: { type: "number" },
      z: { type: "number" },
    },
    additionalProperties: false,
  };
}

function chartSchema() {
  return {
    type: "object",
    properties: {
      type: { type: "string", enum: ["bar", "line", "area", "pie", "donut"] },
      x_key: { type: "string", description: "Category/time key for bar, line, and area charts." },
      name_key: { type: "string", description: "Name key for pie and donut charts." },
      value_key: { type: "string", description: "Value key for pie/donut, or single-series charts." },
      series: {
        type: "array",
        description: "Series to draw. Omit to infer numeric fields.",
        items: {
          type: "object",
          properties: {
            key: { type: "string" },
            label: { type: "string" },
            color: { type: "string" },
          },
          required: ["key"],
          additionalProperties: false,
        },
      },
      data: {
        type: "array",
        description: "Chart rows.",
        items: { type: "object", additionalProperties: true },
      },
    },
    required: ["data"],
    additionalProperties: false,
  };
}

function galleryItemSchema() {
  return {
    type: "object",
    properties: {
      url: { type: "string" },
      src: { type: "string" },
      data: { type: "string", description: "Base64 image data without data: prefix." },
      mime: { type: "string", description: "Image MIME for base64 data." },
      alt: { type: "string" },
      caption: { type: "string" },
    },
    additionalProperties: false,
  };
}

function timelineItemSchema() {
  return {
    type: "object",
    properties: {
      group: { type: "string", description: "Visible section label, such as Jul 3, Phase 1, or Day 2." },
      date: { type: "string", description: "Date label used as group when group is omitted." },
      time: { type: "string", description: "Optional time label." },
      title: { type: "string", description: "Entry title." },
      status: { type: "string", enum: ["done", "in_progress", "planned", "blocked"] },
      description: { type: "string" },
      meta: { type: "string", description: "Small secondary label." },
      link: { type: "string", description: "Optional URL." },
      color: { type: "string", enum: ["gray", "green", "amber", "red", "sky", "violet"] },
    },
    required: ["title"],
    additionalProperties: false,
  };
}

function gridLayoutSchema() {
  return {
    type: "object",
    properties: {
      columns: { type: "number", enum: [12], description: "Always 12." },
      gap: { type: "number", description: "Gap in px." },
    },
    additionalProperties: false,
  };
}

function gridItemInputSchema(requireKind: boolean): Record<string, unknown> {
  return {
    type: "object",
    description: requireKind
      ? "Compact grid block. Put kind-specific fields under data."
      : "Compact patch block. Existing upsert may omit kind; replace and new upsert require it.",
    properties: {
      id: { type: "string", description: "Stable block id. Required for patch items." },
      kind: { type: "string", enum: ["markdown", "metric", "table", "gallery", "chart", "timeline", "grid"] },
      title: { type: "string", description: "Optional block title." },
      variant: { type: "string", enum: ["hero", "normal", "compact", "subtle"] },
      surface: { type: "string", enum: ["default", "tinted"] },
      span: {
        type: "object",
        description: "Optional responsive xs/sm/md/lg widths from 1 to 12.",
        additionalProperties: { type: "integer", minimum: 1, maximum: 12 },
      },
      data: {
        type: "object",
        description: "Kind-specific fields. Call canvas_doc_read(ref='canvas_grid') for the compact data contract.",
        additionalProperties: true,
      },
    },
    required: requireKind ? ["kind", "data"] : ["id"],
    additionalProperties: false,
  };
}

function requiredRecord(value: unknown): Record<string, unknown> {
  const record = asRecord(value);
  if (!record) {
    throw new Error("arguments must be an object");
  }
  return record;
}

function sessionIDFromArgs(args: unknown): string {
  const record = requiredRecord(args);
  return requiredString(record._pudding_session_id, "_pudding_session_id");
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
}

function requiredString(value: unknown, field: string): string {
  const text = stringValue(value).trim();
  if (!text) {
    throw new Error(`${field} is required`);
  }
  return text;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function requiredArray(value: unknown, field: string): unknown[] {
  if (!Array.isArray(value)) {
    throw new Error(`${field} is required`);
  }
  return value;
}

function normalizeChart(value: unknown): Record<string, unknown> {
  const record = requiredRecord(value);
  const data = requiredArray(record.data, "chart.data").map((row) => {
    const object = asRecord(row);
    if (!object) {
      throw new Error("chart.data items must be objects");
    }
    return object;
  });
  return {
    type: chartTypeValue(record.type),
    ...(stringValue(record.x_key) ? { x_key: stringValue(record.x_key) } : {}),
    ...(stringValue(record.name_key) ? { name_key: stringValue(record.name_key) } : {}),
    ...(stringValue(record.value_key) ? { value_key: stringValue(record.value_key) } : {}),
    ...(Array.isArray(record.series) ? { series: normalizeChartSeries(record.series) } : {}),
    data,
  };
}

function chartTypeValue(value: unknown): string {
  return value === "line" || value === "area" || value === "pie" || value === "donut" ? value : "bar";
}

function normalizeChartSeries(value: unknown[]): Array<Record<string, string>> {
  return value.map((item) => {
    const record = requiredRecord(item);
    const key = requiredString(record.key, "series.key");
    return {
      key,
      ...(stringValue(record.label) ? { label: stringValue(record.label) } : {}),
      ...(stringValue(record.color) ? { color: stringValue(record.color) } : {}),
    };
  });
}

function normalizeGalleryItems(value: unknown[]): Array<Record<string, string>> {
  if (value.length === 0) {
    throw new Error("items must not be empty");
  }
  return value.map((item, index) => {
    const record = requiredRecord(item);
    const url = stringValue(record.url) || stringValue(record.src);
    const data = stringValue(record.data);
    if (!url && !data) {
      throw new Error(`items[${index}] must include url, src, or data`);
    }
    return {
      ...(url ? { url } : {}),
      ...(data ? { data } : {}),
      ...(stringValue(record.mime) ? { mime: stringValue(record.mime) } : {}),
      ...(stringValue(record.alt) ? { alt: stringValue(record.alt) } : {}),
      ...(stringValue(record.caption) ? { caption: stringValue(record.caption) } : {}),
    };
  });
}

function normalizeTimelineItems(value: unknown[]): Array<Record<string, string>> {
  if (value.length === 0) {
    throw new Error("items must not be empty");
  }
  return value.map((item, index) => {
    const record = requiredRecord(item);
    const title = requiredString(record.title, `items[${index}].title`);
    const status = timelineStatusValue(record.status);
    const color = timelineColorValue(record.color);
    return {
      ...(stringValue(record.group) ? { group: stringValue(record.group) } : {}),
      ...(stringValue(record.date) ? { date: stringValue(record.date) } : {}),
      ...(stringValue(record.time) ? { time: stringValue(record.time) } : {}),
      title,
      ...(status ? { status } : {}),
      ...(stringValue(record.description) ? { description: stringValue(record.description) } : {}),
      ...(stringValue(record.meta) ? { meta: stringValue(record.meta) } : {}),
      ...(stringValue(record.link) ? { link: stringValue(record.link) } : {}),
      ...(color ? { color } : {}),
    };
  });
}

function galleryLayoutValue(value: unknown): string {
  return value === "row" || value === "column" ? value : "grid";
}

function galleryModeValue(value: unknown): "replace" | "append" {
  return value === "append" ? "append" : "replace";
}

function gridColumnsValue(value: unknown): string {
  return value === "1" || value === "2" || value === "3" || value === "auto" ? value : "";
}

function normalizeGridItems(value: unknown[], depth: number): Array<Record<string, unknown>> {
  return value.map((item, index) => {
    const record = gridItemInputRecord(item);
    const kind = gridItemKind(record.kind, depth);
    const title = stringValue(record.title);
    const base: Record<string, unknown> = {
      ...(stringValue(record.id) ? { id: stringValue(record.id) } : { id: `${kind}-${index + 1}` }),
      kind,
      ...(title ? { title } : {}),
      ...(gridVariantValue(record.variant) ? { variant: gridVariantValue(record.variant) } : {}),
      ...(gridSurfaceValue(record.surface) ? { surface: gridSurfaceValue(record.surface) } : {}),
      ...(normalizeSpan(record.span) ? { span: normalizeSpan(record.span) } : {}),
      ...(stringValue(record.caption) ? { caption: stringValue(record.caption) } : {}),
    };
    if (kind === "markdown") {
      return { ...base, content: stringValue(record.content) || stringValue(record.text) };
    }
    if (kind === "metric") {
      const description = metricDescriptionValue(record);
      return {
        ...base,
        value: metricValue(record.value),
        ...(description ? { description } : {}),
        ...(metricIconValue(record.icon) ? { icon: metricIconValue(record.icon) } : {}),
        ...(metricColorValue(record.color) ? { color: metricColorValue(record.color) } : {}),
      };
    }
    if (kind === "table") {
      return {
        ...base,
        columns: Array.isArray(record.columns) ? record.columns : [],
        rows: Array.isArray(record.rows) ? record.rows : [],
      };
    }
    if (kind === "gallery") {
      return {
        ...base,
        items: Array.isArray(record.items) ? normalizeGalleryItems(record.items) : [],
        ...(record.layout === undefined ? {} : { layout: galleryLayoutValue(record.layout) }),
      };
    }
    if (kind === "timeline") {
      return {
        ...base,
        items: Array.isArray(record.items) ? normalizeTimelineItems(record.items) : [],
      };
    }
    if (kind === "chart") {
      return {
        ...base,
        chart: normalizeChart(asRecord(record.chart) ?? record),
      };
    }
    return {
      ...base,
      items: Array.isArray(record.items) ? normalizeGridItems(record.items, depth + 1) : [],
      ...(asRecord(record.layout) ? { layout: asRecord(record.layout) } : {}),
    };
  });
}

function gridItemInputRecord(value: unknown): Record<string, unknown> {
  const record = requiredRecord(value);
  const data = asRecord(record.data);
  if (!data) {
    return record;
  }
  const metadata = { ...record };
  delete metadata.data;
  return { ...data, ...metadata };
}

function updateGridItemList(
  items: Array<Record<string, unknown>>,
  itemID: string,
  update: (item: Record<string, unknown>) => Record<string, unknown>,
): Array<Record<string, unknown>> {
  let changed = false;
  const next = items.map((item) => {
    if (stringValue(item.id) === itemID) {
      changed = true;
      return update(item);
    }
    if (item.kind === "grid" && Array.isArray(item.items)) {
      const childItems = item.items as Array<Record<string, unknown>>;
      const nested = updateGridItemList(childItems, itemID, update);
      if (nested !== childItems) {
        changed = true;
        return { ...item, items: nested };
      }
    }
    return item;
  });
  return changed ? next : items;
}

function removeGridItemList(items: Array<Record<string, unknown>>, itemID: string): Array<Record<string, unknown>> {
  let changed = false;
  const next: Array<Record<string, unknown>> = [];
  for (const item of items) {
    if (stringValue(item.id) === itemID) {
      changed = true;
      continue;
    }
    if (item.kind === "grid" && Array.isArray(item.items)) {
      const childItems = item.items as Array<Record<string, unknown>>;
      const nested = removeGridItemList(childItems, itemID);
      if (nested !== childItems) {
        changed = true;
        next.push({ ...item, items: nested });
        continue;
      }
    }
    next.push(item);
  }
  return changed ? next : items;
}

function moveGridItemList(
  items: Array<Record<string, unknown>>,
  itemID: string,
  targetID: string,
  position: "before" | "after",
): Array<Record<string, unknown>> {
  if (itemID === targetID) {
    return items;
  }
  const from = items.findIndex((item) => stringValue(item.id) === itemID);
  const to = items.findIndex((item) => stringValue(item.id) === targetID);
  if (from >= 0 || to >= 0) {
    if (from < 0) {
      throw new Error(`canvas_grid_patch: item ${itemID} not found`);
    }
    if (to < 0) {
      throw new Error(`canvas_grid_patch: target item ${targetID} not found`);
    }
    const moving = items[from]!;
    const rest = items.filter((_, index) => index !== from);
    const targetIndex = rest.findIndex((item) => stringValue(item.id) === targetID);
    const insertAt = position === "before" ? targetIndex : targetIndex + 1;
    return [...rest.slice(0, insertAt), moving, ...rest.slice(insertAt)];
  }

  let changed = false;
  const next = items.map((item) => {
    if (item.kind !== "grid" || !Array.isArray(item.items)) {
      return item;
    }
    const childItems = item.items as Array<Record<string, unknown>>;
    const nested = moveGridItemList(childItems, itemID, targetID, position);
    if (nested === childItems) {
      return item;
    }
    changed = true;
    return { ...item, items: nested };
  });
  return changed ? next : items;
}

function reorderGridItemList(items: Array<Record<string, unknown>>, order: string[]): Array<Record<string, unknown>> {
  const ids = order.map((id) => id.trim()).filter(Boolean);
  if (ids.length === 0) {
    return items;
  }
  const byID = new Map(items.map((item) => [stringValue(item.id), item]));
  const picked: Array<Record<string, unknown>> = [];
  const seen = new Set<string>();
  for (const id of ids) {
    const item = byID.get(id);
    if (!item) {
      throw new Error(`canvas_grid_patch: item ${id} not found`);
    }
    if (seen.has(id)) {
      throw new Error(`canvas_grid_patch: duplicate item ${id}`);
    }
    picked.push(item);
    seen.add(id);
  }
  const rest = items.filter((item) => !seen.has(stringValue(item.id)));
  const next = [...picked, ...rest];
  const unchanged = next.length === items.length && next.every((item, index) => item === items[index]);
  return unchanged ? items : next;
}

function mergeGridItemPatch(existing: Record<string, unknown>, patch: Record<string, unknown>): Record<string, unknown> {
  if (stringValue(patch.kind) && patch.kind !== existing.kind) {
    return patch;
  }
  return { ...existing, ...patch, kind: existing.kind };
}

function gridItemKind(value: unknown, depth: number): string {
  if (value === undefined || value === "" || value === "markdown") {
    return "markdown";
  }
  if (value === "metric" || value === "table" || value === "gallery" || value === "chart" || value === "timeline") {
    return value;
  }
  if (value === "grid") {
    if (depth === 0) {
      return "grid";
    }
    throw new Error("canvas_grid: nested grid blocks support only one level");
  }
  throw new Error(`canvas_grid: unsupported item kind ${String(value)}`);
}

function gridVariantValue(value: unknown): string {
  return value === "hero" || value === "compact" || value === "subtle" || value === "normal" ? value : "";
}

function gridSurfaceValue(value: unknown): string {
  return value === "tinted" || value === "default" ? value : "";
}

function metricColorValue(value: unknown): string {
  return value === "green" || value === "amber" || value === "red" || value === "sky" || value === "violet" || value === "default"
    ? value
    : "";
}

function metricIconValue(value: unknown): string {
  return value === "activity" ||
    value === "calendar" ||
    value === "clock" ||
    value === "gauge" ||
    value === "hash" ||
    value === "money" ||
    value === "percent" ||
    value === "users"
    ? value
    : "";
}

function metricDescriptionValue(record: Record<string, unknown>): string {
  const parts: string[] = [];
  const description = stringValue(record.description);
  if (description) {
    parts.push(description);
  }
  const knownKeys = new Set(["id", "kind", "title", "variant", "surface", "span", "caption", "value", "description", "icon", "color"]);
  for (const [key, value] of Object.entries(record)) {
    if (value === undefined || value === null || value === "") {
      continue;
    }
    if (key === "icon") {
      if (!metricIconValue(value)) {
        parts.push(`${key}: ${metricDescriptionPart(value)}`);
      }
      continue;
    }
    if (key === "color") {
      if (!metricColorValue(value)) {
        parts.push(`${key}: ${metricDescriptionPart(value)}`);
      }
      continue;
    }
    if (knownKeys.has(key)) {
      continue;
    }
    parts.push(`${key}: ${metricDescriptionPart(value)}`);
  }
  return parts.join("\n");
}

function metricDescriptionPart(value: unknown): string {
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function metricValue(value: unknown): string | number | boolean {
  return typeof value === "number" || typeof value === "boolean" ? value : stringValue(value);
}

function timelineStatusValue(value: unknown): string {
  return value === "done" || value === "in_progress" || value === "planned" || value === "blocked" ? value : "";
}

function timelineColorValue(value: unknown): string {
  return value === "gray" || value === "green" || value === "amber" || value === "red" || value === "sky" || value === "violet"
    ? value
    : "";
}

function normalizeSpan(value: unknown): Record<string, number> | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  const out: Record<string, number> = {};
  for (const key of ["xs", "sm", "md", "lg"]) {
    const value = spanValue(record[key]);
    if (value) {
      out[key] = value;
    }
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function spanValue(value: unknown): number | undefined {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return undefined;
  }
  return Math.min(12, Math.max(1, Math.round(value)));
}

function jsonToolResult(value: unknown) {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(value),
      },
    ],
  };
}

function canvasItemSummary(item: CanvasItem) {
  const payload = asRecord(item.item);
  const kind = stringValue(payload?.kind) || item.kind;
  return {
    id: item.id,
    kind,
    title: item.title || stringValue(payload?.title),
    sourceSessionID: item.sourceSessionID,
    createdBySessionID: item.createdBySessionID,
    updatedBySessionID: item.updatedBySessionID,
    createdAt: item.createdAt,
    updatedAt: item.updatedAt,
    window: item.window,
    summary: payloadSummary(kind, payload),
  };
}

function canvasItemInspection(item: CanvasItem) {
  return {
    id: item.id,
    canvasID: item.canvasID,
    kind: item.kind,
    title: item.title,
    sourceSessionID: item.sourceSessionID,
    createdBySessionID: item.createdBySessionID,
    updatedBySessionID: item.updatedBySessionID,
    visible: item.visible,
    createdAt: item.createdAt,
    updatedAt: item.updatedAt,
    window: item.window,
    item: item.item,
  };
}

function payloadSummary(kind: string, payload: Record<string, unknown> | undefined) {
  if (!payload) {
    return { kind, fields: 0 };
  }
  if (kind === "markdown") {
    const content = stringValue(payload.content) || stringValue(payload.markdown);
    return { chars: content.length };
  }
  if (kind === "table") {
    const rows = Array.isArray(payload.rows) ? payload.rows : [];
    const columns = Array.isArray(payload.columns) ? payload.columns : [];
    return { rows: rows.length, columns: columns.length };
  }
  if (kind === "chart") {
    const chart = asRecord(payload.chart) ?? payload;
    const data = Array.isArray(chart?.data) ? chart.data : [];
    const series = Array.isArray(chart?.series) ? chart.series : [];
    return { type: chartTypeValue(chart?.type), rows: data.length, series: series.length };
  }
  if (kind === "gallery") {
    const items = Array.isArray(payload.items) ? payload.items : [];
    return { items: items.length, layout: galleryLayoutValue(payload.layout) };
  }
  if (kind === "timeline") {
    const items = Array.isArray(payload.items) ? payload.items : [];
    const groups = Array.from(
      new Set(
        items
          .map((item) => {
            const record = asRecord(item);
            return stringValue(record?.group) || stringValue(record?.date);
          })
          .filter(Boolean),
      ),
    );
    return { items: items.length, groups };
  }
  if (kind === "grid") {
    const blocks = Array.isArray(payload.items) ? payload.items : Array.isArray(payload.blocks) ? payload.blocks : [];
    return {
      blocks: blocks.length,
      blockIDs: blocks.map((block) => stringValue(asRecord(block)?.id)).filter(Boolean),
    };
  }
  return { fields: Object.keys(payload).length };
}
