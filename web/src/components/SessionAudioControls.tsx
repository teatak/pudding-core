import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Mic, Volume2 } from "lucide-react";
import { toast } from "sonner";

import {
  bindAudioInput,
  bindAudioOutput,
  cancelAudioRuntimeInstall,
  getAudioRuntime,
  startAudioRuntimeInstall,
  APIError,
  type AudioBindings,
  type AudioRuntimeStatus,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";

export function SessionAudioControls({ bindings, token, sessionID }: { bindings?: AudioBindings; token: string; sessionID: string }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [runtimeDialogOpen, setRuntimeDialogOpen] = useState(false);
  const [runtimeInstallRequested, setRuntimeInstallRequested] = useState(false);
  const [checkingRuntime, setCheckingRuntime] = useState(false);
  const inputActive = bindings?.inputOwner === sessionID;
  const outputActive = bindings?.outputOwner === sessionID;
  const inputLevel = inputActive ? bindings?.inputLevel ?? 0 : 0;
  const invalidateAudioBindings = () => queryClient.invalidateQueries({ queryKey: queryKeys.audioBindings() });
  const setBindings = (next: AudioBindings) => {
    queryClient.setQueryData(queryKeys.audioBindings(), { bindings: next });
    void invalidateAudioBindings();
  };
  const inputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioInput(token, sessionID, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", outputOwner: "", inputLevel: 0 };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          inputOwner: enabled ? sessionID : current.inputOwner === sessionID ? "" : current.inputOwner,
          inputLevel: enabled ? 0 : current.inputOwner === sessionID ? 0 : current.inputLevel,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(apiErrorMessage(error, t("voice.inputFailed"), t));
    },
  });
  const outputMutation = useMutation({
    mutationFn: (enabled: boolean) => bindAudioOutput(token, sessionID, enabled),
    onMutate: async (enabled) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.audioBindings() });
      const previous = queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings());
      const current = previous?.bindings ?? { inputOwner: "", outputOwner: "", inputLevel: 0 };
      queryClient.setQueryData(queryKeys.audioBindings(), {
        bindings: {
          ...current,
          outputOwner: enabled ? sessionID : current.outputOwner === sessionID ? "" : current.outputOwner,
        },
      });
      return { previous };
    },
    onSuccess: (result) => setBindings(result.bindings),
    onError: (error, _enabled, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.audioBindings(), context.previous);
      }
      toast.error(apiErrorMessage(error, t("voice.outputFailed"), t));
    },
  });
  const runtimeQuery = useQuery({
    queryKey: queryKeys.audioRuntime(),
    queryFn: () => getAudioRuntime(token),
    enabled: runtimeDialogOpen,
    refetchInterval: runtimeDialogOpen ? 750 : false,
  });
  const runtimeStatus = runtimeQuery.data;
  const runtimeRunning = runtimeStatus?.running ?? false;
  const startRuntimeMutation = useMutation({
    mutationFn: () => startAudioRuntimeInstall(token),
    onMutate: () => {
      setRuntimeInstallRequested(true);
    },
    onSuccess: (status) => {
      queryClient.setQueryData(queryKeys.audioRuntime(), status);
      if (status.disabled || (!status.running && !status.installed)) {
        setRuntimeInstallRequested(false);
      }
    },
    onError: (error) => {
      setRuntimeInstallRequested(false);
      toast.error(apiErrorMessage(error, t("voice.runtimeFailed"), t));
    },
  });
  const runtimeStarting = startRuntimeMutation.isPending;
  const cancelRuntimeMutation = useMutation({
    mutationFn: () => cancelAudioRuntimeInstall(token),
    onSuccess: (status) => {
      queryClient.setQueryData(queryKeys.audioRuntime(), status);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.audioRuntime() });
    },
  });

  useEffect(() => {
    if (!runtimeInstallRequested || runtimeRunning || !runtimeStatus?.installed || inputActive || inputMutation.isPending) {
      return;
    }
    setRuntimeInstallRequested(false);
    setRuntimeDialogOpen(false);
    inputMutation.mutate(true);
  }, [inputActive, inputMutation, runtimeInstallRequested, runtimeRunning, runtimeStatus?.installed]);

  const openInput = async () => {
    setCheckingRuntime(true);
    try {
      const status = await queryClient.fetchQuery({
        queryKey: queryKeys.audioRuntime(),
        queryFn: () => getAudioRuntime(token),
        staleTime: 0,
      });
      queryClient.setQueryData(queryKeys.audioRuntime(), status);
      if (status.disabled) {
        toast.error(t("voice.runtimeDisabled"));
        return;
      }
      if (!status.installed) {
        setRuntimeInstallRequested(false);
        setRuntimeDialogOpen(true);
        return;
      }
      inputMutation.mutate(true);
    } catch (error) {
      toast.error(apiErrorMessage(error, t("voice.inputFailed"), t));
    } finally {
      setCheckingRuntime(false);
    }
  };
  const closeRuntimeDialog = () => {
    if (runtimeRunning) {
      cancelRuntimeMutation.mutate(undefined, {
        onSettled: () => {
          setRuntimeInstallRequested(false);
          setRuntimeDialogOpen(false);
        },
      });
      return;
    }
    setRuntimeInstallRequested(false);
    setRuntimeDialogOpen(false);
  };
  const handleRuntimeOpenChange = (open: boolean) => {
    if (open) {
      setRuntimeDialogOpen(true);
      return;
    }
    closeRuntimeDialog();
  };
  const handleInputClick = () => {
    if (inputActive) {
      inputMutation.mutate(false);
      return;
    }
    void openInput();
  };

  return (
    <>
      <AudioControlButtons
        inputActive={inputActive}
        inputLabel={inputActive ? t("voice.inputOn") : t("voice.inputOff")}
        inputLevel={inputLevel}
        inputBusy={inputMutation.isPending || checkingRuntime}
        inputPending={checkingRuntime || (inputMutation.isPending && inputMutation.variables === true)}
        outputActive={outputActive}
        outputLabel={outputActive ? t("voice.outputOn") : t("voice.outputOff")}
        outputBusy={outputMutation.isPending}
        outputPending={outputMutation.isPending && outputMutation.variables === true}
        controlsLabel={t("voice.controls")}
        onInputClick={handleInputClick}
        onOutputClick={() => outputMutation.mutate(!outputActive)}
      />
      <Dialog open={runtimeDialogOpen} onOpenChange={handleRuntimeOpenChange}>
        <DialogContent
          className="sm:max-w-md"
          showCloseButton={!runtimeStarting}
          onEscapeKeyDown={(event) => {
            if (runtimeRunning || runtimeStarting) {
              event.preventDefault();
            }
          }}
          onPointerDownOutside={(event) => {
            if (runtimeRunning || runtimeStarting) {
              event.preventDefault();
            }
          }}
        >
          <DialogHeader>
            <DialogTitle>{runtimeRunning ? t("voice.runtimeDownloading") : t("voice.runtimeTitle")}</DialogTitle>
            <DialogDescription>{runtimeDialogDescription(runtimeStatus, t)}</DialogDescription>
          </DialogHeader>
          <RuntimeDownloadBody status={runtimeStatus} t={t} loading={runtimeQuery.isFetching && !runtimeStatus} />
          <DialogFooter>
            {runtimeRunning ? (
              <Button type="button" variant="destructive" onClick={closeRuntimeDialog} disabled={cancelRuntimeMutation.isPending}>
                {cancelRuntimeMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                {t("voice.runtimeCancel")}
              </Button>
            ) : runtimeStatus?.installed ? (
              <Button type="button" onClick={closeRuntimeDialog}>
                {t("voice.runtimeClose")}
              </Button>
            ) : (
              <>
                <Button type="button" variant="outline" onClick={closeRuntimeDialog} disabled={runtimeStarting}>
                  {t("common.cancel")}
                </Button>
                <Button type="button" onClick={() => startRuntimeMutation.mutate()} disabled={startRuntimeMutation.isPending}>
                  {startRuntimeMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                  {t("voice.runtimeDownload")}
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function apiErrorMessage(error: unknown, fallback: string, t: (key: string) => string) {
  if (error instanceof APIError) {
    if (error.code === "audio_input_unavailable") {
      return t("voice.inputUnavailable");
    }
    if (error.code === "audio_unavailable") {
      return t("voice.audioUnavailable");
    }
    if (error.code === "audio_binding_failed") {
      return fallback;
    }
    return fallback;
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

function runtimeDialogDescription(status: AudioRuntimeStatus | undefined, t: (key: string) => string) {
  if (status?.error) {
    return t("voice.runtimeFailed");
  }
  if (status?.installed) {
    return t("voice.runtimeReady");
  }
  return t("voice.runtimeMissing");
}

function RuntimeDownloadBody({ status, t, loading }: { status?: AudioRuntimeStatus; t: (key: string) => string; loading: boolean }) {
  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        {t("common.loading")}
      </div>
    );
  }
  const progress = runtimeProgress(status);
  return (
    <div className="grid gap-3">
      {status?.running ? (
        <div className="grid gap-2">
          <div className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${progress}%` }} />
          </div>
          <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
            <span className="min-w-0 truncate">
              {formatTemplate(t("voice.runtimeAssetProgress"), {
                index: status.assetIndex || 1,
                total: status.assetTotal || 1,
                asset: status.currentAsset || status.message || "",
              })}
            </span>
            <span className="shrink-0">
              {status.bytesTotal
                ? `${formatBytes(status.bytesDownloaded)} / ${formatBytes(status.bytesTotal)}`
                : status.bytesDownloaded
                  ? formatBytes(status.bytesDownloaded)
                  : t("voice.runtimeUnknownSize")}
            </span>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function runtimeProgress(status?: AudioRuntimeStatus) {
  if (!status?.running) {
    return 0;
  }
  if (status.bytesTotal && status.bytesTotal > 0) {
    return Math.max(2, Math.min(100, ((status.bytesDownloaded ?? 0) / status.bytesTotal) * 100));
  }
  const index = Math.max(1, status.assetIndex || 1);
  const total = Math.max(index, status.assetTotal || index);
  return Math.max(8, Math.min(92, ((index - 1) / total) * 100 + 8));
}

function formatBytes(bytes?: number) {
  if (!bytes || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

function formatTemplate(template: string, values: Record<string, string | number>) {
  return template.replace(/\{(\w+)\}/g, (_, key) => String(values[key] ?? ""));
}

export function AudioControlButtons({
  controlsLabel,
  inputActive,
  inputLabel,
  inputLevel,
  inputBusy,
  inputPending,
  outputActive,
  outputLabel,
  outputBusy,
  outputPending,
  onInputClick,
  onOutputClick,
}: {
  controlsLabel: string;
  inputActive: boolean;
  inputLabel: string;
  inputLevel: number;
  inputBusy?: boolean;
  inputPending: boolean;
  outputActive: boolean;
  outputLabel: string;
  outputBusy?: boolean;
  outputPending: boolean;
  onInputClick: () => void;
  onOutputClick: () => void;
}) {
  const inputDisabled = inputBusy ?? inputPending;
  const outputDisabled = outputBusy ?? outputPending;
  return (
    <div className="flex shrink-0 items-center gap-1" aria-label={controlsLabel}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={inputDisabled}
            aria-label={inputLabel}
            aria-pressed={inputActive}
            className="relative overflow-hidden rounded-full"
            size="icon"
            type="button"
            variant={inputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (!inputDisabled) {
                onInputClick();
              }
            }}
          >
            {inputPending ? <Loader2 className="size-4 animate-spin" /> : <MicButtonContent active={inputActive} level={inputLevel} />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{inputLabel}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-disabled={outputDisabled}
            aria-label={outputLabel}
            aria-pressed={outputActive}
            className="rounded-full"
            size="icon"
            type="button"
            variant={outputActive ? "default" : "ghost"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (!outputDisabled) {
                onOutputClick();
              }
            }}
          >
            {outputPending ? <Loader2 className="size-4 animate-spin" /> : <Volume2 className="size-4" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{outputLabel}</TooltipContent>
      </Tooltip>
    </div>
  );
}

function MicButtonContent({ active, level }: { active: boolean; level: number }) {
  if (!active) {
    return <Mic className="size-4" />;
  }
  const normalized = Math.max(0, Math.min(1, Math.sqrt(Math.max(0, level)) * 2.2));
  const profiles = [0.45, 0.75, 1, 0.72, 0.5];
  return (
    <span className="pointer-events-none flex h-5 w-5 items-center justify-center gap-0.5">
      {profiles.map((profile, index) => (
        <span
          key={index}
          className="w-0.5 rounded-full bg-primary-foreground transition-all duration-100 ease-out"
          style={{ height: `${5 + normalized * profile * 14}px` }}
        />
      ))}
    </span>
  );
}
