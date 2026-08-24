import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Square, SquareTerminal } from "@/components/icons";
import { useState } from "react";
import { toast } from "sonner";

import {
  getBackgroundProcess,
  listBackgroundProcesses,
  stopBackgroundProcess,
  type BackgroundProcess,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { Spinner } from "@/components/Spinner";
import { composerControlStateClassName } from "@/components/composerControlStyles";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export function BackgroundProcessControl({ token, sessionID }: { token: string; sessionID: string }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [expandedProcessID, setExpandedProcessID] = useState<string>();
  const [recentOpen, setRecentOpen] = useState(false);
  const query = useQuery({
    queryKey: queryKeys.backgroundProcesses(sessionID),
    queryFn: () => listBackgroundProcesses(token, sessionID),
    enabled: Boolean(token && sessionID),
  });
  const stopMutation = useMutation({
    mutationFn: (processID: string) => stopBackgroundProcess(token, sessionID, processID),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.backgroundProcesses(sessionID) }),
    onError: () => toast.error(t("composer.backgroundProcessStopFailed")),
  });
  const processes = query.data?.processes ?? [];
  const running = processes.filter((process) => process.running);
  const recent = processes.filter((process) => !process.running);
  if (processes.length === 0) {
    return null;
  }
  const label = t("composer.backgroundProcessesCount").replace("{count}", String(running.length));
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          aria-label={label}
          className={cn(
            "h-8 gap-1 rounded-full px-2 text-xs font-normal text-muted-foreground hover:text-foreground",
            composerControlStateClassName,
          )}
          size="sm"
          type="button"
          variant="ghost"
        >
          <SquareTerminal className="size-3.5" />
          {running.length > 0 ? <span className="tabular-nums">{running.length}</span> : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-96 gap-0 overflow-hidden p-1.5"
        collisionPadding={12}
        side="top"
        sideOffset={8}
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        {running.length > 0 ? (
          <ProcessSectionLabel count={running.length} label={t("composer.backgroundProcessesRunning")} />
        ) : null}
        <div className="max-h-72 overflow-y-auto">
          {running.map((process) => (
            <BackgroundProcessRow
              key={process.processID}
              expanded={expandedProcessID === process.processID}
              process={process}
              sessionID={sessionID}
              stopping={stopMutation.isPending && stopMutation.variables === process.processID}
              token={token}
              onStop={() => stopMutation.mutate(process.processID)}
              onToggle={() => setExpandedProcessID((current) => current === process.processID ? undefined : process.processID)}
            />
          ))}
          {recent.length > 0 ? (
            <Collapsible open={recentOpen} onOpenChange={setRecentOpen}>
              <CollapsibleTrigger asChild>
                <button
                  className="flex h-8 w-full items-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground hover:bg-control-hover hover:text-foreground active:bg-control-active"
                  type="button"
                >
                  <ChevronRight className={cn("size-3.5 transition-transform", recentOpen && "rotate-90")} />
                  <span>{t("composer.backgroundProcessesRecent")}</span>
                  <span className="tabular-nums">{recent.length}</span>
                </button>
              </CollapsibleTrigger>
              <CollapsibleContent>
                {recent.map((process) => (
                  <BackgroundProcessRow
                    key={process.processID}
                    expanded={expandedProcessID === process.processID}
                    process={process}
                    sessionID={sessionID}
                    stopping={false}
                    token={token}
                    onToggle={() => setExpandedProcessID((current) => current === process.processID ? undefined : process.processID)}
                  />
                ))}
              </CollapsibleContent>
            </Collapsible>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ProcessSectionLabel({ count, label }: { count: number; label: string }) {
  return (
    <div className="flex h-7 items-center justify-between px-2 text-[11px] font-medium text-muted-foreground">
      <span>{label}</span>
      <span className="tabular-nums">{count}</span>
    </div>
  );
}

function BackgroundProcessRow({
  expanded,
  process,
  sessionID,
  stopping,
  token,
  onStop,
  onToggle,
}: {
  expanded: boolean;
  process: BackgroundProcess;
  sessionID: string;
  stopping: boolean;
  token: string;
  onStop?: () => void;
  onToggle: () => void;
}) {
  const { t } = useI18n();
  const logsQuery = useQuery({
    queryKey: queryKeys.backgroundProcess(sessionID, process.processID),
    queryFn: () => getBackgroundProcess(token, sessionID, process.processID),
    enabled: expanded,
    refetchInterval: (query) => query.state.data?.process.running ? 1_000 : false,
  });
  const command = processCommand(process);
  const output = logsQuery.data?.output.map((chunk) => chunk.content).join("") ?? "";
  const status = processStatus(process, t);
  return (
    <div className="rounded-md hover:bg-item-hover focus-within:bg-item-hover">
      <div className="flex min-w-0 items-center gap-1">
        <button className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left" type="button" onClick={onToggle}>
          <span
            aria-hidden="true"
            className={cn("size-1.5 shrink-0 rounded-full", process.sandboxDenied ? "bg-amber-500" : process.running ? "bg-emerald-500" : "bg-muted-foreground/40")}
          />
          <div className="min-w-0 flex-1">
            <div className="truncate font-mono text-xs" >{command}</div>
            <div className="truncate text-[10px] leading-4 text-muted-foreground" >
              {status} · {process.cwd}
            </div>
          </div>
          <ChevronRight className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-90")} />
        </button>
        {process.running && onStop ? (
          <Button
            aria-label={t("composer.backgroundProcessStop")}
            className="mr-1 size-6 shrink-0"
            disabled={stopping}
            size="icon"
            type="button"
            variant="ghost"
            onClick={onStop}
          >
            {stopping ? <Spinner className="size-3" /> : <Square className="size-3 fill-current" />}
          </Button>
        ) : null}
      </div>
      {expanded ? (
        <div className="px-2 pb-2 pl-5">
          {logsQuery.isPending ? (
            <div className="flex h-10 items-center justify-center"><Spinner className="size-3.5" /></div>
          ) : (
            <div className="rounded-md bg-muted/50 px-2 py-1.5">
              {logsQuery.data?.truncated ? (
                <div className="pb-1 text-[10px] text-muted-foreground">{t("composer.backgroundProcessOutputTruncated")}</div>
              ) : null}
              <pre className="max-h-36 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-4 text-foreground/80">
                {output || t("composer.backgroundProcessNoOutput")}
              </pre>
            </div>
          )}
        </div>
      ) : null}
    </div>
  );
}

function processStatus(process: BackgroundProcess, t: (key: string) => string) {
  if (process.sandboxDenied) {
    return t("composer.backgroundProcessSandboxDenied");
  }
  if (process.running) {
    return t("composer.backgroundProcessRunning");
  }
  if (process.reason === "stopped" || process.reason === "session_closed" || process.reason === "daemon_closed") {
    return t("composer.backgroundProcessStopped");
  }
  if (process.exitCode && process.exitCode !== 0) {
    return t("composer.backgroundProcessExitCode").replace("{code}", String(process.exitCode));
  }
  return t("composer.backgroundProcessFinished");
}

function processCommand(process: BackgroundProcess) {
  return process.command || process.processID;
}
