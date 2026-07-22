import type { ConversationTurn, Message } from "@/api/client";

export const TURN_PLAN_TOOL = "builtin_plan_update";

export type TurnPlanStatus = "pending" | "in_progress" | "completed";

export type TurnPlanStep = {
  status: TurnPlanStatus;
  step: string;
};

export type TurnPlan = {
  currentStep: number;
  plan: TurnPlanStep[];
  totalSteps: number;
};

export type ActiveTurnPlan = TurnPlan & {
  turnID: string;
};

export function parseTurnPlan(content: string | undefined): TurnPlan | undefined {
  if (!content) {
    return undefined;
  }
  let value: unknown;
  try {
    value = JSON.parse(content);
  } catch {
    return undefined;
  }
  if (!isRecord(value) || !Array.isArray(value.plan)) {
    return undefined;
  }
  const plan: TurnPlanStep[] = [];
  for (const item of value.plan) {
    if (
      !isRecord(item) ||
      typeof item.step !== "string" ||
      !isTurnPlanStatus(item.status) ||
      !item.step.trim()
    ) {
      return undefined;
    }
    plan.push({ status: item.status, step: item.step.trim() });
  }
  const currentStep = Number(value.currentStep);
  const totalSteps = Number(value.totalSteps);
  if (
    plan.length < 2 ||
    totalSteps !== plan.length ||
    !Number.isInteger(currentStep) ||
    currentStep < 1 ||
    currentStep > totalSteps
  ) {
    return undefined;
  }
  return { currentStep, plan, totalSteps };
}

export function latestTurnPlan(messages: Message[]): TurnPlan | undefined {
  for (let messageIndex = messages.length - 1; messageIndex >= 0; messageIndex -= 1) {
    const parts = messages[messageIndex].parts;
    for (let partIndex = parts.length - 1; partIndex >= 0; partIndex -= 1) {
      const part = parts[partIndex];
      if (part.type === "tool_result" && part.name === TURN_PLAN_TOOL && part.ok) {
        const parsed = parseTurnPlan(part.content);
        if (parsed) {
          return parsed;
        }
      }
    }
  }
  return undefined;
}

export function activeTurnPlan(turns: ConversationTurn[]): ActiveTurnPlan | undefined {
  for (let index = turns.length - 1; index >= 0; index -= 1) {
    const turn = turns[index];
    if (turn.status !== "running") {
      continue;
    }
    const plan = latestTurnPlan(turn.messages);
    return plan ? { ...plan, turnID: turn.id } : undefined;
  }
  return undefined;
}

function isTurnPlanStatus(value: unknown): value is TurnPlanStatus {
  return value === "pending" || value === "in_progress" || value === "completed";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
