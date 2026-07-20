import {
  ArrowRight,
  Check,
  CheckCircle2,
  CircleAlert,
  Clock3,
  Copy,
  File,
  Folder,
  GitBranch,
  PanelRightOpen,
  XCircle,
} from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { requestProjectFileReveal } from "@/state/projectRevealStore";

const commandToolName = "builtin_command_run";
const backgroundProcessToolNames = new Set(["builtin_command_start", "builtin_command_poll", "builtin_command_stop"]);
const projectInspectToolName = "builtin_project_inspect";
const projectInstructionsToolName = "builtin_project_instructions";
const languageCodeToolNames = new Set([
  "builtin_code_symbols",
  "builtin_code_definition",
  "builtin_code_references",
  "builtin_code_diagnostics",
  "builtin_code_rename",
]);
const gitToolNames = new Set([
  "builtin_git_status",
  "builtin_git_diff",
  "builtin_git_log",
  "builtin_git_stage",
  "builtin_git_unstage",
  "builtin_git_commit",
]);
const patchToolNames = new Set(["builtin_patch_propose", "builtin_patch_apply"]);
const fileToolNames = new Set([
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

type Translator = (key: string) => string;
type UnknownRecord = Record<string, unknown>;

export function isCodeToolName(name: string) {
  return name === commandToolName || backgroundProcessToolNames.has(name) || name === projectInspectToolName || name === projectInstructionsToolName || languageCodeToolNames.has(name) || gitToolNames.has(name) || patchToolNames.has(name) || fileToolNames.has(name);
}

export function codeToolSummary(name: string, args: unknown, result: unknown, t: Translator) {
  if (!isCodeToolName(name)) {
    return "";
  }
  const input = toRecord(args);
  const output = toRecord(result);
  if (name === commandToolName) {
    if (readBoolean(output, "sandboxDenied")) {
      return t("transcript.codeSandboxDenied");
    }
    const verificationKind = readString(output, "verificationKind");
    const verificationStatus = readString(output, "verificationStatus");
    if (verificationKind && verificationStatus) {
      return verificationStatusLabel(verificationKind, verificationStatus, t);
    }
    if (readBoolean(output, "timedOut")) {
      return t("transcript.codeTimedOut");
    }
    if (readBoolean(output, "cancelled")) {
      return t("transcript.codeCancelled");
    }
    const exitCode = readNumber(output, "exitCode");
    const duration = readNumber(output, "durationMs");
    if (exitCode != null) {
      const exit = replace(t("transcript.codeExitCode"), { code: String(exitCode) });
      return duration != null ? `${exit} · ${formatDuration(duration)}` : exit;
    }
    const argv = readStringArray(output, "argv") || readStringArray(input, "argv");
    return argv ? compactText(formatArgv(argv), 100) : "";
  }
  if (backgroundProcessToolNames.has(name)) {
    if (readBoolean(output, "sandboxDenied")) {
      return t("transcript.codeSandboxDenied");
    }
    const status = readString(output, "status");
    const processID = readString(output, "processID");
    if (status) {
      return [processStatusLabel(status, t), processID ? compactText(processID, 24) : ""].filter(Boolean).join(" · ");
    }
    const script = readString(input, "script");
    const argv = readStringArray(input, "argv");
    return compactText(script || (argv ? formatArgv(argv) : ""), 100);
  }
  if (name === projectInspectToolName) {
    const languages = readRecordArray(output, "languages").map((item) => readString(item, "name")).filter(Boolean);
    return languages.length > 0 ? languages.slice(0, 4).join(" · ") : preferredPath(output) || preferredPath(input);
  }
  if (name === projectInstructionsToolName) {
    return countSummary(output, "instructionCount", "transcript.codeInstructionFiles", t);
  }
  if (name === "builtin_code_symbols") {
    return countSummary(output, "resultCount", "transcript.codeSymbols", t);
  }
  if (name === "builtin_code_diagnostics") {
    return countSummary(output, "diagnosticCount", "transcript.codeDiagnostics", t);
  }
  if (name === "builtin_code_definition" || name === "builtin_code_references") {
    return countSummary(output, "locationCount", "transcript.codeLocations", t);
  }
  if (name === "builtin_code_rename") {
    const oldName = readString(output, "oldName");
    const newName = readString(output, "newName") || readString(input, "new_name");
    return compactText([oldName, newName].filter(Boolean).join(" → "), 100);
  }
  if (name === "builtin_git_status") {
    if (readBoolean(output, "clean")) {
      return t("transcript.codeGitClean");
    }
    return countSummary(output, "fileCount", "transcript.codeFiles", t);
  }
  if (name === "builtin_git_diff") {
    const files = countSummary(output, "fileCount", "transcript.codeFiles", t);
    const additions = readNumber(output, "additions") ?? 0;
    const deletions = readNumber(output, "deletions") ?? 0;
    return files ? `${files} · +${additions} -${deletions}` : "";
  }
  if (name === "builtin_git_log") {
    return countSummary(output, "count", "transcript.codeCommits", t);
  }
  if (name === "builtin_git_stage" || name === "builtin_git_unstage") {
    return countSummary(output, "pathCount", "transcript.codeFiles", t);
  }
  if (name === "builtin_git_commit") {
    const commit = toRecord(output?.commit);
    return commit ? `${readString(commit, "shortHash")} · ${compactText(readString(commit, "subject"), 80)}` : "";
  }
  if (patchToolNames.has(name)) {
    const files = countSummary(output, "fileCount", "transcript.codeFiles", t);
    const additions = readNumber(output, "additions") ?? 0;
    const deletions = readNumber(output, "deletions") ?? 0;
    return files ? `${files} · +${additions} -${deletions}` : "";
  }
  const path = preferredPath(output) || preferredPath(input);
  if (name === "builtin_file_search") {
    return path;
  }
  if (name === "builtin_file_list") {
    return path;
  }
  if (name === "builtin_file_copy" || name === "builtin_file_move") {
    const from = readString(output, "fromRelativePath") || readString(output, "from") || readString(input, "from_path");
    const to = readString(output, "toRelativePath") || readString(output, "to") || readString(input, "to_path");
    return from || to ? compactText(`${from} → ${to}`, 100) : "";
  }
  return path;
}

export function CodeToolDetails({
  args,
  callID,
  liveStderr,
  liveStdout,
  name,
  result,
  sessionID,
}: {
  args: unknown;
  callID?: string;
  liveStderr?: string;
  liveStdout?: string;
  name: string;
  result: unknown;
  sessionID?: string;
}) {
  const { locale, t } = useI18n();
  const input = toRecord(args);
  const output = toRecord(result);
  let body: ReactNode;
  if (name === commandToolName) {
    body = <CommandDetails callID={callID} input={input} liveStderr={liveStderr} liveStdout={liveStdout} output={output} sessionID={sessionID} t={t} />;
  } else if (backgroundProcessToolNames.has(name)) {
    body = <BackgroundProcessDetails input={input} output={output} t={t} />;
  } else if (name === projectInspectToolName) {
    body = <ProjectInspectDetails output={output} t={t} />;
  } else if (name === projectInstructionsToolName) {
    body = <ProjectInstructionsDetails output={output} t={t} />;
  } else if (languageCodeToolNames.has(name)) {
    body = <LanguageCodeDetails callID={callID} name={name} output={output} sessionID={sessionID} t={t} />;
  } else if (name === "builtin_git_status") {
    body = <GitStatusDetails output={output} t={t} />;
  } else if (name === "builtin_git_diff") {
    body = <GitDiffDetails output={output} t={t} />;
  } else if (name === "builtin_git_log") {
    body = <GitLogDetails locale={locale} output={output} t={t} />;
  } else if (name === "builtin_git_stage" || name === "builtin_git_unstage" || name === "builtin_git_commit") {
    body = <GitWriteDetails name={name} output={output} t={t} />;
  } else if (patchToolNames.has(name)) {
    body = <PatchDetails output={output} t={t} />;
  } else {
    body = <FileDetails callID={callID} input={input} name={name} output={output} locale={locale} sessionID={sessionID} t={t} />;
  }
  return <div className="min-w-0 max-w-full overflow-hidden">{body}</div>;
}

function BackgroundProcessDetails({ input, output, t }: { input: UnknownRecord | null; output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const argv = readStringArray(output, "argv") || readStringArray(input, "argv") || [];
  const script = readString(output, "script") || readString(input, "script");
  const command = script || formatArgv(argv);
  const chunks = readRecordArray(output, "output");
  const outputText = chunks.map((chunk) => readString(chunk, "content")).join("");
  const status = readString(output, "status") || "running";
  const processID = readString(output, "processID") || readString(input, "process_id");
  const cwd = readString(output, "cwd");
  const exitCode = readNumber(output, "exitCode");
  const running = readBoolean(output, "running");
  const sandboxDenied = readBoolean(output, "sandboxDenied");
  const StatusIcon = sandboxDenied ? CircleAlert : running ? Clock3 : status === "exited" && exitCode === 0 ? CheckCircle2 : status === "stopped" ? CheckCircle2 : CircleAlert;
  return (
    <div className="overflow-hidden rounded-md border border-border/70 bg-muted/20">
      <div className="px-3 pt-2 text-[11px] font-medium text-muted-foreground">{t("transcript.codeBackgroundProcess")}</div>
      {command ? (
        <section className="group/process-copy relative px-3 py-2 pr-10">
          <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-foreground/85">
            <span className="select-none text-muted-foreground">$ </span>{command}
          </pre>
          <ToolHoverCopyButton className="absolute top-1.5 right-1.5 group-hover/process-copy:opacity-100" text={command} />
        </section>
      ) : null}
      {chunks.length > 0 ? (
        <section className="group/process-copy relative min-h-12 px-3 pt-1 pb-2 pr-10">
          <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-foreground/80">
            {chunks.map((chunk, index) => (
              <span key={`${readNumber(chunk, "offset") ?? index}:${index}`} className={readString(chunk, "stream") === "stderr" ? "text-destructive/85" : undefined}>
                {readString(chunk, "content")}
              </span>
            ))}
          </pre>
          <ToolHoverCopyButton className="absolute top-1.5 right-1.5 group-hover/process-copy:opacity-100" text={outputText} />
        </section>
      ) : null}
      {sandboxDenied ? <SandboxDeniedNotice t={t} /> : null}
      <div className="flex min-w-0 items-center gap-3 border-t border-border/50 px-3 py-2 text-[11px] text-muted-foreground">
        {cwd ? <code className="min-w-0 flex-1 truncate font-mono">{cwd}</code> : <span className="flex-1" />}
        {processID ? <code className="max-w-48 shrink truncate font-mono">{processID}</code> : null}
        <span className={cn("inline-flex shrink-0 items-center gap-1", sandboxDenied && "text-warning", !sandboxDenied && !running && status !== "stopped" && status !== "exited" && "text-warning", !sandboxDenied && status === "exited" && exitCode === 0 && "text-success")}>
          <StatusIcon className="size-3.5" />
          {sandboxDenied ? t("transcript.codeSandboxDenied") : processStatusLabel(status, t)}
          {exitCode != null ? ` · ${replace(t("transcript.codeExitCode"), { code: String(exitCode) })}` : ""}
        </span>
      </div>
    </div>
  );
}

function processStatusLabel(status: string, t: Translator) {
  const key = `transcript.codeProcessStatus.${status}`;
  const translated = t(key);
  return translated === key ? status : translated;
}

function LanguageCodeDetails({ callID, name, output, sessionID, t }: { callID?: string; name: string; output: UnknownRecord | null; sessionID?: string; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  if (name === "builtin_code_rename") {
    return <LanguageCodeRenameDetails output={output} t={t} />;
  }
  if (name === "builtin_code_diagnostics") {
    const diagnostics = readRecordArray(output, "diagnostics");
    return (
      <div className="space-y-1">
        <FileResultMeta
          values={[
            ...codeLanguageMeta(output, t),
            readBoolean(output, "fresh") ? "" : t("transcript.codeResultNotFresh"),
            readBoolean(output, "truncated") ? t("transcript.codePartialResults") : "",
          ]}
        />
        {diagnostics.length > 0
          ? <CommandDiagnosticList callID={callID} diagnostics={diagnostics} sessionID={sessionID} t={t} />
          : <EmptyLine>{t("transcript.codeNoSemanticResults")}</EmptyLine>}
      </div>
    );
  }
  const items = name === "builtin_code_symbols" ? readRecordArray(output, "symbols") : readRecordArray(output, "locations");
  const countKey = name === "builtin_code_symbols" ? "transcript.codeSymbols" : "transcript.codeLocations";
  const external = readNumber(output, "externalResultCount") ?? 0;
  return (
    <div className="space-y-1">
      <FileResultMeta
        values={[
          ...codeLanguageMeta(output, t),
          replace(t(countKey), { count: String(items.length) }),
          external > 0 ? replace(t("transcript.codeExternalResults"), { count: String(external) }) : "",
          readBoolean(output, "truncated") ? t("transcript.codePartialResults") : "",
        ]}
      />
      {items.length > 0
        ? <LanguageCodeLocationList callID={callID} items={items} sessionID={sessionID} symbols={name === "builtin_code_symbols"} t={t} />
        : <EmptyLine>{t("transcript.codeNoSemanticResults")}</EmptyLine>}
    </div>
  );
}

function LanguageCodeRenameDetails({ output, t }: { output: UnknownRecord; t: Translator }) {
  const oldName = readString(output, "oldName");
  const newName = readString(output, "newName");
  const editCount = readNumber(output, "editCount");
  return (
    <div className="space-y-2">
      <div className="flex min-w-0 items-center gap-2 text-[12px] text-foreground/90">
        {oldName ? <code className="min-w-0 truncate font-mono">{oldName}</code> : null}
        {oldName ? <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" /> : null}
        <code className="min-w-0 truncate font-mono font-medium">{newName}</code>
      </div>
      <FileResultMeta
        values={[
          ...codeLanguageMeta(output, t),
          editCount != null ? replace(t("transcript.codeReplacementCount"), { count: String(editCount) }) : "",
        ]}
      />
      <PatchDetails output={output} t={t} />
    </div>
  );
}

function LanguageCodeLocationList({ callID, items, sessionID, symbols, t }: { callID?: string; items: UnknownRecord[]; sessionID?: string; symbols: boolean; t: Translator }) {
  return (
    <div className="max-h-72 overflow-auto border-t border-border/50 pt-1">
      {items.slice(0, 500).map((item, index) => {
        const path = readString(item, "path");
        const providedRelativePath = readString(item, "relativePath");
        const relativePath = providedRelativePath || path;
        const line = readNumber(item, "line") ?? 1;
        const column = readNumber(item, "column") ?? 1;
        const excerpt = readString(item, "excerpt");
        const title = symbols ? readString(item, "name") : relativePath;
        const locationLabel = `${relativePath}:${line}:${column}`;
        const detail = symbols
          ? [readString(item, "kind"), readString(item, "containerName")].filter(Boolean).join(" · ")
          : `${line}:${column}`;
        const previewable = Boolean(sessionID && path && excerpt);
        const content = (
          <>
            <span className="min-w-0">
              <span className="block truncate font-mono text-[11px] text-foreground/90">{title}</span>
              {symbols ? <span className="block truncate font-mono text-[10px] text-muted-foreground">{locationLabel}</span> : null}
            </span>
            <span className="min-w-0 truncate text-right text-[10px] text-muted-foreground">{detail}</span>
            {previewable ? <PanelRightOpen aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/code-location:opacity-100" /> : <span />}
          </>
        );
        return previewable ? (
          <button
            key={`${path}:${line}:${column}:${index}`}
            aria-label={`${t("transcript.codeOpenInCanvas")} ${locationLabel}`}
            className="group/code-location grid min-h-9 w-full grid-cols-[minmax(0,1fr)_minmax(0,40%)_1rem] items-center gap-2 rounded-sm px-1 text-left hover:bg-muted/60"
            type="button"
            onClick={() => requestProjectFileReveal({
              absolutePath: path,
              column,
              fallback: {
                callID,
                content: excerpt,
                focusLine: line,
                lineStart: readNumber(item, "lineStart") ?? line,
                lineStep: 1,
                path,
                sessionID: sessionID!,
                source: "code-location",
                truncated: true,
              },
              line,
              relativePath: providedRelativePath,
              sessionID: sessionID!,
            })}
          >
            {content}
          </button>
        ) : (
          <div key={`${path}:${line}:${column}:${index}`} className="grid min-h-9 grid-cols-[minmax(0,1fr)_minmax(0,40%)_1rem] items-center gap-2 px-1">{content}</div>
        );
      })}
    </div>
  );
}

function ProjectInstructionsDetails({ output, t }: { output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const targets = readRecordArray(output, "targets");
  const instructions = readRecordArray(output, "instructions");
  const warnings = readRecordArray(output, "warnings");
  return (
    <div className="space-y-2">
      <MetricRow
        metrics={[
          metric(readNumber(output, "targetCount") ?? targets.length, t("transcript.codeTargetsLabel")),
          metric(readNumber(output, "instructionCount") ?? instructions.length, t("transcript.codeInstructionFilesLabel")),
          warnings.length > 0 ? metric(warnings.length, t("transcript.codeWarningsLabel"), "text-warning") : null,
        ]}
      />
      {instructions.length > 0 ? <ProjectInstructionFileList instructions={instructions} t={t} /> : <EmptyLine>{t("transcript.codeNoProjectInstructions")}</EmptyLine>}
      {warnings.length > 0 ? <ProjectInstructionWarningList warnings={warnings} /> : null}
    </div>
  );
}

function ProjectInstructionFileList({ instructions, t }: { instructions: UnknownRecord[]; t: Translator }) {
  return (
    <div className="max-h-72 overflow-auto border-t border-border/50 pt-1">
      {instructions.slice(0, 64).map((instruction, index) => {
        const appliesTo = readStringArray(instruction, "appliesTo") || [];
        return (
          <div key={`${readString(instruction, "projectRoot")}:${readString(instruction, "path")}:${index}`} className="grid min-h-8 grid-cols-[1.25rem_minmax(0,1fr)_auto] items-center gap-1.5 py-1 text-[11px]">
            <span className="text-right font-mono text-muted-foreground/70">{readNumber(instruction, "order") ?? index + 1}</span>
            <span className="min-w-0">
              <code className="block truncate font-mono text-foreground/85">{readString(instruction, "path")}</code>
              <span className="block truncate text-[10px] text-muted-foreground">
                {replace(t("transcript.codeInstructionScope"), { scope: readString(instruction, "scopePath") || "." })}
              </span>
            </span>
            <span className="shrink-0 text-right text-[10px] text-muted-foreground">
              {readBoolean(instruction, "truncated") ? <span className="text-warning">{t("transcript.codeTruncated")}</span> : replace(t("transcript.codeAppliesTargets"), { count: String(appliesTo.length) })}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function ProjectInstructionWarningList({ warnings }: { warnings: UnknownRecord[] }) {
  return (
    <div className="border-t border-warning/30 pt-1 text-[10px] text-warning">
      {warnings.slice(0, 20).map((warning, index) => (
        <div key={`${readString(warning, "path")}:${index}`} className="break-words py-0.5">
          <code className="font-mono">{readString(warning, "path")}</code>{readString(warning, "detail") ? ` · ${readString(warning, "detail")}` : ""}
        </div>
      ))}
    </div>
  );
}

function ProjectInspectDetails({ output, t }: { output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const languages = readRecordArray(output, "languages");
  const manifests = readRecordArray(output, "manifests");
  const instructions = readRecordArray(output, "instructions");
  const commands = readRecordArray(output, "suggestedCommands");
  const gitRoot = readString(output, "gitRoot");
  return (
    <div className="space-y-2">
      <MetricRow
        metrics={[
          metric(languages.length, t("transcript.codeLanguagesLabel")),
          metric(manifests.length, t("transcript.codeManifestsLabel")),
          metric(instructions.length, t("transcript.codeInstructionsLabel")),
        ]}
      />
      <FileResultMeta
        values={[
          replace(t("transcript.codeScannedFiles"), { count: String(readNumber(output, "filesScanned") ?? 0) }),
          readBoolean(output, "scanCapped") ? t("transcript.codePartialResults") : "",
        ]}
      />
      {gitRoot ? <ProjectInspectPath label={t("transcript.codeGitRoot")} path={gitRoot} /> : null}
      {languages.length > 0 ? (
        <ProjectInspectList
          label={t("transcript.codeLanguages")}
          items={languages.map((item) => `${readString(item, "name")} · ${readNumber(item, "fileCount") ?? 0}`)}
        />
      ) : null}
      {manifests.length > 0 ? (
        <ProjectInspectList label={t("transcript.codeManifests")} items={manifests.map((item) => readString(item, "path"))} mono />
      ) : null}
      {instructions.length > 0 ? (
        <ProjectInspectList label={t("transcript.codeInstructions")} items={instructions.map((item) => readString(item, "path"))} mono />
      ) : null}
      {commands.length > 0 ? <ProjectCommandList commands={commands} t={t} /> : null}
    </div>
  );
}

function ProjectInspectPath({ label, path }: { label: string; path: string }) {
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-2 text-[11px]">
      <DetailLabel>{label}</DetailLabel>
      <code className="truncate font-mono text-foreground/85" title={path}>{path}</code>
    </div>
  );
}

function ProjectInspectList({ items, label, mono = false }: { items: string[]; label: string; mono?: boolean }) {
  return (
    <section className="min-w-0 max-w-full border-t border-border/50 pt-2">
      <DetailLabel>{label}</DetailLabel>
      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-foreground/85">
        {items.slice(0, 50).map((item, index) => (
          <span key={`${item}:${index}`} className={cn("min-w-0 break-all", mono && "font-mono")}>{item}</span>
        ))}
      </div>
    </section>
  );
}

function ProjectCommandList({ commands, t }: { commands: UnknownRecord[]; t: Translator }) {
  return (
    <section className="border-t border-border/50 pt-2">
      <DetailLabel>{t("transcript.codeSuggestedCommands")}</DetailLabel>
      <div className="mt-1 max-h-52 overflow-auto">
        {commands.slice(0, 50).map((command, index) => {
          const argv = readStringArray(command, "argv") || [];
          return (
            <div key={`${readString(command, "cwd")}:${formatArgv(argv)}:${index}`} className="grid min-h-6 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 text-[11px]">
              <code className="min-w-0 truncate font-mono text-foreground/85">$ {formatArgv(argv)}</code>
              <span className="shrink-0 truncate text-muted-foreground">{readString(command, "cwd")}</span>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function PatchDetails({ output, t }: { output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const files = readRecordArray(output, "files");
  const diff = readString(output, "diff");
  return (
    <div className="space-y-2">
      <MetricRow
        metrics={[
          metric(readNumber(output, "fileCount"), t("transcript.codeFilesLabel")),
          metric(readNumber(output, "additions"), t("transcript.codeAdditions"), "text-success"),
          metric(readNumber(output, "deletions"), t("transcript.codeDeletions"), "text-destructive"),
        ]}
      />
      {files.length > 0 ? <PatchFileList files={files} t={t} /> : null}
      {diff ? <CodeOutput label={t("transcript.codePatchDiff")} text={diff} /> : null}
      {readString(output, "status") === "applied" ? <EmptyLine>{t("transcript.codePatchApplied")}</EmptyLine> : null}
    </div>
  );
}

function PatchFileList({ files, t }: { files: UnknownRecord[]; t: Translator }) {
  return (
    <div className="max-h-64 overflow-auto border-t border-border/50 pt-1">
      {files.slice(0, 100).map((file, index) => (
        <div key={`${readString(file, "path")}:${index}`} className="grid min-h-6 grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-2 text-[11px]">
          <span className="min-w-0 truncate font-mono text-foreground/85">{readString(file, "path")}</span>
          <span className="text-muted-foreground">{t(`transcript.codePatchOperation.${readString(file, "operation")}`)}</span>
          <span className="shrink-0 font-mono">
            <span className="text-success">+{readNumber(file, "additions") ?? 0}</span>{" "}
            <span className="text-destructive">-{readNumber(file, "deletions") ?? 0}</span>
          </span>
        </div>
      ))}
      <OmittedCount count={files.length - Math.min(files.length, 100)} t={t} />
    </div>
  );
}

function CommandDetails({ callID, input, liveStderr = "", liveStdout = "", output, sessionID, t }: { callID?: string; input: UnknownRecord | null; liveStderr?: string; liveStdout?: string; output: UnknownRecord | null; sessionID?: string; t: Translator }) {
  const argv = readStringArray(output, "argv") || readStringArray(input, "argv") || [];
  const script = readString(output, "script") || readString(input, "script");
  const cwd = readString(output, "cwd") || readString(input, "cwd");
  const stdout = output ? readString(output, "stdout") : liveStdout;
  const stderr = output ? readString(output, "stderr") : liveStderr;
  const processOutput = joinTerminalOutput(stdout, stderr);
  const exitCode = readNumber(output, "exitCode");
  const duration = readNumber(output, "durationMs");
  const timedOut = readBoolean(output, "timedOut");
  const cancelled = readBoolean(output, "cancelled");
  const sandboxDenied = readBoolean(output, "sandboxDenied");
  const verificationKind = readString(output, "verificationKind");
  const verificationStatus = readString(output, "verificationStatus");
  const diagnostics = readRecordArray(output, "diagnostics");
  const processCompleted = exitCode != null && exitCode >= 0 && !timedOut && !cancelled;
  const toolFailed = output?.ok === false && !processCompleted;
  const terminalOutput = processOutput || (toolFailed ? readString(output, "detail") || readString(output, "error") || readString(output, "reason") : "");
  const commandSucceeded = processCompleted && exitCode === 0;
  const verificationPassed = verificationStatus === "passed";
  const verificationFailed = verificationStatus === "failed";
  const StatusIcon = sandboxDenied ? CircleAlert : timedOut || cancelled ? Clock3 : toolFailed ? XCircle : verificationPassed || commandSucceeded ? CheckCircle2 : processCompleted ? CircleAlert : Clock3;
  const statusText = sandboxDenied
    ? t("transcript.codeSandboxDenied")
    : verificationKind && verificationStatus
      ? verificationStatusLabel(verificationKind, verificationStatus, t)
      : timedOut
        ? t("transcript.codeTimedOut")
        : cancelled
          ? t("transcript.codeCancelled")
          : exitCode != null
            ? replace(t("transcript.codeExitCode"), { code: String(exitCode) })
            : toolFailed
              ? t("transcript.toolFailed")
              : t("transcript.codeRunning");
  const command = script || formatArgv(argv);
  const outputRef = useRef<HTMLPreElement | null>(null);
  useEffect(() => {
    if (outputRef.current && !output) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [output, terminalOutput]);
  return (
    <div className="overflow-hidden rounded-md border border-border/70 bg-muted/20">
      <div className="px-3 pt-2 text-[11px] font-medium text-muted-foreground">{t("transcript.codeTerminal")}</div>
      {command ? (
        <section className="group/terminal-copy relative px-3 py-2 pr-10">
          <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-foreground/85">
            <span className="select-none text-muted-foreground">$ </span>{command}
          </pre>
          <ToolHoverCopyButton className="absolute top-1.5 right-1.5 group-hover/terminal-copy:opacity-100" text={command} />
        </section>
      ) : null}
      <section className="group/terminal-copy relative min-h-12 px-3 pt-1 pb-2 pr-10">
        <pre ref={outputRef} className={cn("max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-foreground/80", toolFailed && !sandboxDenied && "text-destructive/90")}>
          {terminalOutput || (output ? t("transcript.codeNoOutput") : t("transcript.codeRunning"))}
        </pre>
        <ToolHoverCopyButton className="absolute top-1.5 right-1.5 group-hover/terminal-copy:opacity-100" text={terminalOutput} />
      </section>
      {sandboxDenied ? <SandboxDeniedNotice t={t} /> : null}
      {diagnostics.length > 0 ? <CommandDiagnosticList callID={callID} diagnostics={diagnostics} sessionID={sessionID} t={t} /> : null}
      <div className="flex min-w-0 items-center gap-3 border-t border-border/50 px-3 py-2 text-[11px] text-muted-foreground">
        {cwd ? <code className="min-w-0 flex-1 truncate font-mono">{cwd}</code> : <span className="flex-1" />}
        {readBoolean(output, "stdoutTruncated") || readBoolean(output, "stderrTruncated") ? <span className="shrink-0 text-warning">{t("transcript.codeTruncated")}</span> : null}
        {duration != null ? <span className="shrink-0">{formatDuration(duration)}</span> : null}
        <span className={cn("inline-flex shrink-0 items-center gap-1", sandboxDenied && "text-warning", !sandboxDenied && (toolFailed || verificationFailed) && "text-destructive", !sandboxDenied && (verificationPassed || (!verificationKind && commandSucceeded)) && "text-success", !sandboxDenied && processCompleted && !verificationPassed && !verificationFailed && !commandSucceeded && "text-muted-foreground")}>
          <StatusIcon className="size-3.5" />
          {!sandboxDenied && !verificationKind && commandSucceeded ? t("transcript.codeSucceeded") : statusText}
        </span>
      </div>
    </div>
  );
}

function SandboxDeniedNotice({ t }: { t: Translator }) {
  return (
    <div className="flex items-start gap-1.5 border-t border-warning/30 bg-warning/5 px-3 py-2 text-[11px] text-warning">
      <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
      <span>{t("transcript.codeSandboxDeniedHint")}</span>
    </div>
  );
}

function CommandDiagnosticList({ callID, diagnostics, sessionID, t }: { callID?: string; diagnostics: UnknownRecord[]; sessionID?: string; t: Translator }) {
  return (
    <section className="border-t border-border/50 px-3 py-2">
      <div className="mb-1 text-[10px] font-medium text-muted-foreground">
        {replace(t("transcript.codeDiagnostics"), { count: String(diagnostics.length) })}
      </div>
      <div className="max-h-64 overflow-auto">
        {diagnostics.slice(0, 500).map((diagnostic, index) => {
          const path = readString(diagnostic, "path");
          const providedRelativePath = readString(diagnostic, "relativePath");
          const relativePath = providedRelativePath || path;
          const line = readNumber(diagnostic, "line") ?? 1;
          const column = readNumber(diagnostic, "column");
          const excerpt = readString(diagnostic, "excerpt");
          const severity = readString(diagnostic, "severity");
          const previewable = Boolean(sessionID && path && excerpt);
          const content = (
            <>
              <CircleAlert className={cn("mt-0.5 size-3.5 shrink-0", severity === "error" ? "text-destructive" : severity === "warning" ? "text-warning" : "text-muted-foreground")} />
              <span className="min-w-0">
                <code className="block truncate font-mono text-[10px] text-muted-foreground">
                  {relativePath}:{line}{column != null && column > 0 ? `:${column}` : ""}
                </code>
                <span className="block break-words text-left text-[11px] leading-4 text-foreground/85">
                  {readString(diagnostic, "message")}
                  {readString(diagnostic, "code") ? <code className="ml-1 text-muted-foreground">{readString(diagnostic, "code")}</code> : null}
                </span>
              </span>
            </>
          );
          return previewable ? (
            <button
              key={`${path}:${line}:${column ?? 0}:${index}`}
              aria-label={`${t("transcript.codeOpenInCanvas")} ${relativePath}:${line}:${column ?? 1}`}
              className="group/code-diagnostic grid w-full grid-cols-[1rem_minmax(0,1fr)_1rem] gap-1.5 rounded-sm px-1 py-1.5 hover:bg-muted/60"
              type="button"
              onClick={() => requestProjectFileReveal({
                absolutePath: path,
                column: column ?? undefined,
                fallback: {
                  callID,
                  content: excerpt,
                  focusLine: line,
                  lineStart: readNumber(diagnostic, "lineStart") ?? line,
                  lineStep: 1,
                  path,
                  sessionID: sessionID!,
                  source: "diagnostic",
                  truncated: true,
                },
                line,
                relativePath: providedRelativePath,
                sessionID: sessionID!,
              })}
            >
              {content}
              <PanelRightOpen aria-hidden="true" className="mt-0.5 size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover/code-diagnostic:opacity-100" />
            </button>
          ) : (
            <div key={`${path}:${line}:${column ?? 0}:${index}`} className="grid grid-cols-[1rem_minmax(0,1fr)] gap-1.5 px-1 py-1.5">{content}</div>
          );
        })}
      </div>
    </section>
  );
}

export function ToolHoverCopyButton({ className, text }: { className?: string; text: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | null>(null);
  useEffect(() => () => {
    if (resetTimer.current) {
      window.clearTimeout(resetTimer.current);
    }
  }, []);
  if (!text) {
    return null;
  }
  return (
    <Button
      aria-label={copied ? t("common.copied") : t("common.copy")}
      className={cn("size-6 bg-transparent opacity-0 transition-opacity hover:bg-muted hover:opacity-100 dark:hover:bg-muted/50", className)}
      size="icon-xs"
      type="button"
      variant="ghost"
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
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

function joinTerminalOutput(stdout: string, stderr: string) {
  if (!stdout) {
    return stderr;
  }
  if (!stderr) {
    return stdout;
  }
  return stdout + (stdout.endsWith("\n") ? "" : "\n") + stderr;
}

function GitStatusDetails({ output, t }: { output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const files = readRecordArray(output, "files");
  const branch = readString(output, "branch") || shortHash(readString(output, "head")) || t("transcript.codeDetached");
  const clean = readBoolean(output, "clean");
  const ahead = readNumber(output, "ahead") ?? 0;
  const behind = readNumber(output, "behind") ?? 0;
  return (
    <div className="space-y-2">
      <div className="flex min-w-0 items-center gap-2 text-[12px]">
        <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
        <code className="min-w-0 truncate font-mono text-foreground/90">{branch}</code>
        <span className={cn("ml-auto shrink-0 text-[11px]", clean ? "text-success" : "text-muted-foreground")}>
          {clean ? t("transcript.codeGitClean") : t("transcript.codeGitChanged")}
        </span>
      </div>
      <MetricRow
        metrics={[
          metric(readNumber(output, "fileCount"), t("transcript.codeFilesLabel")),
          metric(readNumber(output, "stagedCount"), t("transcript.codeStaged")),
          metric(readNumber(output, "unstagedCount"), t("transcript.codeUnstaged")),
          metric(readNumber(output, "untrackedCount"), t("transcript.codeUntracked")),
          metric(readNumber(output, "conflictedCount"), t("transcript.codeConflicts")),
        ]}
      />
      {ahead || behind ? (
        <div className="text-[11px] text-muted-foreground">
          {ahead ? replace(t("transcript.codeAhead"), { count: String(ahead) }) : null}
          {ahead && behind ? " · " : null}
          {behind ? replace(t("transcript.codeBehind"), { count: String(behind) }) : null}
        </div>
      ) : null}
      <GitStatusFileList files={files} t={t} />
    </div>
  );
}

function GitStatusFileList({ files, t }: { files: UnknownRecord[]; t: Translator }) {
  const visible = files.slice(0, 200);
  if (visible.length === 0) {
    return null;
  }
  return (
    <div className="max-h-72 overflow-auto border-t border-border/50 pt-1">
      {visible.map((file, index) => {
        const path = readString(file, "path");
        const originalPath = readString(file, "originalPath");
        const status = `${readString(file, "indexStatus")}${readString(file, "worktreeStatus")}`;
        return (
          <div key={`${path}:${index}`} className="grid min-h-6 grid-cols-[2rem_minmax(0,1fr)] items-center gap-1 text-[11px]">
            <code className={cn("font-mono", gitKindTone(readString(file, "kind")))}>{status}</code>
            <div className="flex min-w-0 items-center gap-1 font-mono text-foreground/85">
              {originalPath ? <span className="min-w-0 truncate text-muted-foreground line-through">{originalPath}</span> : null}
              {originalPath ? <ArrowRight className="size-3 shrink-0 text-muted-foreground" /> : null}
              <span className="min-w-0 truncate">{path}</span>
            </div>
          </div>
        );
      })}
      <OmittedCount count={files.length - visible.length} t={t} />
    </div>
  );
}

function GitDiffDetails({ output, t }: { output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const files = readRecordArray(output, "files");
  const diff = readString(output, "diff");
  const visible = files.slice(0, 100);
  return (
    <div className="space-y-2">
      <MetricRow
        metrics={[
          metric(readNumber(output, "fileCount"), t("transcript.codeFilesLabel")),
          metric(readNumber(output, "additions"), t("transcript.codeAdditions"), "text-success"),
          metric(readNumber(output, "deletions"), t("transcript.codeDeletions"), "text-destructive"),
        ]}
      />
      {visible.length > 0 ? (
        <div className="max-h-64 overflow-auto border-t border-border/50 pt-1">
          {visible.map((file, index) => (
            <div key={`${readString(file, "path")}:${index}`} className="grid min-h-6 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 text-[11px]">
              <div className="flex min-w-0 items-center gap-1 font-mono text-foreground/85">
                {readString(file, "originalPath") ? <span className="min-w-0 truncate text-muted-foreground">{readString(file, "originalPath")}</span> : null}
                {readString(file, "originalPath") ? <ArrowRight className="size-3 shrink-0 text-muted-foreground" /> : null}
                <span className="min-w-0 truncate">{readString(file, "path")}</span>
              </div>
              <span className="shrink-0 font-mono">
                <span className="text-success">+{readNumber(file, "additions") ?? 0}</span>{" "}
                <span className="text-destructive">-{readNumber(file, "deletions") ?? 0}</span>
              </span>
            </div>
          ))}
          <OmittedCount count={files.length - visible.length} t={t} />
        </div>
      ) : null}
      {diff ? <CodeOutput label={readBoolean(output, "staged") ? t("transcript.codeStagedDiff") : t("transcript.codeDiff")} text={diff} truncated={readBoolean(output, "truncated")} /> : <EmptyLine>{t("transcript.codeNoChanges")}</EmptyLine>}
    </div>
  );
}

function GitLogDetails({ locale, output, t }: { locale: string; output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const commits = readRecordArray(output, "commits");
  if (commits.length === 0) {
    return <EmptyLine>{t("transcript.codeNoCommits")}</EmptyLine>;
  }
  return (
    <div className="max-h-80 overflow-auto">
      {commits.map((commit, index) => (
        <div key={`${readString(commit, "hash")}:${index}`} className="grid grid-cols-[4.5rem_minmax(0,1fr)] gap-2 border-b border-border/40 py-1.5 last:border-b-0">
          <code className="font-mono text-[11px] text-muted-foreground">{readString(commit, "shortHash")}</code>
          <div className="min-w-0">
            <div className="truncate text-[12px] text-foreground/90">{readString(commit, "subject")}</div>
            <div className="mt-0.5 flex min-w-0 flex-wrap gap-x-2 text-[10px] text-muted-foreground">
              <span className="min-w-0 truncate">{readString(commit, "authorName")}</span>
              <span className="min-w-0 truncate">{formatDate(readString(commit, "authoredAt"), locale)}</span>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function GitWriteDetails({ name, output, t }: { name: string; output: UnknownRecord | null; t: Translator }) {
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (output.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const commit = toRecord(output.commit);
  const statusKey = name === "builtin_git_stage"
    ? "transcript.codeGitStaged"
    : name === "builtin_git_unstage"
      ? "transcript.codeGitUnstaged"
      : "transcript.codeGitCommitted";
  return (
    <div className="space-y-2">
      <div className="flex min-w-0 items-center gap-2 text-[12px]">
        <CheckCircle2 className="size-3.5 shrink-0 text-success" />
        <span className="shrink-0 text-foreground/90">{t(statusKey)}</span>
        {commit ? <code className="min-w-0 truncate font-mono text-muted-foreground">{readString(commit, "shortHash")}</code> : null}
        {commit ? <span className="min-w-0 truncate text-muted-foreground">{readString(commit, "subject")}</span> : null}
      </div>
      <MetricRow
        metrics={[
          metric(readNumber(output, "stagedCount"), t("transcript.codeStaged")),
          metric(readNumber(output, "unstagedCount"), t("transcript.codeUnstaged")),
          metric(readNumber(output, "untrackedCount"), t("transcript.codeUntracked")),
          metric(readNumber(output, "conflictedCount"), t("transcript.codeConflicts")),
        ]}
      />
      <GitStatusFileList files={readRecordArray(output, "files")} t={t} />
    </div>
  );
}

function FileDetails({
  callID,
  input,
  locale,
  name,
  output,
  sessionID,
  t,
}: {
  callID?: string;
  input: UnknownRecord | null;
  locale: string;
  name: string;
  output: UnknownRecord | null;
  sessionID?: string;
  t: Translator;
}) {
  if (output?.ok === false) {
    return <ErrorDetail output={output} t={t} />;
  }
  const path = preferredPath(output) || preferredPath(input);
  const entries = readRecordArray(output, "entries");
  const matches = readRecordArray(output, "matches");
  const content = readString(output, "content");
  const compactPreviewTool = name === "builtin_file_read" || name === "builtin_file_slice" || name === "builtin_file_write";
  const previewContent = name === "builtin_file_write" ? readString(input, "content") : content;
  const previewable = Boolean(sessionID && path && previewContent && compactPreviewTool && output);
  const order = readString(output, "order");
  const lineStart = name === "builtin_file_slice" && order === "reverse"
    ? readNumber(output, "end") ?? 1
    : readNumber(output, "start") ?? 1;
  if (!output) {
    return <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine>;
  }
  if (compactPreviewTool) {
    const fileType = fileTypeLabel(path);
    const metadata = name === "builtin_file_write"
      ? [fileType, formatOptionalBytes(readNumber(output, "bytes")), t("transcript.codeSaved")]
      : [
          fileType,
          formatCount(readNumber(output, "chars") ?? Array.from(content).length, t("transcript.codeCharacters"), locale),
          name === "builtin_file_slice" || readBoolean(output, "truncated")
            ? t("transcript.codePartialContent")
            : t("transcript.codeReadOnlySnapshot"),
        ];
    return (
      <div className="flex min-w-0 items-center justify-between gap-3">
        <FileResultMeta values={metadata} />
        {previewable ? (
          <Button
            className="shrink-0 text-muted-foreground"
            size="xs"
            type="button"
            variant="ghost"
            onClick={() =>
              requestProjectFileReveal({
                absolutePath: readString(output, "path") || readString(input, "path"),
                fallback: {
                  callID,
                  content: previewContent,
                  lineStart: name === "builtin_file_write" ? 1 : lineStart,
                  lineStep: order === "reverse" ? -1 : 1,
                  path,
                  sessionID: sessionID!,
                  source: name === "builtin_file_slice" ? "slice" : name === "builtin_file_write" ? "write" : "read",
                  truncated: name === "builtin_file_write" ? false : readBoolean(output, "truncated"),
                },
                line: name === "builtin_file_write" ? 1 : lineStart,
                relativePath: readString(output, "relativePath"),
                rootPath: readString(output, "root"),
                sessionID: sessionID!,
              })
            }
          >
            <PanelRightOpen />
            {t("transcript.codeOpenPreview")}
          </Button>
        ) : null}
      </div>
    );
  }
  if (name === "builtin_file_patch") {
    return (
      <FileResultMeta
        values={[
          replace(t("transcript.codeReplacementCount"), { count: formatNumber(readNumber(output, "replacements") ?? 0, locale) }),
          t("transcript.codeUpdated"),
        ]}
      />
    );
  }
  if (name === "builtin_file_delete") {
    return <FileResultMeta values={[t("transcript.codeDeleted")]} />;
  }
  if (name === "builtin_file_copy") {
    return (
      <FileResultMeta
        values={[
          fileObjectTypeLabel(readString(output, "copied"), t),
          formatOptionalBytes(readNumber(output, "bytes")),
          t("transcript.codeCopied"),
        ]}
      />
    );
  }
  if (name === "builtin_file_move") {
    return <FileResultMeta values={[t("transcript.codeMoved")]} />;
  }
  if (name === "builtin_file_stat") {
    if (output.exists === false) {
      return <FileResultMeta values={[t("transcript.codeMissing")]} />;
    }
    const objectType = readString(output, "type");
    const mtime = readString(output, "mtime");
    return (
      <FileResultMeta
        values={[
          fileObjectTypeLabel(objectType, t),
          formatOptionalBytes(objectType === "file" ? readNumber(output, "size") : null),
          mtime ? replace(t("transcript.codeModifiedAt"), { time: formatDate(mtime, locale) }) : "",
        ]}
      />
    );
  }
  if (name === "builtin_file_list") {
    return (
      <div className="space-y-2">
        <FileResultMeta
          values={[
            replace(t("transcript.codeItems"), { count: formatNumber(readNumber(output, "totalCount") ?? entries.length, locale) }),
            readBoolean(output, "truncated") ? t("transcript.codePartialResults") : "",
          ]}
        />
        {entries.length > 0 ? <FileEntryList entries={entries} t={t} /> : null}
      </div>
    );
  }
  if (name === "builtin_file_search") {
    return (
      <div className="space-y-2">
        <FileResultMeta
          values={[
            replace(t("transcript.codeMatches"), { count: formatNumber(readNumber(output, "matchCount") ?? matches.length, locale) }),
            replace(t("transcript.codeScannedFiles"), { count: formatNumber(readNumber(output, "filesScanned") ?? 0, locale) }),
            readString(output, "searchType") === "regex" ? t("transcript.codeRegexSearch") : t("transcript.codeLiteralSearch"),
            output.caseSensitive === false ? t("transcript.codeCaseInsensitive") : "",
            readBoolean(output, "resultsCapped") ? t("transcript.codePartialResults") : "",
          ]}
        />
        {matches.length > 0 ? (
          <FileMatchList
            callID={callID}
            matches={matches}
            rootPath={readString(output, "root")}
            sessionID={sessionID}
            t={t}
          />
        ) : null}
      </div>
    );
  }
  return null;
}

function FileEntryList({ entries, t }: { entries: UnknownRecord[]; t: Translator }) {
  const visible = entries.slice(0, 200);
  return (
    <div className="max-h-72 overflow-auto border-t border-border/50 pt-1">
      {visible.map((entry, index) => {
        const directory = readString(entry, "type") === "dir";
        return (
          <div key={`${readString(entry, "path")}:${index}`} className="grid min-h-6 grid-cols-[1rem_minmax(0,1fr)_auto] items-center gap-1 text-[11px]">
            {directory ? <Folder className="size-3 text-muted-foreground" /> : <File className="size-3 text-muted-foreground" />}
            <span className="min-w-0 truncate font-mono text-foreground/85">{readString(entry, "name") || readString(entry, "path")}</span>
            {readNumber(entry, "size") != null ? <span className="text-muted-foreground">{formatBytes(readNumber(entry, "size") || 0)}</span> : null}
          </div>
        );
      })}
      <OmittedCount count={entries.length - visible.length} t={t} />
    </div>
  );
}

function FileMatchList({ callID, matches, rootPath, sessionID, t }: { callID?: string; matches: UnknownRecord[]; rootPath?: string; sessionID?: string; t: Translator }) {
  const visible = matches.slice(0, 200);
  return (
    <div className="max-h-72 overflow-auto border-t border-border/50 pt-1">
      {visible.map((match, index) => {
        const path = readString(match, "path");
        const line = readNumber(match, "line") ?? 1;
        const excerpt = readString(match, "excerpt") || readString(match, "text");
        const previewable = Boolean(sessionID && path && excerpt);
        const content = (
          <>
            <code className="truncate font-mono text-muted-foreground">{path}:{line}</code>
            <span className="whitespace-pre-wrap break-words text-left font-mono leading-4 text-foreground/85">{readString(match, "text")}</span>
          </>
        );
        return previewable ? (
          <button
            key={`${path}:${line}:${index}`}
            className="grid w-full grid-cols-[minmax(0,1fr)] rounded-sm px-1 py-1 text-[11px] hover:bg-muted/60"
            type="button"
            onClick={() => requestProjectFileReveal({
              absolutePath: path,
              fallback: {
                callID,
                content: excerpt,
                lineStart: readNumber(match, "lineStart") ?? line,
                lineStep: 1,
                path,
                sessionID: sessionID!,
                source: "search",
                truncated: true,
              },
              line,
              relativePath: rootPath ? path : undefined,
              rootPath,
              sessionID: sessionID!,
            })}
          >
            {content}
          </button>
        ) : (
          <div key={`${path}:${line}:${index}`} className="grid grid-cols-[minmax(0,1fr)] px-1 py-1 text-[11px]">{content}</div>
        );
      })}
      <OmittedCount count={matches.length - visible.length} t={t} />
    </div>
  );
}

function ErrorDetail({ output, t }: { output: UnknownRecord; t: Translator }) {
  const reason = readString(output, "reason");
  const detail = readString(output, "detail") || readString(output, "error") || reason;
  const friendlyDetail = codeErrorMessage(output, t);
  const hint = codeErrorHint(reason, output, t) || readString(output, "hint");
  return (
    <div className="border-t border-destructive/30 pt-2 text-[11px]">
      <div className="flex items-start gap-1.5 text-destructive">
        <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
        <span className="break-words">{friendlyDetail || detail || t("transcript.toolFailed")}</span>
      </div>
      {hint ? <div className="mt-1 break-words pl-5 text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

function codeLanguageMeta(output: UnknownRecord, t: Translator) {
  const language = readString(output, "language");
  const server = readString(output, "server");
  const languageLabel = language === "go" ? "Go" : language === "typescript" ? "TypeScript / JavaScript" : language;
  return [
    [languageLabel, server].filter(Boolean).join(" · "),
    readBoolean(output, "rootFallback") ? t("transcript.codeRootFallback") : "",
  ].filter(Boolean);
}

function codeErrorMessage(output: UnknownRecord, t: Translator) {
  const reason = readString(output, "reason");
  const server = readString(output, "server") || t("transcript.codeLanguageServer");
  const keys: Record<string, string> = {
    cancelled: "transcript.codeErrorCancelled",
    document_too_large: "transcript.codeErrorDocumentTooLarge",
    invalid_position: "transcript.codeErrorInvalidPosition",
    language_ambiguous: "transcript.codeErrorLanguageAmbiguous",
    language_not_supported: "transcript.codeErrorLanguageUnsupported",
    language_server_capacity: "transcript.codeErrorServerCapacity",
    language_server_crashed: "transcript.codeErrorServerCrashed",
    language_server_initialize_failed: "transcript.codeErrorServerInitialize",
    language_server_protocol_error: "transcript.codeErrorProtocol",
    language_server_start_failed: "transcript.codeErrorServerStart",
    language_server_timeout: "transcript.codeErrorServerTimeout",
    language_server_unavailable: "transcript.codeErrorServerUnavailable",
    mixed_language_targets: "transcript.codeErrorMixedTargets",
    path_not_authorized: "transcript.codeErrorPathUnauthorized",
    rename_failed: "transcript.codeErrorRenameFailed",
    rename_no_changes: "transcript.codeErrorRenameNoChanges",
    rename_not_available: "transcript.codeErrorRenameNotAvailable",
    rename_outside_project: "transcript.codeErrorRenameOutsideProject",
    rename_rejected: "transcript.codeErrorRenameRejected",
    rename_too_large: "transcript.codeErrorRenameTooLarge",
    unsafe_workspace_edit: "transcript.codeErrorUnsafeWorkspaceEdit",
  };
  const key = keys[reason];
  return key ? replace(t(key), { server }) : "";
}

function codeErrorHint(reason: string, output: UnknownRecord, t: Translator) {
  if (reason === "language_server_unavailable") {
    return replace(t("transcript.codeErrorHintInstall"), { server: readString(output, "server") || t("transcript.codeLanguageServer") });
  }
  if (reason === "language_ambiguous") {
    return t("transcript.codeErrorHintLanguage");
  }
  if (reason === "mixed_language_targets") {
    return t("transcript.codeErrorHintSplitTargets");
  }
  return "";
}

function CodeOutput({ destructive, label, text, truncated }: { destructive?: boolean; label: string; text: string; truncated?: boolean }) {
  const { t } = useI18n();
  return (
    <section className="border-t border-border/50 pt-2">
      <div className="mb-1 flex items-center gap-2">
        <DetailLabel>{label}</DetailLabel>
        {truncated ? <span className="text-[10px] text-warning">{t("transcript.codeTruncated")}</span> : null}
      </div>
      <pre className={cn("block max-h-80 w-full min-w-0 max-w-full overflow-auto whitespace-pre font-mono text-[11px] leading-4 text-foreground/80", destructive && "text-destructive/90")}>{text}</pre>
    </section>
  );
}

function MetricRow({ metrics }: { metrics: Array<{ className?: string; label: string; value: number } | null> }) {
  const visible = metrics.filter((item): item is { className?: string; label: string; value: number } => item != null);
  if (visible.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
      {visible.map((item) => (
        <span key={item.label} className={item.className}>
          <strong className="font-mono font-medium text-foreground/90">{item.value}</strong> {item.label}
        </span>
      ))}
    </div>
  );
}

function FileResultMeta({ values }: { values: string[] }) {
  const visible = values.filter(Boolean);
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-[11px] text-muted-foreground">
      {visible.map((value, index) => (
        <span key={`${value}:${index}`} className="inline-flex items-center gap-1.5">
          {index > 0 ? <span aria-hidden="true">·</span> : null}
          <span>{value}</span>
        </span>
      ))}
    </div>
  );
}

function DetailLabel({ children }: { children: ReactNode }) {
  return <div className="text-[10px] font-medium text-muted-foreground">{children}</div>;
}

function EmptyLine({ children }: { children: ReactNode }) {
  return <div className="text-[11px] text-muted-foreground">{children}</div>;
}

function OmittedCount({ count, t }: { count: number; t: Translator }) {
  return count > 0 ? <div className="py-1 text-[10px] text-muted-foreground">{replace(t("transcript.codeMoreItems"), { count: String(count) })}</div> : null;
}

function metric(value: number | null, label: string, className?: string) {
  return value == null ? null : { className, label, value };
}

function toRecord(value: unknown): UnknownRecord | null {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as UnknownRecord;
  }
  if (typeof value !== "string" || !value.trim()) {
    return null;
  }
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as UnknownRecord) : null;
  } catch {
    return null;
  }
}

function readString(record: UnknownRecord | null, key: string) {
  return typeof record?.[key] === "string" ? (record[key] as string) : "";
}

function readNumber(record: UnknownRecord | null, key: string) {
  return typeof record?.[key] === "number" && Number.isFinite(record[key]) ? (record[key] as number) : null;
}

function readBoolean(record: UnknownRecord | null, key: string) {
  return record?.[key] === true;
}

function readStringArray(record: UnknownRecord | null, key: string) {
  const value = record?.[key];
  return Array.isArray(value) && value.every((item) => typeof item === "string") ? (value as string[]) : null;
}

function readRecordArray(record: UnknownRecord | null, key: string) {
  const value = record?.[key];
  return Array.isArray(value) ? value.map(toRecord).filter((item): item is UnknownRecord => item != null) : [];
}

function preferredPath(record: UnknownRecord | null) {
  return readString(record, "relativePath") || readString(record, "path");
}

function formatArgv(argv: string[]) {
  return argv.map((arg) => (/^[A-Za-z0-9_./:=@%+,\-]+$/.test(arg) ? arg : JSON.stringify(arg))).join(" ");
}

function formatDuration(milliseconds: number) {
  if (milliseconds < 1000) {
    return `${Math.max(0, Math.round(milliseconds))}ms`;
  }
  return `${(milliseconds / 1000).toFixed(milliseconds < 10000 ? 1 : 0)}s`;
}

function formatBytes(bytes: number) {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatOptionalBytes(bytes: number | null) {
  return bytes == null ? "" : formatBytes(bytes);
}

function formatCount(value: number, label: string, locale: string) {
  return `${formatNumber(value, locale)} ${label}`;
}

function formatNumber(value: number, locale: string) {
  return new Intl.NumberFormat(locale).format(value);
}

function fileObjectTypeLabel(type: string, t: Translator) {
  switch (type) {
    case "file":
      return t("transcript.codeFile");
    case "dir":
    case "directory":
      return t("transcript.codeDirectory");
    case "symlink":
      return t("transcript.codeSymlink");
    case "other":
      return t("transcript.codeOtherFileType");
    default:
      return "";
  }
}

function fileTypeLabel(path: string) {
  const filename = path.split(/[\\/]/).filter(Boolean).pop() || "";
  const dot = filename.lastIndexOf(".");
  if (dot <= 0 || dot === filename.length - 1) {
    return "";
  }
  const extension = filename.slice(dot + 1).toLowerCase();
  const labels: Record<string, string> = {
    cjs: "JavaScript",
    css: "CSS",
    go: "Go",
    htm: "HTML",
    html: "HTML",
    js: "JavaScript",
    json: "JSON",
    jsx: "JSX",
    markdown: "Markdown",
    md: "Markdown",
    mjs: "JavaScript",
    py: "Python",
    sh: "Shell",
    ts: "TypeScript",
    tsx: "TSX",
    txt: "Text",
    yaml: "YAML",
    yml: "YAML",
    zsh: "Shell",
  };
  return labels[extension] || extension.toUpperCase();
}

function formatDate(value: string, locale: string) {
  const date = new Date(value);
  if (!value || Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function gitKindTone(kind: string) {
  if (kind === "added" || kind === "untracked") {
    return "text-success";
  }
  if (kind === "deleted" || kind === "conflicted") {
    return "text-destructive";
  }
  return "text-warning";
}

function countSummary(record: UnknownRecord | null, key: string, messageKey: string, t: Translator) {
  const count = readNumber(record, key);
  return count == null ? "" : replace(t(messageKey), { count: String(count) });
}

function verificationStatusLabel(kind: string, status: string, t: Translator) {
  const kindLabel = t(`transcript.codeVerificationKind.${kind}`);
  const statusKey = status === "timed_out" ? "timedOut" : status;
  return replace(t(`transcript.codeVerificationStatus.${statusKey}`), { kind: kindLabel });
}

function replace(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce((text, [key, value]) => text.replace(`{${key}}`, value), template);
}

function shortHash(value: string) {
  return value.slice(0, 8);
}

function compactText(value: string, limit: number) {
  return value.length > limit ? `${value.slice(0, limit)}...` : value;
}
