import type { ToolDefinition } from "@/mcp/browserMCP";
import { showInputFlow } from "@/state/inputFlowStore";

export function createInputFlowTools(): ToolDefinition[] {
  return [
    {
      name: "collect_user_input",
      description:
        "Show an interactive UI that collects structured information from the user. Use this instead of asking the user to type answers in chat whenever the answers can be represented as choices, multiple choices, short text, phone, number, date, confirmation, or several form fields. Use type='form' with steps for ordinary questions. Use type='repeat' only when the user may add multiple records with the same fields, such as several room types and quantities. If choices depend on live data, fetch them first and pass the actual options to this tool. This tool returns immediately after showing the UI; the completed answers arrive later as a new user message. Do not continue work that depends on those answers in the current turn.",
      capability: "chat",
      inputSchema: {
        type: "object",
        properties: {
          title: { type: "string", description: "Short, user-facing title." },
          description: { type: "string", description: "Optional concise context for the user." },
          type: {
            type: "string",
            enum: ["form", "repeat"],
            description: "Use form for normal information collection. Use repeat only for multiple same-shaped records.",
          },
          steps: {
            type: "array",
            description: "Fields shown once. Required when type='form'.",
            items: inputStepSchema([
              "single_select",
              "multi_select",
              "quick_number",
              "number_input",
              "text_input",
              "phone_input",
              "date_input",
              "confirm",
            ]),
          },
          resultKey: { type: "string", description: "Result key for repeated records. Defaults to items." },
          minItems: { type: "number", description: "Minimum items the user should select. Defaults to 1." },
          maxItems: { type: "number", description: "Optional maximum item count." },
          repeatSteps: {
            type: "array",
            description: "Fields repeated for every record. Required when type='repeat'.",
            items: inputStepSchema(["single_select", "quick_number"]),
          },
          nextSteps: {
            type: "array",
            description: "Optional fields shown once after all repeated records are collected.",
            items: inputStepSchema([
              "single_select",
              "multi_select",
              "quick_number",
              "number_input",
              "text_input",
              "phone_input",
              "date_input",
              "confirm",
            ]),
          },
          afterItem: {
            type: "object",
            properties: {
              title: { type: "string" },
              actions: {
                type: "array",
                items: {
                  type: "object",
                  properties: {
                    id: { type: "string", enum: ["continue", "done"] },
                    label: { type: "string" },
                  },
                  required: ["id", "label"],
                  additionalProperties: false,
                },
              },
            },
            additionalProperties: false,
          },
        },
        required: ["title", "type"],
        additionalProperties: false,
      },
      handler: async (args) => {
        const record = requiredRecord(args);
        const sessionID = requiredString(record._pudding_session_id, "_pudding_session_id");
        const title = requiredString(record.title, "title");
        if (record.type === "form") {
          if (!Array.isArray(record.steps) || record.steps.length === 0) {
            throw new Error("steps is required when type=form");
          }
        } else if (record.type === "repeat") {
          if (!Array.isArray(record.repeatSteps) || record.repeatSteps.length === 0) {
            throw new Error("repeatSteps is required when type=repeat");
          }
        } else {
          throw new Error("type must be form or repeat");
        }
        const request = showInputFlow({
          args: record,
          sessionID,
          title,
        });
        return jsonToolResult({
          ok: true,
          requestID: request.id,
          status: "awaiting_user",
          title,
        });
      },
    },
  ];
}

function inputStepSchema(types: string[]) {
  return {
    type: "object",
    properties: {
      id: { type: "string", description: "Stable key used in the returned result." },
      type: { type: "string", enum: types, description: "Input control shown to the user." },
      title: { type: "string", description: "Short question or field label." },
      description: { type: "string", description: "Optional concise help text." },
      placeholder: { type: "string", description: "Optional input placeholder." },
      required: { type: "boolean", description: "Defaults to true. Set false to allow skipping." },
      options: {
        type: "array",
        description: "Required for select fields; suggested values for quick_number.",
        items: {
          anyOf: [
            { type: "number" },
            { type: "string" },
            {
              type: "object",
              properties: {
                value: {},
                title: { type: "string" },
                label: { type: "string" },
                description: { type: "string" },
                data: { type: "object", additionalProperties: true },
              },
              additionalProperties: true,
            },
          ],
        },
      },
      min: { type: "number", description: "Minimum number, or minimum selections for multi_select." },
      max: { type: "number", description: "Maximum number, or maximum selections for multi_select." },
      maxFrom: { type: "string", description: "Path from prior selected step, such as room.data.availNum." },
      customLabel: { type: "string", description: "Label for custom numeric input." },
    },
    required: ["id", "type", "title"],
    additionalProperties: false,
  };
}

function requiredRecord(value: unknown): Record<string, unknown> {
  const record = value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
  if (!record) {
    throw new Error("arguments must be an object");
  }
  return record;
}

function requiredString(value: unknown, field: string): string {
  const text = typeof value === "string" ? value.trim() : "";
  if (!text) {
    throw new Error(`${field} is required`);
  }
  return text;
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
