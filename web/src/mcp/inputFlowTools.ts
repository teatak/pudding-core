import type { ToolDefinition } from "@/mcp/browserMCP";
import { waitForInputFlow } from "@/state/inputFlowStore";

export function createInputFlowTools(): ToolDefinition[] {
  return [
    {
      name: "ui_input_flow",
      description:
        "Render an interactive, multi-step user input flow above the composer and wait until the user completes or cancels it. Use this when you need the user to choose structured values, such as selecting one or more room types and quantities, then collecting a few follow-up fields. Current version supports type='repeat' with repeatSteps of type='option_list' and 'quick_number', plus optional nextSteps of type='text_input', 'phone_input', 'option_list', 'quick_number', or 'confirm'. The tool call blocks until completion; the result is returned as JSON in the tool result.",
      capability: "chat",
      inputSchema: {
        type: "object",
        properties: {
          title: { type: "string", description: "Short title for the input flow." },
          description: { type: "string", description: "Optional short helper text." },
          type: { type: "string", enum: ["repeat"], description: "Only repeat is supported in this version." },
          resultKey: { type: "string", description: "Key for the returned array. Defaults to items." },
          minItems: { type: "number", description: "Minimum items the user should select. Defaults to 1." },
          maxItems: { type: "number", description: "Optional maximum item count." },
          repeatSteps: {
            type: "array",
            description: "Steps repeated for each item.",
            items: inputStepSchema(["option_list", "quick_number"]),
          },
          nextSteps: {
            type: "array",
            description: "Steps shown once after the repeated selection is complete.",
            items: inputStepSchema(["text_input", "phone_input", "option_list", "quick_number", "confirm"]),
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
            additionalProperties: true,
          },
        },
        required: ["title", "type", "repeatSteps"],
        additionalProperties: true,
      },
      handler: async (args) => {
        const record = requiredRecord(args);
        const sessionID = requiredString(record._pudding_session_id, "_pudding_session_id");
        const title = requiredString(record.title, "title");
        if (record.type !== "repeat") {
          throw new Error("ui_input_flow only supports type=repeat");
        }
        if (!Array.isArray(record.repeatSteps) || record.repeatSteps.length === 0) {
          throw new Error("repeatSteps is required");
        }
        const result = await waitForInputFlow({
          args: record,
          sessionID,
          title,
        });
        return jsonToolResult(result);
      },
    },
  ];
}

function inputStepSchema(types: string[]) {
  return {
    type: "object",
    properties: {
      id: { type: "string" },
      type: { type: "string", enum: types },
      title: { type: "string" },
      description: { type: "string" },
      placeholder: { type: "string" },
      required: { type: "boolean" },
      options: {
        type: "array",
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
      min: { type: "number" },
      max: { type: "number" },
      maxFrom: { type: "string", description: "Path from prior selected step, such as room.data.availNum." },
      customLabel: { type: "string", description: "Label for custom numeric input." },
    },
    required: ["id", "type", "title"],
    additionalProperties: true,
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
