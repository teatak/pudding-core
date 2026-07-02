import { Check, ChevronDown, ChevronRight, Copy } from "lucide-react";
import {
  Children,
  isValidElement,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
  type ToggleEvent,
} from "react";
import ReactMarkdown, { type Components, type UrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";

import { type ContentPart, type Message } from "@/api/client";
import { PhaseDot } from "@/components/PhaseDot";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { getShikiCodeRenderer, type CodeBlockRenderer } from "@/lib/shiki";
import type { AssistantOverlay, AssistantOverlayPart, TurnPhaseState } from "@/state/overlayStore";

import { useElapsedDuration } from "./time";
import { textFromContentParts, type TranscriptDisplaySettings, type TurnDisclosureState, type TurnPartVM } from "./types";

export function TurnParts({
  disclosure,
  displaySettings,
  parts,
  turnID,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  parts: TurnPartVM[];
  turnID: string;
}) {
  const showReasoningContent = displaySettings?.showReasoning ?? true;
  const showToolDetails = displaySettings?.showToolDetails ?? true;
  return (
    <>
      {parts.map((part, index) => {
        const partKey = part.key || `${part.type}:${index}`;
        const disclosureKey = `${turnID}:${partKey}`;
        switch (part.type) {
          case "text":
            return <MarkdownBody key={partKey} text={part.text} />;
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
                showDetails={showToolDetails}
                onOpenChange={(open) => disclosure?.setOpen(disclosureKey, open)}
              />
            );
          case "tool_result":
            return null;
        }
      })}
    </>
  );
}

export function partsFromMessages(messages: Message[]): TurnPartVM[] {
  return withPartKeys(mergeToolParts(messages.flatMap((message) => message.parts.map(partFromContentPart))));
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
    ...overlayParts.map((part, index): TurnPartVM => {
      if (part.type === "text") {
        return { type: "text", text: part.text };
      }
      if (part.type === "thought") {
        return { type: "thought", active: activePhaseName === "thinking" && index === lastThoughtIndex, text: part.text };
      }
      if (part.type === "approval") {
        return {
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
        };
      }
      const active =
        index === lastToolIndex &&
        (activePhaseName === "streaming_tool_args" ||
          activePhaseName === "executing_tool" ||
          activePhaseName === "awaiting_followup");
      return {
        type: "tool_use",
        active,
        argsText: part.argsText,
        dotPhase: active ? activePhaseName : toolPhaseDot(part.phase),
        id: part.callID,
        name: part.name,
        phase: part.phase,
        phaseUpdatedAt: active ? activePhaseUpdatedAt : undefined,
        summary: part.summary,
        summaryCount: part.summaryCount,
        summaryKind: part.summaryKind,
      };
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

function partFromContentPart(part: ContentPart): TurnPartVM {
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
  let thoughtIndex = 0;
  let toolIndex = 0;
  let resultIndex = 0;
  return parts.map((part) => {
    switch (part.type) {
      case "text":
        return { ...part, key: `text:${textIndex++}` };
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
      className="relative text-[12px] leading-[1.5] text-muted-foreground"
      open={canShowContent && open}
      onToggle={handleToggle}
    >
      {canShowContent && open ? (
        <span aria-hidden="true" className="pointer-events-none absolute top-6 bottom-0 left-[6px] border-l border-border" />
      ) : null}
      <summary
        className="inline-grid h-6 cursor-default list-none grid-cols-[0.75rem_auto] items-center gap-1 pr-1 outline-none hover:text-foreground [&::-webkit-details-marker]:hidden"
        onClick={handleThoughtSummaryClick}
        onKeyDown={handleThoughtSummaryKeyDown}
      >
        <span className="relative z-[1] inline-flex h-6 w-3 shrink-0 items-center justify-center opacity-90">
          <PhaseDot active={active} phase="thinking" size="md" />
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-1">
          <span className="shrink-0 truncate">{active ? t("transcript.thinking") : t("transcript.thought")}</span>
          {canShowContent ? (
            <span className="shrink-0 text-muted-foreground/50">
              {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
            </span>
          ) : null}
        </span>
      </summary>
      {canShowContent && open ? (
        <div className="ml-[5px] py-1 pl-2">
          <div
            ref={bodyRef}
            className="max-h-72 overflow-y-auto whitespace-pre-wrap break-words pr-2 text-[12px] leading-6 text-muted-foreground italic"
          >
            {text}
          </div>
        </div>
      ) : null}
    </details>
  );
}

function ToolUsePart({
  defaultOpen,
  part,
  showDetails,
  onOpenChange,
}: {
  defaultOpen: boolean;
  part: Extract<TurnPartVM, { type: "tool_use" }>;
  showDetails: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const { locale, t } = useI18n();
  const { handleSummaryClick, handleSummaryKeyDown, handleToggle, open } = useLocalDisclosure(defaultOpen, onOpenChange);
  const args = formatToolArgs(part.argsText || part.args);
  const result = formatToolResult(part.resultContent);
  const liveResult = result;
  const baseTitle = toolDisplayName(part.name || part.resultName, t("transcript.tool"), t);
  const active = part.active || part.phase === "streaming_args" || part.phase === "running";
  const elapsed = useElapsedDuration(active && part.phase === "running" ? part.phaseUpdatedAt : undefined, locale);
  const failed = toolFailed(part);
  const dotPhase = part.dotPhase || (failed ? "error" : toolPhaseDot(part.phase));
  const title = toolTitle(part, liveResult, baseTitle, elapsed, t);
  const copyText = toolCopyText(part, args, liveResult, baseTitle, t);
  const toneClass = failed ? "text-destructive" : "text-muted-foreground";
  const summaryClass = failed ? "text-destructive/70" : "text-muted-foreground/50";
  const hoverClass = failed ? "hover:text-destructive" : "hover:text-foreground";
  if (!showDetails) {
    return (
      <div className={cn("grid h-6 w-full grid-cols-[0.75rem_minmax(0,1fr)] items-center gap-1 pr-1 text-[12px] leading-[1.5]", toneClass)}>
        <span className="relative z-[1] inline-flex h-6 w-3 shrink-0 items-center justify-center opacity-90">
          <PhaseDot active={active} phase={dotPhase} size="md" />
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="shrink-0 truncate">{title.label}</span>
          {title.summary ? <span className={cn("min-w-0 truncate", summaryClass)}>{title.summary}</span> : null}
        </span>
      </div>
    );
  }
  return (
    <details
      className={cn("relative text-[12px] leading-[1.5]", toneClass)}
      open={open}
      onToggle={handleToggle}
    >
      {open ? <span aria-hidden="true" className="pointer-events-none absolute top-6 bottom-0 left-[6px] border-l border-border" /> : null}
      <summary
        className={cn("inline-grid h-6 cursor-default list-none grid-cols-[0.75rem_auto] items-center gap-1 pr-1 outline-none [&::-webkit-details-marker]:hidden", hoverClass)}
        onClick={handleSummaryClick}
        onKeyDown={handleSummaryKeyDown}
      >
        <span className="relative z-[1] inline-flex h-6 w-3 shrink-0 items-center justify-center opacity-90">
          <PhaseDot active={active} phase={dotPhase} size="md" />
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="shrink-0 truncate">{title.label}</span>
          {title.summary ? <span className={cn("min-w-0 truncate", summaryClass)}>{title.summary}</span> : null}
          <span className={cn("shrink-0", summaryClass)}>
            {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
          </span>
        </span>
      </summary>
      <div className="ml-[5px] py-1 pl-2">
        <div className="relative rounded-md border border-border/50 bg-muted/20 p-2 pr-9">
          <ToolCopyButton text={copyText} />
          {part.name || part.resultName ? <ToolNameLine name={part.name || part.resultName || ""} /> : null}
          {args ? <ToolDetailBlock label={t("transcript.toolArgs")} text={args} /> : null}
          {liveResult ? <ToolDetailBlock label={t("transcript.toolResult")} text={liveResult.text} /> : null}
          {!args && !liveResult ? <div className="leading-5">{title.summary || title.label}</div> : null}
        </div>
      </div>
    </details>
  );
}

function ToolNameLine({ name }: { name: string }) {
  const { t } = useI18n();
  return (
    <div className="mb-2 flex min-w-0 items-center gap-2 text-[11px] leading-5">
      <span className="font-medium text-muted-foreground/80">{t("transcript.tool")}</span>
      <code className="min-w-0 truncate rounded bg-muted/50 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">{name}</code>
    </div>
  );
}

export function MarkdownBody({ text }: { text: string }) {
  const { t } = useI18n();
  const [codeRenderer, setCodeRenderer] = useState<CodeBlockRenderer | null>(null);
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

  return (
    <div className="pudding-markdown">
      <ReactMarkdown components={components} remarkPlugins={[remarkGfm]} urlTransform={markdownUrlTransform}>
        {text}
      </ReactMarkdown>
    </div>
  );
}

const markdownUrlTransform: UrlTransform = (raw, key) => {
  try {
    const url = new URL(raw, window.location.origin);
    if (key === "src") {
      return url.protocol === "http:" || url.protocol === "https:" ? url.href : "";
    }
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

function openExternalURL(url: string) {
  void import("@wailsio/runtime")
    .then(({ Browser }) => Browser.OpenURL(url))
    .catch(() => {
      window.open(url, "_blank", "noopener,noreferrer");
    });
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
  const known: Record<string, string> = {
    builtin_graphql_request: t("transcript.toolGraphQLRequest"),
    builtin_file_delete: t("transcript.toolFileDelete"),
    builtin_file_list: t("transcript.toolFileList"),
    builtin_file_move: t("transcript.toolFileMove"),
    builtin_file_patch: t("transcript.toolFilePatch"),
    builtin_file_read: t("transcript.toolFileRead"),
    builtin_file_write: t("transcript.toolFileWrite"),
    builtin_rest_request: t("transcript.toolRESTRequest"),
    builtin_skill_read: t("transcript.toolSkillRead"),
    builtin_skill_submit: t("transcript.toolSkillSubmit"),
    builtin_skill_validate: t("transcript.toolSkillValidate"),
    builtin_time_get_current: t("transcript.toolTimeCurrent"),
    builtin_web_fetch: t("transcript.toolWebFetch"),
    builtin_web_search: t("transcript.toolWebSearch"),
    builtin_workspace_list: t("transcript.toolWorkspaceList"),
    canvas_chart: t("transcript.toolCanvasChart"),
    canvas_markdown: t("transcript.toolCanvasMarkdown"),
    canvas_table: t("transcript.toolCanvasTable"),
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
    return { label: baseTitle, summary: capabilitySummary || t("transcript.toolFailed") };
  }
  const capabilitySummary = capabilityToolSummary(part, result, t);
  if (capabilitySummary) {
    return { label: baseTitle, summary: capabilitySummary };
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

function toolCopyText(
  part: Extract<TurnPartVM, { type: "tool_use" }>,
  args: string,
  result: ReturnType<typeof formatToolResult>,
  title: string,
  t: (key: string) => string,
) {
  const lines = [title];
  if (part.name) {
    lines.push(`${t("transcript.tool")}: ${part.name}`);
  }
  if (args) {
    lines.push("", `${t("transcript.toolArgs")}:`, args);
  }
  if (result?.text) {
    lines.push("", `${t("transcript.toolResult")}:`, result.text);
  }
  return lines.join("\n");
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

function ToolCopyButton({ text }: { text: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (resetTimer.current) {
        window.clearTimeout(resetTimer.current);
      }
    };
  }, []);
  return (
    <Button
      aria-label={t("common.copy")}
      className="absolute top-1.5 right-1.5 size-6 bg-transparent transition-colors hover:bg-muted dark:hover:bg-muted/50 active:translate-y-0"
      size="icon-xs"
      type="button"
      variant="ghost"
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          if (resetTimer.current) {
            window.clearTimeout(resetTimer.current);
          }
          resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
        });
      }}
    >
      {copied ? <Check className="text-success" /> : <Copy />}
    </Button>
  );
}
