import {
  ArrowLeft,
  ArrowRight,
  Boxes,
  Camera,
  ChevronDown,
  ChevronRight,
  Copy,
  Database,
  FileDiff,
  FilePenLine,
  FileSearch,
  FileText,
  GitBranch,
  GitCommitHorizontal,
  Globe,
  Info,
  Keyboard,
  LayoutGrid,
  ListChecks,
  ListTree,
  MousePointerClick,
  MoveRight,
  PackageOpen,
  Paperclip,
  RotateCw,
  Route,
  Search,
  Save,
  SquareTerminal,
  TextCursorInput,
  Trash2,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import {
  Children,
  isValidElement,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
  type ToggleEvent,
} from "react";
import ReactMarkdown, { type Components, type UrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";

import { type ContentPart, type Message } from "@/api/client";
import { ImageLightbox, type ImageLightboxItem } from "@/components/ImageLightbox";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { openExternalURL } from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";
import { getShikiCodeRenderer, type CodeBlockRenderer } from "@/lib/shiki";
import type { AssistantOverlay, AssistantOverlayPart, TurnPhaseState } from "@/state/overlayStore";

import { CodeToolDetails, ToolHoverCopyButton, codeToolSummary, isCodeToolName } from "./CodeToolDetails";
import { useElapsedDuration } from "./time";
import { textFromContentParts, type TranscriptDisplaySettings, type TurnDisclosureState, type TurnPartVM } from "./types";

type CompactProcessPart = {
  hiddenParts: TurnPartVM[];
  key: string;
  type: "process_compact";
};

type RenderTurnPart = TurnPartVM | CompactProcessPart;

const processFileToolNames = new Set([
  "builtin_file_copy",
  "builtin_file_delete",
  "builtin_file_list",
  "builtin_file_move",
  "builtin_file_patch",
  "builtin_file_read",
  "builtin_file_search",
  "builtin_file_slice",
  "builtin_file_stat",
  "builtin_file_write",
]);

export function TurnParts({
  disclosure,
  displaySettings,
  parts,
  sessionID,
  token = "",
  turnID,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  parts: TurnPartVM[];
  sessionID?: string;
  token?: string;
  turnID: string;
}) {
  const showReasoningContent = displaySettings?.showReasoning ?? true;
  const showRawToolInfo = displaySettings?.showRawToolInfo ?? true;
  const renderParts = compactProcessRuns(parts);
  return (
    <>
      {renderParts.map((part, index) => {
        const partKey = part.key || `${part.type}:${index}`;
        const disclosureKey = `${turnID}:${partKey}`;
        return renderTranscriptPart({
          disclosure,
          disclosureKey,
          part,
          partKey,
          sessionID,
          showReasoningContent,
          showRawToolInfo,
          token,
        });
      })}
    </>
  );
}

function renderTranscriptPart({
  disclosure,
  disclosureKey,
  part,
  partKey,
  sessionID,
  showReasoningContent,
  showRawToolInfo,
  token,
}: {
  disclosure?: TurnDisclosureState;
  disclosureKey: string;
  part: RenderTurnPart;
  partKey: string;
  sessionID?: string;
  showReasoningContent: boolean;
  showRawToolInfo: boolean;
  token: string;
}) {
  switch (part.type) {
    case "text":
      return <MarkdownBody key={partKey} text={part.text} token={token} />;
    case "attachment":
      return <AttachmentPart key={partKey} attachment={part.attachment} token={token} />;
    case "thought":
      return (
        <ThoughtPart
          key={partKey}
          active={part.active}
          defaultOpen={disclosure?.isOpen(disclosureKey) || false}
          showContent={showReasoningContent}
          text={part.text}
          onOpenChange={(open) => disclosure?.setOpen(disclosureKey, open)}
        />
      );
    case "approval":
      return null;
    case "tool_use":
      return (
        <ToolUsePart
          key={partKey}
          defaultOpen={disclosure?.isOpen(disclosureKey) || false}
          part={part}
          sessionID={sessionID}
          showRawInfo={showRawToolInfo}
          onOpenChange={(open) => disclosure?.setOpen(disclosureKey, open)}
        />
      );
    case "tool_result":
      return null;
    case "process_compact":
      return (
        <ProcessCompactPart
          key={partKey}
          defaultOpen={disclosure?.isOpen(disclosureKey) || false}
          hiddenParts={part.hiddenParts}
          renderPart={(hiddenPart, hiddenIndex) => {
            const hiddenKey = hiddenPart.key || `${hiddenPart.type}:${hiddenIndex}`;
            return renderTranscriptPart({
              disclosure,
              disclosureKey: `${disclosureKey}:${hiddenKey}`,
              part: hiddenPart,
              partKey: hiddenKey,
              sessionID,
              showReasoningContent,
              showRawToolInfo,
              token,
            });
          }}
          onOpenChange={(open) => disclosure?.setOpen(disclosureKey, open)}
        />
      );
  }
}

function compactProcessRuns(parts: TurnPartVM[]): RenderTurnPart[] {
  const out: RenderTurnPart[] = [];
  let processParts: TurnPartVM[] = [];
  let runIndex = 0;

  const flush = () => {
    if (processParts.length > 1) {
      out.push({
        hiddenParts: processParts,
        key: `process-compact:${runIndex}:${processParts[0]?.key || 0}`,
        type: "process_compact",
      });
      runIndex += 1;
    } else {
      out.push(...processParts);
    }
    processParts = [];
  };

  for (const part of parts) {
    if (!isProcessPart(part) || shouldKeepProcessPartVisible(part)) {
      flush();
      out.push(part);
      continue;
    }
    processParts.push(part);
  }
  flush();
  return out;
}

function isProcessPart(part: TurnPartVM) {
  return part.type === "thought" || part.type === "tool_use";
}

function shouldKeepProcessPartVisible(part: TurnPartVM) {
  if (part.type === "thought") {
    return Boolean(part.active);
  }
  if (part.type === "tool_use") {
    return Boolean(part.active);
  }
  return false;
}

function PartIcon({ icon: Icon }: { icon: LucideIcon }) {
  return (
    <span className="relative z-[1] inline-flex h-6 w-4 shrink-0 items-center justify-center text-muted-foreground/65">
      <Icon aria-hidden="true" className="size-3.5" strokeWidth={2} />
    </span>
  );
}

function toolPartIcon(part: Extract<TurnPartVM, { type: "tool_use" }>): LucideIcon {
  const name = part.name || part.resultName || "";
  if (name.startsWith("canvas_")) {
    return LayoutGrid;
  }
  if (name.startsWith("app_mcp__")) {
    return Wrench;
  }
  const known: Record<string, LucideIcon> = {
    builtin_attachment_read_image: FileSearch,
    builtin_app_load: PackageOpen,
    builtin_browser_back: ArrowLeft,
    builtin_browser_click: MousePointerClick,
    builtin_browser_close: Globe,
    builtin_browser_forward: ArrowRight,
    builtin_browser_observe: FileSearch,
    builtin_browser_open: Globe,
    builtin_browser_reload: RotateCw,
    builtin_browser_screenshot: Camera,
    builtin_browser_scroll: MousePointerClick,
    builtin_browser_status: Globe,
    builtin_browser_type: Keyboard,
    builtin_camera_capture: Camera,
    builtin_command_poll: SquareTerminal,
    builtin_command_run: SquareTerminal,
    builtin_command_start: SquareTerminal,
    builtin_command_stop: SquareTerminal,
    builtin_toolkit_load: Boxes,
    builtin_code_symbols: Search,
    builtin_code_definition: Route,
    builtin_code_references: ListTree,
    builtin_code_diagnostics: ListChecks,
    builtin_code_rename: TextCursorInput,
    builtin_desktop_screenshot: Camera,
    builtin_file_copy: Copy,
    builtin_file_delete: Trash2,
    builtin_file_list: ListTree,
    builtin_file_move: MoveRight,
    builtin_file_patch: FilePenLine,
    builtin_file_read: FileText,
    builtin_file_search: Search,
    builtin_file_slice: FileText,
    builtin_file_stat: Info,
    builtin_file_write: Save,
    builtin_git_diff: FileDiff,
    builtin_git_log: GitBranch,
    builtin_git_stage: GitBranch,
    builtin_git_status: GitBranch,
    builtin_git_unstage: GitBranch,
    builtin_git_commit: GitCommitHorizontal,
    builtin_patch_apply: FileDiff,
    builtin_patch_propose: FileDiff,
    builtin_project_inspect: ListTree,
    builtin_project_instructions: FileText,
    builtin_graphql_introspect: Database,
    builtin_graphql_request: Database,
    builtin_graphql_search: Search,
    builtin_history_get_message: FileText,
    builtin_history_search: Search,
    builtin_rest_request: Database,
    builtin_skill_read: FileText,
    builtin_skill_submit: FileText,
    builtin_skill_validate: FileSearch,
    builtin_time_get_current: Wrench,
    builtin_weather_get: Wrench,
    builtin_web_fetch: FileSearch,
    builtin_web_search: Search,
    request_capability: Wrench,
    collect_user_input: ListChecks,
  };
  return known[name] || Wrench;
}

export function partsFromMessages(messages: Message[]): TurnPartVM[] {
  return withPartKeys(
    mergeToolParts(
      messages.flatMap((message) =>
        message.parts.flatMap((part) => {
          const viewPart = partFromContentPart(part);
          return viewPart ? [viewPart] : [];
        }),
      ),
    ),
  );
}

export function assistantTextFromMessages(messages: Message[]) {
  const textParts: string[] = [];
  for (const message of messages) {
    const text = textFromContentParts(message.parts);
    if (text.trim()) {
      textParts.push(text);
    }
  }
  return textParts.join("\n\n");
}

export function partsFromOverlay(
  overlay: AssistantOverlay,
  streamedText: string,
  activePhaseName: TurnPhaseState["phase"] | undefined,
  activePhaseUpdatedAt?: string,
): TurnPartVM[] {
  const overlayParts = orderedOverlayParts(overlay);
  const lastThoughtIndex = findLastOverlayPartIndex(overlayParts, "thought");
  const lastToolIndex = findLastOverlayPartIndex(overlayParts, "tool");
  const hasTextPart = overlayParts.some((part) => part.type === "text");
  return withPartKeys([
    ...overlayParts.flatMap((part, index): TurnPartVM[] => {
      if (part.type === "text") {
        return [{ type: "text", text: part.text }];
      }
      if (part.type === "thought") {
        return [{ type: "thought", active: activePhaseName === "thinking" && index === lastThoughtIndex, text: part.text }];
      }
      if (part.type === "approval") {
        return [{
          type: "approval",
          active: activePhaseName === "awaiting_approval" && !part.status,
          approvalID: part.approvalID,
          approvalKind: part.approvalKind,
          payload: part.payload,
          reason: part.reason,
          risk: part.risk,
          sessionID: part.sessionID,
          status: part.status,
          title: part.title,
        }];
      }
      const active =
        index === lastToolIndex &&
        (activePhaseName === "streaming_tool_args" ||
          activePhaseName === "executing_tool" ||
          activePhaseName === "awaiting_followup");
      return [
        {
          type: "tool_use",
          active,
          argsText: part.argsText,
          dotPhase: active ? activePhaseName : toolPhaseDot(part.phase),
          id: part.callID,
          liveStderr: part.liveStderr,
          liveStdout: part.liveStdout,
          name: part.name,
          phase: part.phase,
          phaseUpdatedAt: active ? activePhaseUpdatedAt : undefined,
          resultContent: part.resultContent,
          resultOk: part.resultOk,
          summary: part.summary,
          summaryCount: part.summaryCount,
          summaryKind: part.summaryKind,
        },
        ...(part.attachments || []).map((attachment) => ({ type: "attachment" as const, attachment })),
      ];
    }),
    ...(hasTextPart ? [] : partsFromText(streamedText)),
  ]);
}

export function toolPhaseDot(phase: Extract<TurnPartVM, { type: "tool_use" }>["phase"]): TurnPhaseState["phase"] {
  switch (phase) {
    case "streaming_args":
      return "streaming_tool_args";
    case "running":
      return "executing_tool";
    case "error":
      return "error";
    case "ok":
    default:
      return "executing_tool";
  }
}

function partsFromText(text: string): TurnPartVM[] {
  return text ? [{ type: "text", text }] : [];
}

function partFromContentPart(part: ContentPart): TurnPartVM | null {
  switch (part.type) {
    case "text":
      return { type: "text", text: part.text };
    case "thought":
      return { type: "thought", text: part.text };
    case "tool_use":
      return { type: "tool_use", args: part.args, id: part.id, name: part.name };
    case "tool_result":
      return {
        type: "tool_result",
        content: part.content,
        id: part.id,
        name: part.name,
        ok: part.ok,
        summaryCount: part.summaryCount,
        summaryKind: part.summaryKind,
      };
    case "attachment":
      return { type: "attachment", attachment: part };
    case "local_folder":
      return null;
  }
}

function mergeToolParts(parts: TurnPartVM[]): TurnPartVM[] {
  const out: TurnPartVM[] = [];
  const toolIndexByID = new Map<string, number>();
  for (const part of parts) {
    if (part.type === "tool_use") {
      out.push(part);
      if (part.id) {
        toolIndexByID.set(part.id, out.length - 1);
      }
      continue;
    }
    if (part.type === "tool_result") {
      const index = part.id ? toolIndexByID.get(part.id) : undefined;
      const existing = typeof index === "number" ? out[index] : undefined;
      if (typeof index === "number" && existing?.type === "tool_use") {
        out[index] = {
          ...existing,
          resultContent: part.content,
          resultName: part.name,
          resultOk: part.ok,
          summaryCount: part.summaryCount,
          summaryKind: part.summaryKind,
        };
      } else {
        out.push({
          type: "tool_use",
          id: part.id,
          name: part.name,
          resultContent: part.content,
          resultName: part.name,
          resultOk: part.ok,
          summaryCount: part.summaryCount,
          summaryKind: part.summaryKind,
        });
      }
      continue;
    }
    out.push(part);
  }
  return out;
}

function withPartKeys(parts: TurnPartVM[]) {
  let textIndex = 0;
  let attachmentIndex = 0;
  let thoughtIndex = 0;
  let toolIndex = 0;
  let resultIndex = 0;
  return parts.map((part) => {
    switch (part.type) {
      case "text":
        return { ...part, key: `text:${textIndex++}` };
      case "attachment":
        return { ...part, key: part.attachment.id ? `attachment:${part.attachment.id}` : `attachment:${attachmentIndex++}` };
      case "thought":
        return { ...part, key: `thought:${thoughtIndex++}` };
      case "approval":
        return { ...part, key: `approval:${part.approvalID}` };
      case "tool_use":
        return { ...part, key: part.id ? `tool:${part.id}` : `tool:${toolIndex++}` };
      case "tool_result":
        return { ...part, key: part.id ? `tool-result:${part.id}` : `tool-result:${resultIndex++}` };
    }
  });
}

function orderedOverlayParts(overlay: AssistantOverlay): AssistantOverlayPart[] {
  return overlay.parts;
}

function findLastOverlayPartIndex(parts: AssistantOverlayPart[], type: AssistantOverlayPart["type"]) {
  for (let i = parts.length - 1; i >= 0; i--) {
    if (parts[i].type === type) {
      return i;
    }
  }
  return -1;
}

function useLocalDisclosure(defaultOpen: boolean, onOpenChange?: (open: boolean) => void) {
  const [open, setOpen] = useState(defaultOpen);
  const openRef = useRef(defaultOpen);

  useEffect(() => {
    openRef.current = defaultOpen;
    setOpen(defaultOpen);
  }, [defaultOpen]);

  function setDisclosureOpen(next: boolean) {
    if (openRef.current === next) {
      return;
    }
    openRef.current = next;
    setOpen(next);
    onOpenChange?.(next);
  }

  function handleToggle(event: ToggleEvent<HTMLDetailsElement>) {
    setDisclosureOpen(event.currentTarget.open);
  }

  function handleSummaryClick(event: MouseEvent<HTMLElement>) {
    event.preventDefault();
    setDisclosureOpen(!openRef.current);
  }

  function handleSummaryKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key !== "Enter" && event.key !== " ") {
      return;
    }
    event.preventDefault();
    setDisclosureOpen(!openRef.current);
  }

  return { handleSummaryClick, handleSummaryKeyDown, handleToggle, open };
}

function AttachmentPart({ attachment, token }: { attachment: Extract<TurnPartVM, { type: "attachment" }>["attachment"]; token: string }) {
  const [imagePreviewIndex, setImagePreviewIndex] = useState<number | null>(null);
  const url = attachmentResourceURL(attachment, token);

  if (isImageAttachment(attachment.mime, attachment.name)) {
    const image: ImageLightboxItem = {
      id: attachment.id,
      name: attachment.name,
      size: attachment.size,
      url,
    };
    return (
      <>
        <MarkdownImageCard image={image} onOpen={() => setImagePreviewIndex(0)} />
        <ImageLightbox images={[image]} openIndex={imagePreviewIndex} onOpenIndexChange={setImagePreviewIndex} />
      </>
    );
  }

  return (
    <a
      className="inline-flex max-w-full items-center gap-1 rounded-md border border-border/70 bg-muted/30 px-2 py-1 text-xs leading-5 text-muted-foreground no-underline hover:bg-muted hover:text-foreground"
      href={url}
      rel="noreferrer"
      target="_blank"
    >
      <Paperclip className="size-3 shrink-0" />
      <span className="min-w-0 truncate">{attachment.name}</span>
    </a>
  );
}

function isImageAttachment(mime: string | undefined, name: string) {
  const cleaned = (mime || "").toLowerCase();
  if (cleaned.startsWith("image/") && cleaned !== "image/svg+xml") {
    return true;
  }
  return /\.(png|jpe?g|gif|webp)$/i.test(name);
}

function ThoughtPart({
  active = false,
  defaultOpen,
  showContent = true,
  text,
  onOpenChange,
}: {
  active?: boolean;
  defaultOpen: boolean;
  showContent?: boolean;
  text: string;
  onOpenChange?: (open: boolean) => void;
}) {
  const { locale, t } = useI18n();
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const canShowContent = showContent && text.trim().length > 0;
  const { handleSummaryClick, handleSummaryKeyDown, handleToggle, open } = useLocalDisclosure(
    canShowContent ? defaultOpen : false,
    onOpenChange,
  );

  function handleThoughtSummaryClick(event: MouseEvent<HTMLElement>) {
    if (!canShowContent) {
      event.preventDefault();
      return;
    }
    handleSummaryClick(event);
  }

  function handleThoughtSummaryKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (!canShowContent) {
      return;
    }
    handleSummaryKeyDown(event);
  }

  useEffect(() => {
    if (!active || !open || !bodyRef.current) {
      return;
    }
    bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
  }, [active, open, text]);

  return (
    <details
      className="relative text-[13px] leading-[1.5] text-muted-foreground"
      open={canShowContent && open}
      onToggle={handleToggle}
    >
      {canShowContent && open ? (
        <span aria-hidden="true" className="pointer-events-none absolute top-6 bottom-0 left-[6px] border-l border-border" />
      ) : null}
      <summary
        className="inline-grid h-6 cursor-default list-none grid-cols-[1rem_auto] items-center gap-1 pr-1 outline-none hover:text-foreground [&::-webkit-details-marker]:hidden"
        onClick={handleThoughtSummaryClick}
        onKeyDown={handleThoughtSummaryKeyDown}
      >
        <PartIcon icon={Route} />
        <span className="flex min-w-0 flex-1 items-center gap-1">
          <span className="shrink-0 truncate">{active ? t("transcript.thinking") : t("transcript.thought")}</span>
          {canShowContent ? (
            <span className="shrink-0 text-muted-foreground/50">
              {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            </span>
          ) : null}
        </span>
      </summary>
      {canShowContent && open ? (
        <div className="ml-[5px] py-1 pl-2">
          <div
            ref={bodyRef}
            className="max-h-72 overflow-y-auto whitespace-pre-wrap break-words pr-2 text-[13px] leading-6 text-muted-foreground italic"
          >
            {text}
          </div>
        </div>
      ) : null}
    </details>
  );
}

function ProcessCompactPart({
  defaultOpen,
  hiddenParts,
  renderPart,
  onOpenChange,
}: {
  defaultOpen: boolean;
  hiddenParts: TurnPartVM[];
  renderPart: (part: TurnPartVM, index: number) => ReactNode;
  onOpenChange?: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const { handleSummaryClick, handleSummaryKeyDown, handleToggle, open } = useLocalDisclosure(defaultOpen, onOpenChange);
  const label = processCompactLabel(hiddenParts, t);
  const hasErrors = hiddenParts.some((part) => part.type === "tool_use" && toolFailed(part));
  return (
    <details className="relative text-[13px] leading-[1.5] text-muted-foreground/70" open={open} onToggle={handleToggle}>
      <summary
        className="inline-grid h-6 cursor-default list-none grid-cols-[1rem_auto] items-center gap-1 pr-1 outline-none hover:text-muted-foreground [&::-webkit-details-marker]:hidden"
        onClick={handleSummaryClick}
        onKeyDown={handleSummaryKeyDown}
      >
        <PartIcon icon={ListChecks} />
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="min-w-0 truncate">{label}</span>
          {hasErrors ? (
            <span className="shrink-0 text-destructive/75">{t("transcript.processHasErrors")}</span>
          ) : null}
          <span className="shrink-0 text-muted-foreground/50">
            {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          </span>
        </span>
      </summary>
      {open ? <div>{hiddenParts.map(renderPart)}</div> : null}
    </details>
  );
}

function processCompactLabel(parts: TurnPartVM[], t: (key: string) => string) {
  const tools = parts.filter((part): part is Extract<TurnPartVM, { type: "tool_use" }> => part.type === "tool_use");
  if (tools.length === 0) {
    return t("transcript.processThought");
  }

  const fileLabel = processFileToolsLabel(tools, t);
  if (fileLabel) {
    return fileLabel;
  }

  const groups: ProcessToolGroup[] = [];
  const groupByKey = new Map<string, ProcessToolGroup>();
  for (const tool of tools) {
    const group = processToolGroup(tool, t);
    const existing = groupByKey.get(group.key);
    if (existing) {
      continue;
    }
    groups.push(group);
    groupByKey.set(group.key, group);
  }

  const separator = " · ";
  return groups.map((group) => processToolGroupLabel(group, t)).join(separator);
}

function processFileToolsLabel(
  tools: Array<Extract<TurnPartVM, { type: "tool_use" }>>,
  t: (key: string) => string,
) {
  const names = tools.map((tool) => tool.name || tool.resultName || "");
  if (!names.every((name) => processFileToolNames.has(name))) {
    return "";
  }

  const paths: string[] = [];
  tools.forEach((tool) => addUniqueToolPath(paths, fileToolTargetPath(tool)));
  const multiple = paths.length > 1;
  const operations = new Set(names.map(fileToolOperation));
  const operation = operations.size === 1 ? operations.values().next().value : "process";
  const keys: Record<string, [string, string]> = {
    copy: ["transcript.processFileCopy", "transcript.processFilesCopy"],
    delete: ["transcript.processFileDelete", "transcript.processFilesDelete"],
    list: ["transcript.processFileList", "transcript.processFileList"],
    move: ["transcript.processFileMove", "transcript.processFilesMove"],
    process: ["transcript.processFileHandle", "transcript.processFilesHandle"],
    read: ["transcript.processFileRead", "transcript.processFilesRead"],
    search: ["transcript.processFileSearch", "transcript.processFileSearch"],
    stat: ["transcript.processFileStat", "transcript.processFilesStat"],
    update: ["transcript.processFileUpdate", "transcript.processFilesUpdate"],
    write: ["transcript.processFileWrite", "transcript.processFilesWrite"],
  };
  const pair = keys[operation || "process"] || keys.process;
  return t(pair[multiple ? 1 : 0]);
}

function fileToolOperation(name: string) {
  switch (name) {
    case "builtin_file_read":
    case "builtin_file_slice":
      return "read";
    case "builtin_file_write":
      return "write";
    case "builtin_file_patch":
      return "update";
    case "builtin_file_copy":
      return "copy";
    case "builtin_file_move":
      return "move";
    case "builtin_file_delete":
      return "delete";
    case "builtin_file_list":
      return "list";
    case "builtin_file_search":
      return "search";
    case "builtin_file_stat":
      return "stat";
    default:
      return "process";
  }
}

function fileToolTargetPath(tool: Extract<TurnPartVM, { type: "tool_use" }>) {
  const value = tool.argsText || tool.args;
  let args: Record<string, unknown> | null = null;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    args = value as Record<string, unknown>;
  } else if (typeof value === "string") {
    const parsed = parseJSON(value.trim());
    if (parsed.ok && parsed.value && typeof parsed.value === "object" && !Array.isArray(parsed.value)) {
      args = parsed.value as Record<string, unknown>;
    }
  }
  const key = (tool.name || tool.resultName) === "builtin_file_copy" || (tool.name || tool.resultName) === "builtin_file_move"
    ? "from_path"
    : "path";
  return typeof args?.[key] === "string" ? String(args[key]) : "";
}

function addUniqueToolPath(paths: string[], value: string) {
  const normalized = value.trim().replaceAll("\\", "/").replace(/\/{2,}/g, "/").replace(/\/$/, "");
  if (!normalized) {
    return;
  }
  const absolute = /^(?:[A-Za-z]:\/|\/)/.test(normalized);
  const duplicate = paths.some((existing) => {
    if (existing === normalized) {
      return true;
    }
    const existingAbsolute = /^(?:[A-Za-z]:\/|\/)/.test(existing);
    if (absolute === existingAbsolute) {
      return false;
    }
    const longer = absolute ? normalized : existing;
    const shorter = absolute ? existing : normalized;
    return longer.endsWith(`/${shorter}`);
  });
  if (!duplicate) {
    paths.push(normalized);
  }
}

type ProcessToolGroup = {
  fallbackName?: string;
  i18nKey?: string;
  key: string;
};

function processToolGroup(tool: Extract<TurnPartVM, { type: "tool_use" }>, t: (key: string) => string): ProcessToolGroup {
  const name = tool.name || tool.resultName || "";
  const fallbackName = toolDisplayName(name, t("transcript.tool"), t);
  const known: Record<string, string> = {
    builtin_browser_back: "transcript.processToolBrowser",
    builtin_browser_click: "transcript.processToolBrowser",
    builtin_browser_close: "transcript.processToolBrowser",
    builtin_browser_forward: "transcript.processToolBrowser",
    builtin_browser_observe: "transcript.processToolBrowser",
    builtin_browser_open: "transcript.processToolBrowser",
    builtin_browser_reload: "transcript.processToolBrowser",
    builtin_browser_screenshot: "transcript.processToolBrowser",
    builtin_browser_scroll: "transcript.processToolBrowser",
    builtin_browser_status: "transcript.processToolBrowser",
    builtin_browser_type: "transcript.processToolBrowser",
    builtin_graphql_request: "transcript.processToolData",
    builtin_rest_request: "transcript.processToolData",
    builtin_web_fetch: "transcript.processToolBrowser",
    builtin_web_search: "transcript.processToolWebSearch",
  };
  const i18nKey = known[name];
  if (i18nKey) {
    return { fallbackName, i18nKey, key: i18nKey };
  }
  return { fallbackName, key: `tool:${fallbackName}` };
}

function processToolGroupLabel(group: ProcessToolGroup, t: (key: string) => string) {
  const translated = group.i18nKey ? t(group.i18nKey) : "";
  return translated && translated !== group.i18nKey ? translated : group.fallbackName || t("transcript.tool");
}

function ToolUsePart({
  defaultOpen,
  part,
  sessionID,
  showRawInfo,
  onOpenChange,
}: {
  defaultOpen: boolean;
  part: Extract<TurnPartVM, { type: "tool_use" }>;
  sessionID?: string;
  showRawInfo: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const { locale, t } = useI18n();
  const { handleSummaryClick, handleSummaryKeyDown, handleToggle, open } = useLocalDisclosure(defaultOpen, onOpenChange);
  const args = formatToolArgs(part.argsText || part.args);
  const result = formatToolResult(part.resultContent);
  const liveResult = result;
  const toolName = part.name || part.resultName || "";
  const baseTitle = toolDisplayName(toolName, t("transcript.tool"), t);
  const codeTool = isCodeToolName(toolName);
  const terminalTool = toolName === "builtin_command_run" || toolName === "builtin_command_start" || toolName === "builtin_command_poll" || toolName === "builtin_command_stop";
  const showDetails = codeTool || showRawInfo;
  const active = part.active || part.phase === "streaming_args" || part.phase === "running";
  const elapsed = useElapsedDuration(active && part.phase === "running" ? part.phaseUpdatedAt : undefined, locale);
  const failed = toolFailed(part);
  const Icon = toolPartIcon(part);
  const title = toolTitle(part, liveResult, baseTitle, elapsed, t);
  const toneClass = failed ? "text-destructive" : "text-muted-foreground";
  const summaryClass = failed ? "text-destructive/70" : "text-muted-foreground/50";
  const hoverClass = failed ? "hover:text-destructive" : "hover:text-foreground";
  if (!showDetails) {
    return (
      <div className={cn("grid h-6 w-full grid-cols-[1rem_minmax(0,1fr)] items-center gap-1 pr-1 text-[13px] leading-[1.5]", toneClass)}>
        <PartIcon icon={Icon} />
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="shrink-0 truncate">{title.label}</span>
          {title.summary ? <span className={cn("min-w-0 truncate", summaryClass)}>{title.summary}</span> : null}
        </span>
      </div>
    );
  }
  return (
    <details
      className={cn("relative text-[13px] leading-[1.5]", toneClass)}
      open={open}
      onToggle={handleToggle}
    >
      {open ? <span aria-hidden="true" className="pointer-events-none absolute top-6 bottom-0 left-[6px] border-l border-border" /> : null}
      <summary
        className={cn("inline-grid h-6 cursor-default list-none grid-cols-[1rem_auto] items-center gap-1 pr-1 outline-none [&::-webkit-details-marker]:hidden", hoverClass)}
        onClick={handleSummaryClick}
        onKeyDown={handleSummaryKeyDown}
      >
        <PartIcon icon={Icon} />
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="shrink-0 truncate">{title.label}</span>
          {title.summary ? <span className={cn("min-w-0 truncate", summaryClass)}>{title.summary}</span> : null}
          <span className={cn("shrink-0", summaryClass)}>
            {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          </span>
        </span>
      </summary>
      <div className="ml-[5px] py-1 pl-2">
        <div className="grid gap-2">
          {codeTool ? (
            <div className={cn(!terminalTool && "rounded-md border border-border/50 bg-muted/20 p-2")}>
              <CodeToolDetails
                args={part.argsText || part.args}
                callID={part.id}
                liveStderr={part.liveStderr}
                liveStdout={part.liveStdout}
                name={toolName}
                result={liveResult?.value}
                sessionID={sessionID}
              />
            </div>
          ) : null}
          {showRawInfo ? (
            <RawToolDataCard
              args={args}
              result={liveResult?.text || ""}
              toolName={toolName}
            />
          ) : null}
        </div>
      </div>
    </details>
  );
}

function ToolNameLine({ name }: { name: string }) {
  const { t } = useI18n();
  return (
    <div className="mb-2 flex min-w-0 items-center gap-2 text-[11px] leading-5">
      <span className="shrink-0 whitespace-nowrap font-medium text-muted-foreground/80">{t("transcript.tool")}</span>
      <code className="min-w-0 truncate whitespace-nowrap rounded bg-muted/50 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
        {name}
      </code>
    </div>
  );
}

type MarkdownSegment = { type: "markdown"; text: string } | { type: "image"; image: SafeHtmlImage };

type SafeHtmlImage = {
  alt: string;
  src: string;
  style: CSSProperties;
  title?: string;
};

type MarkdownImageItem = ImageLightboxItem & {
  sourceKey: string;
};

export function MarkdownBody({ allowHtmlImages = true, text, token = "" }: { allowHtmlImages?: boolean; text: string; token?: string }) {
  const { t } = useI18n();
  const [codeRenderer, setCodeRenderer] = useState<CodeBlockRenderer | null>(null);
  const [imagePreviewIndex, setImagePreviewIndex] = useState<number | null>(null);
  const markdownImages = extractMarkdownImageItems(text, token);
  const imageIndexBySource = new Map(markdownImages.map((item, index) => [item.sourceKey, index]));
  useEffect(() => {
    let cancelled = false;
    void getShikiCodeRenderer().then((renderer) => {
      if (!cancelled) {
        setCodeRenderer(() => renderer);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);
  const components: Components = {
    a({ children, href, node: _node, ...props }) {
      return (
        <a {...props} href={href} target="_blank" rel="noreferrer noopener" onClick={handleMarkdownLinkClick}>
          {children}
        </a>
      );
    },
    img({ alt, node: _node, src }) {
      const label = alt || src || "";
      if (!src) {
        return label ? <span>{label}</span> : null;
      }
      const sourceKey = attachmentPathFromMarkdownURL(src);
      if (sourceKey) {
        const imageIndex = imageIndexBySource.get(sourceKey);
        const image =
          imageIndex !== undefined
            ? markdownImages[imageIndex]
            : {
                id: sourceKey,
                name: label || attachmentNameFromPath(sourceKey),
                sourceKey,
                url: attachmentResourceURL({ url: sourceKey }, token),
              };
        if (image.url) {
          return (
            <MarkdownImageCard
              image={image}
              onOpen={() => {
                if (imageIndex !== undefined) {
                  setImagePreviewIndex(imageIndex);
                }
              }}
            />
          );
        }
      }
      return (
        <a href={src} target="_blank" rel="noreferrer noopener" onClick={handleMarkdownLinkClick}>
          {label}
        </a>
      );
    },
    pre({ children }) {
      const block = getCodeBlock(children);
      if (!block) {
        return <pre>{children}</pre>;
      }
      return (
        <CodeBlock
          code={block.code}
          codeCopiedLabel={t("common.copied")}
          codeCopyLabel={t("common.copy")}
          codeRenderer={codeRenderer}
          lang={block.lang}
        />
      );
    },
    table({ children, node: _node, ...props }) {
      return (
        <div className="table-wrap">
          <table {...props}>{children}</table>
        </div>
      );
    },
  };

  const segments = allowHtmlImages ? splitMarkdownHtmlImages(text) : [{ type: "markdown" as const, text }];
  const hasHtmlImage = segments.some((segment) => segment.type === "image");

  return (
    <>
      <div className={cn("pudding-markdown py-1.5", hasHtmlImage && "pudding-markdown-html-images")}>
        {segments.map((segment, index) => {
          if (segment.type === "image") {
            return (
              <img
                key={`html-img-${index}`}
                alt={segment.image.alt}
                decoding="async"
                loading="lazy"
                src={segment.image.src}
                style={segment.image.style}
                title={segment.image.title}
              />
            );
          }
          if (!segment.text) {
            return null;
          }
          return (
            <ReactMarkdown key={`md-${index}`} components={components} remarkPlugins={[remarkGfm]} urlTransform={markdownUrlTransform}>
              {segment.text}
            </ReactMarkdown>
          );
        })}
      </div>
      <ImageLightbox images={markdownImages} openIndex={imagePreviewIndex} onOpenIndexChange={setImagePreviewIndex} />
    </>
  );
}

function MarkdownImageCard({ image, onOpen }: { image: ImageLightboxItem; onOpen: () => void }) {
  return (
    <button
      className="my-2 block h-20 w-24 overflow-hidden rounded-md border border-border/70 bg-muted/40"
      title={image.name}
      type="button"
      onClick={onOpen}
    >
      <img alt={image.name} className="h-full w-full object-cover" decoding="async" loading="lazy" src={image.url} />
    </button>
  );
}

function extractMarkdownImageItems(text: string, token: string): MarkdownImageItem[] {
  const out: MarkdownImageItem[] = [];
  const pattern = /!\[([^\]]*)\]\(([^)\s]+)(?:\s+["'][^"']*["'])?\)/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    const sourceKey = attachmentPathFromMarkdownURL(stripMarkdownImageURL(match[2] || ""));
    if (!sourceKey) {
      continue;
    }
    const name = (match[1] || "").trim() || attachmentNameFromPath(sourceKey);
    out.push({
      id: `markdown-image:${out.length}:${sourceKey}`,
      name,
      sourceKey,
      url: attachmentResourceURL({ url: sourceKey }, token),
    });
  }
  return out;
}

function stripMarkdownImageURL(value: string): string {
  return value.trim().replace(/^<|>$/g, "");
}

function attachmentPathFromMarkdownURL(value: string | undefined): string {
  const raw = (value || "").trim();
  if (!raw) {
    return "";
  }
  if (isAttachmentPath(raw)) {
    return raw;
  }
  try {
    const url = new URL(raw, window.location.href);
    const path = `${url.pathname}${url.search}`;
    return isAttachmentPath(path) ? path : "";
  } catch {
    return "";
  }
}

function isAttachmentPath(value: string): boolean {
  return /^\/sessions\/[^/]+\/attachments\//.test(value);
}

function attachmentNameFromPath(path: string): string {
  const clean = path.split("?")[0] || "";
  const name = clean.split("/").filter(Boolean).pop() || "image";
  try {
    return decodeURIComponent(name);
  } catch {
    return name;
  }
}

function splitMarkdownHtmlImages(text: string): MarkdownSegment[] {
  const segments: MarkdownSegment[] = [];
  const tagPattern = /<img\b([^>]*)\/?>/gi;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = tagPattern.exec(text))) {
    const image = parseSafeHtmlImage(match[1] || "");
    if (!image) {
      continue;
    }
    const before = text.slice(lastIndex, match.index);
    if (before) {
      segments.push({ type: "markdown", text: before });
    }
    segments.push({ type: "image", image });
    lastIndex = match.index + match[0].length;
  }

  const after = text.slice(lastIndex);
  if (after || segments.length === 0) {
    segments.push({ type: "markdown", text: after });
  }
  return segments;
}

function parseSafeHtmlImage(attrs: string): SafeHtmlImage | null {
  const src = safeHtmlImageSrc(readHtmlAttr(attrs, "src") || "");
  if (!src) {
    return null;
  }
  const title = decodeBasicHtmlEntities(readHtmlAttr(attrs, "title") || "").trim();
  return {
    alt: decodeBasicHtmlEntities(readHtmlAttr(attrs, "alt") || "").trim(),
    src,
    style: readSafeHtmlImageStyle(attrs),
    ...(title ? { title } : {}),
  };
}

function readHtmlAttr(attrs: string, name: string): string | undefined {
  const re = new RegExp(`(?:^|\\s)${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s"'>]+))`, "i");
  const match = re.exec(attrs);
  return match?.[1] ?? match?.[2] ?? match?.[3];
}

function safeHtmlImageSrc(raw: string): string | null {
  const src = decodeBasicHtmlEntities(raw).trim();
  if (!src) {
    return null;
  }
  if (/^data:image\/[a-z0-9.+-]+;base64,/i.test(src)) {
    return src;
  }
  if (src.startsWith("/files/") || src.startsWith("/captures/") || src.startsWith("/attachments/")) {
    return src;
  }
  try {
    const url = new URL(src);
    if (url.protocol === "http:" || url.protocol === "https:") {
      return src;
    }
  } catch {
    return null;
  }
  return null;
}

function readSafeHtmlImageStyle(attrs: string): CSSProperties {
  const style: CSSProperties = { maxWidth: "100%" };
  const attrWidth = safeHtmlImageLength(readHtmlAttr(attrs, "width"));
  const attrHeight = safeHtmlImageLength(readHtmlAttr(attrs, "height"));
  if (attrWidth) {
    style.width = attrWidth;
  }
  if (attrHeight) {
    style.height = attrHeight;
  }

  for (const part of decodeBasicHtmlEntities(readHtmlAttr(attrs, "style") || "").split(";")) {
    const [rawName, ...rawValue] = part.split(":");
    const name = rawName?.trim().toLowerCase();
    const value = rawValue.join(":").trim();
    if (!name || !value) {
      continue;
    }
    if (name === "width") {
      const safe = safeHtmlImageLength(value);
      if (safe) style.width = safe;
    } else if (name === "height") {
      const safe = safeHtmlImageLength(value);
      if (safe) style.height = safe;
    } else if (name === "border-radius") {
      const safe = safeHtmlImageSpacing(value);
      if (safe) style.borderRadius = safe;
    } else if (name === "float") {
      if (value === "left" || value === "right" || value === "none") {
        style.float = value;
      }
    } else if (name === "margin") {
      const safe = safeHtmlImageSpacing(value);
      if (safe) style.margin = safe;
    } else if (name === "margin-left") {
      const safe = safeHtmlImageLength(value);
      if (safe) style.marginLeft = safe;
    } else if (name === "margin-right") {
      const safe = safeHtmlImageLength(value);
      if (safe) style.marginRight = safe;
    } else if (name === "margin-top") {
      const safe = safeHtmlImageLength(value);
      if (safe) style.marginTop = safe;
    } else if (name === "margin-bottom") {
      const safe = safeHtmlImageLength(value);
      if (safe) style.marginBottom = safe;
    } else if (name === "object-fit" && /^(contain|cover|fill|none|scale-down)$/i.test(value)) {
      style.objectFit = value.toLowerCase() as CSSProperties["objectFit"];
    }
  }
  return style;
}

function safeHtmlImageSpacing(raw: string | undefined): string | undefined {
  const parts = decodeBasicHtmlEntities(raw || "")
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  if (parts.length === 0 || parts.length > 4) {
    return undefined;
  }
  const safeParts = parts.map(safeHtmlImageLength);
  return safeParts.every(Boolean) ? safeParts.join(" ") : undefined;
}

function safeHtmlImageLength(raw: string | undefined): string | undefined {
  const value = decodeBasicHtmlEntities(raw || "").trim().toLowerCase();
  if (!value) {
    return undefined;
  }
  if (/^\d{1,4}$/.test(value)) {
    return `${value}px`;
  }
  if (/^\d{1,4}px$/.test(value) || /^(?:100|[1-9]?\d)%$/.test(value)) {
    return value;
  }
  return undefined;
}

function decodeBasicHtmlEntities(value: string): string {
  return value
    .replace(/&amp;/g, "&")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">");
}

const markdownUrlTransform: UrlTransform = (raw, key, node) => {
  if (key === "src" || node.tagName === "img") {
    return attachmentPathFromMarkdownURL(raw);
  }
  try {
    const url = new URL(raw, window.location.origin);
    if (key === "href" && (url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:")) {
      return url.href;
    }
  } catch {
    return "";
  }
  return "";
};

function handleMarkdownLinkClick(event: MouseEvent<HTMLAnchorElement>) {
  if (event.defaultPrevented || event.button !== 0) {
    return;
  }
  const href = event.currentTarget.href;
  if (!href) {
    return;
  }
  event.preventDefault();
  openExternalURL(href);
}

type CodeElementProps = {
  children?: ReactNode;
  className?: string;
};

function getCodeBlock(children: ReactNode) {
  const child = Children.toArray(children)[0];
  if (!isValidElement<CodeElementProps>(child) || child.type !== "code") {
    return null;
  }
  const lang = /language-([^\s]+)/.exec(child.props.className || "")?.[1];
  return {
    code: codeText(child.props.children).replace(/\n$/, ""),
    lang,
  };
}

function codeText(children: ReactNode): string {
  return Children.toArray(children)
    .map((child) => (typeof child === "string" || typeof child === "number" ? String(child) : ""))
    .join("");
}

function CodeBlock({
  code,
  codeCopiedLabel,
  codeCopyLabel,
  codeRenderer,
  lang,
}: {
  code: string;
  codeCopiedLabel: string;
  codeCopyLabel: string;
  codeRenderer: CodeBlockRenderer | null;
  lang?: string;
}) {
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (resetTimer.current) {
        window.clearTimeout(resetTimer.current);
      }
    };
  }, []);
  const highlighted = codeRenderer?.(code, lang);
  return (
    <div className="code-block-wrap">
      <button
        aria-label={copied ? codeCopiedLabel : codeCopyLabel}
        className="code-copy-btn"
        data-copied={copied ? "1" : undefined}
        type="button"
        onClick={() => {
          void navigator.clipboard.writeText(code).then(() => {
            setCopied(true);
            if (resetTimer.current) {
              window.clearTimeout(resetTimer.current);
            }
            resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
          });
        }}
      />
      {highlighted ? (
        <div dangerouslySetInnerHTML={{ __html: highlighted }} />
      ) : (
        <pre data-lang={lang}>
          <code>{code}</code>
        </pre>
      )}
    </div>
  );
}

function formatToolArgs(value: unknown) {
  if (value == null || value === "") {
    return "";
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) {
      return "";
    }
    const parsed = parseJSON(trimmed);
    if (parsed.ok) {
      if (isEmptyObject(parsed.value)) {
        return "";
      }
      return JSON.stringify(parsed.value, null, 2);
    }
    return trimmed;
  }
  if (isEmptyObject(value)) {
    return "";
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function formatToolResult(content: string | undefined) {
  if (!content) {
    return null;
  }
  const trimmed = content.trim();
  if (!trimmed) {
    return null;
  }
  const parsed = parseJSON(trimmed);
  if (parsed.ok) {
    return {
      fieldCount: fieldCount(parsed.value),
      text: JSON.stringify(parsed.value, null, 2),
      value: parsed.value,
    };
  }
  return { fieldCount: null, text: trimmed, value: null };
}

function parseJSON(value: string): { ok: true; value: unknown } | { ok: false } {
  try {
    return { ok: true, value: JSON.parse(value) };
  } catch {
    return { ok: false };
  }
}

function isEmptyObject(value: unknown) {
  return Boolean(value && typeof value === "object" && !Array.isArray(value) && Object.keys(value).length === 0);
}

function fieldCount(value: unknown): number | null {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return Object.keys(value).length;
  }
  if (Array.isArray(value)) {
    return value.length;
  }
  return null;
}

function toolDisplayName(name: string | undefined, fallback: string, t: (key: string) => string) {
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
    builtin_toolkit_load: t("transcript.toolToolkitLoad"),
    builtin_app_load: t("transcript.toolAppLoad"),
    builtin_command_poll: t("transcript.toolCommandPoll"),
    builtin_command_run: t("transcript.toolCommandRun"),
    builtin_command_start: t("transcript.toolCommandStart"),
    builtin_command_stop: t("transcript.toolCommandStop"),
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
    builtin_attachment_read_image: t("transcript.toolAttachmentReadImage"),
    builtin_file_search: t("transcript.toolFileSearch"),
    builtin_file_slice: t("transcript.toolFileSlice"),
    builtin_file_stat: t("transcript.toolFileStat"),
    builtin_file_write: t("transcript.toolFileWrite"),
    builtin_git_diff: t("transcript.toolGitDiff"),
    builtin_git_log: t("transcript.toolGitLog"),
    builtin_git_stage: t("transcript.toolGitStage"),
    builtin_git_status: t("transcript.toolGitStatus"),
    builtin_git_unstage: t("transcript.toolGitUnstage"),
    builtin_git_commit: t("transcript.toolGitCommit"),
    builtin_patch_apply: t("transcript.toolPatchApply"),
    builtin_patch_propose: t("transcript.toolPatchPropose"),
    builtin_project_inspect: t("transcript.toolProjectInspect"),
    builtin_project_instructions: t("transcript.toolProjectInstructions"),
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
    builtin_skill_submit: t("transcript.toolSkillSubmit"),
    builtin_skill_validate: t("transcript.toolSkillValidate"),
    builtin_time_get_current: t("transcript.toolTimeCurrent"),
    builtin_weather_get: t("transcript.toolWeatherGet"),
    builtin_web_fetch: t("transcript.toolWebFetch"),
    builtin_web_search: t("transcript.toolWebSearch"),
    collect_user_input: t("transcript.toolCollectUserInput"),
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

function toolTitle(
  part: Extract<TurnPartVM, { type: "tool_use" }>,
  result: ReturnType<typeof formatToolResult>,
  baseTitle: string,
  elapsed: string,
  t: (key: string) => string,
) {
  if (part.phase === "streaming_args") {
    return { label: t("transcript.toolPreparingName").replace("{name}", baseTitle), summary: "" };
  }
  if (part.phase === "running") {
    return {
      label: t("transcript.toolRunningName").replace("{name}", baseTitle),
      summary: elapsed,
    };
  }
  if (toolFailed(part)) {
    const unknownName = unknownToolName(part, result);
    if (unknownName) {
      return { label: t("transcript.toolUnknown"), summary: unknownName };
    }
    const capabilitySummary = capabilityToolSummary(part, result, t);
    const codeSummary = codeToolSummary(part.name || part.resultName || "", part.argsText || part.args, result?.value, t);
    return { label: baseTitle, summary: capabilitySummary || codeSummary || t("transcript.toolFailed") };
  }
  const inputSummary = inputFlowToolSummary(part, result, t);
  if (inputSummary) {
    return { label: baseTitle, summary: inputSummary };
  }
  const capabilitySummary = capabilityToolSummary(part, result, t);
  if (capabilitySummary) {
    return { label: baseTitle, summary: capabilitySummary };
  }
  const codeSummary = codeToolSummary(part.name || part.resultName || "", part.argsText || part.args, result?.value, t);
  if (codeSummary) {
    return { label: baseTitle, summary: codeSummary };
  }
  const toolkitSummary = toolkitLoadSummary(part, result, t);
  if (toolkitSummary) {
    return { label: baseTitle, summary: toolkitSummary };
  }
  const summary = toolProtocolSummary(part, t);
  if (summary) {
    return { label: baseTitle, summary };
  }
  const structuralSummary = toolStructuralSummary(result, t);
  if (structuralSummary) {
    return { label: baseTitle, summary: structuralSummary };
  }
  return { label: baseTitle, summary: "" };
}

function toolkitLoadSummary(
  part: Extract<TurnPartVM, { type: "tool_use" }>,
  result: ReturnType<typeof formatToolResult>,
  t: (key: string) => string,
) {
  if ((part.name || part.resultName) !== "builtin_toolkit_load") {
    return "";
  }
  const value = result?.value;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return "";
  }
  const record = value as Record<string, unknown>;
  const loaded = Array.isArray(record.loaded) ? record.loaded.filter((item): item is string => typeof item === "string") : [];
  if (loaded.length > 0) {
    return t("transcript.toolkitLoaded").replace("{names}", loaded.join(", "));
  }
  const active = Array.isArray(record.alreadyActive) ? record.alreadyActive.filter((item): item is string => typeof item === "string") : [];
  return active.length > 0 ? t("transcript.toolkitAlreadyActive") : "";
}

function inputFlowToolSummary(
  part: Extract<TurnPartVM, { type: "tool_use" }>,
  result: ReturnType<typeof formatToolResult>,
  t: (key: string) => string,
) {
  if ((part.name || part.resultName) !== "collect_user_input") {
    return "";
  }
  const value = result?.value;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return "";
  }
  return (value as Record<string, unknown>).status === "awaiting_user" ? t("transcript.toolAwaitingInput") : "";
}

function unknownToolName(part: Extract<TurnPartVM, { type: "tool_use" }>, result: ReturnType<typeof formatToolResult>) {
  const value = result?.value;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return "";
  }
  const record = value as Record<string, unknown>;
  if (record.reason !== "unknown_tool") {
    return "";
  }
  const toolName = typeof record.tool === "string" ? record.tool : "";
  return toolName || part.name || part.resultName || "";
}

function capabilityToolSummary(
  part: Extract<TurnPartVM, { type: "tool_use" }>,
  result: ReturnType<typeof formatToolResult>,
  t: (key: string) => string,
) {
  if ((part.name || part.resultName) !== "request_capability") {
    return "";
  }
  const value = result?.value;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return "";
  }
  const status = String((value as Record<string, unknown>).status || "");
  switch (status) {
    case "approved":
      return t("transcript.approvalApproved");
    case "denied":
      return t("transcript.approvalDenied");
    default:
      return "";
  }
}

function toolProtocolSummary(part: Extract<TurnPartVM, { type: "tool_use" }>, t: (key: string) => string) {
  if (!part.summaryKind) {
    return "";
  }
  const count = String(part.summaryCount ?? 0);
  switch (part.summaryKind) {
    case "returned_fields":
      return t("transcript.toolReturnedFields").replace("{count}", count);
    case "returned_items":
      return t("transcript.toolReturnedItems").replace("{count}", count);
    case "read_chars":
      return t("transcript.toolReadChars").replace("{count}", count);
    case "read_files":
      return t("transcript.toolReadFiles").replace("{count}", count);
    case "changed_lines":
      return t("transcript.toolChangedLines").replace("{count}", count);
    default:
      return "";
  }
}

function toolFailed(part: Extract<TurnPartVM, { type: "tool_use" }>) {
  if ((part.name || part.resultName) === "builtin_command_run") {
    const value = formatToolResult(part.resultContent)?.value;
    if (value && typeof value === "object" && !Array.isArray(value)) {
      const record = value as Record<string, unknown>;
      if (
        record.reason === "non_zero_exit" &&
        typeof record.exitCode === "number" &&
        record.exitCode >= 0 &&
        record.timedOut !== true &&
        record.cancelled !== true
      ) {
        return false;
      }
    }
  }
  return part.resultOk === false || part.phase === "error";
}

function toolStructuralSummary(result: ReturnType<typeof formatToolResult>, t: (key: string) => string) {
  const value = result?.value;
  if (Array.isArray(value)) {
    return t("transcript.toolReturnedItems").replace("{count}", String(value.length));
  }
  if (!value || typeof value !== "object") {
    return "";
  }
  const recordCount = resultRecordCount(value as Record<string, unknown>);
  if (recordCount != null) {
    return t("transcript.toolReturnedItems").replace("{count}", String(recordCount));
  }
  const count = Object.keys(value).length;
  return count > 0 ? t("transcript.toolReturnedFields").replace("{count}", String(count)) : "";
}

function resultRecordCount(value: Record<string, unknown>) {
  const preferredKeys = ["results", "items", "records", "data", "rows", "files", "matches"];
  for (const key of preferredKeys) {
    if (Array.isArray(value[key])) {
      return value[key].length;
    }
  }
  const arrays = Object.values(value).filter(Array.isArray);
  return arrays.length === 1 ? arrays[0].length : null;
}

function rawToolCopyText(toolName: string, args: string, result: string, t: (key: string) => string) {
  const lines: string[] = [];
  if (toolName) {
    lines.push(`${t("transcript.tool")}: ${toolName}`);
  }
  if (args) {
    lines.push("", `${t("transcript.toolArgs")}:`, args);
  }
  if (result) {
    lines.push("", `${t("transcript.toolResult")}:`, result);
  }
  return lines.join("\n").trim();
}

function RawToolDataCard({ args, result, toolName }: { args: string; result: string; toolName: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const copyText = rawToolCopyText(toolName, args, result, t);
  return (
    <details
      className="group/raw-data overflow-hidden rounded-md border border-border/50 bg-muted/20 text-[11px] text-muted-foreground"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="flex h-8 cursor-default list-none items-center gap-1 px-2 outline-none hover:text-foreground [&::-webkit-details-marker]:hidden">
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <span>{t("transcript.codeRawData")}</span>
        <span className="min-w-0 flex-1" />
        <ToolHoverCopyButton className="group-hover/raw-data:opacity-100" text={copyText} />
      </summary>
      {open ? (
        <div className="border-t border-border/50 p-2">
          {toolName ? <ToolNameLine name={toolName} /> : null}
          {args ? <ToolDetailBlock label={t("transcript.toolArgs")} text={args} /> : null}
          {result ? <ToolDetailBlock label={t("transcript.toolResult")} text={result} /> : null}
        </div>
      ) : null}
    </details>
  );
}

function ToolDetailBlock({ label, text }: { label: string; text: string }) {
  return (
    <div className="mb-2 last:mb-0">
      <div className="mb-1 text-[11px] font-medium text-muted-foreground/80">{label}</div>
      <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-4 text-muted-foreground">
        {text}
      </pre>
    </div>
  );
}
