import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import {
  createCanvasItem,
  putCanvasItem,
  type CanvasItemPayload,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { apiURL } from "@/state/apiBase";
import { useBrowserMCP, type ToolDefinition } from "@/mcp/browserMCP";

export function useCanvasMCP(token: string) {
  const queryClient = useQueryClient();
  const endpoint = useMemo(() => (token ? mcpWebSocketURL(token) : ""), [token]);
  const serverInfo = useMemo(() => ({ name: "pudding-canvas", version: "1.0" }), []);
  const tools = useMemo<ToolDefinition[]>(
    () => [
      {
        name: "canvas_markdown",
        description: "Create or update a markdown item on the shared canvas.",
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
        description: "Create or update a table item on the shared canvas.",
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
        description: "Create or update a chart item on the shared canvas. Supports bar, line, area, pie, and donut charts.",
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
    ],
    [queryClient, token],
  );

  useBrowserMCP({
    endpoint,
    enabled: Boolean(token),
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
  await queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems() });
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify({
          ok: true,
          id: saved.id,
          kind: saved.kind,
          title: saved.title,
          sourceSessionID: saved.sourceSessionID,
        }),
      },
    ],
  };
}

function mcpWebSocketURL(token: string) {
  const httpURL = new URL(apiURL(`/mcp/ws?token=${encodeURIComponent(token)}`), window.location.href);
  httpURL.protocol = httpURL.protocol === "https:" ? "wss:" : "ws:";
  return httpURL.toString();
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

function requiredRecord(value: unknown): Record<string, unknown> {
  const record = asRecord(value);
  if (!record) {
    throw new Error("arguments must be an object");
  }
  return record;
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
