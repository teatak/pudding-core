import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  BookOpenCheck,
  Braces,
  Camera,
  CircleAlert,
  CircleCheck,
  CircleX,
  Clock3,
  CloudSun,
  Copy,
  Download,
  FileCheck,
  FileInput,
  FileOutput,
  FilePenLine,
  FileSearch,
  GitBranch,
  GitCommitHorizontal,
  GitCompareArrows,
  Globe,
  History,
  Info,
  Keyboard,
  LayoutGrid,
  Lightbulb,
  ListChecks,
  ListTree,
  LocateFixed,
  MessageSquareMore,
  MessageSquareText,
  MousePointerClick,
  MoveRight,
  MoveVertical,
  PackageMinus,
  PackageOpen,
  PackagePlus,
  PanelTop,
  Paperclip,
  Plug,
  RotateCw,
  ScanLine,
  ScanSearch,
  Search,
  Send,
  ShieldCheck,
  Sparkles,
  Square,
  SquareTerminal,
  TextCursorInput,
  Trash2,
  Waypoints,
  Wrench,
  type LucideIcon,
} from "@/components/icons";
import type { AssistantOverlay, AssistantOverlayPart, TurnPhaseState } from "@/state/overlayStore";

type Translate = (key: string) => string;

export type TurnActivitySummary = {
  active: boolean;
  detail?: string;
  failed?: boolean;
  icon: LucideIcon;
  label: string;
  phase?: TurnPhaseState["phase"];
};

export function describeFloatingTurnActivity({
  overlay,
  phase,
  running,
  t,
}: {
  overlay?: AssistantOverlay;
  phase?: TurnPhaseState;
  running: boolean;
  t: Translate;
}): TurnActivitySummary {
  if (!running) {
    if (phase?.phase === "error" || overlay?.status === "failed") {
      return { active: false, failed: true, icon: CircleAlert, label: t("agentConsole.turnFailed") };
    }
    if (
      phase?.phase === "cancelled" ||
      overlay?.status === "cancelled" ||
      overlay?.interrupted
    ) {
      return { active: false, icon: Square, label: t("agentConsole.turnStopped") };
    }
    return { active: false, icon: CircleCheck, label: t("agentConsole.turnCompleted") };
  }

  const approval = findLastPart(overlay?.parts, "approval");
  if (phase?.phase === "awaiting_approval" || (approval && !approval.status)) {
    return {
      active: true,
      icon: ShieldCheck,
      label: t("agentConsole.needsApproval"),
      phase: "awaiting_approval",
    };
  }

  const tool = findLastPart(overlay?.parts, "tool");
  if (tool && isActiveTool(tool, phase)) {
    return describeToolActivity(tool, phase, t);
  }

  switch (phase?.phase) {
    case "submitting":
      return { active: true, icon: Send, label: t("transcript.phaseSubmitting"), phase: phase.phase };
    case "awaiting_model":
      return {
        active: true,
        icon: Sparkles,
        label: t(phase.activity === "steering" ? "transcript.phaseSteering" : "transcript.phaseAwaitingModel"),
        phase: phase.phase,
      };
    case "thinking":
      return { active: true, icon: Lightbulb, label: t("transcript.thinking"), phase: phase.phase };
    case "streaming_text":
      return { active: true, icon: MessageSquareText, label: t("agentConsole.generatingReply"), phase: phase.phase };
    case "streaming_tool_args":
      return { active: true, icon: Wrench, label: t("transcript.toolReadingArgs"), phase: phase.phase };
    case "executing_tool":
      return { active: true, icon: Wrench, label: t("transcript.toolRunning"), phase: phase.phase };
    case "awaiting_followup":
      return { active: true, icon: MessageSquareMore, label: t("transcript.phaseAwaitingFollowup"), phase: phase.phase };
    case "error":
      return { active: false, failed: true, icon: CircleAlert, label: t("agentConsole.turnFailed"), phase: phase.phase };
    case "cancelled":
      return { active: false, icon: Square, label: t("agentConsole.turnStopped"), phase: phase.phase };
    default:
      if (overlay?.text.trim()) {
        return { active: true, icon: MessageSquareText, label: t("agentConsole.generatingReply"), phase: "streaming_text" };
      }
      return { active: true, icon: Sparkles, label: t("transcript.phaseAwaitingModel"), phase: "awaiting_model" };
  }
}

function describeToolActivity(
  tool: Extract<AssistantOverlayPart, { type: "tool" }>,
  phase: TurnPhaseState | undefined,
  t: Translate,
): TurnActivitySummary {
  const name = tool.name || "";
  const detail = toolActivityDetail(name, tool.argsText);
  const label = t("transcript.toolRunningName").replace(
    "{name}",
    toolDisplayName(name, t("transcript.tool"), t),
  );

  return {
    active: true,
    detail,
    icon: toolIcon(name),
    label,
    phase: phase?.phase === "streaming_tool_args" ? "streaming_tool_args" : "executing_tool",
  };
}

function isActiveTool(
  tool: Extract<AssistantOverlayPart, { type: "tool" }>,
  phase?: TurnPhaseState,
) {
  return (
    tool.phase === "streaming_args" ||
    tool.phase === "running" ||
    phase?.phase === "streaming_tool_args" ||
    phase?.phase === "executing_tool"
  );
}

function toolActivityDetail(name: string, argsText: string) {
  const args = parseObject(argsText);
  if (!args) {
    return "";
  }
  if (name === "builtin_file_copy" || name === "builtin_file_move") {
    const from = shortPath(readString(args, "from_path"));
    const to = shortPath(readString(args, "to_path"));
    return from && to ? `${from} → ${to}` : from || to;
  }
  if (name.startsWith("builtin_file_") || name.startsWith("builtin_code_")) {
    return shortPath(readString(args, "path"));
  }
  if (
    name === "builtin_browser_open" ||
    name === "builtin_web_fetch" ||
    name === "builtin_rest_request" ||
    name === "builtin_graphql_request"
  ) {
    return shortURL(readString(args, "url") || readString(args, "endpoint"));
  }
  if (name === "builtin_web_search" || name.endsWith("_search")) {
    return shortValue(readString(args, "query"));
  }
  if (name === "builtin_command_run") {
    return shortValue(readString(args, "command").split(/\r?\n/, 1)[0]);
  }
  if (name.startsWith("builtin_browser_")) {
    return shortValue(
      readString(args, "url") ||
        readString(args, "ref") ||
        readString(args, "selector") ||
        readString(args, "tab_id"),
    );
  }
  return shortValue(
    readString(args, "path") ||
      readString(args, "url") ||
      readString(args, "query") ||
      readString(args, "name"),
  );
}

function readString(value: Record<string, unknown>, key: string) {
  return typeof value[key] === "string" ? String(value[key]).trim() : "";
}

function shortPath(value: string) {
  const normalized = value.replaceAll("\\", "/").replace(/\/$/, "");
  const parts = normalized.split("/").filter(Boolean);
  return shortValue(parts.slice(-2).join("/"));
}

function shortURL(value: string) {
  try {
    const url = new URL(value);
    return shortValue(`${url.hostname}${url.pathname === "/" ? "" : url.pathname}`);
  } catch {
    return shortValue(value);
  }
}

const maximumToolDetailCharacters = 80;

function shortValue(value: string) {
  const normalized = value.replace(/\s+/g, " ").trim();
  const characters = Array.from(normalized);
  if (characters.length <= maximumToolDetailCharacters) {
    return normalized;
  }
  return `${characters.slice(0, maximumToolDetailCharacters).join("")}…`;
}

function parseObject(value: string) {
  if (!value.trim()) {
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(value);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : undefined;
  } catch {
    return undefined;
  }
}

function findLastPart<T extends AssistantOverlayPart["type"]>(
  parts: AssistantOverlayPart[] | undefined,
  type: T,
): Extract<AssistantOverlayPart, { type: T }> | undefined {
  if (!parts) {
    return undefined;
  }
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    const part = parts[index];
    if (part.type === type) {
      return part as Extract<AssistantOverlayPart, { type: T }>;
    }
  }
  return undefined;
}

export function toolIcon(name: string | undefined): LucideIcon {
  if (name?.startsWith("canvas_")) {
    return LayoutGrid;
  }
  if (name?.startsWith("app_mcp__")) {
    return Plug;
  }
  const known: Record<string, LucideIcon> = {
    builtin_attachment_export: FileOutput,
    // Display-only compatibility for canonical history; this tool is no longer callable.
    builtin_attachment_read_image: Paperclip,
    builtin_app_load: PackageOpen,
    builtin_app_unload: PackageMinus,
    builtin_app_save: PackagePlus,
    builtin_browser_back: ArrowLeft,
    builtin_browser_click: MousePointerClick,
    builtin_browser_close: CircleX,
    builtin_browser_forward: ArrowRight,
    builtin_browser_observe: FileSearch,
    builtin_browser_open: Globe,
    builtin_browser_reload: RotateCw,
    builtin_browser_screenshot: Camera,
    builtin_browser_scroll: MoveVertical,
    builtin_browser_status: PanelTop,
    builtin_browser_type: Keyboard,
    builtin_camera_capture: Camera,
    builtin_command_run: SquareTerminal,
    builtin_command_session: Keyboard,
    builtin_code_symbols: Braces,
    builtin_code_definition: LocateFixed,
    builtin_code_references: Waypoints,
    builtin_code_diagnostics: CircleAlert,
    builtin_code_rename: TextCursorInput,
    builtin_desktop_screenshot: ScanLine,
    builtin_file_copy: Copy,
    builtin_file_delete: Trash2,
    builtin_file_list: ListTree,
    builtin_file_move: MoveRight,
    builtin_file_patch: FileCheck,
    builtin_file_read: BookOpenCheck,
    builtin_file_search: Search,
    builtin_file_slice: BookOpenCheck,
    builtin_file_stat: Info,
    builtin_file_write: FilePenLine,
    builtin_media_read: Paperclip,
    builtin_git_diff: GitCompareArrows,
    builtin_git_log: History,
    builtin_git_stage: FileInput,
    builtin_git_status: GitBranch,
    builtin_git_unstage: FileOutput,
    builtin_git_commit: GitCommitHorizontal,
    builtin_graphql_introspect: ScanSearch,
    builtin_graphql_request: Braces,
    builtin_graphql_search: Search,
    builtin_history_get_message: MessageSquareText,
    builtin_history_search: History,
    builtin_rest_request: Send,
    builtin_skill_read: BookOpen,
    builtin_skill_validate: BookOpenCheck,
    builtin_time_get_current: Clock3,
    builtin_weather_get: CloudSun,
    builtin_web_fetch: Download,
    builtin_web_search: Search,
    request_capability: ShieldCheck,
    builtin_request_user_input: MessageSquareMore,
    builtin_plan_update: ListChecks,
  };
  return name ? known[name] || Wrench : Wrench;
}

export function toolDisplayName(name: string | undefined, fallback: string, t: Translate) {
  if (!name) {
    return fallback;
  }
  if (name.startsWith("app_mcp__")) {
    return t("transcript.toolAppMCP");
  }
  const known: Record<string, string> = {
    builtin_graphql_request: t("transcript.toolGraphQLRequest"),
    builtin_graphql_introspect: t("transcript.toolGraphQLIntrospect"),
    builtin_graphql_search: t("transcript.toolGraphQLSearch"),
    builtin_history_get_message: t("transcript.toolHistoryGetMessage"),
    builtin_history_search: t("transcript.toolHistorySearch"),
    builtin_app_load: t("transcript.toolAppLoad"),
    builtin_app_unload: t("transcript.toolAppUnload"),
    builtin_app_save: t("transcript.toolAppSave"),
    builtin_command_run: t("transcript.toolCommandRun"),
    builtin_command_session: t("transcript.toolCommandSession"),
    builtin_code_symbols: t("transcript.toolCodeSymbols"),
    builtin_code_definition: t("transcript.toolCodeDefinition"),
    builtin_code_references: t("transcript.toolCodeReferences"),
    builtin_code_diagnostics: t("transcript.toolCodeDiagnostics"),
    builtin_code_rename: t("transcript.toolCodeRename"),
    builtin_file_copy: t("transcript.toolFileCopy"),
    builtin_file_delete: t("transcript.toolFileDelete"),
    builtin_file_list: t("transcript.toolFileList"),
    builtin_file_move: t("transcript.toolFileMove"),
    builtin_file_patch: t("transcript.toolFilePatch"),
    builtin_file_read: t("transcript.toolFileRead"),
    builtin_attachment_export: t("transcript.toolAttachmentExport"),
    builtin_attachment_read_image: t("transcript.toolMediaRead"),
    builtin_file_search: t("transcript.toolFileSearch"),
    builtin_file_slice: t("transcript.toolFileSlice"),
    builtin_file_stat: t("transcript.toolFileStat"),
    builtin_file_write: t("transcript.toolFileWrite"),
    builtin_media_read: t("transcript.toolMediaRead"),
    builtin_git_diff: t("transcript.toolGitDiff"),
    builtin_git_log: t("transcript.toolGitLog"),
    builtin_git_stage: t("transcript.toolGitStage"),
    builtin_git_status: t("transcript.toolGitStatus"),
    builtin_git_unstage: t("transcript.toolGitUnstage"),
    builtin_git_commit: t("transcript.toolGitCommit"),
    builtin_camera_capture: t("transcript.toolCameraCapture"),
    builtin_desktop_screenshot: t("transcript.toolDesktopScreenshot"),
    builtin_browser_click: t("transcript.toolBrowserClick"),
    builtin_browser_back: t("transcript.toolBrowserBack"),
    builtin_browser_close: t("transcript.toolBrowserClose"),
    builtin_browser_forward: t("transcript.toolBrowserForward"),
    builtin_browser_observe: t("transcript.toolBrowserObserve"),
    builtin_browser_open: t("transcript.toolBrowserOpen"),
    builtin_browser_reload: t("transcript.toolBrowserReload"),
    builtin_browser_screenshot: t("transcript.toolBrowserScreenshot"),
    builtin_browser_scroll: t("transcript.toolBrowserScroll"),
    builtin_browser_status: t("transcript.toolBrowserStatus"),
    builtin_browser_type: t("transcript.toolBrowserType"),
    builtin_rest_request: t("transcript.toolRESTRequest"),
    builtin_skill_read: t("transcript.toolSkillRead"),
    builtin_skill_validate: t("transcript.toolSkillValidate"),
    builtin_time_get_current: t("transcript.toolTimeCurrent"),
    builtin_weather_get: t("transcript.toolWeatherGet"),
    builtin_web_fetch: t("transcript.toolWebFetch"),
    builtin_web_search: t("transcript.toolWebSearch"),
    builtin_request_user_input: t("transcript.toolRequestUserInput"),
    builtin_plan_update: t("transcript.toolPlanUpdate"),
    canvas_chart: t("transcript.toolCanvasChart"),
    canvas_doc_read: t("transcript.toolCanvasDocRead"),
    canvas_gallery: t("transcript.toolCanvasGallery"),
    canvas_grid: t("transcript.toolCanvasGrid"),
    canvas_grid_patch: t("transcript.toolCanvasGridPatch"),
    canvas_item_clear: t("transcript.toolCanvasItemClear"),
    canvas_item_inspect: t("transcript.toolCanvasItemInspect"),
    canvas_item_list: t("transcript.toolCanvasItemList"),
    canvas_item_remove: t("transcript.toolCanvasItemRemove"),
    canvas_markdown: t("transcript.toolCanvasMarkdown"),
    canvas_table: t("transcript.toolCanvasTable"),
    canvas_timeline: t("transcript.toolCanvasTimeline"),
    request_capability: t("transcript.toolRequestCapability"),
  };
  if (known[name]) {
    return known[name];
  }
  return name.replace(/^builtin_/, "").replace(/^mcp_/, "").split("_").filter(Boolean).join(" ");
}
