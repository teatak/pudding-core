import type { ToolDefinition } from "@/mcp/browserMCP";
import { showInputFlow } from "@/state/inputFlowStore";

export function createInputFlowTools(): ToolDefinition[] {
  return [
    {
      name: "builtin_request_user_input",
      description: [
        "Collect structured non-secret answers through a sequential UI. Each step asks one question and shows at most one input box; put separate fields in separate steps, never nested fields or inputs. Use type='form' with steps for ordinary questions, and type='repeat' only for multiple same-shaped records. Answers are sent together when completed; skipping omits only the current optional field. Keep questions and options brief and self-contained; prefer 2–3 choices. Fetch live data before constructing dependent options. Never request passwords, tokens, API keys, private keys, or credentials; use dedicated App connection/settings flows.",
        "waitSeconds controls ONLY how long this tool waits for the user (default 60, integer 0–300): positive values pause this turn until answered, timed out, dismissed, or cancelled; 0 returns awaiting_user immediately so you can continue independent work. User interactions renew an unexpired wait by waitSeconds; they never restart a wait that already ended. An in-time answer returns status=answered with answer.text and answer.parts directly in this tool result, not a second user message. After timeout or an asynchronous return, answers can still arrive later as a user message linked to the original question.",
        "The panel has a separate 60-second inactivity auto-collapse timer, reset by user interaction; waitSeconds=10 does NOT close the panel after 10 seconds. Neither countdown is shown upfront: the panel shows its final 10 seconds; the model wait shows only its final min(10, waitSeconds/2) seconds. Interaction resets the panel timer and any still-active model wait. Users can reopen unanswered questions from the original tool row, even after the turn finishes; reopening does not restart an ended model wait.",
        "timeout, dismissed, or cancelled means no answer, never consent/refusal or permission to guess; perform dependent actions only after receiving the needed answer. Do not re-ask merely because the wait timed out.",
      ].join(" "),
      capability: "chat",
      inputSchema: {
        type: "object",
        properties: {
          waitSeconds: {type: "integer", minimum: 0, maximum: 300, default: 60, description: "LLM wait only, NOT panel lifetime. 0 is asynchronous; 1–300 waits for an answer. Interaction renews an active wait. The panel separately collapses after 60 seconds of inactivity."},
          title: { type: "string", description: "Short, user-facing title." },
          type: {
            type: "string",
            enum: ["form", "repeat"],
            description: "Use form for normal information collection. Use repeat only for multiple same-shaped records.",
          },
          steps: {
            type: "array",
            description: "Ordered steps shown one at a time, one field per step. Required when type='form'.",
            items: inputStepSchema([
              "single_select",
              "multi_select",
              "number_input",
              "text_input",
              "phone_input",
              "date_input",
              "confirm",
            ]),
          },
          resultKey: { type: "string", description: "Result key for repeated records. Defaults to items." },
          minItems: { type: "number", description: "Minimum non-empty records, at least 1. Defaults to 1; entirely skipped records do not count." },
          maxItems: { type: "number", description: "Optional maximum item count." },
          repeatSteps: {
            type: "array",
            description: "Ordered steps repeated for each record, one field at a time. Required when type='repeat'.",
            items: inputStepSchema(["single_select", "number_input"]),
          },
          nextSteps: {
            type: "array",
            description: "Optional list of ordered steps shown one at a time after record collection. Each field is required unless required=false.",
            items: inputStepSchema([
              "single_select",
              "multi_select",
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
        const id = requiredString(record._pudding_request_id, "_pudding_request_id");
        if (record.waitSeconds !== undefined && (!Number.isInteger(record.waitSeconds) || Number(record.waitSeconds) < 0 || Number(record.waitSeconds) > 300)) {
          throw new Error("waitSeconds must be an integer from 0 to 300");
        }
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
        assertNoSensitiveInputSteps(record);
        const request = showInputFlow({
          id,
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

const sensitiveInputPattern = /password|passwd|passcode|secret|api[\s_-]*key|access[\s_-]*token|refresh[\s_-]*token|private[\s_-]*key|密码|口令|密钥|令牌/i;

function assertNoSensitiveInputSteps(record: Record<string, unknown>) {
  for (const field of ["steps", "repeatSteps", "nextSteps"]) {
    const steps = record[field];
    if (!Array.isArray(steps)) {
      continue;
    }
    for (const value of steps) {
      const step = value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
      if (!step) {
        continue;
      }
      const searchable = [step.id, step.title, step.placeholder]
        .filter((item): item is string => typeof item === "string")
        .join(" ");
      if (sensitiveInputPattern.test(searchable)) {
        throw new Error("credential fields must use the dedicated App connection or settings flow");
      }
    }
  }
}

function inputStepSchema(types: string[]) {
  return {
    type: "object",
    properties: {
      id: { type: "string", description: "Stable result key, unique within this step list." },
      type: {
        type: "string",
        enum: types,
        description: "One field: single_select chooses one supplied value, optionally with custom text; multi_select chooses several supplied values; text/phone/number/date show one input; confirm asks for confirmation. Use number_input only when the answer must be numeric.",
      },
      title: { type: "string", description: "One concise, self-contained question or field label. Include only essential context." },
      placeholder: { type: "string", description: "Optional input placeholder." },
      required: { type: "boolean", description: "Defaults to true. Set false to allow skipping only this field, omitting its answer. Applies to form, repeatSteps and nextSteps. A confirm step cannot be skipped." },
      options: {
        type: "array",
        description: "Choices for single_select or multi_select. Supply meaningful values; they are returned as provided, without numeric conversion.",
        items: {
          anyOf: [
            { type: "number" },
            { type: "string" },
            {
              type: "object",
              properties: {
                value: { description: "Model-defined answer returned when this option is chosen. Any JSON value is allowed; it need not be numeric." },
                title: { type: "string" },
                label: { type: "string" },
                data: { type: "object", additionalProperties: true },
              },
              additionalProperties: false,
            },
          ],
        },
      },
      min: { type: "number", description: "Minimum number, or minimum selections for multi_select." },
      max: { type: "number", description: "Maximum number, or maximum selections for multi_select." },
      maxFrom: { type: "string", description: "Path from prior selected step, such as room.data.availNum." },
      allowCustom: { type: "boolean", description: "For single_select only: show one always-visible custom text input alongside the options. Defaults to false. Custom text is returned as a string under this step's id." },
      customLabel: { type: "string", description: "Label for the single_select custom input when allowCustom=true." },
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
