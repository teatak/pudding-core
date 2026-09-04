import { consumeLaunchParam } from "@/state/launchParams";
import type { InputFlowRequest } from "@/state/inputFlowStore";
import type { AssistantOverlayPart } from "@/state/overlayStore";
import type { ActiveTurnPlan } from "@/state/turnPlan";

const STORAGE_KEY = "pudding.composerTestState";
const composerTestStates = ["approval", "app-approval", "interaction", "steps"] as const;

type ComposerApproval = Extract<AssistantOverlayPart, { type: "approval" }>;
type ComposerTestState = (typeof composerTestStates)[number];

export type ComposerTestPresentation = {
  approval?: ComposerApproval;
  inputFlow?: InputFlowRequest;
  plan?: ActiveTurnPlan;
};

let activeState: ComposerTestState | "" = "";

export function initComposerTestState() {
  const launchState = consumeLaunchParam("composerTestState");
  if (!import.meta.env.DEV) {
    sessionStorage.removeItem(STORAGE_KEY);
    activeState = "";
    return;
  }
  if (launchState) {
    const normalized = normalizeComposerTestState(launchState);
    if (normalized) {
      sessionStorage.setItem(STORAGE_KEY, normalized);
    } else {
      sessionStorage.removeItem(STORAGE_KEY);
    }
  }
  activeState = normalizeComposerTestState(sessionStorage.getItem(STORAGE_KEY) || "");
}

export function composerTestPresentation(sessionID: string): ComposerTestPresentation | undefined {
  if (!import.meta.env.DEV || !activeState) {
    return undefined;
  }
  const turnID = `composer-test-${activeState}`;
  if (activeState === "approval") {
    return {
      approval: {
        type: "approval",
        approvalID: "composer-test-approval",
        approvalKind: "tool_call",
        callID: "composer-test-shell",
        payload: {
          command: "npm --prefix web run build",
          execution: "sandbox",
          operation: "shell",
        },
        reason: "用于检查命令审批浮层在真实对话中的位置与层级。",
        sessionID,
      },
    };
  }
  if (activeState === "app-approval") {
    return {
      approval: {
        type: "approval",
        approvalID: "composer-test-app-approval",
        approvalKind: "tool_call",
        callID: "composer-test-computer",
        payload: {
          appID: "com.postmanlabs.mac",
          operation: "computer_use_app",
          scope: "computer",
        },
        sessionID,
      },
    };
  }
  if (activeState === "interaction") {
    return {
      inputFlow: {
        args: {
          type: "form",
          title: "选择执行方式",
          description: "用于检查结构化交互浮层在真实对话中的位置与层级。",
          steps: [
            {
              id: "mode",
              type: "single_select",
              title: "下一步怎么处理？",
              options: [
                { value: "continue", title: "继续执行", description: "按当前方案继续。" },
                { value: "adjust", title: "调整方案", description: "先修改方案再继续。" },
              ],
            },
            {
              id: "note",
              type: "text_input",
              title: "补充说明",
              placeholder: "输入可选说明",
              required: false,
            },
          ],
        },
        createdAt: "2026-01-01T00:00:00.000Z",
        id: "composer-test-interaction",
        sessionID,
        title: "选择执行方式",
      },
    };
  }
  return {
    plan: {
      currentStep: 2,
      plan: [
        { status: "completed", step: "分析当前页面结构" },
        { status: "in_progress", step: "调整底部组件层级" },
        { status: "pending", step: "检查窄窗口布局" },
        { status: "pending", step: "完成回归验证" },
      ],
      totalSteps: 4,
      turnID,
    },
  };
}

function normalizeComposerTestState(value: string): ComposerTestState | "" {
  const normalized = value.trim().toLowerCase();
  return composerTestStates.find((state) => state === normalized) || "";
}
