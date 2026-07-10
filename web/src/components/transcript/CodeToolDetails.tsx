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
import { openFilePreview } from "@/state/filePreviewStore";

const commandToolName = "builtin_command_run";
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
  return name === commandToolName || gitToolNames.has(name) || patchToolNames.has(name) || fileToolNames.has(name);
}

export function codeToolSummary(name: string, args: unknown, result: unknown, t: Translator) {
  if (!isCodeToolName(name)) {
    return "";
  }
  const input = toRecord(args);
  const output = toRecord(result);
  if (name === commandToolName) {
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
    return countSummary(output, "matchCount", "transcript.codeMatches", t) || path;
  }
  if (name === "builtin_file_list") {
    return countSummary(output, "totalCount", "transcript.codeItems", t) || path;
  }
  return path;
}

export function CodeToolDetails({
  args,
  callID,
  name,
  result,
  sessionID,
}: {
  args: unknown;
  callID?: string;
  name: string;
  result: unknown;
  sessionID?: string;
}) {
  const { locale, t } = useI18n();
  const input = toRecord(args);
  const output = toRecord(result);
  let body: ReactNode;
  if (name === commandToolName) {
    body = <CommandDetails input={input} output={output} t={t} />;
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
  return <>{body}</>;
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

function CommandDetails({ input, output, t }: { input: UnknownRecord | null; output: UnknownRecord | null; t: Translator }) {
  const argv = readStringArray(output, "argv") || readStringArray(input, "argv") || [];
  const cwd = readString(output, "cwd") || readString(input, "cwd");
  const stdout = readString(output, "stdout");
  const stderr = readString(output, "stderr");
  const terminalOutput = joinTerminalOutput(stdout, stderr);
  const exitCode = readNumber(output, "exitCode");
  const duration = readNumber(output, "durationMs");
  const timedOut = readBoolean(output, "timedOut");
  const cancelled = readBoolean(output, "cancelled");
  const processCompleted = exitCode != null && exitCode >= 0 && !timedOut && !cancelled;
  const toolFailed = output?.ok === false && !processCompleted;
  const commandSucceeded = processCompleted && exitCode === 0;
  const StatusIcon = timedOut || cancelled ? Clock3 : toolFailed ? XCircle : commandSucceeded ? CheckCircle2 : processCompleted ? CircleAlert : Clock3;
  const statusText = timedOut
    ? t("transcript.codeTimedOut")
    : cancelled
      ? t("transcript.codeCancelled")
      : exitCode != null
        ? replace(t("transcript.codeExitCode"), { code: String(exitCode) })
        : t("transcript.codeRunning");
  const command = formatArgv(argv);
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
        <pre className={cn("max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-foreground/80", toolFailed && "text-destructive/90")}>
          {terminalOutput || t("transcript.codeNoOutput")}
        </pre>
        <ToolHoverCopyButton className="absolute top-1.5 right-1.5 group-hover/terminal-copy:opacity-100" text={terminalOutput} />
      </section>
      <div className="flex min-w-0 items-center gap-3 border-t border-border/50 px-3 py-2 text-[11px] text-muted-foreground">
        {cwd ? <code className="min-w-0 flex-1 truncate font-mono">{cwd}</code> : <span className="flex-1" />}
        {readBoolean(output, "stdoutTruncated") || readBoolean(output, "stderrTruncated") ? <span className="shrink-0 text-warning">{t("transcript.codeTruncated")}</span> : null}
        {duration != null ? <span className="shrink-0">{formatDuration(duration)}</span> : null}
        <span className={cn("inline-flex shrink-0 items-center gap-1", toolFailed && "text-destructive", commandSucceeded && "text-success", processCompleted && !commandSucceeded && "text-muted-foreground")}>
          <StatusIcon className="size-3.5" />
          {commandSucceeded ? t("transcript.codeSucceeded") : statusText}
        </span>
      </div>
    </div>
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
      className={cn("size-6 bg-transparent opacity-0 transition hover:bg-muted hover:opacity-100 dark:hover:bg-muted/50", className)}
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
  const from = readString(output, "fromRelativePath") || readString(output, "from") || readString(input, "from_path");
  const to = readString(output, "toRelativePath") || readString(output, "to") || readString(input, "to_path");
  const path = preferredPath(output) || preferredPath(input);
  const entries = readRecordArray(output, "entries");
  const matches = readRecordArray(output, "matches");
  const content = readString(output, "content");
  const previewable = Boolean(sessionID && path && content && (name === "builtin_file_read" || name === "builtin_file_slice"));
  const order = readString(output, "order");
  const lineStart = name === "builtin_file_slice" && order === "reverse"
    ? readNumber(output, "end") ?? 1
    : readNumber(output, "start") ?? 1;
  return (
    <div className="space-y-2">
      {from || to ? (
        <div className="flex min-w-0 items-center gap-1 font-mono text-[11px] text-foreground/85">
          <span className="min-w-0 truncate">{from}</span>
          <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate">{to}</span>
        </div>
      ) : path ? (
        <div className="flex min-w-0 items-center gap-1.5 font-mono text-[11px] text-foreground/85">
          {readString(output, "type") === "dir" || name === "builtin_file_list" ? <Folder className="size-3.5 shrink-0 text-muted-foreground" /> : <File className="size-3.5 shrink-0 text-muted-foreground" />}
          <span className="min-w-0 truncate">{path}</span>
        </div>
      ) : null}
      <FileMetadata name={name} output={output} locale={locale} t={t} />
      {entries.length > 0 ? <FileEntryList entries={entries} t={t} /> : null}
      {matches.length > 0 ? <FileMatchList matches={matches} t={t} /> : null}
      {previewable ? (
        <Button
          className="w-fit text-muted-foreground"
          size="xs"
          type="button"
          variant="ghost"
          onClick={() =>
            openFilePreview({
              callID,
              content,
              lineStart,
              lineStep: order === "reverse" ? -1 : 1,
              path,
              sessionID: sessionID!,
              source: name === "builtin_file_slice" ? "slice" : "read",
              truncated: readBoolean(output, "truncated"),
            })
          }
        >
          <PanelRightOpen />
          {t("transcript.codeOpenPreview")}
        </Button>
      ) : content ? (
        <CodeOutput label={t("transcript.codeContent")} text={content} truncated={readBoolean(output, "truncated")} />
      ) : null}
      {!output && !path && !from && !to ? <EmptyLine>{t("transcript.codeWaitingResult")}</EmptyLine> : null}
    </div>
  );
}

function FileMetadata({ name, output, locale, t }: { name: string; output: UnknownRecord | null; locale: string; t: Translator }) {
  if (!output) {
    return null;
  }
  const metrics = [
    metric(readNumber(output, "bytes"), t("transcript.codeBytes")),
    metric(readNumber(output, "replacements"), t("transcript.codeReplacements")),
    metric(readNumber(output, "chars"), t("transcript.codeCharacters")),
    metric(readNumber(output, "lines"), t("transcript.codeLines")),
    metric(readNumber(output, "matchCount"), t("transcript.codeMatchesLabel")),
    metric(readNumber(output, "filesScanned"), t("transcript.codeFilesScanned")),
  ];
  const type = readString(output, "type") || readString(output, "copied");
  const size = readNumber(output, "size");
  const mtime = readString(output, "mtime");
  const exists = output.exists;
  return (
    <>
      <MetricRow metrics={metrics} />
      {name === "builtin_file_stat" ? (
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
          {typeof exists === "boolean" ? <span>{exists ? t("transcript.codeExists") : t("transcript.codeMissing")}</span> : null}
          {type ? <span>{type}</span> : null}
          {size != null ? <span>{formatBytes(size)}</span> : null}
          {mtime ? <span>{formatDate(mtime, locale)}</span> : null}
        </div>
      ) : null}
    </>
  );
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

function FileMatchList({ matches, t }: { matches: UnknownRecord[]; t: Translator }) {
  const visible = matches.slice(0, 200);
  return (
    <div className="max-h-72 overflow-auto border-t border-border/50 pt-1">
      {visible.map((match, index) => (
        <div key={`${readString(match, "path")}:${readNumber(match, "line")}:${index}`} className="grid grid-cols-[minmax(0,1fr)] py-1 text-[11px]">
          <code className="truncate font-mono text-muted-foreground">
            {readString(match, "path")}:{readNumber(match, "line") ?? ""}
          </code>
          <span className="whitespace-pre-wrap break-words font-mono leading-4 text-foreground/85">{readString(match, "text")}</span>
        </div>
      ))}
      <OmittedCount count={matches.length - visible.length} t={t} />
    </div>
  );
}

function ErrorDetail({ output, t }: { output: UnknownRecord; t: Translator }) {
  const reason = readString(output, "reason");
  const detail = readString(output, "detail") || readString(output, "error") || reason;
  const hint = readString(output, "hint");
  return (
    <div className="border-t border-destructive/30 pt-2 text-[11px]">
      <div className="flex items-start gap-1.5 text-destructive">
        <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
        <span className="break-words">{detail || t("transcript.toolFailed")}</span>
      </div>
      {hint ? <div className="mt-1 break-words pl-5 text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

function CodeOutput({ destructive, label, text, truncated }: { destructive?: boolean; label: string; text: string; truncated?: boolean }) {
  const { t } = useI18n();
  return (
    <section className="border-t border-border/50 pt-2">
      <div className="mb-1 flex items-center gap-2">
        <DetailLabel>{label}</DetailLabel>
        {truncated ? <span className="text-[10px] text-warning">{t("transcript.codeTruncated")}</span> : null}
      </div>
      <pre className={cn("max-h-80 overflow-auto whitespace-pre font-mono text-[11px] leading-4 text-foreground/80", destructive && "text-destructive/90")}>{text}</pre>
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

function replace(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce((text, [key, value]) => text.replace(`{${key}}`, value), template);
}

function shortHash(value: string) {
  return value.slice(0, 8);
}

function compactText(value: string, limit: number) {
  return value.length > limit ? `${value.slice(0, limit)}...` : value;
}
